package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type ApibayItem struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	InfoHash string `json:"info_hash"`
	Leechers string `json:"leechers"`
	Seeders  string `json:"seeders"`
	Size     string `json:"size"`
	Category string `json:"category"`
	Added    string `json:"added"`
}

type ApibayProvider struct {
	name    string
	url     string
	enabled bool
	weight  float64
	client  *http.Client
}

func NewApibayProvider(name, rawURL string, enabled bool, weight float64) *ApibayProvider {
	if rawURL == "" {
		rawURL = "https://apibay.org"
	}
	return &ApibayProvider{
		name:    name,
		url:     strings.TrimRight(rawURL, "/"),
		enabled: enabled,
		weight:  weight,
		client:  NewResilientHTTPClient(10 * time.Second),
	}
}

func (p *ApibayProvider) Name() string    { return p.name }
func (p *ApibayProvider) Type() string    { return "dht" }
func (p *ApibayProvider) Weight() float64 { return p.weight }
func (p *ApibayProvider) IsEnabled() bool { return p.enabled }

func (p *ApibayProvider) Search(ctx context.Context, query string) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	searchURL := fmt.Sprintf("%s/q.php?q=%s", p.url, url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Digwire/1.0 (Linux; x86_64)")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("apibay server returned %d", resp.StatusCode)
	}

	var items []ApibayItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		return nil, err
	}

	var results []Result
	for _, item := range items {
		// "no results" response has name "No results returned" or id "0"
		if item.ID == "0" || item.InfoHash == "" || item.InfoHash == "0000000000000000000000000000000000000000" {
			continue
		}

		seeders, _ := strconv.Atoi(item.Seeders)
		leechers, _ := strconv.Atoi(item.Leechers)
		sizeBytes, _ := strconv.ParseInt(item.Size, 10, 64)
		addedUnix, _ := strconv.ParseInt(item.Added, 10, 64)

		magURI := fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s&tr=udp%%3A%%2F%%2Ftracker.opentrackr.org%%3A1337%%2Fannounce&tr=udp%%3A%%2F%%2Fopen.stealth.si%%3A80%%2Fannounce&tr=http%%3A%%2F%%2Ftorrent.ubuntu.com%%3A6969%%2Fannounce",
			strings.ToLower(item.InfoHash), url.QueryEscape(item.Name))

		results = append(results, Result{
			Title:        item.Name,
			InfoHash:     strings.ToLower(item.InfoHash),
			MagnetURI:    magURI,
			SizeBytes:    sizeBytes,
			Seeders:      seeders,
			Leechers:     leechers,
			Provider:     p.name,
			ProviderType: "dht",
			PublishDate:  addedUnix,
		})
	}

	return results, nil
}
