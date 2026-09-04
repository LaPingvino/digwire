package dhtindex

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func FuzzTokenize(f *testing.F) {
	seeds := []string{
		"Ubuntu 24.04.1 Desktop amd64.iso",
		"Debian-12.5.0-netinst",
		"Arch.Linux.2026.09.01.x86_64",
		"Special!@#$%^&*()_+=-~`'\"/\\?><.,{}[]|",
		"",
		" ",
		"a",
		"ab",
		"123.456-789_000",
		"Русский текст и фильм 2026",
		"日本語のファイル名テスト",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		tokens := tokenize(input)
		for _, tok := range tokens {
			if len(tok) < 2 {
				t.Errorf("expected token length >= 2, got %q (length %d)", tok, len(tok))
			}
		}
	})
}

func FuzzSwarmActivity(f *testing.F) {
	f.Add(0, 0, int64(1700000000))
	f.Add(100, 50, int64(1725450000))
	f.Add(-1, -5, int64(0))
	f.Add(999999, 999999, int64(2000000000))
	f.Add(1, 0, int64(1000000))

	f.Fuzz(func(t *testing.T, seeders int, peers int, timestampSec int64) {
		act := &SwarmActivity{}
		tm := time.Unix(timestampSec, 0)
		act.RecordSample(seeders, peers, tm)

		cycle := act.UptimeDutyCycle()
		if cycle < 0.0 || cycle > 1.0 {
			t.Errorf("duty cycle out of range [0, 1]: %f", cycle)
		}

		pred := act.PredictHealth()
		if pred == nil {
			t.Errorf("health prediction must not be nil")
		}
		if pred.Confidence < 0 || pred.Confidence > 100 {
			t.Errorf("confidence out of range [0, 100]: %d", pred.Confidence)
		}
	})
}

func FuzzSearch(f *testing.F) {
	tempDir, err := os.MkdirTemp("", "digwire_fuzz_*")
	if err != nil {
		f.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	db, err := sql.Open("sqlite", filepath.Join(tempDir, "fuzz_index.sqlite"))
	if err != nil {
		f.Fatalf("failed to open sqlite: %v", err)
	}
	defer db.Close()

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
	`
	if _, err := db.Exec(schema); err != nil {
		f.Fatalf("failed to create schema: %v", err)
	}

	idx := &Indexer{
		cache:      make(map[string]*DHTRecord),
		db:         db,
		crawlQueue: make(chan string, 10),
		seenCrawl:  make(map[string]bool),
		stopChan:   make(chan struct{}),
	}
	defer idx.Close()

	idx.AddRecord(&DHTRecord{
		InfoHash:     "0123456789abcdef0123456789abcdef01234567",
		Name:         "Ubuntu 24.04 Linux ISO",
		SizeBytes:    4000000000,
		NumFiles:     1,
		DiscoveredAt: time.Now().Unix(),
		Files:        []string{"ubuntu.iso"},
	})

	f.Add("ubuntu")
	f.Add("' OR '1'='1")
	f.Add("'; DROP TABLE dht_records; --")
	f.Add("%")
	f.Add("_")
	f.Add("\\")
	f.Add("linux 2026")
	f.Add(string([]byte{0x00, 0xff, 0xfe, 0xfd}))

	f.Fuzz(func(t *testing.T, query string) {
		// Verify search never panics on arbitrary string input
		_ = idx.Search(query)
	})
}
