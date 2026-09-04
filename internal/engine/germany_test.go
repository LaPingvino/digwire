package engine

import (
	"os"
	"path/filepath"
	"testing"

	"digwire/internal/config"
)

func TestGermanyModeConfigAndEngine(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "digwire-germany-test-*")
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

	eng, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer eng.Close()

	if eng.IsGermanyMode() {
		t.Errorf("expected GermanyMode to initially be false, got true")
	}

	stats := eng.GetGlobalStats()
	if stats.GermanyMode {
		t.Errorf("expected GlobalStats.GermanyMode to be false, got true")
	}

	// Enable Germany Mode dynamically
	eng.SetGermanyMode(true)
	if !eng.IsGermanyMode() {
		t.Errorf("expected GermanyMode to be true after SetGermanyMode(true)")
	}
	if !cfg.GermanyMode {
		t.Errorf("expected cfg.GermanyMode to be updated to true")
	}

	stats = eng.GetGlobalStats()
	if !stats.GermanyMode {
		t.Errorf("expected GlobalStats.GermanyMode to be true, got false")
	}
	if stats.UploadRate != 0 {
		t.Errorf("expected GlobalStats.UploadRate to be 0 in Germany Mode, got %d", stats.UploadRate)
	}

	// Disable Germany Mode dynamically
	eng.SetGermanyMode(false)
	if eng.IsGermanyMode() {
		t.Errorf("expected GermanyMode to be false after SetGermanyMode(false)")
	}
}
