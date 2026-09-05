package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"digwire/internal/config"
	"digwire/internal/engine"
	"digwire/internal/search"
	"digwire/internal/web"
)

func sendToRunningInstance(port int, arg string) bool {
	client := &http.Client{Timeout: 15 * time.Second}
	apiURL := fmt.Sprintf("http://127.0.0.1:%d/api/torrents/add", port)

	candPath := arg
	if strings.HasPrefix(candPath, "file://") {
		candPath = strings.TrimPrefix(candPath, "file://")
		if unescaped, err := url.PathUnescape(candPath); err == nil {
			candPath = unescaped
		}
	}
	if strings.HasPrefix(candPath, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			candPath = filepath.Join(home, strings.TrimPrefix(candPath, "~/"))
		}
	}

	// Case 1: Check if file exists on disk (.torrent)
	if info, err := os.Stat(candPath); err == nil && !info.IsDir() {
		f, err := os.Open(candPath)
		if err == nil {
			defer f.Close()
			var b bytes.Buffer
			w := multipart.NewWriter(&b)
			fw, err := w.CreateFormFile("torrent_file", filepath.Base(candPath))
			if err == nil {
				_, _ = io.Copy(fw, f)
				_ = w.Close()
				req, err := http.NewRequest(http.MethodPost, apiURL, &b)
				if err == nil {
					req.Header.Set("Content-Type", w.FormDataContentType())
					resp, err := client.Do(req)
					if err == nil && resp.StatusCode == http.StatusOK {
						resp.Body.Close()
						return true
					}
					if resp != nil {
						resp.Body.Close()
					}
				}
			}
		}
		return false
	}

	// Case 2: URL / Magnet / InfoHash
	body, _ := json.Marshal(map[string]string{"url": arg})
	req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err == nil && resp.StatusCode == http.StatusOK {
		resp.Body.Close()
		return true
	}
	if resp != nil {
		resp.Body.Close()
	}
	return false
}

func registerMimeTypes() {
	if runtime.GOOS == "linux" {
		go func() {
			_ = exec.Command("xdg-mime", "default", "digwire.desktop", "x-scheme-handler/magnet").Run()
			_ = exec.Command("xdg-mime", "default", "digwire.desktop", "application/x-bittorrent").Run()
		}()
	} else if runtime.GOOS == "windows" {
		go registerWindowsAssociations()
	}
}

func registerWindowsAssociations() {
	exePath, err := os.Executable()
	if err != nil {
		return
	}
	exeDir := filepath.Dir(exePath)
	icoPath := filepath.Join(exeDir, "digwire.ico")
	if _, err := os.Stat(icoPath); err != nil {
		icoPath = exePath
	}

	_ = exec.Command("reg", "add", `HKCU\Software\Classes\magnet`, "/ve", "/t", "REG_SZ", "/d", "URL:Magnet Protocol", "/f").Run()
	_ = exec.Command("reg", "add", `HKCU\Software\Classes\magnet`, "/v", "URL Protocol", "/t", "REG_SZ", "/d", "", "/f").Run()
	_ = exec.Command("reg", "add", `HKCU\Software\Classes\magnet\DefaultIcon`, "/ve", "/t", "REG_SZ", "/d", icoPath, "/f").Run()
	_ = exec.Command("reg", "add", `HKCU\Software\Classes\magnet\shell\open\command`, "/ve", "/t", "REG_SZ", "/d", fmt.Sprintf(`"%s" "%%1"`, exePath), "/f").Run()

	_ = exec.Command("reg", "add", `HKCU\Software\Classes\.torrent`, "/ve", "/t", "REG_SZ", "/d", "Digwire.Torrent", "/f").Run()
	_ = exec.Command("reg", "add", `HKCU\Software\Classes\Digwire.Torrent`, "/ve", "/t", "REG_SZ", "/d", "BitTorrent Seed File", "/f").Run()
	_ = exec.Command("reg", "add", `HKCU\Software\Classes\Digwire.Torrent\DefaultIcon`, "/ve", "/t", "REG_SZ", "/d", icoPath, "/f").Run()
	_ = exec.Command("reg", "add", `HKCU\Software\Classes\Digwire.Torrent\shell\open\command`, "/ve", "/t", "REG_SZ", "/d", fmt.Sprintf(`"%s" "%%1"`, exePath), "/f").Run()
}

var Version = "0.3.2"

