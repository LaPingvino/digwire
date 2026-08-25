package dhtindex

import (
	"fmt"
	"strings"
	"time"
)

// SwarmActivity tracks hourly and daily historical presence of active seeders/peers
type SwarmActivity struct {
	LastSampled     int64   `json:"last_sampled,omitempty"`
	LastSeenHealthy int64   `json:"last_seen_healthy,omitempty"`
	LastSeeders     int     `json:"last_seeders,omitempty"`
	LastPeers       int     `json:"last_peers,omitempty"`
	TotalSamples    int     `json:"total_samples,omitempty"`
	HealthySamples  int     `json:"healthy_samples,omitempty"`
	HourlyActivity  [24]int `json:"hourly_activity,omitempty"`  // Sample count per hour (0..23 UTC)
	WeekdayActivity [7]int  `json:"weekday_activity,omitempty"` // Sample count per day (0=Sun..6=Sat UTC)
}

// HealthPrediction summarizes the tentative health and peak activity times of a swarm
type HealthPrediction struct {
	Status          string `json:"status"` // "active", "periodic", "dormant", "unknown"
	PeakWindow      string `json:"peak_window,omitempty"`
	BestDays        string `json:"best_days,omitempty"`
	LastSeenHealthy int64  `json:"last_seen_healthy,omitempty"`
	Confidence      int    `json:"confidence"` // 0..100
	Description     string `json:"description,omitempty"`
	ActiveNowLikely bool   `json:"active_now_likely"`
}

// RecordSample records a new swarm probe/scrape sample into the activity histogram
func (a *SwarmActivity) RecordSample(seeders, peers int, timestamp ...time.Time) {
	t := time.Now().UTC()
	if len(timestamp) > 0 {
		t = timestamp[0].UTC()
	}

	a.LastSampled = t.Unix()
	a.LastSeeders = seeders
	a.LastPeers = peers
	a.TotalSamples++

	if seeders > 0 || peers > 0 {
		a.LastSeenHealthy = t.Unix()
		a.HealthySamples++
		hour := t.Hour()
		if hour >= 0 && hour < 24 {
			a.HourlyActivity[hour]++
		}
		weekday := int(t.Weekday())
		if weekday >= 0 && weekday < 7 {
			a.WeekdayActivity[weekday]++
		}
	}
}

// PredictHealth evaluates the activity histogram and predicts when the swarm is most active
func (a *SwarmActivity) PredictHealth() *HealthPrediction {
	if a == nil || a.TotalSamples == 0 {
		return &HealthPrediction{
			Status:      "unknown",
			Confidence:  0,
			Description: "Swarm not yet sampled",
		}
	}

	now := time.Now().UTC()
	currentHour := now.Hour()

	// 1. If currently active (sampled within last 10 minutes with seeds)
	if a.LastSeeders > 0 && (now.Unix()-a.LastSampled) < 600 {
		return &HealthPrediction{
			Status:          "active",
			Confidence:      95,
			LastSeenHealthy: a.LastSeenHealthy,
			ActiveNowLikely: true,
			Description:     fmt.Sprintf("%d live seeders currently active", a.LastSeeders),
		}
	}

	// 2. If never seen healthy across multiple samples
	if a.HealthySamples == 0 && a.TotalSamples >= 2 {
		return &HealthPrediction{
			Status:      "dormant",
			Confidence:  85,
			Description: fmt.Sprintf("0 seeders seen across %d checks", a.TotalSamples),
		}
	}

	// 3. Analyze peak hourly activity window
	peakWindow := a.computePeakWindow()
	bestDays := a.computeBestDays()

	confidence := (a.HealthySamples * 100) / a.TotalSamples
	if confidence > 90 {
		confidence = 90
	}

	activeNowLikely := a.HourlyActivity[currentHour] > 0

	descParts := []string{}
	if peakWindow != "" {
		descParts = append(descParts, fmt.Sprintf("Active ~%s", peakWindow))
	}
	if bestDays != "" && bestDays != "Daily" {
		descParts = append(descParts, bestDays)
	}

	desc := "Periodic swarm"
	if len(descParts) > 0 {
		desc = "Periodic: " + strings.Join(descParts, " • ")
	}

	return &HealthPrediction{
		Status:          "periodic",
		PeakWindow:      peakWindow,
		BestDays:        bestDays,
		LastSeenHealthy: a.LastSeenHealthy,
		Confidence:      confidence,
		Description:     desc,
		ActiveNowLikely: activeNowLikely,
	}
}

func (a *SwarmActivity) computePeakWindow() string {
	maxVal := 0
	maxHour := -1
	for h, val := range a.HourlyActivity {
		if val > maxVal {
			maxVal = val
			maxHour = h
		}
	}

	if maxHour == -1 || maxVal == 0 {
		return ""
	}

	start := (maxHour - 2 + 24) % 24
	end := (maxHour + 2) % 24
	return fmt.Sprintf("%02d:00-%02d:00 UTC", start, end)
}

func (a *SwarmActivity) computeBestDays() string {
	weekendSum := a.WeekdayActivity[0] + a.WeekdayActivity[6]
	weekdaySum := 0
	for d := 1; d <= 5; d++ {
		weekdaySum += a.WeekdayActivity[d]
	}

	total := weekendSum + weekdaySum
	if total == 0 {
		return ""
	}

	if float64(weekendSum)/float64(total) >= 0.65 {
		return "Weekends (Sat-Sun)"
	}
	if float64(weekdaySum)/float64(total) >= 0.80 {
		return "Weekdays (Mon-Fri)"
	}

	dayNames := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	var topDays []string
	for d, count := range a.WeekdayActivity {
		if count > 0 {
			topDays = append(topDays, dayNames[d])
		}
	}
	if len(topDays) >= 6 {
		return "Daily"
	}
	if len(topDays) > 0 {
		return strings.Join(topDays, ", ")
	}
	return ""
}
