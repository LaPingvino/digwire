package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"digwire/internal/config"
	"digwire/internal/engine"
	"digwire/internal/search"
	"digwire/internal/web"
)



func main() {
	portFlag := flag.Int("port", 0, "Web interface port (overrides config)")
	dirFlag := flag.String("dir", "", "Download directory (overrides config)")
	headlessFlag := flag.Bool("headless", false, "Do not automatically launch web browser")
	flag.Parse()

	log.Println("⚡ Starting Digwire BitTorrent Client...")

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Printf("Warning: failed to load config, using defaults: %v\n", err)
		cfg = config.DefaultConfig()
	}

	if *portFlag > 0 {
		cfg.WebPort = *portFlag
	}
	if *dirFlag != "" {
		cfg.DownloadDir = *dirFlag
	}

	// Initialize Torrent Engine
	eng, err := engine.NewEngine(cfg)
	if err != nil {
		log.Fatalf("Fatal: failed to initialize torrent engine: %v\n", err)
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

	if !*headlessFlag {
		go func() {
			time.Sleep(300 * time.Millisecond)
			log.Println("🖥️  Opening native application window...")
			cmd := launchNativeWindow(uiURL)
			if cmd != nil {
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

	go func() {
		if err := srv.Start(); err != nil {
			log.Printf("Server stopped: %v\n", err)
		}
	}()

	<-sigChan
	log.Println("\n🛑 Shutting down Digwire...")
	_ = srv.Close()
	eng.Close()
	log.Println("Bye!")
}
