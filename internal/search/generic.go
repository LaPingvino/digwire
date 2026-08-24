package search

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"digwire/internal/config"
)

type GenericProvider struct {
	cfg    config.SearchProviderConfig
	client *http.Client
}

func NewGenericProvider(pCfg config.SearchProviderConfig) *GenericProvider {
	return &GenericProvider{
		cfg: pCfg,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (p *GenericProvider) Name() string    { return p.cfg.Name }
func (p *GenericProvider) Type() string    { return p.cfg.Type }
func (p *GenericProvider) Weight() float64 { return p.cfg.Weight }
func (p *GenericProvider) IsEnabled() bool { return p.cfg.Enabled }

func (p *GenericProvider) Search(ctx context.Context, query string) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	searchURL := strings.ReplaceAll(p.cfg.URL, "{query}", url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0")
	if p.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
		req.Header.Set("X-Api-Key", p.cfg.APIKey)
	}

	if p.cfg.Type == "generic_json" {
		req.Header.Set("Accept", "application/json")
	} else {
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("provider %s returned status: %d", p.cfg.Name, resp.StatusCode)
	}

	if p.cfg.Type == "generic_json" {
		return p.parseJSON(resp.Body, query)
	}
	return p.parseHTML(resp.Body, query)
}

func (p *GenericProvider) parseJSON(r interface{ Read([]byte) (int, error) }, query string) ([]Result, error) {
	var raw interface{}
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, err
	}

	items := extractArrayByPath(raw, p.cfg.ResultsPath)
	var results []Result

	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		title := getStringByPath(m, p.cfg.TitlePath)
		if title == "" {
			continue
		}

		infoHash := getStringByPath(m, p.cfg.HashPath)
		magnetURI := getStringByPath(m, p.cfg.MagnetPath)
		if magnetURI == "" && infoHash != "" {
			magnetURI = fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", infoHash, url.QueryEscape(title))
		}
		if infoHash == "" && magnetURI != "" {
			infoHash = extractHashFromMagnet(magnetURI)
		}

		sizeBytes := getInt64ByPath(m, p.cfg.SizePath)
		seeders := int(getInt64ByPath(m, p.cfg.SeedsPath))
		leechers := int(getInt64ByPath(m, p.cfg.PeersPath))

		score := CalculateRelevance(query, title, seeders, p.cfg.Weight)

		results = append(results, Result{
			Title:        title,
			InfoHash:     strings.ToLower(infoHash),
			MagnetURI:    magnetURI,
			SizeBytes:    sizeBytes,
			Seeders:      seeders,
			Leechers:     leechers,
			Provider:     p.cfg.Name,
			ProviderType: "custom_json",
			Score:        score,
		})
	}

	return results, nil
}

func (p *GenericProvider) parseHTML(r interface{ Read([]byte) (int, error) }, query string) ([]Result, error) {
	bodyBytes := make([]byte, 0, 1024*1024)
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
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

	rowRegex := regexp.MustCompile(fallbackPattern(p.cfg.RowRegex, `(?s)<tr[^>]*>(.*?)</tr>`))
	titleRegex := regexp.MustCompile(fallbackPattern(p.cfg.TitleRegex, `(?s)<a[^>]*class="[^"]*title[^"]*"[^>]*>(.*?)</a>`))
	magnetRegex := regexp.MustCompile(fallbackPattern(p.cfg.MagnetRegex, `href="(magnet:\?[^"]+)"`))
	sizeRegex := regexp.MustCompile(fallbackPattern(p.cfg.SizeRegex, `(?s)<td[^>]*class="[^"]*size[^"]*"[^>]*>([^<]+)</td>`))
	seedsRegex := regexp.MustCompile(fallbackPattern(p.cfg.SeedsRegex, `(?s)<td[^>]*class="[^"]*seed[^"]*"[^>]*>([\d,]+)</td>`))

	rows := rowRegex.FindAllStringSubmatch(body, -1)
	var results []Result

	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		content := row[1]

		titleMatch := titleRegex.FindStringSubmatch(content)
		if len(titleMatch) < 2 {
			continue
		}
		title := strings.TrimSpace(html.UnescapeString(htmlTagStripRegex.ReplaceAllString(titleMatch[1], "")))
		if title == "" {
			continue
		}

		var magnetURI string
		magMatch := magnetRegex.FindStringSubmatch(content)
		if len(magMatch) >= 2 {
			magnetURI = html.UnescapeString(magMatch[1])
		}

		infoHash := extractHashFromMagnet(magnetURI)
		var sizeBytes int64
		sizeMatch := sizeRegex.FindStringSubmatch(content)
		if len(sizeMatch) >= 2 {
			sizeBytes = parseHumanSize(sizeMatch[1])
		}

		var seeds int
		seedsMatch := seedsRegex.FindStringSubmatch(content)
		if len(seedsMatch) >= 2 {
			seeds, _ = strconv.Atoi(strings.ReplaceAll(seedsMatch[1], ",", ""))
		}

		score := CalculateRelevance(query, title, seeds, p.cfg.Weight)

		results = append(results, Result{
			Title:        title,
			InfoHash:     strings.ToLower(infoHash),
			MagnetURI:    magnetURI,
			SizeBytes:    sizeBytes,
			Seeders:      seeds,
			Leechers:     0,
			Provider:     p.cfg.Name,
			ProviderType: "custom_html",
			Score:        score,
		})
	}

	return results, nil
}

func fallbackPattern(pattern, def string) string {
	if strings.TrimSpace(pattern) == "" {
		return def
	}
	return pattern
}

func extractArrayByPath(data interface{}, path string) []interface{} {
	if path == "" {
		if arr, ok := data.([]interface{}); ok {
			return arr
		}
		return nil
	}

	parts := strings.Split(path, ".")
	curr := data
	for _, p := range parts {
		m, ok := curr.(map[string]interface{})
		if !ok {
			return nil
		}
		curr = m[p]
	}

	if arr, ok := curr.([]interface{}); ok {
		return arr
	}
	return nil
}

func getStringByPath(m map[string]interface{}, path string) string {
	if path == "" {
		return ""
	}
	parts := strings.Split(path, ".")
	var curr interface{} = m
	for _, p := range parts {
		subM, ok := curr.(map[string]interface{})
		if !ok {
			return ""
		}
		curr = subM[p]
	}
	if s, ok := curr.(string); ok {
		return s
	}
	if curr != nil {
		return fmt.Sprintf("%v", curr)
	}
	return ""
}

func getInt64ByPath(m map[string]interface{}, path string) int64 {
	if path == "" {
		return 0
	}
	parts := strings.Split(path, ".")
	var curr interface{} = m
	for _, p := range parts {
		subM, ok := curr.(map[string]interface{})
		if !ok {
			return 0
		}
		curr = subM[p]
	}
	switch v := curr.(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	case string:
		return parseHumanSize(v)
	}
	return 0
}
