package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/anacrolix/torrent/metainfo"
)

type archiveOrgMetadataResponse struct {
	Metadata struct {
		Identifier  string `json:"identifier"`
		Title       string `json:"title"`
		Creator     any    `json:"creator"`
		Artist      string `json:"artist"`
		Album       string `json:"album"`
		Date        string `json:"date"`
		MediaType   string `json:"mediatype"`
		Description string `json:"description"`
	} `json:"metadata"`
	Files []struct {
		Name     string `json:"name"`
		Source   string `json:"source"`
		Format   string `json:"format"`
		Original string `json:"original"`
		Size     any    `json:"size"`
		MD5      string `json:"md5"`
		BTIH     string `json:"btih"`
		Title    string `json:"title"`
		Track    string `json:"track"`
		Artist   string `json:"artist"`
		Album    string `json:"album"`
		Length   string `json:"length"`
	} `json:"files"`
}

// inspectArchiveOrg retrieves files and metadata from Archive.org items
func inspectArchiveOrg(ctx context.Context, input string) (*InspectResult, error) {
	id := extractArchiveOrgIdentifier(input)
	if id == "" {
		return nil, fmt.Errorf("could not extract Archive.org identifier from: %s", input)
	}

	client := &http.Client{Timeout: 12 * time.Second}

	// 1. Try fetching .torrent metainfo directly
	torrentURL := fmt.Sprintf("https://archive.org/download/%s/%s_archive.torrent", id, id)
	if req, err := http.NewRequestWithContext(ctx, http.MethodGet, torrentURL, nil); err == nil {
		req.Header.Set("User-Agent", "Digwire/0.3.2 (Swarm Inspector)")
		if resp, err := client.Do(req); err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				bodyBytes, err := io.ReadAll(resp.Body)
				if err == nil && len(bodyBytes) > 0 {
					if mi, err := metainfo.Load(bytes.NewReader(bodyBytes)); err == nil && mi != nil {
						if info, err := mi.UnmarshalInfo(); err == nil {
							var files []TorrentFileDetail
							for idx, f := range info.UpvertedFiles() {
								files = append(files, TorrentFileDetail{
									Index:  idx,
									Path:   f.DisplayPath(&info),
									Length: f.Length,
								})
							}
							return &InspectResult{
								Name:      info.BestName(),
								InfoHash:  mi.HashInfoBytes().HexString(),
								MagnetURI: torrentURL,
								TotalSize: info.TotalLength(),
								NumFiles:  len(files),
								Seeders:   -1,
								Leechers:  -1,
								Files:     files,
							}, nil
						}
					}
				}
			}
		}
	}

	// 2. Query Archive.org Metadata JSON API
	metaURL := fmt.Sprintf("https://archive.org/metadata/%s", id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metaURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Digwire/0.3.2 (Metadata Inspector)")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Archive.org metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Archive.org metadata returned HTTP status %d", resp.StatusCode)
	}

	var data archiveOrgMetadataResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to parse Archive.org metadata: %w", err)
	}

	title := data.Metadata.Title
	if title == "" {
		title = id
	}

	creatorStr := ""
	switch c := data.Metadata.Creator.(type) {
	case string:
		creatorStr = c
	case []any:
		var parts []string
		for _, item := range c {
			if s, ok := item.(string); ok {
				parts = append(parts, s)
			}
		}
		creatorStr = strings.Join(parts, ", ")
	}
	if creatorStr == "" && data.Metadata.Artist != "" {
		creatorStr = data.Metadata.Artist
	}

	albumStr := data.Metadata.Album
	if albumStr == "" {
		albumStr = data.Metadata.Title
	}

	var dirPrefix string
	if creatorStr != "" && albumStr != "" {
		dirPrefix = fmt.Sprintf("%s / %s", creatorStr, albumStr)
	} else if creatorStr != "" {
		dirPrefix = creatorStr
	} else if albumStr != "" {
		dirPrefix = albumStr
	}

	var files []TorrentFileDetail
	var totalSize int64
	var btih string

	for _, f := range data.Files {
		if f.BTIH != "" && btih == "" {
			btih = f.BTIH
		}

		// Filter out internal metadata/spectrogram/peaks files if real media exists
		lowerName := strings.ToLower(f.Name)
		if f.Format == "Metadata" ||
			strings.HasSuffix(lowerName, "_files.xml") ||
			strings.HasSuffix(lowerName, "_meta.sqlite") ||
			strings.HasSuffix(lowerName, "_meta.xml") ||
			strings.HasSuffix(lowerName, ".afpk") ||
			strings.HasSuffix(lowerName, "_spectrogram.png") ||
			strings.HasSuffix(lowerName, "__ia_thumb.jpg") ||
			f.Name == fmt.Sprintf("%s_archive.torrent", id) {
			continue
		}

		var size int64
		switch v := f.Size.(type) {
		case float64:
			size = int64(v)
		case string:
			size, _ = strconv.ParseInt(v, 10, 64)
		case json.Number:
			size, _ = v.Int64()
		}

		filePath := f.Name
		if dirPrefix != "" && !strings.Contains(filePath, "/") && !strings.Contains(filePath, "\\") {
			filePath = fmt.Sprintf("%s/%s", dirPrefix, f.Name)
		}

		files = append(files, TorrentFileDetail{
			Index:  len(files),
			Path:   filePath,
			Length: size,
		})
		totalSize += size
	}

	// If all files were filtered out, include all raw files
	if len(files) == 0 {
		for _, f := range data.Files {
			var size int64
			switch v := f.Size.(type) {
			case float64:
				size = int64(v)
			case string:
				size, _ = strconv.ParseInt(v, 10, 64)
			}
			files = append(files, TorrentFileDetail{
				Index:  len(files),
				Path:   f.Name,
				Length: size,
			})
			totalSize += size
		}
	}

	if btih == "" {
		btih = HashURL(torrentURL)
	}

	return &InspectResult{
		Name:      title,
		InfoHash:  btih,
		MagnetURI: torrentURL,
		TotalSize: totalSize,
		NumFiles:  len(files),
		Seeders:   -1,
		Leechers:  -1,
		Files:     files,
	}, nil
}

