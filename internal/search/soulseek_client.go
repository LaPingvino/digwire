package search

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bh90210/soul"
	"github.com/bh90210/soul/client"
	"github.com/bh90210/soul/peer"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	globalSoulseekClient *SoulseekClient
	soulseekOnce         sync.Once
)

type SoulseekClient struct {
	mu          sync.Mutex
	serverAddr  string
	serverPort  int
	ownPort     int
	username    string
	password    string
	stageDir    string
	client      *client.Client
	state       *client.State
	cancel      context.CancelFunc
	connected   bool
	lastLogin   time.Time
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func GetSoulseekClient() *SoulseekClient {
	soulseekOnce.Do(func() {
		stageDir := filepath.Join(os.TempDir(), "digwire_slsk_stage")
		_ = os.MkdirAll(stageDir, 0755)

		globalSoulseekClient = &SoulseekClient{
			serverAddr: "server.slsknet.org",
			serverPort: 2242,
			ownPort:    22345,
			username:   "dw_" + randomHex(4),
			password:   randomHex(8),
			stageDir:   stageDir,
		}
	})
	return globalSoulseekClient
}

func (s *SoulseekClient) Configure(serverAddr string, username, password string, ownPort int, stageDir string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if serverAddr != "" {
		host := serverAddr
		port := 2242
		if strings.Contains(serverAddr, ":") {
			parts := strings.Split(serverAddr, ":")
			host = parts[0]
			if p, err := strconv.Atoi(parts[1]); err == nil && p > 0 {
				port = p
			}
		}
		s.serverAddr = host
		s.serverPort = port
	}
	if username != "" {
		s.username = username
	}
	if password != "" {
		s.password = password
	}
	if ownPort > 0 {
		s.ownPort = ownPort
	}
	if stageDir != "" {
		s.stageDir = stageDir
		_ = os.MkdirAll(stageDir, 0755)
	}
}

func (s *SoulseekClient) ensureConnected(ctx context.Context) (*client.State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.connected && s.state != nil && time.Since(s.lastLogin) < 2*time.Hour {
		return s.state, nil
	}

	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	s.connected = false

	portToUse := s.ownPort
	if ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", portToUse)); err != nil {
		if freeLn, fErr := net.Listen("tcp", "127.0.0.1:0"); fErr == nil {
			portToUse = freeLn.Addr().(*net.TCPAddr).Port
			_ = freeLn.Close()
		}
	} else {
		_ = ln.Close()
	}

	cfg := &client.Config{
		SoulSeekAddress: s.serverAddr,
		SoulSeekPort:    s.serverPort,
		OwnPort:         portToUse,
		Username:        s.username,
		Password:        s.password,
		SharedFolders:   0,
		SharedFiles:     0,
		LogLevel:        zerolog.Disabled,
		Timeout:         25 * time.Second,
		LoginTimeout:    10 * time.Second,
		DownloadFolder:  s.stageDir,
		MaxPeers:        100,
	}

	log.Logger = log.Level(zerolog.Disabled)

	c, err := client.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create soulseek client: %w", err)
	}

	connCtx, cancel := context.WithCancel(context.Background())

	if err := c.Dial(connCtx, cancel); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to dial soulseek server %s:%d: %w", s.serverAddr, s.serverPort, err)
	}

	state := client.NewState(c)
	if err := state.Login(connCtx); err != nil {
		cancel()
		return nil, fmt.Errorf("soulseek login failed: %w", err)
	}

	s.client = c
	s.state = state
	s.cancel = cancel
	s.connected = true
	s.lastLogin = time.Now()

	return state, nil
}

