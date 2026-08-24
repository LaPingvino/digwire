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

type ytsResponse struct {
	Status        string `json:"status"`
	StatusMessage string `json:"status_message"`
	Data          struct {
		MovieCount int        `json:"movie_count"`
		Movies     []ytsMovie `json:"movies"`
	} `json:"data"`
}

type ytsMovie struct {
	ID               int          `json:"id"`
	Title            string       `json:"title"`
	TitleLong        string       `json:"title_long"`
	Year             int          `json:"year"`
	Rating           float64      `json:"rating"`
	Torrents         []ytsTorrent `json:"torrents"`
}

type ytsTorrent struct {
	URL          string `json:"url"`
	Hash         string `json:"hash"`
	Quality      string `json:"quality"`
	Type         string `json:"type"`
	Seeds        int    `json:"seeds"`
	Peers        int    `json:"peers"`
	Size         string `json:"size"`
	SizeBytes    int64  `json:"size_bytes"`
	DateUploaded string `json:"date_uploaded"`
}

type YTSProvider struct {
	name    string
	url     string
	enabled bool
	weight  float64
	client  *http.Client
}

func NewYTSProvider(name, rawURL string, enabled bool, weight float64) *YTSProvider {
	if rawURL == "" {
		rawURL = "https://yts.mx"
	}
	return &YTSProvider{
		name:    name,
		url:     strings.TrimRight(rawURL, "/"),
		enabled: enabled,
		weight:  weight,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (p *YTSProvider) Name() string    { return p.name }
func (p *YTSProvider) Type() string    { return "yts" }
func (p *YTSProvider) Weight() float64 { return p.weight }
func (p *YTSProvider) IsEnabled() bool { return p.enabled }

func (p *YTSProvider) Search(ctx context.Context, query string) ([]Result, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	searchURL := fmt.Sprintf("%s/api/v2/list_movies.json?query_term=%s&limit=30", p.url, url.QueryEscape(query))
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
		return nil, fmt.Errorf("yts returned status: %d", resp.StatusCode)
	}

	var data ytsResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	var results []Result
	for _, movie := range data.Data.Movies {
		baseTitle := movie.TitleLong
		if baseTitle == "" {
			baseTitle = movie.Title
		}

		for _, t := range movie.Torrents {
			title := fmt.Sprintf("%s [%s] [%s]", baseTitle, t.Quality, strings.ToUpper(t.Type))
			infoHash := strings.ToLower(strings.TrimSpace(t.Hash))
			if infoHash == "" {
				continue
			}

			trackers := "&tr=udp://tracker.opentrackr.org:1337/announce&tr=udp://open.stealth.si:80/announce&tr=udp://tracker.torrent.eu.org:451/announce"
			magnetURI := fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s%s", infoHash, url.QueryEscape(title), trackers)

			score := CalculateRelevance(query, title, t.Seeds, p.weight)

			results = append(results, Result{
				Title:        title,
				InfoHash:     infoHash,
				MagnetURI:    magnetURI,
				SizeBytes:    t.SizeBytes,
				Seeders:      t.Seeds,
				Leechers:     t.Peers,
				Provider:     p.name,
				ProviderType: "yts",
				Score:        score,
			})
		}
	}

	return results, nil
}
