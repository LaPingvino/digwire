package engine

import (
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

func TestAttachAlternateSwarmSharesFiles(t *testing.T) {
	tempDir := t.TempDir()
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
	eng.alternateSwarms = map[string]map[string]alternateCandidate{
		ourHash: {strings.ToLower(altMI.HashInfoBytes().HexString()): {mi: altMI}},
	}
	eng.mu.Unlock()
	verifyNow(t, eng, ours)

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
