package migrations

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/entityid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
)

func init() { sqlite.RegisterTransactionalMigration(1000115, migrateLibraryUUIDReferences) }

// Persist UUIDs in user-authored references so a deleted local key can never
// silently select a different entity. This migration does not rewrite grants,
// provider IDs, media capability keys, or historical import receipts.
func migrateLibraryUUIDReferences(ctx context.Context, tx *sqlx.Tx) error {
	var triggers []string
	if err := tx.SelectContext(ctx, &triggers, "SELECT sql FROM native_library_identity_triggers"); err != nil {
		return err
	}
	for _, statement := range triggers {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "DROP TABLE native_library_identity_triggers"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE library_original_uuids AS
SELECT kind,original_id,min(uuid) AS uuid FROM archive_entities GROUP BY kind,original_id HAVING count(*)=1;
CREATE UNIQUE INDEX library_original_lookup ON library_original_uuids(kind,original_id);
UPDATE scene_cover_sources SET source_file_uuid=(SELECT uuid FROM library_original_uuids WHERE kind='file' AND original_id=source_file_id);`); err != nil {
		return err
	}
	lookup := func(kind string, raw any) (any, error) {
		value := fmt.Sprint(raw)
		if id, err := uuid.Parse(value); err == nil && id != uuid.Nil && id.String() == value {
			return value, nil
		}
		id, err := strconv.Atoi(value)
		if err != nil {
			return nil, fmt.Errorf("invalid stored %s reference: %w", kind, err)
		}
		var public string
		// Current filters used current local keys. A missing key becomes a stable,
		// nonmatching UUID; it must not acquire a match when a new row is inserted.
		if !models.ArchiveEntityKind(kind).IsValid() {
			return nil, fmt.Errorf("invalid library kind %q", kind)
		}
		err = tx.GetContext(ctx, &public, `SELECT uuid FROM archive_entities WHERE `+kind+`_id=?`, id)
		if errors.Is(err, sql.ErrNoRows) {
			return uuid.NewString(), nil
		}
		return public, err
	}
	for _, spec := range []struct{ query, update string }{
		{`SELECT CAST(id AS TEXT) AS key,mode,filter_ast AS value FROM saved_filters WHERE filter_ast!=''`, `UPDATE saved_filters SET filter_ast=? WHERE id=?`},
		{`SELECT view AS key,mode,filter_ast AS value FROM default_filters WHERE filter_ast!=''`, `UPDATE default_filters SET filter_ast=?,revision=revision+1 WHERE view=?`},
		{`SELECT c.view AS key,f.mode,c.alternative_ast AS value FROM default_filter_import_conflicts c JOIN default_filters f USING(view) WHERE c.alternative_ast!=''`, `UPDATE default_filter_import_conflicts SET alternative_ast=? WHERE view=?`},
	} {
		var rows []struct{ Key, Mode, Value string }
		if err := tx.SelectContext(ctx, &rows, spec.query); err != nil {
			return err
		}
		for _, row := range rows {
			var tree map[string]any
			if err := json.Unmarshal([]byte(row.Value), &tree); err != nil {
				return err
			}
			mapped, err := entityid.MapFilterAST(entityid.ModeKind(row.Mode), tree, lookup)
			if err != nil {
				return fmt.Errorf("filter %s: %w", row.Key, err)
			}
			data, err := json.Marshal(mapped)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, spec.update, string(data), row.Key); err != nil {
				return err
			}
		}
	}
	var shares []struct {
		ID       string
		Snapshot []byte
	}
	if err := tx.SelectContext(ctx, &shares, `SELECT id,snapshot FROM shares`); err != nil {
		return err
	}
	for _, row := range shares {
		var snapshot map[string]json.RawMessage
		if err := json.Unmarshal(row.Snapshot, &snapshot); err != nil {
			return err
		}
		if len(snapshot["entries"]) == 0 {
			continue
		}
		var entries []map[string]json.RawMessage
		if err := json.Unmarshal(snapshot["entries"], &entries); err != nil {
			return err
		}
		changed := false
		for _, fields := range entries {
			data, err := json.Marshal(fields)
			if err != nil {
				return err
			}
			var entry models.ShareEntry
			if err := json.Unmarshal(data, &entry); err != nil {
				return err
			}
			if entry.UUID != "" {
				continue
			}
			// Frozen shares can outlive their entities. Only backfill an unambiguous
			// original identity; never turn a reused key into a new library link.
			var candidates []string
			if err := tx.SelectContext(ctx, &candidates, `SELECT uuid FROM library_original_uuids WHERE kind=? AND original_id=?`, strings.ToLower(entry.Kind), entry.ID); err != nil {
				return err
			}
			if len(candidates) == 1 {
				fields["uuid"], err = json.Marshal(candidates[0])
				if err != nil {
					return err
				}
				changed = true
			}
		}
		if !changed {
			continue
		}
		var err error
		snapshot["entries"], err = json.Marshal(entries)
		if err != nil {
			return err
		}
		data, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE shares SET snapshot=? WHERE id=?`, data, row.ID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE library_original_uuids`); err != nil {
		return err
	}
	return nil
}
