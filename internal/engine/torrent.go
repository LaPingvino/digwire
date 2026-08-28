package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"digwire/internal/config"
	"digwire/internal/dhtindex"
	"digwire/internal/search"

	"github.com/anacrolix/dht/v2"
	g "github.com/anacrolix/generics"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

type SavedTorrent struct {
	InfoHash       string   `json:"info_hash"`
	MagnetURI      string   `json:"magnet_uri"`
	Name           string   `json:"name"`
	IsPaused       bool     `json:"is_paused"`
	IsSeeding      bool     `json:"is_seeding,omitempty"`
	AddedAt        int64    `json:"added_at"`
	WebSeeds       []string `json:"webseeds"`
	TotalBytes     int64    `json:"total_bytes,omitempty"`
	CompletedBytes int64    `json:"completed_bytes,omitempty"`
}

type SavedHTTPTask struct {
	ID             string   `json:"id"`
	URL            string   `json:"url"`
	Mirrors        []string `json:"mirrors"`
	Name           string   `json:"name"`
	TotalBytes     int64    `json:"total_bytes"`
	CompletedBytes int64    `json:"completed_bytes"`
	State          string   `json:"state"`
	DestPath       string   `json:"dest_path"`
	AddedAt        int64    `json:"added_at"`
}

type SessionState struct {
	Torrents  []SavedTorrent  `json:"torrents"`
	HTTPTasks []SavedHTTPTask `json:"http_tasks,omitempty"`
}

type TorrentStatus struct {
	InfoHash       string          `json:"info_hash"`
	Name           string          `json:"name"`
	MagnetURI      string          `json:"magnet_uri"`
	TotalBytes     int64           `json:"total_bytes"`
	CompletedBytes int64           `json:"completed_bytes"`
	Progress       float64         `json:"progress"` // 0.0 to 100.0
	DownloadRate   int64           `json:"download_rate"` // bytes/sec
	UploadRate     int64           `json:"upload_rate"`   // bytes/sec
	ETASeconds     int64           `json:"eta_seconds"`
	State          string          `json:"state"` // "downloading", "seeding", "paused", "metadata", "completed"
	SavePath       string          `json:"save_path,omitempty"` // absolute path to downloaded file or folder
	Seeders        int             `json:"seeders"`
	Leechers       int             `json:"leechers"`
	Peers          int             `json:"peers"`
	Files          []string        `json:"files,omitempty"`
	AddedAt        int64           `json:"added_at"`
	SuggestedSwarm *SwarmSuggestion `json:"suggested_swarm,omitempty"`
	WebSeeds       []string        `json:"webseeds,omitempty"`
	Qualifier      *SwarmQualifier `json:"qualifier,omitempty"`
	AvailabilityETA string         `json:"availability_eta,omitempty"`
}

type TorrentFileDetail struct {
	Index          int     `json:"index"`
	Path           string  `json:"path"`
	FullPath       string  `json:"full_path,omitempty"`
	Length         int64   `json:"length"`
	BytesCompleted int64   `json:"bytes_completed"`
	Progress       float64 `json:"progress"`
	Priority       int     `json:"priority"` // 0: None, 1: Normal, 2: High
	Completed      bool    `json:"completed"`
}

type PeerDetail struct {
	Addr   string `json:"addr"`
	Source string `json:"source"`
}

type TorrentDetails struct {
	InfoHash       string              `json:"info_hash"`
	Name           string              `json:"name"`
	MagnetURI      string              `json:"magnet_uri"`
	TotalBytes     int64               `json:"total_bytes"`
	CompletedBytes int64               `json:"completed_bytes"`
	Progress       float64             `json:"progress"`
	PieceLength    int64               `json:"piece_length"`
	NumPieces      int                 `json:"num_pieces"`
	DownloadDir    string              `json:"download_dir"`
	SavePath       string              `json:"save_path,omitempty"` // absolute path to downloaded file or folder
	State          string              `json:"state"`
	Seeders        int                 `json:"seeders"`
	Leechers       int                 `json:"leechers"`
	TotalPeers     int                 `json:"total_peers"`
	Files          []TorrentFileDetail `json:"files"`
	Peers          []PeerDetail        `json:"peers"`
	Trackers       []string            `json:"trackers"`
	WebSeeds       []string            `json:"webseeds"`
	CreatedBy      string              `json:"created_by"`
	Comment        string              `json:"comment"`
	SuggestedSwarm *SwarmSuggestion    `json:"suggested_swarm,omitempty"`
	Qualifier      *SwarmQualifier     `json:"qualifier,omitempty"`
	AvailabilityETA string             `json:"availability_eta,omitempty"`
}

type GlobalStats struct {
	DownloadRate    int64 `json:"download_rate"`
	UploadRate      int64 `json:"upload_rate"`
	ActiveCount     int   `json:"active_count"`
	TotalCount      int   `json:"total_count"`
	DHTNodes        int   `json:"dht_nodes"`
	DHTIndexedCount int   `json:"dht_indexed_count"`
}

type rateTracker struct {
	lastBytesRead       int64
	lastBytesWritten    int64
	lastTime            time.Time
	downloadRate        int64
	uploadRate          int64
	avgDLRate           int64
	peakDLRate          int64
	peakSeeders         int
	peakPeers           int
	lastSampleTime      time.Time
	addedAt             int64
	isPaused            bool
	isSeeding           bool
	isVerifying         bool
	displayName         string
	savedTotalBytes     int64
	savedCompletedBytes int64
	skippedFiles        map[int]bool
}

type Engine struct {
	mu          sync.RWMutex
	client      *torrent.Client
	pieceComp   storage.PieceCompletion
	httpManager *HTTPManager
	searchMgr   *search.Manager
	dhtIndexer  *dhtindex.Indexer
	cfg         *config.Config
	rateMap     map[string]*rateTracker
	webSeedsMap map[string][]string
	stopMonitor chan struct{}
	closeOnce   sync.Once
}

func (e *Engine) DHTIndexer() *dhtindex.Indexer {
	return e.dhtIndexer
}

func (e *Engine) SetSearchManager(sm *search.Manager) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.searchMgr = sm
}

func isHexString(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func getSessionFilePath() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = "."
	}
	return filepath.Join(configDir, "digwire", "session.json")
}

func getTorrentsCacheDir() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = "."
	}
	dir := filepath.Join(configDir, "digwire", "torrents")
	_ = os.MkdirAll(dir, 0755)
	return dir
}

func getTorrentCacheFilePath(infoHashHex string) string {
	if infoHashHex == "" {
		return ""
	}
	return filepath.Join(getTorrentsCacheDir(), strings.ToLower(infoHashHex)+".torrent")
}

func (e *Engine) saveTorrentMetainfo(t *torrent.Torrent) {
	if t == nil || t.Info() == nil {
		return
	}
	hash := t.InfoHash().HexString()
	filePath := getTorrentCacheFilePath(hash)
	if filePath == "" {
		return
	}
	mi := t.Metainfo()
	f, err := os.Create(filePath)
	if err != nil {
		return
	}
	defer f.Close()
	_ = mi.Write(f)
}

func NewEngine(cfg *config.Config) (*Engine, error) {
	if err := os.MkdirAll(cfg.DownloadDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create download directory: %w", err)
	}

	tConfig := torrent.NewDefaultClientConfig()
	tConfig.DataDir = cfg.DownloadDir
	tConfig.NoDHT = !cfg.EnableDHT
	tConfig.PeriodicallyAnnounceTorrentsToDht = true
	tConfig.ListenPort = cfg.ListenPort

	// BEP 10 / BEP 20 Protocol Identity & Extension Handshake
	tConfig.ExtendedHandshakeClientVersion = "Digwire 0.2.2"
	tConfig.Bep20 = "-DW0202-"

	// Swarm Altruism & Reliable Metadata Exchange (BEP 9 / ut_metadata)
	tConfig.Seed = true
	tConfig.AcceptPeerConnections = true
	tConfig.AlwaysWantConns = true
	tConfig.NoDefaultPortForwarding = false
	tConfig.UpnpID = "digwire"

	// High-Availability DHT Bootstrap Routers
	tConfig.DhtStartingNodes = func(network string) dht.StartingNodesGetter {
		return func() ([]dht.Addr, error) {
			custom := []string{
				"router.bittorrent.com:6881",
				"dht.transmissionbt.com:6881",
				"dht.libtorrent.org:25401",
				"router.utorrent.com:6881",
				"dht.aelitis.com:6881",
			}
			addrs, _ := dht.ResolveHostPorts(custom)
			defAddrs, _ := dht.GlobalBootstrapAddrs(network)
			return append(addrs, defAddrs...), nil
		}
	}

	// Active DHT Node (participates in routing, replies to queries from other nodes)
	tConfig.ConfigureAnacrolixDhtServer = func(dhtCfg *dht.ServerConfig) {
		dhtCfg.Passive = false
		dhtCfg.WaitToReply = false
	}

	// 1/1 Gbps High-Throughput & Low-Latency Tuning
	tConfig.EstablishedConnsPerTorrent = 250
	tConfig.HalfOpenConnsPerTorrent = 60
	tConfig.TotalHalfOpenConns = 150
	tConfig.TorrentPeersHighWater = 2000
	tConfig.TorrentPeersLowWater = 200
	tConfig.HandshakesTimeout = 5 * time.Second
	tConfig.NominalDialTimeout = 5 * time.Second
	tConfig.MinDialTimeout = 1 * time.Second
	tConfig.HeaderObfuscationPolicy = torrent.HeaderObfuscationPolicy{
		Preferred:        true,
		RequirePreferred: false,
	}

	numCPU := runtime.NumCPU()
	if numCPU < 4 {
		numCPU = 4
	}
	tConfig.PieceHashersPerTorrent = numCPU

	tConfig.WebTransport = &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   8 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          128,
		MaxIdleConnsPerHost:   32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   8 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DisableCompression:   true,
		ReadBufferSize:        128 * 1024,
		WriteBufferSize:       64 * 1024,
	}

	// Persistent SQLite Piece Completion Database (.torrent.db, stores verified SHA-1 piece hashes on disk across restarts)
	configDir, _ := os.UserConfigDir()
	var pieceCompDir string
	if configDir != "" {
		pieceCompDir = filepath.Join(configDir, "digwire")
	} else {
		pieceCompDir = cfg.DownloadDir
	}
	_ = os.MkdirAll(pieceCompDir, 0755)

	pieceCompletion, pErr := storage.NewDefaultPieceCompletionForDir(pieceCompDir)
	if pErr != nil {
		pieceCompletion = storage.NewMapPieceCompletion()
	}

	storageOpts := storage.NewFileClientOpts{
		ClientBaseDir:   cfg.DownloadDir,
		PieceCompletion: pieceCompletion,
		UsePartFiles:    g.Some(false),
	}
	tConfig.DefaultStorage = storage.NewFileOpts(storageOpts)

	client, err := torrent.NewClient(tConfig)
	if err != nil && tConfig.ListenPort != 0 {
		tConfig.ListenPort = 0
		client, err = torrent.NewClient(tConfig)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to initialize bittorrent client: %w", err)
	}

	dhtIdx, _ := dhtindex.NewIndexer(client)

	e := &Engine{
		client:      client,
		pieceComp:   pieceCompletion,
		httpManager: NewHTTPManager(cfg.DownloadDir),
		dhtIndexer:  dhtIdx,
		cfg:         cfg,
		rateMap:     make(map[string]*rateTracker),
		webSeedsMap: make(map[string][]string),
		stopMonitor: make(chan struct{}),
	}

	// Restore active session across restarts
	e.loadSession()

	go e.monitorLoop()
	return e, nil
}

