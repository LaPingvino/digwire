package engine

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

// AlternateSwarm is another torrent whose files provably hold the same data as files of a local
// torrent, so its swarm can feed the same files on disk. A hybrid or v2 release of the same
// content is the typical case, but any release with matching piece hashes qualifies.
type AlternateSwarm struct {
	InfoHash        string `json:"info_hash"`
	InfoHashV2      string `json:"info_hash_v2,omitempty"`
	ProtocolVersion string `json:"protocol_version"`
	Name            string `json:"name"`
	MagnetURI       string `json:"magnet_uri"`
	Provider        string `json:"provider,omitempty"`
	Seeders         int    `json:"seeders"`
	Leechers        int    `json:"leechers"`
	MatchedFiles    int    `json:"matched_files"`
	MatchedBytes    int64  `json:"matched_bytes"`
	TotalBytes      int64  `json:"total_bytes"`
	Attached        bool   `json:"attached,omitempty"`
}

type fileMatch struct {
	oursIndex, theirsIndex int
	// theirs piece index = ours piece index + pieceDelta, for every full piece of the file.
	pieceDelta      int
	firstPiece, end int
	theirsBestPath  []string
}

type pieceLink struct {
	ours, theirs int
}

type alternateCandidate struct {
	swarm AlternateSwarm
	mi    *metainfo.MetaInfo
}

const (
	maxAlternateProbes   = 8
	alternateProbeTimeout = 10 * time.Second
)

// matchFilesByPieceHashes pairs files of two torrents whose v1 piece hashes prove identical
// content without reading any data. That needs the same piece length and the same offset of the
// file within a piece, so each full piece over the file covers the same bytes in both torrents;
// every one of those pieces must then hash the same.
func matchFilesByPieceHashes(ours, theirs *metainfo.Info) []fileMatch {
	pl := ours.PieceLength
	if pl <= 0 || pl != theirs.PieceLength || len(ours.Pieces) == 0 || len(theirs.Pieces) == 0 {
		return nil
	}
	theirFiles := theirs.UpvertedFiles()
	used := make(map[int]bool)
	var matches []fileMatch
	for oi, of := range ours.UpvertedFiles() {
		first := int((of.TorrentOffset + pl - 1) / pl)
		end := int((of.TorrentOffset + of.Length) / pl)
		if end <= first {
			continue // No full piece inside the file, nothing to prove it with.
		}
		for ti, tf := range theirFiles {
			if used[ti] || tf.Length != of.Length || tf.TorrentOffset%pl != of.TorrentOffset%pl {
				continue
			}
			delta := int((tf.TorrentOffset - of.TorrentOffset) / pl)
			if !pieceHashesEqual(ours, theirs, first, end, delta) {
				continue
			}
			used[ti] = true
			matches = append(matches, fileMatch{
				oursIndex:      oi,
				theirsIndex:    ti,
				pieceDelta:     delta,
				firstPiece:     first,
				end:            end,
				theirsBestPath: tf.BestPath(),
			})
			break
		}
	}
	return matches
}

func pieceHashesEqual(ours, theirs *metainfo.Info, first, end, delta int) bool {
	for p := first; p < end; p++ {
		q := p + delta
		if q < 0 || (q+1)*20 > len(theirs.Pieces) || (p+1)*20 > len(ours.Pieces) {
			return false
		}
		if !bytes.Equal(ours.Pieces[p*20:(p+1)*20], theirs.Pieces[q*20:(q+1)*20]) {
			return false
		}
	}
	return true
}

func pieceLinksFor(matches []fileMatch) []pieceLink {
	var links []pieceLink
	for _, m := range matches {
		for p := m.firstPiece; p < m.end; p++ {
			links = append(links, pieceLink{ours: p, theirs: p + m.pieceDelta})
		}
	}
	return links
}

func protocolOfInfo(info *metainfo.Info) string {
	switch {
	case info.HasV1() && info.HasV2():
		return "hybrid"
	case info.HasV2():
		return "v2"
	}
	return "v1"
}

func protocolRank(protocol string) int {
	switch protocol {
	case "hybrid":
		return 0
	case "v2":
		return 1
	case "":
		return 2
	}
	return 3
}

func (e *Engine) findUserTorrent(infoHashHex string) (*torrent.Torrent, *rateTracker) {
	for _, t := range e.client.Torrents() {
		if strings.EqualFold(t.InfoHash().HexString(), infoHashHex) {
			return t, e.rateMap[strings.ToLower(t.InfoHash().HexString())]
		}
	}
	return nil, nil
}

