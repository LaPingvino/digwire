package engine

import (
	"context"
	"log"
	"strings"
	"sync/atomic"
	"time"

	"github.com/anacrolix/torrent"
)

const (
	alternateSearchInterval = 30 * time.Minute
	httpSwarmRetryInterval  = 5 * time.Minute
	// Enough downloaded data to sample several pieces of a candidate.
	minBytesForSwarmSearch = 64 << 20
	maxHTTPSwarmSearches   = 4
)

// maybeSearchAlternateSwarm starts a background search for another swarm, ideally hybrid or v2,
// carrying an unfinished v1 torrent's files, once enough is downloaded to verify candidates against.
// The best verified swarm becomes the torrent's upgrade suggestion. Called with e.mu held.
func (e *Engine) maybeSearchAlternateSwarm(now time.Time, t *torrent.Torrent, tr *rateTracker, isComplete bool) {
	info := t.Info()
	if info == nil || isComplete || tr.isPaused || tr.siblingHash != "" || tr.suggestedSwarm != nil || e.searchMgr == nil {
		return
	}
	if protocolOfInfo(info) != "v1" || now.Sub(tr.lastAltSearch) < alternateSearchInterval {
		return
	}
	hash := strings.ToLower(t.InfoHash().HexString())
	// Files remembered as equal to v2 files need no downloaded data to be matched by root.
	if t.BytesCompleted() < min(max(minBytesForSwarmSearch, t.Length()/100), t.Length()/4) && len(e.knownPiecesRoots(hash, info)) == 0 {
		return
	}
	for _, other := range e.rateMap {
		if other.siblingHash == hash {
			return // Already has an attached swarm.
		}
	}
	// One search at a time: each probes several swarms and hashes local data.
	if !e.alternateSearchRunning.CompareAndSwap(false, true) {
		return
	}
	tr.lastAltSearch = now
	go func() {
		defer e.alternateSearchRunning.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		swarms, err := e.FindAlternateSwarms(ctx, hash)
		log.Printf("Alternate swarm search for %s: %d verified release(s), err=%v", hash, len(swarms), err)
		if err != nil || len(swarms) == 0 || swarms[0].Attached {
			return
		}
		best := swarms[0]
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.rateMap[hash] == tr {
			tr.suggestedSwarm = &SwarmSuggestion{
				InfoHash:        best.InfoHash,
				MagnetURI:       best.MagnetURI,
				Name:            best.Name,
				Seeders:         best.Seeders,
				Peers:           best.Leechers,
				TotalBytes:      best.MatchedBytes,
				Provider:        best.Provider,
				ProtocolVersion: best.ProtocolVersion,
				InfoHashV2:      best.InfoHashV2,
				VerifiedPieces:  best.VerifiedPieces,
			}
		}
	}()
}

// findAlternateSwarmsInBackground verifies other releases against a torrent's local files, so the
// DHT index learns which of their files equal this torrent's v2 files.
func (e *Engine) findAlternateSwarmsInBackground(infoHashHex string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	swarms, err := e.FindAlternateSwarms(ctx, infoHashHex)
	if err != nil {
		log.Printf("Alternate swarm search for %s failed: %v", infoHashHex, err)
		return
	}
	log.Printf("Alternate swarm search for %s: %d verified release(s)", infoHashHex, len(swarms))
}

// TorrentSwarmSuggestion returns the swarm automatically found for a torrent, if any.
func (e *Engine) TorrentSwarmSuggestion(infoHashHex string) *SwarmSuggestion {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if tr := e.rateMap[strings.ToLower(infoHashHex)]; tr != nil {
		return tr.suggestedSwarm
	}
	return nil
}

// maybeSearchHTTPSwarms looks for torrents carrying web downloads: right away, then again as more
// data arrives, since every downloaded chunk lets more candidates be verified by content.
func (e *Engine) maybeSearchHTTPSwarms(now time.Time) {
	if e.searchMgr == nil || e.httpManager == nil {
		return
	}
	e.httpManager.mu.RLock()
	tasks := make([]*HTTPTask, 0, len(e.httpManager.tasks))
	for _, task := range e.httpManager.tasks {
		tasks = append(tasks, task)
	}
	e.httpManager.mu.RUnlock()

	for _, task := range tasks {
		if task.swarmSearching.Load() {
			continue
		}
		downloaded := atomic.LoadInt64(&task.CompletedBytes)
		task.mu.Lock()
		due := task.State == "downloading" && task.SuggestedSwarm == nil &&
			task.swarmSearches < maxHTTPSwarmSearches &&
			now.Sub(task.lastSwarmSearch) >= httpSwarmRetryInterval &&
			(task.swarmSearches == 0 || downloaded-task.lastSwarmSearchBytes >= max(minBytesForSwarmSearch, task.TotalBytes/10))
		if due {
			task.swarmSearches++
			task.lastSwarmSearch = now
			task.lastSwarmSearchBytes = downloaded
		}
		task.mu.Unlock()
		if !due || !task.swarmSearching.CompareAndSwap(false, true) {
			continue
		}
		go func(task *HTTPTask) {
			defer task.swarmSearching.Store(false)
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			if sugg, err := e.FindSuggestedSwarm(ctx, task, e.searchMgr); err == nil && sugg != nil {
				task.mu.Lock()
				task.SuggestedSwarm = sugg
				task.mu.Unlock()
			}
		}(task)
	}
}
