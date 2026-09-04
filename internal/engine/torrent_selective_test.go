package engine

import (
	"os"
	"path/filepath"
	"testing"

	"digwire/internal/config"
)

func TestSelectiveFileDownloadAndSessionPersistence(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "digwire-selective-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	downloadDir := filepath.Join(tempDir, "downloads")
	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		t.Fatalf("failed to create download dir: %v", err)
	}

	cfg := &config.Config{
		DownloadDir: downloadDir,
		ListenPort:  0,
		GermanyMode: false,
	}
	cfg.SetConfigPath(filepath.Join(tempDir, "config.yaml"))

	eng, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer eng.Close()

	// Create multiple sample files to simulate an album folder
	albumDir := filepath.Join(downloadDir, "Sample Album")
	if err := os.MkdirAll(albumDir, 0755); err != nil {
		t.Fatalf("failed to create album dir: %v", err)
	}

	track1 := filepath.Join(albumDir, "01 - Track One.mp3")
	track2 := filepath.Join(albumDir, "02 - Track Two.mp3")
	track3 := filepath.Join(albumDir, "03 - Track Three.mp3")

	_ = os.WriteFile(track1, []byte("Track 1 payload content audio data"), 0644)
	_ = os.WriteFile(track2, []byte("Track 2 payload content audio data"), 0644)
	_ = os.WriteFile(track3, []byte("Track 3 payload content audio data"), 0644)

	hash, _, err := eng.CreateTorrent(albumDir, "Sample Album Multi-Track")
	if err != nil {
		t.Fatalf("failed to create multi-file torrent: %v", err)
	}

	// Verify SetFilePriority works
	err = eng.SetFilePriority(hash, 0, 1) // Normal
	if err != nil {
		t.Errorf("unexpected error setting file 0 priority: %v", err)
	}
	err = eng.SetFilePriority(hash, 1, 0) // Skip
	if err != nil {
		t.Errorf("unexpected error skipping file 1: %v", err)
	}
	err = eng.SetFilePriority(hash, 2, 2) // High
	if err != nil {
		t.Errorf("unexpected error setting file 2 high priority: %v", err)
	}

	details, err := eng.GetTorrentDetails(hash)
	if err != nil {
		t.Fatalf("failed to get torrent details: %v", err)
	}

	if len(details.Files) < 3 {
		t.Errorf("expected at least 3 files, got %d", len(details.Files))
	}

	// Verify file 1 is marked with priority 0 (Skip)
	if details.Files[1].Priority != 0 {
		t.Errorf("expected file 1 priority to be 0 (Skip), got %d", details.Files[1].Priority)
	}
	// Verify file 2 is marked with priority 2 (High)
	if details.Files[2].Priority != 2 {
		t.Errorf("expected file 2 priority to be 2 (High), got %d", details.Files[2].Priority)
	}
}
