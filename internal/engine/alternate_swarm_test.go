package engine

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"

	"digwire/internal/config"
	"digwire/internal/dhtindex"
)

const testPieceLen = 16384

func writeRandomFile(t *testing.T, path string, size int) []byte {
	t.Helper()
	data := make([]byte, size)
	_, _ = rand.Read(data)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return data
}

func buildV1Info(t *testing.T, root string, pieceLen int64) *metainfo.Info {
	t.Helper()
	info := &metainfo.Info{PieceLength: pieceLen}
	if err := info.BuildFromFilePath(root); err != nil {
		t.Fatal(err)
	}
	return info
}

func unmarshalInfo(t *testing.T, mi *metainfo.MetaInfo) *metainfo.Info {
	t.Helper()
	info, err := mi.UnmarshalInfo()
	if err != nil {
		t.Fatal(err)
	}
	return &info
}

func TestMatchFilesByPieceHashesAgainstHybrid(t *testing.T) {
	dir := t.TempDir()
	movie := writeRandomFile(t, filepath.Join(dir, "Release", "a.mkv"), 6*testPieceLen+123)
	writeRandomFile(t, filepath.Join(dir, "Release", "b.nfo"), 700)

	hybridMI, err := BuildBEP52MetaInfo(filepath.Join(dir, "Release"), true, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	hybrid := unmarshalInfo(t, hybridMI)
	if !hybrid.HasV1() || !hybrid.HasV2() {
		t.Fatalf("expected a hybrid torrent")
	}
	v1 := buildV1Info(t, filepath.Join(dir, "Release"), hybrid.PieceLength)

	matches := matchFilesByPieceHashes(v1, hybrid)
	if len(matches) != 1 || v1.UpvertedFiles()[matches[0].oursIndex].Length != int64(len(movie)) {
		t.Fatalf("expected the movie to match, got %+v", matches)
	}

	// Different content of the same length must not match.
	movie[3*testPieceLen] ^= 0xff
	if err := os.WriteFile(filepath.Join(dir, "Release", "a.mkv"), movie, 0644); err != nil {
		t.Fatal(err)
	}
	changed := buildV1Info(t, filepath.Join(dir, "Release"), hybrid.PieceLength)
	if matches := matchFilesByPieceHashes(changed, hybrid); len(matches) != 0 {
		t.Fatalf("modified file matched: %+v", matches)
	}
}

func verifyNow(t *testing.T, eng *Engine, tor *torrent.Torrent) {
	t.Helper()
	done := make(chan struct{})
	eng.ConsolidateAndVerifyForce(tor, true, func() { close(done) })
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("verification did not finish")
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func registerCandidate(t *testing.T, eng *Engine, ours *torrent.Torrent, mi *metainfo.MetaInfo) []fileMatch {
	t.Helper()
	matches := eng.matchTorrentFiles(ours, mi, unmarshalInfo(t, mi))
	if len(matches) == 0 {
		t.Fatal("candidate did not match local data")
	}
	eng.mu.Lock()
	defer eng.mu.Unlock()
	eng.alternateSwarms = map[string]map[string]alternateCandidate{
		strings.ToLower(ours.InfoHash().HexString()): {strings.ToLower(mi.HashInfoBytes().HexString()): {mi: mi, matches: matches}},
	}
	return matches
}

func newTestEngine(t *testing.T) (*Engine, string) {
	t.Helper()
	tempDir := t.TempDir()
	// Keep the DHT index and torrent cache out of the real user config.
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempDir, "xdg"))
	downloadDir := filepath.Join(tempDir, "downloads")
	cfg := &config.Config{DownloadDir: downloadDir}
	cfg.SetConfigPath(filepath.Join(tempDir, "config.yaml"))
	eng, err := NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(eng.Close)
	return eng, downloadDir
}

