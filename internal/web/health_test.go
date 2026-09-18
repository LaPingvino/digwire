package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"digwire/internal/config"
	"digwire/internal/engine"
	"digwire/internal/search"
)

// A panicking request must be counted and reported, not silently swallowed: the process survives,
// but a panic out of the torrent client can leave it locked, and then only a restart helps.
func TestPanicIsCountedAndReported(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempDir, "xdg"))
	cfg := &config.Config{DownloadDir: filepath.Join(tempDir, "downloads"), WebPort: 9099}
	cfg.SetConfigPath(filepath.Join(tempDir, "config.yaml"))
	eng, err := engine.NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	server := NewServer(cfg, eng, search.NewManager(cfg))

	server.mux.HandleFunc("GET /api/test-panic", func(http.ResponseWriter, *http.Request) {
		panic("boom in a handler")
	})
	handler := server.recoverPanics(server.mux)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/test-panic", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	var rep engine.HealthReport
	if err := json.NewDecoder(rec.Body).Decode(&rep); err != nil {
		t.Fatal(err)
	}
	if rep.Panics != 1 || rep.LastPanic == nil {
		t.Fatalf("health = %+v, want one recorded panic", rep)
	}
	if rep.LastPanic.Message != "boom in a handler" || rep.LastPanic.Where != "GET /api/test-panic" {
		t.Fatalf("recorded %+v", rep.LastPanic)
	}
	if rep.StalledSeconds != 0 {
		t.Fatalf("a running engine reported as stalled for %ds", rep.StalledSeconds)
	}
}
