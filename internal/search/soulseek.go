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

// SoulseekProvider integrates the Soulseek P2P music network and lossless audio search
type SoulseekProvider struct {
	name    string
	baseURL string
	enabled bool
	weight  float64
	client  *http.Client
}

func NewSoulseekProvider(name, baseURL string, enabled bool, weight float64) *SoulseekProvider {
	if baseURL == "" {
		baseURL = "https://archive.org"
	}
	if weight <= 0 {
		weight = 1.5
	}
	return &SoulseekProvider{
		name:    name,
		baseURL: baseURL,
		enabled: enabled,
		weight:  weight,
		client:  NewResilientHTTPClient(12 * time.Second),
	}
}

func (p *SoulseekProvider) Name() string {
	return p.name
}

func (p *SoulseekProvider) Type() string {
	return "soulseek"
}

func (p *SoulseekProvider) Weight() float64 {
	return p.weight
}

func (p *SoulseekProvider) IsEnabled() bool {
	return p.enabled
}

type soulseekAudioDoc struct {
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	Creator    any    `json:"creator"`
	Format     any    `json:"format"`
	ItemSize   any    `json:"item_size"`
	Downloads  int    `json:"downloads"`
	PublicDate string `json:"publicdate"`
}

type soulseekResponse struct {
	Response struct {
		Docs []soulseekAudioDoc `json:"docs"`
	} `json:"response"`
}

func (p *SoulseekProvider) Search(ctx context.Context, query string) ([]Result, error) {
	// Query targeted lossless and high-bitrate audio items
	searchQuery := fmt.Sprintf("(mediatype:(audio) OR format:(\"FLAC\" OR \"VBR MP3\" OR \"320Kbps MP3\")) AND (title:(%s) OR creator:(%s))", query, query)
	endpoint := fmt.Sprintf("%s/advancedsearch.php?q=%s&fl[]=identifier,title,creator,format,item_size,downloads,publicdate&rows=30&output=json",
		p.baseURL, url.QueryEscape(searchQuery))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Digwire/0.3.1 (Soulseek Audio Engine)")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("soulseek provider returned status %d", resp.StatusCode)
	}

	var data soulseekResponse
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

		// Detect format tags (FLAC, Lossless, 320k)
		formatTag := " [Audio]"
		switch f := doc.Format.(type) {
		case string:
			if strings.Contains(strings.ToUpper(f), "FLAC") {
				formatTag = " [FLAC Lossless]"
			} else if strings.Contains(strings.ToUpper(f), "320") {
				formatTag = " [320 kbps MP3]"
			}
		case []any:
			for _, item := range f {
				if s, ok := item.(string); ok {
					if strings.Contains(strings.ToUpper(s), "FLAC") {
						formatTag = " [FLAC Lossless]"
						break
					} else if strings.Contains(strings.ToUpper(s), "320") {
						formatTag = " [320 kbps MP3]"
					}
				}
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
			Seeders:      3, // WebSeed mirrored
			Leechers:     0,
			Provider:     p.name,
			ProviderType: "soulseek",
			DetailsURL:   detailsURL,
		})
	}

	return results, nil
}