func (e *Engine) saveSessionLocked() {
	filePath := getSessionFilePath()
	_ = os.MkdirAll(filepath.Dir(filePath), 0755)

	torrents := e.client.Torrents()
	var list []SavedTorrent

	for _, t := range torrents {
		hash := t.InfoHash().HexString()
		tr := e.rateMap[hash]
		if tr == nil {
			continue // Do not persist probing torrents
		}
		addedAt := tr.addedAt
		isPaused := tr.isPaused

		name := t.Name()
		if name == "" && tr.displayName != "" {
			name = tr.displayName
		}

		var totalBytes, completedBytes int64
		if t.Info() != nil {
			totalBytes = t.Length()
			completedBytes = t.BytesCompleted()
			e.saveTorrentMetainfo(t)
		} else {
			totalBytes = tr.savedTotalBytes
			completedBytes = tr.savedCompletedBytes
		}

		if tr.isVerifying && tr.savedCompletedBytes > completedBytes {
			completedBytes = tr.savedCompletedBytes
		}

		webseeds := e.webSeedsMap[hash]
		mag := fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", hash, url.QueryEscape(name))
		mag = AppendWebSeedsToMagnet(SuperchargeMagnet(mag), webseeds)

		isSeeding := tr.isSeeding || (totalBytes > 0 && completedBytes >= totalBytes) || t.Seeding()

		list = append(list, SavedTorrent{
			InfoHash:       hash,
			MagnetURI:      mag,
			Name:           name,
			IsPaused:       isPaused,
			IsSeeding:      isSeeding,
			AddedAt:        addedAt,
			WebSeeds:       webseeds,
			TotalBytes:     totalBytes,
			CompletedBytes: completedBytes,
		})
	}

	// Save HTTP tasks
	var httpList []SavedHTTPTask
	e.httpManager.mu.RLock()
	for _, task := range e.httpManager.tasks {
		task.mu.Lock()
		httpList = append(httpList, SavedHTTPTask{
			ID:             task.ID,
			URL:            task.URL,
			Mirrors:        task.Mirrors,
			Name:           task.Name,
			TotalBytes:     task.TotalBytes,
			CompletedBytes: task.CompletedBytes,
			State:          task.State,
			DestPath:       task.DestPath,
			AddedAt:        task.AddedAt,
		})
		task.mu.Unlock()
	}
	e.httpManager.mu.RUnlock()

	data, err := json.MarshalIndent(SessionState{Torrents: list, HTTPTasks: httpList}, "", "  ")
	if err == nil {
		_ = os.WriteFile(filePath, data, 0644)
	}
}

func (e *Engine) loadSession() {
	filePath := getSessionFilePath()
	data, err := os.ReadFile(filePath)
	if err != nil {
		return
	}

	var state SessionState
	if err := json.Unmarshal(data, &state); err != nil {
		return
	}

	for _, item := range state.Torrents {
		if item.MagnetURI == "" && item.InfoHash != "" {
			item.MagnetURI = "magnet:?xt=urn:btih:" + item.InfoHash
		}
		if item.MagnetURI == "" && item.InfoHash == "" {
			continue
		}

		var t *torrent.Torrent
		var hash string
		cachedTorrentPath := getTorrentCacheFilePath(item.InfoHash)

		// 1. Try loading cached .torrent metainfo directly so metadata & piece progress are immediately available on start!
		if item.InfoHash != "" {
			if _, err := os.Stat(cachedTorrentPath); err == nil {
				if mi, err := metainfo.LoadFromFile(cachedTorrentPath); err == nil && mi != nil {
					if addedT, err := e.client.AddTorrent(mi); err == nil {
						t = addedT
						hash = t.InfoHash().HexString()
					}
				}
			}
		}

		// 2. Fall back to AddMagnet
		if t == nil {
			if item.MagnetURI == "" {
				continue
			}
			addedT, err := e.client.AddMagnet(item.MagnetURI)
			if err != nil {
				continue
			}
			t = addedT
			hash = t.InfoHash().HexString()
		}

		// Immediately disallow data download so no incoming peer data is requested or written
		// before data is verified! This prevents redundant downloads or race conditions on restart.
		t.DisallowDataDownload()
		t.AllowDataUpload()

		// Inject tier-1 trackers
		t.AddTrackers(GetTier1TrackerList())

		savedCompleted := item.CompletedBytes
		if t != nil && t.Info() != nil {
			bComp := t.BytesCompleted()
			if bComp > savedCompleted {
				savedCompleted = bComp
			}
		}

		peakS := 0
		peakP := 0
		if e.dhtIndexer != nil {
			if rec := e.dhtIndexer.GetRecord(hash); rec != nil && rec.Activity != nil {
				peakS = rec.Activity.LastSeeders
				peakP = rec.Activity.LastPeers
			}
		}

		isSeeding := item.IsSeeding || (item.TotalBytes > 0 && item.CompletedBytes >= item.TotalBytes)

		e.rateMap[hash] = &rateTracker{
			lastTime:            time.Now(),
			addedAt:             item.AddedAt,
			isPaused:            item.IsPaused,
			isSeeding:           isSeeding,
			displayName:         item.Name,
			savedTotalBytes:     item.TotalBytes,
			savedCompletedBytes: savedCompleted,
			peakSeeders:         peakS,
			peakPeers:           peakP,
		}

		if len(item.WebSeeds) > 0 {
			e.webSeedsMap[hash] = SanitizeWebSeeds(item.WebSeeds, false)
		}

		if item.IsPaused {
			t.DisallowDataDownload()
		}

		go func(tor *torrent.Torrent, seeds []string, h string) {
			<-tor.GotInfo()
			if len(seeds) > 0 && tor.Info() != nil {
				clean := SanitizeWebSeeds(seeds, tor.Info().IsDir())
				if len(clean) > 0 {
					tor.AddWebSeeds(clean)
				}
			}
			e.ConsolidateAndVerify(tor)
		}(t, item.WebSeeds, hash)
	}

	// Restore HTTP downloads
	for _, ht := range state.HTTPTasks {
		task := &HTTPTask{
			ID:             ht.ID,
			URL:            ht.URL,
			Mirrors:        ht.Mirrors,
			Name:           ht.Name,
			TotalBytes:     ht.TotalBytes,
			CompletedBytes: ht.CompletedBytes,
			State:          ht.State,
			DestPath:       ht.DestPath,
			AddedAt:        ht.AddedAt,
			lastTime:       time.Now(),
			client:         &http.Client{Timeout: 0},
		}
		e.httpManager.mu.Lock()
		e.httpManager.tasks[ht.ID] = task
		e.httpManager.mu.Unlock()

		if ht.State == "downloading" {
			go task.runDownload()
		}
	}
}

