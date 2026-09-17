package search

import (
	"net/url"
	"strings"
)

// ClassifyMagnet derives the BitTorrent protocol version a magnet link advertises.
// A btih-only magnet may still point at a hybrid torrent, so it yields "" (unknown)
// until the metadata has been resolved.
func ClassifyMagnet(magnetURI string) (protocol, infoHashV2 string) {
	if !strings.HasPrefix(magnetURI, "magnet:?") {
		return "", ""
	}
	q, err := url.ParseQuery(strings.TrimPrefix(magnetURI, "magnet:?"))
	if err != nil {
		return "", ""
	}
	hasV1 := false
	for _, xt := range q["xt"] {
		xt = strings.ToLower(xt)
		switch {
		case strings.HasPrefix(xt, "urn:btih:"):
			hasV1 = true
		case strings.HasPrefix(xt, "urn:btmh:1220"):
			infoHashV2 = strings.TrimPrefix(xt, "urn:btmh:1220")
		}
	}
	switch {
	case infoHashV2 != "" && hasV1:
		return "hybrid", infoHashV2
	case infoHashV2 != "":
		return "v2", infoHashV2
	}
	return "", ""
}
