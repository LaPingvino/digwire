package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

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

func NewServer(cfg *config.Config, engine *engine.Engine, search *search.Manager) *Server {
	s := &Server{
		cfg:    cfg,
		engine: engine,
		search: search,
		mux:    http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	// API Endpoints
	s.mux.HandleFunc("GET /api/torrents", s.handleGetTorrents)
	s.mux.HandleFunc("POST /api/torrents/add", s.handleAddTorrent)
	s.mux.HandleFunc("POST /api/torrents/add-folder", s.handleAddFolderGroup)
	s.mux.HandleFunc("POST /api/torrents/add-group", s.handleAddFolderGroup)
	s.mux.HandleFunc("POST /api/torrents/create", s.handleCreateTorrent)
	s.mux.HandleFunc("POST /api/torrents/create-bridge", s.handleCreateBridgeTorrent)
	s.mux.HandleFunc("GET /api/torrents/{hash}/details", s.handleGetTorrentDetails)
	s.mux.HandleFunc("GET /api/torrents/{hash}/export", s.handleExportTorrentFile)
	s.mux.HandleFunc("GET /api/torrents/inspect", s.handleInspectTorrent)
	s.mux.HandleFunc("GET /api/torrents/scrape", s.handleScrapeTorrent)
	s.mux.HandleFunc("POST /api/torrents/{hash}/files/{index}/priority", s.handleSetFilePriority)
	s.mux.HandleFunc("POST /api/torrents/{hash}/open", s.handleOpenTorrent)
	s.mux.HandleFunc("POST /api/torrents/{hash}/show-in-folder", s.handleShowTorrentInFolder)
	s.mux.HandleFunc("POST /api/torrents/{hash}/files/{index}/open", s.handleOpenFile)
	s.mux.HandleFunc("POST /api/torrents/{hash}/files/{index}/show-in-folder", s.handleShowFileInFolder)
	s.mux.HandleFunc("GET /api/torrents/{hash}/files/{index}/view", s.handleStreamFile)
	s.mux.HandleFunc("POST /api/torrents/{hash}/webseeds", s.handleAddWebSeed)
	s.mux.HandleFunc("POST /api/torrents/{hash}/upgrade-to-swarm", s.handleUpgradeToSwarm)
	s.mux.HandleFunc("POST /api/torrents/{hash}/find-swarm", s.handleTriggerFindSwarm)
	s.mux.HandleFunc("POST /api/torrents/{hash}/verify", s.handleVerifyTorrent)
	s.mux.HandleFunc("POST /api/torrents/{hash}/pause", s.handlePauseTorrent)
	s.mux.HandleFunc("POST /api/torrents/{hash}/resume", s.handleResumeTorrent)
	s.mux.HandleFunc("DELETE /api/torrents/{hash}", s.handleDeleteTorrent)
	s.mux.HandleFunc("GET /api/search", s.handleSearch)
	s.mux.HandleFunc("GET /api/stats", s.handleStats)
	s.mux.HandleFunc("GET /api/config", s.handleGetConfig)
	s.mux.HandleFunc("POST /api/config", s.handleSaveConfig)
	s.mux.HandleFunc("POST /api/config/germany-mode", s.handleToggleGermanyMode)
	s.mux.HandleFunc("GET /api/config/yaml", s.handleGetConfigYAML)
	s.mux.HandleFunc("POST /api/config/yaml", s.handleSaveConfigYAML)
	s.mux.HandleFunc("POST /api/providers/test", s.handleTestProvider)
	s.mux.HandleFunc("POST /api/providers/reset", s.handleResetProviders)
	s.mux.HandleFunc("POST /api/dht/preseed", s.handlePreseedDHT)
	s.mux.HandleFunc("GET /api/events", s.handleEventsSSE)
	s.mux.HandleFunc("POST /api/open-folder", s.handleOpenFolder)
	s.mux.HandleFunc("GET /api/system/pick-path", s.handlePickPath)
	// Media & Social Swarm Endpoints (yt-dlp)
	s.mux.HandleFunc("POST /api/media/inspect", s.handleMediaInspect)
	s.mux.HandleFunc("POST /api/media/download", s.handleMediaDownload)
	s.mux.HandleFunc("GET /api/media/tasks", s.handleGetMediaTasks)
	s.mux.HandleFunc("DELETE /api/media/tasks/{id}", s.handleCancelMediaTask)

	// Subtitles & Audio Track Endpoints
	s.mux.HandleFunc("POST /api/subtitles/search", s.handleSearchSubtitles)
	s.mux.HandleFunc("POST /api/subtitles/download", s.handleDownloadSubtitle)
	s.mux.HandleFunc("POST /api/subtitles/extract", s.handleExtractSubtitle)

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
	if s.engine != nil {
		s.engine.WaitForSession(3 * time.Second)
	}
	torrents := s.engine.GetTorrents()
	_ = json.NewEncoder(w).Encode(torrents)
}

type addRequest struct {
	URL            string      `json:"url"`
	SelectedFiles  []int       `json:"selected_files,omitempty"`
	FilePriorities map[int]int `json:"file_priorities,omitempty"`
}

func (s *Server) handleAddTorrent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	defer func() {
		if rec := recover(); rec != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": fmt.Sprintf("internal error: %v", rec)})
		}
	}()

	ct := r.Header.Get("Content-Type")

	// Case 1: Raw binary upload of .torrent file (application/x-bittorrent or application/octet-stream)
	if strings.HasPrefix(ct, "application/x-bittorrent") || strings.HasPrefix(ct, "application/octet-stream") {
		limitedBody := io.LimitReader(r.Body, 32<<20)
		t, err := s.engine.AddTorrentFile(limitedBody)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "info_hash": t.InfoHash().HexString()})
		return
	}

	// Case 2: Multipart form upload (.torrent file)
	if strings.HasPrefix(ct, "multipart/form-data") {
		err := r.ParseMultipartForm(32 << 20) // 32MB max
		if err != nil {
			http.Error(w, `{"error":"failed to parse multipart form"}`, http.StatusBadRequest)
			return
		}

		var file io.ReadCloser

		// 1) Try standard form file field names
		for _, key := range []string{"torrent_file", "file", "torrent", "upload"} {
			if f, _, err := r.FormFile(key); err == nil {
				file = f
				break
			}
		}

		// 2) Fallback: if no named file matched, pick any file uploaded in the multipart form
		if file == nil && r.MultipartForm != nil && r.MultipartForm.File != nil {
			for _, headers := range r.MultipartForm.File {
				if len(headers) > 0 {
					if f, err := headers[0].Open(); err == nil {
						file = f
						break
					}
				}
			}
		}

		if file != nil {
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

		// 3) Fallback: check if the .torrent content or path was sent as a form value (e.g. filename was omitted)
		if r.MultipartForm != nil && r.MultipartForm.Value != nil {
			for _, key := range []string{"torrent_file", "file", "torrent", "url", "path"} {
				if vals := r.MultipartForm.Value[key]; len(vals) > 0 && len(vals[0]) > 0 {
					val := vals[0]
					if len(val) > 10 && val[0] == 'd' && val[len(val)-1] == 'e' {
						t, err := s.engine.AddTorrentFile(strings.NewReader(val))
						if err == nil {
							_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "info_hash": t.InfoHash().HexString()})
							return
						}
					}
					t, err := s.engine.Add(val)
					if err != nil {
						w.WriteHeader(http.StatusInternalServerError)
						_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
						return
					}
					if t != nil {
						_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "info_hash": t.InfoHash().HexString()})
					} else {
						_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "info_hash": engine.HashURL(val), "type": "http"})
					}
					return
				}
			}
		}

		http.Error(w, `{"error":"missing torrent_file field"}`, http.StatusBadRequest)
		return
	}

	// JSON request (magnet URI or .torrent URL with optional selective file indices/priorities)
	var req addRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json body"}`, http.StatusBadRequest)
		return
	}

	if req.URL == "" {
		http.Error(w, `{"error":"url or magnet is required"}`, http.StatusBadRequest)
		return
	}

	t, err := s.engine.AddWithSelection(req.URL, req.SelectedFiles, req.FilePriorities)
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