func (e *Engine) monitorLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-e.stopMonitor:
			return
		case now := <-ticker.C:
			e.mu.Lock()
			torrents := e.client.Torrents()
			for _, t := range torrents {
				hash := t.InfoHash().HexString()
				tracker, exists := e.rateMap[hash]
				if !exists {
					// Ignore temporary probing/inspecting torrents
					continue
				}

				stats := t.Stats()
				deltaSec := now.Sub(tracker.lastTime).Seconds()
				if deltaSec > 0 {
					tracker.downloadRate = int64(float64(stats.BytesReadData.Int64() - tracker.lastBytesRead) / deltaSec)
					tracker.uploadRate = int64(float64(stats.BytesWrittenData.Int64() - tracker.lastBytesWritten) / deltaSec)
					if tracker.downloadRate < 0 {
						tracker.downloadRate = 0
					}
					if tracker.uploadRate < 0 {
						tracker.uploadRate = 0
					}
					if tracker.downloadRate > 0 {
						if tracker.avgDLRate == 0 {
							tracker.avgDLRate = tracker.downloadRate
						} else {
							tracker.avgDLRate = (tracker.avgDLRate*7 + tracker.downloadRate*3) / 10
						}
					}
				}

				tracker.lastBytesRead = stats.BytesReadData.Int64()
				tracker.lastBytesWritten = stats.BytesWrittenData.Int64()
				tracker.lastTime = now

				webConns := t.WebseedPeerConns()
				sCount := stats.ConnectedSeeders + len(webConns)
				aPeers := stats.ActivePeers + len(webConns)
				totPeers := stats.TotalPeers + len(webConns)
				if totPeers < aPeers {
					totPeers = aPeers
				}

				if sCount > tracker.peakSeeders {
					tracker.peakSeeders = sCount
				}
				if totPeers > tracker.peakPeers {
					tracker.peakPeers = totPeers
				}
				if tracker.downloadRate > tracker.peakDLRate {
					tracker.peakDLRate = tracker.downloadRate
				}

				isSeeding := t.Seeding() || (t.Length() > 0 && t.BytesCompleted() >= t.Length()) || tracker.isSeeding
				if isSeeding && !tracker.isSeeding {
					tracker.isSeeding = true
					t.DisallowDataDownload()
					t.AllowDataUpload()
					e.saveSessionLocked()
				}

				// Periodically sample swarm presence into DHT indexer (every 60 seconds)
				if now.Sub(tracker.lastSampleTime) >= 60*time.Second {
					tracker.lastSampleTime = now
					if e.dhtIndexer != nil {
						dhtSeeders := sCount
						if isSeeding {
							if tracker.peakSeeders > 0 {
								dhtSeeders = tracker.peakSeeders + 1
							} else {
								dhtSeeders = 1
							}
						}
						dhtPeers := totPeers
						if dhtPeers < dhtSeeders {
							dhtPeers = dhtSeeders
						}
						e.dhtIndexer.RecordSwarmActivity(hash, tracker.displayName, dhtSeeders, dhtPeers)
					}
				}
			}
			e.mu.Unlock()

			// Update HTTP task stats
			e.httpManager.UpdateStats(now)
		}
	}
}

func (e *Engine) Add(uriOrURL string) (*torrent.Torrent, error) {
	uriOrURL = strings.TrimSpace(uriOrURL)

	// Support adding by raw 40-character hex infohash or 32-character base32 infohash
	if len(uriOrURL) == 40 && isHexString(uriOrURL) {
		uriOrURL = "magnet:?xt=urn:btih:" + uriOrURL
	} else if len(uriOrURL) == 32 && !strings.Contains(uriOrURL, ":") {
		uriOrURL = "magnet:?xt=urn:btih:" + uriOrURL
	}

	// If it's a web URL to a .torrent file
	if (strings.HasPrefix(uriOrURL, "http://") || strings.HasPrefix(uriOrURL, "https://")) && strings.HasSuffix(strings.ToLower(uriOrURL), ".torrent") {
		req, err := http.NewRequest(http.MethodGet, uriOrURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "Digwire/1.0")

		client := &http.Client{Timeout: 7 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch torrent file: %w", err)
		}
		defer resp.Body.Close()

		mi, err := metainfo.Load(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to decode torrent metadata: %w", err)
		}

		t, err := e.client.AddTorrent(mi)
		if err != nil {
			return nil, err
		}
		e.mu.Lock()
		e.saveTorrentMetainfo(t)
		e.initTracker(t.InfoHash().HexString())
		e.saveSessionLocked()
		e.mu.Unlock()

		e.ConsolidateAndVerify(t)
		return t, nil
	}

	// Direct HTTP file download
	if strings.HasPrefix(uriOrURL, "http://") || strings.HasPrefix(uriOrURL, "https://") {
		task, err := e.httpManager.StartDownload(uriOrURL)
		if err != nil {
			return nil, err
		}
		e.mu.Lock()
		e.saveSessionLocked()
		e.mu.Unlock()

		// Background swarm discovery with multi-piece random sampling verification
		if e.searchMgr != nil {
			go func(t *HTTPTask) {
				ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
				defer cancel()
				sugg, err := e.FindSuggestedSwarm(ctx, t, e.searchMgr)
				if err == nil && sugg != nil {
					t.mu.Lock()
					t.SuggestedSwarm = sugg
					t.mu.Unlock()
				}
			}(task)
		}
		return nil, nil
	}

	// Magnet link or infohash
	var extractedWebSeeds []string
	var extractedPeers []string
	var displayName string
	if strings.HasPrefix(uriOrURL, "magnet:?") {
		if magObj, err := metainfo.ParseMagnetUri(uriOrURL); err == nil {
			extractedWebSeeds = magObj.Params["ws"]
			displayName = magObj.DisplayName
		}
		extractedPeers = ExtractPeersFromMagnet(uriOrURL)
	}

	uriOrURL = SuperchargeMagnet(uriOrURL)
	t, err := e.client.AddMagnet(uriOrURL)
	if err != nil {
		return nil, err
	}

	// Immediately inject Tier-1 Trackers
	t.AddTrackers(GetTier1TrackerList())

	// Immediately inject direct peers (e.g. from x.pe)
	peerInfos := ConvertToPeerInfos(extractedPeers)
	if len(peerInfos) > 0 {
		t.AddPeers(peerInfos)
	}

	hash := t.InfoHash().HexString()
	e.mu.Lock()
	if len(extractedWebSeeds) > 0 {
		e.webSeedsMap[hash] = SanitizeWebSeeds(append(e.webSeedsMap[hash], extractedWebSeeds...), false)
	}
	wsList := e.webSeedsMap[hash]
	e.initTracker(hash, displayName)
	e.saveSessionLocked()
	e.mu.Unlock()

	go func(tor *torrent.Torrent, seeds []string, directPeers []torrent.PeerInfo) {
		// Active peer and tracker retry loop while waiting for metadata
		if tor.Info() == nil {
			ticker := time.NewTicker(3 * time.Second)
			defer ticker.Stop()
			stopRetry := time.After(45 * time.Second)

		retryLoop:
			for {
				select {
				case <-tor.GotInfo():
					break retryLoop
				case <-stopRetry:
					break retryLoop
				case <-ticker.C:
					if len(directPeers) > 0 {
						tor.AddPeers(directPeers)
					}
					tor.AddTrackers(GetTier1TrackerList())
				}
			}
		}

		<-tor.GotInfo()

		if len(seeds) > 0 && tor.Info() != nil {
			clean := SanitizeWebSeeds(seeds, tor.Info().IsDir())
			if len(clean) > 0 {
				tor.AddWebSeeds(clean)
			}
		}
		if e.dhtIndexer != nil && tor.Info() != nil {
			var fileNames []string
			for _, f := range tor.Info().UpvertedFiles() {
				fileNames = append(fileNames, f.DisplayPath(tor.Info()))
			}
			e.dhtIndexer.AddRecord(&dhtindex.DHTRecord{
				InfoHash:     tor.InfoHash().HexString(),
				Name:         tor.Info().BestName(),
				SizeBytes:    tor.Length(),
				NumFiles:     len(fileNames),
				DiscoveredAt: time.Now().Unix(),
				Files:        fileNames,
			})
		}
		e.ConsolidateAndVerify(tor)
	}(t, wsList, peerInfos)

	return t, nil
}

func (e *Engine) AddTorrentFile(reader io.Reader) (*torrent.Torrent, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	mi, err := metainfo.Load(reader)
	if err != nil {
		return nil, fmt.Errorf("invalid torrent file: %w", err)
	}

	t, err := e.client.AddTorrent(mi)
	if err != nil {
		return nil, err
	}
	e.saveTorrentMetainfo(t)
	e.initTracker(t.InfoHash().HexString())
	e.saveSessionLocked()

	e.ConsolidateAndVerify(t)
	return t, nil
}

