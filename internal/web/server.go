package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"digwire/internal/config"
	"digwire/internal/engine"
	"digwire/internal/search"
)

//go:embed embedded/*
var embeddedFiles embed.FS

type Server struct {
	cfg     *config.Config
	engine  *engine.Engine
	search  *search.Manager
	server  *http.Server
	mux     *http.ServeMux
}

func NewServer(cfg *config.Config, eng *engine.Engine, sm *search.Manager) *Server {
	s := &Server{
		cfg:    cfg,
		engine: eng,
		search: sm,
		mux:    http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	// API Endpoints
	s.mux.HandleFunc("GET /api/torrents", s.handleGetTorrents)
	s.mux.HandleFunc("POST /api/torrents/add", s.handleAddTorrent)
	s.mux.HandleFunc("POST /api/torrents/create", s.handleCreateTorrent)
	s.mux.HandleFunc("GET /api/torrents/{hash}/details", s.handleGetTorrentDetails)
	s.mux.HandleFunc("POST /api/torrents/{hash}/files/{index}/priority", s.handleSetFilePriority)
	s.mux.HandleFunc("POST /api/torrents/{hash}/webseeds", s.handleAddWebSeed)
	s.mux.HandleFunc("POST /api/torrents/{hash}/upgrade-to-swarm", s.handleUpgradeToSwarm)
	s.mux.HandleFunc("POST /api/torrents/{hash}/find-swarm", s.handleTriggerFindSwarm)
	s.mux.HandleFunc("POST /api/torrents/{hash}/pause", s.handlePauseTorrent)
	s.mux.HandleFunc("POST /api/torrents/{hash}/resume", s.handleResumeTorrent)
	s.mux.HandleFunc("DELETE /api/torrents/{hash}", s.handleDeleteTorrent)
	s.mux.HandleFunc("GET /api/search", s.handleSearch)
	s.mux.HandleFunc("GET /api/stats", s.handleStats)
	s.mux.HandleFunc("GET /api/config", s.handleGetConfig)
	s.mux.HandleFunc("POST /api/config", s.handleSaveConfig)
	s.mux.HandleFunc("GET /api/events", s.handleEventsSSE)
	s.mux.HandleFunc("POST /api/open-folder", s.handleOpenFolder)

	// Embedded Static UI files
	subFS, err := fs.Sub(embeddedFiles, "embedded")
	if err != nil {
		s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "Embedded UI assets not found", http.StatusInternalServerError)
		})
		return
	}
	fileServer := http.FileServer(http.FS(subFS))
	s.mux.Handle("/", fileServer)
}

func (s *Server) handleGetTorrents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	torrents := s.engine.GetTorrents()
	_ = json.NewEncoder(w).Encode(torrents)
}

type addRequest struct {
	URL string `json:"url"`
}

func (s *Server) handleAddTorrent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	defer func() {
		if rec := recover(); rec != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("internal error: %v", rec)})
		}
	}()

	// Check if multipart form upload (.torrent file)
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		err := r.ParseMultipartForm(32 << 20) // 32MB max
		if err != nil {
			http.Error(w, `{"error":"failed to parse multipart form"}`, http.StatusBadRequest)
			return
		}
		file, _, err := r.FormFile("torrent_file")
		if err != nil {
			http.Error(w, `{"error":"missing torrent_file field"}`, http.StatusBadRequest)
			return
		}
		defer file.Close()

		t, err := s.engine.AddTorrentFile(file)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "info_hash": t.InfoHash().HexString()})
		return
	}

	// JSON request (magnet URI or .torrent URL)
	var req addRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json body"}`, http.StatusBadRequest)
		return
	}

	if req.URL == "" {
		http.Error(w, `{"error":"url or magnet is required"}`, http.StatusBadRequest)
		return
	}

	// If it's an HTTP/HTTPS direct file link (not .torrent), attempt automatic swarm matching & WebSeed injection
	if (strings.HasPrefix(req.URL, "http://") || strings.HasPrefix(req.URL, "https://")) && !strings.HasSuffix(strings.ToLower(req.URL), ".torrent") {
		match, err := s.engine.FindAndAttachSwarm(r.Context(), req.URL, s.search)
		if err == nil && match != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":         "ok",
				"info_hash":      match.InfoHash,
				"name":           match.Name,
				"hybrid_webseed": true,
			})
			return
		}
	}

	t, err := s.engine.Add(req.URL)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if t != nil {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "info_hash": t.InfoHash().HexString()})
	} else {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "info_hash": engine.HashURL(req.URL), "type": "http"})
	}
}

