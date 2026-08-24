package search

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	btDigResultBlockRegex = regexp.MustCompile(`(?s)<div class="one_result"[^>]*>(.*?)</div>\s*</div>\s*</div>`)
	btDigTitleRegex       = regexp.MustCompile(`(?s)<div class="torrent_name"[^>]*><a[^>]*>(.*?)</a>`)
	btDigMagnetRegex      = regexp.MustCompile(`href="(magnet:\?[^"]+)"`)
	btDigSizeRegex        = regexp.MustCompile(`(?s)<span class="torrent_size"[^>]*>(.*?)</span>`)
	btDigFilesRegex       = regexp.MustCompile(`(?s)<span class="torrent_files"[^>]*>(\d+)</span>`)
	htmlTagStripRegex     = regexp.MustCompile(`<[^>]*>`)
)

type BTDigProvider struct {
	name    string
	url     string
	enabled bool
	weight  float64
	client  *http.Client
}

func NewBTDigProvider(name, rawURL string, enabled bool, weight float64) *BTDigProvider {
	if rawURL == "" {
		rawURL = "https://btdig.com"
	}
	return &BTDigProvider{
		name:    name,
		url:     strings.TrimRight(rawURL, "/"),
		enabled: enabled,
		weight:  weight,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (p *BTDigProvider) Name() string    { return p.name }
func (p *BTDigProvider) Type() string    { return "btdig" }
func (p *BTDigProvider) Weight() float64 { return p.weight }
func (p *BTDigProvider) IsEnabled() bool { return p.enabled }

func (p *BTDigProvider) Search(ctx context.Context, query string) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	searchURL := fmt.Sprintf("%s/search?q=%s", p.url, url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("btdig returned status: %d", resp.StatusCode)
	}

	// Read body (up to 1MB)
	bodyBytes := make([]byte, 0, 1024*1024)
	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			bodyBytes = append(bodyBytes, buf[:n]...)
			if len(bodyBytes) >= 1024*1024 {
				break
			}
		}
		if err != nil {
			break
		}
	}
	body := string(bodyBytes)

	// Parse results blocks
	blocks := btDigResultBlockRegex.FindAllStringSubmatch(body, -1)
	var results []Result

	for _, block := range blocks {
		if len(block) < 2 {
			continue
		}
		content := block[1]

		// Extract Magnet
		magMatch := btDigMagnetRegex.FindStringSubmatch(content)
		if len(magMatch) < 2 {
			continue
		}
		magnetURI := html.UnescapeString(magMatch[1])

		// Extract InfoHash
		infoHash := extractHashFromMagnet(magnetURI)
		if infoHash == "" {
			continue
		}

		// Extract Title
		title := "Unknown"
		titleMatch := btDigTitleRegex.FindStringSubmatch(content)
		if len(titleMatch) >= 2 {
			rawTitle := htmlTagStripRegex.ReplaceAllString(titleMatch[1], "")
			title = strings.TrimSpace(html.UnescapeString(rawTitle))
		}

		// Extract Size
		var sizeBytes int64
		sizeMatch := btDigSizeRegex.FindStringSubmatch(content)
		if len(sizeMatch) >= 2 {
			rawSize := htmlTagStripRegex.ReplaceAllString(sizeMatch[1], "")
			sizeBytes = parseHumanSize(strings.TrimSpace(rawSize))
		}

		// BTDig is a pure DHT indexer; estimate reasonable baseline seeders for scoring
		seeders := 5

		score := CalculateRelevance(query, title, seeders, p.weight)

		results = append(results, Result{
			Title:        title,
			InfoHash:     strings.ToLower(infoHash),
			MagnetURI:    magnetURI,
			SizeBytes:    sizeBytes,
			Seeders:      seeders,
			Leechers:     1,
			Provider:     p.name,
			ProviderType: "btdig",
			Score:        score,
		})
	}

	return results, nil
}

func extractHashFromMagnet(magnetURI string) string {
	lower := strings.ToLower(magnetURI)
	idx := strings.Index(lower, "urn:btih:")
	if idx == -1 {
		return ""
	}
	part := magnetURI[idx+9:]
	end := strings.IndexAny(part, ";&/")
	if end != -1 {
		part = part[:end]
	}
	return strings.TrimSpace(part)
}

func parseHumanSize(sizeStr string) int64 {
	sizeStr = strings.TrimSpace(sizeStr)
	if sizeStr == "" {
		return 0
	}
	parts := strings.Fields(sizeStr)
	if len(parts) == 0 {
		return 0
	}

	val, err := strconv.ParseFloat(strings.ReplaceAll(parts[0], ",", ""), 64)
	if err != nil {
		return 0
	}

	if len(parts) < 2 {
		return int64(val)
	}

	unit := strings.ToUpper(parts[1])
	switch {
	case strings.HasPrefix(unit, "KB") || strings.HasPrefix(unit, "KIB"):
		return int64(val * 1024)
	case strings.HasPrefix(unit, "MB") || strings.HasPrefix(unit, "MIB"):
		return int64(val * 1024 * 1024)
	case strings.HasPrefix(unit, "GB") || strings.HasPrefix(unit, "GIB"):
		return int64(val * 1024 * 1024 * 1024)
	case strings.HasPrefix(unit, "TB") || strings.HasPrefix(unit, "TIB"):
		return int64(val * 1024 * 1024 * 1024 * 1024)
	case strings.HasPrefix(unit, "B"):
		return int64(val)
	default:
		return int64(val)
	}
}
