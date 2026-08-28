package engine

import (
	"fmt"
	"math"
	"time"

	"digwire/internal/dhtindex"
)

type SwarmQualifier struct {
	Class           string  `json:"class"`            // "blockbuster", "mainstream", "cult", "long_tail", "deep_archive", "ghost_ship"
	Label           string  `json:"label"`            // e.g. "🐢 Long Tail"
	Badge           string  `json:"badge"`            // e.g. "Long Tail"
	Description     string  `json:"description"`      // e.g. "Niche archival artifact with rare or periodic home seeders."
	EasterEgg       string  `json:"easter_egg"`       // Fun commentary / easter egg
	UptimeRatio     float64 `json:"uptime_ratio"`     // 0.0 to 1.0 (estimated seeder availability)
	EstimatedDays   float64 `json:"estimated_days"`   // e.g. 3.4
	AvailabilityETA string  `json:"availability_eta"` // formatted ETA e.g. "~3-4 days (periodic seeder)"
}

// CalculateSwarmQualifier computes the mainstream/long-tail classification, uptime ratio,
// and availability-based long-tail ETA when not downloading.
func CalculateSwarmQualifier(
	totalBytes, completedBytes int64,
	seeders, leechers, totalPeers int,
	dlRate int64,
	avgObservedRate int64,
	activity *dhtindex.SwarmActivity,
	isHTTP bool,
) SwarmQualifier {
	if isHTTP {
		return SwarmQualifier{
			Class:           "mainstream",
			Label:           "Direct CDN",
			Badge:           "Direct CDN",
			Description:     "High-speed HTTP mirror infrastructure with 100% uptime.",
			EasterEgg:       "No P2P patience required—streaming straight from high-speed web mirrors!",
			UptimeRatio:     1.0,
			EstimatedDays:   0,
			AvailabilityETA: "",
		}
	}

	// 1. Calculate Uptime Ratio / Duty Cycle
	var uptimeRatio float64
	var hasHistory bool
	if activity != nil && activity.TotalSamples > 0 {
		hasHistory = true
		uptimeRatio = activity.UptimeDutyCycle()
		if uptimeRatio <= 0 && seeders > 0 {
			uptimeRatio = 0.20 // fallback if currently active
		}
	} else {
		// Heuristic based on current swarm size
		if seeders >= 50 {
			uptimeRatio = 0.95
		} else if seeders >= 15 {
			uptimeRatio = 0.80
		} else if seeders >= 5 {
			uptimeRatio = 0.50
		} else if seeders >= 2 {
			uptimeRatio = 0.25
		} else if seeders == 1 {
			uptimeRatio = 0.15
		} else if totalPeers > 0 {
			uptimeRatio = 0.05
		} else {
			uptimeRatio = 0.01
		}
	}

	// Clamp uptime ratio between 0.01 and 1.0
	if uptimeRatio < 0.01 {
		uptimeRatio = 0.01
	} else if uptimeRatio > 1.0 {
		uptimeRatio = 1.0
	}

	// 2. Classify Swarm
	var class, label, badge, desc, easterEgg string

	if seeders >= 50 || totalPeers >= 100 {
		class = "blockbuster"
		label = "Blockbuster"
		badge = "Blockbuster"
		desc = "High-velocity mainstream swarm backed by dozens of gigabit seedboxes."
		easterEgg = "Cruising at lightspeed! The swarm is so fast you could probably watch it uncompressed."
	} else if seeders >= 12 || (seeders >= 6 && uptimeRatio >= 0.70) {
		class = "mainstream"
		label = "Mainstream"
		badge = "Mainstream"
		desc = "Popular everyday swarm with abundant seeders and healthy throughput."
		easterEgg = "Healthy mainstream torrent. The seedboxes are singing in four-part harmony."
	} else if seeders >= 4 || (seeders >= 2 && totalPeers >= 6) {
		class = "cult"
		label = "Cult Classic"
		badge = "Cult Classic"
		desc = "Mid-tier community swarm with steady enthusiast seeders keeping it alive."
		easterEgg = "Not on the Billboard top 40, but the dedicated fanbase refuses to let it die."
	} else if seeders >= 1 || (hasHistory && activity.HealthySamples > 0 && (time.Now().Unix()-activity.LastSeenHealthy) < 86400*3) {
		class = "long_tail"
		label = "Long Tail"
		badge = "Long Tail"
		desc = "A niche archival treasure! Seeders are periodic residential peers."
		easterEgg = "A proud resident of Chris Anderson's Long Tail! Somewhere out there, someone's Raspberry Pi is gently seeding this."
	} else if hasHistory && activity.HealthySamples > 0 {
		class = "deep_archive"
		label = "Deep Archive"
		badge = "Deep Archive"
		desc = "Rare historical specimen. Seeders appear sporadically on weekends or night cycles."
		easterEgg = "Digital archaeology at its finest! A rare specimen waiting for a friendly night-owl seeder."
	} else {
		class = "ghost_ship"
		label = "Ghost Ship"
		badge = "Ghost Ship"
		desc = "Dormant swarm in the Mariana Trench of P2P. Relying on DHT discovery and webseeds."
		easterEgg = "Captain, we're in the deepest waters of the DHT. Keep the peer search radar spinning."
	}

	// 3. Compute Long-Tail Availability-Based ETA
	var estDays float64
	var availETA string
	remainingBytes := totalBytes - completedBytes

	if remainingBytes > 0 && totalBytes > 0 {
		// Effective transfer speed when seeder is online:
		effectiveSpeed := avgObservedRate
		if effectiveSpeed < 100*1024 {
			// Residential upload estimate based on swarm tier
			if class == "blockbuster" || class == "mainstream" {
				effectiveSpeed = 2 * 1024 * 1024 // 2 MB/s
			} else if class == "cult" {
				effectiveSpeed = 750 * 1024 // 750 KB/s
			} else {
				effectiveSpeed = 350 * 1024 // 350 KB/s typical home seeder upload
			}
		}

		if seeders > 1 {
			multiplier := math.Min(float64(seeders), 3.0)
			effectiveSpeed = int64(float64(effectiveSpeed) * multiplier)
		}

		dailyThroughput := float64(effectiveSpeed) * 86400.0 * uptimeRatio
		if dailyThroughput > 0 {
			estDays = float64(remainingBytes) / dailyThroughput

			if dlRate == 0 {
				if estDays < 0.05 {
					availETA = "~1-2 hours (when active)"
				} else if estDays < 0.5 {
					availETA = fmt.Sprintf("~%d hours (intermittent)", int(math.Max(2, math.Round(estDays*24))))
				} else if estDays < 1.5 {
					availETA = "~1-2 days (periodic seeder)"
				} else if estDays < 7.0 {
					availETA = fmt.Sprintf("~%d days (periodic seeder)", int(math.Round(estDays)))
				} else if estDays < 14.0 {
					availETA = "~1-2 weeks (long-tail)"
				} else if estDays < 30.0 {
					availETA = fmt.Sprintf("~%d weeks (long-tail)", int(math.Round(estDays/7.0)))
				} else if estDays < 365.0 {
					availETA = fmt.Sprintf("~%d months (deep archive)", int(math.Max(1, math.Round(estDays/30.0))))
				} else {
					availETA = "> 1 year (dormant archive)"
				}
			}
		}
	}

	return SwarmQualifier{
		Class:           class,
		Label:           label,
		Badge:           badge,
		Description:     desc,
		EasterEgg:       easterEgg,
		UptimeRatio:     uptimeRatio,
		EstimatedDays:   estDays,
		AvailabilityETA: availETA,
	}
}
