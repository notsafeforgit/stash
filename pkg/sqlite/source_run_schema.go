package sqlite

import (
	"errors"
	"fmt"
	"github.com/jmoiron/sqlx"
)

func validateSourceRunSchema(conn *sqlx.DB) error {
	for _, name := range []string{"source_runs", "source_runs_active_work", "source_runs_running_collection", "source_runs_running_target", "source_runs_running_destination", "source_runs_running_root", "source_runs_expired", "source_runs_collection_page", "source_runs_scope_page", "source_runs_active", "source_run_scope", "source_run_identity", "source_run_transition", "source_run_requests", "source_run_requests_run", "source_run_request_immutable", "source_run_attempts", "source_run_attempt_valid", "source_run_attempt_immutable", "source_run_cooldowns", "source_run_reviews", "source_run_review_immutable"} {
		var exists bool
		if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var unfinished bool
	if err := conn.Get(&unfinished, `SELECT EXISTS(SELECT 1 FROM source_runs r LEFT JOIN source_run_attempts a ON a.run_uuid=r.uuid AND a.fence=r.fence
WHERE r.state='running' AND (a.run_uuid IS NULL OR a.outcome!='running' OR a.producer_uuid!=r.producer_uuid OR a.owner_uuid!=r.owner_uuid OR a.window!=r.window OR a.progress!=r.progress))
OR EXISTS(SELECT 1 FROM source_run_attempts a JOIN source_runs r ON r.uuid=a.run_uuid WHERE a.outcome='running' AND (r.state!='running' OR r.fence!=a.fence))`); err != nil {
		return err
	}
	if unfinished {
		return errors.New("native database has inconsistent source run ownership")
	}
	return nil
}
