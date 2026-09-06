package search

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bh90210/soul/peer"
)

func TestSoulseekPeerSemaphore(t *testing.T) {
	client := GetSoulseekClient()

	sem1 := client.getPeerSem("TestUser")
	sem2 := client.getPeerSem("testuser")
	sem3 := client.getPeerSem("  TESTUSER  ")
	semOther := client.getPeerSem("OtherUser")

	if sem1 != sem2 {
		t.Errorf("expected sem1 and sem2 to be identical channel for same user")
	}
	if sem1 != sem3 {
		t.Errorf("expected sem1 and sem3 to be identical channel for same user with whitespace/casing")
	}
	if sem1 == semOther {
		t.Errorf("expected different semaphores for different users")
	}

	// Verify channel capacity is 1 (strict serialization per peer)
	if cap(sem1) != 1 {
		t.Errorf("expected semaphore capacity 1, got %d", cap(sem1))
	}
}

func TestSoulseekURIParsing(t *testing.T) {
	res := parseSoulseekFile("ElectroUser", peer.File{
		Name: "Music\\Caravan Palace\\Panic\\01 - Rock It For Me.mp3",
		Size: 4096,
	}, true, 0, 1000, "online")

	if !strings.Contains(res.Artist, "Caravan Palace") {
		t.Errorf("expected artist to be Caravan Palace, got '%s'", res.Artist)
	}
	if !strings.Contains(res.Album, "Panic") {
		t.Errorf("expected album to be Panic, got '%s'", res.Album)
	}
	if res.Seeders != 1 {
		t.Errorf("expected 1 seeder for free slot, got %d", res.Seeders)
	}
	if res.PeerStatus != "online" {
		t.Errorf("expected PeerStatus 'online', got %s", res.PeerStatus)
	}
}

func TestSoulseekOfflineTracking(t *testing.T) {
	client := GetSoulseekClient()
	if client.IsPeerOffline("test_offline_user") {
		t.Errorf("expected user not to be offline initially")
	}

	client.MarkPeerOffline("test_offline_user")
	if !client.IsPeerOffline("test_offline_user") {
		t.Errorf("expected user to be detected as offline after MarkPeerOffline")
	}
	if !client.IsPeerOffline("TEST_OFFLINE_USER") {
		t.Errorf("expected case-insensitive match for offline user")
	}
}

func TestSoulseekTruthfulSharesScanning(t *testing.T) {
	tempDir := t.TempDir()

	// Create test folder structure
	// Album 1 (Music): 2 mp3s, 1 flac, 1 part file (temporary)
	album1 := filepath.Join(tempDir, "Artist - Album1")
	_ = os.MkdirAll(album1, 0755)
	_ = os.WriteFile(filepath.Join(album1, "01.mp3"), []byte("mp3 content 1"), 0644)
	_ = os.WriteFile(filepath.Join(album1, "02.mp3"), []byte("mp3 content 22"), 0644)
	_ = os.WriteFile(filepath.Join(album1, "03.flac"), []byte("flac content 333"), 0644)
	_ = os.WriteFile(filepath.Join(album1, "04.mp3.part"), []byte("incomplete part"), 0644)

	// Album 2 (Movies / Non-music): 1 mkv, 1 srt, 1 empty txt (size 0)
	album2 := filepath.Join(tempDir, "Movie - Title")
	_ = os.MkdirAll(album2, 0755)
	_ = os.WriteFile(filepath.Join(album2, "movie.mkv"), []byte("mkv content"), 0644)
	_ = os.WriteFile(filepath.Join(album2, "sub.srt"), []byte("srt subtitles"), 0644)
	_ = os.WriteFile(filepath.Join(album2, "empty.txt"), []byte(""), 0644)

	// Staging dir: must be ignored by scanner
	stagingDir := filepath.Join(tempDir, ".slsk_stage")
	_ = os.MkdirAll(stagingDir, 0755)
	_ = os.WriteFile(filepath.Join(stagingDir, "stage.mp3"), []byte("stage"), 0644)

	client := GetSoulseekClient()

	// Mode 1: "none" (default off) -> 0 folders, 0 files (truthful!)
	client.SetShareConfig(tempDir, "none", "")
	dirs, stats := client.ScanShares()
	if stats.FolderCount != 0 || stats.FileCount != 0 || len(dirs) != 0 {
		t.Errorf("expected 0 folders and 0 files for 'none' mode, got %d folders, %d files", stats.FolderCount, stats.FileCount)
	}

	// Mode 2: "music" -> only Album1 (3 files: 2 mp3, 1 flac; .part and .slsk_stage ignored)
	client.SetShareConfig(tempDir, "music", "")
	dirs, stats = client.ScanShares()
	if stats.FolderCount != 1 || stats.FileCount != 3 {
		t.Errorf("expected 1 folder and 3 files for 'music' mode, got %d folders, %d files", stats.FolderCount, stats.FileCount)
	}

	// Mode 3: "all" -> Album1 (3 files) + Album2 (2 files: .mkv and .srt; empty txt ignored) = 2 folders, 5 files
	client.SetShareConfig(tempDir, "all", "")
	dirs, stats = client.ScanShares()
	if stats.FolderCount != 2 || stats.FileCount != 5 {
		t.Errorf("expected 2 folders and 5 files for 'all' mode, got %d folders, %d files", stats.FolderCount, stats.FileCount)
	}

	// Mode 4: "custom" (.mkv, .srt) -> only Album2 (2 files: .mkv and .srt) = 1 folder, 2 files
	client.SetShareConfig(tempDir, "custom", ".mkv, .srt")
	dirs, stats = client.ScanShares()
	if stats.FolderCount != 1 || stats.FileCount != 2 {
		t.Errorf("expected 1 folder and 2 files for 'custom' mode, got %d folders, %d files", stats.FolderCount, stats.FileCount)
	}

	// RegisterSoulseekDir: mark Album1 as completed Soulseek download
	// Now, even with "none" (non-Soulseek downloads off), Album1 should be shared
	client.RegisterSoulseekDir(album1)
	client.SetShareConfig(tempDir, "none", "")
	dirs, stats = client.ScanShares()
	if stats.FolderCount != 1 || stats.FileCount != 3 {
		t.Errorf("expected completed Soulseek download to be shared even when non-soulseek sharing is 'none', got %d folders, %d files", stats.FolderCount, stats.FileCount)
	}
}

