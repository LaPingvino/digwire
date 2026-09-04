package engine

import (
	"os"
	"path/filepath"
	"testing"

	"digwire/internal/config"
)

func TestQuickCheckAndVerification(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "digwire-verify-test-*")
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

	eng, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer eng.Close()

	// Create sample payload file
	sampleFilePath := filepath.Join(downloadDir, "sample_payload.dat")
	data := []byte("Digwire fast startup verification payload 1234567890")
	if err := os.WriteFile(sampleFilePath, data, 0644); err != nil {
		t.Fatalf("failed to write sample file: %v", err)
	}

	hash, _, err := eng.CreateTorrent(sampleFilePath, "Test torrent for quick verification")
	if err != nil {
		t.Fatalf("failed to create torrent: %v", err)
	}

	torrents := eng.GetTorrents()
	if len(torrents) == 0 {
		t.Fatalf("expected torrent to be listed in GetTorrents()")
	}

	found := false
	for _, tor := range torrents {
		if tor.InfoHash == hash {
			found = true
			if tor.TotalBytes <= 0 {
				t.Errorf("expected positive TotalBytes, got %d", tor.TotalBytes)
			}
			if tor.CompletedBytes <= 0 {
				t.Errorf("expected positive CompletedBytes, got %d", tor.CompletedBytes)
			}
			if tor.Progress < 100.0 {
				t.Errorf("expected 100%% progress for complete file, got %.2f%%", tor.Progress)
			}
			if tor.State != "seeding" && tor.State != "completed" {
				t.Errorf("expected state to be seeding or completed, got %s", tor.State)
			}
		}
	}
	if !found {
		t.Errorf("created torrent infohash %s not found in GetTorrents()", hash)
	}
}