func (e *Engine) CreateTorrent(sourcePath, comment string) (string, string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	stat, err := os.Stat(sourcePath)
	if err != nil {
		return "", "", fmt.Errorf("path not found: %w", err)
	}

	pieceLen := metainfo.ChoosePieceLength(stat.Size())
	info := metainfo.Info{
		PieceLength: pieceLen,
	}

	if err := info.BuildFromFilePath(sourcePath); err != nil {
		return "", "", fmt.Errorf("failed to scan files for torrent: %w", err)
	}

	mi := metainfo.MetaInfo{
		Comment:      comment,
		CreatedBy:    "Digwire P2P",
		CreationDate: time.Now().Unix(),
		AnnounceList: [][]string{
			{"udp://tracker.opentrackr.org:1337/announce"},
			{"udp://open.stealth.si:80/announce"},
			{"udp://tracker.torrent.eu.org:451/announce"},
			{"udp://explodie.org:6969/announce"},
		},
	}
	mi.SetDefaults()

	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal info dict: %w", err)
	}
	mi.InfoBytes = infoBytes

	torrentName := info.BestName()
	if torrentName == "" {
		torrentName = "share"
	}
	torrentFilePath := filepath.Join(e.cfg.DownloadDir, torrentName+".torrent")
	if f, err := os.Create(torrentFilePath); err == nil {
		_ = mi.Write(f)
		_ = f.Close()
	}

	t, err := e.client.AddTorrent(&mi)
	if err != nil {
		return "", "", fmt.Errorf("failed to seed created torrent: %w", err)
	}

	hash := t.InfoHash().HexString()
	h := t.InfoHash()
	magnetObj := mi.Magnet(&h, &info)
	magnetURI := magnetObj.String()
	if magnetURI == "" {
		magnetURI = fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", hash, url.QueryEscape(torrentName))
	}
	magnetURI = AppendWebSeedsToMagnet(SuperchargeMagnet(magnetURI), nil)

	if e.dhtIndexer != nil {
		var fileNames []string
		for _, f := range info.UpvertedFiles() {
			fileNames = append(fileNames, f.DisplayPath(&info))
		}
		e.dhtIndexer.AddRecord(&dhtindex.DHTRecord{
			InfoHash:     hash,
			Name:         torrentName,
			SizeBytes:    info.TotalLength(),
			NumFiles:     len(fileNames),
			DiscoveredAt: time.Now().Unix(),
			Files:        fileNames,
		})
	}

	e.initTracker(hash, torrentName)
	if tr := e.rateMap[hash]; tr != nil {
		tr.isSeeding = true
		tr.savedTotalBytes = info.TotalLength()
		tr.savedCompletedBytes = info.TotalLength()
	}
	t.DisallowDataDownload()
	t.AllowDataUpload()
	_ = t.VerifyData()
	e.saveSessionLocked()
	return hash, magnetURI, nil
}

func (e *Engine) CreateWebBridgeTorrent(ctx context.Context, fileURL string, mirrors []string, comment string) (string, string, error) {
	filename, sizeBytes, _, err := InspectHTTPFile(ctx, fileURL)
	if err != nil {
		return "", "", fmt.Errorf("failed to inspect web file: %w", err)
	}

	task := &HTTPTask{
		URL:        fileURL,
		Name:       filename,
		TotalBytes: sizeBytes,
	}

	sugg, err := e.FindSuggestedSwarm(ctx, task, e.searchMgr)
	if err == nil && sugg != nil {
		t, err := e.client.AddMagnet(sugg.MagnetURI)
		if err != nil {
			return "", "", err
		}

		var allMirrors []string
		allMirrors = append(allMirrors, fileURL)
		allMirrors = append(allMirrors, mirrors...)
		cleanMirrors := SanitizeWebSeeds(allMirrors, false)
		hash := t.InfoHash().HexString()

		e.mu.Lock()
		e.webSeedsMap[hash] = cleanMirrors
		e.initTracker(hash)
		e.saveSessionLocked()
		e.mu.Unlock()

		go func(tor *torrent.Torrent, seeds []string) {
			<-tor.GotInfo()
			if len(seeds) > 0 && tor.Info() != nil {
				clean := SanitizeWebSeeds(seeds, tor.Info().IsDir())
				if len(clean) > 0 {
					tor.AddWebSeeds(clean)
				}
			}
			e.ConsolidateAndVerify(tor)
		}(t, cleanMirrors)

		mag := AppendWebSeedsToMagnet(SuperchargeMagnet(sugg.MagnetURI), cleanMirrors)
		return hash, mag, nil
	}

	return "", "", fmt.Errorf("could not find or verify swarm for %s", filename)
}

// AdoptExistingLocalProgress scans for any partial or remnant files matching the torrent layout
// (such as .part, .crdownload, .download, or direct wget partial downloads) and moves/renames them
// into the expected torrent file path so VerifyData() can immediately discover and reuse existing progress.
func (e *Engine) AdoptExistingLocalProgress(tor *torrent.Torrent) {
	if tor == nil || tor.Info() == nil {
		return
	}

	info := tor.Info()
	baseDownloadDir := e.cfg.DownloadDir

	for _, f := range info.UpvertedFiles() {
		displayPath := f.DisplayPath(info)
		targetPath := filepath.Join(baseDownloadDir, displayPath)

		// If target file already exists and has data, keep it
		if fi, err := os.Stat(targetPath); err == nil && fi.Size() > 0 {
			continue
		}

		// Candidate partial extensions and fallback locations (including .part)
		candidates := []string{
			targetPath + ".part",
			targetPath + ".crdownload",
			targetPath + ".download",
			targetPath + ".tmp",
		}

		// If multi-file torrent, also check if the file was downloaded directly into Downloads root (e.g. via wget)
		filenameOnly := filepath.Base(displayPath)
		if filenameOnly != "" && filenameOnly != displayPath {
			rootCandidate := filepath.Join(baseDownloadDir, filenameOnly)
			candidates = append(candidates,
				rootCandidate,
				rootCandidate+".part",
				rootCandidate+".crdownload",
				rootCandidate+".download",
				rootCandidate+".tmp",
			)
		}

		// Check candidates and adopt directly into expected targetPath
		for _, cand := range candidates {
			fi, err := os.Stat(cand)
			if err == nil && !fi.IsDir() && fi.Size() > 0 {
				_ = os.MkdirAll(filepath.Dir(targetPath), 0755)
				if err := os.Rename(cand, targetPath); err == nil {
					break
				}
			}
		}
	}
}

func (e *Engine) initTracker(hash string, displayName ...string) {
	name := ""
	if len(displayName) > 0 {
		name = displayName[0]
	}
	if tr, ok := e.rateMap[hash]; !ok {
		peakS := 0
		peakP := 0
		if e.dhtIndexer != nil {
			if rec := e.dhtIndexer.GetRecord(hash); rec != nil && rec.Activity != nil {
				peakS = rec.Activity.LastSeeders
				peakP = rec.Activity.LastPeers
			}
		}
		e.rateMap[hash] = &rateTracker{
			lastTime:    time.Now(),
			addedAt:     time.Now().Unix(),
			displayName: name,
			peakSeeders: peakS,
			peakPeers:   peakP,
		}
	} else if name != "" && tr.displayName == "" {
		tr.displayName = name
	}
}

// ConsolidateAndVerify executes a full data consolidation and cryptographic piece verification pipeline:
// 1. Ensures no duplicate verification races on the same torrent.
// 2. Temporarily disallows incoming peer data writes to prevent write collisions during disk hashing.
// 3. Flags the state as "verifying" so the UI and engine accurately reflect rechecking status.
// 4. Consolidates & adopts any external download remnants (.crdownload, .download, .tmp, wget root files).
// 5. Executes cryptographic SHA-1 verification across all pieces on disk.
// 6. Aligns verified byte totals, clears verifying status, and safely resumes downloading if unpaused.
func (e *Engine) ConsolidateAndVerify(tor *torrent.Torrent, onComplete ...func()) {
	if tor == nil {
		return
	}

	hash := tor.InfoHash().HexString()

	go func() {
		<-tor.GotInfo()

		e.mu.Lock()
		tr := e.rateMap[hash]
		if tr != nil {
			if tr.isVerifying {
				e.mu.Unlock()
				return // Already verifying
			}
			tr.isVerifying = true
		}
		e.mu.Unlock()

		// 1. Disallow incoming peer data writes while hashing disk files
		tor.DisallowDataDownload()

		// 2. Persist metainfo
		e.saveTorrentMetainfo(tor)

		// 3. Consolidate & adopt any external remnants
		e.AdoptExistingLocalProgress(tor)

		// 4. Run cryptographic verification
		_ = tor.VerifyData()

		// 5. Post-verification state alignment
		e.mu.Lock()
		bComp := tor.BytesCompleted()
		tLen := tor.Length()
		isComplete := (tLen > 0 && bComp >= tLen)

		if tr := e.rateMap[hash]; tr != nil {
			tr.isVerifying = false
			tr.savedTotalBytes = tLen
			tr.savedCompletedBytes = bComp
			if isComplete {
				tr.isSeeding = true
			} else if !isComplete && bComp < tLen {
				tr.isSeeding = false
			}

			if isComplete {
				// 100% complete: seed to the swarm, do not download
				tor.DisallowDataDownload()
				tor.AllowDataUpload()
			} else if !tr.isPaused {
				// Has missing pieces and not paused: resume downloading
				tor.AllowDataDownload()
				tor.DownloadAll()
			} else {
				tor.DisallowDataDownload()
			}
		} else {
			if isComplete {
				tor.DisallowDataDownload()
				tor.AllowDataUpload()
			} else {
				tor.AllowDataDownload()
				tor.DownloadAll()
			}
		}
		e.saveSessionLocked()
		e.mu.Unlock()

		for _, fn := range onComplete {
			if fn != nil {
				fn()
			}
		}
	}()
}

func (e *Engine) VerifyTorrentData(infoHashHex string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Check if HTTP Task
	e.httpManager.mu.RLock()
	httpTask, httpExists := e.httpManager.tasks[infoHashHex]
	e.httpManager.mu.RUnlock()

	if httpExists {
		go func(task *HTTPTask) {
			task.mu.Lock()
			partPath := task.DestPath + ".part"
			task.completedChunks = loadCompletedChunks(partPath)
			task.mu.Unlock()
		}(httpTask)
		return nil
	}

	torrents := e.client.Torrents()
	for _, t := range torrents {
		if strings.EqualFold(t.InfoHash().HexString(), infoHashHex) {
			e.ConsolidateAndVerify(t)
			return nil
		}
	}
	return fmt.Errorf("torrent not found: %s", infoHashHex)
}