type createTorrentRequest struct {
	Path    string `json:"path"`
	Comment string `json:"comment"`
}

func (s *Server) handleCreateTorrent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var req createTorrentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json body"}`, http.StatusBadRequest)
		return
	}

	if req.Path == "" {
		http.Error(w, `{"error":"source path is required"}`, http.StatusBadRequest)
		return
	}

	hash, magnet, err := s.engine.CreateTorrent(req.Path, req.Comment)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":     "ok",
		"info_hash":  hash,
		"magnet_uri": magnet,
	})
}

func (s *Server) handleGetTorrentDetails(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	hash := r.PathValue("hash")
	details, err := s.engine.GetTorrentDetails(hash)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusNotFound)
		return
	}

	_ = json.NewEncoder(w).Encode(details)
}

type setFilePriorityRequest struct {
	Priority int `json:"priority"` // 0: none, 1: normal, 2: high
}

func (s *Server) handleSetFilePriority(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	hash := r.PathValue("hash")
	idxStr := r.PathValue("index")
	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		http.Error(w, `{"error":"invalid file index"}`, http.StatusBadRequest)
		return
	}

	var req setFilePriorityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json body"}`, http.StatusBadRequest)
		return
	}

	if err := s.engine.SetFilePriority(hash, idx, req.Priority); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

type addWebSeedRequest struct {
	URL string `json:"url"`
}

func (s *Server) handleAddWebSeed(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	hash := r.PathValue("hash")

	var req addWebSeedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		http.Error(w, `{"error":"valid url required"}`, http.StatusBadRequest)
		return
	}

	if err := s.engine.AddWebSeed(hash, req.URL); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleUpgradeToSwarm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	hash := r.PathValue("hash")

	t, err := s.engine.UpgradeHTTPToSwarm(hash)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":    "ok",
		"info_hash": t.InfoHash().HexString(),
	})
}

func (s *Server) handleTriggerFindSwarm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	hash := r.PathValue("hash")

	sugg, err := s.engine.TriggerFindSwarm(r.Context(), hash)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":          "ok",
		"suggested_swarm": sugg,
	})
}

func (s *Server) handlePauseTorrent(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if err := s.engine.Pause(hash); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleResumeTorrent(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if err := s.engine.Resume(hash); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleDeleteTorrent(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	deleteFiles := r.URL.Query().Get("delete_files") == "true"

	if err := s.engine.Remove(hash, deleteFiles); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	query := r.URL.Query().Get("q")
	if strings.TrimSpace(query) == "" {
		_ = json.NewEncoder(w).Encode([]search.Result{})
		return
	}

	results := s.search.SearchAll(r.Context(), query)
	_ = json.NewEncoder(w).Encode(results)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	stats := s.engine.GetGlobalStats()
	_ = json.NewEncoder(w).Encode(stats)
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(s.cfg)
}

func (s *Server) handleSaveConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var newCfg config.Config
	if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	*s.cfg = newCfg
	_ = s.cfg.Save()
	s.search.UpdateProviders(s.cfg)

	_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

func (s *Server) handleOpenFolder(w http.ResponseWriter, r *http.Request) {
	dir := s.cfg.DownloadDir
	_ = os.MkdirAll(dir, 0755)

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", dir)
	case "darwin":
		cmd = exec.Command("open", dir)
	case "windows":
		cmd = exec.Command("explorer", dir)
	}

	if cmd != nil {
		_ = cmd.Start()
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "opened"})
}

// Server-Sent Events for real-time reactive UI updates
func (s *Server) handleEventsSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			payload := map[string]any{
				"torrents": s.engine.GetTorrents(),
				"stats":    s.engine.GetGlobalStats(),
			}
			data, err := json.Marshal(payload)
			if err == nil {
				_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		}
	}
}

func (s *Server) Start() error {
	addr := fmt.Sprintf("0.0.0.0:%d", s.cfg.WebPort)
	s.server = &http.Server{
		Addr:    addr,
		Handler: s.mux,
	}
	return s.server.ListenAndServe()
}

func (s *Server) Close() error {
	if s.server != nil {
		return s.server.Close()
	}
	return nil
}
