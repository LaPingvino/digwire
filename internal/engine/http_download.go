package engine

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type ChunkSpec struct {
	Index int64
	Start int64
	End   int64 // inclusive
}

type MirrorStat struct {
	URL          string `json:"url"`
	BytesRead    int64  `json:"bytes_read"`
	DownloadRate int64  `json:"download_rate"`
	LastBytes    int64  `json:"-"`
	LastTime     time.Time `json:"-"`
}

type HTTPTask struct {
	mu             sync.RWMutex
	ID             string                 `json:"id"`
	URL            string                 `json:"url"`
	Mirrors        []string               `json:"mirrors"`
	MirrorStats    map[string]*MirrorStat `json:"mirror_stats"`
	Name           string                 `json:"name"`
	TotalBytes     int64                  `json:"total_bytes"`
	CompletedBytes int64                  `json:"completed_bytes"`
	State          string                 `json:"state"` // "downloading", "paused", "completed", "failed"
	DestPath       string                 `json:"dest_path"`
	SupportsRange  bool                   `json:"supports_range"`
	AddedAt        int64                  `json:"added_at"`
	DownloadRate   int64                  `json:"download_rate"`
	ETASeconds     int64                  `json:"eta_seconds"`
	SuggestedSwarm *SwarmSuggestion       `json:"suggested_swarm,omitempty"`
	
	chunkQueue     chan ChunkSpec         `json:"-"`
	cancel         context.CancelFunc     `json:"-"`
	lastCompleted  int64                  `json:"-"`
	lastTime       time.Time              `json:"-"`
	client         *http.Client           `json:"-"`
	file           *os.File               `json:"-"`
	activeWorkers  sync.WaitGroup         `json:"-"`
}

type HTTPManager struct {
	mu          sync.RWMutex
	tasks       map[string]*HTTPTask
	downloadDir string
}

func NewHTTPManager(downloadDir string) *HTTPManager {
	return &HTTPManager{
		tasks:       make(map[string]*HTTPTask),
		downloadDir: downloadDir,
	}
}

func HashURL(u string) string {
	h := sha1.Sum([]byte(u))
	return hex.EncodeToString(h[:])
}

func extractFilename(resp *http.Response, rawURL string) string {
	cd := resp.Header.Get("Content-Disposition")
	if strings.Contains(cd, "filename=") {
		parts := strings.Split(cd, "filename=")
		if len(parts) > 1 {
			fn := strings.Trim(parts[1], "\"' ")
			if fn != "" {
				return fn
			}
		}
	}
	u, err := url.Parse(rawURL)
	if err == nil {
		base := path.Base(u.Path)
		if base != "" && base != "." && base != "/" {
			return base
		}
	}
	return "download_" + HashURL(rawURL)[:8]
}

func (hm *HTTPManager) StartDownload(rawURL string) (*HTTPTask, error) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	id := HashURL(rawURL)
	if existing, exists := hm.tasks[id]; exists {
		if existing.State == "paused" {
			go existing.resume()
		}
		return existing, nil
	}

	probeClient := &http.Client{
		Timeout: 7 * time.Second,
	}

	// Probe range support and file size
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	req.Header.Set("User-Agent", "Digwire/1.0")
	req.Header.Set("Range", "bytes=0-0")

	resp, err := probeClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return nil, fmt.Errorf("server responded with status: %s", resp.Status)
	}

	supportsRange := resp.StatusCode == http.StatusPartialContent || strings.EqualFold(resp.Header.Get("Accept-Ranges"), "bytes")
	var totalBytes int64 = 0
	cr := resp.Header.Get("Content-Range")
	if cr != "" && strings.Contains(cr, "/") {
		parts := strings.Split(cr, "/")
		if len(parts) > 1 {
			totalBytes, _ = strconv.ParseInt(parts[1], 10, 64)
		}
	}
	if totalBytes == 0 {
		totalBytes, _ = strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	}

	filename := extractFilename(resp, rawURL)
	destPath := filepath.Join(hm.downloadDir, filename)

	stats := make(map[string]*MirrorStat)
	stats[rawURL] = &MirrorStat{
		URL:      rawURL,
		LastTime: time.Now(),
	}

	task := &HTTPTask{
		ID:            id,
		URL:           rawURL,
		Mirrors:       []string{rawURL},
		MirrorStats:   stats,
		Name:          filename,
		TotalBytes:    totalBytes,
		State:         "downloading",
		DestPath:      destPath,
		SupportsRange: supportsRange,
		AddedAt:       time.Now().Unix(),
		lastTime:      time.Now(),
		client:        &http.Client{Timeout: 0},
	}

	hm.tasks[id] = task
	go task.runDownload()
	return task, nil
}

