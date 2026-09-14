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
		var magURI string
		if rec.ProtocolVersion == "hybrid" && rec.InfoHashV2 != "" {
			magURI = fmt.Sprintf("magnet:?xt=urn:btih:%s&xt=urn:btmh:1220%s&dn=%s&tr=udp%%3A%%2F%%2Ftracker.opentrackr.org%%3A1337%%2Fannounce&tr=udp%%3A%%2F%%2Fopen.stealth.si%%3A80%%2Fannounce&tr=udp%%3A%%2F%%2Ftracker.torrent.eu.org%%3A451%%2Fannounce",
				rec.InfoHash, rec.InfoHashV2, url.QueryEscape(rec.Name))
		} else if rec.ProtocolVersion == "v2" || len(rec.InfoHash) == 64 {
			magURI = fmt.Sprintf("magnet:?xt=urn:btmh:1220%s&dn=%s&tr=udp%%3A%%2F%%2Ftracker.opentrackr.org%%3A1337%%2Fannounce&tr=udp%%3A%%2F%%2Fopen.stealth.si%%3A80%%2Fannounce&tr=udp%%3A%%2F%%2Ftracker.torrent.eu.org%%3A451%%2Fannounce",
				rec.InfoHash, url.QueryEscape(rec.Name))
		} else {
			magURI = fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s&tr=udp%%3A%%2F%%2Ftracker.opentrackr.org%%3A1337%%2Fannounce&tr=udp%%3A%%2F%%2Fopen.stealth.si%%3A80%%2Fannounce&tr=udp%%3A%%2F%%2Ftracker.torrent.eu.org%%3A451%%2Fannounce",
				rec.InfoHash, url.QueryEscape(rec.Name))
		}

		var fileEntries []FileEntry
		for idx, f := range rec.Files {
			var pRoot string
			if idx < len(rec.PiecesRoots) {
				pRoot = rec.PiecesRoots[idx]
			}
			fileEntries = append(fileEntries, FileEntry{
				Path:       f,
				PiecesRoot: pRoot,
			})
		}

		results = append(results, Result{
			Title:           rec.Name,
			InfoHash:        rec.InfoHash,
			InfoHashV2:      rec.InfoHashV2,
			ProtocolVersion: rec.ProtocolVersion,
			MagnetURI:       magURI,
			SizeBytes:       rec.SizeBytes,
			Seeders:         -1, // Unknown until queried from swarm
			Leechers:        -1,
			Provider:        p.name,
			ProviderType:    "dht_local",
			PublishDate:     rec.DiscoveredAt,
			Files:           fileEntries,
		})
	}

	return results, nil
}