// addPartialV1Download adds a paused v1 torrent of data with piece length pl, of which only the
// first `have` bytes are on disk, and verifies it.
func addPartialV1Download(t *testing.T, eng *Engine, path string, data []byte, pl int64, have int) *torrent.Torrent {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	info := buildV1Info(t, filepath.Dir(path), pl)
	partial := make([]byte, len(data))
	copy(partial, data[:have])
	if err := os.WriteFile(path, partial, 0644); err != nil {
		t.Fatal(err)
	}
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	tor, _, err := eng.client.AddTorrentSpec(torrent.TorrentSpecFromMetaInfo(&metainfo.MetaInfo{InfoBytes: infoBytes}))
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.ToLower(tor.InfoHash().HexString())
	eng.mu.Lock()
	eng.initTracker(hash)
	eng.rateMap[hash].isPaused = true
	eng.mu.Unlock()
	verifyNow(t, eng, tor)
	return tor
}

// A release with a different piece size can't be compared hash-for-hash, so the downloaded half of
// our file is hashed against the hybrid's pieces instead.
func TestMatchByHashingDownloadedDataAcrossPieceSizes(t *testing.T) {
	eng, downloadDir := newTestEngine(t)
	elsewhere := filepath.Join(filepath.Dir(downloadDir), "elsewhere", "Hybrid Release")
	movie := writeRandomFile(t, filepath.Join(elsewhere, "movie.mkv"), 16*testPieceLen)
	hybridMI, err := BuildBEP52MetaInfo(elsewhere, true, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	hybrid := unmarshalInfo(t, hybridMI)

	ourPath := filepath.Join(downloadDir, "Release", "Movie.mkv")
	ours := addPartialV1Download(t, eng, ourPath, movie, 2*hybrid.PieceLength, len(movie)/2)
	if ours.BytesCompleted() == 0 || ours.BytesCompleted() == ours.Length() {
		t.Fatalf("expected a partial download, have %d of %d", ours.BytesCompleted(), ours.Length())
	}
	if m := matchFilesByPieceHashes(ours.Info(), hybrid); len(m) != 0 {
		t.Fatalf("piece grids differ, metadata-only matching should not apply: %+v", m)
	}
	matches := eng.matchTorrentFiles(ours, hybridMI, hybrid)
	if len(matches) != 1 || matches[0].checked == 0 {
		t.Fatalf("expected a match proven by hashing downloaded data, got %+v", matches)
	}

	// A different release of the same size must be discarded.
	other := append([]byte(nil), movie...)
	other[testPieceLen+7] ^= 0xff
	otherDir := filepath.Join(filepath.Dir(downloadDir), "other", "Hybrid Release")
	if err := os.MkdirAll(otherDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherDir, "movie.mkv"), other, 0644); err != nil {
		t.Fatal(err)
	}
	otherMI, err := BuildBEP52MetaInfo(otherDir, true, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if m := eng.matchTorrentFiles(ours, otherMI, unmarshalInfo(t, otherMI)); len(m) != 0 {
		t.Fatalf("different content matched: %+v", m)
	}

	// Attach, finish the data through the hybrid, and let the byte-range sync complete ours.
	registerCandidate(t, eng, ours, hybridMI)
	attached := make(chan struct{})
	altHash, err := eng.AttachAlternateSwarm(ours.InfoHash().HexString(), hybridMI.HashInfoBytes().HexString(), func() { close(attached) })
	if err != nil {
		t.Fatal(err)
	}
	<-attached
	if err := os.WriteFile(ourPath, movie, 0644); err != nil {
		t.Fatal(err)
	}
	eng.mu.RLock()
	alt, altTr := eng.findUserTorrent(altHash)
	eng.mu.RUnlock()
	waitFor(t, "hybrid swarm to verify the full file", func() bool {
		verifyNow(t, eng, alt)
		return alt.BytesCompleted() == alt.Length()
	})
	waitFor(t, "v1 download to complete from the hybrid's pieces", func() bool {
		eng.syncSiblingPieces(alt, altTr, ours)
		return ours.BytesCompleted() == ours.Length()
	})
}

// Pure v2 torrents carry no v1 piece hashes: partial data is checked against piece layers, and a
// complete file against its root.
func TestVerifyFileBySamplingV2(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "V2 Release")
	movie := writeRandomFile(t, filepath.Join(root, "movie.mkv"), 16*testPieceLen)
	mi, err := BuildBEP52MetaInfo(root, false, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	info := unmarshalInfo(t, mi)
	if info.HasV1() {
		t.Fatal("expected a pure v2 torrent")
	}
	file := info.UpvertedFiles()[0]
	half := func(off, n int64) bool { return off+n <= int64(len(movie))/2 }
	all := func(off, n int64) bool { return true }
	ld := localData{path: filepath.Join(root, "movie.mkv"), length: int64(len(movie)), have: half}

	if len(mi.PieceLayers) == 0 {
		t.Fatal("expected piece layers in the built v2 torrent")
	}
	if checked, ok := verifyFileBySampling(ld, mi, info, file); !ok || checked == 0 {
		t.Fatalf("partial data not verified against piece layers: checked=%d ok=%v", checked, ok)
	}
	withoutLayers := *mi
	withoutLayers.PieceLayers = nil
	if _, ok := verifyFileBySampling(ld, &withoutLayers, info, file); ok {
		t.Fatal("partial data cannot be verified without piece layers")
	}
	ld.have = all
	if _, ok := verifyFileBySampling(ld, &withoutLayers, info, file); !ok {
		t.Fatal("complete file not verified against its pieces root")
	}
	movie[5] ^= 0xff
	if err := os.WriteFile(ld.path, movie, 0644); err != nil {
		t.Fatal(err)
	}
	if _, ok := verifyFileBySampling(ld, &withoutLayers, info, file); ok {
		t.Fatal("modified file verified against pieces root")
	}
}

func TestAttachAlternateSwarmSharesFiles(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempDir, "xdg"))
	downloadDir := filepath.Join(tempDir, "downloads")
	cfg := &config.Config{DownloadDir: downloadDir}
	cfg.SetConfigPath(filepath.Join(tempDir, "config.yaml"))
	eng, err := NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	// The alternate release names things differently but holds the same movie.
	altRoot := filepath.Join(tempDir, "elsewhere", "Other Release")
	movie := writeRandomFile(t, filepath.Join(altRoot, "movie.mkv"), 6*testPieceLen)
	altMI, err := BuildBEP52MetaInfo(altRoot, true, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	altInfo := unmarshalInfo(t, altMI)

	// Our v1 download: same movie, only its first two pieces downloaded so far.
	ourPath := filepath.Join(downloadDir, "Release", "Movie.mkv")
	writeRandomFile(t, ourPath, len(movie))
	if err := os.WriteFile(ourPath, movie, 0644); err != nil {
		t.Fatal(err)
	}
	ourInfo := buildV1Info(t, filepath.Join(downloadDir, "Release"), altInfo.PieceLength)
	partial := make([]byte, len(movie))
	copy(partial, movie[:2*altInfo.PieceLength])
	if err := os.WriteFile(ourPath, partial, 0644); err != nil {
		t.Fatal(err)
	}
	infoBytes, err := bencode.Marshal(ourInfo)
	if err != nil {
		t.Fatal(err)
	}
	ours, _, err := eng.client.AddTorrentSpec(torrent.TorrentSpecFromMetaInfo(&metainfo.MetaInfo{InfoBytes: infoBytes}))
	if err != nil {
		t.Fatal(err)
	}
	ourHash := strings.ToLower(ours.InfoHash().HexString())
	eng.mu.Lock()
	eng.initTracker(ourHash)
	eng.rateMap[ourHash].isPaused = true
	eng.mu.Unlock()
	verifyNow(t, eng, ours)
	registerCandidate(t, eng, ours, altMI)

	attached := make(chan struct{})
	altHash, err := eng.AttachAlternateSwarm(ourHash, altMI.HashInfoBytes().HexString(), func() { close(attached) })
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-attached:
	case <-time.After(30 * time.Second):
		t.Fatal("attached swarm verification did not finish")
	}
	eng.mu.RLock()
	alt, _ := eng.findUserTorrent(altHash)
	eng.mu.RUnlock()
	if got, want := alt.BytesCompleted(), 2*altInfo.PieceLength; got != want {
		t.Fatalf("attached swarm verified %d bytes of existing data, want %d", got, want)
	}
	if _, err := os.Stat(filepath.Join(downloadDir, "Other Release")); !os.IsNotExist(err) {
		t.Fatalf("attached swarm created its own copy instead of sharing files: %v", err)
	}

	// Data arriving through the attached swarm is picked up by the original torrent.
	if err := os.WriteFile(ourPath, movie, 0644); err != nil {
		t.Fatal(err)
	}
	// A hash the client started on the old contents can still land after ours, so retry the way
	// the periodic sync does.
	waitFor(t, "attached swarm to verify the new data", func() bool {
		verifyNow(t, eng, alt)
		return alt.BytesCompleted() == alt.Length()
	})
	eng.mu.RLock()
	altTr := eng.rateMap[altHash]
	eng.mu.RUnlock()
	waitFor(t, "original torrent to pick up sibling pieces", func() bool {
		eng.syncSiblingPieces(alt, altTr, ours)
		return ours.BytesCompleted() == ours.Length()
	})

	eng.mu.Lock()
	eng.saveSessionLocked()
	saved := eng.savedTorrentsMap[altHash]
	eng.mu.Unlock()
	if saved.SiblingOf != ourHash || saved.FileMap["movie.mkv"] != filepath.Join("Release", "Movie.mkv") {
		t.Fatalf("attached swarm not persisted: %+v", saved)
	}
}

