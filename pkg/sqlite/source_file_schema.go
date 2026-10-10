package sqlite

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/archive"
)

func validateSourceFileSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{
		"source_content_claims", "source_file_observations", "source_file_matches", "source_post_file_evidence",
		"source_content_claim_reference", "source_content_claim_collection", "source_content_claim_immutable",
		"source_file_observation_claim", "source_file_observation_location", "source_file_observation_collection", "source_file_observation_immutable",
		"source_file_match_observation", "source_file_match_file", "source_file_match_archive", "source_file_match_generation", "source_file_match_kind_update", "source_file_match_immutable",
		"source_post_file_evidence_post", "source_post_file_evidence_observation", "source_post_file_evidence_immutable", "source_post_file_evidence_active_post", "source_post_file_evidence_revision",
	} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	for _, table := range []string{"source_content_claims", "source_file_observations", "source_post_file_evidence"} {
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
	if err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM source_content_claims c
 LEFT JOIN source_collection_revisions r ON r.collection_uuid=c.collection_uuid AND r.revision=c.collection_revision
 WHERE r.collection_uuid IS NULL)
 OR EXISTS(SELECT 1 FROM source_file_observations o
 LEFT JOIN source_collection_revisions c ON c.collection_uuid=o.collection_uuid AND c.revision=o.collection_revision
 LEFT JOIN media_root_revisions r ON r.root_uuid=o.root_uuid AND r.revision=o.root_revision
 LEFT JOIN source_content_claims a ON a.uuid=o.content_claim_uuid
 WHERE c.collection_uuid IS NULL OR r.root_uuid IS NULL
 OR (o.content_claim_uuid IS NOT NULL AND a.collection_uuid IS NOT o.collection_uuid))
 OR EXISTS(SELECT 1 FROM source_file_matches m
 LEFT JOIN source_file_observations o ON o.uuid=m.observation_uuid
 LEFT JOIN archive_entities f ON f.uuid=m.file_uuid
 LEFT JOIN archive_entities z ON z.uuid=m.archive_file_uuid
 WHERE o.uuid IS NULL OR f.kind IS NULL OR f.kind!='file'
 OR (m.archive_file_uuid IS NOT NULL AND (z.kind IS NULL OR z.kind!='file'))
 OR (m.archive_file_uuid IS NULL)!=(m.archive_generation IS NULL))
 OR EXISTS(SELECT 1 FROM source_post_file_evidence e
 LEFT JOIN source_posts p ON p.uuid=e.post_uuid LEFT JOIN source_file_observations o ON o.uuid=e.observation_uuid
 WHERE p.uuid IS NULL OR o.uuid IS NULL)`); err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has invalid source file evidence scope or targets")
	}
	rows, err := conn.Query("SELECT relative_path,archive_path,survivor_path FROM source_file_observations")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var relative string
		var zipPath, survivor sql.NullString
		if err := rows.Scan(&relative, &zipPath, &survivor); err != nil {
			return err
		}
		if !archive.ValidRootRelativePath(relative, false) || (zipPath.Valid && !archive.ValidRootRelativePath(zipPath.String, false)) ||
			(survivor.Valid && !archive.ValidRootRelativePath(survivor.String, false)) {
			return errors.New("native database has invalid source file observation paths")
		}
	}
	return rows.Err()
}
