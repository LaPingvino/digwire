package dhtindex

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func createTestIndexer(t *testing.T) (*Indexer, string) {
	tempDir, err := os.MkdirTemp("", "digwire_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	sqlitePath := filepath.Join(tempDir, "dht_index.sqlite")
	db, err := sql.Open("sqlite", sqlitePath)
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}

	if err := initSchema(db); err != nil {
		t.Fatalf("failed to initialize schema: %v", err)
	}

	idx := &Indexer{
		cache:      make(map[string]*DHTRecord),
		db:         db,
		crawlQueue: make(chan string, 100),
		seenCrawl:  make(map[string]bool),
		stopChan:   make(chan struct{}),
	}
	return idx, tempDir
}

func TestIndexerAddAndGet(t *testing.T) {
	idx, tmp := createTestIndexer(t)
	defer os.RemoveAll(tmp)
	defer idx.Close()

	hash := strings.Repeat("a", 40)
	rec := &DHTRecord{
		InfoHash:     hash,
		Name:         "Ubuntu 24.04 Desktop AMD64",
		SizeBytes:    5000000000,
		NumFiles:     1,
		DiscoveredAt: time.Now().Unix(),
		Files:        []string{"ubuntu-24.04-desktop-amd64.iso"},
	}

	idx.AddRecord(rec)

	// Fetch from cache
	got := idx.GetRecord(hash)
	if got == nil {
		t.Fatalf("expected record in cache, got nil")
	}
	if got.Name != "Ubuntu 24.04 Desktop AMD64" {
		t.Fatalf("expected name match, got %s", got.Name)
	}

	// Clear cache to verify SQLite fallback
	idx.mu.Lock()
	idx.cache = make(map[string]*DHTRecord)
	idx.mu.Unlock()

	gotDB := idx.GetRecord(hash)
	if gotDB == nil {
		t.Fatalf("expected record from SQLite, got nil")
	}
	if gotDB.Name != "Ubuntu 24.04 Desktop AMD64" {
		t.Fatalf("expected name match from SQLite, got %s", gotDB.Name)
	}
	if len(gotDB.Files) != 1 || gotDB.Files[0] != "ubuntu-24.04-desktop-amd64.iso" {
		t.Fatalf("expected files preserved, got %v", gotDB.Files)
	}
}

func TestIndexerSearch(t *testing.T) {
	idx, tmp := createTestIndexer(t)
	defer os.RemoveAll(tmp)
	defer idx.Close()

	hash1 := strings.Repeat("1", 40)
	hash2 := strings.Repeat("2", 40)

	idx.AddRecord(&DHTRecord{
		InfoHash:     hash1,
		Name:         "Arch Linux 2026.09.01 x86_64",
		SizeBytes:    1200000000,
		NumFiles:     1,
		DiscoveredAt: time.Now().Unix(),
		Files:        []string{"archlinux-2026.09.01-x86_64.iso"},
	})

	idx.AddRecord(&DHTRecord{
		InfoHash:     hash2,
		Name:         "Debian 12 Bookworm Netinst",
		SizeBytes:    600000000,
		NumFiles:     1,
		DiscoveredAt: time.Now().Unix(),
		Files:        []string{"debian-12-netinst.iso"},
	})

	results := idx.Search("arch linux")
	if len(results) != 1 {
		t.Fatalf("expected 1 result for 'arch linux', got %d", len(results))
	}
	if results[0].InfoHash != hash1 {
		t.Fatalf("expected hash1, got %s", results[0].InfoHash)
	}

	resultsDeb := idx.Search("debian")
	if len(resultsDeb) != 1 {
		t.Fatalf("expected 1 result for 'debian', got %d", len(resultsDeb))
	}
	if resultsDeb[0].InfoHash != hash2 {
		t.Fatalf("expected hash2, got %s", resultsDeb[0].InfoHash)
	}
}

