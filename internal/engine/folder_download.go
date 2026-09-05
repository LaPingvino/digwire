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
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"digwire/internal/search"
)

// FolderItemInput is the input payload for each file or subfolder in a folder download request
type FolderItemInput struct {
	URL       string `json:"url"`
	Title     string `json:"title"`
	Artist    string `json:"artist,omitempty"`
	Album     string `json:"album,omitempty"`
	Directory string `json:"directory,omitempty"`
	Path      string `json:"path,omitempty"`
	Size      int64  `json:"size,omitempty"`
}

// FolderFileItem describes an individual file being downloaded within a FolderTask
type FolderFileItem struct {
	URL            string `json:"url"`
	Path           string `json:"path"` // Relative path inside folder, e.g. "Kind of Blue/01 - So What.flac"
	Name           string `json:"name"`
	TotalBytes     int64  `json:"total_bytes"`
	CompletedBytes int64  `json:"completed_bytes"`
	State          string `json:"state"` // "pending", "downloading", "completed", "failed"
	Status         string `json:"status,omitempty"`
	Error          string `json:"error,omitempty"`
}

// FolderTask represents a unified multi-file folder download task
type FolderTask struct {
	mu             sync.RWMutex
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	FolderName     string            `json:"folder_name"`
	DestPath       string            `json:"dest_path"`
	TotalBytes     int64             `json:"total_bytes"`
	CompletedBytes int64             `json:"completed_bytes"`
	Progress       float64           `json:"progress"`
	DownloadRate   int64             `json:"download_rate"`
	ETASeconds     int64             `json:"eta_seconds"`
	State          string            `json:"state"` // "downloading", "paused", "creating_swarm", "seeding", "completed", "failed"
	AddedAt        int64             `json:"added_at"`
	Files          []*FolderFileItem `json:"files"`
	InfoHash       string            `json:"info_hash,omitempty"`
	MagnetURI      string            `json:"magnet_uri,omitempty"`
	ActiveFile     string            `json:"active_file,omitempty"`
	StatusMessage  string            `json:"status_message,omitempty"`
	Error          string            `json:"error,omitempty"`

	cancel         context.CancelFunc `json:"-"`
	ctx            context.Context    `json:"-"`
	isPaused       atomic.Bool        `json:"-"`
	eng            *Engine            `json:"-"`
	workersWg      sync.WaitGroup     `json:"-"`
}

// FolderManager manages unified folder downloads and automated post-download BitTorrent swarm creation
type FolderManager struct {
	mu          sync.RWMutex
	tasks       map[string]*FolderTask
	downloadDir string
	engine      *Engine
	client      *http.Client
}

