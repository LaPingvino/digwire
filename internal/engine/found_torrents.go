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

// FoundTorrent is a .torrent Digwire knows of but the user never added: metadata left over from a
// search or a swarm probe, or a file that came along inside a download. It is only ever offered,
// never started on its own.
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
	Source string `json:"source"`
	Path   string `json:"path"`
	FoundAt int64 `json:"found_at"`
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
