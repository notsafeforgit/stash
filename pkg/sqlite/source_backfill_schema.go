package sqlite

import (
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validateSourceBackfillSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"source_backfill_decisions", "source_backfill_subject", "source_backfill_immutable", "source_backfill_requests", "source_backfill_request_reference", "source_backfill_request_valid", "source_backfill_request_immutable"} {
		var exists bool
		if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	var invalid bool
	if !auditData {
		return nil
	}
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM source_backfill_decisions d WHERE d.basis='source_runs' AND
  (json_type(d.evidence,'$.completion.requests') IS NOT 'array'
   OR json_type(d.evidence,'$.targets') IS NOT 'array'
   OR json_extract(d.evidence,'$.completion.uuid') IS NOT d.uuid
   OR json_extract(d.evidence,'$.completion.root_uuid') IS NOT d.root_uuid
   OR json_extract(d.evidence,'$.completion.platform') IS NOT d.platform
   OR json_extract(d.evidence,'$.completion.account') IS NOT d.account
   OR json_extract(d.evidence,'$.completion.component') IS NOT d.component
   OR json_array_length(d.evidence,'$.completion.requests')=0
   OR json_array_length(d.evidence,'$.completion.requests')!=(SELECT count(*) FROM source_backfill_requests p WHERE p.decision_uuid=d.uuid)
   OR EXISTS(SELECT 1 FROM json_each(d.evidence,'$.completion.requests') j
     WHERE NOT EXISTS(SELECT 1 FROM source_backfill_requests p WHERE p.decision_uuid=d.uuid AND p.producer_uuid=d.producer_uuid
       AND p.request_uuid=json_extract(j.value,'$.request_uuid')))))
OR EXISTS(SELECT 1 FROM source_backfill_requests p JOIN source_backfill_decisions d ON d.uuid=p.decision_uuid
  JOIN source_run_requests q ON q.producer_uuid=p.producer_uuid AND q.request_uuid=p.request_uuid
  JOIN source_runs r ON r.uuid=q.run_uuid
  WHERE d.basis!='source_runs' OR d.producer_uuid IS NOT p.producer_uuid OR d.root_uuid IS NOT r.root_uuid OR r.operation!='download')`); err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has inconsistent source backfill proof")
	}
	return nil
}
