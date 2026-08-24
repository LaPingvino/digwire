package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type TorrentsCSVProvider struct {
	name    string
	baseURL string
	enabled bool
	weight  float64
	client  *http.Client
}

func NewTorrentsCSVProvider(name, baseURL string, enabled bool, weight float64) *TorrentsCSVProvider {
	if baseURL == "" {
		baseURL = "https://torrents-csv.com"
	}
	if weight <= 0 {
		weight = 1.0
	}
	return &TorrentsCSVProvider{
		name:    name,
		baseURL: baseURL,
		enabled: enabled,
		weight:  weight,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (p *TorrentsCSVProvider) Name() string {
	return p.name
}

func (p *TorrentsCSVProvider) Type() string {
	return "torrentscsv"
}

func (p *TorrentsCSVProvider) Weight() float64 {
	return p.weight
}

func (p *TorrentsCSVProvider) IsEnabled() bool {
	return p.enabled
}

type torrentsCSVItem struct {
	Name        string `json:"name"`
	InfoHash    string `json:"infohash"`
	SizeBytes   int64  `json:"size_bytes"`
	CreatedUnix int64  `json:"created_unix"`
	Seeders     int    `json:"seeders"`
	Leechers    int    `json:"leechers"`
	ID          int    `json:"id"`
}

type torrentsCSVResponse struct {
	Torrents []torrentsCSVItem `json:"torrents"`
}

func (p *TorrentsCSVProvider) Search(ctx context.Context, query string) ([]Result, error) {
	endpoint := fmt.Sprintf("%s/service/search?q=%s", p.baseURL, url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Digwire/1.0")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("torrents-csv returned status code %d", resp.StatusCode)
	}

	var data torrentsCSVResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(data.Torrents))
	for _, t := range data.Torrents {
		magnet := fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", t.InfoHash, url.QueryEscape(t.Name))
		results = append(results, Result{
			Title:        t.Name,
			InfoHash:     t.InfoHash,
			MagnetURI:    magnet,
			SizeBytes:    t.SizeBytes,
			Seeders:      t.Seeders,
			Leechers:     t.Leechers,
			Provider:     p.name,
			ProviderType: "torrentscsv",
			PublishDate:  t.CreatedUnix,
		})
	}

	return results, nil
}
