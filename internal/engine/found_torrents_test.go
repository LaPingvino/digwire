package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A .torrent that came with a download is offered, never started on its own, and says what of it
// is already on disk so the interface can offer seeding, resuming or downloading.
func TestFoundTorrentsAreOfferedNotStarted(t *testing.T) {
	eng, downloadDir := newTestEngine(t)
	pack := filepath.Join(downloadDir, "Some Release")
	writeRandomFile(t, filepath.Join(pack, "movie.mkv"), 6*testPieceLen)

	// Complete: built from what is on disk, at the place it would download to.
	completeMI := v1MetaInfo(t, pack, "Some Release", testPieceLen)
	writeMetaInfoFile(t, filepath.Join(pack, "bundled.torrent"), completeMI)

	// Missing: describes content that is not here.
	elsewhere := filepath.Join(t.TempDir(), "Other Release")
	writeRandomFile(t, filepath.Join(elsewhere, "other.mkv"), 4*testPieceLen)
	missingMI := v1MetaInfo(t, elsewhere, "Other Release", testPieceLen)
	writeMetaInfoFile(t, filepath.Join(pack, "extra.torrent"), missingMI)

	// Partial: half the file is there.
	partialDir := filepath.Join(downloadDir, "Half Release")
	data := writeRandomFile(t, filepath.Join(partialDir, "half.mkv"), 8*testPieceLen)
	partialMI := v1MetaInfo(t, partialDir, "Half Release", testPieceLen)
	if err := os.WriteFile(filepath.Join(partialDir, "half.mkv"), data[:4*testPieceLen], 0644); err != nil {
		t.Fatal(err)
	}
	writeMetaInfoFile(t, filepath.Join(pack, "half.torrent"), partialMI)

	byName := map[string]FoundTorrent{}
	for _, f := range eng.FoundTorrents() {
		byName[f.Name] = f
	}
	if len(byName) != 3 {
		t.Fatalf("found %d torrents, want 3: %+v", len(byName), byName)
	}
	for name, wantStatus := range map[string]string{"Some Release": "complete", "Other Release": "missing", "Half Release": "partial"} {
		if got := byName[name].Status; got != wantStatus {
			t.Errorf("%s: status %q, want %q", name, got, wantStatus)
		}
		if byName[name].Source != "download" {
			t.Errorf("%s: source %q, want download", name, byName[name].Source)
		}
	}
	if len(eng.GetTorrents()) != 0 {
		t.Fatal("a found torrent was started without being asked")
	}

	// Metadata Digwire kept from a search is not a list of the user's business: a cached torrent
	// whose data is nowhere on disk must not be offered.
	cached := v1MetaInfo(t, elsewhere, "Something A Search Turned Up", testPieceLen)
	writeMetaInfoFile(t, filepath.Join(eng.getTorrentsCacheDir(), strings.ToLower(cached.HashInfoBytes().HexString())+".torrent"), cached)
	for _, f := range eng.FoundTorrents() {
		if f.Name == "Something A Search Turned Up" {
			t.Fatal("cached search metadata with no local data is offered as found")
		}
	}

	// Adding one is an explicit act, and then it is no longer offered.
	hash, err := eng.AddFoundTorrent(byName["Some Release"].InfoHash)
	if err != nil {
		t.Fatal(err)
	}
	tor, _ := eng.findUserTorrent(hash)
	if tor == nil {
		t.Fatal("added torrent is not in the client")
	}
	verifyNow(t, eng, tor)
	waitFor(t, "added torrent to be complete from local files", func() bool { return tor.BytesCompleted() == tor.Length() })
	for _, f := range eng.FoundTorrents() {
		if f.InfoHash == strings.ToLower(hash) {
			t.Fatal("an added torrent is still offered as found")
		}
	}

	// Dismissing hides one until it is restored.
	eng.DismissFoundTorrent(byName["Other Release"].InfoHash)
	for _, f := range eng.FoundTorrents() {
		if f.Name == "Other Release" {
			t.Fatal("dismissed torrent is still offered")
		}
	}
	eng.RestoreFoundTorrents()
	var restored bool
	for _, f := range eng.FoundTorrents() {
		restored = restored || f.Name == "Other Release"
	}
	if !restored {
		t.Fatal("restoring did not bring the dismissed torrent back")
	}
}