// Search performs a live distributed or user search on the Soulseek network
func (s *SoulseekClient) Search(ctx context.Context, query string, timeout time.Duration) ([]Result, error) {
	state, err := s.ensureConnected(ctx)
	if err != nil {
		return nil, err
	}

	token := soul.NewToken()
	if timeout <= 0 {
		timeout = 6 * time.Second
	}
	searchCtx, searchCancel := context.WithTimeout(ctx, timeout)
	defer searchCancel()

	qTrim := strings.TrimSpace(query)
	qLower := strings.ToLower(qTrim)

	searchQuery := qTrim
	targetUser := ""
	if strings.HasPrefix(qLower, "user:") {
		targetUser = strings.TrimSpace(qTrim[5:])
		searchQuery = targetUser
	} else if strings.HasPrefix(qLower, "artist:") {
		searchQuery = strings.TrimSpace(qTrim[7:])
	} else if strings.HasPrefix(qLower, "creator:") {
		searchQuery = strings.TrimSpace(qTrim[8:])
	}

	resultsChan, sErr := state.Search(searchCtx, searchQuery, token)
	if sErr != nil {
		return nil, sErr
	}

	var results []Result
	seenFiles := make(map[string]bool)

	timer := time.After(timeout)
	for {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		case <-timer:
			return results, nil
		case r, ok := <-resultsChan:
			if !ok || r == nil {
				continue
			}
			if len(r.Results) == 0 {
				continue
			}

			peerUser := r.Username
			if targetUser != "" && !strings.EqualFold(peerUser, targetUser) && !strings.Contains(strings.ToLower(peerUser), strings.ToLower(targetUser)) {
				continue
			}
			for _, f := range r.Results {
				if f.Size == 0 {
					continue
				}

				fileKey := fmt.Sprintf("%s::%s", peerUser, f.Name)
				if seenFiles[fileKey] {
					continue
				}
				seenFiles[fileKey] = true

				res := parseSoulseekFile(peerUser, f, r.FreeSlot, r.Queue, r.AverageSpeed)
				results = append(results, res)

				if len(results) >= 120 {
					return results, nil
				}
			}
		}
	}
}

var tokenRegex = regexp.MustCompile(`^(?:[a-zA-Z]:|@@[^\/\\]+)[\/\\]+`)

func parseSoulseekFile(username string, f peer.File, freeSlot bool, queue int, avgSpeed int) Result {
	norm := strings.ReplaceAll(f.Name, "\\", "/")
	norm = tokenRegex.ReplaceAllString(norm, "")
	norm = strings.Trim(norm, "/")

	parts := strings.Split(norm, "/")
	var cleanParts []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			cleanParts = append(cleanParts, trimmed)
		}
	}

	fileName := f.Name
	if len(cleanParts) > 0 {
		fileName = cleanParts[len(cleanParts)-1]
	}
	ext := filepath.Ext(fileName)
	title := strings.TrimSuffix(fileName, ext)

	var artist, album, directory string

	if len(cleanParts) >= 3 {
		artist = cleanParts[len(cleanParts)-3]
		album = cleanParts[len(cleanParts)-2]
		directory = strings.Join(cleanParts[:len(cleanParts)-1], " / ")
	} else if len(cleanParts) == 2 {
		album = cleanParts[0]
		directory = cleanParts[0]
	}

	// Deduce artist if generic
	artistLower := strings.ToLower(artist)
	if artistLower == "music" || artistLower == "mp3" || artistLower == "flac" || artistLower == "audio" || artistLower == "album" || artistLower == "albums" || artist == "" {
		if strings.Contains(album, " - ") {
			subParts := strings.Split(album, " - ")
			artist = strings.TrimSpace(subParts[0])
		} else if strings.Contains(title, " - ") {
			subParts := strings.Split(title, " - ")
			artist = strings.TrimSpace(subParts[0])
		}
	}

	// Format tags based on extension and attributes
	formatTag := ""
	extUpper := strings.ToUpper(strings.TrimPrefix(ext, "."))
	var bitrate uint32
	isVBR := false

	for _, attr := range f.Attributes {
		if attr.Code == peer.Bitrate {
			bitrate = attr.Value
		} else if attr.Code == peer.VBR && attr.Value == 1 {
			isVBR = true
		}
	}

	if extUpper == "FLAC" {
		formatTag = " [FLAC Lossless]"
	} else if extUpper == "MP3" {
		if isVBR {
			formatTag = " [VBR MP3]"
		} else if bitrate > 0 {
			formatTag = fmt.Sprintf(" [%d kbps MP3]", bitrate)
		} else {
			formatTag = " [MP3]"
		}
	} else if extUpper != "" {
		formatTag = fmt.Sprintf(" [%s]", extUpper)
	}

	fullTitle := title + formatTag
	slskURI := fmt.Sprintf("slsk://%s?file=%s&size=%d", url.PathEscape(username), url.QueryEscape(f.Name), f.Size)

	seeders := 0
	if freeSlot {
		seeders = 1
	}

	dirPath := directory
	if dirPath == "" {
		if artist != "" && album != "" {
			dirPath = fmt.Sprintf("%s / %s", artist, album)
		} else if artist != "" {
			dirPath = artist
		} else if album != "" {
			dirPath = album
		}
	}

	return Result{
		Title:        fullTitle,
		MagnetURI:    slskURI,
		SizeBytes:    int64(f.Size),
		Seeders:      seeders,
		Leechers:     queue,
		Provider:     "Soulseek P2P",
		ProviderType: "soulseek",
		DetailsURL:   "",
		Artist:       artist,
		Album:        album,
		Directory:    dirPath,
		Path:         dirPath,
		User:         username,
	}
}