type addGroupRequest struct {
	Name       string                   `json:"name"`
	FolderName string                   `json:"folder_name"`
	Items      []engine.FolderItemInput `json:"items"`
}

func (s *Server) handleAddFolderGroup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var req addGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json body"}`, http.StatusBadRequest)
		return
	}

	if len(req.Items) == 0 {
		http.Error(w, `{"error":"items array is required and must not be empty"}`, http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		req.Name = req.FolderName
	}
	if req.Name == "" {
		req.Name = "Folder Download"
	}

	task, err := s.engine.FolderManager().StartFolderDownload(req.Name, req.FolderName, req.Items)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":    "ok",
		"id":        task.ID,
		"name":      task.Name,
		"dest_path": task.DestPath,
		"num_files": len(task.Files),
	})
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

type createBridgeTorrentRequest struct {
	URL     string   `json:"url"`
	Mirrors []string `json:"mirrors"`
	Comment string   `json:"comment"`
}

func (s *Server) handleCreateBridgeTorrent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var req createBridgeTorrentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		http.Error(w, `{"error":"valid url required"}`, http.StatusBadRequest)
		return
	}

	hash, magnet, err := s.engine.CreateWebBridgeTorrent(r.Context(), req.URL, req.Mirrors, req.Comment)
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

func (s *Server) handleExportTorrentFile(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if hash == "" {
		http.Error(w, `{"error":"missing infohash"}`, http.StatusBadRequest)
		return
	}

	data, filename, err := s.engine.GetTorrentFileBytes(hash)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/x-bittorrent")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
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

func (s *Server) handleOpenTorrent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	hash := r.PathValue("hash")
	targetPath, err := s.engine.GetTorrentSavePath(hash)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if err := engine.OpenPath(targetPath); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "path": targetPath})
}

