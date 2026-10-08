package archive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/stashapp/stash/pkg/models"
)

var capturedYTDLSite = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]*$`)
var capturedYTDLDay = regexp.MustCompile(`^[0-9]{8}$`)

// A named extractor scopes its own video IDs. Generic extractors often use a
// basename or locally numbered ID, so the source webpage must also qualify it.
// Collection/playlist IDs, filenames and signed download URLs are not identity.
func capturedYTDLPost(data sourceObject, path string) (*models.SourcePostIdentifier, error) {
	if _, ok := data["entries"]; ok {
		return nil, errors.New("a yt-dlp collection is not a resolved video")
	}
	id, err := capturedIdentifierValue(capturedFieldAt(data, path, "id"))
	if err != nil || id == "" || strings.TrimSpace(id) != id || len(id) > 1024 || strings.IndexFunc(id, unicode.IsControl) >= 0 {
		return nil, errors.New("yt-dlp leaf has no usable video ID")
	}
	site, _ := data["extractor_key"].(string)
	if strings.IndexFunc(site, func(c rune) bool { return c >= 127 }) >= 0 {
		return nil, errors.New("yt-dlp leaf has no usable extractor key")
	}
	site = strings.ToLower(site)
	if !capturedYTDLSite.MatchString(site) {
		return nil, errors.New("yt-dlp leaf has no usable extractor key")
	}
	if site == "generic" {
		page, _ := data["webpage_url"].(string)
		_, escapeErr := url.PathUnescape(page)
		if len(page) > 8192 || escapeErr != nil || strings.IndexFunc(page, func(c rune) bool { return c <= 32 || c >= 127 || c == '\\' }) >= 0 {
			return nil, errors.New("generic yt-dlp identity requires its source webpage URL")
		}
		u, err := url.Parse(page)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Port() != "" ||
			!capturedYTDLSite.MatchString(strings.ToLower(u.Hostname())) || !strings.EqualFold(u.Host, u.Hostname()) {
			return nil, errors.New("generic yt-dlp identity requires its source webpage URL")
		}
		site = strings.ToLower(u.Hostname())
		// Preserve the original path spelling, including valid escapes. Go's
		// URL.String and Python's urlunsplit encode literal punctuation
		// differently; identity must not depend on the caller's language.
		_, rest, _ := strings.Cut(page, "://")
		rawPath, _, _ := strings.Cut(strings.TrimPrefix(rest, u.Host), "?")
		rawPath, _, _ = strings.Cut(rawPath, "#")
		if rawPath == "" {
			rawPath = "/"
		}
		page = u.Scheme + "://" + site + rawPath
		if u.RawQuery != "" {
			page += "?" + u.RawQuery
		}
		digest := sha256.Sum256([]byte(page + "\x00" + id))
		id = "page:" + hex.EncodeToString(digest[:])
	}
	return capturedPostReference("ytdl:"+site, capturedField{value: id, path: path})
}

func capturedYTDLAlbum(data sourceObject, path string) (*CapturedAlbum, error) {
	if data["ytdl_media"] == nil {
		return nil, nil
	}
	marker, ok := data["ytdl_media"].(sourceObject)
	if !ok || len(marker) != 2 || marker["version"] != json.Number("1") || marker["type"] != "video" {
		return nil, errors.New("yt-dlp attachment requires a resolved video leaf")
	}
	post, err := capturedYTDLPost(data, path)
	if err != nil {
		return nil, err
	}
	count := 1
	return &CapturedAlbum{Policy: CapturedAlbumPolicy, Post: *post, EvidencePath: pointerMember(path, "ytdl_media"),
		Manifest: models.SourceAttachmentManifestInput{Complete: true, ExpectedCount: &count,
			Entries: []models.SourceAttachmentEntry{{Position: 0, Reference: *post, MediaKind: "unknown"}}}}, nil
}

// Keep day-only metadata as a date; do not invent a publication time from it.
// Download window checks require a timestamp and reject undated/day-only data.
func capturedYTDLPublication(data sourceObject) (*string, error) {
	if raw := data["timestamp"]; raw != nil {
		number, ok := raw.(json.Number)
		if !ok {
			return nil, errors.New("invalid yt-dlp publication timestamp")
		}
		seconds, err := number.Float64()
		if err != nil || math.IsInf(seconds, 0) || math.IsNaN(seconds) || seconds < -62135596800 || seconds >= 253402300800 {
			return nil, errors.New("invalid yt-dlp publication timestamp")
		}
		value := time.UnixMilli(int64(math.Floor(seconds * 1000))).UTC().Format("2006-01-02T15:04:05.000Z")
		return &value, nil
	}
	if raw := data["upload_date"]; raw != nil {
		day, ok := raw.(string)
		if !ok || !capturedYTDLDay.MatchString(day) {
			return nil, errors.New("invalid yt-dlp upload date")
		}
		parsed, err := time.Parse("20060102", day)
		if err != nil || parsed.Year() < 1 {
			return nil, errors.New("invalid yt-dlp upload date")
		}
		value := parsed.Format("2006-01-02")
		return &value, nil
	}
	return nil, nil
}
