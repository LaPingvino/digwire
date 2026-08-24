package search

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var (
	bitSearchResultCardRegex = regexp.MustCompile(`(?s)<div class="bg-white rounded-lg shadow-sm border[^>]*>(.*?)</div>\s*</div>\s*</div>`)
	bitSearchTitleRegex      = regexp.MustCompile(`(?s)<h3[^>]*>\s*<a[^>]*>(.*?)</a>`)
	bitSearchMagnetRegex     = regexp.MustCompile(`href="(magnet:\?[^"]+)"`)
	bitSearchStatsRegex      = regexp.MustCompile(`(?s)<div class="stats[^"]*">(.*?)</div>`)
	bitSearchStatItemRegex   = regexp.MustCompile(`(?s)<span[^>]*>(.*?)</span>`)
)

type BitSearchProvider struct {
	name    string
	url     string
	enabled bool
	weight  float64
	client  *http.Client
}

func NewBitSearchProvider(name, rawURL string, enabled bool, weight float64) *BitSearchProvider {
	if rawURL == "" {
		rawURL = "https://bitsearch.to"
	}
	return &BitSearchProvider{
		name:    name,
		url:     strings.TrimRight(rawURL, "/"),
		enabled: enabled,
		weight:  weight,
		client:  NewResilientHTTPClient(10 * time.Second),
	}
}

func (p *BitSearchProvider) Name() string    { return p.name }
func (p *BitSearchProvider) Type() string    { return "bitsearch" }
func (p *BitSearchProvider) Weight() float64 { return p.weight }
func (p *BitSearchProvider) IsEnabled() bool { return p.enabled }

func (p *BitSearchProvider) Search(ctx context.Context, query string) ([]Result, error) {
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
		return nil, fmt.Errorf("bitsearch returned status: %d", resp.StatusCode)
	}

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

	// Fallback regex if card wrapper varies
	magnets := bitSearchMagnetRegex.FindAllStringSubmatch(body, -1)
	titles := bitSearchTitleRegex.FindAllStringSubmatch(body, -1)

	var results []Result
	count := len(magnets)
	if len(titles) < count {
		count = len(titles)
	}

	for i := 0; i < count; i++ {
		rawMagnet := html.UnescapeString(magnets[i][1])
		rawTitle := htmlTagStripRegex.ReplaceAllString(titles[i][1], "")
		title := strings.TrimSpace(html.UnescapeString(rawTitle))
		infoHash := extractHashFromMagnet(rawMagnet)
		if infoHash == "" {
			continue
		}

		seeders := 10
		score := CalculateRelevance(query, title, seeders, p.weight)

		results = append(results, Result{
			Title:        title,
			InfoHash:     strings.ToLower(infoHash),
			MagnetURI:    rawMagnet,
			SizeBytes:    0,
			Seeders:      seeders,
			Leechers:     1,
			Provider:     p.name,
			ProviderType: "bitsearch",
			Score:        score,
		})
	}

	return results, nil
}
