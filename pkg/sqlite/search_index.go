package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/models"
)

// The cache has its own format version and process owner. Never trust an index
// left by a previous process: upstream may have edited the library meanwhile.
const searchIndexSchema = `
CREATE TABLE IF NOT EXISTS snapshot (id INTEGER PRIMARY KEY CHECK(id = 1), owner TEXT, revision INTEGER);
CREATE VIRTUAL TABLE IF NOT EXISTS scenes USING fts5(text, tokenize='trigram', detail='none');
CREATE VIRTUAL TABLE IF NOT EXISTS images USING fts5(text, tokenize='trigram', detail='none');
PRAGMA user_version = 1;`

func openSearchIndex(path string) (*sqlx.DB, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	uri := url.URL{Scheme: "file", Path: path, RawQuery: "_journal=WAL&_sync=NORMAL&_busy_timeout=1000"}
	index, err := sqlx.Open("sqlite3", uri.String())
	if err != nil {
		return nil, err
	}
	index.SetMaxOpenConns(4)
	var version int
	err = index.Get(&version, "PRAGMA user_version")
	if err == nil && version != 0 && version != 1 {
		err = fmt.Errorf("unsupported search cache version %d", version)
	}
	if err == nil {
		_, err = index.Exec(searchIndexSchema)
	}
	if err != nil {
		index.Close()
		return nil, err
	}
	return index, nil
}

func (a *readAcceleration) refreshSearchIndex(ctx context.Context) error {
	started := time.Now()
	tx, err := a.main.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	a.mu.Lock()
	snapshot := a.pin(ctx, tx)
	full, changes := a.fullRebuild, a.changes
	if snapshot != nil {
		a.fullRebuild = false
		a.changes = make(searchChanges)
	}
	a.mu.Unlock()
	if snapshot == nil {
		return fmt.Errorf("library changed while pinning search snapshot")
	}
	if !full && len(changes) == 0 {
		return nil
	}
	indexTx, err := a.index.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = indexTx.Rollback() }()
	for _, table := range []string{sceneTable, imageTable} {
		if full {
			// Recreating the virtual table avoids deleting old postings one by one.
			if _, err := indexTx.ExecContext(ctx, "DROP TABLE "+table+"; CREATE VIRTUAL TABLE "+table+" USING fts5(text, tokenize='trigram', detail='none')"); err != nil {
				return err
			}
			pageQuery := "SELECT id FROM " + table + " ORDER BY id LIMIT 512"
			var pageArgs []interface{}
			for {
				var ids []int
				if err := tx.SelectContext(ctx, &ids, pageQuery, pageArgs...); err != nil {
					return err
				}
				if len(ids) == 0 {
					break
				}
				if err := writeSearchDocuments(ctx, tx, indexTx, table, ids, false); err != nil {
					return err
				}
				pageQuery = "SELECT id FROM " + table + " WHERE id > ? ORDER BY id LIMIT 512"
				pageArgs = []interface{}{ids[len(ids)-1]}
			}
		} else {
			ids := make([]int, 0, len(changes[table]))
			for id := range changes[table] {
				ids = append(ids, id)
			}
			sort.Ints(ids)
			for start := 0; start < len(ids); start += 512 {
				if err := writeSearchDocuments(ctx, tx, indexTx, table, ids[start:min(start+512, len(ids))], true); err != nil {
					return err
				}
			}
		}
	}
	if _, err := indexTx.ExecContext(ctx, "INSERT OR REPLACE INTO snapshot (id, owner, revision) VALUES (1, ?, ?)", a.owner, snapshot.searchRevision); err != nil {
		return err
	}
	if err := indexTx.Commit(); err != nil {
		return err
	}
	if full {
		logger.Infof("Search index rebuilt in %s", time.Since(started).Round(time.Millisecond))
	}
	return nil
}

