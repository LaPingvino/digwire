package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"digwire/internal/config"
	"github.com/anacrolix/torrent/metainfo"
)

func TestBuildBEP52MetaInfo(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	fileA := filepath.Join(tmpDir, "fileA.txt")
	fileB := filepath.Join(tmpDir, "sub", "fileB.bin")
	_ = os.MkdirAll(filepath.Dir(fileB), 0755)

	dataA := make([]byte, 25*1024) // 25 KiB (> 16 KiB block)
	for i := range dataA {
		dataA[i] = byte(i % 256)
	}
	if err := os.WriteFile(fileA, dataA, 0644); err != nil {
		t.Fatalf("failed to write fileA: %v", err)
	}

	dataB := make([]byte, 100*1024) // 100 KiB
	for i := range dataB {
		dataB[i] = byte((i * 7) % 256)
	}
	if err := os.WriteFile(fileB, dataB, 0644); err != nil {
		t.Fatalf("failed to write fileB: %v", err)
	}

	// 1. Build Pure v2
	miV2, errV2 := BuildBEP52MetaInfo(tmpDir, false, "Test pure v2", nil)
	if errV2 != nil {
		t.Fatalf("BuildBEP52MetaInfo pure v2 failed: %v", errV2)
	}
	t.Logf("miV2.InfoBytes: %q", string(miV2.InfoBytes))
	infoV2, errInfoV2 := miV2.UnmarshalInfo()
	if errInfoV2 != nil {
		t.Fatalf("failed to unmarshal info: %v", errInfoV2)
	}
	if !infoV2.HasV2() {
		t.Fatal("expected pure v2 to have HasV2() == true")
	}
	if infoV2.HasV1() {
		t.Fatal("expected pure v2 to have HasV1() == false")
	}
	if err := metainfo.ValidatePieceLayers(miV2.PieceLayers, &infoV2.FileTree, infoV2.PieceLength); err != nil {
		t.Fatalf("ValidatePieceLayers failed for v2: %v", err)
	}
	magV2, errMagV2 := miV2.MagnetV2()
	if errMagV2 != nil {
		t.Fatalf("failed to generate magnet v2: %v", errMagV2)
	}
	t.Logf("Generated pure v2 magnet: %s", magV2.String())

	// 2. Build Hybrid (v1 + v2)
	miHyb, errHyb := BuildBEP52MetaInfo(tmpDir, true, "Test hybrid", nil)
	if errHyb != nil {
		t.Fatalf("BuildBEP52MetaInfo hybrid failed: %v", errHyb)
	}
	infoHyb, errInfoHyb := miHyb.UnmarshalInfo()
	if errInfoHyb != nil {
		t.Fatalf("failed to unmarshal info: %v", errInfoHyb)
	}
	if !infoHyb.HasV2() {
		t.Fatal("expected hybrid to have HasV2() == true")
	}
	if !infoHyb.HasV1() {
		t.Fatal("expected hybrid to have HasV1() == true")
	}
	if err := metainfo.ValidatePieceLayers(miHyb.PieceLayers, &infoHyb.FileTree, infoHyb.PieceLength); err != nil {
		t.Fatalf("ValidatePieceLayers failed for hybrid: %v", err)
	}
	magHyb, errMagHyb := miHyb.MagnetV2()
	if errMagHyb != nil {
		t.Fatalf("failed to generate magnet hybrid: %v", errMagHyb)
	}
	t.Logf("Generated hybrid magnet: %s", magHyb.String())

	// 3. Test seeding the hybrid torrent in Engine
	cfg := config.DefaultConfig()
	cfg.SetConfigPath(filepath.Join(tmpDir, "config.yaml"))
	cfg.DownloadDir = tmpDir
	cfg.ListenPort = 0
	eng, errEng := NewEngine(cfg)
	if errEng != nil {
		t.Fatalf("failed to create engine: %v", errEng)
	}
	defer eng.Close()

	tor, errAdd := eng.SeedMetaInfo(miHyb, filepath.Dir(tmpDir))
	if errAdd != nil {
		t.Fatalf("failed to seed hybrid metainfo: %v", errAdd)
	}
	if tor == nil {
		t.Fatal("expected non-nil torrent")
	}
	t.Logf("Seeding hybrid torrent: %s, infohash: %s", tor.Name(), tor.InfoHash().HexString())

	stats := eng.GetGlobalStats()
	if stats.TotalHybrid != 1 {
		t.Fatalf("expected 1 TotalHybrid in stats, got %d", stats.TotalHybrid)
	}
}

