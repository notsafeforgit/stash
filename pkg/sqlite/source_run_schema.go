package sqlite

import (
	"errors"
	"fmt"
	"github.com/jmoiron/sqlx"
)

func validateSourceRunSchema(conn *sqlx.DB, version uint) error {
	if version >= NativeSchemaBaseline+110 {
		var unique bool
		if err := conn.Get(&unique, `SELECT "unique" FROM pragma_index_list('source_runs') WHERE name='source_runs_running_destination'`); err != nil {
			return err
		}
		if unique {
			return errors.New("native database still serializes independent source destinations")
		}
	}
	if version >= NativeSchemaBaseline+106 {
		for _, name := range []string{"source_collection_aliases", "source_collection_alias_source", "source_collection_alias_immutable", "source_collection_alias_retained", "source_collection_alias_frozen", "source_run_retrievals", "source_run_retrieval_immutable", "source_run_retrieval_retained"} {
			var exists bool
			if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("native database schema is incomplete: missing %s", name)
			}
		}
	}
	if version >= NativeSchemaBaseline+103 {
		if err := validateSourceRunPolicySchema(conn); err != nil {
			return err
		}
	}
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
	if version < NativeSchemaBaseline+98 {
		return nil
	}
	for _, name := range []string{"source_run_basis_insert", "source_run_basis_update", "source_run_attempt_basis"} {
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

func validateSourceRunPolicySchema(conn *sqlx.DB) error {
	for _, name := range []string{"source_run_policy_upgrades", "source_run_policy_upgrade_fence", "source_run_policy_upgrade_valid", "source_run_policy_upgrade_revision", "source_run_policy_upgrade_immutable", "source_run_policy_upgrade_retained"} {
		var exists bool
		if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var invalid bool
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM source_run_policy_upgrades p JOIN source_runs r ON r.uuid=p.run_uuid
WHERE p.expected_revision>=r.revision OR p.effective_after_fence>r.fence OR p.created_at_ms>r.updated_at_ms
OR p.expected_policy_sha256!=coalesce((SELECT prior.policy_sha256 FROM source_run_policy_upgrades prior
WHERE prior.run_uuid=p.run_uuid AND prior.expected_revision<p.expected_revision ORDER BY prior.expected_revision DESC LIMIT 1),r.policy_sha256))`); err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has inconsistent source policy upgrade history")
	}
	return nil
}
