package engine

import (
	"net/url"
	"strings"
)

// DefaultTier1Trackers contains high-availability, fast public Bittorrent trackers
var DefaultTier1Trackers = []string{
	"udp://tracker.opentrackr.org:1337/announce",
	"udp://open.stealth.si:80/announce",
	"udp://tracker.torrent.eu.org:451/announce",
	"udp://tracker.moeking.me:6969/announce",
	"udp://explodie.org:6969/announce",
	"udp://tracker.openbittorrent.com:6969/announce",
	"http://torrent.ubuntu.com:6969/announce",
	"https://tracker.tamersunion.org:443/announce",
	"udp://tracker.dump.cl:6969/announce",
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
		wsEsc := url.QueryEscape(ws)
		if !strings.Contains(magnetURI, "ws="+wsEsc) && !strings.Contains(magnetURI, "ws="+ws) {
			magnetURI += "&ws=" + wsEsc
		}
	}

	return magnetURI
}
