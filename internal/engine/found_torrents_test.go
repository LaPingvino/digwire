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
