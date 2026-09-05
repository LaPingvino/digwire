package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MediaFormat describes an audio or video stream format returned by yt-dlp
type MediaFormat struct {
	FormatID       string  `json:"format_id"`
	FormatNote     string  `json:"format_note,omitempty"`
	Ext            string  `json:"ext"`
	Resolution     string  `json:"resolution,omitempty"`
	Width          int     `json:"width,omitempty"`
	Height         int     `json:"height,omitempty"`
	FPS            float64 `json:"fps,omitempty"`
	VCodec         string  `json:"vcodec,omitempty"`
	ACodec         string  `json:"acodec,omitempty"`
	Filesize       int64   `json:"filesize,omitempty"`
	FilesizeApprox int64   `json:"filesize_approx,omitempty"`
	TBR            float64 `json:"tbr,omitempty"`
	URL            string  `json:"url,omitempty"`
}

// MediaSubtitle describes a subtitle or caption track
type MediaSubtitle struct {
	Ext  string `json:"ext"`
	URL  string `json:"url"`
	Name string `json:"name,omitempty"`
}

// MediaMetadata holds extracted social & streaming media metadata
type MediaMetadata struct {
	URL         string                     `json:"url"`
	ID          string                     `json:"id"`
	Title       string                     `json:"title"`
	Description string                     `json:"description,omitempty"`
	Thumbnail   string                     `json:"thumbnail,omitempty"`
	Duration    int64                      `json:"duration,omitempty"` // in seconds
	Uploader    string                     `json:"uploader,omitempty"`
	UploaderURL string                     `json:"uploader_url,omitempty"`
	Platform    string                     `json:"platform"` // "youtube", "tiktok", "instagram", "twitter", "reddit", "scribd", "twitch", "vimeo", etc.
	UploadDate  string                     `json:"upload_date,omitempty"`
	ViewCount   int64                      `json:"view_count,omitempty"`
	Formats      []MediaFormat              `json:"formats,omitempty"`
	Subtitles    map[string][]MediaSubtitle `json:"subtitles,omitempty"`
	DirectURL    string                     `json:"direct_url,omitempty"`
	CookieArgs   []string                   `json:"cookie_args,omitempty"`
	CookieSource string                     `json:"cookie_source,omitempty"`
}

// MediaDownloadOptions configures how media is fetched and converted
type MediaDownloadOptions struct {
	Format      string   `json:"format"`       // e.g. "bestvideo+bestaudio/best", "1080p", "720p", "audio_mp3", "audio_flac"
	AudioOnly   bool     `json:"audio_only"`   // extract audio only
	AudioFormat string   `json:"audio_format"` // "mp3", "flac", "m4a", "opus"
	Subtitles   []string `json:"subtitles"`    // ["en", "all", "auto"]
	EmbedSubs   bool     `json:"embed_subs"`   // embed subtitles into video container
	AutoSwarm   bool     `json:"auto_swarm"`   // convert to BitTorrent swarm upon completion
}

// MediaTask represents an active or completed media download task
type MediaTask struct {
	mu             sync.RWMutex
	ID             string                `json:"id"`
	URL            string                `json:"url"`
	Title          string                `json:"title"`
	Platform       string                `json:"platform"`
	Thumbnail      string                `json:"thumbnail,omitempty"`
	Duration       int64                 `json:"duration,omitempty"`
	Uploader       string                `json:"uploader,omitempty"`
	State          string                `json:"state"` // "inspecting", "downloading", "processing", "creating_swarm", "seeding", "completed", "failed", "paused"
	Progress       float64               `json:"progress"` // 0.0 - 100.0
	DownloadRate   int64                 `json:"download_rate"` // bytes/sec
	ETASeconds     int64                 `json:"eta_seconds"`
	CompletedBytes int64                 `json:"completed_bytes"`
	TotalBytes     int64                 `json:"total_bytes"`
	DestPath       string                `json:"dest_path"`
	InfoHash       string                `json:"info_hash,omitempty"`
	MagnetURI      string                `json:"magnet_uri,omitempty"`
	AddedAt        int64                 `json:"added_at"`
	Error          string                `json:"error,omitempty"`
	Options        MediaDownloadOptions  `json:"options"`
	Metadata       *MediaMetadata        `json:"metadata,omitempty"`

	cancel context.CancelFunc            `json:"-"`
	cmd    *exec.Cmd                     `json:"-"`
}

