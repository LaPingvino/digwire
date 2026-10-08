package engine

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/anacrolix/torrent/metainfo"
)

// FoundTorrent is a .torrent the user never added but which concerns them: one that came along
// inside a download, or kept metadata whose data turns out to be on their disk. It is only ever
// offered, never started on its own.
type FoundTorrent struct {
	InfoHash        string `json:"info_hash"`
	Name            string `json:"name"`
	TotalBytes      int64  `json:"total_bytes"`
	LocalBytes      int64  `json:"local_bytes"`
	NumFiles        int    `json:"num_files"`
	ProtocolVersion string `json:"protocol_version"`
	// Status is "complete", "partial" or "missing", by what lies in the download folder.
	Status string `json:"status"`
	// Source is "download" for a file that came with a download, "cache" for kept metadata.
	Source  string `json:"source"`
	Path    string `json:"path"`
	FoundAt int64  `json:"found_at"`
	// MadeHere marks a torrent Digwire itself created, e.g. from a Soulseek or media download.
	MadeHere bool `json:"made_here"`
	// Leftover is one of those whose files are gone: nothing to seed and nobody to get it from,
	// so the .torrent is only clutter.
	Leftover bool `json:"leftover"`
	// SameAs names a torrent already in the list holding the very same files, typically the v1
	// release of something now seeded as hybrid. Adding it serves that older swarm as well, from
	// the files that are already there.
	SameAs     string `json:"same_as,omitempty"`
	SameAsHash string `json:"same_as_hash,omitempty"`
}

const (
	maxFoundTorrentScan  = 500
	maxFoundTorrentDepth = 6
)

// FoundTorrents lists .torrent files that are not among the user's torrents, with what of each is
// already on disk, so the interface can offer seeding, resuming or downloading as fits.
func (e *Engine) FoundTorrents() []FoundTorrent {
	if e.cfg == nil {
		return nil
	}
	e.mu.RLock()
	known := make(map[string]bool, len(e.rateMap)+len(e.savedTorrentsMap))
	for hash := range e.rateMap {
		known[strings.ToLower(hash)] = true
	}
	for hash := range e.savedTorrentsMap {
		known[strings.ToLower(hash)] = true
	}
	dismissed := make(map[string]bool, len(e.dismissedFound))
	for hash := range e.dismissedFound {
		dismissed[hash] = true
	}
	e.mu.RUnlock()

	// Which local torrent holds each file, so a found torrent can be recognised as another release
	// of something already here.
	owners := make(map[string]string) // Absolute file path -> torrent hash.
	names := make(map[string]string)  // Torrent hash -> display name.
	e.mu.RLock()
	for _, t := range e.client.Torrents() {
		hash := strings.ToLower(t.InfoHash().HexString())
		tr := e.rateMap[hash]
		if tr == nil || t.Info() == nil {
			continue
		}
		for _, fi := range t.Info().UpvertedFiles() {
			if fi.Length == 0 {
				continue
			}
			path := filepath.Clean(filepath.Join(e.cfg.DownloadDir, storedFilePath(t.Info(), fi, tr.fileMap)))
			owners[path] = hash
			names[hash] = t.Name()
		}
	}
	e.mu.RUnlock()

	seen := make(map[string]bool)
	var found []FoundTorrent
	add := func(path, source string) {
		if len(found) >= maxFoundTorrentScan {
			return
		}
		mi, err := metainfo.LoadFromFile(path)
		if err != nil || mi == nil {
			return
		}
		info, err := mi.UnmarshalInfo()
		if err != nil || info.BestName() == "" {
			return
		}
		hash := strings.ToLower(mi.HashInfoBytes().HexString())
		if hash == "" || known[hash] || dismissed[hash] || seen[hash] {
			return
		}
		seen[hash] = true
		entry := FoundTorrent{
			MadeHere:        madeHere(mi),
			InfoHash:        hash,
			Name:            info.BestName(),
			TotalBytes:      info.TotalLength(),
			NumFiles:        len(info.UpvertedFiles()),
			ProtocolVersion: protocolOfInfo(&info),
			Source:          source,
			Path:            path,
		}
		if st, err := os.Stat(path); err == nil {
			entry.FoundAt = st.ModTime().Unix()
		}
		entry.LocalBytes = e.localBytesOf(&info)
		switch {
		case entry.TotalBytes > 0 && entry.LocalBytes >= entry.TotalBytes:
			entry.Status = "complete"
		case entry.LocalBytes > 0:
			entry.Status = "partial"
		default:
			entry.Status = "missing"
		}
		// Kept metadata is Digwire's own bookkeeping: everything a search or a swarm probe ever
		// resolved, which is not a list of anything the user wants. Only offer it when its data is
		// actually on their disk. A .torrent that came with a download is always worth showing.
		if source == "cache" && entry.Status == "missing" {
			return
		}
		entry.Leftover = entry.MadeHere && entry.Status == "missing"
		if entry.Status == "complete" {
			if owner, ok := sameLocalTorrent(&info, e.cfg.DownloadDir, owners); ok {
				entry.SameAs, entry.SameAsHash = names[owner], owner
			}
		}
		found = append(found, entry)
	}

	// Files that came with a download are the interesting ones, so they are scanned first.
	cacheDir := e.getTorrentsCacheDir()
	scanDepth := strings.Count(filepath.Clean(e.cfg.DownloadDir), string(filepath.Separator))
	_ = filepath.WalkDir(e.cfg.DownloadDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || len(found) >= maxFoundTorrentScan {
			return nil
		}
		if d.IsDir() {
			if path == cacheDir || strings.Count(filepath.Clean(path), string(filepath.Separator))-scanDepth > maxFoundTorrentDepth {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(strings.ToLower(d.Name()), ".torrent") {
			add(path, "download")
		}
		return nil
	})
	if entries, err := os.ReadDir(cacheDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".torrent") {
				add(filepath.Join(cacheDir, entry.Name()), "cache")
			}
		}
	}

	// What the user can act on right away comes first: data on disk, then the largest.
	rank := map[string]int{"complete": 0, "partial": 1, "missing": 2}
	sort.SliceStable(found, func(i, j int) bool {
		if ri, rj := rank[found[i].Status], rank[found[j].Status]; ri != rj {
			return ri < rj
		}
		if found[i].Source != found[j].Source {
			return found[i].Source == "download"
		}
		return found[i].TotalBytes > found[j].TotalBytes
	})
	return found
}

