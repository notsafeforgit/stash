package archive

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"github.com/stashapp/stash/pkg/models"
)

const CapturedAccountPolicy = "captured-account-v1"

// CapturedAccount contains claims from one captured publisher. It does not
// choose an account UUID, establish ownership, or assign depicted performers.
type CapturedAccount = models.CapturedAccount
type CapturedAccountIdentifier = models.CapturedAccountIdentifier

type capturedField struct {
	value interface{}
	path  string
}

func capturedFieldAt(object sourceObject, path, key string) capturedField {
	return capturedField{value: object[key], path: pointerMember(path, key)}
}

func firstCapturedField(object sourceObject, path string, keys ...string) capturedField {
	for _, key := range keys {
		field := capturedFieldAt(object, path, key)
		if field.value != nil && field.value != "" {
			return field
		}
	}
	return capturedField{}
}

var capturedIntegerID = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)$`)

func capturedIdentifierValue(field capturedField) (string, error) {
	switch value := field.value.(type) {
	case nil:
		return "", nil
	case string:
		return value, nil
	case json.Number:
		if capturedIntegerID.MatchString(string(value)) {
			return string(value), nil
		}
	}
	return "", fmt.Errorf("captured identifier at %s must be a string or integer", field.path)
}

func addCapturedAccountIdentifier(a *CapturedAccount, field capturedField, kind, basis string) error {
	value, err := capturedIdentifierValue(field)
	if err != nil || value == "" {
		return err
	}
	ref, err := NormalizeAccountReference(models.AccountReference{Namespace: a.Namespace, Kind: kind, Value: value})
	if err != nil {
		return fmt.Errorf("captured account identifier at %s: %w", field.path, err)
	}
	a.Identifiers = append(a.Identifiers, CapturedAccountIdentifier{Reference: ref, Basis: basis, Path: field.path})
	return nil
}

func capturedAccountLabel(field capturedField) string {
	label, _ := field.value.(string)
	label = strings.TrimSpace(label)
	if len(label) > 1024 || strings.IndexFunc(label, unicode.IsControl) >= 0 {
		return ""
	}
	return label
}

// ExtractCapturedAccount reads gallery-dl/yt-dlp metadata without mutating it.
// Only an explicitly captured publisher ID establishes a pairing with handles.
// Directory names, scrape URLs, captions, and feed-owner profiles are not used.
// A nil result means there is insufficient identity evidence. Invalid claimed
// identifiers return an error so an importer can report them for review.
func ExtractCapturedAccount(raw []byte) (*CapturedAccount, error) {
	return extractCapturedAccount(raw, true)
}

// CapturedAccountReferences also reports handle-only author evidence. This is
// useful for rejecting conflicting historical associations, but does not make a
// handle sufficient for automatic account creation or identity consolidation.
func CapturedAccountReferences(raw []byte) (*CapturedAccount, error) {
	return extractCapturedAccount(raw, false)
}

func extractCapturedAccount(raw []byte, requireID bool) (*CapturedAccount, error) {
	data, err := DecodeJSONObject(raw, MaxSourcePayloadBytes)
	if err != nil {
		return nil, err
	}
	data, path, category, err := capturedSourceContext(data)
	if err != nil {
		return nil, err
	}
	if category == "" {
		return nil, nil
	}
	result := &CapturedAccount{Policy: CapturedAccountPolicy, Namespace: "native:" + category}
	var id, handle, secondary capturedField
	idKind, basis := "id", "captured-author"
	author, _ := data["author"].(sourceObject)
	authorPath := pointerMember(path, "author")
	field := func(key string) capturedField { return capturedFieldAt(data, path, key) }
	first := func(keys ...string) capturedField { return firstCapturedField(data, path, keys...) }
	switch category {
	case "reddit":
		id, handle = field("author_fullname"), field("author")
		if author != nil {
			handle = capturedFieldAt(author, authorPath, "name")
		}
	case "twitter":
		id, handle = capturedFieldAt(author, authorPath, "id"), capturedFieldAt(author, authorPath, "name")
	case "bluesky":
		id, handle = capturedFieldAt(author, authorPath, "did"), capturedFieldAt(author, authorPath, "handle")
	case "tiktok":
		id = capturedFieldAt(author, authorPath, "id")
		secondary = capturedFieldAt(author, authorPath, "secUid")
		handle = firstCapturedField(author, authorPath, "uniqueId", "name")
		if id.value == nil || id.value == "" {
			id, secondary, idKind = secondary, capturedField{}, "secUid"
		}
	case "instagram":
		id, handle = field("owner_id"), field("username")
	case "tumblr":
		blog, _ := data["blog"].(sourceObject)
		blogPath := pointerMember(path, "blog")
		id = capturedFieldAt(blog, blogPath, "uuid")
		handle = first("blog_name")
		if handle.value == nil {
			handle = capturedFieldAt(blog, blogPath, "name")
		}
	case "coomer", "kemono":
		service, _ := data["service"].(string)
		if service == "" {
			return nil, nil
		}
		result.Namespace = "mirror:" + category + ":" + strings.ToLower(service)
		id, idKind, basis = field("user"), "user", "captured-mirror-user"
		result.Label = capturedAccountLabel(field("username"))
	case "ytdl", "ytdl-generic":
		site, _ := first("extractor_key", "subcategory").value.(string)
		site = strings.ToLower(site)
		if site == "generic" {
			page, _ := data["webpage_url"].(string)
			if u, err := url.Parse(page); err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.User == nil {
				site = strings.ToLower(u.Hostname())
			}
		}
		if site == "" || site == "generic" {
			return nil, nil
		}
		result.Namespace = "ytdl:" + site
		id = first("channel_id", "uploader_id")
		// An uploader/channel display name is not necessarily a handle.
		result.Label = capturedAccountLabel(first("uploader", "channel"))
	case "fansly":
		if raw := data["account"]; raw != nil {
			account, ok := raw.(sourceObject)
			if !ok {
				return nil, errors.New("invalid captured Fansly publisher")
			}
			accountPath := pointerMember(path, "account")
			id, handle = capturedFieldAt(account, accountPath, "id"), capturedFieldAt(account, accountPath, "username")
			break
		}
		// Older retained captures use the generic publisher envelope.
		fallthrough
	default:
		owner, ownerPath := author, authorPath
		if owner == nil && sourceTruthy(data["author"]) {
			// Never borrow a feed owner's ID for a separately named author.
			return nil, nil
		}
		if len(owner) == 0 {
			for _, key := range []string{"owner", "uploader", "user", "creator"} {
				if candidate, ok := data[key].(sourceObject); ok && len(candidate) > 0 {
					owner, ownerPath = candidate, pointerMember(path, key)
					break
				}
			}
		}
		if len(owner) > 0 {
			id = firstCapturedField(owner, ownerPath, "id", "did", "uuid")
			handle = firstCapturedField(owner, ownerPath, "username", "account", "handle")
			result.Label = capturedAccountLabel(firstCapturedField(owner, ownerPath, "name"))
		} else {
			id, handle = first("user_id", "uploader_id"), field("username")
			result.Label = capturedAccountLabel(field("uploader"))
		}
	}
	if !ValidAccountNamespace(result.Namespace) {
		return nil, errors.New("captured account has an invalid service namespace")
	}
	if err := addCapturedAccountIdentifier(result, id, idKind, basis); err != nil {
		return nil, err
	}
	if requireID && len(result.Identifiers) == 0 {
		return nil, nil
	}
	if err := addCapturedAccountIdentifier(result, secondary, "secUid", basis); err != nil {
		return nil, err
	}
	if err := addCapturedAccountIdentifier(result, handle, "handle", basis); err != nil {
		return nil, err
	}
	if label := capturedAccountLabel(handle); label != "" {
		result.Label = label
	}
	if len(result.Identifiers) == 0 {
		return nil, nil
	}
	if category == "coomer" || category == "kemono" {
		profile, _ := data["user_profile"].(sourceObject)
		profilePath := pointerMember(path, "user_profile")
		profileID, _ := capturedIdentifierValue(capturedFieldAt(profile, profilePath, "id"))
		service, _ := profile["service"].(string)
		if profileID == result.Identifiers[0].Reference.Value && service == data["service"] {
			// Preserve the advertised public identifier in the mirror's namespace.
			// Neither it nor the display name proves a native service account ID.
			if err := addCapturedAccountIdentifier(result, capturedFieldAt(profile, profilePath, "public_id"), "public_id", "captured-mirror-public-id"); err != nil {
				return nil, err
			}
		}
	}
	return result, nil
}