// probeMetaInfo resolves a candidate's metadata without downloading data, reusing metadata the
// DHT crawler already cached. User torrents are never touched.
func (e *Engine) probeMetaInfo(ctx context.Context, magnetURI string) (*metainfo.MetaInfo, error) {
	hash := strings.ToLower(extractInfoHash(magnetURI))
	if hash != "" {
		if mi, err := metainfo.LoadFromFile(e.getTorrentCacheFilePath(hash)); err == nil {
			return mi, nil
		}
	}
	isUser := func(h string) bool {
		e.mu.RLock()
		defer e.mu.RUnlock()
		_, ok := e.rateMap[h]
		return ok
	}
	if hash != "" && isUser(hash) {
		return nil, fmt.Errorf("already a user torrent")
	}

	t, err := e.client.AddMagnet(SuperchargeMagnet(magnetURI))
	if err != nil {
		return nil, err
	}
	hash = strings.ToLower(t.InfoHash().HexString())
	if isUser(hash) {
		return nil, fmt.Errorf("already a user torrent")
	}
	t.DisallowDataDownload()
	defer func() {
		if !isUser(hash) {
			t.Drop()
		}
	}()

	select {
	case <-t.GotInfo():
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(alternateProbeTimeout):
		return nil, fmt.Errorf("metadata timeout")
	}
	mi := t.Metainfo()
	return &mi, nil
}

// FindAlternateSwarms searches indexers for other releases of a torrent's content and returns
// those whose piece hashes prove they contain the same files, hybrid and v2 releases first.
func (e *Engine) FindAlternateSwarms(ctx context.Context, infoHashHex string) ([]AlternateSwarm, error) {
	e.mu.RLock()
	tor, _ := e.findUserTorrent(infoHashHex)
	searchMgr := e.searchMgr
	e.mu.RUnlock()
	if tor == nil {
		return nil, fmt.Errorf("torrent %s not found", infoHashHex)
	}
	info := tor.Info()
	if info == nil {
		return nil, fmt.Errorf("metadata not available yet")
	}
	if searchMgr == nil {
		return nil, fmt.Errorf("search is not available")
	}
	ourHash := strings.ToLower(tor.InfoHash().HexString())

	var largestFile int64
	for _, f := range info.UpvertedFiles() {
		largestFile = max(largestFile, f.Length)
	}

	type candidate struct {
		magnet, provider, protocol string
		seeders, leechers          int
	}
	seen := map[string]bool{ourHash: true}
	var candidates []candidate
	for _, q := range buildSearchQueries(info.BestName()) {
		for _, r := range searchMgr.SearchAll(ctx, q) {
			h := strings.ToLower(r.InfoHash)
			if h == "" || seen[h] || !strings.HasPrefix(r.MagnetURI, "magnet:") {
				continue
			}
			seen[h] = true
			if r.SizeBytes > 0 && r.SizeBytes < largestFile {
				continue
			}
			candidates = append(candidates, candidate{r.MagnetURI, r.Provider, r.ProtocolVersion, r.Seeders, r.Leechers})
		}
		if len(candidates) >= 3*maxAlternateProbes || ctx.Err() != nil {
			break
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if ri, rj := protocolRank(candidates[i].protocol), protocolRank(candidates[j].protocol); ri != rj {
			return ri < rj
		}
		return candidates[i].seeders > candidates[j].seeders
	})
	if len(candidates) > maxAlternateProbes {
		candidates = candidates[:maxAlternateProbes]
	}

	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		found  []alternateCandidate
		limits = make(chan struct{}, 4)
	)
	for _, c := range candidates {
		wg.Add(1)
		go func(c candidate) {
			defer wg.Done()
			limits <- struct{}{}
			defer func() { <-limits }()
			mi, err := e.probeMetaInfo(ctx, c.magnet)
			if err != nil {
				return
			}
			candInfo, err := mi.UnmarshalInfo()
			if err != nil {
				return
			}
			matches := matchFilesByPieceHashes(info, &candInfo)
			if len(matches) == 0 {
				return
			}
			files := info.UpvertedFiles()
			swarm := AlternateSwarm{
				InfoHash:        mi.HashInfoBytes().HexString(),
				ProtocolVersion: protocolOfInfo(&candInfo),
				Name:            candInfo.BestName(),
				Provider:        c.provider,
				Seeders:         c.seeders,
				Leechers:        c.leechers,
				MatchedFiles:    len(matches),
				TotalBytes:      candInfo.TotalLength(),
			}
			for _, m := range matches {
				swarm.MatchedBytes += files[m.oursIndex].Length
			}
			swarm.MagnetURI = c.magnet
			if mag, err := mi.MagnetV2(); err == nil {
				if mag.V2InfoHash.Ok {
					swarm.InfoHashV2 = mag.V2InfoHash.Value.HexString()
				}
				swarm.MagnetURI = mag.String()
			}
			mu.Lock()
			found = append(found, alternateCandidate{swarm: swarm, mi: mi})
			mu.Unlock()
		}(c)
	}
	wg.Wait()

	sort.SliceStable(found, func(i, j int) bool {
		if ri, rj := protocolRank(found[i].swarm.ProtocolVersion), protocolRank(found[j].swarm.ProtocolVersion); ri != rj {
			return ri < rj
		}
		return found[i].swarm.MatchedBytes > found[j].swarm.MatchedBytes
	})

	e.mu.Lock()
	defer e.mu.Unlock()
	if e.alternateSwarms == nil {
		e.alternateSwarms = make(map[string]map[string]alternateCandidate)
	}
	byHash := make(map[string]alternateCandidate)
	swarms := make([]AlternateSwarm, 0, len(found))
	for _, c := range found {
		h := strings.ToLower(c.swarm.InfoHash)
		_, c.swarm.Attached = e.rateMap[h]
		byHash[h] = c
		swarms = append(swarms, c.swarm)
	}
	e.alternateSwarms[ourHash] = byHash
	return swarms, nil
}