func (s *Server) handleShowTorrentInFolder(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	hash := r.PathValue("hash")
	targetPath, err := s.engine.GetTorrentSavePath(hash)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if err := engine.ShowInFolder(targetPath); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "path": targetPath})
}

func (s *Server) handleOpenFile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	hash := r.PathValue("hash")
	idxStr := r.PathValue("index")
	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		http.Error(w, `{"error":"invalid file index"}`, http.StatusBadRequest)
		return
	}

	filePath, err := s.engine.GetTorrentFilePath(hash, idx)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if err := engine.OpenPath(filePath); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "path": filePath})
}

func (s *Server) handleShowFileInFolder(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	hash := r.PathValue("hash")
	idxStr := r.PathValue("index")
	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		http.Error(w, `{"error":"invalid file index"}`, http.StatusBadRequest)
		return
	}

	filePath, err := s.engine.GetTorrentFilePath(hash, idx)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if err := engine.ShowInFolder(filePath); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "path": filePath})
}

func (s *Server) handleStreamFile(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	idxStr := r.PathValue("index")
	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		http.Error(w, "invalid file index", http.StatusBadRequest)
		return
	}

	filePath, err := s.engine.GetTorrentFilePath(hash, idx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	cleanPath := filepath.Clean(filePath)
	if _, err := os.Stat(cleanPath); err != nil {
		if _, pErr := os.Stat(cleanPath + ".part"); pErr == nil {
			cleanPath = cleanPath + ".part"
		} else {
			http.Error(w, "file not found on disk or not downloaded yet", http.StatusNotFound)
			return
		}
	}

	http.ServeFile(w, r, cleanPath)
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

func (s *Server) handleVerifyTorrent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	hash := r.PathValue("hash")
	if err := s.engine.VerifyTorrentData(hash); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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
	w.Header().Set("Content-Type", "application/json")
	hash := r.PathValue("hash")
	if qHash := r.URL.Query().Get("hash"); qHash != "" && (hash == "" || hash == "undefined") {
		hash = qHash
	}
	if qURL := r.URL.Query().Get("url"); qURL != "" && (hash == "" || hash == "undefined") {
		hash = qURL
	}
	if unescaped, err := url.PathUnescape(hash); err == nil && unescaped != "" {
		hash = unescaped
	}
	hash = strings.TrimSpace(hash)
	deleteFiles := r.URL.Query().Get("delete_files") == "true"

	if err := s.engine.Remove(hash, deleteFiles); err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
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

func (s *Server) handleInspectTorrent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	magOrHash := r.URL.Query().Get("magnet")
	if magOrHash == "" {
		magOrHash = r.URL.Query().Get("hash")
	}
	if magOrHash == "" {
		http.Error(w, `{"error":"missing magnet or hash parameter"}`, http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	res, err := s.engine.InspectMagnetMetadata(ctx, magOrHash)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusGatewayTimeout)
		return
	}

	_ = json.NewEncoder(w).Encode(res)
}

