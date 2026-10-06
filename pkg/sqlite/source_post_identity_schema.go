package sqlite

import (
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

func validatePostIdentitySchema(conn *sqlx.DB) error {
	for _, name := range []string{"source_post_identities", "source_post_identities_canonical", "source_post_consolidations", "source_post_consolidations_destination",
		"source_post_consolidation_context", "source_post_consolidation_scope", "source_post_consolidation_publish", "source_post_consolidation_immutable",
		"source_post_root_initial", "source_post_root_created", "source_post_root_update", "source_post_uuid_immutable", "source_post_group_identifier", "source_post_group_forgotten"} {
		var exists bool
		if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("native post identity schema is incomplete: missing %s", name)
		}
	}
	var invalid bool
	if err := conn.Get(&invalid, "SELECT EXISTS(SELECT 1 FROM source_post_consolidation_context)"); err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has unfinished post consolidation")
	}
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM source_posts p
LEFT JOIN source_post_identities i ON i.post_uuid=p.uuid
LEFT JOIN source_posts r ON r.uuid=i.canonical_uuid
LEFT JOIN source_post_identities ri ON ri.post_uuid=r.uuid
LEFT JOIN source_post_consolidations c ON c.source_uuid=p.uuid
LEFT JOIN source_posts d ON d.uuid=c.destination_uuid
LEFT JOIN source_post_identities di ON di.post_uuid=d.uuid
LEFT JOIN source_post_consolidations next ON next.source_uuid=c.destination_uuid
WHERE i.post_uuid IS NULL OR r.uuid IS NULL OR ri.canonical_uuid!=r.uuid OR r.state!=p.state
OR (i.canonical_uuid=p.uuid AND c.source_uuid IS NOT NULL)
OR (i.canonical_uuid!=p.uuid AND (c.source_uuid IS NULL OR di.canonical_uuid!=i.canonical_uuid))
OR (next.source_uuid IS NOT NULL AND next.sequence<=c.sequence)
OR (c.source_uuid IS NOT NULL AND (p.revision<=c.source_revision OR d.revision<=c.destination_revision)))`); err != nil {
		return err
	}
	if invalid {
		return errors.New("native post identity differs from its consolidation history")
	}
	return nil
}