func TestRemovingTorrentDropsAttachedSwarms(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempDir, "xdg"))
	cfg := &config.Config{DownloadDir: filepath.Join(tempDir, "downloads")}
	cfg.SetConfigPath(filepath.Join(tempDir, "config.yaml"))
	eng, err := NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	root := filepath.Join(tempDir, "src", "Release")
	writeRandomFile(t, filepath.Join(root, "movie.mkv"), 4*testPieceLen)
	add := func(info *metainfo.Info) string {
		infoBytes, err := bencode.Marshal(info)
		if err != nil {
			t.Fatal(err)
		}
		tor, _, err := eng.client.AddTorrentSpec(torrent.TorrentSpecFromMetaInfo(&metainfo.MetaInfo{InfoBytes: infoBytes}))
		if err != nil {
			t.Fatal(err)
		}
		return strings.ToLower(tor.InfoHash().HexString())
	}
	ourHash := add(buildV1Info(t, root, testPieceLen))
	altHash := add(buildV1Info(t, filepath.Join(tempDir, "src"), testPieceLen))
	eng.mu.Lock()
	eng.initTracker(ourHash)
	eng.initTracker(altHash)
	eng.rateMap[altHash].siblingHash = ourHash
	eng.mu.Unlock()

	if err := eng.Remove(ourHash, false); err != nil {
		t.Fatal(err)
	}
	eng.mu.RLock()
	defer eng.mu.RUnlock()
	if tor, tr := eng.findUserTorrent(altHash); tor != nil || tr != nil {
		t.Fatalf("attached swarm survived removal of its torrent")
	}
}

