package sqlite

import (
	"fmt"
	"reflect"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

func validateCheckpointEvidenceSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"checkpoint_evidence_acceptances", "checkpoint_evidence_target", "checkpoint_evidence_acceptance_immutable",
		"checkpoint_evidence_acceptance_scope", "checkpoint_evidence_captures", "checkpoint_evidence_capture",
		"checkpoint_evidence_capture_immutable", "checkpoint_evidence_capture_scope"} {
		var exists bool
		if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	if !auditData {
		return nil
	}
	for after := ""; ; {
		var id string
		if err := conn.Get(&id, "SELECT coalesce(min(uuid),'') FROM checkpoint_evidence_acceptances WHERE uuid>?", after); err != nil || id == "" {
			return err
		}
		if err := validateCheckpointEvidence(conn, id); err != nil {
			return err
		}
		after = id
	}
}

func validateCheckpointEvidence(conn *sqlx.DB, id string) error {
	var row checkpointEvidenceRow
	if err := conn.Get(&row, "SELECT * FROM checkpoint_evidence_acceptances WHERE uuid=?", id); err != nil {
		return err
	}
	value, err := row.decode()
	if err != nil {
		return err
	}
	var source struct {
		Data    string `db:"data"`
		SHA256  string `db:"data_sha256"`
		Updated string `db:"updated_at"`
	}
	if err := conn.Get(&source, `SELECT r.data,r.data_sha256,p.updated_at FROM automation_snapshot_records r
 JOIN automation_checkpoint_imports p ON p.snapshot_uuid=r.snapshot_uuid
 WHERE r.snapshot_uuid=? AND r.ordinal=? AND p.manifest_sha256=? AND p.state!='running'`,
		row.SnapshotUUID, row.Ordinal, value.Input.ManifestSHA256); err != nil {
		return err
	}
	updated, err := time.Parse(time.RFC3339Nano, source.Updated)
	if err != nil || value.CreatedAt.Before(updated) || validateEnrichmentTarget(&value.Target) != nil ||
		value.Target.State != "review" || value.Target.Reason != "legacy_checkpoint_conversion" || value.CreatedAt.Before(value.Target.UpdatedAt) {
		return models.ErrSourcePayloadCorrupt
	}
	expected, body, err := prepareAutomationCheckpoint(catalogEvidenceRow{Ordinal: row.Ordinal, Data: source.Data, SHA256: source.SHA256})
	if err != nil || expected == nil || expected.Outcome != "mapped" || expected.SourceSHA256 != value.SourceSHA256 ||
		*expected.StagedSHA256 != value.StagedSHA256 || *expected.BodySHA256 != value.BodySHA256 ||
		expected.RecordCount != value.RecordCount || expected.PendingCount != value.PendingCount || expected.UnresolvedCount != value.UnscopedCount {
		return models.ErrSourcePayloadCorrupt
	}
	var bound bool
	if err := conn.Get(&bound, `SELECT EXISTS(SELECT 1 FROM automation_enrichment_records r
 JOIN enrichment_targets t ON t.uuid=r.target_uuid
 JOIN enrichment_target_history h ON h.target_uuid=t.uuid AND h.revision=r.target_revision
 JOIN source_posts p ON p.uuid=t.post_uuid JOIN source_post_urls u ON u.uuid=t.url_uuid
 WHERE r.snapshot_uuid=? AND r.ordinal=? AND r.disposition='staged_review'
 AND r.target_uuid=? AND r.target_revision=? AND t.post_uuid=? AND p.revision>=? AND ?>0
 AND t.collection_uuid=? AND t.collection_revision=? AND t.url_uuid=? AND u.url=?
 AND t.policy=? AND t.origin=? AND h.state='review' AND h.reason='legacy_checkpoint_conversion'
 AND h.priority=? AND h.not_before=? AND h.recorded_at=? AND t.created_at=?)`,
		row.SnapshotUUID, row.Ordinal, row.TargetUUID, row.TargetRevision, value.Target.PostUUID, value.PostRevision, value.PostRevision,
		value.Target.CollectionUUID, value.Target.CollectionRevision, value.Target.URLUUID, value.Target.URL,
		value.Target.Policy, value.Target.Origin, value.Target.Priority, value.Target.NotBefore, value.Target.UpdatedAt, value.Target.CreatedAt); err != nil {
		return err
	}
	if !bound {
		return fmt.Errorf("checkpoint evidence source binding: %w", models.ErrSourcePayloadCorrupt)
	}
	staging, err := scrape.DecodeStaging(body)
	if err != nil {
		return err
	}
	captures, err := scrape.PrepareStagedCaptures(row.SnapshotUUID, row.Ordinal, value.Target.PostUUID, staging, value.CreatedAt)
	if err != nil {
		return err
	}
	var bindings []models.CheckpointEvidenceCapture
	if err := conn.Select(&bindings, `SELECT body_index,capture_uuid,payload_sha256 FROM checkpoint_evidence_captures
 WHERE acceptance_uuid=? ORDER BY body_index`, id); err != nil {
		return err
	}
	if len(captures) != len(bindings) || !reflect.DeepEqual(bindings, value.Captures) {
		return fmt.Errorf("checkpoint evidence body bindings: %w", models.ErrSourcePayloadCorrupt)
	}
	for index, input := range captures {
		binding := bindings[index]
		if binding.BodyIndex != index || binding.CaptureUUID != input.UUID {
			return models.ErrSourcePayloadCorrupt
		}
		expectedRaw, err := archive.RestoreCapture(&input.Payload)
		if err != nil || sourceDigest(expectedRaw) != binding.PayloadSHA {
			return models.ErrSourcePayloadCorrupt
		}
		capture, err := findSourceCapture(conn.Get, conn.Select, input.UUID)
		if err != nil {
			return err
		}
		if capture == nil || capture.PostUUID != input.PostUUID || capture.Origin != input.Origin || capture.Platform != input.Platform ||
			!capture.CapturedAt.IsZero() || capture.RecordedAt == nil || !capture.RecordedAt.Equal(value.CreatedAt) ||
			!reflect.DeepEqual(capture.ExtractorVersion, input.ExtractorVersion) || capture.RetentionPolicy != input.RetentionPolicy ||
			!reflect.DeepEqual(capture.Metadata, input.Metadata) {
			return fmt.Errorf("checkpoint evidence capture provenance: %w", models.ErrSourcePayloadCorrupt)
		}
		raw, err := archive.RestoreCapture(capture.Payload)
		if err != nil || sourceDigest(raw) != binding.PayloadSHA {
			return models.ErrSourcePayloadCorrupt
		}
		reference, err := archive.ExtractCapturedPost(raw)
		if err != nil || reference == nil || reference.Namespace != value.PostNamespace || reference.Value != value.PostValue {
			return models.ErrSourcePayloadCorrupt
		}
		if err := conn.Get(&bound, `SELECT EXISTS(SELECT 1 FROM source_post_identifiers WHERE post_uuid=? AND namespace=? AND value=?)
 AND EXISTS(SELECT 1 FROM source_collection_captures WHERE capture_uuid=? AND collection_uuid=? AND collection_revision=?)`,
			input.PostUUID, reference.Namespace, reference.Value, input.UUID, value.Target.CollectionUUID, value.Target.CollectionRevision); err != nil {
			return err
		}
		if !bound {
			return models.ErrSourcePayloadCorrupt
		}
	}
	return nil
}
