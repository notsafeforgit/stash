package sqlite

import (
	"errors"
	"fmt"
	"strings"

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

func validateAlbumJobSchema(conn *sqlx.DB) error {
	var found bool
	if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type='index' AND name='archive_jobs_resource_history')"); err != nil {
		return err
	}
	if !found {
		return errors.New("native database schema is incomplete: missing archive_jobs_resource_history")
	}
	var definition string
	if err := conn.Get(&definition, "SELECT sql FROM sqlite_schema WHERE type='table' AND name='archive_jobs'"); err != nil {
		return err
	}
	if !strings.Contains(definition, "'album.backfill'") {
		return errors.New("native database schema is incomplete: missing album archive job kind")
	}
	var invalid bool
	if err := conn.Get(&invalid, "SELECT EXISTS(SELECT 1 FROM archive_jobs WHERE kind NOT IN ('media.verify','album.backfill'))"); err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has an unsupported archive job kind")
	}
	return nil
}
