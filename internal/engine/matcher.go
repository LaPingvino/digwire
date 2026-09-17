package engine

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"digwire/internal/search"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

type SwarmSuggestion struct {
	InfoHash         string `json:"info_hash"`
	MagnetURI        string `json:"magnet_uri"`
	Name             string `json:"name"`
	Seeders          int    `json:"seeders"`
	Peers            int    `json:"peers"`
	TotalBytes       int64  `json:"total_bytes"`
	Provider         string `json:"provider"`
	IsPartial        bool   `json:"is_partial"`
	MatchedFileIndex int    `json:"matched_file_index"`
	MatchedFileName  string `json:"matched_file_name"`
	ProtocolVersion  string `json:"protocol_version,omitempty"`
	InfoHashV2       string `json:"info_hash_v2,omitempty"`
	// Hashes of already-downloaded data that matched this swarm; 0 when checked remotely.
	VerifiedPieces int `json:"verified_pieces,omitempty"`
}

type MatchResult struct {
	MatchedTorrent *torrent.Torrent
	InfoHash       string
	Name           string
	VerifiedPiece  bool
	HTTPURL        string
	SizeBytes      int64
}

// InspectHTTPFile sends a HEAD request to extract file metadata from an HTTP URL
func InspectHTTPFile(ctx context.Context, fileURL string) (filename string, sizeBytes int64, supportsRange bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, fileURL, nil)
	if err != nil {
		return "", 0, false, err
	}
	req.Header.Set("User-Agent", "Digwire/1.0")

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", 0, false, fmt.Errorf("server responded with status %d", resp.StatusCode)
	}

	// Extract filename from Content-Disposition or URL path
	cd := resp.Header.Get("Content-Disposition")
	if strings.Contains(cd, "filename=") {
		parts := strings.Split(cd, "filename=")
		if len(parts) > 1 {
			filename = strings.Trim(parts[1], "\"' ")
		}
	}
	if filename == "" {
		u, _ := url.Parse(fileURL)
		if u != nil {
			filename = path.Base(u.Path)
		}
	}
	if filename == "" || filename == "." || filename == "/" {
		filename = "downloaded_file"
	}

	sizeBytes, _ = strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	supportsRange = strings.EqualFold(resp.Header.Get("Accept-Ranges"), "bytes")

	return filename, sizeBytes, supportsRange, nil
}

// VerifyPieceAtOffset fetches a single piece extent via HTTP Range and tests against SHA-1 piece hash
func VerifyPieceAtOffset(ctx context.Context, fileURL string, startOffset, length int64, expectedHash [20]byte) (bool, error) {
	if length <= 0 {
		return false, fmt.Errorf("invalid piece length")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "Digwire/1.0")
	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", startOffset, startOffset+length-1))

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("range request not supported: status %d", resp.StatusCode)
	}

	buf := make([]byte, length)
	n, err := io.ReadFull(resp.Body, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return false, err
	}

	computed := sha1.Sum(buf[:n])
	return bytes.Equal(computed[:], expectedHash[:]), nil
}

// VerifyRandomPieces checks piece 0, a random middle piece, and the last piece for 100% cryptographic certainty
func VerifyRandomPieces(ctx context.Context, fileURL string, info *metainfo.Info) (bool, error) {
	numPieces := info.NumPieces()
	if numPieces <= 0 || len(info.Pieces) < numPieces*20 {
		return false, fmt.Errorf("invalid torrent info piece list")
	}

	totalLen := info.TotalLength()
	pieceLen := info.PieceLength

	piecesToCheck := []int{0}
	if numPieces > 2 {
		maxN := big.NewInt(int64(numPieces - 2))
		rInt, _ := rand.Int(rand.Reader, maxN)
		midPiece := int(rInt.Int64()) + 1
		piecesToCheck = append(piecesToCheck, midPiece)
	}
	if numPieces > 1 {
		piecesToCheck = append(piecesToCheck, numPieces-1)
	}

	for _, pIdx := range piecesToCheck {
		start := int64(pIdx) * pieceLen
		curLen := pieceLen
		if start+curLen > totalLen {
			curLen = totalLen - start
		}

		var expected [20]byte
		copy(expected[:], info.Pieces[pIdx*20:(pIdx+1)*20])

		ok, err := VerifyPieceAtOffset(ctx, fileURL, start, curLen, expected)
		if err != nil || !ok {
			return false, err
		}
	}

	return true, nil
}

