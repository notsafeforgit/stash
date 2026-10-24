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

// The disposable cache has its own format version. Persistent source revisions
// validate reuse across processes; owner/revision fence in-process snapshots.
const searchIndexSchema = `
CREATE TABLE IF NOT EXISTS snapshot (id INTEGER PRIMARY KEY CHECK(id = 1), owner TEXT, revision INTEGER, source_token TEXT, source_schema INTEGER);
CREATE VIRTUAL TABLE IF NOT EXISTS scenes USING fts5(text, tokenize='trigram', detail='none');
CREATE VIRTUAL TABLE IF NOT EXISTS images USING fts5(text, tokenize='trigram', detail='none');
PRAGMA user_version = 2;`

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
	var objects int
	if err == nil {
		err = index.Get(&objects, "SELECT count(*) FROM sqlite_schema WHERE type='table' AND name IN ('snapshot','scenes','images')")
	}
	if err == nil && version != 0 && version != 1 && version != 2 {
		err = fmt.Errorf("unsupported search cache version %d", version)
	}
	if err == nil && version == 1 {
		_, err = index.Exec(`BEGIN; ALTER TABLE snapshot ADD COLUMN source_token TEXT;
ALTER TABLE snapshot ADD COLUMN source_schema INTEGER; PRAGMA user_version=2; COMMIT;`)
	}
	if err == nil {
		_, err = index.Exec(searchIndexSchema)
	}
	if err == nil && objects != 3 {
		// IF NOT EXISTS can repair missing virtual tables; their old stamp
		// must not authorize the newly empty tables as a complete index.
		_, err = index.Exec("DELETE FROM snapshot")
	}
	if err != nil {
		index.Close()
		return nil, err
	}
	return index, nil
}

type searchSourceRevision struct {
	Token  string
	Schema int
}

// Keep transaction cleanup synchronous with the search worker. database/sql's
// automatic rollback on context cancellation can otherwise still be running
// after the worker exits, leaving a reader alive while the library closes.
// Pool acquisition and every query retain the caller's cancellation context.
func beginSearchTransaction(ctx context.Context, db *sqlx.DB) (*sqlx.Tx, func(), error) {
	conn, err := db.Connx(ctx)
	if err != nil {
		return nil, nil, err
	}
	tx, err := conn.BeginTxx(context.WithoutCancel(ctx), nil)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	return tx, func() {
		_ = tx.Rollback()
		_ = conn.Close()
	}, nil
}

func searchRevisionIn(ctx context.Context, tx *sqlx.Tx) (searchSourceRevision, error) {
	var source searchSourceRevision
	if err := tx.GetContext(ctx, &source.Token, "SELECT token FROM library_search_state WHERE id=1"); err != nil {
		return source, err
	}
	err := tx.GetContext(ctx, &source.Schema, "PRAGMA main.schema_version")
	return source, err
}

func (a *readAcceleration) reuseSearchIndex(ctx context.Context) error {
	tx, finish, err := beginSearchTransaction(ctx, a.main)
	if err != nil {
		return err
	}
	defer finish()
	a.mu.Lock()
	defer a.mu.Unlock()
	snapshot := a.pin(ctx, tx)
	if snapshot == nil {
		return fmt.Errorf("library changed while validating search index")
	}
	source, err := searchRevisionIn(ctx, tx)
	if err != nil {
		return err
	}
	result, err := a.index.ExecContext(ctx, `UPDATE snapshot SET owner=?, revision=?
WHERE id=1 AND source_token=? AND source_schema=?`, a.owner, snapshot.searchRevision, source.Token, source.Schema)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 1 {
		a.fullRebuild = false
		logger.Info("Search index reused at its validated library revision")
	}
	return err
}

func (a *readAcceleration) refreshSearchIndex(ctx context.Context) error {
	started := time.Now()
	tx, finish, err := beginSearchTransaction(ctx, a.main)
	if err != nil {
		return err
	}
	defer finish()
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
	source, err := searchRevisionIn(ctx, tx)
	if err != nil {
		return err
	}
	indexTx, finishIndex, err := beginSearchTransaction(ctx, a.index)
	if err != nil {
		return err
	}
	defer finishIndex()
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
	if _, err := indexTx.ExecContext(ctx, "INSERT OR REPLACE INTO snapshot (id, owner, revision, source_token, source_schema) VALUES (1, ?, ?, ?, ?)", a.owner, snapshot.searchRevision, source.Token, source.Schema); err != nil {
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
	tx, finish, err := beginSearchTransaction(ctx, a.index)
	if err != nil {
		return nil, false
	}
	defer finish()
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
