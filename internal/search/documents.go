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

// DocumentProvider integrates digital libraries, book collections, and document archives
type DocumentProvider struct {
	name    string
	baseURL string
	enabled bool
	weight  float64
	client  *http.Client
}

func NewDocumentProvider(name, baseURL string, enabled bool, weight float64) *DocumentProvider {
	if baseURL == "" {
		baseURL = "https://archive.org"
	}
	if weight <= 0 {
		weight = 1.3
	}
	return &DocumentProvider{
		name:    name,
		baseURL: baseURL,
		enabled: enabled,
		weight:  weight,
		client:  NewResilientHTTPClient(12 * time.Second),
	}
}

func (p *DocumentProvider) Name() string {
	return p.name
}

func (p *DocumentProvider) Type() string {
	return "documents"
}

func (p *DocumentProvider) Weight() float64 {
	return p.weight
}

func (p *DocumentProvider) IsEnabled() bool {
	return p.enabled
}

type docItem struct {
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	Creator    any    `json:"creator"`
	Format     any    `json:"format"`
	ItemSize   any    `json:"item_size"`
	Downloads  int    `json:"downloads"`
	PublicDate string `json:"publicdate"`
}

type docResponse struct {
	Response struct {
		Docs []docItem `json:"docs"`
	} `json:"response"`
}

func (p *DocumentProvider) Search(ctx context.Context, query string) ([]Result, error) {
	// Query targeted texts, documents, books, and papers
	searchQuery := fmt.Sprintf("(mediatype:(texts) OR format:(\"Text PDF\" OR \"EPUB\")) AND (title:(%s) OR creator:(%s))", query, query)
	endpoint := fmt.Sprintf("%s/advancedsearch.php?q=%s&fl[]=identifier,title,creator,format,item_size,downloads,publicdate&rows=25&output=json",
		p.baseURL, url.QueryEscape(searchQuery))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Digwire/0.3.1 (Library & Documents Engine)")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("documents provider returned status %d", resp.StatusCode)
	}

	var data docResponse
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

		if creatorStr != "" && !strings.Contains(strings.ToLower(title), strings.ToLower(creatorStr)) {
			title = fmt.Sprintf("%s - %s", creatorStr, title)
		}

		formatTag := " [Book / Doc]"
		switch f := doc.Format.(type) {
		case string:
			if strings.Contains(strings.ToUpper(f), "EPUB") {
				formatTag = " [EPUB]"
			} else if strings.Contains(strings.ToUpper(f), "PDF") {
				formatTag = " [PDF]"
			}
		}

		fullTitle := title + formatTag

		var size int64
		switch v := doc.ItemSize.(type) {
		case float64:
			size = int64(v)
		case string:
			size, _ = strconv.ParseInt(v, 10, 64)
		}

		torrentURL := fmt.Sprintf("https://archive.org/download/%s/%s_archive.torrent", doc.Identifier, doc.Identifier)
		detailsURL := fmt.Sprintf("https://archive.org/details/%s", doc.Identifier)

		results = append(results, Result{
			Title:        fullTitle,
			MagnetURI:    torrentURL,
			SizeBytes:    size,
			Seeders:      2,
			Leechers:     0,
			Provider:     p.name,
			ProviderType: "documents",
			DetailsURL:   detailsURL,
		})
	}

	return results, nil
}
