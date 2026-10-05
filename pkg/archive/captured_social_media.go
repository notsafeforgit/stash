package archive

import (
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/stashapp/stash/pkg/models"
)

var capturedBlueskyBlob = regexp.MustCompile(`^[A-Za-z0-9]{1,256}$`)
var capturedTiktokImage = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,256}$`)

func capturedMediaArray(raw interface{}) ([]interface{}, error) {
	values, ok := raw.([]interface{})
	if !ok || len(values) > MaxManifestEntries {
		return nil, errors.New("source media requires a bounded original attachment list")
	}
	return values, nil
}

func capturedBlueskyBlobEntry(raw interface{}, kind string) (sourceObject, error) {
	if raw == nil {
		return nil, nil
	}
	item, ok := raw.(sourceObject)
	if !ok {
		return nil, errors.New("invalid Bluesky source media")
	}
	if item[kind] == nil {
		return nil, nil
	}
	blob, ok := item[kind].(sourceObject)
	if !ok {
		return nil, errors.New("invalid Bluesky source blob")
	}
	ref, ok := blob["ref"].(sourceObject)
	if !ok && blob["ref"] != nil {
		return nil, errors.New("invalid Bluesky blob reference")
	}
	if ref["$link"] == nil && blob["cid"] == nil {
		return nil, nil
	}
	key := ""
	for _, raw := range []interface{}{ref["$link"], blob["cid"]} {
		if raw == nil {
			continue
		}
		value, ok := raw.(string)
		if !ok || (key != "" && value != "" && value != key) {
			return nil, errors.New("contradictory Bluesky blob identity")
		}
		if value != "" {
			key = value
		}
	}
	mime, _ := blob["mimeType"].(string)
	if !capturedBlueskyBlob.MatchString(key) || !strings.HasPrefix(mime, kind+"/") {
		return nil, errors.New("invalid Bluesky blob identity or media kind")
	}
	return sourceObject{"id": key, "kind": kind}, nil
}

func capturedBlueskyMembership(data sourceObject) ([]sourceObject, error) {
	items := []sourceObject{}
	if data["embed"] == nil {
		return items, nil
	}
	media, ok := data["embed"].(sourceObject)
	if !ok {
		return nil, errors.New("invalid Bluesky source embed")
	}
	if raw, exists := media["media"]; exists {
		media, ok = raw.(sourceObject)
		if !ok {
			return nil, errors.New("invalid Bluesky embedded media")
		}
	}
	for _, key := range []string{"images", "items"} {
		raw, exists := media[key]
		if !exists {
			continue
		}
		entries, err := capturedMediaArray(raw)
		if err != nil {
			return nil, err
		}
		for _, raw := range entries {
			kind := "image"
			if key == "items" {
				item, ok := raw.(sourceObject)
				_, image := item["image"]
				_, video := item["video"]
				if (!ok && raw != nil) || (image && video) {
					return nil, errors.New("ambiguous Bluesky source media")
				}
				if !image {
					kind = "video"
				}
			}
			item, err := capturedBlueskyBlobEntry(raw, kind)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
	}
	if _, exists := media["video"]; exists {
		item, err := capturedBlueskyBlobEntry(media, "video")
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if len(items) > MaxManifestEntries {
		return nil, errors.New("source attachment list exceeds 4096 entries")
	}
	return items, nil
}

func capturedTiktokImageKey(raw interface{}) (string, error) {
	value, ok := raw.(string)
	if !ok || len(value) > 16384 || strings.ContainsFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
		return "", errors.New("invalid TikTok original image URL")
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.Port() != "" || parsed.Fragment != "" {
		return "", errors.New("invalid TikTok original image URL")
	}
	path := parsed.EscapedPath()
	filename, err := url.PathUnescape(path[strings.LastIndex(path, "/")+1:])
	if err != nil {
		return "", err
	}
	if index := strings.LastIndex(filename, "."); index > 0 && utf8.RuneCountInString(filename[index+1:]) <= 16 {
		filename = filename[:index]
	}
	key, _, _ := strings.Cut(filename, "~")
	if !capturedTiktokImage.MatchString(key) || key == "." || key == ".." {
		return "", errors.New("TikTok image has no stable source key")
	}
	return "image:" + key, nil
}

func capturedTiktokImageEntry(raw interface{}) (sourceObject, error) {
	if raw == nil {
		return nil, nil
	}
	item, ok := raw.(sourceObject)
	if !ok {
		return nil, errors.New("invalid TikTok source image")
	}
	if item["imageURL"] == nil {
		return nil, nil
	}
	image, ok := item["imageURL"].(sourceObject)
	if !ok {
		return nil, errors.New("invalid TikTok source image URLs")
	}
	if image["urlList"] == nil {
		return nil, nil
	}
	urls, err := capturedMediaArray(image["urlList"])
	if err != nil {
		return nil, err
	}
	if len(urls) == 0 {
		return nil, nil
	}
	key, err := capturedTiktokImageKey(urls[0])
	if err != nil {
		return nil, err
	}
	return sourceObject{"id": key, "kind": "image"}, nil
}

func capturedTiktokMembership(data sourceObject) ([]sourceObject, error) {
	items := []sourceObject{}
	if raw, exists := data["imagePost"]; exists {
		post, ok := raw.(sourceObject)
		if !ok {
			return nil, errors.New("invalid TikTok image post")
		}
		images, err := capturedMediaArray(post["images"])
		if err != nil {
			return nil, err
		}
		for _, raw := range images {
			item, err := capturedTiktokImageEntry(raw)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
	} else if raw, exists := data["video"]; exists {
		if _, ok := raw.(sourceObject); !ok {
			return nil, errors.New("invalid TikTok video post")
		}
		post, err := capturedNumericPostReference("native:tiktok", capturedFieldAt(data, "", "id"))
		if err != nil || post == nil {
			return nil, errors.New("invalid TikTok video post ID")
		}
		items = append(items, sourceObject{"id": "video:" + post.Value, "kind": "video"})
	}
	return items, nil
}

func capturedSocialAlbum(data sourceObject, path, category string) (*CapturedAlbum, error) {
	field := category + "_media"
	if data[field] == nil {
		return nil, nil
	}
	evidence, ok := data[field].(sourceObject)
	if !ok || len(evidence) != 3 {
		return nil, errors.New("invalid source attachment manifest")
	}
	version, versionOK := evidence["version"].(json.Number)
	declared, postOK := evidence["post"].(sourceObject)
	observed, itemsOK := evidence["items"].([]interface{})
	if !versionOK || version.String() != "1" || !postOK || len(declared) != 2 || !itemsOK {
		return nil, errors.New("invalid source attachment manifest")
	}
	var post *models.SourcePostIdentifier
	var items []sourceObject
	var err error
	if category == "bluesky" {
		post, err = capturedBlueskyPost(data, path)
		if err == nil {
			items, err = capturedBlueskyMembership(data)
		}
	} else {
		post, err = capturedNumericPostReference("native:tiktok", capturedFieldAt(data, path, "id"))
		if err == nil {
			items, err = capturedTiktokMembership(data)
		}
	}
	if err != nil {
		return nil, err
	}
	if post == nil || declared["namespace"] != post.Namespace || declared["value"] != post.Value || len(observed) != len(items) {
		return nil, errors.New("source attachment manifest differs from its original post")
	}
	count := len(items)
	result := &CapturedAlbum{Policy: CapturedAlbumPolicy, Post: *post, EvidencePath: pointerMember(path, field) + "/items",
		Manifest: models.SourceAttachmentManifestInput{DeclaredAlbum: count > 1, Complete: true, ExpectedCount: &count}}
	for position, item := range items {
		if item == nil {
			if observed[position] != nil {
				return nil, errors.New("source attachment manifest hides an unavailable source slot")
			}
			result.Manifest.Complete = false
			continue
		}
		stored, ok := observed[position].(sourceObject)
		if !ok || len(stored) != 2 || stored["id"] != item["id"] || stored["kind"] != item["kind"] {
			return nil, errors.New("source attachment manifest differs from its original post")
		}
		result.Manifest.Entries = append(result.Manifest.Entries, models.SourceAttachmentEntry{
			Position: position, Reference: models.SourcePostIdentifier{Namespace: post.Namespace, Value: item["id"].(string)}, MediaKind: item["kind"].(string)})
	}
	return result, nil
}
