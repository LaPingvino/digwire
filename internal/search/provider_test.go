package search

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bh90210/soul/peer"
)

func TestSoulseekProviderSanity(t *testing.T) {
	provider := NewSoulseekProvider("Soulseek P2P Audio", "server.slsknet.org:2242", true, 1.5)
	if !provider.IsEnabled() {
		t.Errorf("expected provider to be enabled")
	}
	if provider.Type() != "soulseek" {
		t.Errorf("expected provider type 'soulseek', got %q", provider.Type())
	}
	if provider.Weight() != 1.5 {
		t.Errorf("expected provider weight 1.5, got %v", provider.Weight())
	}

	// Test peer file parsing
	testFile := peer.File{
		Name: `@@abcd\Music\Queen\A Night at the Opera\01 - Death on Two Legs.flac`,
		Size: 25000000,
	}
	res := parseSoulseekFile("queen_fan", testFile, true, 0, 500000, "online")
	if res.ProviderType != "soulseek" {
		t.Errorf("expected ProviderType 'soulseek', got %q", res.ProviderType)
	}
	if res.PeerStatus != "online" {
		t.Errorf("expected PeerStatus 'online', got %q", res.PeerStatus)
	}
	if res.Artist != "Queen" {
		t.Errorf("expected Artist 'Queen', got %q", res.Artist)
	}
	if res.Album != "A Night at the Opera" {
		t.Errorf("expected Album 'A Night at the Opera', got %q", res.Album)
	}
	if res.User != "queen_fan" {
		t.Errorf("expected User 'queen_fan', got %q", res.User)
	}
	if res.Seeders != 1 {
		t.Errorf("expected Seeders 1 (free slot), got %d", res.Seeders)
	}
	if !strings.HasPrefix(res.MagnetURI, "slsk://queen_fan?file=") {
		t.Errorf("expected slsk:// URI, got %q", res.MagnetURI)
	}
}

func TestDocumentsProviderSanity(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"response": {
				"docs": [
					{
						"identifier": "computational_complexity_papadimitriou",
						"title": "Computational Complexity",
						"creator": "Christos Papadimitriou",
						"format": ["Text PDF", "EPUB"],
						"item_size": 15000000,
						"downloads": 420
					}
				]
			}
		}`))
	}))
	defer mockServer.Close()

	provider := NewDocumentProvider("Digital Library & Books", mockServer.URL, true, 1.3)
	results, err := provider.Search(context.Background(), "Complexity")
	if err != nil {
		t.Fatalf("unexpected search error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0]
	if r.Seeders != -1 || r.Leechers != -1 {
		t.Errorf("expected honest unknown peer count (-1, -1), got (%d, %d)", r.Seeders, r.Leechers)
	}
	if r.Artist != "Christos Papadimitriou" {
		t.Errorf("expected Artist 'Christos Papadimitriou', got %q", r.Artist)
	}
	if r.Directory != "Christos Papadimitriou / Computational Complexity" {
		t.Errorf("expected Directory 'Christos Papadimitriou / Computational Complexity', got %q", r.Directory)
	}
}