func main() {
	// WebKit2GTK Linux rendering and sandboxing compatibility flags (prevents black screen with DMA-BUF compositing on Linux GPUs/Crostini)
	_ = os.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
	_ = os.Setenv("WEBKIT_DISABLE_COMPOSITING_MODE", "1")
	_ = os.Setenv("WEBKIT_FORCE_SANDBOX", "0")
	_ = os.Setenv("WEBKIT_DISABLE_SANDBOX_THIS_IS_DANGEROUS", "1")

	portFlag := flag.Int("port", 0, "Web interface port (overrides config)")
	dirFlag := flag.String("dir", "", "Download directory (overrides config)")
	headlessFlag := flag.Bool("headless", false, "Do not automatically launch web browser or app window")
	noGTKFlag := flag.Bool("no-gtk", false, "Disable WebKitGTK native window, fallback to Chrome/Chromium app mode")
	browserFlag := flag.Bool("browser", false, "Launch default web browser instead of standalone window")
	versionFlag := flag.Bool("version", false, "Print version and exit")
	vFlag := flag.Bool("v", false, "Print version and exit")
	flag.Parse()

	if *versionFlag || *vFlag {
		fmt.Printf("Digwire v%s\n", Version)
		return
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		cfg = config.DefaultConfig()
	}
	if *portFlag > 0 {
		cfg.WebPort = *portFlag
	}
	if *dirFlag != "" {
		cfg.DownloadDir = *dirFlag
	}

	var configDir string
	if cfg.GetConfigPath() != "" {
		configDir = filepath.Dir(cfg.GetConfigPath())
	} else {
		userCfg, _ := os.UserConfigDir()
		if userCfg != "" {
			configDir = filepath.Join(userCfg, "digwire")
		} else {
			configDir = "."
		}
	}
	lockPath := filepath.Join(configDir, "digwire.lock")
	appLock, lockErr := AcquireAppLock(lockPath)

	// Single-instance handling: check if Digwire is already running
	cliArgs := flag.Args()
	checkURL := fmt.Sprintf("http://127.0.0.1:%d/api/config", cfg.WebPort)

	if lockErr != nil {
		// Another instance of Digwire is already running or launching!
		log.Printf("⚡ Digwire instance is already active (lock held). Forwarding request...\n")
		probeClient := &http.Client{Timeout: 500 * time.Millisecond}
		var connected bool
		for i := 0; i < 6; i++ {
			if resp, err := probeClient.Get(checkURL); err == nil {
				if resp.StatusCode == http.StatusOK {
					resp.Body.Close()
					connected = true
					break
				}
				if resp != nil {
					resp.Body.Close()
				}
			}
			time.Sleep(300 * time.Millisecond)
		}

		if connected {
			if len(cliArgs) > 0 {
				for _, arg := range cliArgs {
					arg = strings.TrimSpace(arg)
					if arg == "" {
						continue
					}
					if sendToRunningInstance(cfg.WebPort, arg) {
						log.Printf("✓ Forwarded '%s' to running Digwire instance.\n", arg)
					}
				}
				return
			}
			if !*headlessFlag {
				cmd, isApp := launchNativeWindow(fmt.Sprintf("http://127.0.0.1:%d", cfg.WebPort), *noGTKFlag, *browserFlag)
				if cmd != nil && isApp {
					_ = cmd.Wait()
				}
			}
			return
		}
		log.Printf("Digwire is already running in another process. Exiting duplicate instance.\n")
		return
	}
	defer appLock.Release()

	log.Println("⚡ Starting Digwire BitTorrent Client...")
	registerMimeTypes()

	// Initialize Torrent Engine
	eng, err := engine.NewEngine(cfg)
	if err != nil {
		log.Fatalf("Fatal: failed to initialize torrent engine: %v\n", err)
	}

	// Queue any CLI arguments provided at initial launch
	for _, arg := range cliArgs {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		if info, err := os.Stat(arg); err == nil && !info.IsDir() {
			if f, err := os.Open(arg); err == nil {
				_, _ = eng.AddTorrentFile(f)
				f.Close()
			}
		} else {
			_, _ = eng.Add(arg)
		}
	}

	// Initialize Search Manager
	searchMgr := search.NewManager(cfg)
	searchMgr.SetLocalDHTIndexer(eng.DHTIndexer())
	eng.SetSearchManager(searchMgr)

	// Initialize Web Server
	srv := web.NewServer(cfg, eng, searchMgr)

	uiURL := fmt.Sprintf("http://127.0.0.1:%d", cfg.WebPort)
	log.Printf("✨ Digwire is running at: %s\n", uiURL)
	log.Printf("📁 Downloads folder: %s\n", cfg.DownloadDir)

	go func() {
		if err := srv.Start(); err != nil {
			log.Printf("Server stopped: %v\n", err)
		}
	}()

	if !*headlessFlag {
		go func() {
			// Poll local web server until ready before opening UI window
			checkURL := fmt.Sprintf("http://127.0.0.1:%d/api/config", cfg.WebPort)
			client := &http.Client{Timeout: 500 * time.Millisecond}
			for i := 0; i < 40; i++ {
				resp, err := client.Get(checkURL)
				if err == nil && resp.StatusCode == http.StatusOK {
					resp.Body.Close()
					break
				}
				if resp != nil {
					resp.Body.Close()
				}
				time.Sleep(50 * time.Millisecond)
			}

			log.Println("🖥️  Opening application window...")
			cmd, isApp := launchNativeWindow(uiURL, *noGTKFlag, *browserFlag)
			if cmd != nil && isApp {
				_ = cmd.Wait()
				log.Println("Window closed by user, exiting...")
				p, _ := os.FindProcess(os.Getpid())
				_ = p.Signal(os.Interrupt)
			}
		}()
	}

	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan
	log.Println("\n🛑 Shutting down Digwire...")
	_ = srv.Close()
	eng.Close()
	log.Println("Bye!")
}
