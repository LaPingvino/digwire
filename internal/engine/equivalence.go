package engine

import (
	"encoding/hex"
	"strings"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"

	"digwire/internal/dhtindex"
)

// indexLocalTorrent records a local torrent's metadata in the DHT index. The crawler never touches
// user torrents, so without this their files and pieces roots would stay unknown to lookups.
func (e *Engine) indexLocalTorrent(tor *torrent.Torrent) {
	info := tor.Info()
	if e.dhtIndexer == nil || info == nil {
		return
	}
	mi := tor.Metainfo()
	e.dhtIndexer.AddRecord(dhtindex.RecordFromMetaInfo(tor.InfoHash().HexString(), &mi, info))
}

// recordEquivalentFiles remembers files of two torrents proven to hold the same data: a file
// without a pieces root (v1) gets the root of its twin, so the next time that v1 torrent is added
// its v2 or hybrid releases are found by root at once, without downloading or searching first.
// pairs holds indexes into each torrent's UpvertedFiles.
func (e *Engine) recordEquivalentFiles(aHash string, a *metainfo.Info, bHash string, b *metainfo.Info, pairs [][2]int) {
	if e.dhtIndexer == nil || a == nil || b == nil {
		return
	}
	aFiles, bFiles := a.UpvertedFiles(), b.UpvertedFiles()
	learn := func(hash string, info *metainfo.Info, files []metainfo.FileInfo, idx int, root metainfo.FileInfo) *dhtindex.DHTFileEntry {
		if idx >= len(files) || files[idx].PiecesRoot.Ok || !root.PiecesRoot.Ok || files[idx].Length != root.Length {
			return nil
		}
		return &dhtindex.DHTFileEntry{
			Path:       files[idx].DisplayPath(info),
			SizeBytes:  files[idx].Length,
			PiecesRoot: hex.EncodeToString(root.PiecesRoot.Value[:]),
		}
	}
	record := func(hash string, info *metainfo.Info, entries []dhtindex.DHTFileEntry) {
		if len(entries) == 0 {
			return
		}
		rec := dhtindex.RecordFromMetaInfo(hash, nil, info)
		byPath := make(map[string]string, len(entries))
		for _, fe := range entries {
			byPath[fe.Path] = fe.PiecesRoot
		}
		for i := range rec.FileEntries {
			if root, ok := byPath[rec.FileEntries[i].Path]; ok {
				rec.FileEntries[i].PiecesRoot = root
				rec.PiecesRoots = append(rec.PiecesRoots, root)
			}
		}
		e.dhtIndexer.AddRecord(rec)
	}
	var forA, forB []dhtindex.DHTFileEntry
	for _, p := range pairs {
		if p[0] >= len(aFiles) || p[1] >= len(bFiles) {
			continue
		}
		if fe := learn(aHash, a, aFiles, p[0], bFiles[p[1]]); fe != nil {
			forA = append(forA, *fe)
		}
		if fe := learn(bHash, b, bFiles, p[1], aFiles[p[0]]); fe != nil {
			forB = append(forB, *fe)
		}
	}
	record(aHash, a, forA)
	record(bHash, b, forB)
}

// knownPiecesRoots returns the v2 pieces root of each file of a torrent, from its own metadata or,
// for v1 files, from equivalences remembered in the DHT index. Keys index UpvertedFiles.
func (e *Engine) knownPiecesRoots(infoHashHex string, info *metainfo.Info) map[int][32]byte {
	roots := make(map[int][32]byte)
	var remembered map[string]dhtindex.DHTFileEntry
	for i, f := range info.UpvertedFiles() {
		if f.PiecesRoot.Ok {
			roots[i] = f.PiecesRoot.Value
			continue
		}
		if remembered == nil {
			remembered = make(map[string]dhtindex.DHTFileEntry)
			if e.dhtIndexer != nil {
				for _, fe := range e.dhtIndexer.FileEntries(infoHashHex) {
					remembered[fe.Path] = fe
				}
			}
		}
		fe, ok := remembered[f.DisplayPath(info)]
		if !ok || fe.SizeBytes != f.Length || len(fe.PiecesRoot) != 64 {
			continue
		}
		var root [32]byte
		if _, err := hex.Decode(root[:], []byte(strings.ToLower(fe.PiecesRoot))); err == nil {
			roots[i] = root
		}
	}
	return roots
}
