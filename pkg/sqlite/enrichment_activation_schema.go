package sqlite

import (
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

type enrichmentActivationRecord struct {
	UUID           string    `db:"uuid"`
	InputSHA256    string    `db:"input_sha256"`
	PlanSHA256     string    `db:"plan_sha256"`
	SnapshotUUID   *string   `db:"snapshot_uuid"`
	ManifestSHA256 *string   `db:"manifest_sha256"`
	Plan           string    `db:"plan"`
	CreatedAt      time.Time `db:"created_at"`
}

func (r enrichmentActivationRecord) decode() (*models.EnrichmentActivationPlan, error) {
	plan, err := archive.DecodeEnrichmentActivationPlan([]byte(r.Plan))
	if err != nil || plan.Input.UUID != r.UUID || plan.PlanSHA256 != r.PlanSHA256 || !validJobTime(r.CreatedAt) ||
		(r.SnapshotUUID == nil) != (r.ManifestSHA256 == nil) || (r.SnapshotUUID == nil) != (plan.Input.SnapshotUUID == "") {
		return nil, models.ErrSourcePayloadCorrupt
	}
	_, inputSHA, err := archive.PrepareEnrichmentActivation(plan.Input)
	if err != nil || inputSHA != r.InputSHA256 || (r.SnapshotUUID != nil &&
		(*r.SnapshotUUID != plan.Input.SnapshotUUID || *r.ManifestSHA256 != plan.Input.ManifestSHA256)) {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return plan, nil
}

func validateEnrichmentActivationSchema(conn *sqlx.DB) error {
	for _, object := range []struct{ name, kind string }{
		{"enrichment_activations", "table"}, {"enrichment_activation_targets", "table"},
		{"enrichment_activation_immutable", "trigger"}, {"enrichment_activation_target_immutable", "trigger"},
		{"automation_enrichment_held", "index"}, {"automation_enrichment_held_targets", "index"},
	} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=? AND type=?)", object.name, object.kind); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", object.name)
		}
	}
	var invalid bool
	if err := conn.Get(&invalid, `SELECT NOT EXISTS(SELECT 1 FROM pragma_table_info('enrichment_activations') WHERE name='created_at' AND upper(type)='DATETIME')
 OR EXISTS(SELECT 1 FROM enrichment_activations a LEFT JOIN automation_enrichment_imports i ON i.snapshot_uuid=a.snapshot_uuid
 WHERE (a.snapshot_uuid IS NOT NULL AND (i.state IS NULL OR i.state='running' OR i.manifest_sha256 IS NOT a.manifest_sha256))
 OR json_type(a.plan,'$.entries') IS NOT 'array' OR coalesce(json_array_length(a.plan,'$.entries'),0) NOT BETWEEN 1 AND 100
 OR json_array_length(a.plan,'$.entries')!=(SELECT count(*) FROM enrichment_activation_targets x WHERE x.activation_uuid=a.uuid))
 OR EXISTS(SELECT 1 FROM enrichment_activation_targets x
 LEFT JOIN enrichment_activations a ON a.uuid=x.activation_uuid
 LEFT JOIN enrichment_targets t ON t.uuid=x.target_uuid
 LEFT JOIN enrichment_target_history old ON old.target_uuid=x.target_uuid AND old.revision=x.previous_revision
 LEFT JOIN enrichment_target_history consumed ON consumed.target_uuid=x.target_uuid AND consumed.revision=x.consumed_revision
 LEFT JOIN enrichment_target_history next ON next.target_uuid=x.released_target_uuid AND next.revision=x.released_revision
 WHERE a.uuid IS NULL OR t.uuid IS NULL OR x.consumed_revision!=x.previous_revision+1
 OR old.state IS NOT 'held' OR next.state IS NOT 'pending'
 OR consumed.recorded_at IS NOT a.created_at OR consumed.priority IS NOT old.priority OR consumed.not_before IS NOT old.not_before
 OR (x.released_target_uuid=x.target_uuid AND (consumed.state IS NOT 'pending' OR consumed.reason IS NOT ''))
 OR (x.released_target_uuid!=x.target_uuid AND (consumed.state IS NOT 'excluded' OR consumed.reason IS NOT 'activation_rebound' OR x.released_revision!=1))
 OR next.reason IS NOT '' OR old.priority IS NOT next.priority OR old.not_before IS NOT next.not_before OR next.recorded_at IS NOT a.created_at
 OR (a.snapshot_uuid IS NOT NULL AND NOT EXISTS(SELECT 1 FROM automation_enrichment_records r
  WHERE r.snapshot_uuid=a.snapshot_uuid AND r.target_uuid=x.target_uuid AND r.target_revision=x.previous_revision AND r.disposition='held')))
 OR EXISTS(SELECT 1 FROM enrichment_activations a,json_each(a.plan,'$.entries') e
 LEFT JOIN enrichment_activation_targets x ON x.activation_uuid=a.uuid AND x.target_uuid=json_extract(e.value,'$.target_uuid')
 LEFT JOIN enrichment_targets t ON t.uuid=x.target_uuid
 LEFT JOIN enrichment_target_history h ON h.target_uuid=x.target_uuid AND h.revision=x.previous_revision
 LEFT JOIN enrichment_targets released ON released.uuid=x.released_target_uuid
 LEFT JOIN source_collection_revisions d ON d.collection_uuid=released.collection_uuid AND d.revision=released.collection_revision
 LEFT JOIN source_post_urls u ON u.uuid=t.url_uuid LEFT JOIN source_posts p ON p.uuid=t.post_uuid
 WHERE x.target_uuid IS NULL OR x.previous_revision IS NOT json_extract(e.value,'$.revision')
 OR t.post_uuid IS NOT json_extract(e.value,'$.post_uuid') OR p.revision<json_extract(e.value,'$.post_revision')
 OR t.url_uuid IS NOT json_extract(e.value,'$.url_uuid') OR u.url IS NOT json_extract(e.value,'$.url')
 OR t.collection_uuid IS NOT json_extract(e.value,'$.collection_uuid') OR t.collection_revision IS NOT json_extract(e.value,'$.collection_revision')
 OR d.state IS NOT 'active' OR released.uuid IS NOT json_extract(e.value,'$.released_target_uuid') OR x.released_revision IS NOT json_extract(e.value,'$.released_revision')
 OR released.collection_revision IS NOT json_extract(e.value,'$.activation_collection_revision')
 OR released.collection_uuid IS NOT t.collection_uuid OR released.post_uuid IS NOT t.post_uuid OR released.url_uuid IS NOT t.url_uuid OR released.policy IS NOT t.policy
 OR t.policy IS NOT json_extract(e.value,'$.policy') OR h.priority IS NOT json_extract(e.value,'$.priority'))`); err != nil {
		return err
	}
	if invalid {
		return models.ErrSourcePayloadCorrupt
	}
	// Read each receipt once. Historical holds and releases remain valid after a
	// worker completes a target, someone reschedules it, or a post is forgotten.
	rows, err := conn.Queryx(`SELECT a.uuid,a.input_sha256,a.plan_sha256,a.snapshot_uuid,a.manifest_sha256,a.created_at,
 CASE WHEN e.key=0 THEN a.plan ELSE '' END AS plan,e.key AS entry_index,h.not_before AS entry_not_before
 FROM enrichment_activations a,json_each(a.plan,'$.entries') e
 JOIN enrichment_target_history h ON h.target_uuid=json_extract(e.value,'$.target_uuid') AND h.revision=json_extract(e.value,'$.revision')
 ORDER BY a.uuid,e.key`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var current *models.EnrichmentActivationPlan
	for rows.Next() {
		var row struct {
			enrichmentActivationRecord
			Index     int       `db:"entry_index"`
			NotBefore time.Time `db:"entry_not_before"`
		}
		if err := rows.StructScan(&row); err != nil {
			return err
		}
		if current == nil || current.Input.UUID != row.UUID {
			current, err = row.decode()
			if err != nil {
				return err
			}
		}
		if row.Index < 0 || row.Index >= len(current.Entries) || !row.NotBefore.Equal(current.Entries[row.Index].NotBefore) {
			return models.ErrSourcePayloadCorrupt
		}
	}
	return rows.Err()
}