func (e *Engine) Pause(infoHashHex string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Check if HTTP download task
	if err := e.httpManager.Pause(infoHashHex); err == nil {
		e.saveSessionLocked()
		return nil
	}

	torrents := e.client.Torrents()
	for _, t := range torrents {
		if strings.EqualFold(t.InfoHash().HexString(), infoHashHex) {
			t.DisallowDataDownload()
			if tr, ok := e.rateMap[infoHashHex]; ok {
				tr.isPaused = true
				tr.downloadRate = 0
			}
			e.saveSessionLocked()
			return nil
		}
	}
	return fmt.Errorf("torrent or download not found: %s", infoHashHex)
}

func (e *Engine) Resume(infoHashHex string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Check if HTTP download task
	if err := e.httpManager.Resume(infoHashHex); err == nil {
		e.saveSessionLocked()
		return nil
	}

	torrents := e.client.Torrents()
	for _, t := range torrents {
		if strings.EqualFold(t.InfoHash().HexString(), infoHashHex) {
			if tr, ok := e.rateMap[infoHashHex]; ok {
				tr.isPaused = false
			}
			e.ConsolidateAndVerify(t)
			e.saveSessionLocked()
			return nil
		}
	}
	return fmt.Errorf("torrent or download not found: %s", infoHashHex)
}

func (e *Engine) Remove(infoHashHex string, deleteFiles bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Check if HTTP download task
	if err := e.httpManager.Remove(infoHashHex, deleteFiles); err == nil {
		e.saveSessionLocked()
		return nil
	}

	torrents := e.client.Torrents()
	for _, t := range torrents {
		if strings.EqualFold(t.InfoHash().HexString(), infoHashHex) {
			name := t.Name()
			t.Drop()
			delete(e.rateMap, infoHashHex)
			delete(e.webSeedsMap, infoHashHex)
			_ = os.Remove(getTorrentCacheFilePath(infoHashHex))
			e.saveSessionLocked()

			if deleteFiles && name != "" {
				targetPath := filepath.Join(e.cfg.DownloadDir, name)
				_ = os.RemoveAll(targetPath)
			}
			return nil
		}
	}
	return fmt.Errorf("torrent or download not found: %s", infoHashHex)
}

func (e *Engine) GetTorrents() []TorrentStatus {
	e.mu.RLock()
	defer e.mu.RUnlock()

	torrents := e.client.Torrents()
	statuses := make([]TorrentStatus, 0, len(torrents))

	for _, t := range torrents {
		hash := t.InfoHash().HexString()
		tracker := e.rateMap[hash]
		if tracker == nil {
			// Skip internal background probing torrents
			continue
		}

		addedAt := tracker.addedAt
		isPaused := tracker.isPaused
		dlRate := tracker.downloadRate
		ulRate := tracker.uploadRate

		info := t.Info()
		var totalBytes, completedBytes int64
		var progress float64
		var name string
		var files []string
		state := "downloading"

		if info != nil {
			name = info.Name
			totalBytes = t.Length()
			completedBytes = t.BytesCompleted()
			if tracker.isVerifying && tracker.savedCompletedBytes > completedBytes {
				completedBytes = tracker.savedCompletedBytes
			}
			if totalBytes > 0 {
				progress = (float64(completedBytes) / float64(totalBytes)) * 100.0
			}

			if tracker.isVerifying {
				state = "verifying"
			} else if isPaused {
				state = "paused"
			} else if (completedBytes >= totalBytes && totalBytes > 0) || tracker.isSeeding {
				state = "seeding"
			} else {
				state = "downloading"
			}

			for _, f := range info.Files {
				files = append(files, strings.Join(f.Path, "/"))
			}
		} else {
			name = tracker.displayName
			if name == "" {
				name = t.Name()
			}
			if name == "" {
				name = "Resolving metadata (" + hash[:8] + "...)"
			}
			state = "metadata"
			totalBytes = tracker.savedTotalBytes
			completedBytes = tracker.savedCompletedBytes
			if totalBytes > 0 && completedBytes > 0 {
				progress = (float64(completedBytes) / float64(totalBytes)) * 100.0
			}
		}

		var eta int64 = 0
		if dlRate > 0 && totalBytes > completedBytes {
			eta = (totalBytes - completedBytes) / dlRate
		}

		stats := t.Stats()
		webseeds := e.webSeedsMap[hash]
		magURI := fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", hash, url.QueryEscape(name))
		magURI = AppendWebSeedsToMagnet(SuperchargeMagnet(magURI), webseeds)
		webConns := t.WebseedPeerConns()
		isSeeding := state == "seeding" || state == "completed" || (totalBytes > 0 && completedBytes >= totalBytes) || (tracker != nil && tracker.isSeeding)

		seeders := stats.ConnectedSeeders + len(webConns)
		if isSeeding {
			if tracker != nil && tracker.peakSeeders > 0 {
				seeders = tracker.peakSeeders + 1
			} else {
				seeders = stats.ConnectedSeeders + 1
			}
		}
		leechers := stats.ActivePeers - stats.ConnectedSeeders
		if isSeeding {
			leechers = stats.ActivePeers
		}
		if leechers < 0 {
			leechers = 0
		}
		activePeers := stats.ActivePeers + len(webConns)
		totalPeers := stats.TotalPeers + len(webConns)
		if totalPeers < activePeers {
			totalPeers = activePeers
		}
		if isSeeding && totalPeers < seeders {
			totalPeers = seeders + leechers
		}

		var activity *dhtindex.SwarmActivity
		if e.dhtIndexer != nil {
			if rec := e.dhtIndexer.GetRecord(hash); rec != nil {
				activity = rec.Activity
			}
		}

		peakS := 0
		peakP := 0
		peakDL := int64(0)
		avgRate := int64(0)
		if tracker != nil {
			peakS = tracker.peakSeeders
			peakP = tracker.peakPeers
			peakDL = tracker.peakDLRate
			avgRate = tracker.avgDLRate
		}

		qualifier := CalculateSwarmQualifier(SwarmContext{
			TotalBytes:     totalBytes,
			CompletedBytes: completedBytes,
			Seeders:        seeders,
			Leechers:       leechers,
			ActivePeers:    activePeers,
			TotalPeers:     totalPeers,
			PeakSeeders:    peakS,
			PeakPeers:      peakP,
			DLRate:         dlRate,
			PeakDLRate:     peakDL,
			AvgDLRate:      avgRate,
			IsSeeding:      isSeeding,
			IsHTTP:         false,
			Activity:       activity,
			AddedAt:        addedAt,
		})

		savePath := e.getTorrentSavePath(t)
		statuses = append(statuses, TorrentStatus{
			InfoHash:        hash,
			Name:            name,
			MagnetURI:       magURI,
			TotalBytes:      totalBytes,
			CompletedBytes:  completedBytes,
			Progress:        progress,
			DownloadRate:    dlRate,
			UploadRate:      ulRate,
			ETASeconds:      eta,
			State:           state,
			SavePath:        savePath,
			Seeders:         seeders,
			Leechers:        leechers,
			Peers:           totalPeers,
			Files:           files,
			AddedAt:         addedAt,
			WebSeeds:        webseeds,
			Qualifier:       &qualifier,
			AvailabilityETA: qualifier.AvailabilityETA,
		})
	}

	// Add Direct HTTP Downloads
	e.httpManager.mu.RLock()
	for _, task := range e.httpManager.tasks {
		task.mu.Lock()
		var prog float64 = 0
		if task.TotalBytes > 0 {
			prog = (float64(task.CompletedBytes) / float64(task.TotalBytes)) * 100.0
		}
		qualifier := CalculateSwarmQualifier(SwarmContext{
			TotalBytes:     task.TotalBytes,
			CompletedBytes: task.CompletedBytes,
			Seeders:        len(task.Mirrors),
			Leechers:       0,
			ActivePeers:    len(task.Mirrors),
			TotalPeers:     len(task.Mirrors),
			PeakSeeders:    len(task.Mirrors),
			PeakPeers:      len(task.Mirrors),
			DLRate:         task.DownloadRate,
			PeakDLRate:     task.DownloadRate,
			AvgDLRate:      task.DownloadRate,
			IsSeeding:      false,
			IsHTTP:         true,
			Activity:       nil,
			AddedAt:        task.AddedAt,
		})
		statuses = append(statuses, TorrentStatus{
			InfoHash:        task.ID,
			Name:            task.Name,
			MagnetURI:       task.URL,
			TotalBytes:      task.TotalBytes,
			CompletedBytes:  task.CompletedBytes,
			Progress:        prog,
			DownloadRate:    task.DownloadRate,
			UploadRate:      0,
			ETASeconds:      task.ETASeconds,
			State:           task.State,
			SavePath:        task.DestPath,
			Seeders:         len(task.Mirrors),
			Peers:           len(task.Mirrors),
			Files:           []string{task.Name},
			AddedAt:         task.AddedAt,
			SuggestedSwarm:  task.SuggestedSwarm,
			WebSeeds:        task.Mirrors,
			Qualifier:       &qualifier,
			AvailabilityETA: qualifier.AvailabilityETA,
		})
		task.mu.Unlock()
	}
	e.httpManager.mu.RUnlock()

	// Sort deterministically: newest first, then alphabetical by name
	sort.Slice(statuses, func(i, j int) bool {
		if statuses[i].AddedAt != statuses[j].AddedAt {
			return statuses[i].AddedAt > statuses[j].AddedAt
		}
		return statuses[i].Name < statuses[j].Name
	})

	return statuses
}

