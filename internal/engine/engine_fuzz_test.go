package engine

import (
	"strings"
	"testing"
)

func FuzzExtractInfoHash(f *testing.F) {
	seeds := []string{
		"magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Ubuntu",
		"0123456789abcdef0123456789abcdef01234567",
		"magnet:?xt=urn:btih:ABCD456789abcdef0123456789abcdef01234567",
		"magnet:?xt=urn:btmh:12200123456789abcdef0123456789abcdef01234567",
		"http://example.com/file.iso",
		"",
		"   ",
		"urn:btih:short",
		"urn:btih:0123456789abcdef0123456789abcdef01234567&extra=params",
		strings.Repeat("a", 1000),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		h := extractInfoHash(input)
		if h != "" {
			if len(h) != 40 {
				t.Errorf("extracted hash must be 40 chars hex, got %q (len %d)", h, len(h))
			}
		}
	})
}

func FuzzSuperchargeMagnet(f *testing.F) {
	seeds := []string{
		"magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=Test",
		"0123456789abcdef0123456789abcdef01234567",
		"magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&tr=udp%3A%2F%2Ftracker.opentrackr.org%3A1337%2Fannounce",
		"",
		"invalid",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		out := SuperchargeMagnet(input)
		if input != "" && strings.HasPrefix(input, "magnet:?") {
			if !strings.HasPrefix(out, "magnet:?") {
				t.Errorf("expected magnet prefix in output, got %q", out)
			}
		}
	})
}

func FuzzSanitizeWebSeeds(f *testing.F) {
	seeds := []string{
		"https://releases.ubuntu.com/24.04/ubuntu-24.04-desktop-amd64.iso",
		"http://example.com/folder/file.zip",
		"ftp://invalid.com/file",
		"https://example.com/path/",
		"",
		"   ",
	}
	for _, s := range seeds {
		f.Add(s, false)
		f.Add(s, true)
	}

	f.Fuzz(func(t *testing.T, urlStr string, isDir bool) {
		res := SanitizeWebSeeds([]string{urlStr}, isDir)
		for _, u := range res {
			if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
				t.Errorf("sanitized webseed must start with http:// or https://, got %q", u)
			}
		}
	})
}

func FuzzBuildSearchQueries(f *testing.F) {
	seeds := []string{
		"ubuntu-24.04.1-desktop-amd64.iso",
		"Debian 12.5.0 Bookworm Netinst.iso",
		"Arch.Linux.2026.09.01.x86_64.iso",
		"Movie.Title.2026.1080p.BluRay.x264-GROUP.mkv",
		"file",
		"",
		"   ",
		"../relative/path/to/file.tar.gz",
		"C:\\Windows\\Path\\File.exe",
		"Special_Chars-with.dots.and.numbers.2024.part1.rar",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, filename string) {
		queries := buildSearchQueries(filename)
		for _, q := range queries {
			if strings.TrimSpace(q) == "" {
				t.Errorf("buildSearchQueries generated empty query string")
			}
		}
	})
}
