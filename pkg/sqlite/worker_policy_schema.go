package sqlite

import (
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validateWorkerPolicySchema(conn *sqlx.DB) error {
	for _, name := range []string{"metadata_worker_policy_upgrades", "metadata_worker_policy_head", "metadata_worker_policy_valid",
		"metadata_worker_policy_immutable", "metadata_worker_policy_retained", "metadata_worker_attempt_policies",
		"metadata_worker_attempt_policy_valid", "metadata_worker_attempt_policy_immutable", "metadata_worker_attempt_policy_retained"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var invalid bool
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM metadata_worker_policy_upgrades p
WHERE p.expected_policy_sha256!=coalesce((SELECT prior.policy_sha256 FROM metadata_worker_policy_upgrades prior
WHERE prior.kind=p.kind AND prior.original_policy_sha256=p.original_policy_sha256 AND prior.id<p.id ORDER BY prior.id DESC LIMIT 1),p.original_policy_sha256)
OR p.created_at_ms<coalesce((SELECT prior.created_at_ms FROM metadata_worker_policy_upgrades prior
WHERE prior.kind=p.kind AND prior.original_policy_sha256=p.original_policy_sha256 AND prior.id<p.id ORDER BY prior.id DESC LIMIT 1),0))
OR EXISTS(SELECT 1 FROM metadata_worker_attempt_policies a JOIN archive_jobs j ON j.uuid=a.job_uuid
JOIN archive_job_attempts t ON t.job_uuid=a.job_uuid AND t.fence=a.fence
LEFT JOIN metadata_worker_policy_upgrades p ON p.request_uuid=a.approval_uuid
WHERE j.kind NOT IN ('post.enrich','account.list_page','post.verify_candidate')
OR a.original_policy_sha256 IS NOT CASE WHEN j.kind='account.list_page'
  THEN (SELECT json_extract(d.definition,'$.policy_sha256') FROM discovery_listings d WHERE d.uuid=json_extract(j.arguments,'$.listing_uuid'))
  ELSE json_extract(j.arguments,'$.policy_sha256') END
OR (a.approval_uuid IS NULL AND a.policy_sha256!=a.original_policy_sha256)
OR (a.approval_uuid IS NOT NULL AND (p.request_uuid IS NULL OR p.kind!=j.kind OR p.original_policy_sha256!=a.original_policy_sha256
OR p.policy_sha256!=a.policy_sha256 OR p.created_at_ms>t.started_at_ms)))`); err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has inconsistent metadata worker policy history")
	}
	return nil
}
