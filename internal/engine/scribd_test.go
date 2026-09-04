package engine

import (
	"strings"
	"testing"
)

func TestIsScribdURL(t *testing.T) {
	tests := []struct {
		url      string
		expected bool
	}{
		{"https://www.scribd.com/document/55979805/Canadian-Dollar-Bill", true},
		{"https://scribd.com/doc/123456/Sample", true},
		{"https://www.scribd.com/presentation/999/Slides", true},
		{"https://www.scribd.com/book/777/Novel", true},
		{"https://www.scribd.com/read/888/Article", true},
		{"https://youtube.com/watch?v=123", false},
		{"https://archive.org/details/test", false},
	}

	for _, tt := range tests {
		got := IsScribdURL(tt.url)
		if got != tt.expected {
			t.Errorf("IsScribdURL(%q) = %v; want %v", tt.url, got, tt.expected)
		}
	}
}

func TestExtractScribdID(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"https://www.scribd.com/document/55979805/Canadian-Dollar-Bill", "55979805"},
		{"https://scribd.com/doc/12345678/Sample", "12345678"},
		{"https://www.scribd.com/presentation/987654/My-Deck", "987654"},
		{"https://www.scribd.com/embeds/445566/content", "445566"},
		{"https://scribd.com/?doc_id=112233", "112233"},
	}

	for _, tt := range tests {
		got := ExtractScribdID(tt.url)
		if got != tt.expected {
			t.Errorf("ExtractScribdID(%q) = %q; want %q", tt.url, got, tt.expected)
		}
	}
}

func TestGenerateScribdHTMLReader(t *testing.T) {
	meta := &MediaMetadata{
		ID:          "55979805",
		Title:       "Test Document Title",
		Description: "This is a test description.",
		Uploader:    "Test Author",
		Duration:    5,
		URL:         "https://www.scribd.com/document/55979805/Test-Document",
		Platform:    "scribd",
	}

	bodyText := "Chapter 1: The Beginning\n\nThis is paragraph 1.\n\nThis is paragraph 2."

	htmlOutput := generateScribdHTMLReader(meta, bodyText, true)

	if !strings.Contains(htmlOutput, "Test Document Title") {
		t.Errorf("expected HTML output to contain title")
	}
	if !strings.Contains(htmlOutput, "Test Author") {
		t.Errorf("expected HTML output to contain author")
	}
	if !strings.Contains(htmlOutput, "cover.jpg") {
		t.Errorf("expected HTML output to reference cover.jpg")
	}
	if !strings.Contains(htmlOutput, "<p>Chapter 1: The Beginning</p>") {
		t.Errorf("expected formatted paragraph in HTML output")
	}
}