// DownloadFile transfers a file from a Soulseek peer directly into destPath
func (s *SoulseekClient) DownloadFile(ctx context.Context, username, remoteFile string, size int64, destPath string, onProgress func(completed, total int64)) error {
	state, err := s.ensureConnected(ctx)
	if err != nil {
		return err
	}

	// Ensure destination directory exists
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	_ = os.MkdirAll(s.stageDir, 0755)

	token := soul.NewToken()
	dlCtx, dlCancel := context.WithTimeout(ctx, 15*time.Minute)
	defer dlCancel()

	statusChan, errChan := state.Download(dlCtx, client.Download{
		Username: username,
		Token:    token,
		File: &peer.File{
			Name: remoteFile,
			Size: uint64(size),
		},
	})

	go func() {
		for range statusChan {
			// Track intermediate status updates if needed
		}
	}()

	err = <-errChan
	if err != nil && !errors.Is(err, peer.ErrComplete) {
		return fmt.Errorf("soulseek peer %s transfer error: %w", username, err)
	}

	// Locate downloaded file in stageDir
	// state.Download creates it at path.Join(DownloadFolder, file.File.Name)
	stageCandidate := path.Join(s.stageDir, remoteFile)
	if _, err := os.Stat(stageCandidate); os.IsNotExist(err) {
		// Also check basename
		baseCandidate := filepath.Join(s.stageDir, filepath.Base(remoteFile))
		if _, err := os.Stat(baseCandidate); err == nil {
			stageCandidate = baseCandidate
		}
	}

	if _, err := os.Stat(stageCandidate); os.IsNotExist(err) {
		return fmt.Errorf("download completed but staged file not found at %s", stageCandidate)
	}

	// Move from stage to destPath
	partDest := destPath + ".part"
	_ = os.Remove(partDest)
	_ = os.Remove(destPath)

	if err := os.Rename(stageCandidate, destPath); err != nil {
		// Fallback to copy if across filesystems
		if cpErr := copyFile(stageCandidate, destPath); cpErr != nil {
			return fmt.Errorf("failed to move staged soulseek file: %w", cpErr)
		}
		_ = os.Remove(stageCandidate)
	}

	if onProgress != nil {
		onProgress(size, size)
	}

	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// DownloadSoulseekFile is a top-level helper for downloading a slsk:// URI
func DownloadSoulseekFile(ctx context.Context, slskURI string, destPath string, onProgress func(completed, total int64)) error {
	u, err := url.Parse(slskURI)
	if err != nil {
		return fmt.Errorf("invalid soulseek URI: %w", err)
	}

	username := u.Host
	if username == "" {
		username = strings.TrimPrefix(u.Path, "/")
	}

	q := u.Query()
	remoteFile := q.Get("file")
	if remoteFile == "" {
		return errors.New("missing file parameter in soulseek URI")
	}

	var size int64
	if sVal := q.Get("size"); sVal != "" {
		size, _ = strconv.ParseInt(sVal, 10, 64)
	}

	return GetSoulseekClient().DownloadFile(ctx, username, remoteFile, size, destPath, onProgress)
}
