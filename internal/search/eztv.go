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

type eztvResponse struct {
	TorrentsCount int           `json:"torrents_count"`
	Torrents      []eztvTorrent `json:"torrents"`
}

type eztvTorrent struct {
	ID        int    `json:"id"`
	Hash      string `json:"hash"`
	Filename  string `json:"filename"`
	Title     string `json:"title"`
	MagnetURL string `json:"magnet_url"`
	SizeBytes int64  `json:"size_bytes"`
	Seeds     int    `json:"seeds"`
	Peers     int    `json:"peers"`
}

type EZTVProvider struct {
	name    string
	url     string
	enabled bool
	weight  float64
	client  *http.Client
}

func NewEZTVProvider(name, rawURL string, enabled bool, weight float64) *EZTVProvider {
	if rawURL == "" {
		rawURL = "https://eztv.re"
	}
	return &EZTVProvider{
		name:    name,
		url:     strings.TrimRight(rawURL, "/"),
		enabled: enabled,
		weight:  weight,
		client:  NewResilientHTTPClient(10 * time.Second),
	}
}

func (p *EZTVProvider) Name() string    { return p.name }
func (p *EZTVProvider) Type() string    { return "eztv" }
func (p *EZTVProvider) Weight() float64 { return p.weight }
func (p *EZTVProvider) IsEnabled() bool { return p.enabled }

func (p *EZTVProvider) Search(ctx context.Context, query string) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	searchURL := fmt.Sprintf("%s/api/get-torrents?search_by_name=%s&limit=50", p.url, url.QueryEscape(query))
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
		return nil, fmt.Errorf("eztv returned status: %d", resp.StatusCode)
	}

	var data eztvResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	var results []Result
	for _, item := range data.Torrents {
		title := item.Filename
		if title == "" {
			title = item.Title
		}
		if title == "" {
			continue
		}

		infoHash := strings.ToLower(strings.TrimSpace(item.Hash))
		magnetURI := item.MagnetURL
		if magnetURI == "" && infoHash != "" {
			magnetURI = fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", infoHash, url.QueryEscape(title))
		}

		score := CalculateRelevance(query, title, item.Seeds, p.weight)

		results = append(results, Result{
			Title:        title,
			InfoHash:     infoHash,
			MagnetURI:    magnetURI,
			SizeBytes:    item.SizeBytes,
			Seeders:      item.Seeds,
			Leechers:     item.Peers,
			Provider:     p.name,
			ProviderType: "eztv",
			Score:        score,
		})
	}

	return results, nil
}