// MediaManager manages background yt-dlp downloads and swarm packaging
type MediaManager struct {
	mu          sync.RWMutex
	tasks       map[string]*MediaTask
	downloadDir string
	engine      *Engine
}

func NewMediaManager(downloadDir string, engine *Engine) *MediaManager {
	return &MediaManager{
		tasks:       make(map[string]*MediaTask),
		downloadDir: downloadDir,
		engine:      engine,
	}
}

// DetectYtDlpPath locates the yt-dlp binary on the system
func DetectYtDlpPath() (string, error) {
	candidates := []string{
		"yt-dlp",
		"/usr/bin/yt-dlp",
		"/usr/sbin/yt-dlp",
		"/usr/local/bin/yt-dlp",
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates,
			filepath.Join(home, ".local", "bin", "yt-dlp"),
			filepath.Join(home, "bin", "yt-dlp"),
		)
	}

	for _, p := range candidates {
		if path, err := exec.LookPath(p); err == nil {
			return path, nil
		}
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("yt-dlp executable not found in PATH or standard system directories")
}

// DetectMediaPlatform classifies the platform from a media URL
func DetectMediaPlatform(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "web"
	}
	host := strings.ToLower(u.Hostname())
	host = strings.TrimPrefix(host, "www.")
	host = strings.TrimPrefix(host, "m.")

	matchDomain := func(d string) bool {
		return host == d || strings.HasSuffix(host, "."+d)
	}

	switch {
	case matchDomain("youtube.com") || matchDomain("youtu.be"):
		return "youtube"
	case matchDomain("tiktok.com"):
		return "tiktok"
	case matchDomain("instagram.com"):
		return "instagram"
	case matchDomain("twitter.com") || matchDomain("x.com") || matchDomain("t.co"):
		return "twitter"
	case matchDomain("reddit.com") || matchDomain("redd.it"):
		return "reddit"
	case matchDomain("vimeo.com"):
		return "vimeo"
	case matchDomain("twitch.tv"):
		return "twitch"
	case matchDomain("bilibili.com"):
		return "bilibili"
	case matchDomain("soundcloud.com"):
		return "soundcloud"
	case matchDomain("facebook.com") || matchDomain("fb.watch"):
		return "facebook"
	case matchDomain("scribd.com"):
		return "scribd"
	case matchDomain("archive.org"):
		return "archiveorg"
	case matchDomain("rumble.com"):
		return "rumble"
	case matchDomain("odysee.com"):
		return "odysee"
	case matchDomain("dailymotion.com") || matchDomain("dai.ly"):
		return "dailymotion"
	case matchDomain("threads.net"):
		return "threads"
	case matchDomain("pinterest.com") || matchDomain("pin.it"):
		return "pinterest"
	default:
		return "media"
	}
}

// IsMediaURL determines if a URL should be routed to the media engine
func IsMediaURL(rawURL string) bool {
	rawURL = strings.TrimSpace(rawURL)
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return false
	}
	platform := DetectMediaPlatform(rawURL)
	if platform != "web" && platform != "media" {
		return true
	}

	// Specific media extension check
	lower := strings.ToLower(rawURL)
	mediaExts := []string{".mp4", ".mkv", ".webm", ".avi", ".mov", ".flv", ".mp3", ".flac", ".m4a", ".opus", ".wav", ".pdf", ".epub"}
	for _, ext := range mediaExts {
		if strings.HasSuffix(lower, ext) || strings.Contains(lower, ext+"?") {
			return true
		}
	}

	return false
}

// SanitizeMediaFilename cleans a string for safe filesystem paths
func SanitizeMediaFilename(name string) string {
	name = strings.TrimSpace(name)
	reg := regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)
	name = reg.ReplaceAllString(name, "_")
	name = strings.Trim(name, ". ")
	if len(name) > 120 {
		name = name[:120]
	}
	if name == "" {
		name = "media_item"
	}
	return name
}