func TestUpgradeToBEP52(t *testing.T) {
	tmpDir := t.TempDir()
	sourceDir := filepath.Join(tmpDir, "my-payload")
	_ = os.MkdirAll(sourceDir, 0755)

	testFile := filepath.Join(sourceDir, "document.pdf")
	dummyData := make([]byte, 64*1024) // 64 KiB
	for i := range dummyData {
		dummyData[i] = byte(i % 128)
	}
	_ = os.WriteFile(testFile, dummyData, 0644)

	cfg := config.DefaultConfig()
	cfg.SetConfigPath(filepath.Join(tmpDir, "config.yaml"))
	cfg.DownloadDir = tmpDir
	cfg.ListenPort = 0
	eng, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer eng.Close()

	// 1. Create classic v1 torrent
	v1Hash, v1Mag, errCreate := eng.CreateTorrent(sourceDir, "Initial v1 test", "v1")
	if errCreate != nil {
		t.Fatalf("failed to create v1 torrent: %v", errCreate)
	}
	t.Logf("Created v1 torrent: %s (%s)", v1Hash, v1Mag)

	// 2. Upgrade to BEP 52 Hybrid
	hybHash, hybMag, errUpgrade := eng.UpgradeToBEP52(v1Hash)
	if errUpgrade != nil {
		t.Fatalf("failed to upgrade to BEP 52: %v", errUpgrade)
	}
	t.Logf("Upgraded to BEP 52 Hybrid: %s (%s)", hybHash, hybMag)

	if !strings.Contains(hybMag, "xt=urn:btmh:") {
		t.Fatalf("upgraded magnet must contain xt=urn:btmh: %s", hybMag)
	}
	if !strings.Contains(hybMag, "xt=urn:btih:") {
		t.Fatalf("hybrid magnet must also contain xt=urn:btih: %s", hybMag)
	}

	stats := eng.GetGlobalStats()
	if stats.TotalHybrid < 1 {
		t.Fatalf("expected TotalHybrid >= 1, got %d", stats.TotalHybrid)
	}
}

// A single file becomes a single-file hybrid (no directory, no trailing padding) and seeds from the
// file where it is, rather than from name/name.
func TestSingleFileHybridSeedsInPlace(t *testing.T) {
	eng, downloadDir := newTestEngine(t)
	path := filepath.Join(downloadDir, "image.iso")
	writeRandomFile(t, path, 3*testPieceLen+100)

	mi, err := BuildBEP52MetaInfo(path, true, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	info := unmarshalInfo(t, mi)
	if len(info.Files) != 0 || info.Length != 3*testPieceLen+100 {
		t.Fatalf("expected a single-file v1 part, got length %d with %d files", info.Length, len(info.Files))
	}

	tor, err := eng.SeedMetaInfo(mi, downloadDir)
	if err != nil {
		t.Fatal(err)
	}
	if tor.BytesCompleted() != tor.Length() {
		t.Fatalf("seeded single-file hybrid has %d of %d bytes", tor.BytesCompleted(), tor.Length())
	}
	if _, err := os.Stat(filepath.Join(downloadDir, "image.iso", "image.iso")); err == nil {
		t.Fatal("single-file hybrid stored as name/name")
	}
}

// A hybrid of a directory holding a single file stays a directory on disk.
func TestSingleFileDirectoryHybridKeepsDirectory(t *testing.T) {
	eng, downloadDir := newTestEngine(t)
	dir := filepath.Join(downloadDir, "Movie Release")
	writeRandomFile(t, filepath.Join(dir, "movie.mkv"), 3*testPieceLen+100)

	mi, err := BuildBEP52MetaInfo(dir, true, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	tor, err := eng.SeedMetaInfo(mi, downloadDir)
	if err != nil {
		t.Fatal(err)
	}
	if tor.BytesCompleted() != tor.Length() {
		t.Fatalf("seeded directory hybrid has %d of %d bytes", tor.BytesCompleted(), tor.Length())
	}
}