func (e *Engine) GetTorrentDetails(infoHashHex string) (*TorrentDetails, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Check if HTTP Task
	e.httpManager.mu.RLock()
	task, exists := e.httpManager.tasks[infoHashHex]
	e.httpManager.mu.RUnlock()

	if exists {
		task.mu.Lock()
		defer task.mu.Unlock()

		var prog float64 = 0
		if task.TotalBytes > 0 {
			prog = (float64(task.CompletedBytes) / float64(task.TotalBytes)) * 100.0
		}

		var peerDetails []PeerDetail
		for _, m := range task.Mirrors {
			sourceDesc := "HTTP Mirror Source"
			if st, ok := task.MirrorStats[m]; ok && st.DownloadRate > 0 {
				sourceDesc += fmt.Sprintf(" • ↓ %s/s", formatBytes(st.DownloadRate))
			}
			peerDetails = append(peerDetails, PeerDetail{
				Addr:   m,
				Source: sourceDesc,
			})
		}

		qualifier := CalculateSwarmQualifier(SwarmContext{
			TotalBytes:     task.TotalBytes,
			CompletedBytes: task.CompletedBytes,
			Seeders:        len(task.Mirrors),
			Leechers:       0,
			ActivePeers:    len(task.Mirrors),
			TotalPeers:     len(task.Mirrors),
			PeakSeeders:    len(task.Mirrors),
			PeakPeers:      len(task.Mirrors),
			DLRate:         task.DownloadRate,
			PeakDLRate:     task.DownloadRate,
			AvgDLRate:      task.DownloadRate,
			IsSeeding:      false,
			IsHTTP:         true,
			Activity:       nil,
			AddedAt:        task.AddedAt,
		})

		return &TorrentDetails{
			InfoHash:        task.ID,
			Name:            task.Name,
			MagnetURI:       task.URL,
			TotalBytes:      task.TotalBytes,
			CompletedBytes:  task.CompletedBytes,
			Progress:        prog,
			DownloadDir:     e.cfg.DownloadDir,
			SavePath:        task.DestPath,
			State:           task.State,
			Files: []TorrentFileDetail{
				{
					Index:          0,
					Path:           task.Name,
					FullPath:       task.DestPath,
					Length:         task.TotalBytes,
					BytesCompleted: task.CompletedBytes,
					Progress:       prog,
					Priority:       1,
					Completed:      task.State == "completed" || (task.TotalBytes > 0 && task.CompletedBytes >= task.TotalBytes),
				},
			},
			Peers:           peerDetails,
			Seeders:         len(task.Mirrors),
			Leechers:        0,
			TotalPeers:      len(task.Mirrors),
			WebSeeds:        task.Mirrors,
			CreatedBy:       "Multi-Source HTTP Downloader",
			SuggestedSwarm:  task.SuggestedSwarm,
			Qualifier:       &qualifier,
			AvailabilityETA: qualifier.AvailabilityETA,
		}, nil
	}

	torrents := e.client.Torrents()
	for _, t := range torrents {
		if strings.EqualFold(t.InfoHash().HexString(), infoHashHex) {
			info := t.Info()
			h := t.InfoHash()
			hashHex := h.HexString()
			name := t.Name()
			if name == "" {
				name = "Resolving metadata (" + hashHex[:8] + "...)"
			}
			totalBytes := t.Length()
			completedBytes := t.BytesCompleted()
			var progress float64
			if totalBytes > 0 {
				progress = (float64(completedBytes) / float64(totalBytes)) * 100.0
			}

			magURI := fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", hashHex, url.QueryEscape(name))

			var files []TorrentFileDetail
			var pieceLength int64
			var numPieces int

			if info != nil {
				pieceLength = info.PieceLength
				numPieces = info.NumPieces()

				for idx, tf := range t.Files() {
					fLen := tf.Length()
					fComp := tf.BytesCompleted()
					var fProg float64
					if fLen > 0 {
						fProg = (float64(fComp) / float64(fLen)) * 100.0
					}
					prio := int(tf.Priority())
					tr := e.rateMap[strings.ToLower(hashHex)]
					if tr != nil && tr.skippedFiles != nil && tr.skippedFiles[idx] {
						prio = 0
					} else if prio == 0 {
						prio = 1 // Default to wanted/Normal
					}
					fullPath := filepath.Join(e.cfg.DownloadDir, tf.Path())
					files = append(files, TorrentFileDetail{
						Index:          idx,
						Path:           tf.Path(),
						FullPath:       fullPath,
						Length:         fLen,
						BytesCompleted: fComp,
						Progress:       fProg,
						Priority:       prio,
						Completed:      (fComp >= fLen && fLen > 0) || fProg >= 100.0,
					})
				}
			}

			var peerDetails []PeerDetail
			for _, pc := range t.PeerConns() {
				peerDetails = append(peerDetails, PeerDetail{
					Addr:   pc.RemoteAddr.String(),
					Source: pc.String(),
				})
			}
			for _, ws := range t.WebseedPeerConns() {
				sourceDesc := "WebSeed (BEP 19 HTTP Mirror)"
				dl := int64(ws.DownloadRate())
				if dl > 0 {
					sourceDesc += fmt.Sprintf(" • ↓ %s/s", formatBytes(dl))
				}
				peerDetails = append(peerDetails, PeerDetail{
					Addr:   ws.RemoteAddr.String(),
					Source: sourceDesc,
				})
			}

			trackers := []string{
				"udp://tracker.opentrackr.org:1337/announce",
				"udp://open.stealth.si:80/announce",
				"udp://tracker.torrent.eu.org:451/announce",
			}

			webseeds := e.webSeedsMap[hashHex]
			magURI = AppendWebSeedsToMagnet(SuperchargeMagnet(magURI), webseeds)

			st := t.Stats()
			webConns := t.WebseedPeerConns()
			tr := e.rateMap[strings.ToLower(hashHex)]

			isSeeding := (totalBytes > 0 && completedBytes >= totalBytes) || t.Seeding() || (tr != nil && tr.isSeeding)
			if tr != nil && tr.savedTotalBytes > 0 && tr.savedCompletedBytes >= tr.savedTotalBytes {
				isSeeding = true
			}

			sCount := st.ConnectedSeeders + len(webConns)
			if isSeeding {
				if tr != nil && tr.peakSeeders > 0 {
					sCount = tr.peakSeeders + 1
				} else {
					sCount = st.ConnectedSeeders + 1
				}
			}
			lCount := st.ActivePeers - st.ConnectedSeeders
			if isSeeding {
				lCount = st.ActivePeers
			}
			if lCount < 0 {
				lCount = 0
			}
			actPeers := st.ActivePeers + len(webConns)
			totPeers := st.TotalPeers + len(webConns)
			if totPeers < actPeers {
				totPeers = actPeers
			}
			if isSeeding && totPeers < sCount {
				totPeers = sCount + lCount
			}

			var activity *dhtindex.SwarmActivity
			if e.dhtIndexer != nil {
				if rec := e.dhtIndexer.GetRecord(hashHex); rec != nil {
					activity = rec.Activity
				}
			}

			var peakS, peakP int
			var peakDL, avgRate, curRate int64
			var addedAt int64
			if tr != nil {
				peakS = tr.peakSeeders
				peakP = tr.peakPeers
				peakDL = tr.peakDLRate
				avgRate = tr.avgDLRate
				curRate = tr.downloadRate
				addedAt = tr.addedAt
			}

			qualifier := CalculateSwarmQualifier(SwarmContext{
				TotalBytes:     totalBytes,
				CompletedBytes: completedBytes,
				Seeders:        sCount,
				Leechers:       lCount,
				ActivePeers:    actPeers,
				TotalPeers:     totPeers,
				PeakSeeders:    peakS,
				PeakPeers:      peakP,
				DLRate:         curRate,
				PeakDLRate:     peakDL,
				AvgDLRate:      avgRate,
				IsSeeding:      isSeeding,
				IsHTTP:         false,
				Activity:       activity,
				AddedAt:        addedAt,
			})

			displayState := "downloading"
			if tr != nil && tr.isVerifying {
				displayState = "verifying"
			} else if isSeeding {
				displayState = "seeding"
			} else if tr != nil && tr.isPaused {
				displayState = "paused"
			} else if info == nil {
				displayState = "metadata"
			}

			return &TorrentDetails{
				InfoHash:        hashHex,
				Name:            name,
				MagnetURI:       magURI,
				TotalBytes:      totalBytes,
				CompletedBytes:  completedBytes,
				Progress:        progress,
				PieceLength:     pieceLength,
				NumPieces:       numPieces,
				DownloadDir:     e.cfg.DownloadDir,
				SavePath:        e.getTorrentSavePath(t),
				State:           displayState,
				Seeders:         sCount,
				Leechers:        lCount,
				TotalPeers:      totPeers,
				Files:           files,
				Peers:           peerDetails,
				Trackers:        trackers,
				WebSeeds:        webseeds,
				CreatedBy:       "Digwire P2P",
				Qualifier:       &qualifier,
				AvailabilityETA: qualifier.AvailabilityETA,
			}, nil
		}
	}
	return nil, fmt.Errorf("torrent not found: %s", infoHashHex)
}

