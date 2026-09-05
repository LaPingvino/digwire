package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestExtractArchiveOrgIdentifier(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"https://archive.org/download/beethoven_sym5/beethoven_sym5_archive.torrent", "beethoven_sym5"},
		{"https://archive.org/details/pink_floyd_dsotm", "pink_floyd_dsotm"},
		{"https://archive.org/metadata/bach_goldberg_variations", "bach_goldberg_variations"},
		{"ia:mozart_requiem", "mozart_requiem"},
		{"archiveorg:chopin_nocturnes", "chopin_nocturnes"},
		{"t3fkbciua8r8bucjniu8ue13ms3ltlsxrqagzjpj", "t3fkbciua8r8bucjniu8ue13ms3ltlsxrqagzjpj"},
	}

	for _, tc := range tests {
		got := extractArchiveOrgIdentifier(tc.input)
		if got != tc.expected {
			t.Errorf("extractArchiveOrgIdentifier(%q) = %q, expected %q", tc.input, got, tc.expected)
		}
	}
}

func TestInspectHTTPFile(t *testing.T) {
	// Create mock HTTP server with Content-Disposition and Content-Length
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", "attachment; filename=\"sample_audio_track.flac\"")
		w.Header().Set("Content-Type", "audio/flac")
		w.Header().Set("Content-Length", "10485760")
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("FLAC SAMPLE DATA HEADER"))
	}))
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := inspectHTTPFile(ctx, ts.URL+"/download")
	if err != nil {
		t.Fatalf("inspectHTTPFile failed: %v", err)
	}

	if res.Name != "sample_audio_track.flac" {
		t.Errorf("expected Name 'sample_audio_track.flac', got %q", res.Name)
	}
	if res.TotalSize != 10485760 {
		t.Errorf("expected TotalSize 10485760, got %d", res.TotalSize)
	}
	if len(res.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(res.Files))
	}
	if res.Files[0].Path != "sample_audio_track.flac" {
		t.Errorf("expected file path 'sample_audio_track.flac', got %q", res.Files[0].Path)
	}
}

func TestSanitizeFilename(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"Artist / Album : Track * ? < > |", "Artist _ Album _ Track _ _ _ _ _"},
		{"Clean Name.mp3", "Clean Name.mp3"},
		{"   ", "media_file"},
	}

	for _, c := range cases {
		got := sanitizeFilename(c.input)
		if got != c.expected {
			t.Errorf("sanitizeFilename(%q) = %q, expected %q", c.input, got, c.expected)
		}
	}
}
