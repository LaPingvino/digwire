package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"digwire/internal/config"
	"digwire/internal/engine"
	"digwire/internal/search"
)

func TestGermanyModeAPI(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "digwire-web-germany-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := &config.Config{
		DownloadDir: filepath.Join(tempDir, "downloads"),
		ListenPort:  0,
		WebPort:     9091,
		GermanyMode: false,
	}
	cfg.SetConfigPath(filepath.Join(tempDir, "config.yaml"))

	eng, err := engine.NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer eng.Close()

	sm := search.NewManager(cfg)
	server := NewServer(cfg, eng, sm)

	// 1. Check initial stats
	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	rec := httptest.NewRecorder()
	server.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from /api/stats, got %d", rec.Code)
	}

	var stats engine.GlobalStats
	if err := json.NewDecoder(rec.Body).Decode(&stats); err != nil {
		t.Fatalf("failed to decode stats: %v", err)
	}
	if stats.GermanyMode {
		t.Errorf("expected stats.GermanyMode to be false, got true")
	}

	// 2. Toggle Germany Mode via POST /api/config/germany-mode
	toggleBody, _ := json.Marshal(map[string]bool{"enabled": true})
	req = httptest.NewRequest(http.MethodPost, "/api/config/germany-mode", bytes.NewReader(toggleBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	server.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from /api/config/germany-mode, got %d", rec.Code)
	}

	var toggleResp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&toggleResp); err != nil {
		t.Fatalf("failed to decode toggle resp: %v", err)
	}
	if toggleResp["germany_mode"] != true {
		t.Errorf("expected germany_mode: true in response, got %v", toggleResp["germany_mode"])
	}

	if !eng.IsGermanyMode() {
		t.Errorf("expected engine.IsGermanyMode() to be true")
	}

	// 3. Check stats again
	req = httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	rec = httptest.NewRecorder()
	server.mux.ServeHTTP(rec, req)

	var statsAfter engine.GlobalStats
	_ = json.NewDecoder(rec.Body).Decode(&statsAfter)
	if !statsAfter.GermanyMode {
		t.Errorf("expected statsAfter.GermanyMode to be true, got false")
	}

	// 4. Toggle back
	toggleBody, _ = json.Marshal(map[string]bool{"enabled": false})
	req = httptest.NewRequest(http.MethodPost, "/api/config/germany-mode", bytes.NewReader(toggleBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	server.mux.ServeHTTP(rec, req)

	if eng.IsGermanyMode() {
		t.Errorf("expected engine.IsGermanyMode() to be false")
	}
}

func TestSoulseekSharesAPI(t *testing.T) {
	tempDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.DownloadDir = tempDir
	cfg.SoulseekShareMode = "none"
	cfg.SoulseekShareExts = ".mp3, .flac"
	cfg.SetConfigPath(filepath.Join(tempDir, "config.yaml"))

	eng, err := engine.NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer eng.Close()

	sm := search.NewManager(cfg)
	server := NewServer(cfg, eng, sm)

	// 1. GET /api/soulseek/shares (initially none: 0 folders, 0 files)
	req := httptest.NewRequest(http.MethodGet, "/api/soulseek/shares", nil)
	rec := httptest.NewRecorder()
	server.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from /api/soulseek/shares, got %d", rec.Code)
	}

	var stats search.ShareStats
	if err := json.NewDecoder(rec.Body).Decode(&stats); err != nil {
		t.Fatalf("failed to decode shares stats: %v", err)
	}
	if stats.FolderCount != 0 || stats.FileCount != 0 {
		t.Errorf("expected 0 folders and 0 files initially, got %d folders, %d files", stats.FolderCount, stats.FileCount)
	}

	// 2. Create files on disk: 1 music folder with 2 files
	albumDir := filepath.Join(tempDir, "TestArtist - TestAlbum")
	_ = os.MkdirAll(albumDir, 0755)
	_ = os.WriteFile(filepath.Join(albumDir, "01.mp3"), []byte("audio"), 0644)
	_ = os.WriteFile(filepath.Join(albumDir, "02.flac"), []byte("flac audio"), 0644)

	// 3. Save config via POST /api/config with soulseek_share_mode: "music"
	cfg.SoulseekShareMode = "music"
	cfgBody, _ := json.Marshal(cfg)
	req = httptest.NewRequest(http.MethodPost, "/api/config", bytes.NewReader(cfgBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	server.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from /api/config, got %d", rec.Code)
	}

	// 4. POST /api/soulseek/shares/rescan
	req = httptest.NewRequest(http.MethodPost, "/api/soulseek/shares/rescan", nil)
	rec = httptest.NewRecorder()
	server.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from /api/soulseek/shares/rescan, got %d", rec.Code)
	}

	var updatedStats search.ShareStats
	if err := json.NewDecoder(rec.Body).Decode(&updatedStats); err != nil {
		t.Fatalf("failed to decode updated stats: %v", err)
	}
	if updatedStats.FolderCount != 1 || updatedStats.FileCount != 2 {
		t.Errorf("expected 1 folder and 2 files after rescan, got %d folders, %d files", updatedStats.FolderCount, updatedStats.FileCount)
	}
}

