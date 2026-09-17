package engine

import (
	"bytes"
	"crypto/sha1"
	"io"
	"os"
	"path/filepath"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/merkle"
	"github.com/anacrolix/torrent/metainfo"
)

// localData is one file's content as far as it is downloaded and trustworthy on disk.
type localData struct {
	path   string
	length int64
	// have reports whether the file-relative range [off, off+n) is downloaded.
	have func(off, n int64) bool
}

const maxSampledPieces = 5

// verifyFileBySampling hashes already-downloaded parts of a local file against the hashes a
// candidate torrent expects for one of its files of the same length, so a match is proven with
// the data at hand whatever the candidate's piece size. It uses v1 piece hashes (v1 and hybrid
// torrents), v2 piece layers when known, or the v2 file root once the whole file is local.
// It returns the number of hashes checked; ok means at least one was checked and all matched.
func verifyFileBySampling(ld localData, mi *metainfo.MetaInfo, info *metainfo.Info, file metainfo.FileInfo) (checked int, ok bool) {
	if file.Length != ld.length || info.PieceLength <= 0 {
		return 0, false
	}
	f, err := os.Open(ld.path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	pl := info.PieceLength

	type sample struct {
		fileOff  int64
		expected []byte
		v2       bool
	}
	var available []sample
	switch {
	case len(info.Pieces) > 0:
		// Only pieces wholly inside the file: a hybrid's tail piece also hashes BEP 47 padding.
		for q := (file.TorrentOffset + pl - 1) / pl; (q+1)*pl <= file.TorrentOffset+file.Length; q++ {
			start := q*pl - file.TorrentOffset
			if (q+1)*20 <= int64(len(info.Pieces)) && ld.have(start, pl) {
				available = append(available, sample{start, info.Pieces[q*20 : (q+1)*20], false})
			}
		}
	case file.PiecesRoot.Ok && mi != nil:
		layer := mi.PieceLayers[string(file.PiecesRoot.Value[:])]
		for q := int64(0); (q+1)*pl <= file.Length && (q+1)*32 <= int64(len(layer)); q++ {
			if ld.have(q*pl, pl) {
				available = append(available, sample{q * pl, []byte(layer[q*32 : (q+1)*32]), true})
			}
		}
	}

	if len(available) == 0 {
		if file.PiecesRoot.Ok && len(info.Pieces) == 0 && ld.have(0, ld.length) {
			return 1, fileRootMatches(f, file.PiecesRoot.Value)
		}
		return 0, false
	}

	buf := make([]byte, pl)
	for _, i := range spreadIndexes(len(available), maxSampledPieces) {
		s := available[i]
		if _, err := f.ReadAt(buf, s.fileOff); err != nil {
			return checked, false
		}
		var sum []byte
		if s.v2 {
			h := merkle.NewHash()
			h.Write(buf)
			sum = h.SumMinLength(nil, int(pl))
		} else {
			h := sha1.Sum(buf)
			sum = h[:]
		}
		checked++
		if !bytes.Equal(sum, s.expected) {
			return checked, false
		}
	}
	return checked, true
}

func fileRootMatches(f *os.File, root [32]byte) bool {
	h := merkle.NewHash()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}
	return bytes.Equal(h.Sum(nil), root[:])
}

// spreadIndexes picks up to max indexes out of n, always including the first and last.
func spreadIndexes(n, max int) []int {
	if n <= max {
		out := make([]int, n)
		for i := range out {
			out[i] = i
		}
		return out
	}
	out := make([]int, 0, max)
	for k := 0; k < max; k++ {
		out = append(out, k*(n-1)/(max-1))
	}
	return out
}

// torrentFileData describes a file of a local torrent, counting only bytes of verified pieces:
// files are preallocated, so on-disk size says nothing about what is downloaded.
func torrentFileData(downloadDir string, tor *torrent.Torrent, fileIndex int) localData {
	tf := tor.Files()[fileIndex]
	pl := tor.Info().PieceLength
	return localData{
		path:   filepath.Join(downloadDir, tf.Path()),
		length: tf.Length(),
		have: func(off, n int64) bool {
			if n <= 0 || off < 0 || off+n > tf.Length() {
				return false
			}
			begin := tf.Offset() + off
			for p := begin / pl; p*pl < begin+n; p++ {
				if !tor.Piece(int(p)).State().Complete {
					return false
				}
			}
			return true
		},
	}
}
