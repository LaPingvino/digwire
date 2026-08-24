package search

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type TorznabProvider struct {
	name    string
	baseURL string
	apiKey  string
	enabled bool
	weight  float64
	client  *http.Client
}

func NewTorznabProvider(name, baseURL, apiKey string, enabled bool, weight float64) *TorznabProvider {
	if weight <= 0 {
		weight = 1.0
	}
	return &TorznabProvider{
		name:    name,
		baseURL: baseURL,
		apiKey:  apiKey,
		enabled: enabled,
		weight:  weight,
		client:  NewResilientHTTPClient(12 * time.Second),
	}
}

func (p *TorznabProvider) Name() string {
	return p.name
}

func (p *TorznabProvider) Type() string {
	return "torznab"
}

func (p *TorznabProvider) Weight() float64 {
	return p.weight
}

func (p *TorznabProvider) IsEnabled() bool {
	return p.enabled && p.baseURL != ""
}

type torznabAttr struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type torznabItem struct {
	Title     string        `xml:"title"`
	Link      string        `xml:"link"`
	Size      int64         `xml:"size"`
	Enclosure struct {
		URL    string `xml:"url,attr"`
		Length int64  `xml:"length,attr"`
		Type   string `xml:"type,attr"`
	} `xml:"enclosure"`
	Attrs []torznabAttr `xml:"attr"`
}

type torznabRSS struct {
	Channel struct {
		Items []torznabItem `xml:"item"`
	} `xml:"channel"`
}

func (p *TorznabProvider) Search(ctx context.Context, query string) ([]Result, error) {
	u, err := url.Parse(p.baseURL)
	if err != nil {
		return nil, err
	}

	q := u.Query()
	q.Set("t", "search")
	q.Set("q", query)
	if p.apiKey != "" {
		q.Set("apikey", p.apiKey)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
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
		return nil, fmt.Errorf("torznab provider returned status code %d", resp.StatusCode)
	}

	var rss torznabRSS
	if err := xml.NewDecoder(resp.Body).Decode(&rss); err != nil {
		return nil, err
	}

	var results []Result
	for _, item := range rss.Channel.Items {
		var seeders, leechers int
		var magnetURI, infoHash string

		for _, attr := range item.Attrs {
			switch strings.ToLower(attr.Name) {
			case "seeders":
				seeders, _ = strconv.Atoi(attr.Value)
			case "peers", "leechers":
				leechers, _ = strconv.Atoi(attr.Value)
			case "magneturl":
				magnetURI = attr.Value
			case "infohash":
				infoHash = attr.Value
			}
		}

		size := item.Size
		if size == 0 && item.Enclosure.Length > 0 {
			size = item.Enclosure.Length
		}

		downloadURL := item.Link
		if magnetURI != "" {
			downloadURL = magnetURI
		} else if item.Enclosure.URL != "" {
			downloadURL = item.Enclosure.URL
		}

		results = append(results, Result{
			Title:        item.Title,
			InfoHash:     infoHash,
			MagnetURI:    downloadURL,
			SizeBytes:    size,
			Seeders:      seeders,
			Leechers:     leechers,
			Provider:     p.name,
			ProviderType: "torznab",
			DetailsURL:   item.Link,
		})
	}

	return results, nil
}
