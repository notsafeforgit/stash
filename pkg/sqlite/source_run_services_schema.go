package sqlite

import (
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/models"
)

func validateSourceRunServicesSchema(conn *sqlx.DB) error {
	for _, name := range []string{"source_run_attempt_pacing", "source_run_attempt_pacing_scope",
		"source_run_attempt_pacing_bind", "source_run_attempt_pacing_current", "source_run_attempt_pacing_transition",
		"source_run_attempt_failures", "source_run_attempt_failure_current", "source_run_attempt_failure_immutable"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var invalid bool
	err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM source_run_attempts a
 JOIN source_run_pacing p ON p.run_uuid=a.run_uuid
 WHERE NOT EXISTS(SELECT 1 FROM source_run_attempt_pacing s
  WHERE s.run_uuid=a.run_uuid AND s.fence=a.fence AND s.scope=p.scope AND s.reserved=1))
 OR EXISTS(SELECT 1 FROM source_run_attempt_pacing p
 WHERE NOT EXISTS(SELECT 1 FROM source_run_attempts a WHERE a.run_uuid=p.run_uuid AND a.fence=p.fence)
 OR NOT EXISTS(SELECT 1 FROM source_pacing s WHERE s.scope=p.scope)
 OR NOT EXISTS(SELECT 1 FROM source_run_pacing b WHERE b.run_uuid=p.run_uuid
  AND (b.scope=p.scope OR p.scope IN ('service:redgifs','service:imgur'))))
 OR EXISTS(SELECT 1 FROM source_run_attempt_failures f
 WHERE NOT EXISTS(SELECT 1 FROM source_run_attempts a
 JOIN source_run_attempt_pacing p ON p.run_uuid=a.run_uuid AND p.fence=a.fence AND p.scope=f.scope
 WHERE a.run_uuid=f.run_uuid AND a.fence=f.fence AND a.outcome IN ('retry','deferred')
  AND a.error_code=f.error_code AND a.ended_at_ms=f.created_at_ms
  AND (p.reserved=1 OR f.error_code='source_busy')))`)
	if err != nil {
		return err
	}
	if invalid {
		return models.ErrSourcePayloadCorrupt
	}
	return nil
}
