package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

// addLocalTorrent adds a paused torrent from info and verifies it.
func addLocalTorrent(t *testing.T, eng *Engine, mi *metainfo.MetaInfo) *torrent.Torrent {
	t.Helper()
	tor, _, err := eng.client.AddTorrentSpec(torrent.TorrentSpecFromMetaInfo(mi))
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.ToLower(tor.InfoHash().HexString())
	eng.mu.Lock()
	eng.initTracker(hash)
	eng.rateMap[hash].isPaused = true
	eng.markLinkPendingLocked(hash)
	eng.mu.Unlock()
	verifyNow(t, eng, tor)
	tor, _ = eng.findUserTorrent(hash)
	return tor
}

func v1MetaInfo(t *testing.T, root, name string, pieceLen int64) *metainfo.MetaInfo {
	t.Helper()
	info := buildV1Info(t, root, pieceLen)
	info.Name = name
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	return &metainfo.MetaInfo{InfoBytes: infoBytes}
}

// Like Otje: a hybrid holds two folders that also exist elsewhere as independent v1 torrents under
// other names. Adding such a v1 torrent must use the hybrid's files instead of downloading again.
func TestNewTorrentUsesFilesOfLocalTorrent(t *testing.T) {
	eng, downloadDir := newTestEngine(t)
	show := filepath.Join(downloadDir, "Show")
	writeRandomFile(t, filepath.Join(show, "DVD1", "ep1.mkv"), 9*testPieceLen+123)
	writeRandomFile(t, filepath.Join(show, "DVD1", "ep2.mkv"), 7*testPieceLen+5)
	writeRandomFile(t, filepath.Join(show, "DVD2", "ep3.mkv"), 11*testPieceLen+77)

	// The v1 torrents are built from copies, since their files are not where they would download to.
	elsewhere := t.TempDir()
	dvd1 := filepath.Join(elsewhere, "DVD1")
	dvd1Extra := filepath.Join(elsewhere, "DVD1 extra")
	for _, dir := range []string{dvd1, dvd1Extra} {
		for _, f := range []string{"ep1.mkv", "ep2.mkv"} {
			data, err := os.ReadFile(filepath.Join(show, "DVD1", f))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, f), data, 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	writeRandomFile(t, filepath.Join(dvd1Extra, "notes.nfo"), 3*testPieceLen+9)
	dvd1MI := v1MetaInfo(t, dvd1, "Show DVD1 release", 2*testPieceLen)
	extraMI := v1MetaInfo(t, dvd1Extra, "Show DVD1 with extras", testPieceLen)

	hybridMI, err := BuildBEP52MetaInfo(show, true, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	hybrid := addLocalTorrent(t, eng, hybridMI)
	waitFor(t, "hybrid to complete", func() bool { return hybrid.BytesCompleted() == hybrid.Length() })
	hybridHash := strings.ToLower(hybrid.InfoHash().HexString())

	linked := addLocalTorrent(t, eng, dvd1MI)
	// The client's own initial piece checks can still be settling when verification reports back.
	waitFor(t, "linked torrent to complete from local files", func() bool { return linked.BytesCompleted() == linked.Length() })
	if _, err := os.Stat(filepath.Join(downloadDir, "Show DVD1 release")); !os.IsNotExist(err) {
		t.Fatalf("linked torrent created its own folder (err=%v)", err)
	}
	linkedHash := strings.ToLower(linked.InfoHash().HexString())
	eng.mu.RLock()
	tr := eng.rateMap[linkedHash]
	sibling, isLinked := tr.siblingHash, tr.linkedLocal
	eng.mu.RUnlock()
	if sibling != hybridHash || !isLinked {
		t.Fatalf("sibling=%q linked=%v, want %q true", sibling, isLinked, hybridHash)
	}

	// Files without a match download normally to the torrent's own folder.
	partial := addLocalTorrent(t, eng, extraMI)
	extraHash := strings.ToLower(partial.InfoHash().HexString())
	eng.mu.RLock()
	extraMap := eng.rateMap[extraHash].fileMap
	eng.mu.RUnlock()
	if len(extraMap) != 2 {
		t.Fatalf("file map = %v, want the two episodes", extraMap)
	}
	want := partial.Length() - (3*testPieceLen + 9)
	waitFor(t, "partial torrent to have its matched files", func() bool { return partial.BytesCompleted() >= want-testPieceLen })
	if partial.BytesCompleted() >= partial.Length() {
		t.Fatalf("partial torrent is complete without its extra file")
	}
	eng.mu.RLock()
	skipped := len(eng.rateMap[extraHash].skippedFiles)
	eng.mu.RUnlock()
	if skipped != 0 {
		t.Fatalf("unmatched files were skipped; they should download")
	}

	// Deleting a linked torrent with its files leaves the hybrid's files alone.
	if err := eng.Remove(linkedHash, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(show, "DVD1", "ep1.mkv")); err != nil {
		t.Fatalf("hybrid lost its file: %v", err)
	}

	// Deleting the hybrid with its files keeps those the remaining linked torrent still uses.
	if err := eng.Remove(hybridHash, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(show, "DVD1", "ep2.mkv")); err != nil {
		t.Fatalf("file in use by a linked torrent was deleted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(show, "DVD2", "ep3.mkv")); !os.IsNotExist(err) {
		t.Fatalf("unused hybrid file was kept (err=%v)", err)
	}
	eng.mu.RLock()
	stillLinked := eng.rateMap[extraHash] != nil && len(eng.rateMap[extraHash].fileMap) == 2
	eng.mu.RUnlock()
	if !stillLinked {
		t.Fatal("linked torrent was dropped along with its source")
	}
}

// Only a newly added torrent looks for local files; one reverified later keeps its own layout.
func TestReverifyDoesNotLinkExistingTorrent(t *testing.T) {
	eng, downloadDir := newTestEngine(t)
	show := filepath.Join(downloadDir, "Show")
	writeRandomFile(t, filepath.Join(show, "a.mkv"), 6*testPieceLen+1)
	hybridMI, err := BuildBEP52MetaInfo(show, true, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	addLocalTorrent(t, eng, hybridMI)

	elsewhere := filepath.Join(t.TempDir(), "Copy")
	data, _ := os.ReadFile(filepath.Join(show, "a.mkv"))
	_ = os.MkdirAll(elsewhere, 0755)
	_ = os.WriteFile(filepath.Join(elsewhere, "a.mkv"), data, 0644)
	tor, _, err := eng.client.AddTorrentSpec(torrent.TorrentSpecFromMetaInfo(v1MetaInfo(t, elsewhere, "Copy", testPieceLen)))
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.ToLower(tor.InfoHash().HexString())
	eng.mu.Lock()
	eng.initTracker(hash)
	eng.rateMap[hash].isPaused = true
	eng.mu.Unlock()
	verifyNow(t, eng, tor)
	eng.mu.RLock()
	mapped := len(eng.rateMap[hash].fileMap)
	eng.mu.RUnlock()
	if mapped != 0 {
		t.Fatalf("reverified torrent was linked to local files")
	}
}

// A torrent with the same name and layout as a local one lands on the same paths; deleting either
// with files must not remove what the other still needs.
func TestSameNameTorrentsShareFilesSafely(t *testing.T) {
	eng, downloadDir := newTestEngine(t)
	show := filepath.Join(downloadDir, "Show")
	writeRandomFile(t, filepath.Join(show, "a.mkv"), 6*testPieceLen+1)
	writeRandomFile(t, filepath.Join(show, "b.mkv"), 5*testPieceLen+2)

	hybridMI, err := BuildBEP52MetaInfo(show, true, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	addLocalTorrent(t, eng, hybridMI)
	v1 := addLocalTorrent(t, eng, v1MetaInfo(t, show, "Show", testPieceLen))
	waitFor(t, "v1 torrent to complete", func() bool { return v1.BytesCompleted() == v1.Length() })
	if err := eng.Remove(v1.InfoHash().HexString(), true); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"a.mkv", "b.mkv"} {
		if _, err := os.Stat(filepath.Join(show, f)); err != nil {
			t.Fatalf("%s deleted while the hybrid still uses it: %v", f, err)
		}
	}
}

func writeMetaInfoFile(t *testing.T, path string, mi *metainfo.MetaInfo) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := mi.Write(f); err != nil {
		t.Fatal(err)
	}
}
