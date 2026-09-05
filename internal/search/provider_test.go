package search

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSoulseekProviderSanity(t *testing.T) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"response": {
				"docs": [
					{
						"identifier": "queen_night_opera_flac",
						"title": "A Night at the Opera",
						"creator": "Queen",
						"format": ["FLAC", "VBR MP3"],
						"item_size": 350000000,
						"downloads": 1500
					}
				]
			}
		}`))
	}))
	defer mockServer.Close()

	provider := NewSoulseekProvider("Soulseek P2P Audio", mockServer.URL, true, 1.5)
	if !provider.IsEnabled() {
		t.Errorf("expected provider to be enabled")
	}
	if provider.Type() != "soulseek" {
		t.Errorf("expected provider type 'soulseek', got %q", provider.Type())
	}

	results, err := provider.Search(context.Background(), "Queen")
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
	if r.ProviderType != "soulseek" {
		t.Errorf("expected ProviderType 'soulseek', got %q", r.ProviderType)
	}
	if r.Artist != "Queen" {
		t.Errorf("expected Artist 'Queen', got %q", r.Artist)
	}
	if r.Album != "A Night at the Opera" {
		t.Errorf("expected Album 'A Night at the Opera', got %q", r.Album)
	}
	if r.Directory != "Queen / A Night at the Opera" {
		t.Errorf("expected Directory 'Queen / A Night at the Opera', got %q", r.Directory)
	}
	if r.User != "queen_night_opera_flac" {
		t.Errorf("expected User 'queen_night_opera_flac', got %q", r.User)
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
