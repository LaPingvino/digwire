package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"digwire/internal/config"
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

func TestMediaManagerCancelTask(t *testing.T) {
	mm := NewMediaManager("/tmp", nil)
	taskURL := "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
	taskID := HashURL(taskURL)

	task := &MediaTask{
		ID:       taskID,
		URL:      taskURL,
		Platform: "youtube",
		State:    "failed",
	}

	mm.tasks[taskID] = task

	// Test case-insensitive lookup
	if got := mm.GetTask(taskID); got == nil {
		t.Fatalf("expected to find task by ID")
	}
	if got := mm.GetTask(taskURL); got == nil {
		t.Fatalf("expected to find task by URL")
	}

	// Test cancellation / deletion
	if err := mm.CancelTask(taskID, false); err != nil {
		t.Fatalf("CancelTask failed: %v", err)
	}

	if got := mm.GetTask(taskID); got != nil {
		t.Fatalf("expected task to be deleted")
	}
}

func TestMediaTaskTorrentLikeActions(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "digwire_media_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a mock media directory with multiple files (e.g. scribd / youtube package)
	mediaDir := filepath.Join(tempDir, "Media_Item_123")
	if err := os.MkdirAll(mediaDir, 0755); err != nil {
		t.Fatalf("failed to create media dir: %v", err)
	}

	pdfFile := filepath.Join(mediaDir, "Document.pdf")
	_ = os.WriteFile(pdfFile, []byte("%PDF-1.4 mock content"), 0644)
	txtFile := filepath.Join(mediaDir, "Document.txt")
	_ = os.WriteFile(txtFile, []byte("Mock text content"), 0644)
	htmlFile := filepath.Join(mediaDir, "Document.html")
	_ = os.WriteFile(htmlFile, []byte("<html>Mock reader</html>"), 0644)

	// Test scanMediaTaskFiles
	files := scanMediaTaskFiles(mediaDir, "Document", 100, 100, true)
	if len(files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(files))
	}

	// Create engine and media manager
	cfg := &config.Config{
		DownloadDir: tempDir,
		ListenPort:  0,
		GermanyMode: false,
	}
	cfg.SetConfigPath(filepath.Join(tempDir, "config.yaml"))
	eng, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer eng.Close()

	taskURL := "https://www.scribd.com/document/12345/Test-Book"
	taskID := HashURL(taskURL)
	mTask := &MediaTask{
		ID:             taskID,
		URL:            taskURL,
		Title:          "Test Book",
		Platform:       "scribd",
		State:          "completed",
		Progress:       100.0,
		DestPath:       mediaDir,
		TotalBytes:     1000,
		CompletedBytes: 1000,
	}
	eng.mediaManager.mu.Lock()
	eng.mediaManager.tasks[taskID] = mTask
	eng.mediaManager.mu.Unlock()

	// 1. Test GetTorrentSavePath
	savePath, err := eng.GetTorrentSavePath(taskID)
	if err != nil {
		t.Fatalf("GetTorrentSavePath failed: %v", err)
	}
	if savePath != mediaDir {
		t.Errorf("expected savePath %s, got %s", mediaDir, savePath)
	}

	// 2. Test GetTorrentFilePath
	filePath0, err := eng.GetTorrentFilePath(taskID, 0)
	if err != nil {
		t.Fatalf("GetTorrentFilePath(0) failed: %v", err)
	}
	if !strings.HasSuffix(filePath0, "Document.html") {
		t.Errorf("expected Document.html (alphabetical), got %s", filePath0)
	}

	filePath1, err := eng.GetTorrentFilePath(taskID, 1)
	if err != nil {
		t.Fatalf("GetTorrentFilePath(1) failed: %v", err)
	}
	if !strings.HasSuffix(filePath1, "Document.pdf") {
		t.Errorf("expected Document.pdf, got %s", filePath1)
	}

	// 3. Test GetTorrentDetails
	details, err := eng.GetTorrentDetails(taskID)
	if err != nil {
		t.Fatalf("GetTorrentDetails failed: %v", err)
	}
	if details.Name != "Test Book" {
		t.Errorf("expected details name 'Test Book', got %s", details.Name)
	}
	if len(details.Files) != 3 {
		t.Errorf("expected 3 files in details, got %d", len(details.Files))
	}
	if !details.IsMedia || details.Platform != "scribd" {
		t.Errorf("expected IsMedia true and Platform scribd, got %v / %s", details.IsMedia, details.Platform)
	}

	// 4. Test GetTorrentFileBytes (on the fly export)
	torrentBytes, name, err := eng.GetTorrentFileBytes(taskID)
	if err != nil {
		t.Fatalf("GetTorrentFileBytes failed: %v", err)
	}
	if len(torrentBytes) == 0 {
		t.Fatalf("expected non-empty torrent bytes")
	}
	if name == "" {
		t.Fatalf("expected non-empty torrent name")
	}

	// 5. Test Remove
	if err := eng.Remove(taskID, true); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Verify task deleted from manager and files removed
	if eng.mediaManager.GetTask(taskID) != nil {
		t.Fatalf("expected media task to be removed")
	}
	if _, err := os.Stat(mediaDir); !os.IsNotExist(err) {
		t.Fatalf("expected mediaDir to be deleted from disk on deleteFiles=true")
	}
}



