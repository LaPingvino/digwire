package search

import "testing"

func TestClassifyMagnet(t *testing.T) {
	const v1 = "0123456789abcdef0123456789abcdef01234567"
	const v2 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	tests := []struct {
		magnet, protocol, v2 string
	}{
		{"magnet:?xt=urn:btih:" + v1 + "&dn=x", "", ""},
		{"magnet:?xt=urn:btmh:1220" + v2 + "&dn=x", "v2", v2},
		{"magnet:?xt=urn:btih:" + v1 + "&xt=urn:btmh:1220" + v2, "hybrid", v2},
		{"https://example.org/file.torrent", "", ""},
	}
	for _, tt := range tests {
		protocol, gotV2 := ClassifyMagnet(tt.magnet)
		if protocol != tt.protocol || gotV2 != tt.v2 {
			t.Errorf("ClassifyMagnet(%q) = %q, %q; want %q, %q", tt.magnet, protocol, gotV2, tt.protocol, tt.v2)
		}
	}
}
