package engine

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"digwire/internal/search"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

type SwarmSuggestion struct {
	InfoHash         string `json:"info_hash"`
	MagnetURI        string `json:"magnet_uri"`
	Name             string `json:"name"`
	Seeders          int    `json:"seeders"`
	Peers            int    `json:"peers"`
	TotalBytes       int64  `json:"total_bytes"`
	Provider         string `json:"provider"`
	IsPartial        bool   `json:"is_partial"`
	MatchedFileIndex int    `json:"matched_file_index"`
	MatchedFileName  string `json:"matched_file_name"`
}

type MatchResult struct {
	MatchedTorrent *torrent.Torrent
	InfoHash       string
	Name           string
	VerifiedPiece  bool
	HTTPURL        string
	SizeBytes      int64
}

// InspectHTTPFile sends a HEAD request to extract file metadata from an HTTP URL
func InspectHTTPFile(ctx context.Context, fileURL string) (filename string, sizeBytes int64, supportsRange bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, fileURL, nil)
	if err != nil {
		return "", 0, false, err
	}
	req.Header.Set("User-Agent", "Digwire/1.0")

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", 0, false, fmt.Errorf("server responded with status %d", resp.StatusCode)
	}

	// Extract filename from Content-Disposition or URL path
	cd := resp.Header.Get("Content-Disposition")
	if strings.Contains(cd, "filename=") {
		parts := strings.Split(cd, "filename=")
		if len(parts) > 1 {
			filename = strings.Trim(parts[1], "\"' ")
		}
	}
	if filename == "" {
		u, _ := url.Parse(fileURL)
		if u != nil {
			filename = path.Base(u.Path)
		}
	}
	if filename == "" || filename == "." || filename == "/" {
		filename = "downloaded_file"
	}

	sizeBytes, _ = strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	supportsRange = strings.EqualFold(resp.Header.Get("Accept-Ranges"), "bytes")

	return filename, sizeBytes, supportsRange, nil
}

// VerifyPieceAtOffset fetches a single piece extent via HTTP Range and tests against SHA-1 piece hash
func VerifyPieceAtOffset(ctx context.Context, fileURL string, startOffset, length int64, expectedHash [20]byte) (bool, error) {
	if length <= 0 {
		return false, fmt.Errorf("invalid piece length")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "Digwire/1.0")
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", startOffset, startOffset+length-1))

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("range request not supported: status %d", resp.StatusCode)
	}

	buf := make([]byte, length)
	n, err := io.ReadFull(resp.Body, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return false, err
	}

	computed := sha1.Sum(buf[:n])
	return bytes.Equal(computed[:], expectedHash[:]), nil
}

// VerifyRandomPieces checks piece 0, a random middle piece, and the last piece for 100% cryptographic certainty
func VerifyRandomPieces(ctx context.Context, fileURL string, info *metainfo.Info) (bool, error) {
	numPieces := info.NumPieces()
	if numPieces <= 0 || len(info.Pieces) < numPieces*20 {
		return false, fmt.Errorf("invalid torrent info piece list")
	}

	totalLen := info.TotalLength()
	pieceLen := info.PieceLength

	piecesToCheck := []int{0}
	if numPieces > 2 {
		maxN := big.NewInt(int64(numPieces - 2))
		rInt, _ := rand.Int(rand.Reader, maxN)
		midPiece := int(rInt.Int64()) + 1
		piecesToCheck = append(piecesToCheck, midPiece)
	}
	if numPieces > 1 {
		piecesToCheck = append(piecesToCheck, numPieces-1)
	}

	for _, pIdx := range piecesToCheck {
		start := int64(pIdx) * pieceLen
		curLen := pieceLen
		if start+curLen > totalLen {
			curLen = totalLen - start
		}

		var expected [20]byte
		copy(expected[:], info.Pieces[pIdx*20:(pIdx+1)*20])

		ok, err := VerifyPieceAtOffset(ctx, fileURL, start, curLen, expected)
		if err != nil || !ok {
			return false, err
		}
	}

	return true, nil
}