func extractArchiveOrgIdentifier(input string) string {
	input = strings.TrimSpace(input)
	if strings.HasPrefix(input, "ia:") {
		return strings.TrimPrefix(input, "ia:")
	}
	if strings.HasPrefix(input, "archiveorg:") {
		return strings.TrimPrefix(input, "archiveorg:")
	}
	if strings.Contains(input, "archive.org/download/") {
		parts := strings.Split(input, "archive.org/download/")
		if len(parts) > 1 {
			seg := strings.Split(parts[1], "/")[0]
			return strings.TrimSpace(seg)
		}
	}
	if strings.Contains(input, "archive.org/details/") {
		parts := strings.Split(input, "archive.org/details/")
		if len(parts) > 1 {
			seg := strings.Split(parts[1], "/")[0]
			return strings.TrimSpace(seg)
		}
	}
	if strings.Contains(input, "archive.org/metadata/") {
		parts := strings.Split(input, "archive.org/metadata/")
		if len(parts) > 1 {
			seg := strings.Split(parts[1], "/")[0]
			return strings.TrimSpace(seg)
		}
	}
	if !strings.Contains(input, "://") && !strings.Contains(input, "/") && !strings.Contains(input, " ") && len(input) > 3 {
		return input
	}
	return ""
}

// inspectHTTPFile probes direct HTTP/HTTPS downloads for metadata, Content-Length, and filename
func inspectHTTPFile(ctx context.Context, fileURL string) (*InspectResult, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	// 1. Try reading initial bytes to check if it is a bencoded .torrent file
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Digwire/0.3.2 (HTTP Inspector)")
	req.Header.Set("Range", "bytes=0-2097151")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent {
		bodyBytes, err := io.ReadAll(resp.Body)
		if err == nil && len(bodyBytes) > 0 {
			if mi, err := metainfo.Load(bytes.NewReader(bodyBytes)); err == nil && mi != nil {
				if info, err := mi.UnmarshalInfo(); err == nil {
					var files []TorrentFileDetail
					for idx, f := range info.UpvertedFiles() {
						files = append(files, TorrentFileDetail{
							Index:  idx,
							Path:   f.DisplayPath(&info),
							Length: f.Length,
						})
					}
					return &InspectResult{
						Name:      info.BestName(),
						InfoHash:  mi.HashInfoBytes().HexString(),
						MagnetURI: fileURL,
						TotalSize: info.TotalLength(),
						NumFiles:  len(files),
						Seeders:   -1,
						Leechers:  -1,
						Files:     files,
					}, nil
				}
			}
		}
	}

	// 2. Extract metadata from HTTP headers
	headReq, err := http.NewRequestWithContext(ctx, http.MethodHead, fileURL, nil)
	if err == nil {
		headReq.Header.Set("User-Agent", "Digwire/0.3.2 (HTTP Inspector)")
		if headResp, err := client.Do(headReq); err == nil {
			defer headResp.Body.Close()
			if headResp.StatusCode == http.StatusOK || headResp.StatusCode == http.StatusPartialContent {
				contentLength := headResp.ContentLength
				filename := extractFilenameFromHeaders(headResp, fileURL)
				return &InspectResult{
					Name:      filename,
					InfoHash:  HashURL(fileURL),
					MagnetURI: fileURL,
					TotalSize: contentLength,
					NumFiles:  1,
					Seeders:   -1,
					Leechers:  -1,
					Files: []TorrentFileDetail{
						{
							Index:  0,
							Path:   filename,
							Length: contentLength,
						},
					},
				}, nil
			}
		}
	}

	filename := extractFilenameFromURL(fileURL)
	return &InspectResult{
		Name:      filename,
		InfoHash:  HashURL(fileURL),
		MagnetURI: fileURL,
		TotalSize: -1,
		NumFiles:  1,
		Seeders:   -1,
		Leechers:  -1,
		Files: []TorrentFileDetail{
			{
				Index:  0,
				Path:   filename,
				Length: 0,
			},
		},
	}, nil
}

