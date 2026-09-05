package engine

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"digwire/internal/config"
)

func TestFolderDownloadManager(t *testing.T) {
	// Mock HTTP server providing mock files
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte(strings.Repeat("X", 100)))
	}))
	defer server.Close()

	tempDir, err := os.MkdirTemp("", "digwire_folder_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := config.DefaultConfig()
	cfg.DownloadDir = tempDir

	eng := &Engine{
		cfg:              cfg,
		rateMap:          make(map[string]*rateTracker),
		webSeedsMap:      make(map[string][]string),
		savedTorrentsMap: make(map[string]SavedTorrent),
	}
	mgr := NewFolderManager(tempDir, eng)
	eng.folderManager = mgr

	items := []FolderItemInput{
		{
			URL:    server.URL + "/track1.flac",
			Title:  "01 - Intro.flac",
			Artist: "Test Artist",
			Album:  "First Album",
			Size:   100,
		},
		{
			URL:    server.URL + "/track2.flac",
			Title:  "02 - Groove.flac",
			Artist: "Test Artist",
			Album:  "Second Album",
			Size:   100,
		},
	}

	task, err := mgr.StartFolderDownload("Test Artist Collection", "Test Artist", items)
	if err != nil {
		t.Fatalf("StartFolderDownload failed: %v", err)
	}

	if task.ID == "" {
		t.Errorf("expected non-empty task ID")
	}
	if len(task.Files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(task.Files))
	}

	// Verify folder in folder paths:
	// First Album / 01 - Intro.flac
	// Second Album / 02 - Groove.flac
	expectedSubpath1 := filepath.Join("First Album", "01 - Intro.flac")
	if task.Files[0].Path != expectedSubpath1 {
		t.Errorf("expected path %q, got %q", expectedSubpath1, task.Files[0].Path)
	}

	// Wait for download to complete
	timeout := time.After(5 * time.Second)
	completed := false
	for !completed {
		select {
		case <-timeout:
			t.Fatalf("timed out waiting for folder download to finish, state: %s, completed: %d/%d", task.State, task.CompletedBytes, task.TotalBytes)
		default:
			task.mu.RLock()
			st := task.State
			task.mu.RUnlock()
			if st == "completed" || st == "seeding" {
				completed = true
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	// Verify files exist on disk in their subfolders ("folder in folder")
	file1Path := filepath.Join(task.DestPath, expectedSubpath1)
	if fi, err := os.Stat(file1Path); err != nil || fi.Size() != 100 {
		t.Errorf("expected file 1 at %s with size 100, err: %v", file1Path, err)
	}

	expectedSubpath2 := filepath.Join("Second Album", "02 - Groove.flac")
	file2Path := filepath.Join(task.DestPath, expectedSubpath2)
	if fi, err := os.Stat(file2Path); err != nil || fi.Size() != 100 {
		t.Errorf("expected file 2 at %s with size 100, err: %v", file2Path, err)
	}

	// Verify GetTorrentSavePath
	savePath, err := eng.GetTorrentSavePath(task.ID)
	if err != nil || savePath != task.DestPath {
		t.Errorf("GetTorrentSavePath failed: %v, savePath: %s", err, savePath)
	}

	// Verify GetTorrentFilePath
	filePath0, err := eng.GetTorrentFilePath(task.ID, 0)
	if err != nil || filePath0 != file1Path {
		t.Errorf("GetTorrentFilePath failed: %v, got %s", err, filePath0)
	}

	// Verify Remove
	if err := mgr.Remove(task.ID, true); err != nil {
		t.Errorf("Remove failed: %v", err)
	}
	if _, err := os.Stat(task.DestPath); !os.IsNotExist(err) {
		t.Errorf("expected destPath to be removed when deleteFiles is true")
	}
}
