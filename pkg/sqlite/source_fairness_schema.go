package sqlite

import (
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/models"
)

func validateSourceFairnessSchema(conn *sqlx.DB, handoffs, details bool) error {
	for _, name := range []string{"source_service_turns", "source_service_turns_bind", "source_enrichment_waiters",
		"source_enrichment_waiters_expiry", "source_enrichment_waiter_scopes", "source_enrichment_waiter_scopes_scope",
		"source_enrichment_waiter_current", "source_enrichment_waiter_identity", "source_enrichment_waiter_end"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var invalid bool
	query := `SELECT EXISTS(SELECT 1 FROM source_pacing p
 WHERE NOT EXISTS(SELECT 1 FROM source_service_turns t WHERE t.scope=p.scope))
 OR EXISTS(SELECT 1 FROM source_service_turns t WHERE NOT EXISTS(SELECT 1 FROM source_pacing p WHERE p.scope=t.scope))
 OR EXISTS(SELECT 1 FROM source_enrichment_waiters w
 WHERE NOT EXISTS(SELECT 1 FROM archive_jobs j JOIN enrichment_job_pacing p ON p.job_uuid=j.uuid
 WHERE j.uuid=w.job_uuid AND j.kind IN ('post.enrich','account.list_page','post.verify_candidate') AND j.state='queued' AND j.fence=w.fence)
 OR NOT EXISTS(SELECT 1 FROM source_enrichment_waiter_scopes s JOIN enrichment_job_pacing p ON p.job_uuid=s.job_uuid AND p.scope=s.scope WHERE s.job_uuid=w.job_uuid))
 OR EXISTS(SELECT 1 FROM source_enrichment_waiter_scopes s
 WHERE NOT EXISTS(SELECT 1 FROM source_enrichment_waiters w WHERE w.job_uuid=s.job_uuid)
 OR NOT EXISTS(SELECT 1 FROM source_pacing p WHERE p.scope=s.scope)
 OR (NOT EXISTS(SELECT 1 FROM enrichment_job_pacing p WHERE p.job_uuid=s.job_uuid AND p.scope=s.scope)
 AND NOT EXISTS(SELECT 1 FROM enrichment_checkpoints h,json_each(h.body,'$.pending') p
 WHERE h.job_uuid=s.job_uuid AND source_scope_v1(json_extract(p.value,'$.url'))=s.scope)))
 OR EXISTS(SELECT 1 FROM source_enrichment_waiters w JOIN enrichment_checkpoints h ON h.job_uuid=w.job_uuid,json_each(h.body,'$.pending') p
	WHERE NOT EXISTS(SELECT 1 FROM source_enrichment_waiter_scopes s WHERE s.job_uuid=w.job_uuid AND s.scope=source_scope_v1(json_extract(p.value,'$.url'))))`
	if handoffs {
		query = strings.Replace(query, "WHERE h.job_uuid=s.job_uuid AND source_scope_v1(json_extract(p.value,'$.url'))=s.scope)", `WHERE h.job_uuid=s.job_uuid AND source_scope_v1(json_extract(p.value,'$.url'))=s.scope)
 AND NOT EXISTS(SELECT 1 FROM enrichment_job_seed_services x WHERE x.job_uuid=s.job_uuid AND x.scope=s.scope
 AND NOT EXISTS(SELECT 1 FROM enrichment_checkpoints WHERE job_uuid=x.job_uuid))`, 1)
		query += ` OR EXISTS(SELECT 1 FROM source_enrichment_waiters w JOIN enrichment_job_seed_services x ON x.job_uuid=w.job_uuid
 WHERE NOT EXISTS(SELECT 1 FROM enrichment_checkpoints WHERE job_uuid=w.job_uuid)
 AND NOT EXISTS(SELECT 1 FROM source_enrichment_waiter_scopes s WHERE s.job_uuid=w.job_uuid AND s.scope=x.scope))`
	}
	if details {
		query = strings.ReplaceAll(query, "enrichment_checkpoints h", "(SELECT job_uuid,body FROM enrichment_checkpoints UNION ALL SELECT job_uuid,body FROM discovery_detail_checkpoints) h")
	}
	err := conn.Get(&invalid, query)
	if err != nil {
		return err
	}
	if invalid {
		return models.ErrSourcePayloadCorrupt
	}
	return nil
}
