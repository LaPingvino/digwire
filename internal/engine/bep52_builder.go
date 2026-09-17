package engine

import (
	"crypto/sha1"
	"crypto/sha256"
	"fmt"
	"io"
	"math/bits"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/merkle"
	"github.com/anacrolix/torrent/metainfo"
)

type bep52FileRecord struct {
	relPath  string
	pathList []string
	size     int64
	absPath  string
}

// choosePowerOfTwoPieceLength selects a power-of-two piece length suitable for BEP 52.
func choosePowerOfTwoPieceLength(totalSize int64) int64 {
	base := metainfo.ChoosePieceLength(totalSize)
	if base < merkle.BlockSize {
		base = merkle.BlockSize
	}
	// Round up to power of two
	if base&(base-1) != 0 {
		base = 1 << bits.Len64(uint64(base-1))
	}
	return base
}

// BuildBEP52MetaInfo constructs a fully BEP 52 compliant BitTorrent v2 or Hybrid (v1+v2) MetaInfo.
func BuildBEP52MetaInfo(sourcePath string, isHybrid bool, comment string, trackers [][]string) (*metainfo.MetaInfo, error) {
	stat, err := os.Stat(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat source path %q: %w", sourcePath, err)
	}

	var records []bep52FileRecord
	var totalSize int64
	var torrentName string

	if stat.IsDir() {
		torrentName = stat.Name()
		err = filepath.Walk(sourcePath, func(path string, fi os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if fi.IsDir() {
				return nil
			}
			rel, relErr := filepath.Rel(sourcePath, path)
			if relErr != nil {
				return relErr
			}
			parts := strings.Split(rel, string(filepath.Separator))
			records = append(records, bep52FileRecord{
				relPath:  filepath.ToSlash(rel),
				pathList: parts,
				size:     fi.Size(),
				absPath:  path,
			})
			totalSize += fi.Size()
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("failed scanning directory: %w", err)
		}
	} else {
		torrentName = stat.Name()
		records = append(records, bep52FileRecord{
			relPath:  stat.Name(),
			pathList: []string{stat.Name()},
			size:     stat.Size(),
			absPath:  sourcePath,
		})
		totalSize = stat.Size()
	}

	// Sort deterministically
	sort.Slice(records, func(i, j int) bool {
		return records[i].relPath < records[j].relPath
	})

	pieceLen := choosePowerOfTwoPieceLength(totalSize)
	blocksPerPiece := int(pieceLen / merkle.BlockSize)

	rootFileTree := metainfo.FileTree{Dir: make(map[string]metainfo.FileTree)}
	pieceLayers := make(map[string]string)

	for _, rec := range records {
		var piecesRoot [32]byte
		var layerHashes [][32]byte

		if rec.size == 0 {
			// Zero-length file
			piecesRoot = [32]byte{}
		} else {
			f, openErr := os.Open(rec.absPath)
			if openErr != nil {
				return nil, fmt.Errorf("failed to open file %s: %w", rec.absPath, openErr)
			}

			chunkBufSize := 1024 * 1024
			buf := make([]byte, chunkBufSize)
			var currentPieceBlocks [][32]byte

			for {
				n, rErr := f.Read(buf)
				if n > 0 {
					for offset := 0; offset < n; offset += merkle.BlockSize {
						end := offset + merkle.BlockSize
						if end > n {
							end = n
						}
						blockHash := sha256.Sum256(buf[offset:end])
						currentPieceBlocks = append(currentPieceBlocks, blockHash)
						if len(currentPieceBlocks) == blocksPerPiece {
							pRoot := merkle.RootWithPadHash(currentPieceBlocks, [32]byte{})
							layerHashes = append(layerHashes, pRoot)
							currentPieceBlocks = nil
						}
					}
				}
				if rErr != nil {
					if rErr == io.EOF {
						break
					}
					f.Close()
					return nil, fmt.Errorf("error reading %s: %w", rec.absPath, rErr)
				}
			}
			if len(currentPieceBlocks) > 0 {
				paddedBlocks := make([][32]byte, blocksPerPiece)
				copy(paddedBlocks, currentPieceBlocks)
				pRoot := merkle.RootWithPadHash(paddedBlocks, [32]byte{})
				layerHashes = append(layerHashes, pRoot)
				currentPieceBlocks = nil
			}
			f.Close()

			if rec.size <= pieceLen {
				if len(layerHashes) > 0 {
					piecesRoot = layerHashes[0]
				} else {
					piecesRoot = [32]byte{}
				}
			} else {
				padHash := metainfo.HashForPiecePad(pieceLen)
				piecesRoot = merkle.RootWithPadHash(layerHashes, padHash)
				var concat []byte
				for _, h := range layerHashes {
					concat = append(concat, h[:]...)
				}
				pieceLayers[string(piecesRoot[:])] = string(concat)
			}
		}

		// Insert into FileTree
		insertFileTree(&rootFileTree, rec.pathList, metainfo.FileTreeFile{
			Length:     rec.size,
			PiecesRoot: string(piecesRoot[:]),
		})
	}

	info := metainfo.Info{
		Name:        torrentName,
		PieceLength: pieceLen,
		MetaVersion: 2,
		FileTree:    rootFileTree,
	}

	if isHybrid {
		// Build v1 files and pieces with BEP 47 padding files
		var v1Files []metainfo.FileInfo
		var sha1Buf []byte
		hasher := sha1.New()

		for i, rec := range records {
			v1Files = append(v1Files, metainfo.FileInfo{
				Length: rec.size,
				Path:   rec.pathList,
			})

			f, openErr := os.Open(rec.absPath)
			if openErr != nil {
				return nil, fmt.Errorf("failed to open %s for v1 hashing: %w", rec.absPath, openErr)
			}
			chunk := make([]byte, 1024*1024)
			for {
				n, rErr := f.Read(chunk)
				if n > 0 {
					hasher.Write(chunk[:n])
					sha1Buf = append(sha1Buf, chunk[:n]...)
					for int64(len(sha1Buf)) >= pieceLen {
						pHash := sha1.Sum(sha1Buf[:pieceLen])
						info.Pieces = append(info.Pieces, pHash[:]...)
						sha1Buf = sha1Buf[pieceLen:]
					}
				}
				if rErr != nil {
					break
				}
			}
			f.Close()

			// Pad to the next piece boundary; the last file needs none (BEP 47).
			rem := rec.size % pieceLen
			if rem != 0 && i < len(records)-1 {
				padLen := pieceLen - rem
				padPath := []string{".pad", fmt.Sprintf("%d", padLen)}
				v1Files = append(v1Files, metainfo.FileInfo{
					Length: padLen,
					Path:   padPath,
					ExtendedFileAttrs: metainfo.ExtendedFileAttrs{
						Attr: "p",
					},
				})
				// Write padding zero bytes
				zeroPad := make([]byte, padLen)
				hasher.Write(zeroPad)
				sha1Buf = append(sha1Buf, zeroPad...)
				for int64(len(sha1Buf)) >= pieceLen {
					pHash := sha1.Sum(sha1Buf[:pieceLen])
					info.Pieces = append(info.Pieces, pHash[:]...)
					sha1Buf = sha1Buf[pieceLen:]
				}
			}
		}

		if len(sha1Buf) > 0 {
			pHash := sha1.Sum(sha1Buf)
			info.Pieces = append(info.Pieces, pHash[:]...)
		}

		if stat.IsDir() {
			info.Files = v1Files
		} else {
			// A single file stays a single-file torrent for v1 clients too.
			info.Length = stat.Size()
		}
	}

	infoBytes, err := bencode.Marshal(&info)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal info dict: %w", err)
	}

	if len(trackers) == 0 {
		trackers = [][]string{
			{"udp://tracker.opentrackr.org:1337/announce"},
			{"udp://open.stealth.si:80/announce"},
			{"udp://tracker.torrent.eu.org:451/announce"},
			{"udp://explodie.org:6969/announce"},
		}
	}

	mi := &metainfo.MetaInfo{
		InfoBytes:    infoBytes,
		PieceLayers:  pieceLayers,
		AnnounceList: trackers,
		Comment:      comment,
		CreatedBy:    "Digwire P2P (BEP 52)",
		CreationDate: time.Now().Unix(),
	}
	mi.SetDefaults()

	// Validate piece layers
	if err := metainfo.ValidatePieceLayers(mi.PieceLayers, &info.FileTree, info.PieceLength); err != nil {
		return nil, fmt.Errorf("BEP 52 piece layers validation failed: %w", err)
	}

	return mi, nil
}

func insertFileTree(ft *metainfo.FileTree, path []string, leaf metainfo.FileTreeFile) {
	if len(path) == 0 {
		return
	}
	if ft.Dir == nil {
		ft.Dir = make(map[string]metainfo.FileTree)
	}
	if len(path) == 1 {
		ft.Dir[path[0]] = metainfo.FileTree{File: leaf}
		return
	}
	sub, exists := ft.Dir[path[0]]
	if !exists {
		sub = metainfo.FileTree{Dir: make(map[string]metainfo.FileTree)}
	}
	insertFileTree(&sub, path[1:], leaf)
	ft.Dir[path[0]] = sub
}
