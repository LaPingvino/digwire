package search

import (
	"context"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"digwire/internal/config"
	"digwire/internal/dhtindex"
)

type Manager struct {
	mu        sync.RWMutex
	providers []Provider
	localDHT  *LocalDHTProvider
}

func NewManager(cfg *config.Config) *Manager {
	m := &Manager{}
	m.UpdateProviders(cfg)
	return m
}

func (m *Manager) SetLocalDHTIndexer(idx *dhtindex.Indexer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if idx != nil {
		m.localDHT = NewLocalDHTProvider("Local DHT Cache", idx, true, 1.5)
	}
}

func (m *Manager) UpdateProviders(cfg *config.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(cfg.FallbackDNS) > 0 {
		SetFallbackDNSServers(cfg.FallbackDNS)
	}

	m.providers = nil
	for _, pCfg := range cfg.SearchProviders {
		weight := pCfg.Weight
		if weight <= 0 {
			weight = 1.0
		}
		switch pCfg.Type {
		case "btdig":
			m.providers = append(m.providers, NewBTDigProvider(pCfg.Name, pCfg.URL, pCfg.Enabled, weight))
		case "bitsearch":
			m.providers = append(m.providers, NewBitSearchProvider(pCfg.Name, pCfg.URL, pCfg.Enabled, weight))
		case "dht", "apibay":
			m.providers = append(m.providers, NewApibayProvider(pCfg.Name, pCfg.URL, pCfg.Enabled, weight))
		case "eztv":
			m.providers = append(m.providers, NewEZTVProvider(pCfg.Name, pCfg.URL, pCfg.Enabled, weight))
		case "yts":
			m.providers = append(m.providers, NewYTSProvider(pCfg.Name, pCfg.URL, pCfg.Enabled, weight))
		case "solidtorrents":
			m.providers = append(m.providers, NewSolidTorrentsProvider(pCfg.Name, pCfg.URL, pCfg.Enabled, weight))
		case "torrentscsv":
			m.providers = append(m.providers, NewTorrentsCSVProvider(pCfg.Name, pCfg.URL, pCfg.Enabled, weight))
		case "limetorrents":
			m.providers = append(m.providers, NewLimeTorrentsProvider(pCfg.Name, pCfg.URL, pCfg.Enabled, weight))
		case "torlock":
			m.providers = append(m.providers, NewTorLockProvider(pCfg.Name, pCfg.URL, pCfg.Enabled, weight))
		case "archiveorg":
			m.providers = append(m.providers, NewArchiveOrgProvider(pCfg.Name, pCfg.URL, pCfg.Enabled, weight))
		case "torznab":
			m.providers = append(m.providers, NewTorznabProvider(pCfg.Name, pCfg.URL, pCfg.APIKey, pCfg.Enabled, weight))
		default:
			m.providers = append(m.providers, NewGenericProvider(pCfg))
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
	seederScore := 0.0
	if seeders > 0 {
		seederScore = math.Log2(float64(seeders+1)) * 4.0
	} else if seeders < 0 {
		// Unknown/unprobed swarm baseline score
		seederScore = 6.0
	}

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
	if m.localDHT != nil && m.localDHT.IsEnabled() {
		providers = append(providers, m.localDHT)
	}
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
			sI := combined[i].Seeders
			if sI < 0 {
				sI = 0
			}
			sJ := combined[j].Seeders
			if sJ < 0 {
				sJ = 0
			}
			return sI > sJ
		}
		return combined[i].Score > combined[j].Score
	})

	// Attach health predictions and log known swarm stats into DHT temporal history
	m.mu.RLock()
	locDHT := m.localDHT
	m.mu.RUnlock()

	if locDHT != nil && locDHT.indexer != nil {
		for i := range combined {
			if combined[i].InfoHash != "" {
				combined[i].Health = locDHT.indexer.GetHealthPrediction(combined[i].InfoHash)
				if combined[i].Seeders >= 0 {
					locDHT.indexer.RecordSwarmActivity(combined[i].InfoHash, combined[i].Title, combined[i].Seeders, combined[i].Leechers)
				}
			}
		}
	}

	// Auto-index discovered torrents into local DHT database
	if locDHT != nil && locDHT.indexer != nil {
		go func(items []Result) {
			for _, item := range items {
				if item.InfoHash != "" && item.Title != "" {
					locDHT.indexer.AddRecord(&dhtindex.DHTRecord{
						InfoHash:     item.InfoHash,
						Name:         item.Title,
						SizeBytes:    item.SizeBytes,
						NumFiles:     len(item.Files),
						DiscoveredAt: time.Now().Unix(),
					})
				}
			}
		}(combined)
	}

	return combined
}