type ytdlpJSON struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Thumbnail   string `json:"thumbnail"`
	Duration    any    `json:"duration"`
	Uploader    string `json:"uploader"`
	UploaderURL string `json:"uploader_url"`
	Extractor   string `json:"extractor"`
	UploadDate  string `json:"upload_date"`
	ViewCount   int64  `json:"view_count"`
	URL         string `json:"url"`
	Formats     []struct {
		FormatID       string  `json:"format_id"`
		FormatNote     string  `json:"format_note"`
		Ext            string  `json:"ext"`
		Resolution     string  `json:"resolution"`
		Width          int     `json:"width"`
		Height         int     `json:"height"`
		FPS            float64 `json:"fps"`
		VCodec         string  `json:"vcodec"`
		ACodec         string  `json:"acodec"`
		Filesize       int64   `json:"filesize"`
		FilesizeApprox int64   `json:"filesize_approx"`
		TBR            float64 `json:"tbr"`
		URL            string  `json:"url"`
	} `json:"formats"`
	Subtitles map[string][]struct {
		Ext  string `json:"ext"`
		URL  string `json:"url"`
		Name string `json:"name"`
	} `json:"subtitles"`
	AutomaticCaptions map[string][]struct {
		Ext  string `json:"ext"`
		URL  string `json:"url"`
		Name string `json:"name"`
	} `json:"automatic_captions"`
}

// GetMediaExtractorArgs returns prioritized combinations of cookie and extractor arguments for yt-dlp
func GetMediaExtractorArgs() [][]string {
	var candidates [][]string

	// 1. Custom cookies.txt in user config dir
	configDir, _ := os.UserConfigDir()
	if configDir != "" {
		cookiesFile := filepath.Join(configDir, "digwire", "cookies.txt")
		if fi, err := os.Stat(cookiesFile); err == nil && fi.Size() > 0 {
			candidates = append(candidates, []string{"--cookies", cookiesFile})
		}
	}

	// 2. Installed browser profiles (Firefox, Chromium, Chrome, Brave, Edge, Opera, Vivaldi)
	home, _ := os.UserHomeDir()
	browserProfiles := []struct {
		name string
		path string
	}{
		{"firefox", filepath.Join(home, ".mozilla", "firefox")},
		{"chromium", filepath.Join(home, ".config", "chromium")},
		{"chrome", filepath.Join(home, ".config", "google-chrome")},
		{"brave", filepath.Join(home, ".config", "BraveSoftware", "Brave-Browser")},
		{"edge", filepath.Join(home, ".config", "microsoft-edge")},
		{"opera", filepath.Join(home, ".config", "opera")},
		{"vivaldi", filepath.Join(home, ".config", "vivaldi")},
	}

	for _, bp := range browserProfiles {
		if fi, err := os.Stat(bp.path); err == nil && fi.IsDir() {
			candidates = append(candidates, []string{"--cookies-from-browser", bp.name})
		}
	}

	// 3. Browsers available on PATH
	for _, b := range []string{"firefox", "google-chrome", "chromium", "brave-browser", "brave", "microsoft-edge", "opera", "vivaldi"} {
		if _, err := exec.LookPath(b); err == nil {
			cleanName := b
			if strings.HasPrefix(b, "google-chrome") {
				cleanName = "chrome"
			} else if strings.HasPrefix(b, "brave") {
				cleanName = "brave"
			} else if strings.HasPrefix(b, "microsoft-edge") {
				cleanName = "edge"
			}
			alreadyPresent := false
			for _, c := range candidates {
				if len(c) == 2 && c[0] == "--cookies-from-browser" && c[1] == cleanName {
					alreadyPresent = true
					break
				}
			}
			if !alreadyPresent {
				candidates = append(candidates, []string{"--cookies-from-browser", cleanName})
			}
		}
	}

	// 4. Default no-cookies attempt
	candidates = append(candidates, []string{})

	return candidates
}

