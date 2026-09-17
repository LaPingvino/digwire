package dhtindex

import (
	"encoding/hex"
	"strings"
	"time"

	"github.com/anacrolix/torrent/metainfo"
)

// RecordFromMetaInfo describes a torrent's metadata as an index record: protocol, files with
// their sizes and, for v2 and hybrid torrents, their pieces roots.
func RecordFromMetaInfo(infoHashHex string, mi *metainfo.MetaInfo, info *metainfo.Info) *DHTRecord {
	rec := &DHTRecord{
		InfoHash:        strings.ToLower(infoHashHex),
		ProtocolVersion: "v1",
		Name:            info.BestName(),
		SizeBytes:       info.TotalLength(),
		DiscoveredAt:    time.Now().Unix(),
	}
	switch {
	case info.HasV1() && info.HasV2():
		rec.ProtocolVersion = "hybrid"
	case info.HasV2():
		rec.ProtocolVersion = "v2"
	}
	if mi != nil && len(mi.InfoBytes) > 0 && info.HasV2() {
		if mag, err := mi.MagnetV2(); err == nil && mag.V2InfoHash.Ok {
			rec.InfoHashV2 = hex.EncodeToString(mag.V2InfoHash.Value[:])
		}
	}
	for _, f := range info.UpvertedFiles() {
		if strings.Contains(f.Attr, "p") {
			continue // BEP 47 padding.
		}
		entry := DHTFileEntry{Path: f.DisplayPath(info), SizeBytes: f.Length}
		if f.PiecesRoot.Ok {
			entry.PiecesRoot = hex.EncodeToString(f.PiecesRoot.Value[:])
			rec.PiecesRoots = append(rec.PiecesRoots, entry.PiecesRoot)
		}
		rec.Files = append(rec.Files, entry.Path)
		rec.FileEntries = append(rec.FileEntries, entry)
	}
	rec.NumFiles = len(rec.Files)
	return rec
}

// mergeFileEntries adds entries to existing ones by path, filling in pieces roots learned later,
// such as those proven equal to a v2 release's files. It reports whether anything changed.
func mergeFileEntries(existing, added []DHTFileEntry) ([]DHTFileEntry, bool) {
	byPath := make(map[string]int, len(existing))
	for i, fe := range existing {
		byPath[fe.Path] = i
	}
	changed := false
	for _, fe := range added {
		i, ok := byPath[fe.Path]
		if !ok {
			byPath[fe.Path] = len(existing)
			existing = append(existing, fe)
			changed = true
			continue
		}
		if fe.PiecesRoot != "" && existing[i].PiecesRoot != fe.PiecesRoot {
			existing[i].PiecesRoot = fe.PiecesRoot
			changed = true
		}
		if fe.SizeBytes > 0 && existing[i].SizeBytes == 0 {
			existing[i].SizeBytes = fe.SizeBytes
			changed = true
		}
	}
	return existing, changed
}

func mergeRoots(existing, added []string) []string {
	seen := make(map[string]bool, len(existing))
	for _, r := range existing {
		seen[r] = true
	}
	for _, r := range added {
		if r != "" && !seen[r] {
			seen[r] = true
			existing = append(existing, r)
		}
	}
	return existing
}
