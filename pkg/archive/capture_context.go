package archive

import (
	"bytes"
	"slices"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

const CaptureContextPolicy = "source-retention-v1+capture-context-v1"

// CaptureContextRetention applies current reduction only to the new source
// fields. Every excluded parent must have an exact, separately verified binding.
func CaptureContextRetention(raw []byte, links []models.SourceCaptureContext) error {
	if len(links) < 1 || len(links) > 2 {
		return models.ErrSourceCaptureReplay
	}
	data, err := DecodeJSONObject(raw, MaxSourcePayloadBytes)
	if err != nil {
		return err
	}
	previous := ""
	for _, link := range links {
		if !translationUUID(link.CaptureUUID) || !translationUUID(link.ParentUUID) || link.CaptureUUID == link.ParentUUID ||
			!slices.Contains([]string{"/_parent", "/_reddit"}, link.Path) || link.Path <= previous {
			return models.ErrSourceCaptureReplay
		}
		key := strings.TrimPrefix(link.Path, "/")
		if _, ok := data[key].(sourceObject); !ok {
			return models.ErrSourceCaptureReplay
		}
		delete(data, key)
		previous = link.Path
	}
	for _, key := range []string{"_parent", "_reddit"} {
		if _, exists := data[key]; exists {
			return models.ErrSourceCaptureReplay
		}
	}
	flat, err := EncodeSourceJSON(data)
	if err != nil {
		return err
	}
	clean, err := RetainSourcePayload(flat)
	if err != nil {
		return err
	}
	if !bytes.Equal(flat, clean) {
		return models.ErrSourceCaptureReplay
	}
	return nil
}

// ValidateCaptureContext verifies the exact embedded parent, without applying a
// newer retention policy or assigning the child's observation time to it.
func ValidateCaptureContext(link models.SourceCaptureContext, child, parent *models.SourceCapture) error {
	if child == nil || parent == nil || child.Payload == nil || parent.Payload == nil ||
		link.CaptureUUID != child.UUID || link.ParentUUID != parent.UUID || child.UUID == parent.UUID ||
		child.PostUUID != parent.PostUUID || (link.Path != "/_parent" && link.Path != "/_reddit") ||
		(!parent.CapturedAt.IsZero() && (child.CapturedAt.IsZero() || parent.CapturedAt.After(child.CapturedAt))) {
		return models.ErrSourceCaptureReplay
	}
	raw, err := RestoreCapture(child.Payload)
	if err != nil {
		return err
	}
	data, err := DecodeJSONObject(raw, MaxSourcePayloadBytes)
	if err != nil {
		return err
	}
	embedded, ok := data[strings.TrimPrefix(link.Path, "/")].(sourceObject)
	if !ok {
		return models.ErrSourceCaptureReplay
	}
	expected, err := EncodeSourceJSON(embedded)
	if err != nil {
		return err
	}
	actual, err := RestoreCapture(parent.Payload)
	if err != nil {
		return err
	}
	if !bytes.Equal(expected, actual) {
		return models.ErrSourceCaptureReplay
	}
	return nil
}

// CapturedContextPath is the payload location used for the post and publisher.
// Feed/profile context does not replace a social post's own publisher.
func CapturedContextPath(raw []byte) (string, error) {
	data, err := DecodeJSONObject(raw, MaxSourcePayloadBytes)
	if err != nil {
		return "", err
	}
	_, path, _, err := capturedSourceContext(data)
	return path, err
}

func (t *EnrichmentTranscript) CaptureContext(ordinal int, capture, parentCapture string) (*models.SourceCaptureContext, error) {
	if ordinal < 0 || ordinal >= len(t.Records) || !translationUUID(capture) {
		return nil, models.ErrEnrichmentInvalid
	}
	record := t.Records[ordinal]
	if record.Parent == nil {
		return nil, nil
	}
	if !translationUUID(parentCapture) {
		return nil, models.ErrEnrichmentInvalid
	}
	path := "/_parent"
	if t.expanded[*record.Parent]["category"] == "reddit" {
		path = "/_reddit"
	}
	return &models.SourceCaptureContext{CaptureUUID: capture, Path: path, ParentUUID: parentCapture}, nil
}
