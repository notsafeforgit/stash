package sqlite

import (
	"context"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
)

// TEMP triggers belong only to the fork's writer connection. They never enter
// sqlite_schema, database backups, or an upstream connection's write path. The
// journal is transactional, so rolled-back edits do not invalidate the index.
func prepareSearchChanges(ctx context.Context, tx *sqlx.Tx) error {
	var schemaVersion int
	if err := tx.GetContext(ctx, &schemaVersion, "PRAGMA main.schema_version"); err != nil {
		return err
	}
	var installedVersion int
	err := tx.GetContext(ctx, &installedVersion, "SELECT version FROM temp.stash_search_schema")
	if err != nil || installedVersion != schemaVersion {
		if _, err := tx.ExecContext(ctx, "SAVEPOINT stash_search_setup"); err != nil {
			return err
		}
		if err := installSearchChanges(ctx, tx, schemaVersion); err != nil {
			_, _ = tx.ExecContext(ctx, "ROLLBACK TO stash_search_setup; RELEASE stash_search_setup")
			return err
		}
		if _, err := tx.ExecContext(ctx, "RELEASE stash_search_setup"); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, "DELETE FROM temp.stash_search_changes")
	return err
}

func installSearchChanges(ctx context.Context, tx *sqlx.Tx, schemaVersion int) error {
	// An upstream table rebuild may have removed or retargeted TEMP triggers
	// on a still-open fork connection. Reinstall after main.schema_version moves.
	var triggers []string
	if err := tx.SelectContext(ctx, &triggers, "SELECT name FROM sqlite_temp_schema WHERE type = 'trigger' AND name GLOB 'stash_search_*'"); err != nil {
		return err
	}
	for _, name := range triggers {
		if _, err := tx.ExecContext(ctx, `DROP TRIGGER temp."`+strings.ReplaceAll(name, `"`, `""`)+`"`); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "DROP TABLE IF EXISTS temp.stash_search_changes; DROP TABLE IF EXISTS temp.stash_search_schema;"+searchChangeTriggers()); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "CREATE TEMP TABLE stash_search_schema (version INTEGER); INSERT INTO stash_search_schema VALUES (?)", schemaVersion)
	return err
}

func collectSearchChanges(ctx context.Context, tx *sqlx.Tx) (searchChanges, error) {
	var rows []struct {
		Kind string
		ID   int
	}
	// Above this limit a full rebuild is cheaper and bounds Go memory.
	if err := tx.SelectContext(ctx, &rows, "SELECT kind, id FROM temp.stash_search_changes LIMIT ?", maxSearchChanges+1); err != nil {
		return nil, err
	}
	if len(rows) > maxSearchChanges {
		return nil, fmt.Errorf("search change batch exceeds incremental limit")
	}
	changes := make(searchChanges)
	for _, row := range rows {
		if changes[row.Kind] == nil {
			changes[row.Kind] = make(map[int]struct{})
		}
		changes[row.Kind][row.ID] = struct{}{}
	}
	return changes, nil
}

func searchChangeTriggers() string {
	var sql strings.Builder
	sql.WriteString("CREATE TEMP TABLE stash_search_changes (kind TEXT NOT NULL, id INTEGER NOT NULL, PRIMARY KEY(kind, id)) WITHOUT ROWID;\n")
	trigger := func(name, event, table, body string) {
		fmt.Fprintf(&sql, "CREATE TEMP TRIGGER stash_search_%s %s ON main.%s BEGIN %s END;\n", name, event, table, body)
	}
	mark := func(table, id string) string {
		return "INSERT OR IGNORE INTO stash_search_changes VALUES ('" + table + "', " + id + ");"
	}
	for _, table := range []string{sceneTable, imageTable} {
		id := strings.TrimSuffix(table, "s") + "_id"
		trigger(table+"_insert", "AFTER INSERT", table, mark(table, "NEW.id"))
		trigger(table+"_delete", "BEFORE DELETE", table, mark(table, "OLD.id"))
		trigger(table+"_update", "AFTER UPDATE OF id, title, details", table, mark(table, "OLD.id")+mark(table, "NEW.id"))
		links := table + "_files"
		trigger(links+"_insert", "AFTER INSERT", links, mark(table, "NEW."+id))
		trigger(links+"_delete", "BEFORE DELETE", links, mark(table, "OLD."+id))
		trigger(links+"_update", "AFTER UPDATE OF "+id+", file_id", links, mark(table, "OLD."+id)+mark(table, "NEW."+id))
	}
	fileOwners := func(fileID string) string {
		return "INSERT OR IGNORE INTO stash_search_changes SELECT 'scenes', scene_id FROM scenes_files WHERE file_id = " + fileID + ";" +
			"INSERT OR IGNORE INTO stash_search_changes SELECT 'images', image_id FROM images_files WHERE file_id = " + fileID + ";"
	}
	trigger("files_update", "AFTER UPDATE OF id, basename, parent_folder_id", "files", fileOwners("OLD.id")+fileOwners("NEW.id"))
	trigger("files_delete", "BEFORE DELETE", "files", fileOwners("OLD.id"))
	for _, change := range []struct{ name, event, body string }{
		{"insert", "AFTER INSERT", fileOwners("NEW.file_id")},
		{"delete", "BEFORE DELETE", fileOwners("OLD.file_id")},
		{"update", "AFTER UPDATE", fileOwners("OLD.file_id") + fileOwners("NEW.file_id")},
	} {
		trigger("fingerprints_"+change.name, change.event, "files_fingerprints", change.body)
	}
	folderOwners := ""
	for _, table := range []string{sceneTable, imageTable} {
		id := strings.TrimSuffix(table, "s") + "_id"
		folderOwners += "INSERT OR IGNORE INTO stash_search_changes SELECT '" + table + "', " + id + " FROM " + table + "_files JOIN files ON files.id = file_id WHERE files.parent_folder_id IN (OLD.id, NEW.id);"
	}
	trigger("folders_update", "AFTER UPDATE OF id, path", "folders", folderOwners)
	trigger("markers_insert", "AFTER INSERT", "scene_markers", mark(sceneTable, "NEW.scene_id"))
	trigger("markers_delete", "BEFORE DELETE", "scene_markers", mark(sceneTable, "OLD.scene_id"))
	trigger("markers_update", "AFTER UPDATE OF scene_id, title", "scene_markers", mark(sceneTable, "OLD.scene_id")+mark(sceneTable, "NEW.scene_id"))
	return sql.String()
}
