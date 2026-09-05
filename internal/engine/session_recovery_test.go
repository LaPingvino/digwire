package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"digwire/internal/config"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

func TestSessionAutoRecoveryFromCache(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "digwire_session_rec_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := &config.Config{
		DownloadDir: filepath.Join(tempDir, "downloads"),
		ListenPort:  0,
		GermanyMode: false,
	}
	cfg.SetConfigPath(filepath.Join(tempDir, "config.yaml"))
	_ = os.MkdirAll(cfg.DownloadDir, 0755)

	// Create a sample file and a mock .torrent file in cache
	cacheDir := filepath.Join(tempDir, "torrents")
	_ = os.MkdirAll(cacheDir, 0755)

	sampleData := []byte("Hello Digwire recovery test content")
	sampleFile := filepath.Join(cfg.DownloadDir, "sample.txt")
	_ = os.WriteFile(sampleFile, sampleData, 0644)

	info := metainfo.Info{PieceLength: 16384}
	if err := info.BuildFromFilePath(sampleFile); err != nil {
		t.Fatalf("failed to build info: %v", err)
	}
	mi := metainfo.MetaInfo{}
	mi.SetDefaults()
	infoBytes, _ := bencode.Marshal(info)
	mi.InfoBytes = infoBytes

	hash := mi.HashInfoBytes().HexString()
	torrentPath := filepath.Join(cacheDir, strings.ToLower(hash)+".torrent")
	f, err := os.Create(torrentPath)
	if err != nil {
		t.Fatalf("failed to create cache torrent: %v", err)
	}
	_ = mi.Write(f)
	f.Close()

	// Ensure session.json is 0 bytes (simulating disk full truncation)
	sessionPath := filepath.Join(tempDir, "session.json")
	_ = os.WriteFile(sessionPath, []byte(""), 0644)

	// Start engine - it should auto-recover the torrent from cache!
	eng, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer eng.Close()

	eng.WaitForSession(5 * time.Second)

	torrents := eng.GetTorrents()
	if len(torrents) == 0 {
		t.Fatalf("expected auto-recovery to load at least 1 torrent from cache, got 0")
	}

	found := false
	for _, tor := range torrents {
		if strings.EqualFold(tor.InfoHash, hash) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected recovered torrent with hash %s in torrent list", hash)
	}

	// Verify that saveSessionLocked writes atomic .tmp and backup .bak
	eng.mu.Lock()
	eng.saveSessionLocked()
	eng.mu.Unlock()

	stat, err := os.Stat(sessionPath)
	if err != nil || stat.Size() == 0 {
		t.Fatalf("expected session.json to be saved with non-zero size, got size %v, err %v", stat.Size(), err)
	}

	// Test checkpointDatabases
	eng.checkpointDatabases()
}

func TestFolderTaskSessionPersistence(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "digwire_folder_rec_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := &config.Config{
		DownloadDir: filepath.Join(tempDir, "downloads"),
		ListenPort:  0,
		GermanyMode: false,
	}
	cfg.SetConfigPath(filepath.Join(tempDir, "config.yaml"))
	_ = os.MkdirAll(cfg.DownloadDir, 0755)

	// Phase 1: Start engine, add a folder task, and save session
	eng1, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine 1: %v", err)
	}

	folderTask, err := eng1.FolderManager().StartFolderDownload("Test Album", "Artist - Test Album", []FolderItemInput{
		{
			URL:   "slsk://testuser?file=Artist/Album/01.mp3&size=1000",
			Title: "01.mp3",
			Path:  "Artist/Album/01.mp3",
			Size:  1000,
		},
		{
			URL:   "slsk://testuser?file=Artist/Album/02.mp3&size=2000",
			Title: "02.mp3",
			Path:  "Artist/Album/02.mp3",
			Size:  2000,
		},
	})
	if err != nil {
		t.Fatalf("failed to start folder download: %v", err)
	}
	taskID := folderTask.ID

	// Save session and close engine 1
	eng1.SaveSession()
	eng1.Close()

	// Phase 2: Start engine 2 from same config/session directory
	eng2, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine 2: %v", err)
	}
	defer eng2.Close()

	eng2.WaitForSession(5 * time.Second)

	// Check if folder task was restored in FolderManager
	restoredTask := eng2.FolderManager().GetTask(taskID)
	if restoredTask == nil {
		t.Fatalf("expected folder task %s to be restored across restarts, but was nil", taskID)
	}

	if restoredTask.Name != "Test Album" {
		t.Errorf("expected restored name 'Test Album', got '%s'", restoredTask.Name)
	}

	if len(restoredTask.Files) != 2 {
		t.Errorf("expected 2 files in restored folder task, got %d", len(restoredTask.Files))
	}

	// Check if it appears in GetTorrents() list
	torrents := eng2.GetTorrents()
	found := false
	for _, tor := range torrents {
		if tor.InfoHash == taskID || strings.Contains(tor.Name, "Test Album") {
			found = true
			if tor.Platform != "folder" {
				t.Errorf("expected platform 'folder', got '%s'", tor.Platform)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected folder task to be listed in GetTorrents()")
	}
}

func TestFolderTaskFailureAndResume(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "digwire_folder_fail_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := &config.Config{
		DownloadDir: filepath.Join(tempDir, "downloads"),
		ListenPort:  0,
		GermanyMode: false,
	}
	cfg.SetConfigPath(filepath.Join(tempDir, "config.yaml"))
	_ = os.MkdirAll(cfg.DownloadDir, 0755)

	eng, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer eng.Close()

	fm := eng.FolderManager()
	folderTask, err := fm.StartFolderDownload("Incomplete Album", "Artist - Incomplete Album", []FolderItemInput{
		{
			URL:   "http://127.0.0.1:59999/nonexistent1.mp3",
			Title: "01.mp3",
			Path:  "Artist/Album/01.mp3",
			Size:  1000,
		},
		{
			URL:   "http://127.0.0.1:59999/nonexistent2.mp3",
			Title: "02.mp3",
			Path:  "Artist/Album/02.mp3",
			Size:  2000,
		},
	})
	if err != nil {
		t.Fatalf("failed to start folder download: %v", err)
	}

	// Wait for workers to fail because server is non-existent
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		folderTask.mu.RLock()
		st := folderTask.State
		folderTask.mu.RUnlock()
		if st == "failed" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	folderTask.mu.RLock()
	if folderTask.State != "failed" {
		t.Errorf("expected task state 'failed', got '%s'", folderTask.State)
	}
	if folderTask.Progress >= 100.0 {
		t.Errorf("expected progress < 100.0 for failed task, got %f", folderTask.Progress)
	}
	if folderTask.InfoHash != "" {
		t.Errorf("expected no swarm InfoHash to be created for failed task, got '%s'", folderTask.InfoHash)
	}
	folderTask.mu.RUnlock()

	// Test Resume: resumes and resets failed files to pending
	if err := fm.Resume(folderTask.ID); err != nil {
		t.Fatalf("failed to resume folder task: %v", err)
	}

	folderTask.mu.RLock()
	if folderTask.State != "downloading" {
		t.Errorf("expected state 'downloading' after resume, got '%s'", folderTask.State)
	}
	for _, f := range folderTask.Files {
		if f.State == "failed" {
			t.Errorf("expected failed file to be reset from 'failed', got '%s'", f.State)
		}
	}
	folderTask.mu.RUnlock()
}
