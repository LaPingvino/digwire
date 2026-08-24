package search

import (
	"context"
	"fmt"
	"net/url"

	"digwire/internal/dhtindex"
)

type LocalDHTProvider struct {
	name    string
	indexer *dhtindex.Indexer
	enabled bool
	weight  float64
}

func NewLocalDHTProvider(name string, indexer *dhtindex.Indexer, enabled bool, weight float64) *LocalDHTProvider {
	if name == "" {
		name = "Local DHT Cache"
	}
	if weight <= 0 {
		weight = 1.5
	}
	return &LocalDHTProvider{
		name:    name,
		indexer: indexer,
		enabled: enabled,
		weight:  weight,
	}
}

func (p *LocalDHTProvider) Name() string    { return p.name }
func (p *LocalDHTProvider) Type() string    { return "dht_local" }
func (p *LocalDHTProvider) Weight() float64 { return p.weight }
func (p *LocalDHTProvider) IsEnabled() bool { return p.enabled && p.indexer != nil }

func (p *LocalDHTProvider) Search(ctx context.Context, query string) ([]Result, error) {
	if p.indexer == nil {
		return nil, nil
	}

	records := p.indexer.Search(query)
	var results []Result

	for _, rec := range records {
		magURI := fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s&tr=udp%%3A%%2F%%2Ftracker.opentrackr.org%%3A1337%%2Fannounce&tr=udp%%3A%%2F%%2Fopen.stealth.si%%3A80%%2Fannounce&tr=http%%3A%%2F%%2Ftorrent.ubuntu.com%%3A6969%%2Fannounce",
			rec.InfoHash, url.QueryEscape(rec.Name))

		var fileEntries []FileEntry
		for _, f := range rec.Files {
			fileEntries = append(fileEntries, FileEntry{
				Path: f,
			})
		}

		results = append(results, Result{
			Title:        rec.Name,
			InfoHash:     rec.InfoHash,
			MagnetURI:    magURI,
			SizeBytes:    rec.SizeBytes,
			Seeders:      10, // Active DHT
			Leechers:     2,
			Provider:     p.name,
			ProviderType: "dht_local",
			PublishDate:  rec.DiscoveredAt,
			Files:        fileEntries,
		})
	}

	return results, nil
}
