package sqlite

import (
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/models"
)

func validateSourcePacingSchema(conn *sqlx.DB) error {
	for _, name := range []string{"source_pacing", "source_run_pacing", "enrichment_job_pacing", "source_run_pacing_scope", "enrichment_job_pacing_scope",
		"source_run_pacing_bind", "enrichment_job_pacing_bind", "source_run_pacing_immutable", "enrichment_job_pacing_immutable",
		"enrichment_attempt_pacing", "enrichment_attempt_pacing_scope", "enrichment_attempt_pacing_bind", "enrichment_attempt_pacing_immutable", "enrichment_attempt_pacing_current"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var invalid bool
	err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM source_runs r
 JOIN source_collection_revisions c ON c.collection_uuid=r.collection_uuid AND c.revision=r.collection_revision
 LEFT JOIN source_run_pacing p ON p.run_uuid=r.uuid LEFT JOIN source_pacing s ON s.scope=p.scope
 WHERE p.scope IS NOT source_scope_v1(c.target_url) OR s.scope IS NULL)
 OR EXISTS(SELECT 1 FROM enrichment_job_targets b
 JOIN enrichment_targets t ON t.uuid=b.target_uuid JOIN source_post_urls u ON u.uuid=t.url_uuid
 LEFT JOIN enrichment_job_pacing p ON p.job_uuid=b.job_uuid LEFT JOIN source_pacing s ON s.scope=p.scope
 WHERE p.scope IS NOT source_scope_v1(u.url) OR s.scope IS NULL)
 OR EXISTS(SELECT 1 FROM source_run_pacing p WHERE NOT EXISTS(SELECT 1 FROM source_runs r WHERE r.uuid=p.run_uuid))
 OR EXISTS(SELECT 1 FROM enrichment_job_pacing p WHERE NOT EXISTS(SELECT 1 FROM enrichment_job_targets b WHERE b.job_uuid=p.job_uuid)
 AND NOT EXISTS(SELECT 1 FROM archive_jobs j WHERE j.uuid=p.job_uuid AND j.kind='account.list_page'))
 OR EXISTS(SELECT 1 FROM archive_job_attempts a JOIN enrichment_job_pacing p ON p.job_uuid=a.job_uuid
 WHERE NOT EXISTS(SELECT 1 FROM enrichment_attempt_pacing s WHERE s.job_uuid=a.job_uuid AND s.fence=a.fence AND s.scope=p.scope))
 OR EXISTS(SELECT 1 FROM enrichment_attempt_pacing p
 WHERE NOT EXISTS(SELECT 1 FROM archive_job_attempts a WHERE a.job_uuid=p.job_uuid AND a.fence=p.fence)
 OR NOT EXISTS(SELECT 1 FROM enrichment_job_pacing b WHERE b.job_uuid=p.job_uuid)
 OR NOT EXISTS(SELECT 1 FROM source_pacing s WHERE s.scope=p.scope))`)
	if err != nil {
		return err
	}
	if invalid {
		return models.ErrSourcePayloadCorrupt
	}
	return nil
}
