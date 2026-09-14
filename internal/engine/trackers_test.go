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

func TestAddBEP52Magnets(t *testing.T) {
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

	// 1. User's pure v2 magnet
	v2Mag := "magnet:?xt=urn:btmh:1220caf1e1c30e81cb361b9ee167c4aa64228a7fa4fa9f6105232b28ad099f3a302e&dn=bittorrent-v2-test"
	torV2, errV2 := eng.Add(v2Mag)
	if errV2 != nil {
		t.Fatalf("pure v2 add failed: %v", errV2)
	}
	if torV2 == nil {
		t.Fatal("expected non-nil torrent for pure v2")
	}
	t.Logf("pure v2 add: tor=%v, err=%v", torV2, errV2)

	// 2. User's hybrid magnet
	hybridMag := "magnet:?xt=urn:btih:631a31dd0a46257d5078c0dee4e66e26f73e42ac&xt=urn:btmh:1220d8dd32ac93357c368556af3ac1d95c9d76bd0dff6fa9833ecdac3d53134efabb&dn=bittorrent-v1-v2-hybrid-test"
	torHyb, errHyb := eng.Add(hybridMag)
	if errHyb != nil {
		t.Fatalf("hybrid add failed: %v", errHyb)
	}
	if torHyb == nil {
		t.Fatal("expected non-nil torrent for hybrid")
	}
	t.Logf("hybrid add: tor=%v, err=%v", torHyb, errHyb)

	stats := eng.GetGlobalStats()
	if stats.TotalV2 != 1 {
		t.Fatalf("expected 1 TotalV2, got %d", stats.TotalV2)
	}
	if stats.TotalHybrid != 1 {
		t.Fatalf("expected 1 TotalHybrid, got %d", stats.TotalHybrid)
	}

	torrents := eng.GetTorrents()
	if len(torrents) != 2 {
		t.Fatalf("expected 2 torrents, got %d", len(torrents))
	}
	var foundV2, foundHybrid bool
	for _, tor := range torrents {
		if tor.ProtocolVersion == "v2" {
			foundV2 = true
			if tor.InfoHashV2 != "caf1e1c30e81cb361b9ee167c4aa64228a7fa4fa9f6105232b28ad099f3a302e" {
				t.Fatalf("unexpected v2 hash: %s", tor.InfoHashV2)
			}
		}
		if tor.ProtocolVersion == "hybrid" {
			foundHybrid = true
			if tor.InfoHashV2 != "d8dd32ac93357c368556af3ac1d95c9d76bd0dff6fa9833ecdac3d53134efabb" {
				t.Fatalf("unexpected hybrid v2 hash: %s", tor.InfoHashV2)
			}
		}
	}
	if !foundV2 {
		t.Fatal("expected to find v2 torrent in GetTorrents")
	}
	if !foundHybrid {
		t.Fatal("expected to find hybrid torrent in GetTorrents")
	}
}

func TestSplitMagnet(t *testing.T) {
	hybrid := "magnet:?xt=urn:btih:631a31dd0a46257d5078c0dee4e66e26f73e42ac&xt=urn:btmh:1220d8dd32ac93357c368556af3ac1d95c9d76bd0dff6fa9833ecdac3d53134efabb&dn=test&tr=http%3A%2F%2Ftracker.com"
	variants := SplitMagnet(hybrid)

	if !strings.Contains(variants.Full, "urn:btih:") || !strings.Contains(variants.Full, "urn:btmh:") {
		t.Fatalf("Full magnet should contain both hashes: %s", variants.Full)
	}

	if !strings.Contains(variants.V1Only, "urn:btih:") || strings.Contains(variants.V1Only, "urn:btmh:") {
		t.Fatalf("V1Only should only contain btih: %s", variants.V1Only)
	}
	if !strings.Contains(variants.V1Only, "dn=test") || !strings.Contains(variants.V1Only, "tr=http") {
		t.Fatalf("V1Only missing common params: %s", variants.V1Only)
	}

	if !strings.Contains(variants.V2Only, "urn:btmh:") || strings.Contains(variants.V2Only, "urn:btih:") {
		t.Fatalf("V2Only should only contain btmh: %s", variants.V2Only)
	}
	if !strings.Contains(variants.V2Only, "dn=test") || !strings.Contains(variants.V2Only, "tr=http") {
		t.Fatalf("V2Only missing common params: %s", variants.V2Only)
	}

	// Pure v2
	pureV2 := "magnet:?xt=urn:btmh:1220caf1e1c30e81cb361b9ee167c4aa64228a7fa4fa9f6105232b28ad099f3a302e&dn=v2only"
	v2Variants := SplitMagnet(pureV2)
	if v2Variants.V1Only != "" {
		t.Fatalf("pure v2 should not have V1Only: %s", v2Variants.V1Only)
	}
	if !strings.Contains(v2Variants.V2Only, "urn:btmh:") {
		t.Fatalf("pure v2 should have V2Only: %s", v2Variants.V2Only)
	}
}

