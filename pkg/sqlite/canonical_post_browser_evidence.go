package sqlite

import (
	"context"
	"fmt"

	"github.com/stashapp/stash/pkg/models"
)

// Original-owner evidence APIs remain available to import/replay and complete
// merge comparison. Current browsing pages each original owner's indexed range
// before ordering the bounded union; it never reconstructs retained payloads.
const canonicalPostIdentifiersQuery = `WITH candidates AS MATERIALIZED (
 SELECT json_extract(item.value,'$[0]') AS namespace,json_extract(item.value,'$[1]') AS value
 FROM source_post_identities i CROSS JOIN json_each((SELECT json_group_array(json_array(namespace,value)) FROM (
  SELECT namespace,value FROM source_post_identifiers WHERE post_uuid=i.post_uuid AND (namespace,value)>(?,?)
  ORDER BY namespace,value LIMIT ?
 ))) item WHERE i.canonical_uuid=(SELECT canonical_uuid FROM source_post_identities WHERE post_uuid=?)
) SELECT namespace,value FROM candidates ORDER BY namespace,value LIMIT ?`

func (s *SourceEvidenceStore) CurrentPostIdentifiers(ctx context.Context, post string, after *models.SourcePostIdentifier, limit int) ([]models.SourcePostIdentifier, error) {
	post, err := archiveUUID(post)
	if err != nil {
		return nil, err
	}
	limit, err = sourcePageLimit(limit)
	if err != nil {
		return nil, err
	}
	cursor := models.SourcePostIdentifier{}
	if after != nil {
		if err := validatePostIdentifier(*after); err != nil {
			return nil, fmt.Errorf("%w: %s", models.ErrSourcePostBrowseInvalid, err)
		}
		cursor = *after
	}
	var rows []struct {
		Namespace string `db:"namespace"`
		Value     string `db:"value"`
	}
	if err := dbWrapper.Select(ctx, &rows, canonicalPostIdentifiersQuery, cursor.Namespace, cursor.Value, limit, post, limit); err != nil {
		return nil, err
	}
	result := make([]models.SourcePostIdentifier, 0, len(rows))
	for _, row := range rows {
		result = append(result, models.SourcePostIdentifier{Namespace: row.Namespace, Value: row.Value})
	}
	return result, nil
}

// Choose one original witness per exact URL before applying the cursor. The
// winner is independent of pagination, so a duplicate on a later owner cannot
// reappear on a subsequent page. All original URLs/evidence remain immutable.
const canonicalPostURLsQuery = `WITH candidates AS MATERIALIZED (
 SELECT item.value AS uuid FROM source_post_identities i
 CROSS JOIN json_each((SELECT json_group_array(uuid) FROM (
  SELECT u.uuid FROM source_post_urls u WHERE u.post_uuid=i.post_uuid AND u.uuid>?
  AND NOT EXISTS(SELECT 1 FROM source_post_identities other
   CROSS JOIN source_post_urls earlier ON earlier.post_uuid=other.post_uuid AND earlier.url=u.url
   WHERE other.canonical_uuid=i.canonical_uuid AND earlier.uuid<u.uuid)
  ORDER BY u.uuid LIMIT ?
 ))) item WHERE i.canonical_uuid=(SELECT canonical_uuid FROM source_post_identities WHERE post_uuid=?)
), selected AS (SELECT uuid FROM candidates ORDER BY uuid LIMIT ?)
SELECT u.* FROM selected JOIN source_post_urls u ON u.uuid=selected.uuid ORDER BY u.uuid`

func (s *SourcePostLinksStore) CurrentURLs(ctx context.Context, post, after string, limit int) ([]models.SourcePostURL, error) {
	post, after, limit, err := postLinkPage(post, after, limit)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		UUID     string `db:"uuid"`
		PostUUID string `db:"post_uuid"`
		URL      string `db:"url"`
	}
	if err := dbWrapper.Select(ctx, &rows, canonicalPostURLsQuery, after, limit, post, limit); err != nil {
		return nil, err
	}
	result := make([]models.SourcePostURL, 0, len(rows))
	for _, row := range rows {
		result = append(result, models.SourcePostURL{UUID: row.UUID, PostUUID: row.PostUUID, URL: row.URL})
	}
	return result, nil
}

const canonicalPostCapturesQuery = `WITH candidates AS MATERIALIZED (
 SELECT json_extract(item.value,'$[0]') AS uuid,json_extract(item.value,'$[1]') AS clock
 FROM source_post_identities i
 CROSS JOIN json_each((SELECT json_group_array(json_array(uuid,clock)) FROM (
  SELECT uuid,coalesce(captured_at,recorded_at) AS clock FROM source_captures INDEXED BY source_captures_order
  WHERE post_uuid=i.post_uuid AND (coalesce(captured_at,recorded_at),uuid)>(?,?)
  ORDER BY coalesce(captured_at,recorded_at),uuid LIMIT ?
 ))) item WHERE i.canonical_uuid=(SELECT canonical_uuid FROM source_post_identities WHERE post_uuid=?)
), selected AS (SELECT uuid,clock FROM candidates ORDER BY clock,uuid LIMIT ?)
SELECT ` + sourceCaptureColumns + `,c.recorded_at FROM selected
JOIN source_captures c ON c.uuid=selected.uuid
JOIN source_post_revisions r ON r.post_uuid=c.post_uuid AND r.uuid=c.revision_uuid
ORDER BY selected.clock,selected.uuid`

func (s *SourceEvidenceStore) CurrentCaptures(ctx context.Context, post string, after *models.SourceCaptureCursor, limit int) ([]*models.SourceCapture, error) {
	post, err := archiveUUID(post)
	if err != nil {
		return nil, err
	}
	limit, err = sourcePageLimit(limit)
	if err != nil {
		return nil, err
	}
	afterTime, afterUUID := "", ""
	if after != nil {
		afterUUID, err = archiveUUID(after.UUID)
		clock := models.SourceCaptureInput{CapturedAt: after.CapturedAt, RecordedAt: after.RecordedAt}
		if err != nil || canonicalCaptureTime(&clock) != nil {
			return nil, models.ErrSourcePostBrowseInvalid
		}
		afterTime = clock.CapturedAt.Format(accountObservationTimeFormat)
		if clock.RecordedAt != nil {
			afterTime = clock.RecordedAt.Format(accountObservationTimeFormat)
		}
	}
	var rows []sourceCaptureRow
	if err := dbWrapper.Select(ctx, &rows, canonicalPostCapturesQuery, afterTime, afterUUID, limit, post, limit); err != nil {
		return nil, err
	}
	result := make([]*models.SourceCapture, 0, len(rows))
	for _, row := range rows {
		capture, err := row.resolve()
		if err != nil {
			return nil, err
		}
		result = append(result, capture)
	}
	return result, nil
}