// InspectMedia inspects a media URL using yt-dlp and extracts rich metadata, automatically authenticating with available browser cookies when required
func InspectMedia(ctx context.Context, rawURL string) (*MediaMetadata, error) {
	if DetectMediaPlatform(rawURL) == "scribd" || IsScribdURL(rawURL) {
		return InspectScribd(ctx, rawURL)
	}

	bin, err := DetectYtDlpPath()
	if err != nil {
		return nil, err
	}

	extractorCandidates := GetMediaExtractorArgs()
	var lastErr error
	var winningStdout []byte
	var winningCookieArgs []string

	for _, cookieArgs := range extractorCandidates {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		inspectCtx, cancel := context.WithTimeout(ctx, 35*time.Second)

		args := []string{
			"-J",
			"--no-warnings",
			"--flat-playlist",
			"--no-check-certificates",
		}
		args = append(args, cookieArgs...)
		args = append(args, rawURL)

		cmd := exec.CommandContext(inspectCtx, bin, args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		if err := cmd.Run(); err == nil && stdout.Len() > 0 {
			cancel()
			winningStdout = stdout.Bytes()
			winningCookieArgs = cookieArgs
			break
		}
		if ctx.Err() != nil {
			cancel()
			return nil, ctx.Err()
		}

		// Retry with full dump without flat-playlist if flat-playlist failed
		fullArgs := []string{"-J", "--no-warnings", "--no-check-certificates"}
		fullArgs = append(fullArgs, cookieArgs...)
		fullArgs = append(fullArgs, rawURL)

		cmd2 := exec.CommandContext(inspectCtx, bin, fullArgs...)
		stdout.Reset()
		stderr.Reset()
		cmd2.Stdout = &stdout
		cmd2.Stderr = &stderr

		if err2 := cmd2.Run(); err2 == nil && stdout.Len() > 0 {
			cancel()
			winningStdout = stdout.Bytes()
			winningCookieArgs = cookieArgs
			break
		}

		cancel()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		errStr := strings.TrimSpace(stderr.String())
		if errStr == "" && err != nil {
			errStr = err.Error()
		}
		lastErr = fmt.Errorf("%s", errStr)
	}

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if len(winningStdout) == 0 {
		if lastErr != nil && strings.Contains(strings.ToLower(lastErr.Error()), "sign in to confirm you") {
			return nil, fmt.Errorf("YouTube requires sign-in: Please log into YouTube in your browser (Firefox/Chrome/Chromium/Brave) or export cookies to ~/.config/digwire/cookies.txt")
		}
		if lastErr != nil {
			return nil, fmt.Errorf("media inspection failed: %w", lastErr)
		}
		return nil, fmt.Errorf("failed to extract media metadata")
	}

	var data ytdlpJSON
	if err := json.Unmarshal(winningStdout, &data); err != nil {
		return nil, fmt.Errorf("failed to parse media metadata: %w", err)
	}

	platform := DetectMediaPlatform(rawURL)
	if data.Extractor != "" && platform == "web" {
		platform = strings.ToLower(data.Extractor)
	}

	var durationSec int64
	switch d := data.Duration.(type) {
	case float64:
		durationSec = int64(d)
	case int64:
		durationSec = d
	case string:
		durationSec, _ = strconv.ParseInt(d, 10, 64)
	}

	cookieSource := "default"
	if len(winningCookieArgs) >= 2 {
		cookieSource = winningCookieArgs[1]
	}

	meta := &MediaMetadata{
		URL:          rawURL,
		ID:           data.ID,
		Title:        data.Title,
		Description:  data.Description,
		Thumbnail:    data.Thumbnail,
		Duration:     durationSec,
		Uploader:     data.Uploader,
		UploaderURL:  data.UploaderURL,
		Platform:     platform,
		UploadDate:   data.UploadDate,
		ViewCount:    data.ViewCount,
		DirectURL:    data.URL,
		CookieArgs:   winningCookieArgs,
		CookieSource: cookieSource,
		Subtitles:    make(map[string][]MediaSubtitle),
	}

	if meta.Title == "" {
		meta.Title = "Media " + data.ID
	}

	for _, f := range data.Formats {
		meta.Formats = append(meta.Formats, MediaFormat{
			FormatID:       f.FormatID,
			FormatNote:     f.FormatNote,
			Ext:            f.Ext,
			Resolution:     f.Resolution,
			Width:          f.Width,
			Height:         f.Height,
			FPS:            f.FPS,
			VCodec:         f.VCodec,
			ACodec:         f.ACodec,
			Filesize:       f.Filesize,
			FilesizeApprox: f.FilesizeApprox,
			TBR:            f.TBR,
			URL:            f.URL,
		})
	}

	// Merge regular subtitles and automatic captions
	for lang, subs := range data.Subtitles {
		for _, s := range subs {
			meta.Subtitles[lang] = append(meta.Subtitles[lang], MediaSubtitle{
				Ext:  s.Ext,
				URL:  s.URL,
				Name: s.Name,
			})
		}
	}
	for lang, subs := range data.AutomaticCaptions {
		if _, exists := meta.Subtitles[lang]; !exists {
			for _, s := range subs {
				meta.Subtitles[lang] = append(meta.Subtitles[lang], MediaSubtitle{
					Ext:  s.Ext,
					URL:  s.URL,
					Name: s.Name + " (Auto)",
				})
			}
		}
	}

	return meta, nil
}

// StartDownload initiates a background yt-dlp media download and swarm generator
func (mm *MediaManager) StartDownload(rawURL string, opts MediaDownloadOptions) (*MediaTask, error) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	id := HashURL(rawURL)
	if existing, exists := mm.tasks[id]; exists {
		if existing.State == "paused" {
			go existing.resume(mm.engine)
		}
		return existing, nil
	}

	task := &MediaTask{
		ID:       id,
		URL:      rawURL,
		Platform: DetectMediaPlatform(rawURL),
		State:    "inspecting",
		AddedAt:  time.Now().Unix(),
		Options:  opts,
	}

	mm.tasks[id] = task
	go task.run(mm.engine, mm.downloadDir)

	return task, nil
}

