package dhtindex

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	_ "modernc.org/sqlite"
)

type DHTRecord struct {
	InfoHash     string         `json:"info_hash"`
	Name         string         `json:"name"`
	SizeBytes    int64          `json:"size_bytes"`
	NumFiles     int            `json:"num_files"`
	DiscoveredAt int64          `json:"discovered_at"`
	Files        []string       `json:"files,omitempty"`
	Activity     *SwarmActivity `json:"activity,omitempty"`
}

type Indexer struct {
	mu           sync.RWMutex
	records      map[string]*DHTRecord
	tokenIndex   map[string]map[string]bool // token -> set of infohashes
	db           *sql.DB
	crawlQueue   chan string
	seenCrawl    map[string]bool
	client       *torrent.Client
	stopChan     chan struct{}
	closeOnce    sync.Once
	isPreseeding bool
}

var tokenRegex = regexp.MustCompile(`[a-zA-Z0-9\.\-]+`)

func tokenize(s string) []string {
	matches := tokenRegex.FindAllString(strings.ToLower(s), -1)
	var tokens []string
	seen := make(map[string]bool)
	for _, m := range matches {
		m = strings.Trim(m, ".-_")
		if len(m) >= 2 && !seen[m] {
			seen[m] = true
			tokens = append(tokens, m)
		}
	}
	return tokens
}

func NewIndexer(client *torrent.Client) (*Indexer, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = "."
	}
	appDir := filepath.Join(configDir, "digwire")
	_ = os.MkdirAll(appDir, 0755)

	sqlitePath := filepath.Join(appDir, "dht_index.sqlite")
	db, err := sql.Open("sqlite", sqlitePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite index: %w", err)
	}

	// Optimize SQLite for concurrent DHT indexing
	_, _ = db.Exec("PRAGMA journal_mode = WAL;")
	_, _ = db.Exec("PRAGMA busy_timeout = 5000;")
	_, _ = db.Exec("PRAGMA synchronous = NORMAL;")

	schema := `
	CREATE TABLE IF NOT EXISTS dht_records (
		info_hash TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		size_bytes INTEGER DEFAULT 0,
		num_files INTEGER DEFAULT 0,
		discovered_at INTEGER NOT NULL,
		files_json TEXT DEFAULT '[]',
		activity_json TEXT DEFAULT '{}',
		last_seeders INTEGER DEFAULT 0,
		last_peers INTEGER DEFAULT 0,
		last_seen_healthy INTEGER DEFAULT 0
	);
	CREATE INDEX IF NOT EXISTS idx_dht_name ON dht_records(name);
	CREATE INDEX IF NOT EXISTS idx_dht_discovered ON dht_records(discovered_at DESC);
	`
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to initialize sqlite schema: %w", err)
	}

	idx := &Indexer{
		records:    make(map[string]*DHTRecord),
		tokenIndex: make(map[string]map[string]bool),
		db:         db,
		crawlQueue: make(chan string, 10000),
		seenCrawl:  make(map[string]bool),
		client:     client,
		stopChan:   make(chan struct{}),
	}

	// Auto-migrate legacy dht_index.jsonl if present
	legacyJSONL := filepath.Join(appDir, "dht_index.jsonl")
	if _, err := os.Stat(legacyJSONL); err == nil {
		idx.migrateJSONL(legacyJSONL)
		_ = os.Rename(legacyJSONL, legacyJSONL+".migrated")
	}

	idx.loadFromSQLite()

	// Launch concurrent crawler worker pool (8 workers)
	for i := 0; i < 8; i++ {
		go idx.crawlerWorker()
	}

	// If the database has fewer than 100 records, launch background preseed from open database
	if len(idx.records) < 100 {
		go func() {
			time.Sleep(2 * time.Second)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			_, _ = idx.PreseedFromTorrentsCSV(ctx)
		}()
	}

	return idx, nil
}