func (t *HTTPTask) runDownload() {
	t.mu.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	destPath := t.DestPath
	partPath := destPath + ".part"
	t.State = "downloading"
	totalBytes := t.TotalBytes
	supportsRange := t.SupportsRange
	t.mu.Unlock()

	// Open or create .part file
	file, err := os.OpenFile(partPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		t.mu.Lock()
		t.State = "failed"
		t.mu.Unlock()
		return
	}
	t.file = file
	defer file.Close()

	if totalBytes > 0 && supportsRange {
		// Multi-mirror Segmented Downloader
		// Chunk size: 2MB to 8MB
		const chunkSize int64 = 4 * 1024 * 1024
		numChunks := (totalBytes + chunkSize - 1) / chunkSize
		t.chunkQueue = make(chan ChunkSpec, numChunks)

		// Populate chunk queue
		for i := int64(0); i < numChunks; i++ {
			start := i * chunkSize
			end := start + chunkSize - 1
			if end >= totalBytes {
				end = totalBytes - 1
			}
			t.chunkQueue <- ChunkSpec{
				Index: i,
				Start: start,
				End:   end,
			}
		}

		t.mu.RLock()
		mirrors := make([]string, len(t.Mirrors))
		copy(mirrors, t.Mirrors)
		t.mu.RUnlock()

		// Launch 2 parallel worker routines per mirror URL
		for _, m := range mirrors {
			for w := 0; w < 2; w++ {
				t.activeWorkers.Add(1)
				go t.mirrorWorker(ctx, m)
			}
		}

		t.activeWorkers.Wait()

		select {
		case <-ctx.Done():
			return
		default:
			if atomic.LoadInt64(&t.CompletedBytes) >= totalBytes {
				_ = file.Sync()
				file.Close()
				_ = os.Rename(partPath, destPath)
				t.mu.Lock()
				t.State = "completed"
				t.DownloadRate = 0
				t.ETASeconds = 0
				t.mu.Unlock()
			}
		}
	} else {
		// Single sequential stream fallback
		t.singleStreamDownload(ctx, partPath, destPath)
	}
}

func (t *HTTPTask) mirrorWorker(ctx context.Context, mirrorURL string) {
	defer t.activeWorkers.Done()

	buf := make([]byte, 64*1024)
	for {
		select {
		case <-ctx.Done():
			return
		case chunk, ok := <-t.chunkQueue:
			if !ok {
				return
			}

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, mirrorURL, nil)
			if err != nil {
				// Requeue chunk on error
				select {
				case t.chunkQueue <- chunk:
				default:
				}
				return
			}
			req.Header.Set("User-Agent", "Digwire/1.0")
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", chunk.Start, chunk.End))

			resp, err := t.client.Do(req)
			if err != nil || (resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK) {
				if resp != nil {
					resp.Body.Close()
				}
				// Requeue chunk on failure
				select {
				case t.chunkQueue <- chunk:
				default:
				}
				return
			}

			offset := chunk.Start
			var chunkBytesRead int64 = 0
			var success = true

			for offset <= chunk.End {
				n, rErr := resp.Body.Read(buf)
				if n > 0 {
					_, wErr := t.file.WriteAt(buf[:n], offset)
					if wErr != nil {
						success = false
						break
					}
					offset += int64(n)
					chunkBytesRead += int64(n)
					atomic.AddInt64(&t.CompletedBytes, int64(n))

					// Record per-mirror stats
					t.mu.Lock()
					if st, exists := t.MirrorStats[mirrorURL]; exists {
						atomic.AddInt64(&st.BytesRead, int64(n))
					}
					t.mu.Unlock()
				}
				if rErr != nil {
					if rErr != io.EOF && offset <= chunk.End {
						success = false
					}
					break
				}
			}
			resp.Body.Close()

			if !success && offset <= chunk.End {
				// Requeue remaining segment
				select {
				case t.chunkQueue <- ChunkSpec{
					Index: chunk.Index,
					Start: offset,
					End:   chunk.End,
				}:
				default:
				}
			}
		}
	}
}

