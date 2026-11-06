package sourceadapter

import (
	"path"
	"strings"

	"github.com/stashapp/stash/pkg/jsonutil"
	"github.com/stashapp/stash/pkg/models"
)

// CapturedRedgifsGalleryLink identifies the requested seed of an additional
// gallery member. A host item alone cannot authorize another collection.
func CapturedRedgifsGalleryLink(raw []byte) (*models.SourcePostIdentifier, error) {
	data, err := jsonutil.DecodeJSONObject(raw, MaxSourcePayloadBytes)
	if err != nil {
		return nil, err
	}
	_, _, category, err := capturedSourceContext(data)
	if err != nil || category != "redgifs" || data["category"] != "redgifs" {
		return nil, err
	}
	gallery, _ := data["gallery"].(string)
	u := sourceURL(data["source_extractor_url"])
	if gallery == "" || len(gallery) > 256 || u == nil || u.User != nil || u.Port() != "" {
		return nil, nil
	}
	switch u.Hostname() {
	case "redgifs.com", "www.redgifs.com", "m.redgifs.com", "v3.redgifs.com":
		if !capturedRedgifsWatch.MatchString(u.EscapedPath()) {
			return nil, nil
		}
	case "i.redgifs.com":
		name := strings.TrimSuffix(strings.TrimPrefix(u.EscapedPath(), "/i/"), path.Ext(u.EscapedPath()))
		if !strings.HasPrefix(u.EscapedPath(), "/i/") || !redgifsItemID.MatchString(name) {
			return nil, nil
		}
	default:
		return nil, nil
	}
	ref, _, _ := redditExternalReference(data["source_extractor_url"])
	return ref, nil
}

// CapturedRedgifsGalleryMember compares the item's API gallery identity with
// the linked seed's already accepted response. Reddit attachment authority and
// each Redgifs item's own title, publisher and date remain separate.
func CapturedRedgifsGalleryMember(parent, member []byte) (bool, error) {
	linked, err := CapturedRedgifsGalleryLink(member)
	if err != nil || linked == nil {
		return false, err
	}
	seed, err := jsonutil.DecodeJSONObject(parent, MaxSourcePayloadBytes)
	if err != nil {
		return false, err
	}
	item, err := jsonutil.DecodeJSONObject(member, MaxSourcePayloadBytes)
	if err != nil {
		return false, err
	}
	_, _, category, err := capturedSourceContext(seed)
	if err != nil || category != "reddit" || seed["category"] != "redgifs" || seed["gallery"] != item["gallery"] {
		return false, err
	}
	requested := linked.Value
	if linked.Namespace == "native:reddit" {
		u := sourceURL(item["source_extractor_url"])
		requested = strings.TrimSuffix(path.Base(u.EscapedPath()), path.Ext(u.EscapedPath()))
	}
	seedPost, err := capturedRedgifsPost(seed, "")
	if err != nil || seedPost.Value != strings.ToLower(requested) {
		return false, err
	}
	album, err := ExtractCapturedAlbum(parent)
	if err != nil || album == nil || album.Post.Namespace != "native:reddit" {
		return false, err
	}
	for _, entry := range album.Manifest.Entries {
		if entry.Reference == *linked {
			return true, nil
		}
	}
	return false, nil
}