func (s *Server) handleScrapeTorrent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	magOrHash := r.URL.Query().Get("magnet")
	if magOrHash == "" {
		magOrHash = r.URL.Query().Get("hash")
	}
	if magOrHash == "" {
		http.Error(w, `{"error":"missing magnet or hash parameter"}`, http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	seeders, leechers, err := s.engine.ScrapeSwarm(ctx, magOrHash)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusGatewayTimeout)
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"seeders":  seeders,
		"leechers": leechers,
	})
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
	s.engine.SetGermanyMode(s.cfg.GermanyMode)

	_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

type toggleGermanyModeRequest struct {
	Enabled *bool `json:"enabled"`
}

func (s *Server) handleToggleGermanyMode(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var req toggleGermanyModeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Enabled == nil {
		newVal := !s.engine.IsGermanyMode()
		s.cfg.GermanyMode = newVal
		_ = s.cfg.Save()
		s.engine.SetGermanyMode(newVal)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "germany_mode": newVal})
		return
	}

	s.cfg.GermanyMode = *req.Enabled
	_ = s.cfg.Save()
	s.engine.SetGermanyMode(*req.Enabled)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "germany_mode": *req.Enabled})
}

func (s *Server) handleGetConfigYAML(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	cfgPath := config.GetConfigPath()
	data, err := os.ReadFile(cfgPath)
	if err != nil || len(data) == 0 {
		out, err := yaml.Marshal(s.cfg)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(out)
		return
	}
	_, _ = w.Write(data)
}

