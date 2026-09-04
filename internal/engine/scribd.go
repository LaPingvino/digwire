package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	scribdDocIDRegex = regexp.MustCompile(`/(?:document|doc|presentation|book|read|embeds)/(\d+)`)
	scribdJSONIDRegex = regexp.MustCompile(`"(?:doc_id|document_id)"\s*:\s*"?(\d+)`)
	scribdTitleCleanRegex = regexp.MustCompile(`(?i)\s*\|\s*(?:PDF|Scribd|Travel|Book|Document|Free\s+Download).*$`)
)

// IsScribdURL returns true if the given URL is a Scribd document or presentation
func IsScribdURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "scribd.com" || strings.HasSuffix(host, ".scribd.com")
}

// ExtractScribdID parses the canonical document ID from a Scribd URL
func ExtractScribdID(rawURL string) string {
	if m := scribdDocIDRegex.FindStringSubmatch(rawURL); len(m) >= 2 {
		return m[1]
	}
	// Fallback to numeric parameter if present
	u, err := url.Parse(rawURL)
	if err == nil {
		if id := u.Query().Get("id"); id != "" {
			return id
		}
		if docID := u.Query().Get("doc_id"); docID != "" {
			return docID
		}
	}
	return ""
}

// InspectScribd extracts metadata, cover images, author, and description for a Scribd document
func InspectScribd(ctx context.Context, rawURL string) (*MediaMetadata, error) {
	docID := ExtractScribdID(rawURL)

	client := &http.Client{
		Timeout: 20 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Scribd page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusFound {
		return nil, fmt.Errorf("scribd returned status %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10MB limit
	if err != nil {
		return nil, fmt.Errorf("failed to read Scribd response: %w", err)
	}

	pageHTML := string(bodyBytes)

	if docID == "" {
		if m := scribdJSONIDRegex.FindStringSubmatch(pageHTML); len(m) >= 2 {
			docID = m[1]
		} else {
			docID = HashURL(rawURL)[:8]
		}
	}

	// 1. Extract Title
	title := ""
	ogTitleRegex := regexp.MustCompile(`<meta\s+property=["']og:title["']\s+content=["']([^"']+)["']`)
	if m := ogTitleRegex.FindStringSubmatch(pageHTML); len(m) >= 2 {
		title = html.UnescapeString(m[1])
	}
	if title == "" {
		titleTagRegex := regexp.MustCompile(`<title>([^<]+)</title>`)
		if m := titleTagRegex.FindStringSubmatch(pageHTML); len(m) >= 2 {
			title = html.UnescapeString(m[1])
		}
	}
	title = scribdTitleCleanRegex.ReplaceAllString(title, "")
	title = strings.TrimSpace(title)
	if title == "" {
		title = fmt.Sprintf("Scribd Document %s", docID)
	}

	// 2. Extract Description
	desc := ""
	ogDescRegex := regexp.MustCompile(`<meta\s+property=["']og:description["']\s+content=["']([^"']+)["']`)
	if m := ogDescRegex.FindStringSubmatch(pageHTML); len(m) >= 2 {
		desc = html.UnescapeString(m[1])
	}
	if desc == "" {
		metaDescRegex := regexp.MustCompile(`<meta\s+name=["']description["']\s+content=["']([^"']+)["']`)
		if m := metaDescRegex.FindStringSubmatch(pageHTML); len(m) >= 2 {
			desc = html.UnescapeString(m[1])
		}
	}

	// 3. Extract Cover Thumbnail
	thumb := ""
	ogImgRegex := regexp.MustCompile(`<meta\s+property=["']og:image["']\s+content=["']([^"']+)["']`)
	if m := ogImgRegex.FindStringSubmatch(pageHTML); len(m) >= 2 {
		thumb = m[1]
	}

	// 4. Extract Page Count
	pageCount := int64(1)
	pageCountRegex := regexp.MustCompile(`"page_count"\s*:\s*(\d+)`)
	if m := pageCountRegex.FindStringSubmatch(pageHTML); len(m) >= 2 {
		if p, err := strconv.ParseInt(m[1], 10, 64); err == nil && p > 0 {
			pageCount = p
		}
	}

	// 5. Extract Author / Creator
	author := ""
	authorRegex := regexp.MustCompile(`"(?:author|creator)"\s*:\s*(?:\{[^}]*"name"\s*:\s*"([^"]+)"|"([^"]+)")`)
	if m := authorRegex.FindStringSubmatch(pageHTML); len(m) >= 2 {
		if m[1] != "" {
			author = html.UnescapeString(m[1])
		} else if len(m) >= 3 && m[2] != "" {
			author = html.UnescapeString(m[2])
		}
	}

	return &MediaMetadata{
		URL:         rawURL,
		ID:          docID,
		Title:       title,
		Description: desc,
		Thumbnail:   thumb,
		Duration:    pageCount, // Store page count in duration field
		Uploader:    author,
		Platform:    "scribd",
		Formats: []MediaFormat{
			{FormatID: "pdf", Ext: "pdf", FormatNote: "Full PDF Document"},
			{FormatID: "epub", Ext: "epub", FormatNote: "EPUB Digital Book"},
			{FormatID: "txt", Ext: "txt", FormatNote: "Extracted Text Edition"},
			{FormatID: "html", Ext: "html", FormatNote: "Interactive Offline Reader"},
		},
	}, nil
}

// runScribd performs the full document retrieval, offline reader generation, and swarm conversion
func (t *MediaTask) runScribd(eng *Engine, baseDownloadDir string) {
	ctx, cancel := context.WithCancel(context.Background())
	t.mu.Lock()
	t.cancel = cancel
	t.State = "inspecting"
	t.Progress = 5.0
	rawURL := t.URL
	t.mu.Unlock()

	meta, err := InspectScribd(ctx, rawURL)
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		t.mu.Lock()
		t.State = "failed"
		t.Error = fmt.Sprintf("Scribd inspection error: %v", err)
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
	t.Platform = "scribd"
	t.State = "downloading"
	t.Progress = 15.0
	t.mu.Unlock()

	safeTitle := SanitizeMediaFilename(meta.Title)
	docDir := filepath.Join(baseDownloadDir, "Documents", fmt.Sprintf("%s_%s", safeTitle, meta.ID))
	_ = os.MkdirAll(docDir, 0755)

	t.mu.Lock()
	t.DestPath = docDir
	t.mu.Unlock()

	// 1. Download cover image
	var coverBytes []byte
	if meta.Thumbnail != "" {
		t.mu.Lock()
		t.Progress = 25.0
		t.mu.Unlock()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, meta.Thumbnail, nil)
		if err == nil {
			req.Header.Set("User-Agent", "Mozilla/5.0")
			if resp, err := http.DefaultClient.Do(req); err == nil && resp.StatusCode == http.StatusOK {
				data, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if len(data) > 0 {
					coverBytes = data
					coverPath := filepath.Join(docDir, "cover.jpg")
					_ = os.WriteFile(coverPath, data, 0644)
				}
			}
		}
	}

	// 2. Try direct binary document downloads with extensions: pdf, epub, txt, docx
	t.mu.Lock()
	t.Progress = 40.0
	t.mu.Unlock()

	var downloadedFileSize int64

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	for _, ext := range []string{"pdf", "epub", "txt", "docx"} {
		if ctx.Err() != nil {
			return
		}
		dlURL := fmt.Sprintf("https://www.scribd.com/document_downloads/%s?extension=%s", meta.ID, ext)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, dlURL, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36")
		req.Header.Set("Referer", rawURL)

		resp, err := client.Do(req)
		if err != nil {
			continue
		}

		ct := resp.Header.Get("Content-Type")
		isBinary := strings.Contains(ct, "pdf") || strings.Contains(ct, "epub") ||
			strings.Contains(ct, "octet-stream") || strings.Contains(ct, "wordprocessingml") ||
			(ext == "txt" && strings.Contains(ct, "text/plain"))

		if resp.StatusCode == http.StatusOK && isBinary {
			var buf bytes.Buffer
			n, err := io.Copy(&buf, io.LimitReader(resp.Body, 200<<20)) // 200MB limit
			resp.Body.Close()
			if err == nil && n > 1024 {
				// Check magic bytes for PDF or ZIP (EPUB/DOCX)
				b := buf.Bytes()
				if (ext == "pdf" && bytes.HasPrefix(b, []byte("%PDF-"))) ||
					((ext == "epub" || ext == "docx") && bytes.HasPrefix(b, []byte("PK\x03\x04"))) ||
					ext == "txt" {
					filePath := filepath.Join(docDir, fmt.Sprintf("%s.%s", safeTitle, ext))
					if err := os.WriteFile(filePath, b, 0644); err == nil {
						downloadedFileSize = n
						break
					}
				}
			}
		} else {
			resp.Body.Close()
		}
	}

	t.mu.Lock()
	t.Progress = 65.0
	t.mu.Unlock()

	// 3. Extract text content & generate offline reader package
	extractedText := extractScribdDocumentText(ctx, rawURL)
	if extractedText == "" {
		extractedText = fmt.Sprintf("%s\n\nAuthor: %s\nPages: %d\nURL: %s\n\n%s",
			meta.Title, meta.Uploader, meta.Duration, rawURL, meta.Description)
	}

	txtPath := filepath.Join(docDir, fmt.Sprintf("%s.txt", safeTitle))
	_ = os.WriteFile(txtPath, []byte(extractedText), 0644)

	// Generate clean standalone HTML reader
	htmlReaderContent := generateScribdHTMLReader(meta, extractedText, len(coverBytes) > 0)
	htmlPath := filepath.Join(docDir, fmt.Sprintf("%s.html", safeTitle))
	_ = os.WriteFile(htmlPath, []byte(htmlReaderContent), 0644)

	// Save canonical metadata JSON
	metaJSON, _ := json.MarshalIndent(meta, "", "  ")
	_ = os.WriteFile(filepath.Join(docDir, "metadata.json"), metaJSON, 0644)

	// Calculate total directory bytes
	var totalDirBytes int64
	_ = filepath.Walk(docDir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() {
			totalDirBytes += info.Size()
		}
		return nil
	})

	if downloadedFileSize == 0 {
		downloadedFileSize = totalDirBytes
	}

	t.mu.Lock()
	t.Progress = 90.0
	t.CompletedBytes = downloadedFileSize
	t.TotalBytes = totalDirBytes
	t.State = "creating_swarm"
	t.mu.Unlock()

	// 4. Convert document folder into deterministic BitTorrent swarm
	if eng != nil {
		comment := fmt.Sprintf("Digwire Scribd Document Swarm: %s", rawURL)
		infoHash, magnetURI, err := eng.CreateTorrent(docDir, comment)
		if err == nil && infoHash != "" {
			t.mu.Lock()
			t.InfoHash = infoHash
			t.MagnetURI = magnetURI
			if eng.IsGermanyMode() {
				t.State = "completed"
			} else {
				t.State = "seeding"
			}
			t.Progress = 100.0
			t.mu.Unlock()
		} else {
			t.mu.Lock()
			t.State = "completed"
			t.Progress = 100.0
			t.mu.Unlock()
		}
	} else {
		t.mu.Lock()
		t.State = "completed"
		t.Progress = 100.0
		t.mu.Unlock()
	}
}

// extractScribdDocumentText scrapes and normalizes readable document text from Scribd
func extractScribdDocumentText(ctx context.Context, rawURL string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return ""
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	pageHTML := string(bodyBytes)

	var textLines []string
	seen := make(map[string]bool)

	// Extract from text_line spans
	spanRegex := regexp.MustCompile(`<span\s+class=["'][^"']*text_line[^"']*["'][^>]*>(.*?)</span>`)
	tagStrip := regexp.MustCompile(`<[^>]+>`)

	for _, m := range spanRegex.FindAllStringSubmatch(pageHTML, -1) {
		line := strings.TrimSpace(tagStrip.ReplaceAllString(m[1], ""))
		line = html.UnescapeString(line)
		if line != "" && !seen[line] {
			seen[line] = true
			textLines = append(textLines, line)
		}
	}

	// Fallback to paragraph tags
	if len(textLines) < 3 {
		pRegex := regexp.MustCompile(`<p\b[^>]*>(.*?)</p>`)
		for _, m := range pRegex.FindAllStringSubmatch(pageHTML, -1) {
			line := strings.TrimSpace(tagStrip.ReplaceAllString(m[1], ""))
			line = html.UnescapeString(line)
			lower := strings.ToLower(line)
			if len(line) > 30 && !strings.Contains(lower, "cookie") && !strings.Contains(lower, "scribd") && !seen[line] {
				seen[line] = true
				textLines = append(textLines, line)
			}
		}
	}

	return strings.Join(textLines, "\n\n")
}

// generateScribdHTMLReader creates a self-contained Libadwaita-styled offline reader
func generateScribdHTMLReader(meta *MediaMetadata, bodyText string, hasCover bool) string {
	coverImgTag := ""
	if hasCover {
		coverImgTag = `<div class="cover-wrapper"><img src="cover.jpg" alt="Cover" class="cover-image"></div>`
	}

	formattedParagraphs := ""
	for _, p := range strings.Split(bodyText, "\n\n") {
		p = strings.TrimSpace(p)
		if p != "" {
			formattedParagraphs += fmt.Sprintf("<p>%s</p>\n", html.EscapeString(p))
		}
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>%s - Digwire Document Reader</title>
  <style>
    :root {
      --bg: #1e1e1e;
      --card-bg: #2d2d2d;
      --text: #e0e0e0;
      --dim: #888888;
      --accent: #3584e4;
      --border: rgba(255, 255, 255, 0.1);
    }
    @media (prefers-color-scheme: light) {
      :root {
        --bg: #f6f8fa;
        --card-bg: #ffffff;
        --text: #24292f;
        --dim: #57606a;
        --accent: #0969da;
        --border: rgba(0, 0, 0, 0.1);
      }
    }
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
      line-height: 1.7;
      background: var(--bg);
      color: var(--text);
      margin: 0;
      padding: 40px 20px;
      display: flex;
      justify-content: center;
    }
    .reader-container {
      max-width: 800px;
      width: 100%%;
      background: var(--card-bg);
      padding: 40px;
      border-radius: 12px;
      border: 1px solid var(--border);
      box-shadow: 0 4px 16px rgba(0, 0, 0, 0.15);
    }
    .header {
      border-bottom: 1px solid var(--border);
      padding-bottom: 24px;
      margin-bottom: 32px;
      display: flex;
      gap: 24px;
      align-items: flex-start;
    }
    .cover-image {
      max-width: 160px;
      border-radius: 8px;
      box-shadow: 0 2px 8px rgba(0,0,0,0.2);
    }
    h1 {
      margin: 0 0 12px 0;
      font-size: 26px;
      line-height: 1.3;
      color: var(--text);
    }
    .meta-info {
      font-size: 14px;
      color: var(--dim);
      margin-bottom: 8px;
    }
    .badge {
      display: inline-block;
      padding: 3px 8px;
      border-radius: 6px;
      font-size: 12px;
      font-weight: 600;
      background: rgba(46, 204, 113, 0.2);
      color: #2ecc71;
      margin-right: 8px;
    }
    .content {
      font-size: 16px;
    }
    p {
      margin-bottom: 1.4em;
    }
    .footer {
      margin-top: 40px;
      padding-top: 20px;
      border-top: 1px solid var(--border);
      font-size: 13px;
      color: var(--dim);
      text-align: center;
    }
  </style>
</head>
<body>
  <div class="reader-container">
    <div class="header">
      %s
      <div>
        <span class="badge">Scribd Archive</span>
        <h1>%s</h1>
        <div class="meta-info"><strong>Author:</strong> %s</div>
        <div class="meta-info"><strong>Pages:</strong> %d</div>
        <div class="meta-info"><a href="%s" target="_blank" style="color: var(--accent); text-decoration: none;">View Original on Scribd &rarr;</a></div>
      </div>
    </div>
    <div class="content">
      %s
    </div>
    <div class="footer">
      Archived and packaged into decentralized BitTorrent swarm by Digwire.
    </div>
  </div>
</body>
</html>`,
		html.EscapeString(meta.Title),
		coverImgTag,
		html.EscapeString(meta.Title),
		html.EscapeString(meta.Uploader),
		meta.Duration,
		html.EscapeString(meta.URL),
		formattedParagraphs,
	)
}
