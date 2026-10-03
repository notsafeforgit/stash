package sqlite

import (
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

func validateEnrichmentHandoffSchema(conn *sqlx.DB) error {
	for _, object := range []struct{ name, kind string }{
		{"enrichment_handoff_jobs", "table"}, {"enrichment_job_retained_records", "table"}, {"enrichment_job_seed_services", "table"},
		{"enrichment_handoff_job_immutable", "trigger"}, {"enrichment_handoff_job_scope", "trigger"},
		{"enrichment_job_retained_immutable", "trigger"}, {"enrichment_job_retained_scope", "trigger"},
		{"enrichment_job_seed_service_immutable", "trigger"}, {"enrichment_job_seed_service_scope", "trigger"},
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
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM archive_jobs j WHERE j.kind='post.enrich'
 AND (json_type(j.arguments,'$.handoff') IS NOT NULL)!=EXISTS(SELECT 1 FROM enrichment_handoff_jobs h WHERE h.job_uuid=j.uuid))
 OR EXISTS(SELECT 1 FROM enrichment_handoff_jobs h LEFT JOIN archive_jobs j ON j.uuid=h.job_uuid
 LEFT JOIN enrichment_job_targets b ON b.job_uuid=h.job_uuid
 WHERE j.kind IS NOT 'post.enrich' OR b.job_uuid IS NULL OR json_extract(j.arguments,'$.handoff.uuid') IS NOT h.handoff_uuid)
 OR EXISTS(SELECT 1 FROM enrichment_job_retained_records r LEFT JOIN enrichment_handoff_jobs h ON h.job_uuid=r.job_uuid WHERE h.job_uuid IS NULL)
 OR EXISTS(SELECT 1 FROM enrichment_job_seed_services r LEFT JOIN enrichment_handoff_jobs h ON h.job_uuid=r.job_uuid WHERE h.job_uuid IS NULL)`); err != nil {
		return err
	}
	if invalid {
		return models.ErrSourcePayloadCorrupt
	}
	for after := ""; ; {
		var id string
		if err := conn.Get(&id, "SELECT coalesce(min(job_uuid),'') FROM enrichment_handoff_jobs WHERE job_uuid>?", after); err != nil || id == "" {
			return err
		}
		var row archiveJobRow
		if err := conn.Get(&row, "SELECT * FROM archive_jobs WHERE uuid=?", id); err != nil {
			return err
		}
		job := row.resolve()
		work, err := archive.DecodeEnrichmentJob(job)
		if err != nil {
			return err
		}
		if _, err := readEnrichmentSeed(conn.Get, conn.Select, job, work); err != nil {
			return fmt.Errorf("enrichment handoff %s: %w", id, err)
		}
		after = id
	}
}