func (s *Server) handleSaveConfigYAML(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"failed to read request body"}`, http.StatusBadRequest)
		return
	}

	var newCfg config.Config
	if err := yaml.Unmarshal(body, &newCfg); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "YAML syntax error: " + err.Error()})
		return
	}

	*s.cfg = newCfg
	_ = s.cfg.Save()
	s.search.UpdateProviders(s.cfg)
	s.engine.SetGermanyMode(s.cfg.GermanyMode)

	_ = json.NewEncoder(w).Encode(map[string]string{"status": "saved"})
}

type testProviderRequest struct {
	Provider config.SearchProviderConfig `json:"provider"`
	Query    string                      `json:"query"`
}

func (s *Server) handleTestProvider(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var req testProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	q := strings.TrimSpace(req.Query)
	if q == "" {
		q = "ubuntu"
	}

	tempCfg := &config.Config{
		SearchProviders: []config.SearchProviderConfig{req.Provider},
	}
	tempManager := search.NewManager(tempCfg)

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	start := time.Now()
	results := tempManager.SearchAll(ctx, q)
	duration := time.Since(start)

	if len(results) == 0 {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":          true,
			"count":       0,
			"duration_ms": duration.Milliseconds(),
			"samples":     []interface{}{},
		})
		return
	}

	type sampleResult struct {
		Title     string `json:"title"`
		SizeBytes int64  `json:"size_bytes"`
		Seeders   int    `json:"seeders"`
		InfoHash  string `json:"info_hash"`
	}
	var samples []sampleResult
	limit := 5
	if len(results) < limit {
		limit = len(results)
	}
	for i := 0; i < limit; i++ {
		samples = append(samples, sampleResult{
			Title:     results[i].Title,
			SizeBytes: results[i].SizeBytes,
			Seeders:   results[i].Seeders,
			InfoHash:  results[i].InfoHash,
		})
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":          true,
		"count":       len(results),
		"duration_ms": duration.Milliseconds(),
		"samples":     samples,
	})
}

func (s *Server) handleResetProviders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	def := config.DefaultConfig()
	s.cfg.SearchProviders = def.SearchProviders
	_ = s.cfg.Save()
	s.search.UpdateProviders(s.cfg)

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "reset",
		"providers": s.cfg.SearchProviders,
	})
}

func (s *Server) handlePreseedDHT(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	indexer := s.engine.DHTIndexer()
	if indexer == nil {
		http.Error(w, `{"error":"DHT indexer is not active"}`, http.StatusBadRequest)
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		_, _ = indexer.PreseedFromTorrentsCSV(ctx)
	}()

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "preseeding_started",
		"message": "TorrentsCSV pre-seeding started in background",
		"current_size": indexer.Size(),
	})
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

func (s *Server) handlePickPath(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	targetType := r.URL.Query().Get("type") // "file" or "folder"

	// 1. Check if zenity is available
	if zenityPath, err := exec.LookPath("zenity"); err == nil {
		args := []string{"--file-selection", "--title=Select File or Folder to Seed"}
		if targetType == "folder" || targetType == "directory" {
			args = append(args, "--directory")
		}
		cmd := exec.Command(zenityPath, args...)
		out, err := cmd.Output()
		if err == nil {
			selected := strings.TrimSpace(string(out))
			if selected != "" {
				_ = json.NewEncoder(w).Encode(map[string]string{"path": selected})
				return
			}
		}
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			_ = json.NewEncoder(w).Encode(map[string]string{"path": "", "cancelled": "true"})
			return
		}
	}

	// 2. Check if kdialog is available
	if kdialogPath, err := exec.LookPath("kdialog"); err == nil {
		var cmd *exec.Cmd
		if targetType == "folder" || targetType == "directory" {
			cmd = exec.Command(kdialogPath, "--getexistingdirectory")
		} else {
			cmd = exec.Command(kdialogPath, "--getopenfilename")
		}
		out, err := cmd.Output()
		if err == nil {
			selected := strings.TrimSpace(string(out))
			if selected != "" {
				_ = json.NewEncoder(w).Encode(map[string]string{"path": selected})
				return
			}
		}
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			_ = json.NewEncoder(w).Encode(map[string]string{"path": "", "cancelled": "true"})
			return
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]string{"error": "no native dialog available", "fallback": "inapp"})
}

func (s *Server) handleBrowseDir(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	dirPath := r.URL.Query().Get("path")
	if dirPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "/"
		}
		dirPath = home
	}
	dirPath = filepath.Clean(dirPath)

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	type fileItem struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		IsDir bool   `json:"is_dir"`
		Size  int64  `json:"size"`
	}

	var items []fileItem
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") && e.Name() != ".." {
			continue
		}
		info, err := e.Info()
		var sz int64
		if err == nil {
			sz = info.Size()
		}
		items = append(items, fileItem{
			Name:  e.Name(),
			Path:  filepath.Join(dirPath, e.Name()),
			IsDir: e.IsDir(),
			Size:  sz,
		})
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"current": dirPath,
		"parent":  filepath.Dir(dirPath),
		"items":   items,
	})
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

func (s *Server) handleMediaInspect(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.URL) == "" {
		http.Error(w, `{"error":"missing or invalid media url"}`, http.StatusBadRequest)
		return
	}

	meta, err := engine.InspectMedia(r.Context(), req.URL)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(meta)
}

func (s *Server) handleMediaDownload(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		URL         string   `json:"url"`
		Format      string   `json:"format"`
		AudioOnly   bool     `json:"audio_only"`
		AudioFormat string   `json:"audio_format"`
		Subtitles   []string `json:"subtitles"`
		EmbedSubs   bool     `json:"embed_subs"`
		AutoSwarm   bool     `json:"auto_swarm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.URL) == "" {
		http.Error(w, `{"error":"missing or invalid media url"}`, http.StatusBadRequest)
		return
	}

	opts := engine.MediaDownloadOptions{
		Format:      req.Format,
		AudioOnly:   req.AudioOnly,
		AudioFormat: req.AudioFormat,
		Subtitles:   req.Subtitles,
		EmbedSubs:   req.EmbedSubs,
		AutoSwarm:   true,
	}

	if s.engine.MediaManager() == nil {
		http.Error(w, `{"error":"media manager not initialized"}`, http.StatusInternalServerError)
		return
	}

	task, err := s.engine.MediaManager().StartDownload(req.URL, opts)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"id":     task.ID,
		"title":  task.Title,
	})
}

