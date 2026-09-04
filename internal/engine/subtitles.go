package engine

import (
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
	"strings"
	"time"
)

// SubtitleTrack represents an available or attached subtitle track
type SubtitleTrack struct {
	ID              string  `json:"id"`
	Title           string  `json:"title"`
	Language        string  `json:"language"`
	LanguageCode    string  `json:"language_code"`
	Format          string  `json:"format"` // "srt", "vtt", "ass"
	Downloads       int     `json:"downloads,omitempty"`
	Rating          float64 `json:"rating,omitempty"`
	HearingImpaired bool    `json:"hearing_impaired,omitempty"`
	DownloadURL     string  `json:"download_url,omitempty"`
	Provider        string  `json:"provider"` // "OpenSubtitles", "YTS", "Embedded", "Local"
	IsEmbedded      bool    `json:"is_embedded,omitempty"`
	StreamIndex     int     `json:"stream_index,omitempty"`
	AttachedPath    string  `json:"attached_path,omitempty"`
	IsAttached      bool    `json:"is_attached"`
}

// SubtitleSearchOptions specifies query parameters for subtitle discovery
type SubtitleSearchOptions struct {
	Query     string `json:"query"`
	Language  string `json:"language"` // "all", "en", "es", "fr", "de", "it", "pt", "ru", "zh", "ja", "ko", "eo", etc.
	Hash      string `json:"hash"`
	FilePath  string `json:"file_path"`
	ImdbID    string `json:"imdb_id,omitempty"`
}

// MapLanguageCode converts language codes to friendly names
func MapLanguageCode(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	switch code {
	case "en", "eng":
		return "English"
	case "es", "spa":
		return "Spanish"
	case "fr", "fre", "fra":
		return "French"
	case "de", "ger", "deu":
		return "German"
	case "it", "ita":
		return "Italian"
	case "pt", "por", "pt-br":
		return "Portuguese"
	case "ru", "rus":
		return "Russian"
	case "zh", "chi", "zho":
		return "Chinese"
	case "ja", "jpn":
		return "Japanese"
	case "ko", "kor":
		return "Korean"
	case "ar", "ara":
		return "Arabic"
	case "eo", "epo":
		return "Esperanto"
	case "nl", "dut", "nld":
		return "Dutch"
	case "pl", "pol":
		return "Polish"
	case "tr", "tur":
		return "Turkish"
	case "sv", "swe":
		return "Swedish"
	case "no", "nor":
		return "Norwegian"
	case "da", "dan":
		return "Danish"
	case "fi", "fin":
		return "Finnish"
	case "hi", "hin":
		return "Hindi"
	default:
		if len(code) > 0 {
			return strings.ToUpper(code[:1]) + code[1:]
		}
		return "Unknown"
	}
}

// CleanMediaTitleForSubtitles extracts a clean movie/series title from torrent names
func CleanMediaTitleForSubtitles(name string) string {
	name = strings.ReplaceAll(name, ".", " ")
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.ReplaceAll(name, "-", " ")

	// Stop at quality/codec indicators
	noiseWords := []string{
		"1080p", "720p", "2160p", "4k", "uhd", "hdr", "bluray", "blu-ray", "bdrip", "brrip",
		"web-dl", "webrip", "hdrip", "dvdrip", "x264", "x265", "hevc", "avc", "aac", "dts",
		"ac3", "atmos", "proper", "repack", "remux", "yts", "eztv", "rarbg", "vostfr", "ita",
	}

	lower := strings.ToLower(name)
	earliestIdx := len(name)
	for _, word := range noiseWords {
		idx := strings.Index(lower, " "+word)
		if idx != -1 && idx < earliestIdx {
			earliestIdx = idx
		}
	}

	clean := strings.TrimSpace(name[:earliestIdx])
	if clean == "" {
		return strings.TrimSpace(name)
	}
	return clean
}

