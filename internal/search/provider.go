package search

import (
	"context"

	"digwire/internal/dhtindex"
)

// FileEntry represents a file inside a torrent search result
type FileEntry struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
}

// Result represents a unified torrent search result from any backend.
type Result struct {
	Title        string                     `json:"title"`
	InfoHash     string                     `json:"info_hash"`
	MagnetURI    string                     `json:"magnet_uri"`
	SizeBytes    int64                      `json:"size_bytes"`
	Seeders      int                        `json:"seeders"`
	Leechers     int                        `json:"leechers"`
	Provider     string                     `json:"provider"`
	ProviderType string                     `json:"provider_type"` // "torrentscsv", "archiveorg", "torznab", "dht", "btdig"
	Score        float64                    `json:"score"`
	DetailsURL   string                     `json:"details_url,omitempty"`
	PublishDate  int64                      `json:"publish_date,omitempty"`
	Files        []FileEntry                `json:"files,omitempty"`
	Health       *dhtindex.HealthPrediction `json:"health,omitempty"`
	Directory    string                     `json:"directory,omitempty"`
	Path         string                     `json:"path,omitempty"`
	Artist       string                     `json:"artist,omitempty"`
	Album        string                     `json:"album,omitempty"`
	User         string                     `json:"user,omitempty"`
}

// Provider is the interface that all search backends must implement.
type Provider interface {
	Name() string
	Type() string
	Weight() float64
	IsEnabled() bool
	Search(ctx context.Context, query string) ([]Result, error)
}