func (s *Server) handleGetMediaTasks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.engine.MediaManager() == nil {
		_ = json.NewEncoder(w).Encode([]any{})
		return
	}
	tasks := s.engine.MediaManager().GetTasks()
	_ = json.NewEncoder(w).Encode(tasks)
}

func (s *Server) handleCancelMediaTask(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	id := r.PathValue("id")
	if qID := r.URL.Query().Get("id"); qID != "" && (id == "" || id == "undefined") {
		id = qID
	}
	if unescaped, err := url.PathUnescape(id); err == nil && unescaped != "" {
		id = unescaped
	}
	id = strings.TrimSpace(id)
	deleteFiles := r.URL.Query().Get("delete_files") == "true"

	if err := s.engine.Remove(id, deleteFiles); err != nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleSearchSubtitles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		Query string `json:"query"`
		Lang  string `json:"lang"`
		Hash  string `json:"hash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	query := strings.TrimSpace(req.Query)
	if query == "" && req.Hash != "" {
		if details, err := s.engine.GetTorrentDetails(req.Hash); err == nil && details != nil {
			query = engine.CleanMediaTitleForSubtitles(details.Name)
		}
	}

	tracks, err := engine.SearchOpenSubtitles(r.Context(), query, req.Lang)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(tracks)
}

func (s *Server) handleDownloadSubtitle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		Hash        string `json:"hash"`
		DownloadURL string `json:"download_url"`
		Lang        string `json:"lang"`
		FileName    string `json:"file_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.DownloadURL == "" {
		http.Error(w, `{"error":"missing download_url"}`, http.StatusBadRequest)
		return
	}

	savePath, err := s.engine.GetTorrentSavePath(req.Hash)
	if err != nil || savePath == "" {
		savePath = s.cfg.DownloadDir
	}

	targetDir := savePath
	if fi, err := os.Stat(savePath); err == nil && !fi.IsDir() {
		targetDir = filepath.Dir(savePath)
	}

	videoFilename := req.FileName
	if videoFilename == "" {
		videoFilename = filepath.Base(savePath)
	}

	outPath, err := engine.DownloadAndAttachSubtitle(r.Context(), targetDir, videoFilename, req.DownloadURL, req.Lang)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"path":   outPath,
	})
}

func (s *Server) handleExtractSubtitle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		Hash        string `json:"hash"`
		StreamIndex int    `json:"stream_index"`
		Lang        string `json:"lang"`
		FilePath    string `json:"file_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
		return
	}

	videoPath := req.FilePath
	if videoPath == "" && req.Hash != "" {
		savePath, err := s.engine.GetTorrentSavePath(req.Hash)
		if err == nil {
			videoPath = savePath
		}
	}

	if fi, err := os.Stat(videoPath); err != nil || fi.IsDir() {
		http.Error(w, `{"error":"video file not found"}`, http.StatusBadRequest)
		return
	}

	outPath, err := engine.ExtractEmbeddedSubtitle(videoPath, req.StreamIndex, req.Lang)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"path":   outPath,
	})
}
