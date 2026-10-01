package sqlite

import (
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validateArchiveJobSchema(conn *sqlx.DB) error {
	for _, name := range []string{"archive_jobs", "archive_jobs_active_work", "archive_jobs_running_resource", "archive_jobs_ready", "archive_jobs_expired", "archive_jobs_list", "archive_job_identity", "archive_job_transition", "archive_job_submissions", "archive_job_submissions_job", "archive_job_submission_immutable", "archive_job_attempts", "archive_job_attempt_valid", "archive_job_attempt_immutable"} {
		var exists bool
		if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	return nil
}