// ScanAttachedSubtitles checks the local directory for existing subtitle files
func ScanAttachedSubtitles(videoPath string) []SubtitleTrack {
	var tracks []SubtitleTrack
	if videoPath == "" {
		return tracks
	}

	dir := filepath.Dir(videoPath)
	videoBase := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))

	entries, err := os.ReadDir(dir)
	if err != nil {
		return tracks
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext == ".srt" || ext == ".vtt" || ext == ".ass" || ext == ".sub" {
			trackPath := filepath.Join(dir, name)
			
			// Detect language from filename (e.g. Movie.en.srt or Movie.eng.srt)
			subBase := strings.TrimSuffix(name, ext)
			langCode := "en"
			langName := "English"

			parts := strings.Split(subBase, ".")
			if len(parts) > 1 {
				candidateCode := strings.ToLower(parts[len(parts)-1])
				if len(candidateCode) == 2 || len(candidateCode) == 3 {
					langCode = candidateCode
					langName = MapLanguageCode(langCode)
				}
			}

			tracks = append(tracks, SubtitleTrack{
				ID:           name,
				Title:        name,
				Language:     langName,
				LanguageCode: langCode,
				Format:       strings.TrimPrefix(ext, "."),
				Provider:     "Local File",
				AttachedPath: trackPath,
				IsAttached:   true,
			})
		}
	}

	_ = videoBase
	return tracks
}

// ScanEmbeddedSubtitles probes video container streams using ffprobe if installed
func ScanEmbeddedSubtitles(videoPath string) []SubtitleTrack {
	var tracks []SubtitleTrack
	if videoPath == "" {
		return tracks
	}
	if fi, err := os.Stat(videoPath); err != nil || fi.IsDir() {
		return tracks
	}

	probeBin, err := exec.LookPath("ffprobe")
	if err != nil {
		return tracks
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, probeBin,
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams",
		"-select_streams", "s",
		videoPath,
	)

	out, err := cmd.Output()
	if err != nil {
		return tracks
	}

	type streamInfo struct {
		Index     int `json:"index"`
		CodecName string `json:"codec_name"`
		Tags      struct {
			Language string `json:"language"`
			Title    string `json:"title"`
		} `json:"tags"`
	}
	var probeData struct {
		Streams []streamInfo `json:"streams"`
	}

	if err := json.Unmarshal(out, &probeData); err == nil {
		for _, s := range probeData.Streams {
			langCode := s.Tags.Language
			if langCode == "" {
				langCode = "und"
			}
			title := s.Tags.Title
			if title == "" {
				title = fmt.Sprintf("Embedded Track #%d (%s)", s.Index, MapLanguageCode(langCode))
			}

			tracks = append(tracks, SubtitleTrack{
				ID:           fmt.Sprintf("embedded_%d", s.Index),
				Title:        title,
				Language:     MapLanguageCode(langCode),
				LanguageCode: langCode,
				Format:       s.CodecName,
				Provider:     "Embedded Stream",
				IsEmbedded:   true,
				StreamIndex:  s.Index,
				IsAttached:   true,
			})
		}
	}

	return tracks
}

// SearchOpenSubtitles queries OpenSubtitles API for subtitle tracks
func SearchOpenSubtitles(ctx context.Context, title string, langCode string) ([]SubtitleTrack, error) {
	var tracks []SubtitleTrack
	if title == "" {
		return tracks, nil
	}

	client := &http.Client{Timeout: 10 * time.Second}

	if langCode == "" || langCode == "all" {
		langCode = "en,es,fr,de,it,pt,ru,zh,ja,ko,eo,nl"
	}

	endpoint := fmt.Sprintf("https://api.opensubtitles.com/api/v1/subtitles?query=%s&languages=%s",
		url.QueryEscape(title), url.QueryEscape(langCode))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Digwire v0.3.0")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		// Fallback to OpenSubtitles legacy REST endpoint if v1 rate limits or fails
		return searchOpenSubtitlesFallback(ctx, title, langCode)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return searchOpenSubtitlesFallback(ctx, title, langCode)
	}

	var data struct {
		Data []struct {
			ID         string `json:"id"`
			Attributes struct {
				Language        string  `json:"language"`
				DownloadCount   int     `json:"download_count"`
				Ratings         float64 `json:"ratings"`
				HearingImpaired bool    `json:"hearing_impaired"`
				Release         string  `json:"release"`
				Files           []struct {
					FileID   int    `json:"file_id"`
					FileName string `json:"file_name"`
				} `json:"files"`
				URL string `json:"url"`
			} `json:"attributes"`
		} `json:"data"`
	}

	if err := json.Unmarshal(respBody(resp.Body), &data); err != nil {
		return nil, err
	}

	for _, item := range data.Data {
		fn := item.Attributes.Release
		if len(item.Attributes.Files) > 0 && item.Attributes.Files[0].FileName != "" {
			fn = item.Attributes.Files[0].FileName
		}
		if fn == "" {
			fn = fmt.Sprintf("%s (%s)", title, item.Attributes.Language)
		}

		dlURL := item.Attributes.URL
		if len(item.Attributes.Files) > 0 && item.Attributes.Files[0].FileID > 0 {
			dlURL = fmt.Sprintf("https://dl.opensubtitles.org/en/download/sub/%d", item.Attributes.Files[0].FileID)
		}

		tracks = append(tracks, SubtitleTrack{
			ID:              item.ID,
			Title:           fn,
			Language:        MapLanguageCode(item.Attributes.Language),
			LanguageCode:    item.Attributes.Language,
			Format:          "srt",
			Downloads:       item.Attributes.DownloadCount,
			Rating:          item.Attributes.Ratings,
			HearingImpaired: item.Attributes.HearingImpaired,
			DownloadURL:     dlURL,
			Provider:        "OpenSubtitles",
			IsAttached:      false,
		})
	}

	return tracks, nil
}