// NewFolderManager initializes a new FolderManager
func NewFolderManager(downloadDir string, engine *Engine) *FolderManager {
	return &FolderManager{
		tasks:       make(map[string]*FolderTask),
		downloadDir: downloadDir,
		engine:      engine,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func sanitizePathSegment(s string) string {
	s = strings.TrimSpace(s)
	replacer := strings.NewReplacer(
		"/", "-",
		"\\", "-",
		":", "-",
		"*", "",
		"?", "",
		"\"", "",
		"<", "",
		">", "",
		"|", "",
		"\x00", "",
	)
	s = replacer.Replace(s)
	s = strings.Trim(s, " .")
	if s == "" {
		return "unnamed"
	}
	return s
}

func sanitizeRelativePath(rel string) string {
	rel = strings.ReplaceAll(rel, "\\", "/")
	rel = filepath.Clean(rel)
	rel = strings.TrimPrefix(rel, "/")
	parts := strings.Split(rel, "/")
	var cleanParts []string
	for _, p := range parts {
		p = sanitizePathSegment(p)
		if p != "" && p != "." && p != ".." {
			cleanParts = append(cleanParts, p)
		}
	}
	if len(cleanParts) == 0 {
		return "file"
	}
	return filepath.Join(cleanParts...)
}

// StartFolderDownload creates and starts a unified folder-in-folder download task
func (m *FolderManager) StartFolderDownload(name, folderName string, items []FolderItemInput) (*FolderTask, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("no items provided for folder download")
	}

	cleanFolderName := sanitizePathSegment(folderName)
	if cleanFolderName == "" || cleanFolderName == "unnamed" {
		cleanFolderName = sanitizePathSegment(name)
	}
	if cleanFolderName == "" || cleanFolderName == "unnamed" {
		cleanFolderName = fmt.Sprintf("Folder_Download_%d", time.Now().Unix())
	}

	h := sha1.New()
	h.Write([]byte(name + "|" + cleanFolderName + "|" + fmt.Sprintf("%d", time.Now().UnixNano())))
	taskID := "folder_" + hex.EncodeToString(h.Sum(nil))[:16]

	destPath := filepath.Join(m.downloadDir, cleanFolderName)
	if err := os.MkdirAll(destPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create destination folder %s: %w", destPath, err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	task := &FolderTask{
		ID:         taskID,
		Name:       name,
		FolderName: cleanFolderName,
		DestPath:   destPath,
		State:      "downloading",
		AddedAt:    time.Now().Unix(),
		Files:      make([]*FolderFileItem, 0, len(items)),
		cancel:     cancel,
		ctx:        ctx,
		eng:        m.engine,
	}

	// Expand items (handling Archive.org albums/collections or direct files)
	for _, item := range items {
		rawURL := strings.TrimSpace(item.URL)
		if rawURL == "" {
			continue
		}

		// Check if item is an Archive.org album/item
		if strings.Contains(rawURL, "archive.org/download/") || strings.Contains(rawURL, "archive.org/details/") || strings.HasPrefix(rawURL, "ia:") {
			id := extractArchiveOrgIdentifier(rawURL)
			albumName := item.Album
			if albumName == "" {
				albumName = item.Title
			}
			if albumName == "" {
				albumName = id
			}
			albumDir := sanitizePathSegment(albumName)

			inspCtx, inspCancel := context.WithTimeout(ctx, 8*time.Second)
			insp, err := inspectArchiveOrg(inspCtx, rawURL)
			inspCancel()

			if err == nil && insp != nil && len(insp.Files) > 0 {
				for _, f := range insp.Files {
					lower := strings.ToLower(f.Path)
					if strings.HasSuffix(lower, "_meta.xml") || strings.HasSuffix(lower, "_files.xml") ||
						strings.HasSuffix(lower, "_meta.sqlite") || strings.HasSuffix(lower, "_archive.torrent") ||
						strings.HasSuffix(lower, ".torrent") {
						continue
					}
					// Queue media / document file
					fURL := fmt.Sprintf("https://archive.org/download/%s/%s", id, url.PathEscape(f.Path))
					relPath := filepath.Join(albumDir, sanitizeRelativePath(f.Path))
					task.Files = append(task.Files, &FolderFileItem{
						URL:        fURL,
						Path:       relPath,
						Name:       filepath.Base(f.Path),
						TotalBytes: f.Length,
						State:      "pending",
					})
					task.TotalBytes += f.Length
				}
				continue
			}
		}

		// Standard item (direct audio/media file or document)
		var relPath string
		if item.Path != "" {
			relPath = sanitizeRelativePath(item.Path)
		} else if item.Album != "" {
			relPath = filepath.Join(sanitizePathSegment(item.Album), sanitizePathSegment(item.Title))
		} else {
			relPath = sanitizePathSegment(item.Title)
		}

		// Ensure extension is preserved for Soulseek downloads if not present
		if (strings.HasPrefix(rawURL, "slsk://") || strings.HasPrefix(rawURL, "soulseek://")) && filepath.Ext(relPath) == "" {
			if u, err := url.Parse(rawURL); err == nil {
				rf := u.Query().Get("file")
				ext := filepath.Ext(rf)
				if ext != "" {
					relPath += ext
				}
			}
		}

		fileName := filepath.Base(relPath)
		if fileName == "" || fileName == "." {
			fileName = item.Title
		}

		task.Files = append(task.Files, &FolderFileItem{
			URL:        rawURL,
			Path:       relPath,
			Name:       fileName,
			TotalBytes: item.Size,
			State:      "pending",
			Status:     "Pending",
		})
		task.TotalBytes += item.Size
	}

	m.mu.Lock()
	m.tasks[taskID] = task
	m.mu.Unlock()

	// Launch background downloader
	go task.runDownload(m)

	return task, nil
}

func (t *FolderTask) runDownload(m *FolderManager) {
	t.workersWg.Add(1)
	defer t.workersWg.Done()

	// Concurrency limited worker pool (3 concurrent downloads)
	semaphore := make(chan struct{}, 3)
	var wg sync.WaitGroup

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	stopRateTicker := make(chan struct{})
	defer close(stopRateTicker)

	go func() {
		var lastCompleted int64
		lastTime := time.Now()
		for {
			select {
			case <-stopRateTicker:
				return
			case <-ticker.C:
				t.mu.Lock()
				now := time.Now()
				elapsed := now.Sub(lastTime).Seconds()
				if elapsed >= 0.5 {
					currentCompleted := t.CompletedBytes
					delta := currentCompleted - lastCompleted
					if delta < 0 {
						delta = 0
					}
					rate := int64(float64(delta) / elapsed)
					t.DownloadRate = rate
					lastCompleted = currentCompleted
					lastTime = now

					if t.TotalBytes > 0 {
						t.Progress = (float64(t.CompletedBytes) / float64(t.TotalBytes)) * 100.0
						if rate > 0 && t.TotalBytes > t.CompletedBytes {
							t.ETASeconds = (t.TotalBytes - t.CompletedBytes) / rate
						} else {
							t.ETASeconds = 0
						}
					}
				}
				t.mu.Unlock()
			}
		}
	}()

	for _, fileItem := range t.Files {
		if t.ctx.Err() != nil {
			break
		}

		for t.isPaused.Load() {
			time.Sleep(500 * time.Millisecond)
			if t.ctx.Err() != nil {
				break
			}
		}

		select {
		case <-t.ctx.Done():
			return
		case semaphore <- struct{}{}:
		}

		wg.Add(1)
		go func(item *FolderFileItem) {
			defer func() {
				<-semaphore
				wg.Done()
			}()

			t.downloadFileItem(m, item)
		}(fileItem)
	}

	wg.Wait()

	if t.ctx.Err() != nil {
		return
	}

	// Final verification of total downloaded bytes
	var finalCompleted int64
	for _, f := range t.Files {
		finalCompleted += f.CompletedBytes
	}

	t.mu.Lock()
	t.CompletedBytes = finalCompleted
	if t.TotalBytes == 0 || t.CompletedBytes > t.TotalBytes {
		t.TotalBytes = finalCompleted
	}
	t.Progress = 100.0
	t.DownloadRate = 0
	t.ETASeconds = 0
	t.State = "creating_swarm"
	t.mu.Unlock()

	// AUTOMATED POST-DOWNLOAD: Add it as a magnet/torrent swarm afterwards!
	if t.eng != nil {
		comment := fmt.Sprintf("Digwire Folder Swarm: %s", t.Name)
		infoHash, magnetURI, err := t.eng.CreateTorrent(t.DestPath, comment)
		if err == nil && infoHash != "" {
			t.mu.Lock()
			t.InfoHash = infoHash
			t.MagnetURI = magnetURI
			if t.eng.IsGermanyMode() {
				t.State = "completed"
			} else {
				t.State = "seeding"
			}
			t.mu.Unlock()
		} else {
			t.mu.Lock()
			t.State = "completed"
			t.mu.Unlock()
		}
	} else {
		t.mu.Lock()
		t.State = "completed"
		t.mu.Unlock()
	}
}

func (t *FolderTask) downloadFileItem(m *FolderManager, item *FolderFileItem) {
	targetPath := filepath.Join(t.DestPath, item.Path)
	targetDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		item.State = "failed"
		item.Error = err.Error()
		return
	}

	// Check if already completed
	if fi, err := os.Stat(targetPath); err == nil && !fi.IsDir() {
		if item.TotalBytes > 0 && fi.Size() == item.TotalBytes {
			t.mu.Lock()
			t.CompletedBytes += (fi.Size() - item.CompletedBytes)
			t.mu.Unlock()
			item.CompletedBytes = fi.Size()
			item.State = "completed"
			return
		}
	}

	partPath := targetPath + ".part"
	var existingBytes int64
	if fi, err := os.Stat(partPath); err == nil && !fi.IsDir() {
		existingBytes = fi.Size()
		t.mu.Lock()
		t.CompletedBytes += (existingBytes - item.CompletedBytes)
		t.mu.Unlock()
		item.CompletedBytes = existingBytes
	}

	// Check if this is a Soulseek P2P file transfer
	if strings.HasPrefix(item.URL, "slsk://") || strings.HasPrefix(item.URL, "soulseek://") {
		t.mu.Lock()
		item.State = "downloading"
		item.Status = "Connecting to peer..."
		t.ActiveFile = item.Name
		t.StatusMessage = fmt.Sprintf("Downloading %s", item.Name)
		t.mu.Unlock()

		var lastReported int64 = existingBytes
		err := search.DownloadSoulseekFile(t.ctx, item.URL, targetPath, func(completed, total int64, statusText string) {
			t.mu.Lock()
			if statusText != "" {
				item.Status = statusText
			}
			if total > 0 && item.TotalBytes <= 0 {
				item.TotalBytes = total
				t.TotalBytes += total
			}
			delta := completed - lastReported
			if delta > 0 {
				t.CompletedBytes += delta
				lastReported = completed
				item.CompletedBytes = completed
				if item.TotalBytes > 0 {
					item.Status = fmt.Sprintf("%.0f%%", (float64(item.CompletedBytes)/float64(item.TotalBytes))*100)
				}
			}
			t.mu.Unlock()
		})

		t.mu.Lock()
		if err != nil {
			item.State = "failed"
			item.Error = err.Error()
			item.Status = "Failed: " + err.Error()
			t.mu.Unlock()
			return
		}
		item.State = "completed"
		item.Status = "Completed"
		if fi, sErr := os.Stat(targetPath); sErr == nil {
			item.CompletedBytes = fi.Size()
			if item.TotalBytes <= 0 {
				item.TotalBytes = fi.Size()
			}
		}
		t.mu.Unlock()
		return
	}

	req, err := http.NewRequestWithContext(t.ctx, http.MethodGet, item.URL, nil)
	if err != nil {
		t.mu.Lock()
		item.State = "failed"
		item.Error = err.Error()
		item.Status = "Failed"
		t.mu.Unlock()
		return
	}
	req.Header.Set("User-Agent", "Digwire/0.3.3 (Unified Folder Downloader)")

	if existingBytes > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", existingBytes))
	}

	t.mu.Lock()
	item.State = "downloading"
	item.Status = "Connecting..."
	t.ActiveFile = item.Name
	t.StatusMessage = fmt.Sprintf("Downloading %s", item.Name)
	t.mu.Unlock()

	resp, err := m.client.Do(req)
	if err != nil {
		t.mu.Lock()
		item.State = "failed"
		item.Error = err.Error()
		item.Status = "Failed"
		t.mu.Unlock()
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		t.mu.Lock()
		item.State = "failed"
		item.Error = fmt.Sprintf("HTTP error: %s", resp.Status)
		item.Status = "Failed"
		t.mu.Unlock()
		return
	}

	if item.TotalBytes <= 0 {
		if resp.ContentLength > 0 {
			item.TotalBytes = existingBytes + resp.ContentLength
			t.mu.Lock()
			t.TotalBytes += resp.ContentLength
			t.mu.Unlock()
		}
	}

	outFlags := os.O_CREATE | os.O_WRONLY
	if existingBytes > 0 && resp.StatusCode == http.StatusPartialContent {
		outFlags |= os.O_APPEND
	} else {
		outFlags |= os.O_TRUNC
		if existingBytes > 0 {
			t.mu.Lock()
			t.CompletedBytes -= existingBytes
			t.mu.Unlock()
			item.CompletedBytes = 0
			existingBytes = 0
		}
	}

	outFile, err := os.OpenFile(partPath, outFlags, 0644)
	if err != nil {
		t.mu.Lock()
		item.State = "failed"
		item.Error = err.Error()
		item.Status = "Failed"
		t.mu.Unlock()
		return
	}
	defer outFile.Close()

	buf := make([]byte, 64*1024)
	for {
		if t.ctx.Err() != nil {
			return
		}
		for t.isPaused.Load() {
			time.Sleep(400 * time.Millisecond)
			if t.ctx.Err() != nil {
				return
			}
		}

		n, rErr := resp.Body.Read(buf)
		if n > 0 {
			wN, wErr := outFile.Write(buf[:n])
			if wErr != nil {
				t.mu.Lock()
				item.State = "failed"
				item.Error = wErr.Error()
				item.Status = "Failed"
				t.mu.Unlock()
				return
			}
			t.mu.Lock()
			t.CompletedBytes += int64(wN)
			item.CompletedBytes += int64(wN)
			if item.TotalBytes > 0 {
				item.Status = fmt.Sprintf("%.0f%%", (float64(item.CompletedBytes)/float64(item.TotalBytes))*100)
			}
			t.mu.Unlock()
		}
		if rErr != nil {
			if rErr == io.EOF {
				break
			}
			t.mu.Lock()
			item.State = "failed"
			item.Error = rErr.Error()
			item.Status = "Failed"
			t.mu.Unlock()
			return
		}
	}

	_ = outFile.Sync()
	_ = outFile.Close()

	// Atomic rename to final path
	if err := os.Rename(partPath, targetPath); err != nil {
		t.mu.Lock()
		item.State = "failed"
		item.Error = err.Error()
		item.Status = "Failed"
		t.mu.Unlock()
		return
	}

	t.mu.Lock()
	item.State = "completed"
	item.Status = "Completed"
	t.mu.Unlock()
}

