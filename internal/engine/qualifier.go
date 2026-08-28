package engine

import (
	"fmt"
	"math"
	"time"

	"digwire/internal/dhtindex"
)

type SwarmQualifier struct {
	Class           string  `json:"class"`            // "blockbuster", "mainstream", "cult", "long_tail", "deep_archive", "ghost_ship", "discovering"
	Label           string  `json:"label"`            // e.g. "Long Tail"
	Badge           string  `json:"badge"`            // e.g. "Long Tail"
	Description     string  `json:"description"`      // e.g. "Niche archival artifact with rare or periodic home seeders."
	EasterEgg       string  `json:"easter_egg"`       // Fun commentary / easter egg
	UptimeRatio     float64 `json:"uptime_ratio"`     // 0.0 to 1.0 (estimated seeder availability)
	EstimatedDays   float64 `json:"estimated_days"`   // e.g. 3.4
	AvailabilityETA string  `json:"availability_eta"` // formatted ETA e.g. "~3-4 days (periodic seeder)"
}

// SwarmContext encapsulates the complete real-time and historical telemetry of a swarm
type SwarmContext struct {
	TotalBytes      int64
	CompletedBytes  int64
	Seeders         int   // Currently connected seeders
	Leechers        int   // Currently connected leechers
	ActivePeers     int   // Actively connected peer sockets (stats.ActivePeers)
	TotalPeers      int   // Total known peer pool from trackers/DHT/PEX (stats.TotalPeers)
	PeakSeeders     int   // Highest observed seeders for this torrent
	PeakPeers       int   // Highest observed swarm peers
	DLRate          int64 // Instantaneous download rate
	PeakDLRate      int64 // Highest observed download rate
	AvgDLRate       int64 // Exponential moving average download rate
	IsSeeding       bool  // Completed download and actively seeding
	IsHTTP          bool  // Direct HTTP/HTTPS mirror download
	Activity        *dhtindex.SwarmActivity
	AddedAt         int64 // Timestamp when torrent was added to engine
}