func respBody(r io.Reader) []byte {
	b, _ := io.ReadAll(r)
	return b
}

func searchOpenSubtitlesFallback(ctx context.Context, title string, langCode string) ([]SubtitleTrack, error) {
	// Query public subtitle aggregator or YTS subtitles mirror
	var tracks []SubtitleTrack
	client := &http.Client{Timeout: 8 * time.Second}

	endpoint := fmt.Sprintf("https://yts-subs.com/ajax/search?query=%s", url.QueryEscape(title))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return tracks, nil
	}
	req.Header.Set("User-Agent", "Digwire v0.3.0")

	resp, err := client.Do(req)
	if err != nil {
		return tracks, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var ytsData struct {
			Subtitles []struct {
				ID       string `json:"id"`
				Lang     string `json:"lang"`
				Title    string `json:"title"`
				URL      string `json:"url"`
				Rating   int    `json:"rating"`
				Hi       bool   `json:"hi"`
			} `json:"subtitles"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&ytsData); err == nil {
			for _, sub := range ytsData.Subtitles {
				tracks = append(tracks, SubtitleTrack{
					ID:              sub.ID,
					Title:           sub.Title,
					Language:        MapLanguageCode(sub.Lang),
					LanguageCode:    sub.Lang,
					Format:          "srt",
					Rating:          float64(sub.Rating),
					HearingImpaired: sub.Hi,
					DownloadURL:     sub.URL,
					Provider:        "YTS Subtitles",
					IsAttached:      false,
				})
			}
		}
	}

	return tracks, nil
}

// DownloadAndAttachSubtitle downloads an external subtitle file and saves it next to the video
func DownloadAndAttachSubtitle(ctx context.Context, targetDir, videoFilename, dlURL, langCode string) (string, error) {
	if targetDir == "" {
		return "", fmt.Errorf("target directory is empty")
	}
	_ = os.MkdirAll(targetDir, 0755)

	videoBase := strings.TrimSuffix(videoFilename, filepath.Ext(videoFilename))
	if videoBase == "" {
		videoBase = "subtitle"
	}
	if langCode == "" {
		langCode = "en"
	}

	outFilename := fmt.Sprintf("%s.%s.srt", videoBase, langCode)
	outPath := filepath.Join(targetDir, outFilename)

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dlURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Digwire v0.3.0")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to download subtitle: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("subtitle server responded with status: %s", resp.Status)
	}

	f, err := os.Create(outPath)
	if err != nil {
		return "", fmt.Errorf("failed to save subtitle file: %w", err)
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to write subtitle data: %w", err)
	}

	return outPath, nil
}

// ExtractEmbeddedSubtitle extracts an embedded subtitle track to a standalone .srt file
func ExtractEmbeddedSubtitle(videoPath string, streamIndex int, langCode string) (string, error) {
	ffmpegBin, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", fmt.Errorf("ffmpeg not found on system: %w", err)
	}

	dir := filepath.Dir(videoPath)
	base := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	if langCode == "" {
		langCode = "sub"
	}

	outPath := filepath.Join(dir, fmt.Sprintf("%s.track%d.%s.srt", base, streamIndex, langCode))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, ffmpegBin,
		"-y",
		"-i", videoPath,
		"-map", fmt.Sprintf("0:%d", streamIndex),
		outPath,
	)

	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("extraction failed: %s", errBuf.String())
	}

	return outPath, nil
}