// Pause pauses the folder download
func (m *FolderManager) Pause(id string) error {
	m.mu.RLock()
	task, ok := m.tasks[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}

	task.isPaused.Store(true)
	task.mu.Lock()
	if task.State == "downloading" {
		task.State = "paused"
	}
	task.DownloadRate = 0
	task.mu.Unlock()
	return nil
}

// Resume resumes a paused folder download
func (m *FolderManager) Resume(id string) error {
	m.mu.RLock()
	task, ok := m.tasks[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}

	task.isPaused.Store(false)
	task.mu.Lock()
	if task.State == "paused" {
		task.State = "downloading"
	}
	task.mu.Unlock()
	return nil
}

// Remove cancels and removes a folder task
func (m *FolderManager) Remove(id string, deleteFiles bool) error {
	m.mu.Lock()
	task, ok := m.tasks[id]
	if !ok {
		// Check by InfoHash
		for k, t := range m.tasks {
			if strings.EqualFold(t.InfoHash, id) {
				task = t
				id = k
				ok = true
				break
			}
		}
	}
	if ok {
		delete(m.tasks, id)
	}
	m.mu.Unlock()

	if !ok || task == nil {
		return fmt.Errorf("task not found: %s", id)
	}

	if task.cancel != nil {
		task.cancel()
	}

	if deleteFiles && task.DestPath != "" && task.DestPath != "/" && task.DestPath != m.downloadDir {
		_ = os.RemoveAll(task.DestPath)
	}

	return nil
}

// GetTask returns a task by ID or InfoHash
func (m *FolderManager) GetTask(idOrHash string) *FolderTask {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if t, ok := m.tasks[idOrHash]; ok {
		return t
	}
	for _, t := range m.tasks {
		if strings.EqualFold(t.InfoHash, idOrHash) || strings.EqualFold(t.ID, idOrHash) {
			return t
		}
	}
	return nil
}

// GetTasks returns all folder tasks
func (m *FolderManager) GetTasks() []*FolderTask {
	m.mu.RLock()
	defer m.mu.RUnlock()

	res := make([]*FolderTask, 0, len(m.tasks))
	for _, t := range m.tasks {
		res = append(res, t)
	}
	return res
}
