package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anacrolix/torrent/metainfo"

	"digwire/internal/dhtindex"
)

// The Otje case: a hybrid whose two folders are also published as independent v1 torrents, which
// nobody may seed. Verifying those against the hybrid's files must remember their v2 roots, so a
// later add of either v1 torrent finds the hybrid by root without any downloaded data.
func TestHybridTeachesRootsToV1FolderReleases(t *testing.T) {
	eng, downloadDir := newTestEngine(t)
	if eng.dhtIndexer == nil {
		t.Fatal("no DHT index")
	}
	const mib = 1 << 20
	show := filepath.Join(downloadDir, "Show")
	writeRandomFile(t, filepath.Join(show, "DVD1", "ep1.mkv"), mib+3*testPieceLen+11)
	writeRandomFile(t, filepath.Join(show, "DVD1", "ep2.mkv"), mib+5*testPieceLen+7)
	writeRandomFile(t, filepath.Join(show, "DVD2", "ep3.mkv"), mib+2*testPieceLen+3)

	// The folder releases, known to the index with metadata but without any roots.
	folderRelease := func(folder, name string, pieceLen int64) (string, *metainfo.Info) {
		mi := v1MetaInfo(t, filepath.Join(show, folder), name, pieceLen)
		info := unmarshalInfo(t, mi)
		hash := mi.HashInfoBytes().HexString()
		f, err := os.Create(eng.getTorrentCacheFilePath(hash))
		if err != nil {
			t.Fatal(err)
		}
		if err := mi.Write(f); err != nil {
			t.Fatal(err)
		}
		f.Close()
		eng.dhtIndexer.AddRecord(dhtindex.RecordFromMetaInfo(hash, mi, info))
		return hash, info
	}
	if err := os.MkdirAll(eng.getTorrentsCacheDir(), 0755); err != nil {
		t.Fatal(err)
	}
	dvd1Hash, dvd1Info := folderRelease("DVD1", "Show DVD1", 4*testPieceLen)
	dvd2Hash, dvd2Info := folderRelease("DVD2", "Show DVD2", testPieceLen)

	hybridMI, err := BuildBEP52MetaInfo(show, true, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	hybrid := addLocalTorrent(t, eng, hybridMI)
	waitFor(t, "hybrid to complete", func() bool { return hybrid.BytesCompleted() == hybrid.Length() })
	hybridHash := strings.ToLower(hybrid.InfoHash().HexString())

	swarms, err := eng.FindAlternateSwarms(context.Background(), hybridHash)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]int{}
	for _, s := range swarms {
		found[strings.ToLower(s.InfoHash)] = s.MatchedFiles
	}
	if found[dvd1Hash] != 2 || found[dvd2Hash] != 1 {
		t.Fatalf("verified releases = %v, want DVD1 with 2 files and DVD2 with 1", found)
	}

	hybridRoots := map[[32]byte]bool{}
	for _, f := range hybrid.Info().UpvertedFiles() {
		if f.PiecesRoot.Ok {
			hybridRoots[f.PiecesRoot.Value] = true
		}
	}
	for hash, info := range map[string]*metainfo.Info{dvd1Hash: dvd1Info, dvd2Hash: dvd2Info} {
		roots := eng.knownPiecesRoots(hash, info)
		if len(roots) != len(info.UpvertedFiles()) {
			t.Fatalf("%s: remembered %d roots for %d files", hash, len(roots), len(info.UpvertedFiles()))
		}
		for _, root := range roots {
			if !hybridRoots[root] {
				t.Fatalf("%s: remembered root %x is not one of the hybrid's", hash, root)
			}
		}
		var rootList [][32]byte
		for _, r := range roots {
			rootList = append(rootList, r)
		}
		candidates := eng.gatherSwarmCandidates(context.Background(), nil, nil, rootList, map[string]bool{hash: true})
		if len(candidates) == 0 || !strings.Contains(candidates[0].magnet, hybridHash) {
			t.Fatalf("%s: root lookup proposed %+v, want the hybrid first", hash, candidates)
		}
	}
}
