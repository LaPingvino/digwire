package engine

import (
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

// storedFilePath is where a torrent file lives, relative to the download directory.
func storedFilePath(info *metainfo.Info, fi metainfo.FileInfo, fileMap map[string]string) string {
	if p, ok := fileMap[strings.Join(fi.BestPath(), "/")]; ok {
		return p
	}
	return torrentFilePath(storage.FilePathMakerOpts{Info: info, File: &fi})
}

// linkMatch is a file of a new torrent proven to be present in another local torrent.
type linkMatch struct {
	ourIndex, sourceIndex int // Indexes into UpvertedFiles.
	sourceInfo            *metainfo.Info
	sourceHash            string
	path                  string // Relative to the download directory.
	length                int64
}

// findLocalFileMatches looks for files of tor that other local torrents already hold, proving each
// by hashing the other torrent's verified data against tor's own piece hashes.
func (e *Engine) findLocalFileMatches(tor *torrent.Torrent) map[string]linkMatch {
	info := tor.Info()
	if info == nil || e.cfg == nil || e.cfg.DownloadDir == "" {
		return nil
	}
	mi := tor.Metainfo()
	ourHash := strings.ToLower(tor.InfoHash().HexString())

	type source struct {
		tor  *torrent.Torrent
		info *metainfo.Info
		hash string
		idx  int
		path string
	}
	bySize := make(map[int64][]source)
	e.mu.RLock()
	for _, other := range e.client.Torrents() {
		hash := strings.ToLower(other.InfoHash().HexString())
		otr := e.rateMap[hash]
		oinfo := other.Info()
		if hash == ourHash || otr == nil || oinfo == nil {
			continue
		}
		for i, fi := range oinfo.UpvertedFiles() {
			if fi.Length == 0 || i >= len(other.Files()) {
				continue
			}
			bySize[fi.Length] = append(bySize[fi.Length], source{other, oinfo, hash, i, storedFilePath(oinfo, fi, otr.fileMap)})
		}
	}
	e.mu.RUnlock()

	matches := make(map[string]linkMatch)
	for oi, fi := range info.UpvertedFiles() {
		if fi.Length == 0 {
			continue
		}
		for _, src := range bySize[fi.Length] {
			ld := torrentFileData(e.cfg.DownloadDir, src.tor, src.idx)
			ld.path = filepath.Join(e.cfg.DownloadDir, src.path)
			if _, ok := verifyFileBySampling(ld, &mi, info, fi); ok {
				matches[strings.Join(fi.BestPath(), "/")] = linkMatch{oi, src.idx, src.info, src.hash, src.path, fi.Length}
				break
			}
		}
	}
	return matches
}

// linkToLocalData re-adds a torrent so files another local torrent already holds are used in place
// instead of downloaded again. Files without a proven match download to their usual location.
// It returns the torrent to continue with: the re-added one, or tor itself when nothing matched.
func (e *Engine) linkToLocalData(tor *torrent.Torrent) *torrent.Torrent {
	info := tor.Info()
	hash := strings.ToLower(tor.InfoHash().HexString())
	e.mu.RLock()
	tr := e.rateMap[hash]
	skip := tr == nil || len(tr.fileMap) > 0 || info == nil || tor.BytesCompleted() >= tor.Length()
	e.mu.RUnlock()
	if skip {
		return tor
	}

	matches := e.findLocalFileMatches(tor)
	if len(matches) == 0 {
		return tor
	}
	fileMap := make(map[string]string, len(matches))
	bytesBySource := make(map[string]int64)
	pairsBySource := make(map[string][][2]int)
	for key, m := range matches {
		fileMap[key] = m.path
		bytesBySource[m.sourceHash] += m.length
		pairsBySource[m.sourceHash] = append(pairsBySource[m.sourceHash], [2]int{m.ourIndex, m.sourceIndex})
	}
	for _, m := range matches {
		if pairs := pairsBySource[m.sourceHash]; pairs != nil {
			e.recordEquivalentFiles(hash, info, m.sourceHash, m.sourceInfo, pairs)
			delete(pairsBySource, m.sourceHash)
		}
	}
	sibling := ""
	for h, n := range bytesBySource {
		if sibling == "" || n > bytesBySource[sibling] {
			sibling = h
		}
	}

	mi := tor.Metainfo()
	spec := torrent.TorrentSpecFromMetaInfo(&mi)
	spec.Storage = e.mappedFileStorage(fileMap)
	wasPaused := tr.isPaused
	tor.Drop()
	newTor, _, err := e.client.AddTorrentSpec(spec)
	if err != nil {
		log.Printf("⚠️ Re-adding %s to use local files failed: %v", hash, err)
		// Put it back as it was, so the download continues normally.
		if back, _, err := e.client.AddTorrentSpec(torrent.TorrentSpecFromMetaInfo(&mi)); err == nil {
			return back
		}
		return tor
	}
	if e.IsGermanyMode() {
		newTor.DisallowDataUpload()
	}

	e.mu.Lock()
	if cur := e.rateMap[hash]; cur != nil {
		cur.fileMap = fileMap
		cur.siblingHash = sibling
		cur.linkedLocal = true
		cur.isPaused = wasPaused
		if seeds := e.webSeedsMap[hash]; len(seeds) > 0 {
			newTor.AddWebSeeds(seeds)
		}
	}
	e.saveSessionLocked()
	e.mu.Unlock()
	log.Printf("🔗 %s: using %d file(s) already on disk from %d local torrent(s)", info.BestName(), len(matches), len(bytesBySource))
	return newTor
}

// markLinkPendingLocked has a newly added torrent's first verification look for its files among
// other local torrents. A torrent that was already present keeps its own progress.
func (e *Engine) markLinkPendingLocked(hash string) {
	tr := e.rateMap[strings.ToLower(hash)]
	if tr == nil {
		return
	}
	if t, _ := e.findUserTorrent(hash); t != nil && t.Info() != nil && t.BytesCompleted() > 0 {
		return
	}
	tr.linkPending = true
}

// pathsInUseLocked returns the absolute paths of files belonging to local torrents other than
// except, so deleting one torrent never takes files another one still relies on.
func (e *Engine) pathsInUseLocked(except string) map[string]bool {
	inUse := make(map[string]bool)
	for _, t := range e.client.Torrents() {
		hash := strings.ToLower(t.InfoHash().HexString())
		tr := e.rateMap[hash]
		if hash == strings.ToLower(except) || tr == nil || t.Info() == nil {
			continue
		}
		for _, fi := range t.Info().UpvertedFiles() {
			inUse[filepath.Clean(filepath.Join(e.cfg.DownloadDir, storedFilePath(t.Info(), fi, tr.fileMap)))] = true
		}
	}
	return inUse
}

// removeTorrentFiles deletes a torrent's files under target, keeping any file still in use.
func removeTorrentFiles(target string, inUse map[string]bool) {
	target = filepath.Clean(target)
	shared := false
	for p := range inUse {
		if p == target || strings.HasPrefix(p, target+string(filepath.Separator)) {
			shared = true
			break
		}
	}
	if !shared {
		_ = os.RemoveAll(target)
		return
	}
	var dirs []string
	_ = filepath.WalkDir(target, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			dirs = append(dirs, p)
		} else if !inUse[filepath.Clean(p)] {
			_ = os.Remove(p)
		}
		return nil
	})
	for i := len(dirs) - 1; i >= 0; i-- {
		_ = os.Remove(dirs[i]) // Only succeeds once empty.
	}
}