// sameLocalTorrent reports the torrent that holds every file of info, if one torrent holds them
// all: then this is another release of the same content, not a separate download.
func sameLocalTorrent(info *metainfo.Info, downloadDir string, owners map[string]string) (string, bool) {
	owner := ""
	for _, fi := range info.UpvertedFiles() {
		if fi.Length == 0 {
			continue
		}
		hash, ok := owners[filepath.Clean(filepath.Join(downloadDir, storedFilePath(info, fi, nil)))]
		if !ok || (owner != "" && hash != owner) {
			return "", false // Not here, or spread over several torrents.
		}
		owner = hash
	}
	return owner, owner != ""
}

// madeHere reports whether Digwire created this torrent itself, which it records in the metadata.
func madeHere(mi *metainfo.MetaInfo) bool {
	return strings.Contains(strings.ToLower(mi.CreatedBy+" "+mi.Comment), "digwire")
}

// DeleteFoundTorrentFile removes a found .torrent file from disk. Only files this listing offered
// are touched, and only when the user asks: it is their folder.
func (e *Engine) DeleteFoundTorrentFile(infoHashHex string) error {
	hash := strings.ToLower(strings.TrimSpace(infoHashHex))
	for _, f := range e.FoundTorrents() {
		if f.InfoHash == hash {
			if err := os.Remove(f.Path); err != nil {
				return fmt.Errorf("removing %s: %w", filepath.Base(f.Path), err)
			}
			return nil
		}
	}
	return fmt.Errorf("no torrent file found for %s", infoHashHex)
}

// DeleteLeftoverTorrentFiles removes every .torrent Digwire made whose files are gone, and reports
// how many went.
func (e *Engine) DeleteLeftoverTorrentFiles() (int, error) {
	removed := 0
	var firstErr error
	for _, f := range e.FoundTorrents() {
		if !f.Leftover {
			continue
		}
		if err := os.Remove(f.Path); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		removed++
	}
	return removed, firstErr
}

// localBytesOf reports how much of a torrent's content is in the download folder already. It goes
// by file size: nothing is claimed complete until it has been hashed, which happens after adding.
func (e *Engine) localBytesOf(info *metainfo.Info) int64 {
	var total int64
	for _, f := range info.UpvertedFiles() {
		path := filepath.Join(e.cfg.DownloadDir, storedFilePath(info, f, nil))
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			total += min(st.Size(), f.Length)
		}
	}
	return total
}

// AddFoundTorrent starts a found torrent, as if the user had added the .torrent file themselves.
func (e *Engine) AddFoundTorrent(infoHashHex string) (string, error) {
	hash := strings.ToLower(strings.TrimSpace(infoHashHex))
	for _, f := range e.FoundTorrents() {
		if f.InfoHash != hash {
			continue
		}
		file, err := os.Open(f.Path)
		if err != nil {
			return "", fmt.Errorf("opening %s: %w", filepath.Base(f.Path), err)
		}
		defer file.Close()
		tor, err := e.AddTorrentFile(file)
		if err != nil {
			return "", err
		}
		return tor.InfoHash().HexString(), nil
	}
	return "", fmt.Errorf("no torrent file found for %s", infoHashHex)
}

// DismissFoundTorrent hides a found torrent from the list. The file itself is left alone: it may
// be part of a download, and kept metadata is worth having for later lookups.
func (e *Engine) DismissFoundTorrent(infoHashHex string) {
	hash := strings.ToLower(strings.TrimSpace(infoHashHex))
	if hash == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.dismissedFound == nil {
		e.dismissedFound = make(map[string]int64)
	}
	e.dismissedFound[hash] = time.Now().Unix()
	e.saveSessionLocked()
}

// RestoreFoundTorrents brings back everything that was dismissed.
func (e *Engine) RestoreFoundTorrents() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.dismissedFound = make(map[string]int64)
	e.saveSessionLocked()
}
