package engine

import (
	"strings"
	"testing"
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