func (t *MediaTask) run(eng *Engine, baseDownloadDir string) {
	if t.Platform == "scribd" || DetectMediaPlatform(t.URL) == "scribd" || IsScribdURL(t.URL) {
		t.runScribd(eng, baseDownloadDir)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.mu.Lock()
	t.cancel = cancel
	t.State = "inspecting"
	t.Progress = 5.0
	rawURL := t.URL
	opts := t.Options
	t.mu.Unlock()

	bin, err := DetectYtDlpPath()
	if err != nil {
		t.mu.Lock()
		t.State = "failed"
		t.Error = err.Error()
		t.mu.Unlock()
		return
	}

	// Inspect metadata first
	meta, err := InspectMedia(ctx, rawURL)
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		t.mu.Lock()
		t.State = "failed"
		t.Error = fmt.Sprintf("Inspection error: %v", err)
		t.mu.Unlock()
		return
	}

	if ctx.Err() != nil {
		return
	}

	t.mu.Lock()
	t.Metadata = meta
	t.Title = meta.Title
	t.Thumbnail = meta.Thumbnail
	t.Duration = meta.Duration
	t.Uploader = meta.Uploader
	t.Platform = meta.Platform
	t.State = "downloading"
	t.Progress = 10.0
	t.mu.Unlock()

	// Prepare destination directory
	safeTitle := SanitizeMediaFilename(meta.Title)
	mediaDir := filepath.Join(baseDownloadDir, "Media", fmt.Sprintf("%s_%s", safeTitle, meta.ID))
	_ = os.MkdirAll(mediaDir, 0755)

	t.mu.Lock()
	t.DestPath = mediaDir
	t.mu.Unlock()

	// Cache thumbnail image locally
	if meta.Thumbnail != "" {
		go func(thumbURL, dir string) {
			resp, err := http.Get(thumbURL)
			if err == nil && resp.StatusCode == 200 {
				defer resp.Body.Close()
				thumbPath := filepath.Join(dir, "cover.jpg")
				if out, err := os.Create(thumbPath); err == nil {
					_, _ = io.Copy(out, resp.Body)
					out.Close()
				}
			}
		}(meta.Thumbnail, mediaDir)
	}

	// Build yt-dlp execution arguments
	args := []string{
		"--no-warnings",
		"--newline",
		"--progress",
		"--no-colors",
		"--progress-template", "download:%(progress._percent_str)s|%(progress._speed_str)s|%(progress._eta_str)s|%(progress._total_bytes_estimate_str)s",
		"--output", filepath.Join(mediaDir, "%(title)s.%(ext)s"),
		"--write-thumbnail",
		"--write-info-json",
		"--write-subs",
		"--sub-langs", "all,-live_chat",
		"--no-check-certificates",
	}

	if len(meta.CookieArgs) > 0 {
		args = append(args, meta.CookieArgs...)
	}

	if opts.AudioOnly {
		args = append(args, "-x")
		audioFormat := opts.AudioFormat
		if audioFormat == "" {
			audioFormat = "mp3"
		}
		args = append(args, "--audio-format", audioFormat, "--audio-quality", "0")
	} else if opts.Format != "" {
		switch opts.Format {
		case "1080p":
			args = append(args, "-f", "bestvideo[height<=1080]+bestaudio/best[height<=1080]/best")
		case "720p":
			args = append(args, "-f", "bestvideo[height<=720]+bestaudio/best[height<=720]/best")
		case "4k":
			args = append(args, "-f", "bestvideo[height<=2160]+bestaudio/best")
		default:
			args = append(args, "-f", opts.Format)
		}
	}

	if opts.EmbedSubs {
		args = append(args, "--embed-subs")
	}

	args = append(args, rawURL)

	cmd := exec.CommandContext(ctx, bin, args...)
	t.mu.Lock()
	t.cmd = cmd
	t.mu.Unlock()

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.mu.Lock()
		t.State = "failed"
		t.Error = err.Error()
		t.mu.Unlock()
		return
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		t.mu.Lock()
		t.State = "failed"
		t.Error = err.Error()
		t.mu.Unlock()
		return
	}

	combinedReader := io.MultiReader(stdoutPipe, stderrPipe)

	if err := cmd.Start(); err != nil {
		t.mu.Lock()
		t.State = "failed"
		t.Error = fmt.Sprintf("Failed to launch downloader: %v", err)
		t.mu.Unlock()
		return
	}

	// Stream progress in real-time
	scanner := bufio.NewScanner(combinedReader)
	for scanner.Scan() {
		line := scanner.Text()
		if pct, speed, eta, total, ok := parseYtDlpProgressLine(line); ok {
			t.mu.Lock()
			if pct > t.Progress || t.Progress < 10.0 {
				t.Progress = pct
			}
			if speed > 0 {
				t.DownloadRate = speed
			}
			if eta > 0 {
				t.ETASeconds = eta
			}
			if total > 0 {
				t.TotalBytes = total
			}
			if t.TotalBytes > 0 && t.Progress > 0 {
				t.CompletedBytes = int64(float64(t.TotalBytes) * (t.Progress / 100.0))
			}
			t.mu.Unlock()
		} else if strings.Contains(line, "[Merger]") || strings.Contains(line, "[ExtractAudio]") || strings.Contains(line, "[EmbedSubtitle]") {
			t.mu.Lock()
			t.State = "processing"
			if t.Progress < 98.0 {
				t.Progress = 98.0
			}
			t.mu.Unlock()
		}
	}

	if err := cmd.Wait(); err != nil {
		if ctx.Err() == context.Canceled {
			return
		}
		t.mu.Lock()
		t.State = "failed"
		t.Error = fmt.Sprintf("Download terminated with error: %v", err)
		t.mu.Unlock()
		return
	}

	t.mu.Lock()
	t.Progress = 100.0
	t.DownloadRate = 0
	t.ETASeconds = 0
	t.State = "processing"
	t.mu.Unlock()

	// Convert downloaded folder into deterministic BitTorrent swarm
	if eng != nil {
		t.mu.Lock()
		t.State = "creating_swarm"
		t.mu.Unlock()

		comment := fmt.Sprintf("Digwire Media Swarm: %s", rawURL)
		infoHash, magnetURI, err := eng.CreateTorrent(mediaDir, comment)
		if err == nil && infoHash != "" {
			// If direct stream URL was present, add as WebSeed
			if meta.DirectURL != "" {
				_ = eng.AddWebSeed(infoHash, meta.DirectURL)
			}

			t.mu.Lock()
			t.InfoHash = infoHash
			t.MagnetURI = magnetURI
			if eng.IsGermanyMode() {
				t.State = "completed"
			} else {
				t.State = "seeding"
			}
			t.mu.Unlock()
		} else {
			t.mu.Lock()
			t.State = "completed"
			t.mu.Unlock()
		}
	} else {
		t.mu.Lock()
		t.State = "completed"
		t.mu.Unlock()
	}
	if eng != nil {
		eng.SaveSession()
	}
}

