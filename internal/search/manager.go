package search

import (
	"context"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"digwire/internal/config"
)

type Manager struct {
	mu        sync.RWMutex
	providers []Provider
}

func NewManager(cfg *config.Config) *Manager {
	m := &Manager{}
	m.UpdateProviders(cfg)
	return m
}

func (m *Manager) UpdateProviders(cfg *config.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.providers = nil
	for _, pCfg := range cfg.SearchProviders {
		weight := pCfg.Weight
		if weight <= 0 {
			weight = 1.0
		}
		switch pCfg.Type {
		case "torrentscsv":
			m.providers = append(m.providers, NewTorrentsCSVProvider(pCfg.Name, pCfg.URL, pCfg.Enabled, weight))
		case "archiveorg":
			m.providers = append(m.providers, NewArchiveOrgProvider(pCfg.Name, pCfg.URL, pCfg.Enabled, weight))
		case "torznab":
			m.providers = append(m.providers, NewTorznabProvider(pCfg.Name, pCfg.URL, pCfg.APIKey, pCfg.Enabled, weight))
		}
	}
}

// CalculateRelevance computes a balanced score between keyword relevance and swarm health
func CalculateRelevance(query, title string, seeders int, weight float64) float64 {
	qLower := strings.ToLower(strings.TrimSpace(query))
	tLower := strings.ToLower(strings.TrimSpace(title))

	if qLower == "" || tLower == "" {
		return 0
	}

	var matchScore float64 = 0

	// 1. Exact or contiguous matching
	if tLower == qLower {
		matchScore += 100.0
	} else if strings.HasPrefix(tLower, qLower) {
		matchScore += 60.0
	} else if strings.Contains(tLower, qLower) {
		matchScore += 40.0
	}

	// 2. Token overlap matching
	qTokens := strings.Fields(qLower)
	matchedTokens := 0
	for _, tok := range qTokens {
		if strings.Contains(tLower, tok) {
			matchedTokens++
			matchScore += 12.0
		}
	}

	if len(qTokens) > 1 && matchedTokens == len(qTokens) {
		matchScore += 25.0 // Bonus for having all search terms
	}

	// 3. Swarm health / seeders score (Logarithmic scaling so seeds help, but don't overwhelm relevance)
	seederScore := math.Log2(float64(seeders+1)) * 4.0

	// 4. Apply provider bias weight
	if weight <= 0 {
		weight = 1.0
	}

	totalScore := (matchScore + seederScore) * weight
	return math.Round(totalScore*10) / 10
}

func (m *Manager) SearchAll(ctx context.Context, query string) []Result {
	m.mu.RLock()
	providers := make([]Provider, len(m.providers))
	copy(providers, m.providers)
	m.mu.RUnlock()

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	resultChan := make(chan []Result, len(providers))

	for _, p := range providers {
		if !p.IsEnabled() {
			continue
		}
		wg.Add(1)
		go func(prov Provider) {
			defer wg.Done()
			results, err := prov.Search(ctx, query)
			if err == nil && len(results) > 0 {
				// Compute relevance score for each result
				for i := range results {
					results[i].Score = CalculateRelevance(query, results[i].Title, results[i].Seeders, prov.Weight())
				}
				resultChan <- results
			}
		}(p)
	}

	wg.Wait()
	close(resultChan)

	var combined []Result
	seen := make(map[string]bool)

	for list := range resultChan {
		for _, item := range list {
			key := item.InfoHash
			if key == "" {
				key = item.MagnetURI
			}
			if key != "" && seen[key] {
				continue
			}
			if key != "" {
				seen[key] = true
			}
			combined = append(combined, item)
		}
	}

	// Sort primary by balanced relevance Score descending, then by seeders
	sort.Slice(combined, func(i, j int) bool {
		if combined[i].Score == combined[j].Score {
			return combined[i].Seeders > combined[j].Seeders
		}
		return combined[i].Score > combined[j].Score
	})

	return combined
}
