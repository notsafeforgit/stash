package sqlite

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

type CaptureContentCleanupResult struct {
	Captures           int
	Posts              int
	RevisionsRemoved   int
	PayloadsRemoved    int
	StoredBytesRemoved int64
	ProtectedCaptures  int
}

// CompactCaptureContent is explicit offline maintenance. Existing capture IDs
// continue to anchor media, profile, translation and edit choices; redundant post
// bodies and revisions are removed. Published enrichment proof inputs are left
// intact because they attest to exact bytes, not just post content.
func (db *Database) CompactCaptureContent(ctx context.Context, progress func(string, int)) (*CaptureContentCleanupResult, error) {
	result := &CaptureContentCleanupResult{}
	err := txn.WithTxn(ctx, db, func(ctx context.Context) error {
		var guard string
		if err := dbWrapper.Get(ctx, &guard, `SELECT sql FROM sqlite_schema WHERE name='source_capture_immutable' AND type='trigger'`); err != nil {
			return err
		}
		if guard == "" {
			return errors.New("capture immutable guard is missing")
		}
		for _, query := range []string{
			`DROP TRIGGER source_capture_immutable`,
			`CREATE TEMP TABLE compact_protected(uuid TEXT PRIMARY KEY) WITHOUT ROWID`,
			`INSERT OR IGNORE INTO compact_protected SELECT capture_uuid FROM source_capture_contexts UNION SELECT parent_capture_uuid FROM source_capture_contexts UNION SELECT capture_uuid FROM enrichment_published_records UNION SELECT capture_uuid FROM checkpoint_evidence_captures UNION SELECT capture_uuid FROM enrichment_completion_captures UNION SELECT legacy_capture_uuid FROM enrichment_completions WHERE legacy_capture_uuid IS NOT NULL UNION SELECT capture_uuid FROM discovery_published_records`,
			`CREATE TEMP TABLE compact_old_revisions(uuid TEXT PRIMARY KEY) WITHOUT ROWID`,
			`CREATE TEMP TABLE compact_old_payloads(digest TEXT PRIMARY KEY) WITHOUT ROWID`,
			`CREATE TEMP TABLE compact_posts(uuid TEXT PRIMARY KEY) WITHOUT ROWID`,
		} {
			if _, err := dbWrapper.Exec(ctx, query); err != nil {
				return err
			}
		}
		if err := dbWrapper.Get(ctx, &result.ProtectedCaptures, `SELECT count(*) FROM compact_protected p JOIN source_captures c ON c.uuid=p.uuid WHERE c.origin IN ('gallery-dl','gallery-dl-enrichment') AND c.retention_policy!='post-content-v1'`); err != nil {
			return err
		}
		evidence := &SourceEvidenceStore{}
		after := ""
		for {
			var ids []string
			if err := dbWrapper.Select(ctx, &ids, `SELECT c.uuid FROM source_captures c WHERE c.uuid>? AND c.origin IN ('gallery-dl','gallery-dl-enrichment') AND c.retention_policy IN ('legacy-retained-v1','gallery-dl-retained-v1') AND NOT EXISTS(SELECT 1 FROM compact_protected p WHERE p.uuid=c.uuid) ORDER BY c.uuid LIMIT 500`, after); err != nil {
				return err
			}
			if len(ids) == 0 {
				break
			}
			for _, id := range ids {
				capture, err := evidence.FindCapture(ctx, id)
				if err != nil {
					return fmt.Errorf("capture %s: %w", id, err)
				}
				input := models.SourceCaptureInput{UUID: id, PostUUID: capture.PostUUID, Origin: capture.Origin, Platform: capture.Platform, CapturedAt: capture.CapturedAt, RecordedAt: capture.RecordedAt, ExtractorVersion: capture.ExtractorVersion, Metadata: capture.Metadata, Payload: *capture.Payload}
				if err := compactCaptureInput(&input); err != nil {
					return err
				}
				metadata, revisionSig, err := canonicalCaptureInput(&input)
				if err != nil {
					return err
				}
				// Profile retention and references are independent of the post noise policy.
				if !reflect.DeepEqual(input.Payload.Refs, capture.Payload.Refs) {
					return fmt.Errorf("capture %s profile references changed", id)
				}
				bodyDigest, err := putSourcePayload(ctx, input.Payload.Shared)
				if err != nil {
					return err
				}
				patchDigest, err := putSourcePayload(ctx, input.Payload.Patch)
				if err != nil {
					return err
				}
				signature, err := captureSignature(input, revisionSig, patchDigest, input.Payload.Refs)
				if err != nil {
					return err
				}
				contentDigest, err := captureContentDigest(input, revisionSig)
				if err != nil {
					return err
				}
				if _, err := dbWrapper.Exec(ctx, `INSERT OR IGNORE INTO compact_old_revisions VALUES(?)`, capture.RevisionUUID); err != nil {
					return err
				}
				for _, digest := range []string{sourceDigest(capture.Payload.Shared), sourceDigest(capture.Payload.Patch)} {
					if _, err := dbWrapper.Exec(ctx, `INSERT OR IGNORE INTO compact_old_payloads VALUES(?)`, digest); err != nil {
						return err
					}
				}
				if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_post_revisions(uuid,post_uuid,signature,body_digest,metadata,structure_version) VALUES(?,?,?,?,?,?) ON CONFLICT(post_uuid,signature) DO NOTHING`, uuid.NewString(), capture.PostUUID, revisionSig, bodyDigest, metadata, archive.CaptureStructureVersion); err != nil {
					return err
				}
				var revision string
				if err := dbWrapper.Get(ctx, &revision, `SELECT uuid FROM source_post_revisions WHERE post_uuid=? AND signature=?`, capture.PostUUID, revisionSig); err != nil {
					return err
				}
				if _, err := dbWrapper.Exec(ctx, `UPDATE source_captures SET revision_uuid=?,retention_policy=?,patch_digest=?,signature=? WHERE uuid=?`, revision, archive.PostContentRetentionVersion, patchDigest, signature, id); err != nil {
					return err
				}
				if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_capture_content(capture_uuid,post_uuid,digest) VALUES(?,?,?)`, id, capture.PostUUID, contentDigest); err != nil {
					return err
				}
				if _, err := dbWrapper.Exec(ctx, `INSERT OR IGNORE INTO compact_posts VALUES(?)`, capture.PostUUID); err != nil {
					return err
				}
				result.Captures++
			}
			after = ids[len(ids)-1]
			if progress != nil {
				progress("captures", result.Captures)
			}
		}
		r, err := dbWrapper.Exec(ctx, `UPDATE source_posts SET revision=revision+1 WHERE uuid IN (SELECT uuid FROM compact_posts)`)
		if err != nil {
			return err
		}
		n, err := r.RowsAffected()
		if err != nil {
			return err
		}
		result.Posts = int(n)
		r, err = dbWrapper.Exec(ctx, `DELETE FROM source_post_revisions WHERE uuid IN (SELECT uuid FROM compact_old_revisions) AND NOT EXISTS(SELECT 1 FROM source_captures c WHERE c.revision_uuid=source_post_revisions.uuid)`)
		if err != nil {
			return err
		}
		n, err = r.RowsAffected()
		if err != nil {
			return err
		}
		result.RevisionsRemoved = int(n)
		// Restrict collection to replaced bodies and patches. Profiles or other
		// revisions that still refer to a blob keep it alive.
		if _, err := dbWrapper.Exec(ctx, `DELETE FROM compact_old_payloads WHERE EXISTS(SELECT 1 FROM source_captures c WHERE c.patch_digest=compact_old_payloads.digest) OR EXISTS(SELECT 1 FROM source_post_revisions r WHERE r.body_digest=compact_old_payloads.digest) OR EXISTS(SELECT 1 FROM source_profile_bodies p WHERE p.payload_digest=compact_old_payloads.digest)`); err != nil {
			return err
		}
		if err := dbWrapper.Get(ctx, &result.StoredBytesRemoved, `SELECT coalesce(sum(length(data)),0) FROM source_payloads WHERE digest IN (SELECT digest FROM compact_old_payloads)`); err != nil {
			return err
		}
		r, err = dbWrapper.Exec(ctx, `DELETE FROM source_payloads WHERE digest IN (SELECT digest FROM compact_old_payloads)`)
		if err != nil {
			return err
		}
		n, err = r.RowsAffected()
		if err != nil {
			return err
		}
		result.PayloadsRemoved = int(n)
		if _, err := dbWrapper.Exec(ctx, guard); err != nil {
			return err
		}
		for _, table := range []string{"compact_protected", "compact_old_revisions", "compact_old_payloads", "compact_posts"} {
			if _, err := dbWrapper.Exec(ctx, "DROP TABLE temp."+table); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