func (e *Engine) AddWebSeed(infoHashHex string, webSeedURL string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	webSeedURL = strings.TrimSpace(webSeedURL)
	if webSeedURL == "" {
		return fmt.Errorf("empty webseed url")
	}

	// Check if HTTP Task
	e.httpManager.mu.RLock()
	task, exists := e.httpManager.tasks[infoHashHex]
	e.httpManager.mu.RUnlock()
	if exists {
		task.AddMirror(webSeedURL)
		e.saveSessionLocked()
		return nil
	}

	torrents := e.client.Torrents()
	for _, t := range torrents {
		if strings.EqualFold(t.InfoHash().HexString(), infoHashHex) {
			var urlsToAdd []string
			urlsToAdd = append(urlsToAdd, webSeedURL)

			info := t.Info()
			if info != nil && info.IsDir() && !strings.HasSuffix(webSeedURL, "/") {
				urlsToAdd = append(urlsToAdd, webSeedURL+"/")
			}

			t.AddWebSeeds(urlsToAdd)
			e.webSeedsMap[infoHashHex] = append(e.webSeedsMap[infoHashHex], webSeedURL)
			e.saveSessionLocked()
			return nil
		}
	}
	return fmt.Errorf("torrent not found: %s", infoHashHex)
}

func (e *Engine) SetFilePriority(infoHashHex string, fileIndex int, priority int) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	torrents := e.client.Torrents()
	for _, t := range torrents {
		if strings.EqualFold(t.InfoHash().HexString(), infoHashHex) {
			files := t.Files()
			if fileIndex < 0 || fileIndex >= len(files) {
				return fmt.Errorf("file index %d out of bounds (total %d)", fileIndex, len(files))
			}
			f := files[fileIndex]
			switch priority {
			case 0:
				f.Cancel()
			case 2:
				f.SetPriority(torrent.PiecePriorityHigh)
			default:
				f.Download()
			}

			h := strings.ToLower(t.InfoHash().HexString())
			if tr := e.rateMap[h]; tr != nil {
				if tr.skippedFiles == nil {
					tr.skippedFiles = make(map[int]bool)
				}
				if priority == 0 {
					tr.skippedFiles[fileIndex] = true
				} else {
					delete(tr.skippedFiles, fileIndex)
				}
			}
			return nil
		}
	}
	return fmt.Errorf("torrent not found: %s", infoHashHex)
}

func (e *Engine) GetTorrentSavePath(infoHashHex string) (string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Check HTTP task
	e.httpManager.mu.RLock()
	task, exists := e.httpManager.tasks[infoHashHex]
	e.httpManager.mu.RUnlock()
	if exists {
		return task.DestPath, nil
	}

	// Check torrent
	torrents := e.client.Torrents()
	for _, t := range torrents {
		if strings.EqualFold(t.InfoHash().HexString(), infoHashHex) {
			return e.getTorrentSavePath(t), nil
		}
	}
	return "", fmt.Errorf("torrent not found: %s", infoHashHex)
}

func (e *Engine) getTorrentSavePath(t *torrent.Torrent) string {
	if t == nil {
		return e.cfg.DownloadDir
	}
	info := t.Info()
	if info != nil {
		if info.IsDir() {
			return filepath.Join(e.cfg.DownloadDir, info.Name)
		}
		if len(t.Files()) > 0 {
			return filepath.Join(e.cfg.DownloadDir, t.Files()[0].Path())
		}
	}
	name := t.Name()
	if name != "" {
		return filepath.Join(e.cfg.DownloadDir, name)
	}
	return e.cfg.DownloadDir
}

func (e *Engine) GetTorrentFilePath(infoHashHex string, fileIndex int) (string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Check HTTP task
	e.httpManager.mu.RLock()
	task, exists := e.httpManager.tasks[infoHashHex]
	e.httpManager.mu.RUnlock()
	if exists {
		if fileIndex != 0 {
			return "", fmt.Errorf("invalid file index %d for http download", fileIndex)
		}
		return task.DestPath, nil
	}

	// Check torrent
	torrents := e.client.Torrents()
	for _, t := range torrents {
		if strings.EqualFold(t.InfoHash().HexString(), infoHashHex) {
			files := t.Files()
			if fileIndex < 0 || fileIndex >= len(files) {
				return "", fmt.Errorf("file index %d out of bounds (total %d)", fileIndex, len(files))
			}
			return filepath.Join(e.cfg.DownloadDir, files[fileIndex].Path()), nil
		}
	}
	return "", fmt.Errorf("torrent not found: %s", infoHashHex)
}

func OpenPath(targetPath string) error {
	clean := filepath.Clean(targetPath)
	if _, err := os.Stat(clean); err != nil {
		if _, pErr := os.Stat(clean + ".part"); pErr == nil {
			clean = clean + ".part"
		}
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", clean)
	case "darwin":
		cmd = exec.Command("open", clean)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", clean)
	default:
		cmd = exec.Command("xdg-open", clean)
	}
	return cmd.Start()
}

func ShowInFolder(targetPath string) error {
	clean := filepath.Clean(targetPath)
	info, err := os.Stat(clean)
	if err != nil {
		if pInfo, pErr := os.Stat(clean + ".part"); pErr == nil {
			clean = clean + ".part"
			info = pInfo
		} else {
			dir := filepath.Dir(clean)
			if dInfo, dErr := os.Stat(dir); dErr == nil {
				clean = dir
				info = dInfo
			} else {
				return err
			}
		}
	}

	switch runtime.GOOS {
	case "linux":
		if !info.IsDir() {
			fileURI := "file://" + clean
			cmd := exec.Command("dbus-send", "--session", "--dest=org.freedesktop.FileManager1",
				"--type=method_call", "/org/freedesktop/FileManager1",
				"org.freedesktop.FileManager1.ShowItems", "array:string:"+fileURI, "string:\"\"")
			if err := cmd.Run(); err == nil {
				return nil
			}
		}
		dir := clean
		if !info.IsDir() {
			dir = filepath.Dir(clean)
		}
		return exec.Command("xdg-open", dir).Start()

	case "darwin":
		if info.IsDir() {
			return exec.Command("open", clean).Start()
		}
		return exec.Command("open", "-R", clean).Start()

	case "windows":
		if info.IsDir() {
			return exec.Command("explorer", clean).Start()
		}
		return exec.Command("explorer", "/select,", clean).Start()

	default:
		dir := clean
		if !info.IsDir() {
			dir = filepath.Dir(clean)
		}
		return exec.Command("xdg-open", dir).Start()
	}
}

func (e *Engine) GetGlobalStats() GlobalStats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var totalDL, totalUL int64
	var activeCount int

	for _, tr := range e.rateMap {
		totalDL += tr.downloadRate
		totalUL += tr.uploadRate
		if tr.downloadRate > 0 || tr.uploadRate > 0 {
			activeCount++
		}
	}

	// Add HTTP download rates to global stats
	e.httpManager.mu.RLock()
	for _, task := range e.httpManager.tasks {
		if task.State == "downloading" {
			totalDL += task.DownloadRate
			activeCount++
		}
	}
	e.httpManager.mu.RUnlock()

	dhtNodes := 0
	for _, dhtInstance := range e.client.DhtServers() {
		if st, ok := dhtInstance.Stats().(dht.ServerStats); ok {
			dhtNodes += st.GoodNodes
		}
	}

	indexedCount := 0
	if e.dhtIndexer != nil {
		indexedCount = e.dhtIndexer.Size()
	}

	return GlobalStats{
		DownloadRate:    totalDL,
		UploadRate:      totalUL,
		ActiveCount:     activeCount,
		TotalCount:      len(e.rateMap) + len(e.httpManager.tasks),
		DHTNodes:        dhtNodes,
		DHTIndexedCount: indexedCount,
	}
}

func (e *Engine) Close() {
	e.closeOnce.Do(func() {
		e.mu.Lock()
		e.saveSessionLocked()
		e.mu.Unlock()
		if e.dhtIndexer != nil {
			e.dhtIndexer.Close()
		}
		close(e.stopMonitor)
		e.client.Close()
		if e.pieceComp != nil {
			_ = e.pieceComp.Close()
		}
	})
}

type InspectResult struct {
	Name      string              `json:"name"`
	InfoHash  string              `json:"info_hash"`
	MagnetURI string              `json:"magnet_uri"`
	TotalSize int64               `json:"total_size"`
	NumFiles  int                 `json:"num_files"`
	Seeders   int                 `json:"seeders"`
	Leechers  int                 `json:"leechers"`
	Files     []TorrentFileDetail `json:"files"`
}

func extractInfoHash(input string) string {
	input = strings.TrimSpace(input)
	if len(input) == 40 {
		return input
	}
	lower := strings.ToLower(input)
	idx := strings.Index(lower, "urn:btih:")
	if idx != -1 {
		part := input[idx+9:]
		end := strings.IndexAny(part, ";&/")
		if end != -1 {
			part = part[:end]
		}
		return strings.TrimSpace(part)
	}
	return ""
}

