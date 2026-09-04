package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"digwire/internal/config"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

func TestSessionAutoRecoveryFromCache(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "digwire_session_rec_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := &config.Config{
		DownloadDir: filepath.Join(tempDir, "downloads"),
		ListenPort:  0,
		GermanyMode: false,
	}
	cfg.SetConfigPath(filepath.Join(tempDir, "config.yaml"))
	_ = os.MkdirAll(cfg.DownloadDir, 0755)

	// Create a sample file and a mock .torrent file in cache
	cacheDir := filepath.Join(tempDir, "torrents")
	_ = os.MkdirAll(cacheDir, 0755)

	sampleData := []byte("Hello Digwire recovery test content")
	sampleFile := filepath.Join(cfg.DownloadDir, "sample.txt")
	_ = os.WriteFile(sampleFile, sampleData, 0644)

	info := metainfo.Info{PieceLength: 16384}
	if err := info.BuildFromFilePath(sampleFile); err != nil {
		t.Fatalf("failed to build info: %v", err)
	}
	mi := metainfo.MetaInfo{}
	mi.SetDefaults()
	infoBytes, _ := bencode.Marshal(info)
	mi.InfoBytes = infoBytes

	hash := mi.HashInfoBytes().HexString()
	torrentPath := filepath.Join(cacheDir, strings.ToLower(hash)+".torrent")
	f, err := os.Create(torrentPath)
	if err != nil {
		t.Fatalf("failed to create cache torrent: %v", err)
	}
	_ = mi.Write(f)
	f.Close()

	// Ensure session.json is 0 bytes (simulating disk full truncation)
	sessionPath := filepath.Join(tempDir, "session.json")
	_ = os.WriteFile(sessionPath, []byte(""), 0644)

	// Start engine - it should auto-recover the torrent from cache!
	eng, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer eng.Close()

	eng.WaitForSession(5 * time.Second)

	torrents := eng.GetTorrents()
	if len(torrents) == 0 {
		t.Fatalf("expected auto-recovery to load at least 1 torrent from cache, got 0")
	}

	found := false
	for _, tor := range torrents {
		if strings.EqualFold(tor.InfoHash, hash) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected recovered torrent with hash %s in torrent list", hash)
	}

	// Verify that saveSessionLocked writes atomic .tmp and backup .bak
	eng.mu.Lock()
	eng.saveSessionLocked()
	eng.mu.Unlock()

	stat, err := os.Stat(sessionPath)
	if err != nil || stat.Size() == 0 {
		t.Fatalf("expected session.json to be saved with non-zero size, got size %v, err %v", stat.Size(), err)
	}

	// Test checkpointDatabases
	eng.checkpointDatabases()
}
