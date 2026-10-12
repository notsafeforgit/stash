package sourceadapter

import (
	"encoding/json"

	"github.com/stashapp/stash/pkg/jsonutil"
)

// PreservePostText fills gaps in a saved caption instead of collecting edits.
// Only the enclosing post's text is considered: publisher bios, quoted posts,
// per-file names and attachment evidence keep their incoming values.
func PreservePostText(incoming, saved []byte) (json.RawMessage, bool, error) {
	post, err := ExtractCapturedPost(incoming)
	if err != nil || post == nil {
		return incoming, false, err
	}
	previous, err := ExtractCapturedPost(saved)
	if err != nil || previous == nil || *post != *previous {
		return incoming, false, nil
	}
	root, err := jsonutil.DecodeJSONObject(incoming, MaxSourcePayloadBytes)
	if err != nil {
		return nil, false, err
	}
	oldRoot, err := jsonutil.DecodeJSONObject(saved, MaxSourcePayloadBytes)
	if err != nil {
		return nil, false, err
	}
	data, _, category, err := capturedSourceContext(root)
	if err != nil {
		return nil, false, err
	}
	old, _, _, err := capturedSourceContext(oldRoot)
	if err != nil {
		return nil, false, err
	}
	var fields []string
	switch category {
	case "reddit":
		fields = []string{"title", "selftext", "selftext_html"}
	case "twitter":
		fields = []string{"content", "full_text", "text"}
		if legacy, ok := data["legacy"].(sourceObject); ok {
			if prior, ok := old["legacy"].(sourceObject); ok {
				preserveTextFields(legacy, prior, fields)
			}
		}
	case "bluesky":
		fields = []string{"text"}
	case "tiktok":
		fields = []string{"desc"} // title is generated for each downloaded file
	case "instagram":
		fields = []string{"description"}
	case "tumblr":
		fields = []string{"title", "caption", "body", "description", "summary"}
	case "jpgfish", "imglike", "putmega", "leakgallery":
		fields = []string{"title", "description", "caption"}
	case "patreon", "fansly", "kemono", "coomer":
		fields = []string{"title", "content"}
	case "ytdl", "ytdl-generic":
		fields = []string{"title", "description"}
	default:
		return incoming, false, nil
	}
	preserveTextFields(data, old, fields)
	ret, err := jsonutil.EncodeSourceJSON(root)
	if err != nil {
		return nil, false, err
	}
	// Some services embed media in HTML captions. A new attachment is useful
	// evidence, even when it arrives inside text; never hide it as a text edit.
	before, err := ExtractCapturedAlbum(incoming)
	if err != nil {
		return nil, false, err
	}
	after, err := ExtractCapturedAlbum(ret)
	if err != nil {
		return incoming, false, nil
	}
	beforeJSON, err := json.Marshal(before)
	if err != nil {
		return nil, false, err
	}
	afterJSON, err := json.Marshal(after)
	if err != nil {
		return nil, false, err
	}
	if string(beforeJSON) != string(afterJSON) {
		return incoming, false, nil
	}
	return ret, true, nil
}

func preserveTextFields(data, saved sourceObject, fields []string) {
	for _, key := range fields {
		if text, ok := saved[key].(string); ok && text != "" {
			data[key] = text
		}
	}
}
