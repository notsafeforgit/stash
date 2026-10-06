package sqlite

import (
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

type enrichmentRebindingRow struct {
	UUID           string    `db:"uuid"`
	InputSHA256    string    `db:"input_sha256"`
	PlanSHA256     string    `db:"plan_sha256"`
	ActivationUUID string    `db:"activation_uuid"`
	Plan           string    `db:"plan"`
	CreatedAt      time.Time `db:"created_at"`
}

func (r enrichmentRebindingRow) decode() (*models.EnrichmentRebindPlan, error) {
	plan, err := archive.DecodeEnrichmentRebindPlan([]byte(r.Plan))
	if err != nil || plan.Input.UUID != r.UUID || plan.PlanSHA256 != r.PlanSHA256 || plan.Activation.Input.UUID != r.ActivationUUID || !validJobTime(r.CreatedAt) {
		return nil, models.ErrSourcePayloadCorrupt
	}
	_, digest, err := archive.PrepareEnrichmentRebind(plan.Input)
	if err != nil || digest != r.InputSHA256 {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return plan, nil
}

func validateEnrichmentRebindSchema(conn *sqlx.DB) error {
	for _, object := range []struct{ name, kind string }{
		{"enrichment_rebindings", "table"}, {"enrichment_rebinding_targets", "table"},
		{"enrichment_rebinding_immutable", "trigger"}, {"enrichment_rebinding_target_immutable", "trigger"}, {"enrichment_rebinding_target_scope", "trigger"},
		{"enrichment_targets_pending_scope", "index"},
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
	if err := conn.Get(&invalid, `SELECT
NOT EXISTS(SELECT 1 FROM pragma_table_info('enrichment_rebindings') WHERE name='created_at' AND upper(type)='DATETIME')
OR EXISTS(SELECT 1 FROM enrichment_rebindings r
 LEFT JOIN enrichment_activations a ON a.uuid=r.activation_uuid
 LEFT JOIN source_collection_revisions c ON c.collection_uuid=json_extract(r.plan,'$.input.collection_uuid') AND c.revision=json_extract(r.plan,'$.input.collection_revision')
 WHERE a.uuid IS NULL OR c.state IS NOT 'active' OR a.created_at IS NOT r.created_at
 OR json_extract(r.plan,'$.activation') IS NOT a.plan
 OR json_type(r.plan,'$.input.targets') IS NOT 'array' OR coalesce(json_array_length(r.plan,'$.input.targets'),0) NOT BETWEEN 1 AND 100
 OR json_array_length(r.plan,'$.input.targets')!=(SELECT count(*) FROM enrichment_rebinding_targets x WHERE x.rebinding_uuid=r.uuid)
 OR json_extract(r.plan,'$.collection.uuid') IS NOT c.collection_uuid OR json_extract(r.plan,'$.collection.revision') IS NOT c.revision
 OR json_extract(r.plan,'$.collection.label') IS NOT c.label OR json_extract(r.plan,'$.collection.kind') IS NOT c.kind
 OR json_extract(r.plan,'$.collection.namespace') IS NOT c.namespace OR json_extract(r.plan,'$.collection.state') IS NOT c.state
 OR json_extract(r.plan,'$.collection.target_url') IS NOT c.target_url OR json_extract(r.plan,'$.collection.account_uuid') IS NOT c.account_uuid
 OR json_extract(r.plan,'$.collection.root_uuid') IS NOT c.root_uuid OR json_extract(r.plan,'$.collection.path_prefix') IS NOT c.path_prefix)
OR EXISTS(SELECT 1 FROM enrichment_rebinding_targets x
 LEFT JOIN enrichment_rebindings r ON r.uuid=x.rebinding_uuid
 LEFT JOIN enrichment_activation_targets a ON a.activation_uuid=x.activation_uuid AND a.target_uuid=x.target_uuid
 LEFT JOIN enrichment_target_history old ON old.target_uuid=x.target_uuid AND old.revision=x.previous_revision
 LEFT JOIN enrichment_target_history held ON held.target_uuid=x.target_uuid AND held.revision=x.held_revision
 WHERE r.uuid IS NULL OR a.target_uuid IS NULL OR x.activation_uuid IS NOT r.activation_uuid
 OR x.held_revision!=x.previous_revision+1 OR a.previous_revision!=x.held_revision OR a.released_target_uuid=a.target_uuid
 OR old.state IS NOT 'pending' OR old.completion_uuid IS NOT NULL OR held.state IS NOT 'held' OR held.completion_uuid IS NOT NULL
 OR held.reason IS NOT 'collection_rebind' OR held.priority IS NOT old.priority OR held.not_before IS NOT old.not_before OR held.recorded_at IS NOT r.created_at
 OR EXISTS(SELECT 1 FROM enrichment_job_targets j WHERE j.target_uuid=x.target_uuid AND j.target_revision<=x.previous_revision))
OR EXISTS(SELECT 1 FROM enrichment_rebindings r,json_each(r.plan,'$.input.targets') e
 LEFT JOIN enrichment_rebinding_targets x ON x.rebinding_uuid=r.uuid AND x.target_uuid=json_extract(e.value,'$.target_uuid')
 WHERE x.target_uuid IS NULL OR x.previous_revision IS NOT json_extract(e.value,'$.revision'))`); err != nil {
		return err
	}
	if invalid {
		return models.ErrSourcePayloadCorrupt
	}
	rows, err := conn.Queryx(`SELECT r.*,c.created_at AS collection_created_at FROM enrichment_rebindings r
JOIN source_collections c ON c.uuid=json_extract(r.plan,'$.input.collection_uuid') ORDER BY r.uuid`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row struct {
			enrichmentRebindingRow
			CollectionCreatedAt time.Time `db:"collection_created_at"`
		}
		if err := rows.StructScan(&row); err != nil {
			return err
		}
		plan, err := row.decode()
		if err != nil {
			return err
		}
		if !plan.Collection.CreatedAt.Equal(row.CollectionCreatedAt) {
			return models.ErrSourcePayloadCorrupt
		}
	}
	return rows.Err()
}
