# ⚡ Digwire

> Modern, lightweight BitTorrent & Hybrid Multi-Source client written in Go with a native GNOME Libadwaita / Fragments look and feel.

---

## ✨ Features

- **🎨 Modern GNOME Libadwaita Interface**: Clean GTK-style UI matching GNOME Fragments and Transmission with responsive dark theme, rounded cards, real-time speed rates, and ETA countdowns.
- **🔍 Multi-Backend Search**: Built-in simultaneous search across **TorrentsCSV**, **Archive.org**, and **Torznab** (Jackett/Prowlarr) with configurable bias weighting and logarithmic relevance scoring.
- **🚀 Hybrid BitTorrent + WebSeed (BEP 19)**: Downloads concurrently from both HTTP/HTTPS web mirror locations and global P2P swarms with piece validation.
- **📥 Direct HTTP & Multi-Source Segment Downloader**: Paste any plain HTTP/HTTPS download link (e.g. ISOs, direct archives). Digwire partitions the transfer across worker queues, resumes interrupted `.part` files via range requests, and multiplies download speeds across multiple HTTP mirror sources.
- **⚡ Automatic Swarm Discovery & Random Piece Sampling**: When downloading direct HTTP files, Digwire searches indexers for candidate swarms and verifies identical content by cryptographically probing sample pieces across the file without downloading everything first. If verified, offers a 1-click **Upgrade to Swarm** button!
- **🧩 Partial & Selective Downloads**: Choose exactly which files to download or skip in any multi-file torrent or collection pack.
- **📤 FrostWire "Send as Torrent"**: Select any local file or folder to automatically hash pieces, generate shareable magnet links, and begin instant local seeding across the Mainline DHT.
- **📋 Metadata Inspector & 1-Click Copy**: Deep inspect infohashes, active peer IP connections, WebSeed mirrors, trackers, and file trees.
- **🔄 Session Persistence**: All active transfers, pause states, and attached WebSeeds survive application restarts cleanly.
- **🖥️ Standalone Desktop Window**: Runs seamlessly in standalone desktop app mode with clean window integration and system `.desktop` launcher.

---

## 🚀 Quick Start

### Prerequisites
- [Go](https://golang.org/) 1.22+
- Chrome / Chromium / Brave (for native desktop app window)

### Installation & Build

```bash
git clone https://github.com/LaPingvino/digwire.git
cd digwire
go build -o digwire ./cmd/digwire
```

### Running

```bash
# Launch as native desktop application
./digwire

# Or run headless web interface on a custom port
./digwire -headless -port 9091
```

---

## 🛠️ Architecture

- **Engine (`internal/engine`)**: High-performance BitTorrent client wrapped around `github.com/anacrolix/torrent` with Mainline DHT, PEX, UPnP, BEP 19 WebSeeding, and an integrated multi-source HTTP segment downloader.
- **Search (`internal/search`)**: Parallel query aggregation engine with provider scoring and configurable weights.
- **Web & UI (`internal/web`)**: Go standard library REST API with Server-Sent Events (SSE) streaming real-time metrics to an embedded, dependency-free vanilla JS/CSS Libadwaita frontend.

---

## 📜 License

MIT License
