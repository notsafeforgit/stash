package sourceadapter

import (
	"encoding/json"
	"strings"

	"github.com/stashapp/stash/pkg/jsonutil"
	"github.com/stashapp/stash/pkg/models"
)

// CapturedMetadata uses source text verbatim. It follows the download producer's
// projection; a linked media host cannot replace its enclosing post's caption.
func CapturedMetadata(raw []byte) (models.SourcePostMetadata, error) {
	ret := models.SourcePostMetadata{}
	data, err := jsonutil.DecodeJSONObject(raw, MaxSourcePayloadBytes)
	if err != nil {
		return ret, err
	}
	data, _, category, err := capturedSourceContext(data)
	if err != nil {
		return ret, err
	}
	textKeys, dateKeys := []string{"content", "selftext", "title"}, []string{"date"}
	switch category {
	case "erome":
		// gallery-dl derives date from a media URL's cache/version parameter,
		// which does not establish the album's publication time.
		dateKeys = nil
	case "bluesky":
		textKeys, dateKeys = []string{"text"}, []string{"createdAt", "date"}
	case "tiktok":
		textKeys = []string{"desc", "title"}
	case "instagram":
		textKeys, dateKeys = []string{"description"}, []string{"post_date"}
		if data["type"] == "story" || data["type"] == "highlight" {
			dateKeys = []string{"date"}
		}
	case "tumblr":
		textKeys = []string{"caption", "body", "description", "summary"}
	case "jpgfish", "imglike", "putmega", "leakgallery":
		textKeys = []string{"description", "caption", "title"}
	case "patreon":
		dateKeys = []string{"published_at", "date"}
	case "kemono", "coomer":
		// The extractor's date may fall back to the mirror's import time.
		dateKeys = []string{"published"}
	case "ytdl", "ytdl-generic":
		textKeys, dateKeys = []string{"description", "title"}, nil
		ret.PublishedAt, err = capturedYTDLPublication(data)
		if err != nil {
			return ret, err
		}
	}
	for _, field := range []struct {
		out  **string
		keys []string
	}{
		{&ret.Title, []string{"title"}},
		{&ret.OriginalText, textKeys},
		{&ret.PublishedAt, dateKeys},
		{&ret.Language, []string{"lang", "language"}},
	} {
		for _, key := range field.keys {
			if value, ok := data[key].(string); ok && value != "" {
				*field.out = &value
				break
			}
		}
	}
	if ret.PublishedAt != nil && strings.HasPrefix(*ret.PublishedAt, "0001-") {
		switch category {
		case "tumblr", "jpgfish", "imglike", "putmega", "leakgallery":
			ret.PublishedAt = nil
		}
	}
	if ret.PublishedAt != nil {
		basis := "source"
		ret.DateBasis = &basis
	}
	encoded, err := json.Marshal(ret)
	if err != nil || len(encoded) > 262144 {
		return models.SourcePostMetadata{}, models.ErrEnrichmentInvalid
	}
	return ret, nil
}
