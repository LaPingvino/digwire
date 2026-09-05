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

type ArchiveOrgProvider struct {
	name    string
	baseURL string
	enabled bool
	weight  float64
	client  *http.Client
}

func NewArchiveOrgProvider(name, baseURL string, enabled bool, weight float64) *ArchiveOrgProvider {
	if baseURL == "" {
		baseURL = "https://archive.org"
	}
	if weight <= 0 {
		weight = 0.6
	}
	return &ArchiveOrgProvider{
		name:    name,
		baseURL: baseURL,
		enabled: enabled,
		weight:  weight,
		client:  NewResilientHTTPClient(10 * time.Second),
	}
}

func (p *ArchiveOrgProvider) Name() string {
	return p.name
}

func (p *ArchiveOrgProvider) Type() string {
	return "archiveorg"
}

func (p *ArchiveOrgProvider) Weight() float64 {
	return p.weight
}

func (p *ArchiveOrgProvider) IsEnabled() bool {
	return p.enabled
}

type archiveDoc struct {
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	Creator    any    `json:"creator"`
	ItemSize   any    `json:"item_size"`
	Downloads  int    `json:"downloads"`
	PublicDate string `json:"publicdate"`
}

type archiveResponse struct {
	Response struct {
		Docs []archiveDoc `json:"docs"`
	} `json:"response"`
}

func (p *ArchiveOrgProvider) Search(ctx context.Context, query string) ([]Result, error) {
	// Search specifically targeting title or identifier to ensure high relevance
	searchQuery := fmt.Sprintf("(title:(%s) OR identifier:(%s)) AND format:\"Archive BitTorrent\"", query, query)
	endpoint := fmt.Sprintf("%s/advancedsearch.php?q=%s&fl[]=identifier,title,creator,item_size,downloads,publicdate&rows=25&output=json",
		p.baseURL, url.QueryEscape(searchQuery))

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
		return nil, fmt.Errorf("archive.org returned status %d", resp.StatusCode)
	}

	var data archiveResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(data.Response.Docs))
	for _, doc := range data.Response.Docs {
		if doc.Identifier == "" {
			continue
		}
		title := doc.Title
		if title == "" {
			title = doc.Identifier
		}

		creatorStr := ""
		switch c := doc.Creator.(type) {
		case string:
			creatorStr = c
		case []any:
			var parts []string
			for _, item := range c {
				if s, ok := item.(string); ok {
					parts = append(parts, s)
				}
			}
			creatorStr = strings.Join(parts, ", ")
		}

		var size int64
		switch v := doc.ItemSize.(type) {
		case float64:
			size = int64(v)
		case string:
			size, _ = strconv.ParseInt(v, 10, 64)
		}

		torrentURL := fmt.Sprintf("https://archive.org/download/%s/%s_archive.torrent", doc.Identifier, doc.Identifier)
		detailsURL := fmt.Sprintf("https://archive.org/details/%s", doc.Identifier)

		var dirPath string
		if creatorStr != "" && doc.Title != "" {
			dirPath = fmt.Sprintf("%s / %s", creatorStr, doc.Title)
		} else if creatorStr != "" {
			dirPath = creatorStr
		} else {
			dirPath = doc.Title
		}

		results = append(results, Result{
			Title:        title,
			MagnetURI:    torrentURL,
			SizeBytes:    size,
			Seeders:      -1, // Direct HTTP WebSeed & swarm probeable
			Leechers:     -1,
			Provider:     p.name,
			ProviderType: "archiveorg",
			DetailsURL:   detailsURL,
			Artist:       creatorStr,
			Album:        doc.Title,
			Directory:    dirPath,
			Path:         dirPath,
			User:         doc.Identifier,
		})
	}

	return results, nil
}
