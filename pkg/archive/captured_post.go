package archive

import "github.com/stashapp/stash/pkg/models"

// ExtractCapturedPost identifies the post described by the supported capture
// envelope, independently of album membership. Crossposts keep their own ID.
// Unsupported extractors need an explicit identity adapter before network intake.
func ExtractCapturedPost(raw []byte) (*models.SourcePostIdentifier, error) {
	data, err := DecodeJSONObject(raw, MaxSourcePayloadBytes)
	if err != nil {
		return nil, err
	}
	category, _ := data["category"].(string)
	path := ""
	if parent, ok := data["_reddit"].(sourceObject); ok && sourceTruthy(parent["id"]) {
		data, path, category = parent, "/_reddit", "reddit"
	}
	switch category {
	case "reddit":
		return capturedPostReference("native:reddit", capturedFieldAt(data, path, "id"))
	case "twitter":
		root := data
		if legacy, ok := data["legacy"].(sourceObject); ok {
			data, path = legacy, "/legacy"
		}
		return capturedPostReference("native:twitter", capturedFieldAt(root, "", "tweet_id"), capturedFieldAt(root, "", "rest_id"), capturedFieldAt(data, path, "id_str"))
	default:
		return nil, nil
	}
}