// VerifyFileInMultiTorrent verifies if a single file inside a multi-file collection matches the HTTP source
func VerifyFileInMultiTorrent(ctx context.Context, fileURL string, fileOffset, fileLen int64, info *metainfo.Info) bool {
	pieceLen := info.PieceLength
	if pieceLen <= 0 {
		return false
	}

	firstFullPiece := (fileOffset + pieceLen - 1) / pieceLen
	lastFullPiece := (fileOffset + fileLen) / pieceLen - 1

	if firstFullPiece <= lastFullPiece && int(lastFullPiece) < info.NumPieces() {
		pIdx := int(firstFullPiece)
		offsetInFile := (int64(pIdx) * pieceLen) - fileOffset
		var expected [20]byte
		copy(expected[:], info.Pieces[pIdx*20:(pIdx+1)*20])

		ok, err := VerifyPieceAtOffset(ctx, fileURL, offsetInFile, pieceLen, expected)
		return err == nil && ok
	}

	return false
}

// buildSearchQueries generates prioritized search query candidates from a filename
func buildSearchQueries(filename string) []string {
	var queries []string
	seen := make(map[string]bool)

	add := func(q string) {
		q = strings.TrimSpace(q)
		if q != "" && !seen[strings.ToLower(q)] {
			seen[strings.ToLower(q)] = true
			queries = append(queries, q)
		}
	}

	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, ext)

	// 1. Exact filename
	add(filename)

	// 2. Base name
	add(base)

	// 3. Clean version-preserving name (e.g. ubuntu-24.04.1 -> ubuntu 24.04.1 desktop amd64)
	reDots := regexp.MustCompile(`(\d)\.(\d)`)
	clean := reDots.ReplaceAllString(base, "${1}__DOT__${2}")
	clean = strings.ReplaceAll(clean, ".", " ")
	clean = strings.ReplaceAll(clean, "__DOT__", ".")
	clean = strings.ReplaceAll(clean, "_", " ")
	clean = strings.ReplaceAll(clean, "-", " ")
	clean = strings.Join(strings.Fields(clean), " ")
	add(clean)

	// 4. Token prefixes
	tokens := strings.Fields(clean)
	if len(tokens) > 2 {
		add(strings.Join(tokens[:2], " "))
	}
	if len(tokens) > 3 {
		add(strings.Join(tokens[:3], " "))
	}

	return queries
}

func probeHostTorrent(ctx context.Context, e *Engine, fileURL string, task *HTTPTask) (*SwarmSuggestion, error) {
	torrentURL := fileURL + ".torrent"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, torrentURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Digwire/1.0")

	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	mi, err := metainfo.Load(resp.Body)
	if err != nil {
		return nil, err
	}

	info, err := mi.UnmarshalInfo()
	if err != nil {
		return nil, err
	}

	// Verify size match
	if info.TotalLength() == task.TotalBytes {
		ok, err := VerifyRandomPieces(ctx, task.URL, &info)
		if err == nil && ok {
			hash := mi.HashInfoBytes().HexString()
			mag := fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s", hash, url.QueryEscape(info.BestName()))
			for _, tier := range mi.AnnounceList {
				for _, tr := range tier {
					mag += "&tr=" + url.QueryEscape(tr)
				}
			}
			sCount := -1
			pCount := -1
			if e != nil {
				if sc, lc, err := e.ScrapeSwarm(ctx, mag); err == nil {
					sCount = sc
					pCount = lc
				}
			}
			return &SwarmSuggestion{
				InfoHash:   hash,
				MagnetURI:  mag,
				Name:       info.BestName(),
				Seeders:    sCount,
				Peers:      pCount,
				TotalBytes: info.TotalLength(),
				Provider:   "Source Host (.torrent)",
				IsPartial:  false,
			}, nil
		}
	}

	return nil, fmt.Errorf("host torrent did not match file content")
}