var (
	ansiRegex     = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	stdDlRegex    = regexp.MustCompile(`\[download\]\s+([0-9.]+)%\s+of\s+~?([0-9.]+\s*[a-zA-Z/]+)(?:\s+at\s+([0-9.]+\s*[a-zA-Z/]+))?(?:\s+ETA\s+([0-9:]+))?`)
	completeRegex = regexp.MustCompile(`\[download\]\s+100(?:\.0+)?%\s+of\s+~?([0-9.]+\s*[a-zA-Z/]+)`)
	fragRegex     = regexp.MustCompile(`\[download\]\s+(?:Downloading\s+video\s+fragment|fragment)\s+(\d+)\s+of\s+(\d+)`)
)

func parseByteSize(s string) int64 {
	s = strings.TrimSpace(strings.TrimPrefix(s, "~"))
	if s == "" || s == "N/A" || s == "NA" || s == "Unknown" {
		return 0
	}
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0
	}
	numStr := fields[0]
	val, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		for i, c := range numStr {
			if (c < '0' || c > '9') && c != '.' {
				unit := strings.ToUpper(numStr[i:])
				val, _ = strconv.ParseFloat(numStr[:i], 64)
				return convertUnitToBytes(val, unit)
			}
		}
		return 0
	}
	unit := ""
	if len(fields) > 1 {
		unit = strings.ToUpper(fields[1])
	}
	return convertUnitToBytes(val, unit)
}

