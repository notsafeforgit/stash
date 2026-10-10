package sqlite

import (
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validateSourcePostLinksSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{
		"source_post_urls", "source_post_url_evidence", "source_post_identifier_evidence", "source_post_account_claims",
		"source_post_urls_page", "source_post_urls_lookup", "source_post_url_evidence_page",
		"source_post_identifier_evidence_page", "source_post_account_claim_page", "source_post_account_claim_account",
		"source_post_url_immutable", "source_post_url_evidence_immutable", "source_post_url_evidence_revision",
		"source_post_identifier_evidence_scope", "source_post_identifier_evidence_immutable", "source_post_identifier_evidence_revision",
		"source_post_account_claim_immutable", "source_post_account_claim_revision",
		"source_post_url_active_post", "source_post_url_evidence_active_post",
		"source_post_identifier_evidence_active_post", "source_post_account_claim_active_post",
	} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	// The SQLite driver returns time.Time only for a declared date/time
	// column. Timestamp scanning must never encounter a plain text column.
	for _, table := range []string{"source_post_url_evidence", "source_post_identifier_evidence", "source_post_account_claims"} {
		var typed bool
		if err := conn.Get(&typed, "SELECT EXISTS(SELECT 1 FROM pragma_table_info(?) WHERE name='observed_at' AND upper(type)='DATETIME')", table); err != nil {
			return err
		}
		if !typed {
			return fmt.Errorf("native database schema is incomplete: invalid %s.observed_at", table)
		}
	}
	var invalid bool
	if !auditData {
		return nil
	}
	if err := conn.Get(&invalid, `SELECT
 EXISTS(SELECT 1 FROM source_post_urls u LEFT JOIN source_posts p ON p.uuid=u.post_uuid WHERE p.uuid IS NULL
 OR NOT EXISTS(SELECT 1 FROM source_post_url_evidence e WHERE e.url_uuid=u.uuid))
 OR EXISTS(SELECT 1 FROM source_post_url_evidence e LEFT JOIN source_post_urls u ON u.uuid=e.url_uuid WHERE u.uuid IS NULL)
 OR EXISTS(SELECT 1 FROM source_post_identifier_evidence e LEFT JOIN source_post_identifiers i ON i.namespace=e.namespace AND i.value=e.value
 LEFT JOIN source_posts p ON p.uuid=e.post_uuid WHERE i.post_uuid IS NOT e.post_uuid OR p.uuid IS NULL)
 OR EXISTS(SELECT 1 FROM source_post_account_claims e LEFT JOIN source_posts p ON p.uuid=e.post_uuid
 LEFT JOIN source_accounts a ON a.uuid=e.account_uuid WHERE p.uuid IS NULL OR a.uuid IS NULL)`); err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has incomplete source post link evidence")
	}
	return nil
}