func TestSwarmActivityPrediction(t *testing.T) {
	idx, tmp := createTestIndexer(t)
	defer os.RemoveAll(tmp)
	defer idx.Close()

	hash := strings.Repeat("c", 40)
	idx.RecordSwarmActivity(hash, "Popular Open Source Movie", 50, 10)

	pred := idx.GetHealthPrediction(hash)
	if pred == nil {
		t.Fatalf("expected prediction, got nil")
	}
	if pred.Status != "active" {
		t.Fatalf("expected active status, got %s", pred.Status)
	}
}

func TestUserTorrentDropProtection(t *testing.T) {
	idx, tmp := createTestIndexer(t)
	defer os.RemoveAll(tmp)
	defer idx.Close()

	userHash := strings.Repeat("f", 40)
	idx.SetUserTorrentChecker(func(h string) bool {
		return strings.EqualFold(h, userHash)
	})

	// User torrent should not be queued for crawler
	idx.QueueCrawl(userHash)
	if len(idx.crawlQueue) != 0 {
		t.Fatalf("expected user torrent not to be queued for crawling")
	}
}

func TestBEP52StorageAndPiecesRootSearch(t *testing.T) {
	idx, tmp := createTestIndexer(t)
	defer os.RemoveAll(tmp)
	defer idx.Close()

	v1Hash := strings.Repeat("c", 40)
	v2Hash := strings.Repeat("d", 64)
	piecesRoot := strings.Repeat("e", 64)

	rec := &DHTRecord{
		InfoHash:        v1Hash,
		InfoHashV2:      v2Hash,
		ProtocolVersion: "hybrid",
		Name:            "BEP52 Hybrid Album",
		SizeBytes:       250000000,
		NumFiles:        1,
		DiscoveredAt:    time.Now().Unix(),
		Files:           []string{"01 - Title Track.flac"},
		PiecesRoots:     []string{piecesRoot},
		FileEntries: []DHTFileEntry{
			{
				Path:       "01 - Title Track.flac",
				SizeBytes:  250000000,
				PiecesRoot: piecesRoot,
			},
		},
	}

	idx.AddRecord(rec)

	// Fetch via v1 hash
	byV1 := idx.GetRecord(v1Hash)
	if byV1 == nil || byV1.InfoHashV2 != v2Hash || byV1.ProtocolVersion != "hybrid" {
		t.Fatalf("expected hybrid record by v1 hash, got %+v", byV1)
	}

	// Fetch via v2 hash
	byV2 := idx.GetRecord(v2Hash)
	if byV2 == nil || byV2.InfoHash != v1Hash {
		t.Fatalf("expected hybrid record by v2 hash, got %+v", byV2)
	}

	// Search by pieces_root directly
	rootMatches := idx.SearchByPiecesRoot(piecesRoot)
	if len(rootMatches) != 1 || rootMatches[0].InfoHash != v1Hash {
		t.Fatalf("expected 1 match for pieces_root, got %d", len(rootMatches))
	}

	// General search with 64-char pieces root
	genMatches := idx.Search(piecesRoot)
	if len(genMatches) != 1 || genMatches[0].InfoHash != v1Hash {
		t.Fatalf("expected 1 match for search(piecesRoot), got %d", len(genMatches))
	}
}

// Databases created before BEP 52 support lack the v2 columns; opening one must migrate it rather
// than fail (which silently disabled the whole index).
func TestInitSchemaMigratesPreBEP52Database(t *testing.T) {
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "dht_index.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
	CREATE TABLE dht_records (
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
	INSERT INTO dht_records (info_hash, name, discovered_at) VALUES ('0123456789abcdef0123456789abcdef01234567', 'old', 1);
	`); err != nil {
		t.Fatal(err)
	}

	if err := initSchema(db); err != nil {
		t.Fatalf("initSchema on pre-BEP 52 database: %v", err)
	}
	var proto string
	if err := db.QueryRow(`SELECT protocol_version FROM dht_records`).Scan(&proto); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO dht_files (info_hash, file_path) VALUES ('x', 'y')`); err != nil {
		t.Fatalf("dht_files table missing after migration: %v", err)
	}
}

