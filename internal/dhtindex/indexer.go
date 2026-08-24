package dhtindex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
)

type DHTRecord struct {
	InfoHash     string   `json:"info_hash"`
	Name         string   `json:"name"`
	SizeBytes    int64    `json:"size_bytes"`
	NumFiles     int      `json:"num_files"`
	DiscoveredAt int64    `json:"discovered_at"`
	Files        []string `json:"files,omitempty"`
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
		crawlQueue: make(chan string, 5000),
		seenCrawl:  make(map[string]bool),
		client:     client,
		stopChan:   make(chan struct{}),
	}

	idx.loadFromDisk()
	go idx.crawlerLoop()

	return idx, nil
}

func (idx *Indexer) loadFromDisk() {
	file, err := os.Open(idx.filePath)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// Support large lines
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
	if _, exists := idx.records[rec.InfoHash]; exists {
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

// QueueCrawl adds an infohash to the passive crawler queue
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
		// Queue full, drop
	}
}

// crawlerLoop pulls infohashes from queue and resolves BEP 9 metadata
func (idx *Indexer) crawlerLoop() {
	for {
		select {
		case <-idx.stopChan:
			return
		case hashHex := <-idx.crawlQueue:
			if idx.client == nil {
				time.Sleep(1 * time.Second)
				continue
			}

			// Add magnet tentatively with timeout
			mag := fmt.Sprintf("magnet:?xt=urn:btih:%s&tr=udp%%3A%%2F%%2Ftracker.opentrackr.org%%3A1337%%2Fannounce&tr=http%%3A%%2F%%2Ftorrent.ubuntu.com%%3A6969%%2Fannounce", hashHex)
			t, err := idx.client.AddMagnet(mag)
			if err != nil {
				continue
			}

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
			case <-time.After(6 * time.Second):
				t.Drop()
			case <-idx.stopChan:
				t.Drop()
				return
			}

			// Rate limit crawler
			time.Sleep(150 * time.Millisecond)
		}
	}
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
