package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sourceadapter"
	"github.com/stashapp/stash/pkg/txn"
)

// CompactPostText discards obsolete caption copies, including imported metadata
// and bodies already processed by CompactCaptureContent. Capture IDs continue to
// identify attachment/profile evidence, not separate versions of the caption.
// It is offline maintenance, with all replacements and blob collection atomic.
func (db *Database) CompactPostText(ctx context.Context, progress func(string, int)) (*CaptureContentCleanupResult, error) {
	result := &CaptureContentCleanupResult{}
	err := txn.WithTxn(ctx, db, func(ctx context.Context) error {
		var guard string
		if err := dbWrapper.Get(ctx, &guard, `SELECT sql FROM sqlite_schema WHERE name='source_capture_immutable' AND type='trigger'`); err != nil {
			return err
		}
		for _, query := range []string{
			`DROP TRIGGER source_capture_immutable`,
			`CREATE TEMP TABLE compact_protected(uuid TEXT PRIMARY KEY) WITHOUT ROWID`,
			`INSERT OR IGNORE INTO compact_protected SELECT capture_uuid FROM source_capture_contexts UNION SELECT parent_capture_uuid FROM source_capture_contexts UNION SELECT capture_uuid FROM enrichment_published_records UNION SELECT capture_uuid FROM checkpoint_evidence_captures UNION SELECT capture_uuid FROM enrichment_completion_captures UNION SELECT legacy_capture_uuid FROM enrichment_completions WHERE legacy_capture_uuid IS NOT NULL UNION SELECT capture_uuid FROM discovery_published_records UNION SELECT capture_uuid FROM capture_translation_entries`,
			`CREATE TEMP TABLE compact_old_revisions(uuid TEXT PRIMARY KEY) WITHOUT ROWID`,
			`CREATE TEMP TABLE compact_old_payloads(digest TEXT PRIMARY KEY) WITHOUT ROWID`,
			`CREATE TEMP TABLE compact_posts(uuid TEXT PRIMARY KEY) WITHOUT ROWID`,
			`CREATE TEMP TABLE compact_text_candidates(uuid TEXT PRIMARY KEY) WITHOUT ROWID`,
			`INSERT INTO compact_text_candidates SELECT post_uuid FROM source_post_revisions GROUP BY post_uuid HAVING count(DISTINCT metadata)>1`,
		} {
			if _, err := dbWrapper.Exec(ctx, query); err != nil {
				return err
			}
		}
		evidence := &SourceEvidenceStore{}
		after := ""
		for {
			var posts []string
			if err := dbWrapper.Select(ctx, &posts, `SELECT uuid FROM compact_text_candidates WHERE uuid>? ORDER BY uuid LIMIT 100`, after); err != nil {
				return err
			}
			if len(posts) == 0 {
				break
			}
			for _, post := range posts {
				if err := compactPostText(ctx, evidence, post, result); err != nil {
					return fmt.Errorf("post %s: %w", post, err)
				}
			}
			after = posts[len(posts)-1]
			if progress != nil {
				progress("caption copies", result.Captures)
			}
		}
		if _, err := dbWrapper.Exec(ctx, `DROP TABLE temp.compact_text_candidates`); err != nil {
			return err
		}
		return finishCaptureContentCleanup(ctx, guard, result)
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func compactPostText(ctx context.Context, evidence *SourceEvidenceStore, post string, result *CaptureContentCleanupResult) error {
	metadata, err := evidence.PostContent(ctx, post)
	if err != nil || metadata == nil {
		return err
	}
	var ids []string
	if err := dbWrapper.Select(ctx, &ids, `SELECT uuid FROM source_captures INDEXED BY source_captures_order WHERE post_uuid=? ORDER BY coalesce(captured_at,recorded_at) DESC,uuid DESC`, post); err != nil {
		return err
	}
	// Use the latest source text before translation. NFO display text may have
	// been translated; it cannot replace the original obtained from the service.
	var saved json.RawMessage
	for _, id := range ids {
		var source bool
		if err := dbWrapper.Get(ctx, &source, `SELECT origin IN ('gallery-dl','gallery-dl-enrichment') FROM source_captures WHERE uuid=?`, id); err != nil {
			return err
		}
		if !source {
			continue
		}
		capture, err := evidence.FindCapture(ctx, id)
		if err != nil {
			return err
		}
		raw, err := archive.RestoreCapture(capture.Payload)
		if err != nil {
			return err
		}
		if saved != nil {
			var matched bool
			raw, matched, err = sourceadapter.PreservePostText(raw, saved)
			if err != nil {
				return err
			}
			if !matched {
				continue
			}
		}
		saved = raw
	}
	if saved != nil {
		original, err := archive.CapturedMetadata(saved)
		if err != nil {
			return err
		}
		if original.Title != nil {
			metadata.Title = original.Title
		}
		if original.OriginalText != nil {
			metadata.OriginalText = original.OriginalText
		}
		if original.Language != nil {
			metadata.Language = original.Language
		}
	}
	for _, id := range ids {
		var protected bool
		if err := dbWrapper.Get(ctx, &protected, `SELECT EXISTS(SELECT 1 FROM compact_protected WHERE uuid=?)`, id); err != nil {
			return err
		}
		if protected {
			result.ProtectedCaptures++
			continue
		}
		capture, err := evidence.FindCapture(ctx, id)
		if err != nil {
			return err
		}
		input := models.SourceCaptureInput{UUID: id, PostUUID: post, Origin: capture.Origin, Platform: capture.Platform, CapturedAt: capture.CapturedAt, RecordedAt: capture.RecordedAt, ExtractorVersion: capture.ExtractorVersion, Metadata: *metadata, Payload: *capture.Payload, RetentionPolicy: capture.RetentionPolicy}
		switch capture.Origin {
		case "gallery-dl", "gallery-dl-enrichment":
			if saved == nil {
				continue
			}
			raw, err := archive.RestoreCapture(capture.Payload)
			if err != nil {
				return err
			}
			raw, matched, err := sourceadapter.PreservePostText(raw, saved)
			if err != nil {
				return err
			}
			if !matched {
				// An HTML caption can also be the only description of an
				// attachment. That is meaningful evidence, not a text version.
				result.ProtectedCaptures++
				continue
			}
			payload, err := archive.PrepareRetainedCapture(input.Origin, input.Platform, raw)
			if err != nil {
				return err
			}
			input.Payload = *payload
			if err := compactCaptureInput(&input); err != nil {
				return err
			}
		case "legacy-nfo", "imported-metadata", "derived-twitter-id":
			// These captures retain projected fields, not raw post text.
			if capture.RetentionPolicy != importedMetadataPolicy && capture.Origin != "derived-twitter-id" {
				result.ProtectedCaptures++
				continue
			}
		default:
			continue
		}
		if reflect.DeepEqual(input.Metadata, capture.Metadata) && reflect.DeepEqual(input.Payload, *capture.Payload) && input.RetentionPolicy == capture.RetentionPolicy {
			continue
		}
		if !reflect.DeepEqual(input.Payload.Refs, capture.Payload.Refs) {
			return fmt.Errorf("capture %s profile references changed", id)
		}
		if err := replaceCaptureContent(ctx, capture, input, result); err != nil {
			return err
		}
	}
	return nil
}

func replaceCaptureContent(ctx context.Context, capture *models.SourceCapture, input models.SourceCaptureInput, result *CaptureContentCleanupResult) error {
	metadata, revisionSig, err := canonicalCaptureInput(&input)
	if err != nil {
		return err
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
	if _, err := dbWrapper.Exec(ctx, `INSERT OR IGNORE INTO compact_old_revisions VALUES(?)`, capture.RevisionUUID); err != nil {
		return err
	}
	for _, digest := range []string{sourceDigest(capture.Payload.Shared), sourceDigest(capture.Payload.Patch)} {
		if _, err := dbWrapper.Exec(ctx, `INSERT OR IGNORE INTO compact_old_payloads VALUES(?)`, digest); err != nil {
			return err
		}
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_post_revisions(uuid,post_uuid,signature,body_digest,metadata,structure_version) VALUES(?,?,?,?,?,?) ON CONFLICT(post_uuid,signature) DO NOTHING`, uuid.NewString(), input.PostUUID, revisionSig, bodyDigest, metadata, archive.CaptureStructureVersion); err != nil {
		return err
	}
	var revision string
	if err := dbWrapper.Get(ctx, &revision, `SELECT uuid FROM source_post_revisions WHERE post_uuid=? AND signature=?`, input.PostUUID, revisionSig); err != nil {
		return err
	}
	if _, err := dbWrapper.Exec(ctx, `UPDATE source_captures SET revision_uuid=?,retention_policy=?,patch_digest=?,signature=? WHERE uuid=?`, revision, input.RetentionPolicy, patchDigest, signature, input.UUID); err != nil {
		return err
	}
	if input.RetentionPolicy == archive.PostContentRetentionVersion {
		digest, err := captureContentDigest(input, revisionSig)
		if err != nil {
			return err
		}
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_capture_content(capture_uuid,post_uuid,digest) VALUES(?,?,?) ON CONFLICT(capture_uuid) DO UPDATE SET digest=excluded.digest`, input.UUID, input.PostUUID, digest); err != nil {
			return err
		}
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT OR IGNORE INTO compact_posts VALUES(?)`, input.PostUUID); err != nil {
		return err
	}
	result.Captures++
	return nil
}
