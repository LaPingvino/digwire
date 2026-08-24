package engine

import (
	"net"
	"net/url"
	"strings"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
)

// DefaultTier1Trackers contains high-availability, fast public Bittorrent trackers
var DefaultTier1Trackers = []string{
	"udp://tracker.opentrackr.org:1337/announce",
	"udp://open.stealth.si:80/announce",
	"udp://tracker.torrent.eu.org:451/announce",
	"udp://tracker.moeking.me:6969/announce",
	"udp://explodie.org:6969/announce",
	"udp://tracker.openbittorrent.com:6969/announce",
	"udp://tracker.openbittorrent.com:80/announce",
	"udp://opentracker.i2p.rocks:6969/announce",
	"udp://open.demonii.com:1337/announce",
	"udp://tracker.dler.org:6969/announce",
	"udp://p4p.arenabg.com:1337/announce",
	"udp://tracker.bittor.pw:1337/announce",
	"udp://tracker.altrosky.nl:2710/announce",
	"http://torrent.ubuntu.com:6969/announce",
	"https://tracker.tamersunion.org:443/announce",
	"udp://tracker.dump.cl:6969/announce",
}

// GetTier1TrackerList returns the announce list in 2D format for torrent.AddTrackers
func GetTier1TrackerList() [][]string {
	var list [][]string
	for _, tr := range DefaultTier1Trackers {
		list = append(list, []string{tr})
	}
	return list
}

// ExtractPeersFromMagnet extracts direct peer endpoints from x.pe, peer, x.p parameters in magnet URIs
func ExtractPeersFromMagnet(magnetURI string) []string {
	if !strings.HasPrefix(magnetURI, "magnet:?") {
		return nil
	}
	var peers []string
	if magObj, err := metainfo.ParseMagnetUri(magnetURI); err == nil && magObj.Params != nil {
		peers = append(peers, magObj.Params["x.pe"]...)
		peers = append(peers, magObj.Params["peer"]...)
		peers = append(peers, magObj.Params["x.p"]...)
		peers = append(peers, magObj.Params["peers"]...)
	}

	u, err := url.Parse(magnetURI)
	if err == nil {
		q := u.Query()
		for _, k := range []string{"x.pe", "peer", "x.p", "peers"} {
			peers = append(peers, q[k]...)
		}
	}

	seen := make(map[string]bool)
	var clean []string
	for _, p := range peers {
		p = strings.TrimSpace(p)
		if p != "" && !seen[p] {
			seen[p] = true
			clean = append(clean, p)
		}
	}
	return clean
}

// ConvertToPeerInfos parses IP:Port strings (IPv4/IPv6) into anacrolix/torrent PeerInfo objects
func ConvertToPeerInfos(peerStrings []string) []torrent.PeerInfo {
	var infos []torrent.PeerInfo
	for _, p := range peerStrings {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		tcpAddr, err := net.ResolveTCPAddr("tcp", p)
		if err == nil && tcpAddr != nil {
			infos = append(infos, torrent.PeerInfo{
				Addr:    tcpAddr,
				Source:  torrent.PeerSourceIncoming,
				Trusted: true,
			})
		}
	}
	return infos
}

// SuperchargeMagnet enriches a magnet URI with Tier-1 trackers if missing or sparse
func SuperchargeMagnet(magnetURI string) string {
	if !strings.HasPrefix(magnetURI, "magnet:?") {
		return magnetURI
	}

	existingTrackers := make(map[string]bool)
	parts := strings.Split(strings.TrimPrefix(magnetURI, "magnet:?"), "&")
	for _, p := range parts {
		if strings.HasPrefix(p, "tr=") {
			trVal, err := url.QueryUnescape(strings.TrimPrefix(p, "tr="))
			if err == nil {
				existingTrackers[trVal] = true
			}
		}
	}

	result := magnetURI
	for _, tr := range DefaultTier1Trackers {
		if !existingTrackers[tr] {
			result += "&tr=" + url.QueryEscape(tr)
			existingTrackers[tr] = true
		}
	}

	return result
}

// AppendWebSeedsToMagnet embeds BEP 19 WebSeed parameters (&ws=...) into a magnet link
func AppendWebSeedsToMagnet(magnetURI string, webseeds []string) string {
	if !strings.HasPrefix(magnetURI, "magnet:?") {
		return magnetURI
	}

	for _, ws := range webseeds {
		ws = strings.TrimSpace(ws)
		if ws == "" {
			continue
		}
		wsEsc := url.QueryEscape(ws)
		if !strings.Contains(magnetURI, "ws="+wsEsc) && !strings.Contains(magnetURI, "ws="+ws) {
			magnetURI += "&ws=" + wsEsc
		}
	}

	return magnetURI
}

// SanitizeWebSeeds deduplicates and validates WebSeed URLs for single-file and multi-file torrents
func SanitizeWebSeeds(urls []string, isDir bool) []string {
	seen := make(map[string]bool)
	var clean []string
	for _, u := range urls {
		u = strings.TrimSpace(u)
		if u == "" || (!strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://")) {
			continue
		}
		if isDir && !strings.HasSuffix(u, "/") {
			u += "/"
		}
		if !seen[u] {
			seen[u] = true
			clean = append(clean, u)
		}
	}
	return clean
}
