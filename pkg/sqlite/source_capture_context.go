package sqlite

import (
	"context"
	"strings"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

func (s *SourceEvidenceStore) CaptureContexts(ctx context.Context, value string) ([]models.SourceCaptureContext, error) {
	if !validSourceRunUUID(value) {
		return nil, models.ErrSourceCaptureReplay
	}
	var ret []models.SourceCaptureContext
	err := dbWrapper.Select(ctx, &ret, "SELECT * FROM source_capture_contexts WHERE capture_uuid=? ORDER BY path", value)
	return ret, err
}

// The source capture and its context bindings are one immutable operation.
// Parent evidence must already exist; no post-publication reinterpretation is
// permitted and the signature binds every path to its original capture UUID.
func (s *SourceEvidenceStore) validateCaptureContexts(ctx context.Context, input models.SourceCaptureInput) error {
	child := &models.SourceCapture{UUID: input.UUID, PostUUID: input.PostUUID,
		CapturedAt: input.CapturedAt, Payload: &input.Payload}
	for _, link := range input.Contexts {
		parent, err := s.FindCapture(ctx, link.ParentUUID)
		if err != nil {
			return err
		}
		if err := archive.ValidateCaptureContext(link, child, parent); err != nil {
			return err
		}
	}
	return nil
}

// Follow only the context actually selected by the publisher extractor. A feed
// owner's profile must not replace the observed author of a social post.
func publisherObservationCapture(ctx context.Context, capture *models.SourceCapture) (*models.SourceCapture, error) {
	seen := map[string]bool{}
	for capture != nil && !seen[capture.UUID] && len(seen) <= 32 {
		if len(capture.Contexts) == 0 {
			return capture, nil
		}
		seen[capture.UUID] = true
		raw, err := archive.RestoreCapture(capture.Payload)
		if err != nil {
			return nil, err
		}
		path, err := archive.CapturedContextPath(raw)
		if err != nil {
			// The preview reports malformed source identity separately. It still
			// must not turn a source extraction error into a new observation time.
			return capture, nil
		}
		var selected *models.SourceCaptureContext
		for i := range capture.Contexts {
			link := &capture.Contexts[i]
			if path == link.Path || strings.HasPrefix(path, link.Path+"/") {
				selected = link
				break
			}
		}
		if selected == nil {
			return capture, nil
		}
		parent, err := (&SourceEvidenceStore{}).FindCapture(ctx, selected.ParentUUID)
		if err != nil {
			return nil, err
		}
		if err := archive.ValidateCaptureContext(*selected, capture, parent); err != nil {
			return nil, models.ErrSourcePayloadCorrupt
		}
		capture = parent
	}
	return nil, models.ErrSourcePayloadCorrupt
}
