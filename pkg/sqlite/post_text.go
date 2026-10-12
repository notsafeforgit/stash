package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sourceadapter"
)

// PostContent returns one saved post projection without loading raw responses,
// profile bodies or exposing historical extraction snapshots as caption edits.
func (s *SourceEvidenceStore) PostContent(ctx context.Context, post string) (*models.SourcePostMetadata, error) {
	if _, err := archiveUUID(post); err != nil {
		return nil, err
	}
	latest, err := sourceReviewCurrentLatestCapture(ctx, post)
	if err != nil || latest == nil {
		return nil, err
	}
	var raw string
	if err := dbWrapper.Get(ctx, &raw, `SELECT metadata FROM source_post_revisions WHERE uuid=?`, latest.RevisionUUID); err != nil {
		return nil, err
	}
	var metadata models.SourcePostMetadata
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return nil, err
	}
	return &metadata, nil
}

// A caption is saved content, not an edit log. Use source captures only: legacy
// NFO text can be translated or incomplete and must not mask the original text
// when it is first obtained from the source.
func (s *SourceEvidenceStore) preservePostText(ctx context.Context, input *models.SourceCaptureInput) error {
	var id string
	err := dbWrapper.Get(ctx, &id, `SELECT uuid FROM source_captures INDEXED BY source_captures_order
 WHERE post_uuid=? AND origin='gallery-dl' AND platform=?
 ORDER BY coalesce(captured_at,recorded_at) DESC,uuid DESC LIMIT 1`, input.PostUUID, input.Platform)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	prior, err := s.FindCapture(ctx, id)
	if err != nil {
		return err
	}
	saved, err := archive.RestoreCapture(prior.Payload)
	if err != nil {
		return err
	}
	incoming, err := archive.RestoreCapture(&input.Payload)
	if err != nil {
		return err
	}
	retained, matched, err := sourceadapter.PreservePostText(incoming, saved)
	if err != nil || !matched {
		return err
	}
	payload, err := archive.PrepareRetainedCapture(input.Origin, input.Platform, retained)
	if err != nil {
		return err
	}
	input.Payload = *payload
	// Derive text from the retained source, including a newly filled body that
	// previously fell back to the title. Producer-projected text is only used
	// when the source has no corresponding field.
	metadata, err := archive.CapturedMetadata(retained)
	if err != nil {
		return err
	}
	for _, field := range []struct {
		dest          **string
		source, prior *string
	}{
		{&input.Metadata.Title, metadata.Title, prior.Metadata.Title},
		{&input.Metadata.OriginalText, metadata.OriginalText, prior.Metadata.OriginalText},
	} {
		if *field.dest == nil && field.prior == nil {
			continue
		}
		if field.source != nil && *field.source != "" {
			*field.dest = field.source
		} else if field.prior != nil && *field.prior != "" {
			*field.dest = field.prior
		}
	}
	return nil
}
