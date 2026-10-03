package sqlite

import (
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/models"
)

func validateEnrichmentReleaseSchema(conn *sqlx.DB) error {
	for _, object := range []struct{ name, kind string }{
		{"enrichment_checkpoint_releases", "table"}, {"enrichment_checkpoint_release_immutable", "trigger"},
		{"enrichment_checkpoint_release_scope", "trigger"}, {"enrichment_checkpoint_published_delete", "trigger"},
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
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM enrichment_checkpoint_releases x
 LEFT JOIN enrichment_publications p ON p.job_uuid=x.job_uuid
 WHERE p.job_uuid IS NULL OR x.created_at<p.created_at)
 OR NOT EXISTS(SELECT 1 FROM pragma_table_info('enrichment_checkpoint_releases') WHERE name='created_at' AND upper(type)='DATETIME')`); err != nil {
		return err
	}
	if invalid {
		return models.ErrSourcePayloadCorrupt
	}
	return nil
}
