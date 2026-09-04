package engine

import (
	"testing"
)

func TestDetectMediaPlatform(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"https://youtu.be/wQjzhD_nCgU?si=p-eAuOZICr8SayW8", "youtube"},
		{"https://www.youtube.com/watch?v=dQw4w9WgXcQ", "youtube"},
		{"https://www.tiktok.com/@user/video/1234567890", "tiktok"},
		{"https://www.instagram.com/reel/C123456/", "instagram"},
		{"https://twitter.com/user/status/1234567890", "twitter"},
		{"https://x.com/user/status/1234567890", "twitter"},
		{"https://www.reddit.com/r/videos/comments/abc123/cool_video/", "reddit"},
		{"https://vimeo.com/123456789", "vimeo"},
		{"https://www.scribd.com/document/123456/Sample-Book", "scribd"},
	}

	for _, tt := range tests {
		got := DetectMediaPlatform(tt.url)
		if got != tt.expected {
			t.Errorf("DetectMediaPlatform(%q) = %q; want %q", tt.url, got, tt.expected)
		}
	}
}

func TestIsMediaURL(t *testing.T) {
	if !IsMediaURL("https://youtu.be/wQjzhD_nCgU?si=p-eAuOZICr8SayW8") {
		t.Errorf("expected YouTube URL to be recognized as MediaURL")
	}
	if !IsMediaURL("https://example.com/video.mp4") {
		t.Errorf("expected mp4 direct link to be recognized as MediaURL")
	}
	if IsMediaURL("magnet:?xt=urn:btih:6e1c86db3452830f811e16c9ff19c9f05f7bd4fb") {
		t.Errorf("expected magnet URI to not be recognized as MediaURL")
	}
}

func TestSanitizeMediaFilename(t *testing.T) {
	got := SanitizeMediaFilename("Test: / Video * Name ? <Great> | 2026")
	expected := "Test_ _ Video _ Name _ _Great_ _ 2026"
	if got != expected {
		t.Errorf("SanitizeMediaFilename() = %q; want %q", got, expected)
	}
}

func TestGetMediaExtractorArgs(t *testing.T) {
	args := GetMediaExtractorArgs()
	if len(args) == 0 {
		t.Errorf("expected at least one extractor candidate set")
	}
}