func writeSearchDocuments(ctx context.Context, source, target *sqlx.Tx, table string, ids []int, replace bool) error {
	encoded, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	arg := string(encoded)
	if replace {
		if _, err := target.ExecContext(ctx, "DELETE FROM "+table+" WHERE rowid IN (SELECT value FROM json_each(?))", arg); err != nil {
			return err
		}
	}
	id := strings.TrimSuffix(table, "s") + "_id"
	selected := " IN (SELECT value FROM json_each(?))"
	// Separate source scans avoid multiplying files, fingerprints and markers.
	parts := []string{
		"SELECT id, COALESCE(title, '') || char(10) || COALESCE(details, '') AS text FROM " + table + " WHERE id" + selected,
		"SELECT " + id + ", folders.path || '" + string(filepath.Separator) + "' || files.basename FROM " + table + "_files JOIN files ON file_id = files.id JOIN folders ON files.parent_folder_id = folders.id WHERE " + id + selected,
		"SELECT " + id + ", fingerprint FROM " + table + "_files JOIN files_fingerprints USING(file_id) WHERE " + id + selected,
	}
	args := []interface{}{arg, arg, arg}
	if table == sceneTable {
		parts = append(parts, "SELECT scene_id, title FROM scene_markers WHERE scene_id"+selected)
		args = append(args, arg)
	}
	rows, err := source.QueryxContext(ctx, strings.Join(parts, " UNION ALL ")+" ORDER BY 1", args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	insert, err := target.PrepareContext(ctx, "INSERT INTO "+table+" (rowid, text) VALUES (?, ?)")
	if err != nil {
		return err
	}
	defer insert.Close()
	var document strings.Builder
	lastID, haveID := 0, false
	for rows.Next() {
		var id int
		var text *string
		if err := rows.Scan(&id, &text); err != nil {
			return err
		}
		if haveID && id != lastID {
			if _, err := insert.ExecContext(ctx, lastID, document.String()); err != nil {
				return err
			}
			document.Reset()
		}
		lastID, haveID = id, true
		if text != nil {
			// LIKE stops at NUL. Do not let one field hide later source fields.
			document.WriteString(strings.ReplaceAll(*text, "\x00", "\n"))
			document.WriteByte('\n')
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if haveID {
		_, err = insert.ExecContext(ctx, lastID, document.String())
	}
	return err
}

func indexedSearchTerms(search string) []string {
	usable := func(term string) bool {
		if !utf8.ValidString(term) || strings.ContainsRune(term, 0) {
			return false
		}
		for _, literal := range strings.FieldsFunc(term, func(r rune) bool { return r == '%' || r == '_' }) {
			if utf8.RuneCountInString(literal) >= 3 {
				return true
			}
		}
		return false
	}
	spec := models.ParseSearchString(search)
	for _, term := range spec.MustHave {
		if usable(term) {
			return []string{term}
		}
	}
	for _, set := range spec.AnySets {
		allUsable := len(set) > 0
		for _, term := range set {
			allUsable = allUsable && usable(term)
		}
		if allUsable {
			return set
		}
	}
	return nil
}

// Returns a candidate superset only. Final SQL still checks each original
// joined row, including negative terms, wildcards, Unicode and filter criteria.
func indexedMediaCandidates(ctx context.Context, table, search string) ([]int, bool) {
	snapshot := snapshotFromContext(ctx)
	terms := indexedSearchTerms(search)
	if snapshot == nil || snapshot.cache.index == nil || len(terms) == 0 {
		return nil, false
	}
	a := snapshot.cache
	tx, err := a.index.BeginTxx(ctx, nil)
	if err != nil {
		return nil, false
	}
	defer func() { _ = tx.Rollback() }()
	var version struct {
		Owner    string
		Revision uint64
	}
	if err := tx.GetContext(ctx, &version, "SELECT owner, revision FROM snapshot WHERE id = 1"); err != nil || version.Owner != a.owner || version.Revision != snapshot.searchRevision {
		return nil, false
	}
	var clauses []string
	var args []interface{}
	for _, term := range terms {
		// UNION keeps each LIKE independently indexable (an OR expression can
		// cause SQLite to scan the entire FTS table).
		clauses = append(clauses, "SELECT rowid AS id FROM "+table+" WHERE text LIKE ?")
		args = append(args, like(term))
	}
	var ids []int
	err = tx.SelectContext(ctx, &ids, fmt.Sprintf("SELECT DISTINCT id FROM (%s) LIMIT %d", strings.Join(clauses, " UNION ALL "), mediaSearchCandidateLimit+1), args...)
	return ids, err == nil
}
