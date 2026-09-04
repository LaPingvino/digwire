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

func TestParseYtDlpProgressLine(t *testing.T) {
	tests := []struct {
		line        string
		expectedPct float64
		hasSpeed    bool
		hasTotal    bool
		valid       bool
	}{
		{
			line:        " 45.2%| 2.50MiB/s|00:35| 50.20MiB",
			expectedPct: 45.2,
			hasSpeed:    true,
			hasTotal:    true,
			valid:       true,
		},
		{
			line:        "\x1b[0;94m 75.0%\x1b[0m|\x1b[0;32m 10.00MiB/s\x1b[0m|\x1b[0;33m00:10\x1b[0m|\x1b[0;35m 100.00MiB\x1b[0m",
			expectedPct: 75.0,
			hasSpeed:    true,
			hasTotal:    true,
			valid:       true,
		},
		{
			line:        "[download]  25.4% of ~50.20MiB at  3.50MiB/s ETA 00:15",
			expectedPct: 25.4,
			hasSpeed:    true,
			hasTotal:    true,
			valid:       true,
		},
		{
			line:        "[download] 100% of 100.00MiB in 00:10 at 9.80MiB/s",
			expectedPct: 100.0,
			hasSpeed:    false,
			hasTotal:    true,
			valid:       true,
		},
		{
			line:        "[download] Downloading video fragment 12 of 48",
			expectedPct: 25.0,
			hasSpeed:    false,
			hasTotal:    false,
			valid:       true,
		},
		{
			line:        "Just some random log line",
			expectedPct: 0,
			hasSpeed:    false,
			hasTotal:    false,
			valid:       false,
		},
	}

	for _, tt := range tests {
		pct, speed, _, total, ok := parseYtDlpProgressLine(tt.line)
		if ok != tt.valid {
			t.Errorf("parseYtDlpProgressLine(%q) ok = %v; want %v", tt.line, ok, tt.valid)
			continue
		}
		if tt.valid && pct != tt.expectedPct {
			t.Errorf("parseYtDlpProgressLine(%q) pct = %v; want %v", tt.line, pct, tt.expectedPct)
		}
		if tt.hasSpeed && speed <= 0 {
			t.Errorf("parseYtDlpProgressLine(%q) speed = %v; want > 0", tt.line, speed)
		}
		if tt.hasTotal && total <= 0 {
			t.Errorf("parseYtDlpProgressLine(%q) total = %v; want > 0", tt.line, total)
		}
	}
}

func TestParseByteSize(t *testing.T) {
	var expected50MB float64 = 50.20 * 1024 * 1024
	if parseByteSize("50.20MiB") != int64(expected50MB) {
		t.Errorf("unexpected byte size for 50.20MiB: %d", parseByteSize("50.20MiB"))
	}
	var expected15GB float64 = 1.5 * 1024 * 1024 * 1024
	if parseByteSize("1.5GiB") != int64(expected15GB) {
		t.Errorf("unexpected byte size for 1.5GiB: %d", parseByteSize("1.5GiB"))
	}
	if parseByteSize("500KiB") != 500*1024 {
		t.Errorf("unexpected byte size for 500KiB: %d", parseByteSize("500KiB"))
	}
	if parseByteSize("N/A") != 0 {
		t.Errorf("expected 0 for N/A")
	}
}

func TestParseETASec(t *testing.T) {
	if parseETASec("01:30") != 90 {
		t.Errorf("unexpected ETA for 01:30: %d", parseETASec("01:30"))
	}
	if parseETASec("01:02:03") != 3723 {
		t.Errorf("unexpected ETA for 01:02:03: %d", parseETASec("01:02:03"))
	}
	if parseETASec("N/A") != 0 {
		t.Errorf("expected 0 for N/A")
	}
}

