package sqlite

import (
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

func validateAutomationCheckpointSchema(conn *sqlx.DB) error {
	for _, name := range []string{"automation_checkpoint_imports", "automation_checkpoint_import_guard", "automation_checkpoint_records",
		"automation_checkpoint_record_immutable", "automation_checkpoint_bodies", "automation_checkpoint_body_immutable",
		"automation_checkpoint_review", "automation_checkpoint_sources", "automation_enrichment_staged_input"} {
		var exists bool
		if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var invalid bool
	err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM automation_checkpoint_imports i
 LEFT JOIN automation_enrichment_imports p ON p.snapshot_uuid=i.snapshot_uuid
 WHERE p.state IS NULL OR p.state='running' OR i.manifest_sha256 IS NOT p.manifest_sha256 OR i.policy!='legacy-enrichment-staging-v1'
 OR i.state NOT IN ('running','mapped','review') OR (i.state!='running' AND i.processed_records!=i.source_records)
 OR (i.state='mapped' AND i.review_records!=0) OR (i.state='review' AND i.review_records=0)
 OR i.source_records!=(SELECT count(*) FROM automation_snapshot_records WHERE snapshot_uuid=i.snapshot_uuid AND `+automationStagedInput+`)
 OR i.processed_records!=(SELECT count(*) FROM automation_checkpoint_records r WHERE r.snapshot_uuid=i.snapshot_uuid)
 OR i.mapped_records!=(SELECT count(*) FROM automation_checkpoint_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='mapped')
 OR i.review_records!=(SELECT count(*) FROM automation_checkpoint_records r WHERE r.snapshot_uuid=i.snapshot_uuid AND r.outcome='review')
 OR i.last_ordinal!=(SELECT coalesce(max(ordinal),0) FROM automation_checkpoint_records r WHERE r.snapshot_uuid=i.snapshot_uuid)
 OR i.processed_records!=(SELECT count(*) FROM automation_snapshot_records WHERE snapshot_uuid=i.snapshot_uuid AND ordinal<=i.last_ordinal AND `+automationStagedInput+`))
 OR EXISTS(SELECT 1 FROM automation_checkpoint_records r
 LEFT JOIN automation_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
 LEFT JOIN automation_enrichment_records a ON a.snapshot_uuid=r.snapshot_uuid AND a.ordinal=r.ordinal
 LEFT JOIN automation_checkpoint_bodies b ON b.hash=r.body_sha256
 WHERE a.ordinal IS NULL OR e.source_table IS NOT 'enrichment_jobs' OR r.source_sha256 IS NOT e.data_sha256
 OR coalesce(json_type(e.data,'$.values.staged_json'),'null')='null'
 OR (r.outcome='mapped' AND b.hash IS NULL))
 OR EXISTS(SELECT 1 FROM automation_checkpoint_bodies b WHERE NOT EXISTS(SELECT 1 FROM automation_checkpoint_records r WHERE r.body_sha256=b.hash))`)
	if err != nil {
		return err
	}
	if invalid {
		return models.ErrSourcePayloadCorrupt
	}
	progress, err := conn.Query(`SELECT p.updated_at,i.created_at,i.updated_at FROM automation_checkpoint_imports i JOIN automation_enrichment_imports p ON p.snapshot_uuid=i.snapshot_uuid`)
	if err != nil {
		return err
	}
	defer progress.Close()
	for progress.Next() {
		var parent, created, updated string
		if err := progress.Scan(&parent, &created, &updated); err != nil {
			return err
		}
		p, e1 := time.Parse(time.RFC3339Nano, parent)
		c, e2 := time.Parse(time.RFC3339Nano, created)
		u, e3 := time.Parse(time.RFC3339Nano, updated)
		if e1 != nil || e2 != nil || e3 != nil || !validJobTime(c) || !validJobTime(u) || c.Before(p) || u.Before(c) {
			return models.ErrSourcePayloadCorrupt
		}
	}
	err = progress.Err()
	if err != nil {
		return err
	}
	rows, err := conn.Queryx("SELECT " + automationCheckpointColumns + `,e.data,b.body FROM automation_checkpoint_records r
 JOIN automation_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
 LEFT JOIN automation_checkpoint_bodies b ON b.hash=r.body_sha256 ORDER BY r.snapshot_uuid,r.ordinal`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row struct {
			models.AutomationCheckpointRecord
			Data string  `db:"data"`
			Body *string `db:"body"`
		}
		if err := rows.StructScan(&row); err != nil {
			return err
		}
		expected, body, err := prepareAutomationCheckpoint(catalogEvidenceRow{Ordinal: row.Ordinal, Data: row.Data, SHA256: row.SourceSHA256})
		if err != nil || !sameAutomationCheckpoint(*expected, row.AutomationCheckpointRecord) || (row.Body == nil) != (body == nil) ||
			(row.Body != nil && (len(*row.Body) > scrape.MaxEnrichmentStagingBytes || *row.Body != string(body))) {
			return models.ErrSourcePayloadCorrupt
		}
	}
	return rows.Err()
}
