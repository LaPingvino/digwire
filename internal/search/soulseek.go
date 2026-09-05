package search

import (
	"context"
	"time"
)

// SoulseekProvider integrates the native Soulseek P2P music network
type SoulseekProvider struct {
	name       string
	serverAddr string
	enabled    bool
	weight     float64
}

func NewSoulseekProvider(name, serverAddr string, enabled bool, weight float64) *SoulseekProvider {
	if serverAddr == "" || serverAddr == "https://archive.org" {
		serverAddr = "server.slsknet.org:2242"
	}
	if weight <= 0 {
		weight = 1.5
	}
	if name == "" {
		name = "Soulseek P2P"
	}

	GetSoulseekClient().Configure(serverAddr, "", "", 0, "")

	return &SoulseekProvider{
		name:       name,
		serverAddr: serverAddr,
		enabled:    enabled,
		weight:     weight,
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

func (p *SoulseekProvider) Search(ctx context.Context, query string) ([]Result, error) {
	// Query the live Soulseek distributed network
	return GetSoulseekClient().Search(ctx, query, 6*time.Second)
}