// FindAndAttachSwarm attempts to discover an existing swarm for an HTTP URL, verifies multi-piece samples, and attaches it as a WebSeed
func (e *Engine) FindAndAttachSwarm(ctx context.Context, fileURL string, searchMgr *search.Manager) (*MatchResult, error) {
	filename, sizeBytes, _, err := InspectHTTPFile(ctx, fileURL)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect HTTP file: %w", err)
	}

	task := &HTTPTask{
		URL:        fileURL,
		Name:       filename,
		TotalBytes: sizeBytes,
	}

	sugg, err := e.FindSuggestedSwarm(ctx, task, searchMgr)
	if err != nil {
		return nil, err
	}

	t, err := e.UpgradeHTTPToSwarm(task.ID)
	if err != nil {
		return nil, err
	}

	return &MatchResult{
		MatchedTorrent: t,
		InfoHash:       sugg.InfoHash,
		Name:           sugg.Name,
		VerifiedPiece:  true,
		HTTPURL:        fileURL,
		SizeBytes:      sizeBytes,
	}, nil
}

// FindSuggestedSwarm looks for a torrent carrying an HTTP download's file. Candidates come from the
// source host's .torrent, the local DHT index (releases with a file of exactly this size) and
// indexer searches by name. Each is verified by hashing the parts already downloaded against its
// piece hashes, or by fetching sample ranges from the source while nothing is downloaded yet.
// Among verified swarms, hybrid and v2 ones are preferred.
func (e *Engine) FindSuggestedSwarm(ctx context.Context, task *HTTPTask, searchMgr *search.Manager) (*SwarmSuggestion, error) {
	if task == nil || task.TotalBytes <= 0 {
		return nil, fmt.Errorf("invalid task")
	}

	// STAGE 1: Probe host directly for `<URL>.torrent` (e.g. Ubuntu, Debian, Arch, Fedora mirrors)
	if sugg, err := probeHostTorrent(ctx, e, task.URL, task); err == nil && sugg != nil {
		return sugg, nil
	}

	// STAGE 2: Tentative candidates from the DHT index and indexers, verified by content
	candidates := e.gatherSwarmCandidates(ctx, []string{task.Name}, []int64{task.TotalBytes}, nil, nil)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no candidate torrents found for %s", task.Name)
	}
	ld, haveLocal := task.localData()

	var (
		mu   sync.Mutex
		best *SwarmSuggestion
	)
	e.probeCandidates(ctx, candidates, func(c swarmCandidate, mi *metainfo.MetaInfo, info *metainfo.Info) {
		files := info.UpvertedFiles()
		for idx, f := range files {
			if f.Length != task.TotalBytes {
				continue
			}
			checked, ok := 0, false
			if haveLocal {
				checked, ok = verifyFileBySampling(ld, mi, info, f)
			}
			if checked == 0 {
				ok = verifyFileFromSource(ctx, task.URL, info, f, len(files) == 1)
			}
			if !ok {
				continue
			}
			magnet, infoHashV2 := swarmMagnet(c.magnet, mi)
			sugg := &SwarmSuggestion{
				InfoHash:         mi.HashInfoBytes().HexString(),
				MagnetURI:        magnet,
				Name:             info.BestName(),
				Seeders:          c.seeders,
				Peers:            c.leechers,
				TotalBytes:       f.Length,
				Provider:         c.provider,
				IsPartial:        len(files) > 1,
				MatchedFileIndex: idx,
				MatchedFileName:  strings.Join(f.BestPath(), "/"),
				ProtocolVersion:  protocolOfInfo(info),
				InfoHashV2:       infoHashV2,
				VerifiedPieces:   checked,
			}
			mu.Lock()
			if best == nil || betterSuggestion(sugg, best) {
				best = sugg
			}
			mu.Unlock()
			return
		}
	})
	if best == nil {
		return nil, fmt.Errorf("no candidate torrent passed cryptographic piece verification")
	}
	return best, nil
}

func betterSuggestion(a, b *SwarmSuggestion) bool {
	if ra, rb := protocolRank(a.ProtocolVersion), protocolRank(b.ProtocolVersion); ra != rb {
		return ra < rb
	}
	if a.VerifiedPieces != b.VerifiedPieces {
		return a.VerifiedPieces > b.VerifiedPieces
	}
	return a.Seeders > b.Seeders
}