func (idx *Indexer) migrateJSONL(jsonlPath string) {
	file, err := os.Open(jsonlPath)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	tx, err := idx.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
	INSERT INTO dht_records (
		info_hash, name, size_bytes, num_files, discovered_at, files_json, activity_json,
		last_seeders, last_peers, last_seen_healthy
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(info_hash) DO UPDATE SET
		name = CASE WHEN excluded.name != '' THEN excluded.name ELSE dht_records.name END,
		size_bytes = CASE WHEN excluded.size_bytes > 0 THEN excluded.size_bytes ELSE dht_records.size_bytes END,
		num_files = CASE WHEN excluded.num_files > 0 THEN excluded.num_files ELSE dht_records.num_files END,
		files_json = CASE WHEN excluded.files_json != '[]' THEN excluded.files_json ELSE dht_records.files_json END,
		activity_json = excluded.activity_json,
		last_seeders = excluded.last_seeders,
		last_peers = excluded.last_peers,
		last_seen_healthy = CASE WHEN excluded.last_seen_healthy > 0 THEN excluded.last_seen_healthy ELSE dht_records.last_seen_healthy END;
	`)
	if err != nil {
		return
	}
	defer stmt.Close()

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec DHTRecord
		if err := json.Unmarshal(line, &rec); err == nil && rec.InfoHash != "" {
			filesJSON, _ := json.Marshal(rec.Files)
			activityJSON, _ := json.Marshal(rec.Activity)
			var lastSeeders, lastPeers int
			var lastSeenHealthy int64
			if rec.Activity != nil {
				lastSeeders = rec.Activity.LastSeeders
				lastPeers = rec.Activity.LastPeers
				lastSeenHealthy = rec.Activity.LastSeenHealthy
			}
			_, _ = stmt.Exec(
				strings.ToLower(rec.InfoHash), rec.Name, rec.SizeBytes, rec.NumFiles, rec.DiscoveredAt,
				string(filesJSON), string(activityJSON), lastSeeders, lastPeers, lastSeenHealthy,
			)
		}
	}
	_ = tx.Commit()
}

func (idx *Indexer) loadFromSQLite() {
	if idx.db == nil {
		return
	}
	rows, err := idx.db.Query(`
		SELECT info_hash, name, size_bytes, num_files, discovered_at, files_json, activity_json
		FROM dht_records
	`)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var rec DHTRecord
		var filesStr, actStr string
		if err := rows.Scan(&rec.InfoHash, &rec.Name, &rec.SizeBytes, &rec.NumFiles, &rec.DiscoveredAt, &filesStr, &actStr); err != nil {
			continue
		}
		if filesStr != "" && filesStr != "null" {
			_ = json.Unmarshal([]byte(filesStr), &rec.Files)
		}
		if actStr != "" && actStr != "null" && actStr != "{}" {
			_ = json.Unmarshal([]byte(actStr), &rec.Activity)
		}
		idx.addRecordInMemory(&rec)
	}
}

func (idx *Indexer) addRecordInMemory(rec *DHTRecord) {
	rec.InfoHash = strings.ToLower(rec.InfoHash)
	idx.records[rec.InfoHash] = rec

	tokens := tokenize(rec.Name)
	for _, f := range rec.Files {
		tokens = append(tokens, tokenize(f)...)
	}

	for _, tok := range tokens {
		if idx.tokenIndex[tok] == nil {
			idx.tokenIndex[tok] = make(map[string]bool)
		}
		idx.tokenIndex[tok][rec.InfoHash] = true
	}
}

// GetRecord returns a cached DHT record by infohash if present
func (idx *Indexer) GetRecord(infoHashHex string) *DHTRecord {
	if idx == nil {
		return nil
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.records[strings.ToLower(strings.TrimSpace(infoHashHex))]
}

// GetHealthPrediction evaluates historical swarm health for an infohash
func (idx *Indexer) GetHealthPrediction(infoHashHex string) *HealthPrediction {
	if idx == nil {
		return nil
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	rec := idx.records[strings.ToLower(strings.TrimSpace(infoHashHex))]
	if rec == nil || rec.Activity == nil {
		return nil
	}
	return rec.Activity.PredictHealth()
}

// RecordSwarmActivity logs a temporal swarm probe/presence sample into the DHT record
func (idx *Indexer) RecordSwarmActivity(infoHashHex string, name string, seeders int, peers int) {
	if idx == nil {
		return
	}
	infoHashHex = strings.ToLower(strings.TrimSpace(infoHashHex))
	if len(infoHashHex) != 40 {
		return
	}

	idx.mu.Lock()
	rec := idx.records[infoHashHex]
	if rec == nil {
		rec = &DHTRecord{
			InfoHash:     infoHashHex,
			Name:         name,
			DiscoveredAt: time.Now().Unix(),
			Activity:     &SwarmActivity{},
		}
		idx.addRecordInMemory(rec)
	}
	if rec.Activity == nil {
		rec.Activity = &SwarmActivity{}
	}
	if name != "" && rec.Name == "" {
		rec.Name = name
	}
	rec.Activity.RecordSample(seeders, peers)
	idx.mu.Unlock()

	idx.saveRecordToSQLite(rec)
}

// AddRecord adds or updates a record and persists to disk
func (idx *Indexer) AddRecord(rec *DHTRecord) {
	if rec == nil || rec.InfoHash == "" || rec.Name == "" {
		return
	}
	rec.InfoHash = strings.ToLower(rec.InfoHash)
	if rec.DiscoveredAt == 0 {
		rec.DiscoveredAt = time.Now().Unix()
	}

	idx.mu.Lock()
	existing := idx.records[rec.InfoHash]
	if existing != nil {
		if rec.Activity != nil && existing.Activity == nil {
			existing.Activity = rec.Activity
		}
		idx.mu.Unlock()
		return
	}
	idx.addRecordInMemory(rec)
	idx.mu.Unlock()

	idx.saveRecordToSQLite(rec)
}

func (idx *Indexer) saveRecordToSQLite(rec *DHTRecord) {
	if idx.db == nil || rec == nil {
		return
	}
	filesJSON, _ := json.Marshal(rec.Files)
	activityJSON, _ := json.Marshal(rec.Activity)

	var lastSeeders, lastPeers int
	var lastSeenHealthy int64
	if rec.Activity != nil {
		lastSeeders = rec.Activity.LastSeeders
		lastPeers = rec.Activity.LastPeers
		lastSeenHealthy = rec.Activity.LastSeenHealthy
	}

	query := `
	INSERT INTO dht_records (
		info_hash, name, size_bytes, num_files, discovered_at, files_json, activity_json,
		last_seeders, last_peers, last_seen_healthy
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(info_hash) DO UPDATE SET
		name = CASE WHEN excluded.name != '' THEN excluded.name ELSE dht_records.name END,
		size_bytes = CASE WHEN excluded.size_bytes > 0 THEN excluded.size_bytes ELSE dht_records.size_bytes END,
		num_files = CASE WHEN excluded.num_files > 0 THEN excluded.num_files ELSE dht_records.num_files END,
		files_json = CASE WHEN excluded.files_json != '[]' THEN excluded.files_json ELSE dht_records.files_json END,
		activity_json = excluded.activity_json,
		last_seeders = excluded.last_seeders,
		last_peers = excluded.last_peers,
		last_seen_healthy = CASE WHEN excluded.last_seen_healthy > 0 THEN excluded.last_seen_healthy ELSE dht_records.last_seen_healthy END;
	`
	_, _ = idx.db.Exec(query,
		rec.InfoHash, rec.Name, rec.SizeBytes, rec.NumFiles, rec.DiscoveredAt,
		string(filesJSON), string(activityJSON), lastSeeders, lastPeers, lastSeenHealthy,
	)
}

// QueueCrawl adds an infohash to the crawler worker queue
func (idx *Indexer) QueueCrawl(infoHashHex string) {
	infoHashHex = strings.ToLower(strings.TrimSpace(infoHashHex))
	if len(infoHashHex) != 40 {
		return
	}

	idx.mu.Lock()
	if idx.seenCrawl[infoHashHex] || idx.records[infoHashHex] != nil {
		idx.mu.Unlock()
		return
	}
	idx.seenCrawl[infoHashHex] = true
	idx.mu.Unlock()

	select {
	case idx.crawlQueue <- infoHashHex:
	default:
	}
}

// crawlerWorker processes infohashes and resolves BEP 9 metadata concurrently
func (idx *Indexer) crawlerWorker() {
	for {
		select {
		case <-idx.stopChan:
			return
		case hashHex := <-idx.crawlQueue:
			if idx.client == nil {
				time.Sleep(1 * time.Second)
				continue
			}

			mag := fmt.Sprintf("magnet:?xt=urn:btih:%s&tr=udp%%3A%%2F%%2Ftracker.opentrackr.org%%3A1337%%2Fannounce&tr=udp%%3A%%2F%%2Fopen.stealth.si%%3A80%%2Fannounce&tr=http%%3A%%2F%%2Ftorrent.ubuntu.com%%3A6969%%2Fannounce", hashHex)
			t, err := idx.client.AddMagnet(mag)
			if err != nil {
				continue
			}
			t.DisallowDataDownload()

			select {
			case <-t.GotInfo():
				info := t.Info()
				if info != nil {
					// Cache .torrent file to disk for instant zero-latency retrieval
					configDir, _ := os.UserConfigDir()
					if configDir != "" {
						tDir := filepath.Join(configDir, "digwire", "torrents")
						_ = os.MkdirAll(tDir, 0755)
						tPath := filepath.Join(tDir, strings.ToLower(hashHex)+".torrent")
						if f, err := os.Create(tPath); err == nil {
							mi := t.Metainfo()
							_ = mi.Write(f)
							f.Close()
						}
					}

					var fileNames []string
					for _, f := range info.UpvertedFiles() {
						fileNames = append(fileNames, f.DisplayPath(info))
					}
					idx.AddRecord(&DHTRecord{
						InfoHash:     hashHex,
						Name:         info.BestName(),
						SizeBytes:    t.Length(),
						NumFiles:     len(fileNames),
						DiscoveredAt: time.Now().Unix(),
						Files:        fileNames,
					})
				}
				t.Drop()
			case <-time.After(5 * time.Second):
				t.Drop()
			case <-idx.stopChan:
				t.Drop()
				return
			}
		}
	}
}

// PreseedFromTorrentsCSV pre-fetches verified torrents from open indexes directly into local DHT cache
func (idx *Indexer) PreseedFromTorrentsCSV(ctx context.Context) (int, error) {
	idx.mu.Lock()
	if idx.isPreseeding {
		idx.mu.Unlock()
		return 0, nil
	}
	idx.isPreseeding = true
	idx.mu.Unlock()

	defer func() {
		idx.mu.Lock()
		idx.isPreseeding = false
		idx.mu.Unlock()
	}()

	client := &http.Client{Timeout: 12 * time.Second}

	seedQueries := []string{
		"linux", "ubuntu", "arch", "debian", "fedora", "tails",
		"2026", "2025", "2024", "1080p", "2160p", "4k", "flac",
		"music", "iso", "software", "documentary", "audiobook",
		"remaster", "bluray", "complete", "season", "pack",
	}

	imported := 0
	for _, q := range seedQueries {
		select {
		case <-ctx.Done():
			return imported, ctx.Err()
		case <-idx.stopChan:
			return imported, nil
		default:
		}

		searchURL := fmt.Sprintf("https://torrents-csv.com/service/search?q=%s&size=100", url.QueryEscape(q))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0")
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			continue
		}

		var data struct {
			Torrents []struct {
				Name      string `json:"name"`
				InfoHash  string `json:"infohash"`
				SizeBytes int64  `json:"size_bytes"`
			} `json:"torrents"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&data); err == nil {
			for _, item := range data.Torrents {
				if item.Name != "" && item.InfoHash != "" {
					idx.AddRecord(&DHTRecord{
						InfoHash:     strings.ToLower(item.InfoHash),
						Name:         item.Name,
						SizeBytes:    item.SizeBytes,
						NumFiles:     1,
						DiscoveredAt: time.Now().Unix(),
					})
					imported++
				}
			}
		}
		resp.Body.Close()
		time.Sleep(120 * time.Millisecond)
	}

	return imported, nil
}

// Search performs instant multi-token substring search on the local index
func (idx *Indexer) Search(query string) []*DHTRecord {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	qTokens := tokenize(query)
	if len(qTokens) == 0 {
		return nil
	}

	matchCounts := make(map[string]int)

	for _, tok := range qTokens {
		for indexedTok, hashSet := range idx.tokenIndex {
			if strings.Contains(indexedTok, tok) || strings.Contains(tok, indexedTok) {
				for h := range hashSet {
					matchCounts[h]++
				}
			}
		}
	}

	var results []*DHTRecord
	for h, count := range matchCounts {
		if count > 0 {
			if rec, ok := idx.records[h]; ok {
				results = append(results, rec)
			}
		}
	}

	return results
}

// Size returns total indexed torrents count
func (idx *Indexer) Size() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.records)
}

func (idx *Indexer) Close() {
	idx.closeOnce.Do(func() {
		close(idx.stopChan)
		if idx.db != nil {
			_ = idx.db.Close()
		}
	})
}