func (t *HTTPTask) singleStreamDownload(ctx context.Context, partPath, destPath string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.URL, nil)
	if err != nil {
		t.mu.Lock()
		t.State = "failed"
		t.mu.Unlock()
		return
	}
	req.Header.Set("User-Agent", "Digwire/1.0")

	resp, err := t.client.Do(req)
	if err != nil {
		if ctx.Err() != context.Canceled {
			t.mu.Lock()
			t.State = "failed"
			t.mu.Unlock()
		}
		return
	}
	defer resp.Body.Close()

	buf := make([]byte, 64*1024)
	for {
		select {
		case <-ctx.Done():
			return
		default:
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				_, writeErr := t.file.Write(buf[:n])
				if writeErr != nil {
					t.mu.Lock()
					t.State = "failed"
					t.mu.Unlock()
					return
				}
				atomic.AddInt64(&t.CompletedBytes, int64(n))
			}
			if readErr != nil {
				if readErr == io.EOF {
					_ = t.file.Sync()
					t.file.Close()
					_ = os.Rename(partPath, destPath)
					t.mu.Lock()
					t.State = "completed"
					t.DownloadRate = 0
					t.ETASeconds = 0
					t.mu.Unlock()
					return
				}
				if ctx.Err() != context.Canceled {
					t.mu.Lock()
					t.State = "failed"
					t.mu.Unlock()
				}
				return
			}
		}
	}
}

func (t *HTTPTask) pause() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cancel != nil {
		t.cancel()
	}
	t.State = "paused"
	t.DownloadRate = 0
}

func (t *HTTPTask) resume() {
	t.mu.Lock()
	if t.State == "downloading" {
		t.mu.Unlock()
		return
	}
	t.mu.Unlock()
	go t.runDownload()
}

func (t *HTTPTask) AddMirror(mirrorURL string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	mirrorURL = strings.TrimSpace(mirrorURL)
	if mirrorURL == "" {
		return
	}
	for _, m := range t.Mirrors {
		if m == mirrorURL {
			return
		}
	}
	t.Mirrors = append(t.Mirrors, mirrorURL)
	if t.MirrorStats == nil {
		t.MirrorStats = make(map[string]*MirrorStat)
	}
	t.MirrorStats[mirrorURL] = &MirrorStat{
		URL:      mirrorURL,
		LastTime: time.Now(),
	}

	// If actively downloading with range support, launch workers for the new mirror immediately!
	if t.State == "downloading" && t.chunkQueue != nil && t.cancel != nil {
		ctx, _ := context.WithCancel(context.Background())
		for w := 0; w < 2; w++ {
			t.activeWorkers.Add(1)
			go t.mirrorWorker(ctx, mirrorURL)
		}
	}
}

func (hm *HTTPManager) UpdateStats(now time.Time) {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	for _, task := range hm.tasks {
		task.mu.Lock()
		if task.State == "downloading" {
			current := atomic.LoadInt64(&task.CompletedBytes)
			deltaSec := now.Sub(task.lastTime).Seconds()
			if deltaSec > 0 {
				deltaBytes := current - task.lastCompleted
				if deltaBytes < 0 {
					deltaBytes = 0
				}
				task.DownloadRate = int64(float64(deltaBytes) / deltaSec)
				if task.TotalBytes > current && task.DownloadRate > 0 {
					task.ETASeconds = (task.TotalBytes - current) / task.DownloadRate
				} else {
					task.ETASeconds = 0
				}
			}
			task.lastCompleted = current
			task.lastTime = now

			// Update per-mirror rates
			for _, st := range task.MirrorStats {
				readCurrent := atomic.LoadInt64(&st.BytesRead)
				mDeltaSec := now.Sub(st.LastTime).Seconds()
				if mDeltaSec > 0 {
					mDeltaBytes := readCurrent - st.LastBytes
					if mDeltaBytes < 0 {
						mDeltaBytes = 0
					}
					st.DownloadRate = int64(float64(mDeltaBytes) / mDeltaSec)
				}
				st.LastBytes = readCurrent
				st.LastTime = now
			}
		} else {
			task.DownloadRate = 0
			task.ETASeconds = 0
		}
		task.mu.Unlock()
	}
}

func (hm *HTTPManager) Pause(id string) error {
	hm.mu.RLock()
	task, exists := hm.tasks[id]
	hm.mu.RUnlock()
	if !exists {
		return fmt.Errorf("task not found")
	}
	task.pause()
	return nil
}

func (hm *HTTPManager) Resume(id string) error {
	hm.mu.RLock()
	task, exists := hm.tasks[id]
	hm.mu.RUnlock()
	if !exists {
		return fmt.Errorf("task not found")
	}
	task.resume()
	return nil
}

func (hm *HTTPManager) Remove(id string, deleteFiles bool) error {
	hm.mu.Lock()
	task, exists := hm.tasks[id]
	if exists {
		task.pause()
		delete(hm.tasks, id)
	}
	hm.mu.Unlock()

	if exists && deleteFiles {
		_ = os.Remove(task.DestPath)
		_ = os.Remove(task.DestPath + ".part")
	}
	return nil
}
