package search

import (
	"strings"
	"testing"

	"github.com/bh90210/soul/peer"
)

func TestSoulseekPeerSemaphore(t *testing.T) {
	client := GetSoulseekClient()

	sem1 := client.getPeerSem("TestUser")
	sem2 := client.getPeerSem("testuser")
	sem3 := client.getPeerSem("  TESTUSER  ")
	semOther := client.getPeerSem("OtherUser")

	if sem1 != sem2 {
		t.Errorf("expected sem1 and sem2 to be identical channel for same user")
	}
	if sem1 != sem3 {
		t.Errorf("expected sem1 and sem3 to be identical channel for same user with whitespace/casing")
	}
	if sem1 == semOther {
		t.Errorf("expected different semaphores for different users")
	}

	// Verify channel capacity is 1 (strict serialization per peer)
	if cap(sem1) != 1 {
		t.Errorf("expected semaphore capacity 1, got %d", cap(sem1))
	}
}

func TestSoulseekURIParsing(t *testing.T) {
	res := parseSoulseekFile("ElectroUser", peer.File{
		Name: "Music\\Caravan Palace\\Panic\\01 - Rock It For Me.mp3",
		Size: 4096,
	}, true, 0, 1000)

	if !strings.Contains(res.Artist, "Caravan Palace") {
		t.Errorf("expected artist to be Caravan Palace, got '%s'", res.Artist)
	}
	if !strings.Contains(res.Album, "Panic") {
		t.Errorf("expected album to be Panic, got '%s'", res.Album)
	}
	if res.Seeders != 1 {
		t.Errorf("expected 1 seeder for free slot, got %d", res.Seeders)
	}
}
