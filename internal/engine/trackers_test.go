package engine

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"digwire/internal/config"
)

func TestDefaultTier1TrackersDoesNotContainUbuntu(t *testing.T) {
	for _, tr := range DefaultTier1Trackers {
		if strings.Contains(tr, "torrent.ubuntu.com") {
			t.Fatalf("DefaultTier1Trackers should not contain private tracker ubuntu: %s", tr)
		}
	}
}

func TestSuperchargeMagnetTrackers(t *testing.T) {
	mag := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Test"
	supercharged := SuperchargeMagnet(mag)
	if strings.Contains(supercharged, "torrent.ubuntu.com") {
		t.Fatalf("SuperchargeMagnet should not add ubuntu tracker: %s", supercharged)
	}
	if !strings.Contains(supercharged, "tracker.opentrackr.org") {
		t.Fatalf("SuperchargeMagnet missing tracker.opentrackr.org: %s", supercharged)
	}
}

func TestEngineDHTBootstrapLive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live DHT bootstrap test in short mode")
	}
	tmp := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.SetConfigPath(filepath.Join(tmp, "config.yaml"))
	cfg.DownloadDir = tmp
	cfg.ListenPort = 0
	eng, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer eng.Close()

	dhtServers := eng.client.DhtServers()
	t.Logf("Initialized %d DHT server(s)", len(dhtServers))
	if len(dhtServers) == 0 {
		t.Fatal("expected at least 1 DHT server")
	}

	var dhtNodes int
	for i := 0; i < 20; i++ {
		time.Sleep(250 * time.Millisecond)
		stats := eng.GetGlobalStats()
		if stats.DHTNodes > 0 {
			dhtNodes = stats.DHTNodes
			break
		}
	}
	t.Logf("Acquired %d DHT nodes within timeout", dhtNodes)
	if dhtNodes == 0 {
		t.Fatal("failed to bootstrap any DHT nodes!")
	}
}
