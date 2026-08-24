package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type solidTorrentsResponse struct {
	Results []solidTorrentItem `json:"results"`
	Hits    int                `json:"hits"`
}

type solidTorrentItem struct {
	Title    string `json:"title"`
	InfoHash string `json:"infoHash"`
	Magnet   string `json:"magnet"`
	Size     int64  `json:"size"`
	Swarm    struct {
		Seeders  int `json:"seeders"`
		Leechers int `json:"leechers"`
	} `json:"swarm"`
}

type SolidTorrentsProvider struct {
	name    string
	url     string
	enabled bool
	weight  float64
	client  *http.Client
}

func NewSolidTorrentsProvider(name, rawURL string, enabled bool, weight float64) *SolidTorrentsProvider {
	if rawURL == "" {
		rawURL = "https://solidtorrents.to"
	}
	return &SolidTorrentsProvider{
		name:    name,
		url:     strings.TrimRight(rawURL, "/"),
		enabled: enabled,
		weight:  weight,
		client:  NewResilientHTTPClient(10 * time.Second),
	}
}

func (p *SolidTorrentsProvider) Name() string    { return p.name }
func (p *SolidTorrentsProvider) Type() string    { return "solidtorrents" }
func (p *SolidTorrentsProvider) Weight() float64 { return p.weight }
func (p *SolidTorrentsProvider) IsEnabled() bool { return p.enabled }

func (p *SolidTorrentsProvider) Search(ctx context.Context, query string) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	searchURL := fmt.Sprintf("%s/api/v1/search?q=%s", p.url, url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:128.0) Gecko/20100101 Firefox/128.0")
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("solidtorrents returned status: %d", resp.StatusCode)
	}

	var data solidTorrentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	var results []Result
	for _, item := range data.Results {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			continue
		}

		infoHash := strings.ToLower(strings.TrimSpace(item.InfoHash))
		magnetURI := item.Magnet
		if magnetURI == "" && infoHash != "" {
			magnetURI = fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", infoHash, url.QueryEscape(title))
		}

		score := CalculateRelevance(query, title, item.Swarm.Seeders, p.weight)

		results = append(results, Result{
			Title:        title,
			InfoHash:     infoHash,
			MagnetURI:    magnetURI,
			SizeBytes:    item.Size,
			Seeders:      item.Swarm.Seeders,
			Leechers:     item.Swarm.Leechers,
			Provider:     p.name,
			ProviderType: "solidtorrents",
			Score:        score,
		})
	}

	return results, nil
}