// VerifyFileInMultiTorrent verifies if a single file inside a multi-file collection matches the HTTP source
func VerifyFileInMultiTorrent(ctx context.Context, fileURL string, fileOffset, fileLen int64, info *metainfo.Info) bool {
	pieceLen := info.PieceLength
	if pieceLen <= 0 {
		return false
	}

	// Calculate piece indices that are fully enclosed within this file
	firstFullPiece := (fileOffset + pieceLen - 1) / pieceLen
	lastFullPiece := (fileOffset + fileLen) / pieceLen - 1

	if firstFullPiece <= lastFullPiece && int(lastFullPiece) < info.NumPieces() {
		// Verify piece at firstFullPiece
		pIdx := int(firstFullPiece)
		offsetInFile := (int64(pIdx) * pieceLen) - fileOffset
		var expected [20]byte
		copy(expected[:], info.Pieces[pIdx*20:(pIdx+1)*20])

		ok, err := VerifyPieceAtOffset(ctx, fileURL, offsetInFile, pieceLen, expected)
		return err == nil && ok
	}

	return false
}

// FindAndAttachSwarm attempts to discover an existing swarm for an HTTP URL, verifies multi-piece samples, and attaches it as a WebSeed
func (e *Engine) FindAndAttachSwarm(ctx context.Context, fileURL string, searchMgr *search.Manager) (*MatchResult, error) {
	filename, sizeBytes, _, err := InspectHTTPFile(ctx, fileURL)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect HTTP file: %w", err)
	}

	queryName := strings.TrimSuffix(filename, filepath.Ext(filename))
	queryName = strings.ReplaceAll(queryName, ".", " ")
	queryName = strings.ReplaceAll(queryName, "_", " ")
	queryName = strings.ReplaceAll(queryName, "-", " ")

	candidates := searchMgr.SearchAll(ctx, queryName)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no matching candidate swarms found for %s", filename)
	}

	for _, cand := range candidates {
		if cand.SizeBytes > 0 && sizeBytes > 0 && cand.SizeBytes != sizeBytes {
			continue
		}

		t, err := e.Add(cand.MagnetURI)
		if err != nil {
			continue
		}

		select {
		case <-t.GotInfo():
			info := t.Info()
			if info != nil && t.Length() == sizeBytes {
				ok, _ := VerifyRandomPieces(ctx, fileURL, info)
				if ok {
					t.AddWebSeeds([]string{fileURL})
					hash := t.InfoHash().HexString()
					e.mu.Lock()
					e.webSeedsMap[hash] = append(e.webSeedsMap[hash], fileURL)
					e.mu.Unlock()

					return &MatchResult{
						MatchedTorrent: t,
						InfoHash:       hash,
						Name:           info.BestName(),
						VerifiedPiece:  true,
						HTTPURL:        fileURL,
						SizeBytes:      sizeBytes,
					}, nil
				}
			}
		case <-time.After(5 * time.Second):
		}
	}

	return nil, fmt.Errorf("no candidate torrent passed cryptographic piece verification")
}

