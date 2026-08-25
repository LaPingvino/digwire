package dhtindex

import (
	"bufio"
	"context"
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
	filePath     string
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
	dbPath := filepath.Join(appDir, "dht_index.jsonl")

	idx := &Indexer{
		records:    make(map[string]*DHTRecord),
		tokenIndex: make(map[string]map[string]bool),
		filePath:   dbPath,
		crawlQueue: make(chan string, 10000),
		seenCrawl:  make(map[string]bool),
		client:     client,
		stopChan:   make(chan struct{}),
	}

	idx.loadFromDisk()

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

func (idx *Indexer) loadFromDisk() {
	file, err := os.Open(idx.filePath)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec DHTRecord
		if err := json.Unmarshal(line, &rec); err == nil && rec.InfoHash != "" {
			idx.addRecordInMemory(&rec)
		}
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

	// Append updated record to disk
	data, err := json.Marshal(rec)
	if err == nil {
		f, err := os.OpenFile(idx.filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			_, _ = f.Write(append(data, '\n'))
			_ = f.Close()
		}
	}
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

	// Append to disk file
	data, err := json.Marshal(rec)
	if err == nil {
		f, err := os.OpenFile(idx.filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			_, _ = f.Write(append(data, '\n'))
			_ = f.Close()
		}
	}
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
	})
}
