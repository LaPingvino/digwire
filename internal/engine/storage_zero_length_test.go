package engine

import (
	"bytes"
	"crypto/sha1"
	"testing"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

// Regression test: a chunk spanning a zero-length file (e.g. an empty .nfo between the
// video and a trailing .txt) must be writable and readable. With the mmap file IO backend
// this failed with "invalid argument" (mmap of a 0-byte file), which silently disabled
// downloading and left the torrent stuck just short of 100%.
func TestFileStorageWriteAcrossZeroLengthFile(t *testing.T) {
	data := bytes.Repeat([]byte("digwire!"), 8) // 64 bytes
	info := &metainfo.Info{
		Name:        "zero-len",
		PieceLength: int64(len(data)),
		Files: []metainfo.FileInfo{
			{Path: []string{"movie.mkv"}, Length: 40},
			{Path: []string{"empty.nfo"}, Length: 0},
			{Path: []string{"readme.txt"}, Length: 24},
		},
	}
	sum := sha1.Sum(data)
	info.Pieces = sum[:]

	dir := t.TempDir()
	client := storage.NewFileOpts(newFileClientOpts(dir, storage.NewMapPieceCompletion()))
	defer client.Close()

	var ih metainfo.Hash
	tor, err := client.OpenTorrent(t.Context(), info, ih)
	if err != nil {
		t.Fatal(err)
	}
	defer tor.Close()

	piece := tor.Piece(info.Piece(0))
	if _, err := piece.WriteAt(data, 0); err != nil {
		t.Fatalf("writing chunk across zero-length file: %v", err)
	}
	got := make([]byte, len(data))
	if _, err := piece.ReadAt(got, 0); err != nil {
		t.Fatalf("reading chunk across zero-length file: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("read back %q, want %q", got, data)
	}
}
