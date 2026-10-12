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
	var result *models.SourcePostMetadata
	// A derived date or a partial response must not hide an older caption.
	// Seek the newest nonempty value for each field within this identity group;
	// neither payloads nor a library-wide capture scan are needed.
	for _, field := range []string{"title", "original_text", "published_at"} {
		var raw string
		err := dbWrapper.Get(ctx, &raw, `WITH candidates AS MATERIALIZED (
 SELECT item.value AS uuid FROM source_post_identities i
 CROSS JOIN json_each((SELECT json_group_array(uuid) FROM (
  SELECT c.uuid FROM source_captures c INDEXED BY source_captures_order
  JOIN source_post_revisions r ON r.uuid=c.revision_uuid
  WHERE c.post_uuid=i.post_uuid AND length(coalesce(json_extract(r.metadata,?),''))>0
  ORDER BY coalesce(c.captured_at,c.recorded_at) DESC,c.uuid DESC LIMIT 1
 ))) item WHERE i.canonical_uuid=(SELECT canonical_uuid FROM source_post_identities WHERE post_uuid=?)
 ) SELECT r.metadata FROM candidates JOIN source_captures c ON c.uuid=candidates.uuid
 JOIN source_post_revisions r ON r.uuid=c.revision_uuid
 ORDER BY coalesce(c.captured_at,c.recorded_at) DESC,c.uuid DESC LIMIT 1`, "$."+field, post)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var metadata models.SourcePostMetadata
		if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
			return nil, err
		}
		if result == nil {
			result = &models.SourcePostMetadata{}
		}
		switch field {
		case "title":
			result.Title, result.Language = metadata.Title, metadata.Language
		case "original_text":
			result.OriginalText, result.Language = metadata.OriginalText, metadata.Language
		case "published_at":
			result.PublishedAt, result.DateBasis = metadata.PublishedAt, metadata.DateBasis
		}
	}
	return result, nil
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
