package sqlite

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

func validateEnrichmentDiscoverySchema(conn *sqlx.DB) error {
	for _, name := range []string{"automation_discovery_lookup", "enrichment_discovery_resolutions", "enrichment_discovery_resolution_immutable"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	for after := ""; ; {
		var row models.EnrichmentDiscoveryResolution
		err := conn.Get(&row, "SELECT * FROM enrichment_discovery_resolutions WHERE job_uuid>? ORDER BY job_uuid LIMIT 1", after)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := verifyDiscoveryResolution(conn.Get, conn.Select, row); err != nil {
			return err
		}
		after = row.JobUUID
	}
}

// Recheck the corroboration from retained native bytes even after staging was
// released. Publication validation separately proves the entire checkpoint.
func verifyDiscoveryResolution(get enrichmentGet, selectRows enrichmentSelect, row models.EnrichmentDiscoveryResolution) error {
	if !validSourceRunUUID(row.JobUUID) || !validSourceRunUUID(row.SnapshotUUID) || !validSourceRunUUID(row.PostUUID) ||
		row.SourceOrdinal < 1 || row.RecordOrdinal < 0 || row.PostRevision < 1 || row.CheckpointRevision < 1 ||
		row.Policy != scrape.DiscoveryIdentityPolicy || !validJobTime(row.CreatedAt) ||
		row.EvidenceUUID != uuid.NewSHA1(uuid.MustParse(row.JobUUID), []byte("discovery-post-identity-v1")).String() {
		return models.ErrSourcePayloadCorrupt
	}
	var evidence struct {
		Data      string `db:"data"`
		SHA       string `db:"data_sha256"`
		Candidate string `db:"candidate_url"`
		Capture   string `db:"capture_uuid"`
	}
	err := get(&evidence, `SELECT e.data,e.data_sha256,d.candidate_url,r.capture_uuid
 FROM automation_discovery_records d JOIN automation_snapshot_records e ON e.snapshot_uuid=d.snapshot_uuid AND e.ordinal=d.ordinal
 JOIN automation_discovery_imports i ON i.snapshot_uuid=d.snapshot_uuid
 JOIN enrichment_job_targets b ON b.job_uuid=? JOIN enrichment_targets t ON t.uuid=b.target_uuid
 JOIN source_post_urls u ON u.uuid=t.url_uuid
 JOIN enrichment_publications p ON p.job_uuid=b.job_uuid
 JOIN enrichment_checkpoint_receipts checkpoint ON checkpoint.job_uuid=p.job_uuid AND checkpoint.revision=p.checkpoint_revision
 JOIN enrichment_published_records r ON r.job_uuid=p.job_uuid AND r.ordinal=?
 JOIN source_post_identifiers id ON id.namespace=? AND id.value=? AND id.post_uuid=d.post_uuid
 JOIN source_posts post ON post.uuid=d.post_uuid
 WHERE d.snapshot_uuid=? AND d.ordinal=? AND e.source_table='discovery_targets' AND d.disposition='lookup' AND d.outcome='mapped'
 AND i.state!='running' AND d.post_uuid=? AND t.post_uuid=d.post_uuid AND t.collection_uuid=d.collection_uuid
 AND u.url=d.candidate_url AND p.checkpoint_revision=? AND post.revision>? AND p.created_at>=? AND checkpoint.created_at<=?`,
		row.JobUUID, row.RecordOrdinal, row.Namespace, row.Value, row.SnapshotUUID, row.SourceOrdinal, row.PostUUID, row.CheckpointRevision, row.PostRevision, row.CreatedAt.UTC(), row.CreatedAt.UTC())
	if err != nil {
		return models.ErrSourcePayloadCorrupt
	}
	if sourceDigest([]byte(evidence.Data)) != evidence.SHA {
		return models.ErrSourcePayloadCorrupt
	}
	object, err := archive.DecodeJSONObject([]byte(evidence.Data), scrape.CatalogChunkLimit)
	if err != nil {
		return err
	}
	values, ok := object["values"].(map[string]any)
	if !ok {
		return models.ErrSourcePayloadCorrupt
	}
	capture, err := findSourceCapture(get, selectRows, evidence.Capture)
	if err != nil {
		return err
	}
	if capture == nil || capture.PostUUID != row.PostUUID || capture.Origin != "gallery-dl" || capture.CapturedAt.IsZero() || capture.RecordedAt != nil || row.CreatedAt.Before(capture.CapturedAt) {
		return models.ErrSourcePayloadCorrupt
	}
	raw, err := archive.RestoreCapture(capture.Payload)
	if err != nil {
		return err
	}
	ref, basis, err := scrape.MatchDiscoveryLookup(values, raw)
	if err != nil || ref == nil || ref.Namespace != row.Namespace || ref.Value != row.Value || basis != row.Basis {
		return models.ErrSourcePayloadCorrupt
	}
	input, err := discoveryIdentifierInput(row, capture.CapturedAt)
	if err != nil {
		return err
	}
	if err := normalizePostLink(&input.SourcePostEvidence); err != nil {
		return err
	}
	digest, err := sourceSignature("stash-post-identifier-evidence-v1", input)
	if err != nil {
		return err
	}
	var link postLinkRow
	if err := get(&link, "SELECT * FROM source_post_identifier_evidence WHERE uuid=?", row.EvidenceUUID); err != nil {
		return err
	}
	if link.Digest != digest || link.PostUUID != row.PostUUID || link.Namespace != row.Namespace || link.Value != row.Value || link.Origin != input.Origin ||
		link.Basis != row.Basis || !link.ObservedAt.Timestamp.Equal(capture.CapturedAt) || link.Details != string(input.Details) {
		return models.ErrSourcePayloadCorrupt
	}
	return nil
}