func convertUnitToBytes(val float64, unit string) int64 {
	unit = strings.TrimSuffix(unit, "/S")
	unit = strings.TrimSuffix(unit, "PS")
	switch unit {
	case "B", "BYTES":
		return int64(val)
	case "KB", "KIB", "K":
		return int64(val * 1024)
	case "MB", "MIB", "M":
		return int64(val * 1024 * 1024)
	case "GB", "GIB", "G":
		return int64(val * 1024 * 1024 * 1024)
	case "TB", "TIB", "T":
		return int64(val * 1024 * 1024 * 1024 * 1024)
	default:
		return int64(val)
	}
}

func parseETASec(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "N/A" || s == "NA" || s == "Unknown" {
		return 0
	}
	parts := strings.Split(s, ":")
	switch len(parts) {
	case 2:
		m, _ := strconv.ParseInt(parts[0], 10, 64)
		sec, _ := strconv.ParseInt(parts[1], 10, 64)
		return m*60 + sec
	case 3:
		h, _ := strconv.ParseInt(parts[0], 10, 64)
		m, _ := strconv.ParseInt(parts[1], 10, 64)
		sec, _ := strconv.ParseInt(parts[2], 10, 64)
		return h*3600 + m*60 + sec
	default:
		v, _ := strconv.ParseInt(s, 10, 64)
		return v
	}
}

