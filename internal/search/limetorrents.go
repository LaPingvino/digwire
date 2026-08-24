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
	limeRowRegex    = regexp.MustCompile(`(?s)<tr[^>]*>\s*<td class="tdleft">(.*?)</tr>`)
	limeTitleRegex  = regexp.MustCompile(`(?s)<div class="tt-name"><a[^>]*>(.*?)</a>`)
	limeMagnetRegex = regexp.MustCompile(`href="(magnet:\?[^"]+)"`)
	limeHashRegex   = regexp.MustCompile(`([a-fA-F0-9]{40})`)
	limeSizeRegex   = regexp.MustCompile(`(?s)<td class="tdnormal">([^<]+)</td>`)
	limeSeedsRegex  = regexp.MustCompile(`(?s)<td class="tdseed">([\d,]+)</td>`)
	limeLeechRegex  = regexp.MustCompile(`(?s)<td class="tdleech">([\d,]+)</td>`)
)

type LimeTorrentsProvider struct {
	name    string
	url     string
	enabled bool
	weight  float64
	client  *http.Client
}

func NewLimeTorrentsProvider(name, rawURL string, enabled bool, weight float64) *LimeTorrentsProvider {
	if rawURL == "" {
		rawURL = "https://www.limetorrents.lol"
	}
	return &LimeTorrentsProvider{
		name:    name,
		url:     strings.TrimRight(rawURL, "/"),
		enabled: enabled,
		weight:  weight,
		client:  NewResilientHTTPClient(10 * time.Second),
	}
}

func (p *LimeTorrentsProvider) Name() string    { return p.name }
func (p *LimeTorrentsProvider) Type() string    { return "limetorrents" }
func (p *LimeTorrentsProvider) Weight() float64 { return p.weight }
func (p *LimeTorrentsProvider) IsEnabled() bool { return p.enabled }

func (p *LimeTorrentsProvider) Search(ctx context.Context, query string) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	searchURL := fmt.Sprintf("%s/search/all/%s/", p.url, url.PathEscape(query))
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
		return nil, fmt.Errorf("limetorrents returned status: %d", resp.StatusCode)
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

	rows := limeRowRegex.FindAllStringSubmatch(body, -1)
	var results []Result

	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		content := row[1]

		titleMatch := limeTitleRegex.FindStringSubmatch(content)
		if len(titleMatch) < 2 {
			continue
		}
		title := strings.TrimSpace(html.UnescapeString(htmlTagStripRegex.ReplaceAllString(titleMatch[1], "")))
		if title == "" {
			continue
		}

		var magnetURI string
		magMatch := limeMagnetRegex.FindStringSubmatch(content)
		if len(magMatch) >= 2 {
			magnetURI = html.UnescapeString(magMatch[1])
		}

		infoHash := extractHashFromMagnet(magnetURI)
		if infoHash == "" {
			hashMatch := limeHashRegex.FindStringSubmatch(content)
			if len(hashMatch) >= 2 {
				infoHash = hashMatch[1]
				magnetURI = fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", infoHash, url.QueryEscape(title))
			}
		}

		if infoHash == "" {
			continue
		}

		var sizeBytes int64
		sizeMatch := limeSizeRegex.FindStringSubmatch(content)
		if len(sizeMatch) >= 2 {
			sizeBytes = parseHumanSize(sizeMatch[1])
		}

		var seeds int
		seedsMatch := limeSeedsRegex.FindStringSubmatch(content)
		if len(seedsMatch) >= 2 {
			seeds, _ = strconv.Atoi(strings.ReplaceAll(seedsMatch[1], ",", ""))
		}

		var leechers int
		leechMatch := limeLeechRegex.FindStringSubmatch(content)
		if len(leechMatch) >= 2 {
			leechers, _ = strconv.Atoi(strings.ReplaceAll(leechMatch[1], ",", ""))
		}

		score := CalculateRelevance(query, title, seeds, p.weight)

		results = append(results, Result{
			Title:        title,
			InfoHash:     strings.ToLower(infoHash),
			MagnetURI:    magnetURI,
			SizeBytes:    sizeBytes,
			Seeders:      seeds,
			Leechers:     leechers,
			Provider:     p.name,
			ProviderType: "limetorrents",
			Score:        score,
		})
	}

	return results, nil
}
