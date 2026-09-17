package engine

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
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
// content is the typical case, but any release with matching hashes qualifies.
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
	VerifiedPieces  int    `json:"verified_pieces"`
	TotalBytes      int64  `json:"total_bytes"`
	Attached        bool   `json:"attached,omitempty"`
}

type fileMatch struct {
	oursIndex, theirsIndex int
	theirsBestPath         []string
	// Number of hashes that proved the match.
	checked int
}

type alternateCandidate struct {
	swarm   AlternateSwarm
	mi      *metainfo.MetaInfo
	matches []fileMatch
}

// swarmCandidate is a tentative swarm for some content, to be verified before it is trusted.
type swarmCandidate struct {
	magnet, provider, protocol string
	seeders, leechers          int
}

const (
	maxAlternateProbes    = 8
	alternateProbeTimeout = 10 * time.Second
	// Pure v2 metadata carries no piece hashes; peers send them on request after metadata.
	pieceLayersTimeout = 20 * time.Second
	// Smallest file size worth a DHT index lookup; small sizes collide too often to be useful.
	minFileSizeForLookup = 1 << 20
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
				theirsBestPath: tf.BestPath(),
				checked:        end - first,
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

// matchTorrentFiles pairs files of a local torrent with a candidate's files holding the same data:
// by identical piece hashes where the piece grids line up, otherwise by hashing the parts of our
// files that are already downloaded against the candidate's expected hashes.
func (e *Engine) matchTorrentFiles(tor *torrent.Torrent, mi *metainfo.MetaInfo, theirs *metainfo.Info) []fileMatch {
	matches := matchFilesByPieceHashes(tor.Info(), theirs)
	usedOurs := make(map[int]bool)
	usedTheirs := make(map[int]bool)
	for _, m := range matches {
		usedOurs[m.oursIndex] = true
		usedTheirs[m.theirsIndex] = true
	}
	theirFiles := theirs.UpvertedFiles()
	for oi := range tor.Files() {
		if usedOurs[oi] {
			continue
		}
		ld := torrentFileData(e.cfg.DownloadDir, tor, oi)
		for ti, tf := range theirFiles {
			if usedTheirs[ti] || tf.Length != ld.length {
				continue
			}
			if checked, ok := verifyFileBySampling(ld, mi, theirs, tf); ok {
				usedTheirs[ti] = true
				matches = append(matches, fileMatch{oursIndex: oi, theirsIndex: ti, theirsBestPath: tf.BestPath(), checked: checked})
				break
			}
		}
	}
	return matches
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
// DHT crawler already cached. For pure v2 torrents it also waits for piece layers so partial data
// can be checked. User torrents are never touched.
func (e *Engine) probeMetaInfo(ctx context.Context, magnetURI string) (*metainfo.MetaInfo, error) {
	hash := strings.ToLower(extractInfoHash(magnetURI))
	if hash != "" {
		if mi, err := metainfo.LoadFromFile(e.getTorrentCacheFilePath(hash)); err == nil {
			if info, err := mi.UnmarshalInfo(); err == nil && (info.HasV1() || len(mi.PieceLayers) > 0) {
				return mi, nil
			}
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
	if info := t.Info(); info.HasV2() && !info.HasV1() {
		deadline := time.After(pieceLayersTimeout)
		for len(mi.PieceLayers) == 0 {
			select {
			case <-ctx.Done():
				return &mi, nil
			case <-deadline:
				return &mi, nil
			case <-time.After(time.Second):
				mi = t.Metainfo()
			}
		}
	}
	return &mi, nil
}

func magnetForRecord(infoHash, infoHashV2, name string) string {
	dn := url.QueryEscape(name)
	switch {
	case len(infoHash) == 64:
		return fmt.Sprintf("magnet:?xt=urn:btmh:1220%s&dn=%s", infoHash, dn)
	case infoHashV2 != "":
		return fmt.Sprintf("magnet:?xt=urn:btih:%s&xt=urn:btmh:1220%s&dn=%s", infoHash, infoHashV2, dn)
	}
	return fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", infoHash, dn)
}

// gatherSwarmCandidates collects tentative swarms that may carry some content: releases found in
// the local DHT index holding a file of exactly one of the given sizes, and indexer search results
// by name. None of them is trusted until its hashes are checked against local data.
func (e *Engine) gatherSwarmCandidates(ctx context.Context, name string, fileSizes []int64, exclude map[string]bool) []swarmCandidate {
	seen := make(map[string]bool, len(exclude))
	for h := range exclude {
		seen[strings.ToLower(h)] = true
	}
	var largest int64
	for _, size := range fileSizes {
		largest = max(largest, size)
	}

	var bySize, byName []swarmCandidate
	if e.dhtIndexer != nil {
		sort.Slice(fileSizes, func(i, j int) bool { return fileSizes[i] > fileSizes[j] })
		for i, size := range fileSizes {
			if i >= 3 || size < minFileSizeForLookup {
				break
			}
			for _, rec := range e.dhtIndexer.SearchByFileSize(size) {
				h := strings.ToLower(rec.InfoHash)
				if seen[h] {
					continue
				}
				seen[h] = true
				seeders, leechers := -1, -1
				if rec.Activity != nil {
					seeders, leechers = rec.Activity.LastSeeders, rec.Activity.LastPeers
				}
				bySize = append(bySize, swarmCandidate{
					magnet:   magnetForRecord(h, rec.InfoHashV2, rec.Name),
					provider: "Local DHT Index (file size match)",
					protocol: rec.ProtocolVersion,
					seeders:  seeders,
					leechers: leechers,
				})
			}
		}
	}

	if e.searchMgr != nil {
		for _, q := range buildSearchQueries(name) {
			for _, r := range e.searchMgr.SearchAll(ctx, q) {
				h := strings.ToLower(r.InfoHash)
				if h == "" || seen[h] || !strings.HasPrefix(r.MagnetURI, "magnet:") {
					continue
				}
				seen[h] = true
				if r.SizeBytes > 0 && r.SizeBytes < largest {
					continue
				}
				byName = append(byName, swarmCandidate{r.MagnetURI, r.Provider, r.ProtocolVersion, r.Seeders, r.Leechers})
			}
			if len(byName) >= 3*maxAlternateProbes || ctx.Err() != nil {
				break
			}
		}
	}

	rank := func(list []swarmCandidate) {
		sort.SliceStable(list, func(i, j int) bool {
			if ri, rj := protocolRank(list[i].protocol), protocolRank(list[j].protocol); ri != rj {
				return ri < rj
			}
			return list[i].seeders > list[j].seeders
		})
	}
	rank(bySize)
	rank(byName)
	// An exact file size is a stronger lead than a similar name.
	candidates := append(bySize, byName...)
	if len(candidates) > maxAlternateProbes {
		candidates = candidates[:maxAlternateProbes]
	}
	return candidates
}

// probeCandidates resolves candidates' metadata a few at a time and passes each to check.
func (e *Engine) probeCandidates(ctx context.Context, candidates []swarmCandidate, check func(swarmCandidate, *metainfo.MetaInfo, *metainfo.Info)) {
	var wg sync.WaitGroup
	limits := make(chan struct{}, 4)
	for _, c := range candidates {
		wg.Add(1)
		go func(c swarmCandidate) {
			defer wg.Done()
			limits <- struct{}{}
			defer func() { <-limits }()
			mi, err := e.probeMetaInfo(ctx, c.magnet)
			if err != nil {
				return
			}
			info, err := mi.UnmarshalInfo()
			if err != nil {
				return
			}
			check(c, mi, &info)
		}(c)
	}
	wg.Wait()
}

func swarmMagnet(fallback string, mi *metainfo.MetaInfo) (magnet, infoHashV2 string) {
	mag, err := mi.MagnetV2()
	if err != nil {
		return fallback, ""
	}
	if mag.V2InfoHash.Ok {
		infoHashV2 = mag.V2InfoHash.Value.HexString()
	}
	return mag.String(), infoHashV2
}

// FindAlternateSwarms looks for other releases of a torrent's content and returns those whose
// hashes prove they contain the same files, hybrid and v2 releases first.
func (e *Engine) FindAlternateSwarms(ctx context.Context, infoHashHex string) ([]AlternateSwarm, error) {
	e.mu.RLock()
	tor, _ := e.findUserTorrent(infoHashHex)
	e.mu.RUnlock()
	if tor == nil {
		return nil, fmt.Errorf("torrent %s not found", infoHashHex)
	}
	info := tor.Info()
	if info == nil {
		return nil, fmt.Errorf("metadata not available yet")
	}
	ourHash := strings.ToLower(tor.InfoHash().HexString())

	var sizes []int64
	for _, f := range tor.Files() {
		sizes = append(sizes, f.Length())
	}
	candidates := e.gatherSwarmCandidates(ctx, info.BestName(), sizes, map[string]bool{ourHash: true})

	var (
		mu    sync.Mutex
		found []alternateCandidate
	)
	e.probeCandidates(ctx, candidates, func(c swarmCandidate, mi *metainfo.MetaInfo, candInfo *metainfo.Info) {
		if strings.EqualFold(mi.HashInfoBytes().HexString(), ourHash) {
			return
		}
		matches := e.matchTorrentFiles(tor, mi, candInfo)
		if len(matches) == 0 {
			return
		}
		swarm := AlternateSwarm{
			InfoHash:        mi.HashInfoBytes().HexString(),
			ProtocolVersion: protocolOfInfo(candInfo),
			Name:            candInfo.BestName(),
			Provider:        c.provider,
			Seeders:         c.seeders,
			Leechers:        c.leechers,
			MatchedFiles:    len(matches),
			TotalBytes:      candInfo.TotalLength(),
		}
		for _, m := range matches {
			swarm.MatchedBytes += tor.Files()[m.oursIndex].Length()
			swarm.VerifiedPieces += m.checked
		}
		swarm.MagnetURI, swarm.InfoHashV2 = swarmMagnet(c.magnet, mi)
		mu.Lock()
		found = append(found, alternateCandidate{swarm: swarm, mi: mi, matches: matches})
		mu.Unlock()
	})

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

// fileStorage stores a torrent's files under baseDir, sharing the engine's persistent piece
// completion so completion Digwire records is the completion the torrent sees.
func (e *Engine) fileStorage(baseDir string) storage.ClientImplCloser {
	return storage.NewFileOpts(newFileClientOpts(baseDir, nonClosingPieceCompletion{e.pieceComp}))
}

// mappedFileStorage stores files named in fileMap (keyed by the torrent's own file path) at the
// given paths relative to the download dir, so an attached swarm writes into the files of the
// torrent it was matched against. Other files land in their default location.
func (e *Engine) mappedFileStorage(fileMap map[string]string) storage.ClientImplCloser {
	opts := newFileClientOpts(e.cfg.DownloadDir, nonClosingPieceCompletion{e.pieceComp})
	opts.FilePathMaker = func(o storage.FilePathMakerOpts) string {
		if p, ok := fileMap[strings.Join(o.File.BestPath(), "/")]; ok {
			return p
		}
		return torrentFilePath(o)
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
	if !ok || len(cand.matches) == 0 {
		return "", fmt.Errorf("swarm %s was not verified for this torrent; search again", alternateHashHex)
	}
	if alreadyAdded {
		return "", fmt.Errorf("that swarm is already in your downloads")
	}

	ourFiles := tor.Files()
	fileMap := make(map[string]string, len(cand.matches))
	matched := make(map[int]bool, len(cand.matches))
	for _, m := range cand.matches {
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
	e.initTracker(newHash, cand.swarm.Name)
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
	if ourTr := e.rateMap[ourHash]; ourTr != nil {
		ourTr.suggestedSwarm = nil
		tr.isPaused = ourTr.isPaused
	}
	e.saveSessionLocked()
	e.mu.Unlock()

	e.saveTorrentMetainfo(newTor)
	e.ConsolidateAndVerifyForce(newTor, true, onVerified...)
	return newHash, nil
}

// syncSiblingPieces re-verifies pieces that one of two torrents sharing files has completed but the
// other has not, so both reflect data downloaded through either swarm. Piece grids may differ.
func (e *Engine) syncSiblingPieces(alt *torrent.Torrent, tr *rateTracker, sibling *torrent.Torrent) {
	if alt.Info() == nil || sibling.Info() == nil || tr.isVerifying.Load() || !tr.siblingSyncing.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer tr.siblingSyncing.Store(false)
		siblingFiles := make(map[string]*torrent.File)
		for _, f := range sibling.Files() {
			siblingFiles[f.Path()] = f
		}
		altFiles := alt.Files()
		for i, fi := range alt.Info().UpvertedFiles() {
			sf, ok := siblingFiles[tr.fileMap[strings.Join(fi.BestPath(), "/")]]
			if !ok || i >= len(altFiles) || sf.Length() != altFiles[i].Length() {
				continue
			}
			verifyPiecesCoveredBy(sibling, sf, alt, altFiles[i])
			verifyPiecesCoveredBy(alt, altFiles[i], sibling, sf)
		}
	}()
}

// verifyPiecesCoveredBy re-verifies incomplete pieces of dst lying wholly within dstFile whose bytes
// src already holds as complete pieces of srcFile, the same data at the same file offsets.
func verifyPiecesCoveredBy(dst *torrent.Torrent, dstFile *torrent.File, src *torrent.Torrent, srcFile *torrent.File) {
	dpl, spl := dst.Info().PieceLength, src.Info().PieceLength
	fileBegin, fileEnd := dstFile.Offset(), dstFile.Offset()+dstFile.Length()
	for i := (fileBegin + dpl - 1) / dpl; i*dpl < fileEnd; i++ {
		begin, end := i*dpl, min((i+1)*dpl, dst.Length())
		if end > fileEnd {
			break // The piece reaches into the next file, which src does not provide.
		}
		piece := dst.Piece(int(i))
		if piece.State().Complete {
			continue
		}
		srcBegin := srcFile.Offset() + begin - fileBegin
		covered := true
		for j := srcBegin / spl; j*spl < srcBegin+end-begin; j++ {
			if !src.Piece(int(j)).State().Complete {
				covered = false
				break
			}
		}
		if covered {
			_ = piece.VerifyDataContext(context.Background())
		}
	}
}