// A torrent Digwire made for a download whose files are gone is junk: nothing to seed, nobody to
// get it from. It is marked as such and can be deleted.
func TestLeftoverTorrentsCanBeCleanedUp(t *testing.T) {
	eng, downloadDir := newTestEngine(t)
	source := filepath.Join(t.TempDir(), "Gone Album")
	writeRandomFile(t, filepath.Join(source, "track.flac"), 5*testPieceLen)
	mi, err := BuildBEP52MetaInfo(source, true, "Created by Digwire from Soulseek", nil)
	if err != nil {
		t.Fatal(err)
	}
	leftoverPath := filepath.Join(downloadDir, "Gone Album.torrent")
	writeMetaInfoFile(t, leftoverPath, mi)

	// One that came from elsewhere, also without data: not ours to delete.
	theirs := filepath.Join(t.TempDir(), "Someone Elses")
	writeRandomFile(t, filepath.Join(theirs, "film.mkv"), 4*testPieceLen)
	writeMetaInfoFile(t, filepath.Join(downloadDir, "theirs.torrent"), v1MetaInfo(t, theirs, "Someone Elses", testPieceLen))

	var leftover, other *FoundTorrent
	for _, f := range eng.FoundTorrents() {
		entry := f
		if entry.Name == "Gone Album" {
			leftover = &entry
		}
		if entry.Name == "Someone Elses" {
			other = &entry
		}
	}
	if leftover == nil || !leftover.MadeHere || !leftover.Leftover {
		t.Fatalf("our own leftover not recognised: %+v", leftover)
	}
	if other == nil || other.MadeHere || other.Leftover {
		t.Fatalf("a torrent from elsewhere was called a leftover: %+v", other)
	}

	removed, err := eng.DeleteLeftoverTorrentFiles()
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("removed %d files, want 1", removed)
	}
	if _, err := os.Stat(leftoverPath); !os.IsNotExist(err) {
		t.Fatalf("leftover file still there (err=%v)", err)
	}
	if _, err := os.Stat(filepath.Join(downloadDir, "theirs.torrent")); err != nil {
		t.Fatalf("someone else's torrent file was deleted: %v", err)
	}
}

// The v1 release of something now seeded as hybrid holds the very same files. Saying so makes the
// offer honest: adding it serves that older swarm as well, from the files already on disk.
func TestFoundTorrentIsRecognisedAsAnotherReleaseOfALocalTorrent(t *testing.T) {
	eng, downloadDir := newTestEngine(t)
	show := filepath.Join(downloadDir, "Release")
	writeRandomFile(t, filepath.Join(show, "movie.mkv"), 7*testPieceLen)

	hybridMI, err := BuildBEP52MetaInfo(show, true, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	hybrid := addLocalTorrent(t, eng, hybridMI)
	waitFor(t, "hybrid to complete", func() bool { return hybrid.BytesCompleted() == hybrid.Length() })

	// The older v1 of the same content, still lying around as a .torrent.
	writeMetaInfoFile(t, filepath.Join(downloadDir, "old-v1.torrent"), v1MetaInfo(t, show, "Release", testPieceLen))

	var entry *FoundTorrent
	for _, f := range eng.FoundTorrents() {
		found := f
		if found.Name == "Release" {
			entry = &found
		}
	}
	if entry == nil {
		t.Fatal("the v1 release is not offered")
	}
	if entry.Status != "complete" {
		t.Fatalf("status %q, want complete", entry.Status)
	}
	if entry.SameAsHash != strings.ToLower(hybrid.InfoHash().HexString()) {
		t.Fatalf("same_as_hash %q, want the local hybrid %s", entry.SameAsHash, hybrid.InfoHash().HexString())
	}
}
