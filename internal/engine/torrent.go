package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
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
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

type SavedTorrent struct {
	InfoHash  string   `json:"info_hash"`
	MagnetURI string   `json:"magnet_uri"`
	Name      string   `json:"name"`
	IsPaused  bool     `json:"is_paused"`
	AddedAt   int64    `json:"added_at"`
	WebSeeds  []string `json:"webseeds"`
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
	InfoHash       string   `json:"info_hash"`
	Name           string   `json:"name"`
	MagnetURI      string   `json:"magnet_uri"`
	TotalBytes     int64    `json:"total_bytes"`
	CompletedBytes int64    `json:"completed_bytes"`
	Progress       float64  `json:"progress"` // 0.0 to 100.0
	DownloadRate   int64    `json:"download_rate"` // bytes/sec
	UploadRate     int64    `json:"upload_rate"`   // bytes/sec
	ETASeconds     int64    `json:"eta_seconds"`
	State          string           `json:"state"` // "downloading", "seeding", "paused", "metadata", "completed"
	Seeders        int              `json:"seeders"`
	Peers          int              `json:"peers"`
	Files          []string         `json:"files,omitempty"`
	AddedAt        int64            `json:"added_at"`
	SuggestedSwarm *SwarmSuggestion `json:"suggested_swarm,omitempty"`
	WebSeeds       []string         `json:"webseeds,omitempty"`
}

type TorrentFileDetail struct {
	Index          int     `json:"index"`
	Path           string  `json:"path"`
	Length         int64   `json:"length"`
	BytesCompleted int64   `json:"bytes_completed"`
	Progress       float64 `json:"progress"`
	Priority       int     `json:"priority"` // 0: None, 1: Normal, 2: High
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
	State          string              `json:"state"`
	Files          []TorrentFileDetail `json:"files"`
	Peers          []PeerDetail        `json:"peers"`
	Trackers       []string            `json:"trackers"`
	WebSeeds       []string            `json:"webseeds"`
	CreatedBy      string              `json:"created_by"`
	Comment        string              `json:"comment"`
	SuggestedSwarm *SwarmSuggestion    `json:"suggested_swarm,omitempty"`
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
	lastBytesRead    int64
	lastBytesWritten int64
	lastTime         time.Time
	downloadRate     int64
	uploadRate       int64
	addedAt          int64
	isPaused         bool
	displayName      string
}

type Engine struct {
	mu          sync.RWMutex
	client      *torrent.Client
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

func NewEngine(cfg *config.Config) (*Engine, error) {
	if err := os.MkdirAll(cfg.DownloadDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create download directory: %w", err)
	}

	tConfig := torrent.NewDefaultClientConfig()
	tConfig.DataDir = cfg.DownloadDir
	tConfig.NoDHT = !cfg.EnableDHT
	tConfig.PeriodicallyAnnounceTorrentsToDht = true
	tConfig.ListenPort = cfg.ListenPort

	// 1/1 Gbps High-Throughput & Low-Latency Tuning
	tConfig.EstablishedConnsPerTorrent = 250
	tConfig.HalfOpenConnsPerTorrent = 60
	tConfig.TotalHalfOpenConns = 150
	tConfig.TorrentPeersHighWater = 2000
	tConfig.TorrentPeersLowWater = 200
	tConfig.HandshakesTimeout = 5 * time.Second
	tConfig.NominalDialTimeout = 5 * time.Second
	tConfig.MinDialTimeout = 1 * time.Second

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
		webseeds := e.webSeedsMap[hash]
		mag := fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", hash, url.QueryEscape(name))
		mag = AppendWebSeedsToMagnet(SuperchargeMagnet(mag), webseeds)

		list = append(list, SavedTorrent{
			InfoHash:  hash,
			MagnetURI: mag,
			Name:      name,
			IsPaused:  isPaused,
			AddedAt:   addedAt,
			WebSeeds:  webseeds,
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
		if item.MagnetURI == "" {
			continue
		}

		t, err := e.client.AddMagnet(item.MagnetURI)
		if err != nil {
			continue
		}

		hash := t.InfoHash().HexString()
		e.rateMap[hash] = &rateTracker{
			lastTime: time.Now(),
			addedAt:  item.AddedAt,
			isPaused: item.IsPaused,
		}

		if len(item.WebSeeds) > 0 {
			e.webSeedsMap[hash] = SanitizeWebSeeds(item.WebSeeds, false)
		}

		if !item.IsPaused {
			go func(tor *torrent.Torrent, seeds []string) {
				<-tor.GotInfo()
				if len(seeds) > 0 && tor.Info() != nil {
					clean := SanitizeWebSeeds(seeds, tor.Info().IsDir())
					if len(clean) > 0 {
						tor.AddWebSeeds(clean)
					}
				}
				tor.DownloadAll()
				e.mu.Lock()
				e.saveSessionLocked()
				e.mu.Unlock()
			}(t, item.WebSeeds)
		} else {
			t.DisallowDataDownload()
		}
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
					tracker = &rateTracker{
						lastBytesRead:    t.BytesCompleted(),
						lastBytesWritten: 0,
						lastTime:         now,
						addedAt:          time.Now().Unix(),
					}
					e.rateMap[hash] = tracker
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
				}

				tracker.lastBytesRead = stats.BytesReadData.Int64()
				tracker.lastBytesWritten = stats.BytesWrittenData.Int64()
				tracker.lastTime = now
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
		t.DownloadAll()
		e.mu.Lock()
		e.initTracker(t.InfoHash().HexString())
		e.saveSessionLocked()
		e.mu.Unlock()
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
		tor.DownloadAll()
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
		e.mu.Lock()
		e.saveSessionLocked()
		e.mu.Unlock()
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
	t.DownloadAll()
	e.initTracker(t.InfoHash().HexString())
	e.saveSessionLocked()
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

	e.initTracker(hash)
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
			tor.DownloadAll()
		}(t, cleanMirrors)

		mag := AppendWebSeedsToMagnet(SuperchargeMagnet(sugg.MagnetURI), cleanMirrors)
		return hash, mag, nil
	}

	return "", "", fmt.Errorf("could not find or verify swarm for %s", filename)
}