func extractFilenameFromHeaders(resp *http.Response, fileURL string) string {
	cd := resp.Header.Get("Content-Disposition")
	if cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if filename, ok := params["filename*"]; ok {
				if parts := strings.SplitN(filename, "''", 2); len(parts) == 2 {
					if unescaped, err := url.PathUnescape(parts[1]); err == nil {
						return filepath.Base(unescaped)
					}
				}
			}
			if filename, ok := params["filename"]; ok && filename != "" {
				return filepath.Base(filename)
			}
		}
	}
	return extractFilenameFromURL(fileURL)
}

func extractFilenameFromURL(fileURL string) string {
	if u, err := url.Parse(fileURL); err == nil {
		base := path.Base(u.Path)
		if base != "" && base != "/" && base != "." {
			if unescaped, err := url.PathUnescape(base); err == nil {
				return unescaped
			}
			return base
		}
	}
	return "download.bin"
}

func inspectMediaStream(ctx context.Context, mediaURL string) (*InspectResult, error) {
	meta, err := InspectMedia(ctx, mediaURL)
	if err != nil {
		return nil, err
	}

	var files []TorrentFileDetail
	totalSize := int64(0)

	videoExt := "mp4"
	if meta.Platform == "soundcloud" || strings.Contains(strings.ToLower(meta.Title), "audio") {
		videoExt = "mp3"
	}
	primaryName := fmt.Sprintf("%s.%s", sanitizeFilename(meta.Title), videoExt)

	files = append(files, TorrentFileDetail{
		Index:  0,
		Path:   primaryName,
		Length: totalSize,
	})

	for lang, subs := range meta.Subtitles {
		for _, s := range subs {
			subExt := s.Ext
			if subExt == "" {
				subExt = "vtt"
			}
			subName := fmt.Sprintf("%s.%s.%s", sanitizeFilename(meta.Title), lang, subExt)
			files = append(files, TorrentFileDetail{
				Index:  len(files),
				Path:   subName,
				Length: 0,
			})
		}
	}

	return &InspectResult{
		Name:      meta.Title,
		InfoHash:  HashURL(mediaURL),
		MagnetURI: mediaURL,
		TotalSize: totalSize,
		NumFiles:  len(files),
		Seeders:   -1,
		Leechers:  -1,
		Files:     files,
	}, nil
}

func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	invalid := []string{"/", "\\", ":", "*", "?", "\"", "<", ">", "|"}
	for _, char := range invalid {
		name = strings.ReplaceAll(name, char, "_")
	}
	if name == "" {
		return "media_file"
	}
	return name
}