type nonClosingPieceCompletion struct {
	storage.PieceCompletion
}

func (nonClosingPieceCompletion) Close() error { return nil }

// mappedFileStorage stores files named in fileMap (keyed by the torrent's own file path) at the
// given paths relative to the download dir, so an attached swarm writes into the files of the
// torrent it was matched against. Other files land in their default location.
func (e *Engine) mappedFileStorage(fileMap map[string]string) storage.ClientImplCloser {
	opts := newFileClientOpts(e.cfg.DownloadDir, nonClosingPieceCompletion{e.pieceComp})
	opts.FilePathMaker = func(o storage.FilePathMakerOpts) string {
		if p, ok := fileMap[strings.Join(o.File.BestPath(), "/")]; ok {
			return p
		}
		var parts []string
		if o.Info.BestName() != metainfo.NoName {
			parts = append(parts, o.Info.BestName())
		}
		return filepath.Join(append(parts, o.File.BestPath()...)...)
	}
	return storage.NewFileOpts(opts)
}

// AttachAlternateSwarm adds a verified alternate swarm on top of a torrent's existing files. Both
// torrents then download into and seed from the same data, and pieces completed by one are
// re-verified in the other.
func (e *Engine) AttachAlternateSwarm(infoHashHex, alternateHashHex string, onVerified ...func()) (string, error) {
	ourHash := strings.ToLower(infoHashHex)
	altHash := strings.ToLower(alternateHashHex)

	e.mu.RLock()
	tor, _ := e.findUserTorrent(ourHash)
	cand, ok := e.alternateSwarms[ourHash][altHash]
	_, alreadyAdded := e.rateMap[altHash]
	e.mu.RUnlock()
	if tor == nil || tor.Info() == nil {
		return "", fmt.Errorf("torrent %s not found or has no metadata", infoHashHex)
	}
	if !ok {
		return "", fmt.Errorf("swarm %s was not verified for this torrent; search again", alternateHashHex)
	}
	if alreadyAdded {
		return "", fmt.Errorf("that swarm is already in your downloads")
	}

	candInfo, err := cand.mi.UnmarshalInfo()
	if err != nil {
		return "", err
	}
	matches := matchFilesByPieceHashes(tor.Info(), &candInfo)
	if len(matches) == 0 {
		return "", fmt.Errorf("no matching files")
	}
	ourFiles := tor.Files()
	fileMap := make(map[string]string, len(matches))
	matched := make(map[int]bool, len(matches))
	for _, m := range matches {
		fileMap[strings.Join(m.theirsBestPath, "/")] = ourFiles[m.oursIndex].Path()
		matched[m.theirsIndex] = true
	}

	spec := torrent.TorrentSpecFromMetaInfo(cand.mi)
	spec.Storage = e.mappedFileStorage(fileMap)
	newTor, _, err := e.client.AddTorrentSpec(spec)
	if err != nil {
		return "", fmt.Errorf("adding swarm: %w", err)
	}
	newHash := strings.ToLower(newTor.InfoHash().HexString())

	e.mu.Lock()
	e.initTracker(newHash, candInfo.BestName())
	tr := e.rateMap[newHash]
	tr.magnetURI = cand.swarm.MagnetURI
	tr.fileMap = fileMap
	tr.siblingHash = ourHash
	tr.skippedFiles = make(map[int]bool)
	for i := range newTor.Files() {
		if !matched[i] {
			tr.skippedFiles[i] = true
		}
	}
	if ourTr := e.rateMap[ourHash]; ourTr != nil && ourTr.isPaused {
		tr.isPaused = true
	}
	e.saveSessionLocked()
	e.mu.Unlock()

	e.saveTorrentMetainfo(newTor)
	e.ConsolidateAndVerifyForce(newTor, true, onVerified...)
	return newHash, nil
}

// syncSiblingPieces re-verifies pieces that one of two torrents sharing files has completed but the
// other has not, so both reflect data downloaded through either swarm.
func (e *Engine) syncSiblingPieces(alt *torrent.Torrent, tr *rateTracker, sibling *torrent.Torrent) {
	if alt.Info() == nil || sibling.Info() == nil || tr.isVerifying.Load() || !tr.siblingSyncing.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer tr.siblingSyncing.Store(false)
		links := pieceLinksFor(matchFilesByPieceHashes(sibling.Info(), alt.Info()))
		for _, l := range links {
			ours, theirs := sibling.Piece(l.ours), alt.Piece(l.theirs)
			oursDone, theirsDone := ours.State().Complete, theirs.State().Complete
			switch {
			case theirsDone && !oursDone:
				_ = ours.VerifyDataContext(context.Background())
			case oursDone && !theirsDone:
				_ = theirs.VerifyDataContext(context.Background())
			}
		}
	}()
}
