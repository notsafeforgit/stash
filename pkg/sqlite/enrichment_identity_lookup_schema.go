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

// enrichmentIdentityLookup is one retained historical finder row. Import
// retirement copied these rows out of the frozen automation input so pending
// enrichment of legacy-only posts can still corroborate a candidate's identity.
type enrichmentIdentityLookup struct {
	ID             int64  `db:"id"`
	PostUUID       string `db:"post_uuid"`
	CollectionUUID string `db:"collection_uuid"`
	URL            string `db:"url"`
	SourceValues   string `db:"source_values"`
}

func (l enrichmentIdentityLookup) values() (map[string]any, error) {
	values, err := archive.DecodeJSONObject([]byte(l.SourceValues), 16<<20)
	if err != nil {
		return nil, models.ErrSourcePayloadCorrupt
	}
	if candidate, ok := scrape.LookupCandidateURL(values); !ok || candidate != l.URL {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return values, nil
}

func validateEnrichmentIdentitySchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"enrichment_identity_lookups", "enrichment_identity_lookup_target", "enrichment_identity_lookup_immutable",
		"enrichment_identity_resolutions", "enrichment_identity_resolution_immutable"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	if !auditData {
		return nil
	}
	for after := int64(0); ; {
		var row enrichmentIdentityLookup
		err := conn.Get(&row, "SELECT * FROM enrichment_identity_lookups WHERE id>? ORDER BY id LIMIT 1", after)
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		if err != nil {
			return err
		}
		if !validSourceRunUUID(row.PostUUID) || !validSourceRunUUID(row.CollectionUUID) {
			return models.ErrSourcePayloadCorrupt
		}
		if _, err := row.values(); err != nil {
			return err
		}
		after = row.ID
	}
	for after := ""; ; {
		var row models.EnrichmentIdentityResolution
		err := conn.Get(&row, "SELECT * FROM enrichment_identity_resolutions WHERE job_uuid>? ORDER BY job_uuid LIMIT 1", after)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := verifyIdentityResolution(conn.Get, conn.Select, row); err != nil {
			return err
		}
		after = row.JobUUID
	}
}

// Recheck the corroboration from retained native bytes. Publication validation
// separately proves the entire checkpoint.
func verifyIdentityResolution(get enrichmentGet, selectRows enrichmentSelect, row models.EnrichmentIdentityResolution) error {
	if !validSourceRunUUID(row.JobUUID) || !validSourceRunUUID(row.PostUUID) || row.LookupID < 1 ||
		row.RecordOrdinal < 0 || row.PostRevision < 1 || row.CheckpointRevision < 1 ||
		row.Policy != scrape.DiscoveryIdentityPolicy || !validJobTime(row.CreatedAt) ||
		row.EvidenceUUID != uuid.NewSHA1(uuid.MustParse(row.JobUUID), []byte("discovery-post-identity-v1")).String() {
		return models.ErrSourcePayloadCorrupt
	}
	var evidence struct {
		enrichmentIdentityLookup
		Capture string `db:"capture_uuid"`
	}
	err := get(&evidence, `SELECT l.*,r.capture_uuid FROM enrichment_identity_lookups l
 JOIN enrichment_job_targets b ON b.job_uuid=? JOIN enrichment_targets t ON t.uuid=b.target_uuid
 JOIN source_post_urls u ON u.uuid=t.url_uuid
 JOIN enrichment_publications p ON p.job_uuid=b.job_uuid
 JOIN enrichment_checkpoint_receipts checkpoint ON checkpoint.job_uuid=p.job_uuid AND checkpoint.revision=p.checkpoint_revision
 JOIN enrichment_published_records r ON r.job_uuid=p.job_uuid AND r.ordinal=?
 JOIN source_post_identifiers id ON id.namespace=? AND id.value=? AND id.post_uuid=l.post_uuid
 JOIN source_posts post ON post.uuid=l.post_uuid
 WHERE l.id=? AND l.post_uuid=? AND t.post_uuid=l.post_uuid AND t.collection_uuid=l.collection_uuid
 AND u.url=l.url AND p.checkpoint_revision=? AND post.revision>? AND p.created_at>=? AND checkpoint.created_at<=?`,
		row.JobUUID, row.RecordOrdinal, row.Namespace, row.Value, row.LookupID, row.PostUUID, row.CheckpointRevision, row.PostRevision, row.CreatedAt.UTC(), row.CreatedAt.UTC())
	if err != nil {
		return models.ErrSourcePayloadCorrupt
	}
	values, err := evidence.values()
	if err != nil {
		return err
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
	input, err := lookupIdentifierInput(row, capture.CapturedAt)
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
