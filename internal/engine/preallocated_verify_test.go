package engine

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"

	"digwire/internal/config"
)

// A full-size file on disk is not proof of a complete download: storage preallocates files long
// before their data arrives. Verification must hash, not compare sizes.
func TestPreallocatedFileIsNotMarkedComplete(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempDir, "xdg"))
	downloadDir := filepath.Join(tempDir, "downloads")
	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{DownloadDir: downloadDir}
	cfg.SetConfigPath(filepath.Join(tempDir, "config.yaml"))
	eng, err := NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	payload := filepath.Join(downloadDir, "movie.mkv")
	data := make([]byte, 8*16384)
	_, _ = rand.Read(data)
	if err := os.WriteFile(payload, data, 0644); err != nil {
		t.Fatal(err)
	}
	info := metainfo.Info{PieceLength: 16384}
	if err := info.BuildFromFilePath(payload); err != nil {
		t.Fatal(err)
	}
	infoBytes, err := bencode.Marshal(&info)
	if err != nil {
		t.Fatal(err)
	}

	// Same size, but only the first piece holds real data.
	prealloc := make([]byte, len(data))
	copy(prealloc, data[:16384])
	if err := os.WriteFile(payload, prealloc, 0644); err != nil {
		t.Fatal(err)
	}

	tor, _, err := eng.client.AddTorrentSpec(torrent.TorrentSpecFromMetaInfo(&metainfo.MetaInfo{InfoBytes: infoBytes}))
	if err != nil {
		t.Fatal(err)
	}
	hash := tor.InfoHash().HexString()
	eng.mu.Lock()
	eng.initTracker(hash)
	eng.rateMap[hash].isPaused = true
	eng.mu.Unlock()

	done := make(chan struct{})
	eng.ConsolidateAndVerify(tor, func() { close(done) })
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("verification did not finish")
	}

	if got, want := tor.BytesCompleted(), int64(16384); got != want {
		t.Fatalf("BytesCompleted = %d, want %d (only the first piece is real)", got, want)
	}
	for _, st := range eng.GetTorrents() {
		if st.InfoHash == hash && (st.State == "seeding" || st.State == "completed") {
			t.Fatalf("preallocated torrent reported as %s", st.State)
		}
	}
}
