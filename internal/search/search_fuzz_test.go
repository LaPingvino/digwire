package search

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func FuzzRelevanceScoring(f *testing.F) {
	f.Add(100, 10, int64(1000000000), 1.5, "Ubuntu Desktop AMD64", "ubuntu")
	f.Add(0, 0, int64(0), 1.0, "Random File", "random")
	f.Add(99999, 1000, int64(50000000000), 2.0, "Linux Kernel", "kernel")
	f.Add(-1, -1, int64(-100), 0.5, "", "")

	f.Fuzz(func(t *testing.T, seeders int, leechers int, sizeBytes int64, weight float64, title string, query string) {
		res := Result{
			Title:     title,
			Seeders:   seeders,
			Leechers:  leechers,
			SizeBytes: sizeBytes,
			Provider:  "FuzzProvider",
		}
		score := calculateScore(res, query, weight)
		if math.IsNaN(score) || math.IsInf(score, 0) {
			t.Errorf("calculateScore returned NaN or Inf: %f", score)
		}
	})
}

func calculateScore(r Result, query string, providerWeight float64) float64 {
	score := 0.0
	lowerTitle := strings.ToLower(r.Title)
	lowerQuery := strings.ToLower(query)

	if lowerQuery != "" && strings.Contains(lowerTitle, lowerQuery) {
		score += 50.0
	}

	seeds := r.Seeders
	if seeds > 0 {
		score += math.Log10(float64(seeds)+1.0) * 15.0
	}

	if providerWeight > 0 {
		score *= providerWeight
	}

	return score
}

func FuzzApibayJSON(f *testing.F) {
	seeds := []string{
		`[{"id":"123","name":"Ubuntu 24.04","info_hash":"0123456789abcdef0123456789abcdef01234567","leechers":"10","seeders":"100","num_files":"1","size":"4000000000"}]`,
		`[]`,
		`{"error":"not found"}`,
		`[{"name":""}]`,
		`invalid json`,
		`{"nested":[{"id":1}]}`,
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, rawJSON string) {
		var apibayItems []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			InfoHash string `json:"info_hash"`
			Leechers string `json:"leechers"`
			Seeders  string `json:"seeders"`
			NumFiles string `json:"num_files"`
			Size     string `json:"size"`
		}
		_ = json.Unmarshal([]byte(rawJSON), &apibayItems)
	})
}
