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

func TestBEP52InfoHashExtraction(t *testing.T) {
	// 1. Classic v1 hex (40 chars)
	v1Hex := "0123456789abcdef0123456789abcdef01234567"
	if extractInfoHash(v1Hex) != v1Hex {
		t.Fatalf("expected %s, got %s", v1Hex, extractInfoHash(v1Hex))
	}

	// 2. BEP 52 v2 hex (64 chars)
	v2Hex := "d8dd32ac93357c368556af3ac1d95c9d76bd0dff6fa9833ecdac3d53134efabb"
	if extractInfoHash(v2Hex) != v2Hex {
		t.Fatalf("expected %s, got %s", v2Hex, extractInfoHash(v2Hex))
	}

	// 3. Pure v2 magnet with multihash (1220 prefix for sha2-256)
	v2Magnet := "magnet:?xt=urn:btmh:1220" + v2Hex + "&dn=TestV2"
	if extractInfoHash(v2Magnet) != v2Hex {
		t.Fatalf("expected %s, got %s", v2Hex, extractInfoHash(v2Magnet))
	}

	// 4. Hybrid magnet with both v1 and v2
	hybridMagnet := "magnet:?xt=urn:btih:" + v1Hex + "&xt=urn:btmh:1220" + v2Hex + "&dn=HybridTest"
	h := extractInfoHash(hybridMagnet)
	if h != v1Hex && h != v2Hex {
		t.Fatalf("expected %s or %s, got %s", v1Hex, v2Hex, h)
	}
}

