package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

func compactCaptureInput(input *models.SourceCaptureInput) error {
	if len(input.Contexts) != 0 {
		return errors.New("context captures require their retained evidence")
	}
	raw, err := archive.RestoreCapture(&input.Payload)
	if err != nil {
		return err
	}
	raw, err = archive.RetainPostContent(raw)
	if err != nil {
		return err
	}
	payload, err := archive.PrepareRetainedCapture(input.Origin, input.Platform, raw)
	if err != nil {
		return err
	}
	input.Payload = *payload
	input.RetentionPolicy = archive.PostContentRetentionVersion
	return nil
}

// Unlike the capture signature this key deliberately excludes the observation
// clock and extractor version. Actual content, profiles and attachment identity
// still distinguish captures. Producer receipts handle event replay separately.
func captureContentDigest(input models.SourceCaptureInput, revision string) (string, error) {
	return sourceSignature("stash-capture-content-v1", []any{input.PostUUID, revision, input.Origin, input.Platform, sourceDigest(input.Payload.Patch), input.Payload.Refs})
}

// RecordContentCapture is for authenticated, validated HTTP producer events.
// The caller must check its durable event receipt in the same transaction first;
// this method counts a new event, not a transport retry. Immutable enrichment
// checkpoints continue to use RecordCapture and their original evidence policy.
func (s *SourceEvidenceStore) RecordContentCapture(ctx context.Context, input models.SourceCaptureInput) (*models.SourceCapture, error) {
	if input.CapturedAt.IsZero() {
		return nil, errors.New("repeat sightings require an observation time")
	}
	if err := compactCaptureInput(&input); err != nil {
		return nil, err
	}
	_, revision, err := canonicalCaptureInput(&input)
	if err != nil {
		return nil, err
	}
	digest, err := captureContentDigest(input, revision)
	if err != nil {
		return nil, err
	}
	post, err := s.FindPost(ctx, input.PostUUID)
	if err != nil {
		return nil, err
	}
	if post == nil {
		return nil, models.ErrSourcePostConflict
	}
	if post.State != "active" {
		return nil, models.ErrSourcePostForgotten
	}
	var id string
	err = dbWrapper.Get(ctx, &id, `SELECT c.uuid FROM source_capture_content x JOIN source_captures c ON c.uuid=x.capture_uuid WHERE x.post_uuid=? AND x.digest=? AND c.captured_at IS NOT NULL
 AND c.revision_uuid=(SELECT revision_uuid FROM source_captures WHERE post_uuid=c.post_uuid ORDER BY coalesce(captured_at,recorded_at) DESC,uuid DESC LIMIT 1)
 AND NOT EXISTS(SELECT 1 FROM source_captures newer LEFT JOIN source_capture_content nc ON nc.capture_uuid=newer.uuid WHERE newer.post_uuid=c.post_uuid AND newer.patch_digest=c.patch_digest AND newer.captured_at>c.captured_at AND nc.digest IS NOT x.digest)
 ORDER BY c.captured_at DESC,c.uuid DESC LIMIT 1`, input.PostUUID, digest)
	if errors.Is(err, sql.ErrNoRows) {
		return s.RecordCapture(ctx, input)
	}
	if err != nil {
		return nil, err
	}
	capture, err := s.FindCapture(ctx, id)
	if err != nil {
		return nil, err
	}
	first, last := capture.CapturedAt, input.CapturedAt
	if last.Before(first) {
		first, last = last, first
	}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO source_capture_sightings(capture_uuid,count,first_seen,last_seen) VALUES(?,2,?,?)
 ON CONFLICT(capture_uuid) DO UPDATE SET count=count+1,first_seen=min(first_seen,excluded.first_seen),last_seen=max(last_seen,excluded.last_seen)`, id, first.Format(accountObservationTimeFormat), last.Format(accountObservationTimeFormat))
	return capture, err
}

// History pages revisions, not individual per-file scrape records, and includes
// all observations of each version even when there are more than a page of them.
func (s *SourceEvidenceStore) CaptureHistory(ctx context.Context, post, after string, limit int) ([]models.SourceCaptureHistory, error) {
	post, after, limit, err := postLinkPage(post, after, limit)
	if err != nil {
		return nil, err
	}
	var rows []struct {
		UUID     string         `db:"uuid"`
		Metadata string         `db:"metadata"`
		Count    int            `db:"count"`
		Unknown  int            `db:"unknown_count"`
		First    sql.NullString `db:"first_seen"`
		Last     sql.NullString `db:"last_seen"`
	}
	err = dbWrapper.Select(ctx, &rows, `WITH revisions AS (
 SELECT r.uuid,r.metadata FROM source_post_identities i JOIN source_post_revisions r ON r.post_uuid=i.post_uuid
 WHERE i.canonical_uuid=(SELECT canonical_uuid FROM source_post_identities WHERE post_uuid=?) AND r.uuid>?
 AND EXISTS(SELECT 1 FROM source_captures c WHERE c.revision_uuid=r.uuid)
 ORDER BY r.uuid LIMIT ?
 ) SELECT r.uuid,r.metadata,sum(coalesce(s.count,1)) AS count,
 sum(CASE WHEN c.captured_at IS NULL THEN 1 ELSE 0 END) AS unknown_count,
 min(coalesce(s.first_seen,c.captured_at)) AS first_seen,max(coalesce(s.last_seen,c.captured_at)) AS last_seen
 FROM revisions r JOIN source_captures c ON c.revision_uuid=r.uuid
 LEFT JOIN source_capture_sightings s ON s.capture_uuid=c.uuid GROUP BY r.uuid ORDER BY r.uuid`, post, after, limit)
	if err != nil {
		return nil, err
	}
	result := make([]models.SourceCaptureHistory, 0, len(rows))
	for _, row := range rows {
		item := models.SourceCaptureHistory{UUID: row.UUID, Count: row.Count, UnknownCount: row.Unknown}
		for _, clock := range []struct {
			value sql.NullString
			dest  **time.Time
		}{{row.First, &item.FirstSeen}, {row.Last, &item.LastSeen}} {
			if clock.value.Valid {
				value, err := time.Parse(time.RFC3339Nano, clock.value.String)
				if err != nil {
					return nil, err
				}
				*clock.dest = &value
			}
		}
		if err := json.Unmarshal([]byte(row.Metadata), &item.Metadata); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}
