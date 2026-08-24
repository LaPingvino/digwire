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
	torlockRowRegex   = regexp.MustCompile(`(?s)<tr[^>]*>\s*<td>(.*?)</tr>`)
	torlockTitleRegex = regexp.MustCompile(`(?s)<a href="/torrent/(\d+)/([^.]+)\.html"[^>]*>(.*?)</a>`)
	torlockSizeRegex  = regexp.MustCompile(`(?s)<td class="ts">([^<]+)</td>`)
	torlockSeedsRegex = regexp.MustCompile(`(?s)<td class="tul">([\d,]+)</td>`)
	torlockLeechRegex = regexp.MustCompile(`(?s)<td class="tdl">([\d,]+)</td>`)
)

type TorLockProvider struct {
	name    string
	url     string
	enabled bool
	weight  float64
	client  *http.Client
}

func NewTorLockProvider(name, rawURL string, enabled bool, weight float64) *TorLockProvider {
	if rawURL == "" {
		rawURL = "https://www.torlock.com"
	}
	return &TorLockProvider{
		name:    name,
		url:     strings.TrimRight(rawURL, "/"),
		enabled: enabled,
		weight:  weight,
		client:  NewResilientHTTPClient(10 * time.Second),
	}
}

func (p *TorLockProvider) Name() string    { return p.name }
func (p *TorLockProvider) Type() string    { return "torlock" }
func (p *TorLockProvider) Weight() float64 { return p.weight }
func (p *TorLockProvider) IsEnabled() bool { return p.enabled }

func (p *TorLockProvider) Search(ctx context.Context, query string) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	searchURL := fmt.Sprintf("%s/all/torrents/%s.html", p.url, url.PathEscape(query))
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
		return nil, fmt.Errorf("torlock returned status: %d", resp.StatusCode)
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

	rows := torlockRowRegex.FindAllStringSubmatch(body, -1)
	var results []Result

	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		content := row[1]

		titleMatch := torlockTitleRegex.FindStringSubmatch(content)
		if len(titleMatch) < 4 {
			continue
		}
		torrentID := titleMatch[1]
		titleSlug := titleMatch[2]
		rawTitle := htmlTagStripRegex.ReplaceAllString(titleMatch[3], "")
		title := strings.TrimSpace(html.UnescapeString(rawTitle))
		if title == "" {
			title = strings.ReplaceAll(titleSlug, "-", " ")
		}

		var sizeBytes int64
		sizeMatch := torlockSizeRegex.FindStringSubmatch(content)
		if len(sizeMatch) >= 2 {
			sizeBytes = parseHumanSize(sizeMatch[1])
		}

		var seeds int
		seedsMatch := torlockSeedsRegex.FindStringSubmatch(content)
		if len(seedsMatch) >= 2 {
			seeds, _ = strconv.Atoi(strings.ReplaceAll(seedsMatch[1], ",", ""))
		}

		var leechers int
		leechMatch := torlockLeechRegex.FindStringSubmatch(content)
		if len(leechMatch) >= 2 {
			leechers, _ = strconv.Atoi(strings.ReplaceAll(leechMatch[1], ",", ""))
		}

		detailsURL := fmt.Sprintf("%s/torrent/%s/%s.html", p.url, torrentID, titleSlug)
		score := CalculateRelevance(query, title, seeds, p.weight)

		results = append(results, Result{
			Title:        title,
			DetailsURL:   detailsURL,
			MagnetURI:    "", // Torlock uses details page torrent downloads
			SizeBytes:    sizeBytes,
			Seeders:      seeds,
			Leechers:     leechers,
			Provider:     p.name,
			ProviderType: "torlock",
			Score:        score,
		})
	}

	return results, nil
}