func (e *Engine) initTracker(hash string, displayName ...string) {
	name := ""
	if len(displayName) > 0 {
		name = displayName[0]
	}
	if tr, ok := e.rateMap[hash]; !ok {
		e.rateMap[hash] = &rateTracker{
			lastTime:    time.Now(),
			addedAt:     time.Now().Unix(),
			displayName: name,
		}
	} else if name != "" && tr.displayName == "" {
		tr.displayName = name
	}
}

func (e *Engine) VerifyTorrentData(infoHashHex string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	torrents := e.client.Torrents()
	for _, t := range torrents {
		if strings.EqualFold(t.InfoHash().HexString(), infoHashHex) {
			go func(tor *torrent.Torrent) {
				<-tor.GotInfo()
				_ = tor.VerifyData()
			}(t)
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
			t.AllowDataDownload()
			if t.Info() != nil {
				t.DownloadAll()
			} else {
				go func(tor *torrent.Torrent) {
					<-tor.GotInfo()
					tor.DownloadAll()
					e.mu.Lock()
					e.saveSessionLocked()
					e.mu.Unlock()
				}(t)
			}
			if tr, ok := e.rateMap[infoHashHex]; ok {
				tr.isPaused = false
			}
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
			if totalBytes > 0 {
				progress = (float64(completedBytes) / float64(totalBytes)) * 100.0
			}

			if isPaused {
				state = "paused"
			} else if completedBytes >= totalBytes && totalBytes > 0 {
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

		statuses = append(statuses, TorrentStatus{
			InfoHash:       hash,
			Name:           name,
			MagnetURI:      magURI,
			TotalBytes:     totalBytes,
			CompletedBytes: completedBytes,
			Progress:       progress,
			DownloadRate:   dlRate,
			UploadRate:     ulRate,
			ETASeconds:     eta,
			State:          state,
			Seeders:        stats.ConnectedSeeders + len(webConns),
			Peers:          stats.ActivePeers + len(webConns),
			Files:          files,
			AddedAt:        addedAt,
			WebSeeds:       webseeds,
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
		statuses = append(statuses, TorrentStatus{
			InfoHash:       task.ID,
			Name:           task.Name,
			MagnetURI:      task.URL,
			TotalBytes:     task.TotalBytes,
			CompletedBytes: task.CompletedBytes,
			Progress:       prog,
			DownloadRate:   task.DownloadRate,
			UploadRate:     0,
			ETASeconds:     task.ETASeconds,
			State:          task.State,
			Seeders:        len(task.Mirrors),
			Peers:          len(task.Mirrors),
			Files:          []string{task.Name},
			AddedAt:        task.AddedAt,
			SuggestedSwarm: task.SuggestedSwarm,
			WebSeeds:       task.Mirrors,
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

		return &TorrentDetails{
			InfoHash:       task.ID,
			Name:           task.Name,
			MagnetURI:      task.URL,
			TotalBytes:     task.TotalBytes,
			CompletedBytes: task.CompletedBytes,
			Progress:       prog,
			DownloadDir:    e.cfg.DownloadDir,
			State:          task.State,
			Files: []TorrentFileDetail{
				{
					Path:           task.Name,
					Length:         task.TotalBytes,
					BytesCompleted: task.CompletedBytes,
					Progress:       prog,
				},
			},
			Peers:          peerDetails,
			WebSeeds:       task.Mirrors,
			CreatedBy:      "Multi-Source HTTP Downloader",
			SuggestedSwarm: task.SuggestedSwarm,
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
					files = append(files, TorrentFileDetail{
						Index:          idx,
						Path:           tf.Path(),
						Length:         fLen,
						BytesCompleted: fComp,
						Progress:       fProg,
						Priority:       int(tf.Priority()),
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

			return &TorrentDetails{
				InfoHash:       hashHex,
				Name:           name,
				MagnetURI:      magURI,
				TotalBytes:     totalBytes,
				CompletedBytes: completedBytes,
				Progress:       progress,
				PieceLength:    pieceLength,
				NumPieces:      numPieces,
				DownloadDir:    e.cfg.DownloadDir,
				State:          t.String(),
				Files:          files,
				Peers:          peerDetails,
				Trackers:       trackers,
				WebSeeds:       webseeds,
				CreatedBy:      "Digwire P2P",
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
			return nil
		}
	}
	return fmt.Errorf("torrent not found: %s", infoHashHex)
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
	})
}

type InspectResult struct {
	Name      string              `json:"name"`
	InfoHash  string              `json:"info_hash"`
	MagnetURI string              `json:"magnet_uri"`
	TotalSize int64               `json:"total_size"`
	NumFiles  int                 `json:"num_files"`
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
				return &InspectResult{
					Name:      info.BestName(),
					InfoHash:  hash,
					MagnetURI: uriOrHash,
					TotalSize: t.Length(),
					NumFiles:  len(files),
					Files:     files,
				}, nil
			}
		}
	}
	e.mu.RUnlock()

	// 2. Check if cached in DHT Indexer
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
				Files:     files,
			}, nil
		}
	}

	// 3. Resolve metadata live over BEP 9 DHT swarm and direct peers
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

			return &InspectResult{
				Name:      info.BestName(),
				InfoHash:  hash,
				MagnetURI: mag,
				TotalSize: t.Length(),
				NumFiles:  len(files),
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
