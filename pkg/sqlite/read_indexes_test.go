package sqlite_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestReadIndexesUpstreamRoundTrip(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "read-indexes.sqlite")
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(path))
	version := db.Version()
	require.NoError(t, db.Close())

	// Plain upstream SQLite can maintain these indexes: no fork-specific
	// collations, functions, triggers, or table columns are needed to write.
	raw, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	for _, table := range []string{"scenes", "images"} {
		_, err := raw.Exec(fmt.Sprintf("INSERT INTO %s (title, created_at, updated_at) VALUES ('upstream write', '2026-01-01', '2026-01-01')", table))
		require.NoError(t, err)
		_, err = raw.Exec("UPDATE " + table + " SET title = 'upstream edit'")
		require.NoError(t, err)
		// Upstream table migrations may remove an index; roll-forward repairs it.
		_, err = raw.Exec("DROP INDEX fork_" + table + "_created_at")
		require.NoError(t, err)
	}
	require.NoError(t, raw.Close())

	for range 2 {
		db = sqlite.NewDatabase()
		require.NoError(t, db.Open(path))
		require.Equal(t, version, db.Version())
		require.NoError(t, db.Close())
	}
	raw = openRawDB(t, path)
	defer raw.Close()
	for _, table := range []string{"scenes", "images"} {
		var title, definition string
		require.NoError(t, raw.QueryRow("SELECT title FROM "+table).Scan(&title))
		require.Equal(t, "upstream edit", title)
		require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name = ?", "fork_"+table+"_created_at").Scan(&definition))
		require.Contains(t, definition, "(created_at, title)")
	}
	require.Equal(t, version, queryUint(t, raw, "SELECT version FROM schema_migrations"))
}

func TestReadIndexesPreserveTiedSortOrder(t *testing.T) {
	db, err := sql.Open("sqlite3ex", ":memory:")
	require.NoError(t, err)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE scenes(id INTEGER PRIMARY KEY, created_at TEXT, title TEXT);
CREATE INDEX fork_scenes_created_at ON scenes(created_at, title)`)
	require.NoError(t, err)
	for i := 1; i <= 90; i++ {
		titles := []any{nil, "", "Scene 2", "scene 2", "Scene 10"}
		_, err := db.Exec("INSERT INTO scenes VALUES (?, ?, ?)", i, fmt.Sprint(i%3), titles[i%len(titles)])
		require.NoError(t, err)
	}
	read := func(hint, direction string, offset int) []int {
		t.Helper()
		query := fmt.Sprintf("SELECT DISTINCT id FROM scenes %s ORDER BY created_at %s, COALESCE(title, '') COLLATE NATURAL_CI ASC, id ASC LIMIT 13 OFFSET %d", hint, direction, offset)
		rows, err := db.Query(query)
		require.NoError(t, err)
		defer rows.Close()
		var ids []int
		for rows.Next() {
			var id int
			require.NoError(t, rows.Scan(&id))
			ids = append(ids, id)
		}
		require.NoError(t, rows.Err())
		return ids
	}
	for _, direction := range []string{"ASC", "DESC"} {
		for offset := 0; offset < 90; offset += 13 {
			require.Equal(t, read("NOT INDEXED", direction, offset), read("INDEXED BY fork_scenes_created_at", direction, offset))
		}
	}
	var equal bool
	require.NoError(t, db.QueryRow("SELECT 'Scene 2' = 'scene 2' COLLATE NATURAL_CI").Scan(&equal))
	require.True(t, equal, "equal natural titles must reach the explicit ID tie-breaker")
}
