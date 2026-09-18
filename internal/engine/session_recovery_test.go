package engine

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"digwire/internal/config"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

// A lost session must not turn Digwire's metadata cache into the user's torrent list: those
// torrents are offered as found instead, with what of them is on disk.
func TestLostSessionOffersCachedTorrentsInsteadOfStartingThem(t *testing.T) {
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

	eng, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer eng.Close()

	eng.WaitForSession(5 * time.Second)

	if torrents := eng.GetTorrents(); len(torrents) != 0 {
		t.Fatalf("cached metadata was started as %d torrent(s) without being asked", len(torrents))
	}

	var offered *FoundTorrent
	for _, f := range eng.FoundTorrents() {
		if strings.EqualFold(f.InfoHash, hash) {
			entry := f
			offered = &entry
		}
	}
	if offered == nil {
		t.Fatalf("cached torrent %s is not offered as found", hash)
	}
	if offered.Status != "complete" {
		t.Fatalf("found torrent status %q, want complete: its file is on disk", offered.Status)
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

// A download the session remembers as finished is assumed complete at startup, then checked:
// intact data keeps seeding, while missing files or corrupt data go back to downloading.
func TestSessionSeedingClaimIsVerified(t *testing.T) {
	content := bytes.Repeat([]byte("digwire!"), 8192)
	corrupt := append([]byte(nil), content...)
	corrupt[20000] ^= 0xff
	for _, tc := range []struct {
		name        string
		onDisk      []byte // nil: file missing
		wantSeeding bool
	}{
		{"intact data keeps seeding", content, true},
		{"missing files download again", nil, false},
		{"corrupt data downloads again", corrupt, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempDir, "xdg"))
			cfg := &config.Config{DownloadDir: filepath.Join(tempDir, "downloads")}
			cfg.SetConfigPath(filepath.Join(tempDir, "config.yaml"))

			path := filepath.Join(cfg.DownloadDir, "movie.mkv")
			if err := os.MkdirAll(cfg.DownloadDir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, content, 0644); err != nil {
				t.Fatal(err)
			}
			info := metainfo.Info{PieceLength: 16384}
			if err := info.BuildFromFilePath(path); err != nil {
				t.Fatal(err)
			}
			if tc.onDisk == nil {
				_ = os.Remove(path)
			} else if err := os.WriteFile(path, tc.onDisk, 0644); err != nil {
				t.Fatal(err)
			}
			infoBytes, err := bencode.Marshal(info)
			if err != nil {
				t.Fatal(err)
			}
			mi := metainfo.MetaInfo{InfoBytes: infoBytes}
			hash := strings.ToLower(mi.HashInfoBytes().HexString())

			cacheDir := filepath.Join(tempDir, "torrents")
			if err := os.MkdirAll(cacheDir, 0755); err != nil {
				t.Fatal(err)
			}
			f, err := os.Create(filepath.Join(cacheDir, hash+".torrent"))
			if err != nil {
				t.Fatal(err)
			}
			_ = mi.Write(f)
			f.Close()

			session, err := json.Marshal(SessionState{Torrents: []SavedTorrent{{
				InfoHash:       hash,
				MagnetURI:      "magnet:?xt=urn:btih:" + hash,
				Name:           info.Name,
				IsSeeding:      true,
				TotalBytes:     info.TotalLength(),
				CompletedBytes: info.TotalLength(),
			}}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(tempDir, "session.json"), session, 0644); err != nil {
				t.Fatal(err)
			}

			eng, err := NewEngine(cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer eng.Close()
			eng.WaitForSession(5 * time.Second)

			status := func() TorrentStatus {
				for _, st := range eng.GetTorrents() {
					if strings.EqualFold(st.InfoHash, hash) {
						return st
					}
				}
				t.Fatal("torrent not loaded from session")
				return TorrentStatus{}
			}
			if tc.onDisk != nil {
				if st := status(); st.Progress < 100 {
					t.Fatalf("assumed-complete torrent started at %.1f%% instead of 100%%", st.Progress)
				}
			}
			deadline := time.Now().Add(20 * time.Second)
			for {
				eng.mu.RLock()
				tr := eng.rateMap[hash]
				settled := tr != nil && !tr.verifyPending.Load() && !tr.isVerifying.Load()
				eng.mu.RUnlock()
				if settled {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("verification did not settle")
				}
				time.Sleep(50 * time.Millisecond)
			}
			// Let the once-a-second monitor loop act on the verified state.
			time.Sleep(1500 * time.Millisecond)

			st := status()
			if seeding := st.State == "seeding" || st.State == "completed"; seeding != tc.wantSeeding {
				t.Fatalf("state %q with %d of %d bytes, want seeding=%v", st.State, st.CompletedBytes, st.TotalBytes, tc.wantSeeding)
			}
		})
	}
}
