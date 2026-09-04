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
		GermanyMode: false,
	}

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
