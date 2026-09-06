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
	"github.com/bh90210/soul/server"
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
	peerSemMu   sync.Mutex
	peerSems    map[string]chan struct{}

	peerStatusMu sync.RWMutex
	peerStatuses map[string]PeerStatusInfo
}

type PeerStatusInfo struct {
	Status      server.UserStatus
	CantConnect bool
	UpdatedAt   time.Time
}

var ErrPeerOffline = errors.New("peer is offline or unreachable")

func IsErrPeerOffline(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrPeerOffline) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "offline") || strings.Contains(msg, "unreachable") || strings.Contains(msg, "no peer")
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
			serverAddr:   "server.slsknet.org",
			serverPort:   2242,
			ownPort:      22345,
			username:     "dw_" + randomHex(4),
			password:     randomHex(8),
			stageDir:     stageDir,
			peerSems:     make(map[string]chan struct{}),
			peerStatuses: make(map[string]PeerStatusInfo),
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
		SharedFolders:   8,
		SharedFiles:     250,
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

	go s.listenServerRelays(connCtx, c)

	return state, nil
}

func (s *SoulseekClient) listenServerRelays(ctx context.Context, c *client.Client) {
	statusListener := c.Relays.GetUserStatus.Listener(100)
	defer statusListener.Close()

	cantConnectListener := c.Relays.CantConnectToPeer.Listener(100)
	defer cantConnectListener.Close()

	for {
		select {
		case <-ctx.Done():
			return
		case st, ok := <-statusListener.Ch():
			if !ok || st == nil {
				continue
			}
			userKey := strings.ToLower(strings.TrimSpace(st.Username))
			if userKey == "" {
				continue
			}
			s.peerStatusMu.Lock()
			s.peerStatuses[userKey] = PeerStatusInfo{
				Status:      st.Status,
				CantConnect: st.Status == server.StatusOffline,
				UpdatedAt:   time.Now(),
			}
			s.peerStatusMu.Unlock()
		case cc, ok := <-cantConnectListener.Ch():
			if !ok || cc == nil {
				continue
			}
			userKey := strings.ToLower(strings.TrimSpace(cc.Username))
			if userKey == "" {
				continue
			}
			s.peerStatusMu.Lock()
			s.peerStatuses[userKey] = PeerStatusInfo{
				Status:      server.StatusOffline,
				CantConnect: true,
				UpdatedAt:   time.Now(),
			}
			s.peerStatusMu.Unlock()
		}
	}
}

func (s *SoulseekClient) IsPeerOffline(username string) bool {
	key := strings.ToLower(strings.TrimSpace(username))
	if key == "" {
		return false
	}
	s.peerStatusMu.RLock()
	info, ok := s.peerStatuses[key]
	s.peerStatusMu.RUnlock()
	if ok {
		if info.CantConnect || info.Status == server.StatusOffline {
			return true
		}
	}
	return false
}

func (s *SoulseekClient) MarkPeerOffline(username string) {
	key := strings.ToLower(strings.TrimSpace(username))
	if key == "" {
		return
	}
	s.peerStatusMu.Lock()
	s.peerStatuses[key] = PeerStatusInfo{
		Status:      server.StatusOffline,
		CantConnect: true,
		UpdatedAt:   time.Now(),
	}
	s.peerStatusMu.Unlock()
}

func (s *SoulseekClient) QueryUserStatus(ctx context.Context, username string) (server.UserStatus, error) {
	key := strings.ToLower(strings.TrimSpace(username))
	if key == "" {
		return server.StatusOffline, fmt.Errorf("empty username")
	}

	s.peerStatusMu.RLock()
	info, ok := s.peerStatuses[key]
	s.peerStatusMu.RUnlock()
	if ok && time.Since(info.UpdatedAt) < 15*time.Second {
		return info.Status, nil
	}

	_, err := s.ensureConnected(ctx)
	if err != nil {
		return server.StatusOffline, err
	}

	if s.client != nil && s.client.Writer != nil {
		req := server.GetUserStatus{}
		if b, sErr := req.Serialize(username); sErr == nil {
			select {
			case s.client.Writer <- b:
			default:
			}
		}
	}

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return server.StatusOffline, ctx.Err()
		case <-time.After(100 * time.Millisecond):
			s.peerStatusMu.RLock()
			info, ok = s.peerStatuses[key]
			s.peerStatusMu.RUnlock()
			if ok && time.Since(info.UpdatedAt) < 5*time.Second {
				return info.Status, nil
			}
		}
	}

	s.peerStatusMu.RLock()
	info, ok = s.peerStatuses[key]
	s.peerStatusMu.RUnlock()
	if ok {
		return info.Status, nil
	}
	return server.StatusOffline, fmt.Errorf("status query timed out for %s", username)
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

				peerStatus := "online"
				if s.IsPeerOffline(peerUser) {
					peerStatus = "offline"
				}
				res := parseSoulseekFile(peerUser, f, r.FreeSlot, r.Queue, r.AverageSpeed, peerStatus)
				results = append(results, res)

				if len(results) >= 120 {
					return results, nil
				}
			}
		}
	}
}

