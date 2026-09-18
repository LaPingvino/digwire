package engine

import (
	"path/filepath"
	"testing"
	"time"
)

// A release holding an empty file (a 0-byte .nfo, say) must convert to hybrid and keep working:
// the empty file shares the last piece's boundary, which used to panic deep in the client and
// leave its lock held, freezing the whole app.
func TestUpgradeToBEP52WithEmptyFile(t *testing.T) {
	eng, downloadDir := newTestEngine(t)
	show := filepath.Join(downloadDir, "Show")
	writeRandomFile(t, filepath.Join(show, "a.mkv"), 40*testPieceLen)
	// Empty files at the start, between and at the end all share a piece boundary with a real file.
	writeRandomFile(t, filepath.Join(show, "a-first.nfo"), 0)
	writeRandomFile(t, filepath.Join(show, "b.nfo"), 0)
	writeRandomFile(t, filepath.Join(show, "c.txt"), 0)
	writeRandomFile(t, filepath.Join(show, "d.mkv"), 17*testPieceLen+13)
	writeRandomFile(t, filepath.Join(show, "z-last.nfo"), 0)

	v1 := addLocalTorrent(t, eng, v1MetaInfo(t, show, "Show", testPieceLen))
	waitFor(t, "v1 complete", func() bool { return v1.BytesCompleted() == v1.Length() })

	done := make(chan error, 1)
	go func() {
		_, _, err := eng.UpgradeToBEP52(v1.InfoHash().HexString())
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("upgrade failed: %v", err)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("upgrade did not finish in 60s")
	}
	// The API must stay responsive right after.
	ok := make(chan struct{})
	go func() { eng.GetTorrents(); close(ok) }()
	select {
	case <-ok:
	case <-time.After(20 * time.Second):
		t.Fatal("GetTorrents blocked after the upgrade")
	}
}

// The stall detector must notice an engine whose monitor loop stopped coming around.
func TestHealthReportsStalledEngine(t *testing.T) {
	eng, _ := newTestEngine(t)
	if rep := eng.Health().Report(); rep.StalledSeconds != 0 {
		t.Fatalf("fresh engine reported stalled: %+v", rep)
	}
	eng.Health().SetLastTickForTest(time.Now().Add(-5 * time.Minute))
	rep := eng.Health().Report()
	if rep.StalledSeconds < 240 {
		t.Fatalf("stalled engine reported %ds", rep.StalledSeconds)
	}
	eng.Health().Tick()
	if rep := eng.Health().Report(); rep.StalledSeconds != 0 {
		t.Fatalf("engine still reported stalled after a tick: %+v", rep)
	}
}