// CalculateSwarmQualifier computes the mainstream/long-tail classification, uptime ratio,
// and availability-based long-tail ETA when stalled. It features a sticky high-water mark
// so swarms don't flicker or downgrade when completing download into seeding mode.
func CalculateSwarmQualifier(ctx SwarmContext) SwarmQualifier {
	if ctx.IsHTTP {
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

	now := time.Now().Unix()

	// 1. Initial Discovery Period:
	// When a torrent is brand-new (< 15 seconds), trackers and DHT are still being queried.
	// Avoid flash-labeling a blockbuster as "Ghost Ship" or "Cult Classic" before the first tracker responds.
	isBrandNew := ctx.AddedAt > 0 && (now-ctx.AddedAt) < 15
	if isBrandNew && ctx.Seeders == 0 && ctx.ActivePeers == 0 && ctx.TotalPeers == 0 && ctx.DLRate == 0 {
		return SwarmQualifier{
			Class:           "discovering",
			Label:           "Discovering",
			Badge:           "Discovering",
			Description:     "Contacting trackers and querying DHT for swarm peers...",
			EasterEgg:       "Pinging the global swarm! Listening for seeders across the DHT.",
			UptimeRatio:     0.50,
			EstimatedDays:   0,
			AvailabilityETA: "",
		}
	}

	// 2. Sticky High-Water Mark & Seeder Behavior:
	// In the BitTorrent protocol, seeders disconnect from other seeders because neither has
	// pieces to upload to the other. When a download completes and enters seeding mode,
	// ConnectedSeeders drops to 0. We use peak metrics to ensure a Blockbuster remains a Blockbuster.
	effSeeders := ctx.Seeders
	if ctx.IsSeeding {
		if ctx.PeakSeeders > 0 {
			effSeeders = ctx.PeakSeeders + 1 // Retain peak seeders + local client
		} else if effSeeders == 0 {
			effSeeders = 1 // Local client is seeding
		}
	} else if ctx.PeakSeeders > effSeeders {
		effSeeders = ctx.PeakSeeders
	}

	effPeers := ctx.TotalPeers
	if ctx.ActivePeers > effPeers {
		effPeers = ctx.ActivePeers
	}
	if ctx.PeakPeers > effPeers {
		effPeers = ctx.PeakPeers
	}
	if effPeers < effSeeders {
		effPeers = effSeeders
	}

	effDLRate := ctx.DLRate
	if ctx.PeakDLRate > effDLRate {
		effDLRate = ctx.PeakDLRate
	}

	// 3. Calculate Uptime Ratio / Duty Cycle
	var uptimeRatio float64
	var hasHistory bool
	if ctx.Activity != nil && ctx.Activity.TotalSamples > 0 {
		hasHistory = true
		uptimeRatio = ctx.Activity.UptimeDutyCycle()
		if uptimeRatio <= 0 && effSeeders > 0 {
			uptimeRatio = 0.30 // fallback if currently active
		}
	} else {
		// Sharpened heuristics based on swarm depth and throughput
		if effSeeders >= 20 || effPeers >= 40 || effDLRate >= 2*1024*1024 {
			uptimeRatio = 0.98
		} else if effSeeders >= 6 || effPeers >= 12 || effDLRate >= 350*1024 {
			uptimeRatio = 0.85
		} else if effSeeders >= 2 || effPeers >= 4 {
			uptimeRatio = 0.55
		} else if effSeeders >= 1 {
			uptimeRatio = 0.25
		} else if effPeers > 0 {
			uptimeRatio = 0.10
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

	// 4. Sharpened Swarm Archetype Classification
	var class, label, badge, desc, easterEgg string

	// Tier 1: Blockbuster
	// 20+ seeders, 40+ known swarm peers, or peak throughput >= 2 MB/s
	if effSeeders >= 20 || effPeers >= 40 || effDLRate >= 2*1024*1024 {
		class = "blockbuster"
		label = "Blockbuster"
		badge = "Blockbuster"
		if ctx.IsSeeding {
			desc = "High-velocity blockbuster swarm! You are actively seeding as a verified donor."
			easterEgg = "Cruising at lightspeed! The swarm is thriving and your seed helps keep downloads instantaneous."
		} else {
			desc = "High-velocity mainstream swarm backed by dozens of gigabit seedboxes."
			easterEgg = "Cruising at lightspeed! The swarm is so fast you could probably watch it uncompressed."
		}
	// Tier 2: Mainstream
	// 6+ seeders, 12+ known peers, or peak throughput >= 350 KB/s
	} else if effSeeders >= 6 || effPeers >= 12 || effDLRate >= 350*1024 {
		class = "mainstream"
		label = "Mainstream"
		badge = "Mainstream"
		if ctx.IsSeeding {
			desc = "Popular mainstream swarm. You are actively seeding to maintain high swarm health."
			easterEgg = "Healthy mainstream torrent. Thank you for seeding and paying it forward!"
		} else {
			desc = "Popular everyday swarm with abundant seeders and healthy throughput."
			easterEgg = "Healthy mainstream torrent. The seedboxes are singing in four-part harmony."
		}
	// Tier 3: Cult Classic
	// 2+ seeders, 4+ known peers, or peak throughput >= 50 KB/s
	} else if effSeeders >= 2 || effPeers >= 4 || effDLRate >= 50*1024 {
		class = "cult"
		label = "Cult Classic"
		badge = "Cult Classic"
		if ctx.IsSeeding {
			desc = "Cult classic community swarm. Your seed directly preserves this release for enthusiasts."
			easterEgg = "A true patron of the arts! Dedicated seeders like you keep the cult classic alive."
		} else {
			desc = "Mid-tier community swarm with steady enthusiast seeders keeping it alive."
			easterEgg = "Not on the Billboard top 40, but the dedicated fanbase refuses to let it die."
		}
	// Tier 4: Long Tail
	// 1+ seeder, 1+ peer, or healthy DHT record in past 7 days
	} else if effSeeders >= 1 || effPeers >= 1 || (hasHistory && ctx.Activity.HealthySamples > 0 && (now-ctx.Activity.LastSeenHealthy) < 86400*7) {
		class = "long_tail"
		label = "Long Tail"
		badge = "Long Tail"
		if ctx.IsSeeding {
			desc = "Niche archival treasure! You are now the guardian angel keeping this rare artifact online."
			easterEgg = "Preservation hero! In Chris Anderson's Long Tail, every seeder is an irreplaceable pillar of internet history."
		} else {
			desc = "A niche archival treasure! Seeders are periodic residential peers."
			easterEgg = "A proud resident of Chris Anderson's Long Tail! Somewhere out there, someone's Raspberry Pi is gently seeding this."
		}
	// Tier 5: Deep Archive
	// Historical samples exist in DHT indexer, but no active seeders right now
	} else if hasHistory && ctx.Activity.HealthySamples > 0 {
		class = "deep_archive"
		label = "Deep Archive"
		badge = "Deep Archive"
		desc = "Rare historical specimen. Seeders appear sporadically on weekends or night cycles."
		easterEgg = "Digital archaeology at its finest! A rare specimen waiting for a friendly night-owl seeder."
	// Tier 6: Ghost Ship
	// Completely dormant
	} else {
		class = "ghost_ship"
		label = "Ghost Ship"
		badge = "Ghost Ship"
		desc = "Dormant swarm in the Mariana Trench of P2P. Relying on DHT discovery and webseeds."
		easterEgg = "Captain, we're in the deepest waters of the DHT. Keep the peer search radar spinning."
	}

	// 5. Compute Long-Tail Availability-Based ETA
	// ONLY applicable when:
	// - File is NOT completed/seeding
	// - Download is currently stalled (DLRate == 0)
	// - Remaining bytes > 0
	var estDays float64
	var availETA string
	remainingBytes := ctx.TotalBytes - ctx.CompletedBytes

	if !ctx.IsSeeding && remainingBytes > 0 && ctx.TotalBytes > 0 && ctx.DLRate == 0 {
		effectiveSpeed := ctx.AvgDLRate
		if effectiveSpeed < 100*1024 {
			if class == "blockbuster" || class == "mainstream" {
				effectiveSpeed = 2 * 1024 * 1024 // 2 MB/s
			} else if class == "cult" {
				effectiveSpeed = 750 * 1024 // 750 KB/s
			} else {
				effectiveSpeed = 350 * 1024 // 350 KB/s typical residential upload
			}
		}

		if effSeeders > 1 {
			multiplier := math.Min(float64(effSeeders), 3.0)
			effectiveSpeed = int64(float64(effectiveSpeed) * multiplier)
		}

		dailyThroughput := float64(effectiveSpeed) * 86400.0 * uptimeRatio
		if dailyThroughput > 0 {
			estDays = float64(remainingBytes) / dailyThroughput

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