var tokenRegex = regexp.MustCompile(`^(?:[a-zA-Z]:|@@[^\/\\]+)[\/\\]+`)

func parseSoulseekFile(username string, f peer.File, freeSlot bool, queue int, avgSpeed int, peerStatus string) Result {
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

	filePath := strings.Join(cleanParts, "/")
	if filePath == "" {
		filePath = fileName
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
		Path:         filePath,
		User:         username,
		PeerStatus:   peerStatus,
	}
}

func (s *SoulseekClient) getPeerSem(username string) chan struct{} {
	s.peerSemMu.Lock()
	defer s.peerSemMu.Unlock()
	if s.peerSems == nil {
		s.peerSems = make(map[string]chan struct{})
	}
	key := strings.ToLower(strings.TrimSpace(username))
	sem, ok := s.peerSems[key]
	if !ok {
		sem = make(chan struct{}, 1)
		s.peerSems[key] = sem
	}
	return sem
}

// DownloadFile transfers a file from a Soulseek peer directly into destPath
func (s *SoulseekClient) DownloadFile(ctx context.Context, username, remoteFile string, size int64, destPath string, onProgress func(completed, total int64, statusText string)) error {
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

	slashRemote := strings.ReplaceAll(remoteFile, "\\", "/")
	candidates := []string{
		path.Join(s.stageDir, remoteFile),
		path.Join(s.stageDir, slashRemote),
		filepath.Join(s.stageDir, filepath.Base(slashRemote)),
		filepath.Join(s.stageDir, filepath.Base(remoteFile)),
	}

	// Pre-create any subdirectories in stageDir that state.Download might try to write to
	fullStagePath := path.Join(s.stageDir, remoteFile)
	_ = os.MkdirAll(filepath.Dir(fullStagePath), 0755)
	_ = os.MkdirAll(filepath.Dir(path.Join(s.stageDir, slashRemote)), 0755)
	_ = os.MkdirAll(filepath.Dir(filepath.Join(s.stageDir, slashRemote)), 0755)

	// Clean up any stale files from previous attempts
	for _, c := range candidates {
		_ = os.Remove(c)
	}

	// If peer is already known to be offline, fail fast without wasting time
	if s.IsPeerOffline(username) {
		return fmt.Errorf("soulseek peer %s is currently offline or unreachable: %w", username, ErrPeerOffline)
	}

	// Serialize transfers per peer: Soulseek peers upload to a user sequentially (one slot at a time)
	peerSem := s.getPeerSem(username)
	if onProgress != nil {
		onProgress(0, size, "Queued (waiting for peer slot)...")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case peerSem <- struct{}{}:
		defer func() {
			// Cooldown to allow the peer connection and remote client to transition slots cleanly
			time.Sleep(1500 * time.Millisecond)
			<-peerSem
		}()
	}

	if s.IsPeerOffline(username) {
		return fmt.Errorf("soulseek peer %s is currently offline or unreachable: %w", username, ErrPeerOffline)
	}

	if onProgress != nil {
		onProgress(0, size, "Connecting to peer...")
	}

	var statusChan chan string
	var errChan chan error
	var dlCancel context.CancelFunc

	for peerAttempt := 1; peerAttempt <= 6; peerAttempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if s.IsPeerOffline(username) {
			return fmt.Errorf("soulseek peer %s is currently offline or unreachable: %w", username, ErrPeerOffline)
		}

		token := soul.NewToken()
		var dlCtx context.Context
		dlCtx, dlCancel = context.WithTimeout(ctx, 30*time.Minute)

		statusChan, errChan = state.Download(dlCtx, client.Download{
			Username: username,
			Token:    token,
			File: &peer.File{
				Name: remoteFile,
				Size: uint64(size),
			},
		})

		// Check if immediate error (e.g. "no peer") was returned
		var immediateErr error
		select {
		case immediateErr = <-errChan:
			dlCancel()
			if immediateErr != nil && strings.Contains(strings.ToLower(immediateErr.Error()), "no peer") {
				if peerAttempt == 6 {
					s.MarkPeerOffline(username)
					return fmt.Errorf("soulseek peer %s is currently offline or unreachable: %w", username, ErrPeerOffline)
				}
				if onProgress != nil {
					onProgress(0, size, fmt.Sprintf("Connecting to peer %s (%d/6)...", username, peerAttempt))
				}
				// Request connection from server and trigger user search
				cReq := server.ConnectToPeer{}
				if cMsg, sErr := cReq.Serialize(soul.NewToken(), username, peer.ConnectionType); sErr == nil && s.client != nil && s.client.Writer != nil {
					select {
					case s.client.Writer <- cMsg:
					default:
					}
				}
				// Query user status from server as well
				sReq := server.GetUserStatus{}
				if sMsg, sErr := sReq.Serialize(username); sErr == nil && s.client != nil && s.client.Writer != nil {
					select {
					case s.client.Writer <- sMsg:
					default:
					}
				}
				go func() {
					sCtx, sCancel := context.WithTimeout(context.Background(), 3*time.Second)
					defer sCancel()
					_, _ = state.Search(sCtx, "user:"+username, soul.NewToken())
				}()

				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(2 * time.Second):
					if s.IsPeerOffline(username) {
						return fmt.Errorf("soulseek peer %s is currently offline or unreachable: %w", username, ErrPeerOffline)
					}
					continue
				}
			}
			if immediateErr != nil && !errors.Is(immediateErr, peer.ErrComplete) {
				return fmt.Errorf("soulseek peer %s transfer error: %w", username, immediateErr)
			}
		case <-time.After(200 * time.Millisecond):
			// Peer connection accepted and request sent!
		}
		break
	}
	defer dlCancel()

	var lastPct int64 = -1
	go func() {
		for st := range statusChan {
			if onProgress == nil {
				continue
			}
			stLower := strings.ToLower(st)
			if strings.HasPrefix(stLower, "copied ") {
				var pct int64
				if _, sErr := fmt.Sscanf(st, "copied %d%%", &pct); sErr == nil {
					if pct != lastPct {
						lastPct = pct
						curBytes := int64(0)
						if size > 0 {
							curBytes = (size * pct) / 100
						}
						onProgress(curBytes, size, fmt.Sprintf("%d%%", pct))
					}
				}
			} else if strings.Contains(stLower, "queued at position") {
				onProgress(0, size, fmt.Sprintf("In peer queue (pos %s)...", strings.TrimSpace(strings.TrimPrefix(stLower, "queued at position"))))
			} else if strings.Contains(stLower, "queue") {
				onProgress(0, size, "Queued by peer...")
			} else if strings.Contains(stLower, "connection") {
				onProgress(0, size, "Connecting...")
			} else if strings.Contains(stLower, "file created") {
				onProgress(0, size, "Transferring...")
			}
		}
	}()

	err = <-errChan
	if err != nil && !errors.Is(err, peer.ErrComplete) {
		errLower := strings.ToLower(err.Error())
		if strings.Contains(errLower, "no peer") || strings.Contains(errLower, "connection refused") || strings.Contains(errLower, "timeout") {
			s.MarkPeerOffline(username)
			return fmt.Errorf("soulseek peer %s is currently offline or unreachable: %w", username, ErrPeerOffline)
		}
		return fmt.Errorf("soulseek peer %s transfer error: %w", username, err)
	}

	// Locate downloaded file in stageDir
	var stageCandidate string
	for _, c := range candidates {
		if fi, sErr := os.Stat(c); sErr == nil && !fi.IsDir() && fi.Size() > 0 {
			stageCandidate = c
			break
		}
	}

	if stageCandidate == "" {
		// Walk stageDir looking for the matching filename
		targetBase := strings.ToLower(filepath.Base(slashRemote))
		_ = filepath.Walk(s.stageDir, func(p string, fi os.FileInfo, wErr error) error {
			if wErr == nil && !fi.IsDir() && strings.ToLower(fi.Name()) == targetBase && fi.Size() > 0 {
				stageCandidate = p
				return filepath.SkipAll
			}
			return nil
		})
	}

	if stageCandidate == "" {
		return fmt.Errorf("download completed but staged file not found for %s in %s", remoteFile, s.stageDir)
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
		finalSize := size
		if fi, sErr := os.Stat(destPath); sErr == nil {
			finalSize = fi.Size()
		}
		onProgress(finalSize, finalSize, "Completed")
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
func DownloadSoulseekFile(ctx context.Context, slskURI string, destPath string, onProgress func(completed, total int64, statusText string)) error {
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
