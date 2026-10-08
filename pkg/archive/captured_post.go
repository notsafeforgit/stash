package archive

import (
	"errors"
	"regexp"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

var capturedNumericPost = regexp.MustCompile(`^[0-9]{1,30}$`)
var capturedBlueskyRecord = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)

// Child-host metadata belongs to the enclosing source post. Follow retained
// extractor ancestry for supported media hosts. Social extractors also retain
// feed/profile parents; those must not replace the actual post or publisher.
func capturedSourceContext(data sourceObject) (sourceObject, string, string, error) {
	path := ""
	category, _ := data["category"].(string)
	category = strings.ToLower(category)
	for range 32 {
		if parent, ok := data["_reddit"].(sourceObject); ok && sourceTruthy(parent["id"]) {
			data, path, category = parent, pointerMember(path, "_reddit"), "reddit"
			continue
		}
		if category != "imgur" && category != "redgifs" {
			return data, path, strings.ToLower(category), nil
		}
		if raw := data["_parent"]; raw != nil {
			parent, ok := raw.(sourceObject)
			if !ok {
				return nil, "", "", errors.New("invalid captured extractor parent")
			}
			category, ok = parent["category"].(string)
			if !ok || category == "" {
				return nil, "", "", errors.New("captured extractor parent has no category")
			}
			category = strings.ToLower(category)
			data, path = parent, pointerMember(path, "_parent")
			continue
		}
		return data, path, strings.ToLower(category), nil
	}
	return nil, "", "", errors.New("captured extractor ancestry exceeds limit")
}

func capturedNumericPostReference(namespace string, fields ...capturedField) (*models.SourcePostIdentifier, error) {
	ref, err := capturedPostReference(namespace, fields...)
	if err == nil && ref != nil && !capturedNumericPost.MatchString(ref.Value) {
		return nil, errors.New("captured source post ID is not numeric")
	}
	return ref, err
}

func capturedBlueskyPost(data sourceObject, path string) (*models.SourcePostIdentifier, error) {
	post, err := capturedIdentifierValue(capturedFieldAt(data, path, "post_id"))
	if err != nil {
		return nil, err
	}
	author, _ := data["author"].(sourceObject)
	did, err := capturedIdentifierValue(capturedFieldAt(author, pointerMember(path, "author"), "did"))
	if err != nil {
		return nil, err
	}
	if raw := data["uri"]; raw != nil && raw != "" {
		uri, ok := raw.(string)
		if !ok || !strings.HasPrefix(uri, "at://") {
			return nil, errors.New("invalid captured Bluesky post URI")
		}
		parts := strings.Split(strings.TrimPrefix(uri, "at://"), "/")
		if len(parts) != 3 || parts[1] != "app.bsky.feed.post" || (did != "" && did != parts[0]) || (post != "" && post != parts[2]) {
			return nil, errors.New("captured Bluesky post identifiers disagree")
		}
		did, post = parts[0], parts[2]
	}
	if did == "" || post == "" {
		return nil, nil
	}
	if !profilePatterns["bluesky_id"].MatchString(did) || !capturedBlueskyRecord.MatchString(post) {
		return nil, errors.New("invalid captured Bluesky post identity")
	}
	return capturedPostReference("native:bluesky", capturedField{value: did + "/" + post, path: path})
}

func capturedMirrorPost(data sourceObject, path, category string) (*models.SourcePostIdentifier, error) {
	service, _ := data["service"].(string)
	namespace := "mirror:" + category + ":" + strings.ToLower(service)
	if !ValidAccountNamespace(namespace) {
		return nil, errors.New("captured mirror post has no valid service")
	}
	parts := make([]string, 2)
	for i, key := range []string{"user", "id"} {
		value, err := capturedIdentifierValue(capturedFieldAt(data, path, key))
		if err != nil {
			return nil, err
		}
		if value == "" {
			return nil, nil
		}
		if _, err := NormalizeAccountReference(models.AccountReference{Namespace: namespace, Kind: "id", Value: value}); err != nil {
			return nil, err
		}
		if strings.ContainsAny(value, "/\\?#%") {
			return nil, errors.New("captured mirror post contains an ambiguous path component")
		}
		parts[i] = value
	}
	return capturedPostReference(namespace, capturedField{value: strings.Join(parts, "/"), path: path})
}

// CapturedPostPlatform names the actual extractor for capture provenance. The
// post reference separately preserves the mirror's service/account qualification.
func CapturedPostPlatform(post models.SourcePostIdentifier) string {
	parts := strings.Split(post.Namespace, ":")
	if len(parts) == 3 && parts[0] == "mirror" {
		return parts[1]
	}
	return strings.TrimPrefix(post.Namespace, "native:")
}

// ExtractCapturedPost identifies the post described by the supported capture
// envelope, independently of album membership. Crossposts keep their own ID.
// Unsupported extractors need an explicit identity adapter before network intake.
func ExtractCapturedPost(raw []byte) (*models.SourcePostIdentifier, error) {
	data, err := DecodeJSONObject(raw, MaxSourcePayloadBytes)
	if err != nil {
		return nil, err
	}
	data, path, category, err := capturedSourceContext(data)
	if err != nil {
		return nil, err
	}
	switch category {
	case "ytdl", "ytdl-generic":
		return capturedYTDLPost(data, path)
	case "reddit":
		return capturedPostReference("native:reddit", capturedFieldAt(data, path, "id"))
	case "twitter":
		root, rootPath := data, path
		if legacy, ok := data["legacy"].(sourceObject); ok {
			data, path = legacy, pointerMember(path, "legacy")
		}
		return capturedPostReference("native:twitter", capturedFieldAt(root, rootPath, "tweet_id"), capturedFieldAt(root, rootPath, "rest_id"), capturedFieldAt(data, path, "id_str"))
	case "bluesky":
		return capturedBlueskyPost(data, path)
	case "tiktok", "patreon", "fansly":
		return capturedNumericPostReference("native:"+category, capturedFieldAt(data, path, "id"))
	case "instagram":
		if album, err := capturedInstagramAlbum(data, path); err != nil {
			return nil, err
		} else if album != nil {
			return &album.Post, nil
		}
		if data["type"] == "story" || data["type"] == "highlight" {
			return nil, nil // these are containers, not regular post IDs
		}
		return capturedNumericPostReference("native:instagram", capturedFieldAt(data, path, "post_id"), capturedFieldAt(data, path, "sidecar_media_id"))
	case "kemono", "coomer":
		return capturedMirrorPost(data, path, category)
	default:
		return nil, nil
	}
}