func TestRemovingAttachedSwarmKeepsSharedFiles(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempDir, "xdg"))
	downloadDir := filepath.Join(tempDir, "downloads")
	cfg := &config.Config{DownloadDir: downloadDir}
	cfg.SetConfigPath(filepath.Join(tempDir, "config.yaml"))
	eng, err := NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()

	// Same release name as the original, so deleting "its" files would hit the original's folder.
	root := filepath.Join(downloadDir, "Release")
	moviePath := filepath.Join(root, "movie.mkv")
	writeRandomFile(t, moviePath, 4*testPieceLen)
	infoBytes, err := bencode.Marshal(buildV1Info(t, root, 2*testPieceLen))
	if err != nil {
		t.Fatal(err)
	}
	alt, _, err := eng.client.AddTorrentSpec(torrent.TorrentSpecFromMetaInfo(&metainfo.MetaInfo{InfoBytes: infoBytes}))
	if err != nil {
		t.Fatal(err)
	}
	altHash := strings.ToLower(alt.InfoHash().HexString())
	eng.mu.Lock()
	eng.initTracker(altHash)
	eng.rateMap[altHash].siblingHash = "0123456789abcdef0123456789abcdef01234567"
	eng.mu.Unlock()

	if err := eng.Remove(altHash, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(moviePath); err != nil {
		t.Fatalf("removing an attached swarm with files deleted the shared data: %v", err)
	}
}