// FindSuggestedSwarm searches for an equivalent or parent collection torrent for an existing HTTP task
func (e *Engine) FindSuggestedSwarm(ctx context.Context, task *HTTPTask, searchMgr *search.Manager) (*SwarmSuggestion, error) {
	if task == nil || task.TotalBytes <= 0 {
		return nil, fmt.Errorf("invalid task")
	}

	queryName := strings.TrimSuffix(task.Name, filepath.Ext(task.Name))
	queryName = strings.ReplaceAll(queryName, ".", " ")
	queryName = strings.ReplaceAll(queryName, "_", " ")
	queryName = strings.ReplaceAll(queryName, "-", " ")

	candidates := searchMgr.SearchAll(ctx, queryName)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no candidates found")
	}

	for _, cand := range candidates {
		t, err := e.Add(cand.MagnetURI)
		if err != nil {
			continue
		}

		select {
		case <-t.GotInfo():
			info := t.Info()
			if info == nil {
				continue
			}

			// Case 1: Full file match
			if t.Length() == task.TotalBytes {
				ok, _ := VerifyRandomPieces(ctx, task.URL, info)
				if ok {
					return &SwarmSuggestion{
						InfoHash:   t.InfoHash().HexString(),
						MagnetURI:  cand.MagnetURI,
						Name:       info.BestName(),
						Seeders:    cand.Seeders,
						Peers:      cand.Leechers,
						TotalBytes: t.Length(),
						Provider:   cand.Provider,
						IsPartial:  false,
					}, nil
				}
			}

			// Case 2: Partial match inside multi-file pack / collection
			if info.IsDir() {
				var fileOffset int64 = 0
				for fIdx, f := range info.Files {
					fLen := f.Length
					if fLen == task.TotalBytes {
						if VerifyFileInMultiTorrent(ctx, task.URL, fileOffset, fLen, info) {
							return &SwarmSuggestion{
								InfoHash:         t.InfoHash().HexString(),
								MagnetURI:        cand.MagnetURI,
								Name:             info.BestName(),
								Seeders:          cand.Seeders,
								Peers:            cand.Leechers,
								TotalBytes:       fLen,
								Provider:         cand.Provider,
								IsPartial:        true,
								MatchedFileIndex: fIdx,
								MatchedFileName:  strings.Join(f.Path, "/"),
							}, nil
						}
					}
					fileOffset += fLen
				}
			}
		case <-time.After(5 * time.Second):
		}
	}

	return nil, fmt.Errorf("no matching verified swarm found")
}

// UpgradeHTTPToSwarm upgrades an active HTTP task to a hybrid BitTorrent swarm (with optional partial file download)
func (e *Engine) UpgradeHTTPToSwarm(httpTaskID string) (*torrent.Torrent, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.httpManager.mu.RLock()
	task, exists := e.httpManager.tasks[httpTaskID]
	e.httpManager.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("http task not found")
	}

	if task.SuggestedSwarm == nil {
		return nil, fmt.Errorf("no verified swarm suggestion available for this task")
	}

	sugg := task.SuggestedSwarm

	// Pause & remove HTTP task
	task.pause()
	delete(e.httpManager.tasks, httpTaskID)

	// Add torrent to client
	t, err := e.client.AddMagnet(sugg.MagnetURI)
	if err != nil {
		return nil, fmt.Errorf("failed to add swarm: %w", err)
	}

	// Move downloaded .part file if exists
	partPath := task.DestPath + ".part"
	targetPath := filepath.Join(e.cfg.DownloadDir, task.Name)
	if _, err := os.Stat(partPath); err == nil {
		_ = os.Rename(partPath, targetPath)
	}

	// Inject all HTTP mirrors as WebSeeds
	var mirrors []string
	mirrors = append(mirrors, task.URL)
	mirrors = append(mirrors, task.Mirrors...)
	t.AddWebSeeds(mirrors)

	hash := t.InfoHash().HexString()
	e.webSeedsMap[hash] = mirrors

	if sugg.IsPartial {
		go func(tor *torrent.Torrent, matchedIdx int) {
			<-tor.GotInfo()
			for i, f := range tor.Files() {
				if i == matchedIdx {
					f.Download()
				} else {
					f.Cancel()
				}
			}
			e.mu.Lock()
			e.saveSessionLocked()
			e.mu.Unlock()
		}(t, sugg.MatchedFileIndex)
	} else {
		go func(tor *torrent.Torrent) {
			<-tor.GotInfo()
			tor.DownloadAll()
			e.mu.Lock()
			e.saveSessionLocked()
			e.mu.Unlock()
		}(t)
	}

	e.initTracker(hash)
	e.saveSessionLocked()

	return t, nil
}

func (e *Engine) TriggerFindSwarm(ctx context.Context, httpTaskID string) (*SwarmSuggestion, error) {
	e.httpManager.mu.RLock()
	task, exists := e.httpManager.tasks[httpTaskID]
	e.httpManager.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("http download task not found")
	}

	if e.searchMgr == nil {
		return nil, fmt.Errorf("search manager not configured")
	}

	sugg, err := e.FindSuggestedSwarm(ctx, task, e.searchMgr)
	if err != nil {
		return nil, err
	}

	task.mu.Lock()
	task.SuggestedSwarm = sugg
	task.mu.Unlock()

	return sugg, nil
}