func parseYtDlpProgressLine(rawLine string) (pct float64, speed int64, eta int64, total int64, ok bool) {
	clean := ansiRegex.ReplaceAllString(rawLine, "")
	clean = strings.TrimSpace(clean)
	if clean == "" {
		return 0, 0, 0, 0, false
	}

	// 1. Template format: pct|speed|eta|total
	if strings.Contains(clean, "|") {
		parts := strings.Split(clean, "|")
		if len(parts) >= 4 {
			pStr := strings.Trim(strings.TrimSuffix(strings.TrimSpace(parts[0]), "%"), " ")
			p, err := strconv.ParseFloat(pStr, 64)
			if err == nil {
				pct = p
				speed = parseByteSize(parts[1])
				eta = parseETASec(parts[2])
				total = parseByteSize(parts[3])
				return pct, speed, eta, total, true
			}
		}
	}

	// 2. Standard yt-dlp [download] XX.X% of ...
	if m := stdDlRegex.FindStringSubmatch(clean); len(m) >= 3 {
		p, err := strconv.ParseFloat(m[1], 64)
		if err == nil {
			pct = p
			total = parseByteSize(m[2])
			if len(m) >= 4 && m[3] != "" {
				speed = parseByteSize(m[3])
			}
			if len(m) >= 5 && m[4] != "" {
				eta = parseETASec(m[4])
			}
			return pct, speed, eta, total, true
		}
	}

	// 3. Complete line [download] 100% of ...
	if m := completeRegex.FindStringSubmatch(clean); len(m) >= 2 {
		pct = 100.0
		total = parseByteSize(m[1])
		return pct, 0, 0, total, true
	}

	// 4. Fragment line
	if m := fragRegex.FindStringSubmatch(clean); len(m) >= 3 {
		cur, _ := strconv.ParseFloat(m[1], 64)
		tot, _ := strconv.ParseFloat(m[2], 64)
		if tot > 0 {
			pct = (cur / tot) * 100.0
			return pct, 0, 0, 0, true
		}
	}

	return 0, 0, 0, 0, false
}

func (t *MediaTask) pause() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cancel != nil {
		t.cancel()
	}
	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
	}
	t.State = "paused"
	t.DownloadRate = 0
}

func (t *MediaTask) resume(eng *Engine) {
	t.mu.Lock()
	if t.State == "downloading" || t.State == "completed" || t.State == "seeding" {
		t.mu.Unlock()
		return
	}
	destPath := t.DestPath
	t.mu.Unlock()

	if eng != nil && destPath != "" {
		go t.run(eng, eng.cfg.DownloadDir)
	}
}

// GetTasks returns all current media tasks
func (mm *MediaManager) GetTasks() []*MediaTask {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	list := make([]*MediaTask, 0, len(mm.tasks))
	for _, t := range mm.tasks {
		list = append(list, t)
	}
	return list
}

// GetTask finds a task by ID, InfoHash, or URL (case-insensitive)
func (mm *MediaManager) GetTask(idOrURL string) *MediaTask {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	clean := strings.TrimSpace(idOrURL)
	if clean == "" {
		return nil
	}

	for k, t := range mm.tasks {
		if strings.EqualFold(k, clean) ||
			strings.EqualFold(t.ID, clean) ||
			strings.EqualFold(t.InfoHash, clean) ||
			strings.EqualFold(t.URL, clean) ||
			strings.EqualFold(HashURL(t.URL), clean) {
			return t
		}
	}
	return nil
}

// CancelTask cancels and cleans up a media task
func (mm *MediaManager) CancelTask(id string, deleteFiles bool) error {
	mm.mu.Lock()
	clean := strings.TrimSpace(id)
	var task *MediaTask
	var taskId string

	for k, t := range mm.tasks {
		if strings.EqualFold(k, clean) ||
			strings.EqualFold(t.ID, clean) ||
			strings.EqualFold(t.InfoHash, clean) ||
			strings.EqualFold(t.URL, clean) ||
			strings.EqualFold(HashURL(t.URL), clean) ||
			(len(clean) >= 6 && (strings.Contains(strings.ToLower(t.URL), strings.ToLower(clean)) || strings.Contains(strings.ToLower(t.ID), strings.ToLower(clean)))) {
			task = t
			taskId = k
			break
		}
	}

	if task != nil {
		task.pause()
		delete(mm.tasks, taskId)
		if task.ID != "" {
			delete(mm.tasks, task.ID)
			delete(mm.tasks, strings.ToLower(task.ID))
		}
		if task.URL != "" {
			delete(mm.tasks, task.URL)
			delete(mm.tasks, strings.ToLower(task.URL))
			delete(mm.tasks, HashURL(task.URL))
			delete(mm.tasks, strings.ToLower(HashURL(task.URL)))
		}
		if task.InfoHash != "" {
			delete(mm.tasks, task.InfoHash)
			delete(mm.tasks, strings.ToLower(task.InfoHash))
		}
	}
	mm.mu.Unlock()

	if task == nil {
		return fmt.Errorf("media task not found: %s", id)
	}

	if deleteFiles && task.DestPath != "" {
		_ = os.RemoveAll(task.DestPath)
	}

	return nil
}