// GetTorrentFileBytes returns the raw bencoded .torrent file data and recommended filename
func (e *Engine) GetTorrentFileBytes(infoHashHex string) ([]byte, string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	hash := strings.ToLower(infoHashHex)

	// 1. Check if cached .torrent file exists on disk
	filePath := getTorrentCacheFilePath(hash)
	if filePath != "" {
		if data, err := os.ReadFile(filePath); err == nil && len(data) > 0 {
			name := hash + ".torrent"
			for _, t := range e.client.Torrents() {
				if strings.EqualFold(t.InfoHash().HexString(), hash) {
					if t.Info() != nil && t.Info().Name != "" {
						name = t.Info().Name + ".torrent"
					} else if t.Name() != "" {
						name = t.Name() + ".torrent"
					}
					break
				}
			}
			return data, name, nil
		}
	}

	// 2. Generate on the fly from memory if torrent is active and has Info()
	for _, t := range e.client.Torrents() {
		if strings.EqualFold(t.InfoHash().HexString(), hash) {
			if t.Info() == nil {
				return nil, "", fmt.Errorf("metadata is still resolving for %s", hash)
			}
			mi := t.Metainfo()
			var buf bytes.Buffer
			if err := mi.Write(&buf); err != nil {
				return nil, "", err
			}
			name := t.Info().Name
			if name == "" {
				name = hash
			}
			return buf.Bytes(), name + ".torrent", nil
		}
	}

	return nil, "", fmt.Errorf("torrent file not found for hash: %s", hash)
}

// InspectMagnetMetadata resolves or retrieves the file list and metadata of any magnet link or infohash
func (e *Engine) InspectMagnetMetadata(ctx context.Context, uriOrHash string) (*InspectResult, error) {
	hash := extractInfoHash(uriOrHash)
	if hash == "" {
		return nil, fmt.Errorf("invalid magnet link or infohash")
	}
	hash = strings.ToLower(hash)

	// 1. Check if loaded in active torrents
	e.mu.RLock()
	for _, t := range e.client.Torrents() {
		if strings.EqualFold(t.InfoHash().HexString(), hash) {
			if info := t.Info(); info != nil {
				e.mu.RUnlock()
				var files []TorrentFileDetail
				for idx, f := range info.UpvertedFiles() {
					files = append(files, TorrentFileDetail{
						Index:  idx,
						Path:   f.DisplayPath(info),
						Length: f.Length,
					})
				}
				st := t.Stats()
				seeders := st.ConnectedSeeders
				if seeders == 0 && st.ActivePeers > 0 {
					seeders = st.ActivePeers
				}
				leechers := st.TotalPeers - seeders
				if leechers < 0 {
					leechers = 0
				}
				return &InspectResult{
					Name:      info.BestName(),
					InfoHash:  hash,
					MagnetURI: uriOrHash,
					TotalSize: t.Length(),
					NumFiles:  len(files),
					Seeders:   seeders,
					Leechers:  leechers,
					Files:     files,
				}, nil
			}
		}
	}
	e.mu.RUnlock()

	// 2. Check if cached .torrent metainfo exists on disk
	cachedPath := getTorrentCacheFilePath(hash)
	if cachedPath != "" {
		if mi, err := metainfo.LoadFromFile(cachedPath); err == nil && mi != nil {
			if info, err := mi.UnmarshalInfo(); err == nil {
				var files []TorrentFileDetail
				for idx, f := range info.UpvertedFiles() {
					files = append(files, TorrentFileDetail{
						Index:  idx,
						Path:   f.DisplayPath(&info),
						Length: f.Length,
					})
				}
				return &InspectResult{
					Name:      info.BestName(),
					InfoHash:  hash,
					MagnetURI: uriOrHash,
					TotalSize: info.TotalLength(),
					NumFiles:  len(files),
					Seeders:   -1,
					Leechers:  -1,
					Files:     files,
				}, nil
			}
		}
	}

	// 3. Check if cached in DHT Indexer
	if e.dhtIndexer != nil {
		if rec := e.dhtIndexer.GetRecord(hash); rec != nil && len(rec.Files) > 0 {
			var files []TorrentFileDetail
			for idx, f := range rec.Files {
				files = append(files, TorrentFileDetail{
					Index:  idx,
					Path:   f,
					Length: 0,
				})
			}
			return &InspectResult{
				Name:      rec.Name,
				InfoHash:  hash,
				MagnetURI: uriOrHash,
				TotalSize: rec.SizeBytes,
				NumFiles:  len(rec.Files),
				Seeders:   -1,
				Leechers:  -1,
				Files:     files,
			}, nil
		}
	}

	// 4. Resolve metadata live over BEP 9 DHT swarm and direct peers
	mag := uriOrHash
	if !strings.HasPrefix(mag, "magnet:?") {
		mag = fmt.Sprintf("magnet:?xt=urn:btih:%s", hash)
	}
	extractedPeers := ExtractPeersFromMagnet(mag)
	mag = SuperchargeMagnet(mag)

	t, err := e.client.AddMagnet(mag)
	if err != nil {
		return nil, err
	}
	t.DisallowDataDownload()

	t.AddTrackers(GetTier1TrackerList())
	peerInfos := ConvertToPeerInfos(extractedPeers)
	if len(peerInfos) > 0 {
		t.AddPeers(peerInfos)
	}

	defer func() {
		e.mu.RLock()
		_, isUserDl := e.rateMap[hash]
		e.mu.RUnlock()
		if !isUserDl {
			t.Drop()
		}
	}()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-t.GotInfo():
			info := t.Info()
			if info == nil {
				return nil, fmt.Errorf("failed to extract metadata")
			}
			e.saveTorrentMetainfo(t)
			var files []TorrentFileDetail
			var fileNames []string
			for idx, f := range info.UpvertedFiles() {
				displayPath := f.DisplayPath(info)
				fileNames = append(fileNames, displayPath)
				files = append(files, TorrentFileDetail{
					Index:  idx,
					Path:   displayPath,
					Length: f.Length,
				})
			}

			if e.dhtIndexer != nil {
				e.dhtIndexer.AddRecord(&dhtindex.DHTRecord{
					InfoHash:     hash,
					Name:         info.BestName(),
					SizeBytes:    t.Length(),
					NumFiles:     len(files),
					DiscoveredAt: time.Now().Unix(),
					Files:        fileNames,
				})
			}

			st := t.Stats()
			seeders := st.ConnectedSeeders
			if seeders == 0 && st.ActivePeers > 0 {
				seeders = st.ActivePeers
			}
			leechers := st.TotalPeers - seeders
			if leechers < 0 {
				leechers = 0
			}

			if e.dhtIndexer != nil {
				e.dhtIndexer.RecordSwarmActivity(hash, info.BestName(), seeders, leechers)
			}

			return &InspectResult{
				Name:      info.BestName(),
				InfoHash:  hash,
				MagnetURI: mag,
				TotalSize: t.Length(),
				NumFiles:  len(files),
				Seeders:   seeders,
				Leechers:  leechers,
				Files:     files,
			}, nil

		case <-ticker.C:
			if len(peerInfos) > 0 {
				t.AddPeers(peerInfos)
			}
			t.AddTrackers(GetTier1TrackerList())

		case <-ctx.Done():
			return nil, fmt.Errorf("metadata lookup timed out (swarm peers not responding)")
		}
	}
}

// ScrapeSwarm performs a fast 3-second live probe of a torrent swarm's seeders and peer count
func (e *Engine) ScrapeSwarm(ctx context.Context, uriOrHash string) (seeders int, leechers int, err error) {
	hash := extractInfoHash(uriOrHash)
	if hash == "" {
		return -1, -1, fmt.Errorf("invalid magnet link or infohash")
	}
	hash = strings.ToLower(hash)

	// Check if torrent already exists in engine
	e.mu.RLock()
	for _, t := range e.client.Torrents() {
		if strings.EqualFold(t.InfoHash().HexString(), hash) {
			st := t.Stats()
			e.mu.RUnlock()
			s := st.ConnectedSeeders
			if s == 0 && st.ActivePeers > 0 {
				s = st.ActivePeers
			}
			l := st.TotalPeers - s
			if l < 0 {
				l = 0
			}
			if e.dhtIndexer != nil {
				e.dhtIndexer.RecordSwarmActivity(hash, "", s, l)
			}
			return s, l, nil
		}
	}
	e.mu.RUnlock()

	mag := uriOrHash
	if !strings.HasPrefix(mag, "magnet:?") {
		mag = fmt.Sprintf("magnet:?xt=urn:btih:%s", hash)
	}
	extractedPeers := ExtractPeersFromMagnet(mag)
	mag = SuperchargeMagnet(mag)

	t, err := e.client.AddMagnet(mag)
	if err != nil {
		return -1, -1, err
	}
	t.DisallowDataDownload()
	t.AddTrackers(GetTier1TrackerList())
	peerInfos := ConvertToPeerInfos(extractedPeers)
	if len(peerInfos) > 0 {
		t.AddPeers(peerInfos)
	}

	defer func() {
		e.mu.RLock()
		_, isUserDl := e.rateMap[hash]
		e.mu.RUnlock()
		if !isUserDl {
			t.Drop()
		}
	}()

	select {
	case <-time.After(3 * time.Second):
		st := t.Stats()
		s := st.ConnectedSeeders
		if s == 0 && st.ActivePeers > 0 {
			s = st.ActivePeers
		}
		l := st.TotalPeers - s
		if l < 0 {
			l = 0
		}
		if e.dhtIndexer != nil {
			e.dhtIndexer.RecordSwarmActivity(hash, "", s, l)
		}
		return s, l, nil
	case <-ctx.Done():
		return -1, -1, ctx.Err()
	}
}