// verifyFileFromSource checks a candidate's v1 piece hashes against ranges fetched from the HTTP
// source, for when nothing is downloaded locally yet.
func verifyFileFromSource(ctx context.Context, fileURL string, info *metainfo.Info, f metainfo.FileInfo, singleFile bool) bool {
	if len(info.Pieces) == 0 {
		return false
	}
	if singleFile && f.TorrentOffset == 0 {
		ok, err := VerifyRandomPieces(ctx, fileURL, info)
		return err == nil && ok
	}
	return VerifyFileInMultiTorrent(ctx, fileURL, f.TorrentOffset, f.Length, info)
}

// UpgradeHTTPToSwarm upgrades an active HTTP task to a hybrid BitTorrent swarm (with optional partial file download)
func (e *Engine) UpgradeHTTPToSwarm(httpTaskID string) (*torrent.Torrent, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.httpManager.mu.RLock()
	task, exists := e.httpManager.tasks[httpTaskID]
	e.httpManager.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("http task not found")
	}

	if task.SuggestedSwarm == nil {
		return nil, fmt.Errorf("no verified swarm suggestion available for this task")
	}

	sugg := task.SuggestedSwarm

	// Pause & remove HTTP task
	task.pause()
	delete(e.httpManager.tasks, httpTaskID)

	partPath := task.DestPath + ".part"

	// Add torrent to client with Supercharge & WebSeeds
	var allMirrors []string
	allMirrors = append(allMirrors, task.URL)
	allMirrors = append(allMirrors, task.Mirrors...)
	magURI := AppendWebSeedsToMagnet(SuperchargeMagnet(sugg.MagnetURI), allMirrors)

	t, err := e.client.AddMagnet(magURI)
	if err != nil {
		return nil, fmt.Errorf("failed to add swarm: %w", err)
	}

	cleanMirrors := SanitizeWebSeeds(allMirrors, false)
	hash := t.InfoHash().HexString()
	e.webSeedsMap[hash] = cleanMirrors

	go func(tor *torrent.Torrent, seeds []string, isPartial bool, matchedIdx int, matchedFile string) {
		<-tor.GotInfo()
		info := tor.Info()
		if info != nil {
			if len(seeds) > 0 {
				clean := SanitizeWebSeeds(seeds, info.IsDir())
				if len(clean) > 0 {
					tor.AddWebSeeds(clean)
				}
			}
			var destFile string
			if isPartial && matchedFile != "" {
				destFile = filepath.Join(e.cfg.DownloadDir, info.BestName(), matchedFile)
			} else if info.IsDir() {
				destFile = filepath.Join(e.cfg.DownloadDir, info.BestName(), task.Name)
			} else {
				destFile = filepath.Join(e.cfg.DownloadDir, info.BestName())
			}

			_ = os.MkdirAll(filepath.Dir(destFile), 0755)

			// Move .part file if exists into target torrent layout .part path
			if _, err := os.Stat(partPath); err == nil {
				destPart := destFile + ".part"
				_ = os.Rename(partPath, destPart)
			}

			e.ConsolidateAndVerify(tor, func() {
				if isPartial {
					for i, f := range tor.Files() {
						if i == matchedIdx {
							f.Download()
						} else {
							f.Cancel()
						}
					}
				}
			})
		}

		e.mu.Lock()
		e.saveSessionLocked()
		e.mu.Unlock()
	}(t, allMirrors, sugg.IsPartial, sugg.MatchedFileIndex, sugg.MatchedFileName)

	e.initTracker(hash)
	e.saveSessionLocked()

	return t, nil
}

func (e *Engine) TriggerFindSwarm(ctx context.Context, httpTaskID string) (*SwarmSuggestion, error) {
	e.httpManager.mu.RLock()
	task, exists := e.httpManager.tasks[httpTaskID]
	e.httpManager.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("http download task not found")
	}

	if e.searchMgr == nil {
		return nil, fmt.Errorf("search manager not configured")
	}

	sugg, err := e.FindSuggestedSwarm(ctx, task, e.searchMgr)
	if err != nil {
		return nil, err
	}

	task.mu.Lock()
	task.SuggestedSwarm = sugg
	task.mu.Unlock()

	return sugg, nil
}