// A web download is matched against a hybrid by hashing the chunks it already has.
func TestHTTPDownloadVerifiedByDownloadedChunks(t *testing.T) {
	dir := t.TempDir()
	releaseDir := filepath.Join(dir, "Hybrid Release")
	data := writeRandomFile(t, filepath.Join(releaseDir, "image.iso"), int(2*httpChunkSize+12345))
	mi, err := BuildBEP52MetaInfo(releaseDir, true, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	info := unmarshalInfo(t, mi)
	file := info.UpvertedFiles()[0]

	destPath := filepath.Join(dir, "downloads", "image.iso")
	partial := make([]byte, len(data))
	copy(partial[:httpChunkSize], data[:httpChunkSize]) // Only chunk 0 downloaded.
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destPath+".part", partial, 0644); err != nil {
		t.Fatal(err)
	}
	task := &HTTPTask{
		DestPath:        destPath,
		TotalBytes:      int64(len(data)),
		State:           "downloading",
		completedChunks: map[int64]bool{0: true},
	}
	ld, ok := task.localData()
	if !ok {
		t.Fatal("no local data for a partial download")
	}
	if checked, ok := verifyFileBySampling(ld, mi, info, file); !ok || checked == 0 {
		t.Fatalf("downloaded chunk not verified: checked=%d ok=%v", checked, ok)
	}

	// Chunk 1 is zeros on disk but not marked downloaded, so it must not be sampled; if it were, the
	// match would fail. Corrupting chunk 0 must fail.
	partial[100] ^= 0xff
	if err := os.WriteFile(destPath+".part", partial, 0644); err != nil {
		t.Fatal(err)
	}
	if _, ok := verifyFileBySampling(ld, mi, info, file); ok {
		t.Fatal("corrupt downloaded data verified")
	}
}

// The DHT index proposes releases holding a file of exactly the right size, whatever their name.
func TestGatherSwarmCandidatesByFileSize(t *testing.T) {
	eng, _ := newTestEngine(t)
	if eng.dhtIndexer == nil {
		t.Fatal("no DHT index")
	}
	v1 := strings.Repeat("a", 40)
	v2 := strings.Repeat("b", 64)
	eng.dhtIndexer.AddRecord(&dhtindex.DHTRecord{
		InfoHash:        v1,
		InfoHashV2:      v2,
		ProtocolVersion: "hybrid",
		Name:            "Completely Different Name",
		SizeBytes:       123456789,
		NumFiles:        1,
		Files:           []string{"x.mkv"},
		FileEntries:     []dhtindex.DHTFileEntry{{Path: "x.mkv", SizeBytes: 123456789}},
	})
	candidates := eng.gatherSwarmCandidates(context.Background(), "My Movie", []int64{123456789}, nil)
	if len(candidates) == 0 || !strings.Contains(candidates[0].magnet, v1) || !strings.Contains(candidates[0].magnet, "btmh:1220"+v2) {
		t.Fatalf("expected the hybrid with a same-size file as candidate, got %+v", candidates)
	}
	if got := eng.gatherSwarmCandidates(context.Background(), "My Movie", []int64{123456789}, map[string]bool{v1: true}); len(got) != 0 {
		t.Fatalf("excluded hash still proposed: %+v", got)
	}
}
