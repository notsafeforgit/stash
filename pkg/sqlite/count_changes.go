package sqlite

import (
	"context"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
	"github.com/mattn/go-sqlite3"
)

// SQLite's compiler identifies actual read dependencies, including subqueries
// and views. Do not infer them by parsing SQL or maintaining a filter allowlist.
// The observer is private to read acceleration and used with a.mu held.
func (a *readAcceleration) countDependencies(query string) ([]string, error) {
	tables := make(map[string]bool)
	err := a.observer.Raw(func(raw interface{}) error {
		conn, ok := raw.(*CustomSQLiteConn)
		if !ok {
			return fmt.Errorf("unsupported count dependency connection")
		}
		if !a.countAuthorizerInstalled {
			conn.RegisterAuthorizer(func(op int, table, column, database string) int {
				if op == sqlite3.SQLITE_READ && a.compilingDependencies != nil {
					// SQLite may omit the database for an optimized count(*) read.
					if database != "main" && database != "" {
						return sqlite3.SQLITE_DENY
					}
					a.compilingDependencies[table] = true
				}
				return sqlite3.SQLITE_OK
			})
			a.countAuthorizerInstalled = true
		}
		a.compilingDependencies = tables
		defer func() { a.compilingDependencies = nil }()
		statement, err := conn.Prepare(query)
		if err != nil {
			return err
		}
		return statement.Close()
	})
	if err != nil {
		return nil, err
	}
	ret := make([]string, 0, len(tables))
	for table := range tables {
		ret = append(ret, table)
	}
	return ret, nil
}

func prepareCountChanges(ctx context.Context, tx *sqlx.Tx) error {
	var current, installed int
	if err := tx.GetContext(ctx, &current, "PRAGMA main.schema_version"); err != nil {
		return err
	}
	err := tx.GetContext(ctx, &installed, "SELECT version FROM temp.stash_count_schema")
	if err != nil || current != installed {
		if _, err := tx.ExecContext(ctx, "SAVEPOINT stash_count_setup"); err != nil {
			return err
		}
		if err := installCountChanges(ctx, tx, current); err != nil {
			_, _ = tx.ExecContext(ctx, "ROLLBACK TO stash_count_setup; RELEASE stash_count_setup")
			return err
		}
		if _, err := tx.ExecContext(ctx, "RELEASE stash_count_setup"); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, "DELETE FROM temp.stash_count_changes")
	return err
}

func installCountChanges(ctx context.Context, tx *sqlx.Tx, version int) error {
	var previous []string
	if err := tx.SelectContext(ctx, &previous, "SELECT name FROM sqlite_temp_schema WHERE type = 'trigger' AND name GLOB 'stash_count_*'"); err != nil {
		return err
	}
	quote := func(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
	for _, trigger := range previous {
		if _, err := tx.ExecContext(ctx, "DROP TRIGGER temp."+quote(trigger)); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS temp.stash_count_schema;
CREATE TEMP TABLE stash_count_schema(version INTEGER);
CREATE TEMP TABLE IF NOT EXISTS stash_count_changes(name TEXT PRIMARY KEY) WITHOUT ROWID;`); err != nil {
		return err
	}
	var tables []string
	if err := tx.SelectContext(ctx, &tables, "SELECT name FROM pragma_table_list WHERE schema = 'main' AND type = 'table' AND name NOT LIKE 'sqlite_%'"); err != nil {
		return err
	}
	for _, table := range tables {
		for _, event := range []string{"INSERT", "UPDATE", "DELETE"} {
			statement := "CREATE TEMP TRIGGER " + quote("stash_count_"+table+"_"+event) + " AFTER " + event + " ON main." + quote(table) + " BEGIN INSERT INTO stash_count_changes VALUES ('" + strings.ReplaceAll(table, "'", "''") + "') ON CONFLICT(name) DO NOTHING; END"
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return err
			}
		}
	}
	_, err := tx.ExecContext(ctx, "INSERT INTO temp.stash_count_schema VALUES (?)", version)
	return err
}

func collectCountChanges(ctx context.Context, tx *sqlx.Tx) ([]string, error) {
	var current, installed int
	if err := tx.GetContext(ctx, &current, "PRAGMA main.schema_version"); err != nil {
		return nil, err
	}
	if err := tx.GetContext(ctx, &installed, "SELECT version FROM temp.stash_count_schema"); err != nil {
		return nil, err
	}
	if current != installed {
		return nil, fmt.Errorf("schema changed during write")
	}
	var tables []string
	err := tx.SelectContext(ctx, &tables, "SELECT name FROM temp.stash_count_changes")
	return tables, err
}

func (a *readAcceleration) invalidateCounts(tables []string) {
	a.revision++
	changed := make(map[string]bool, len(tables))
	for _, table := range tables {
		changed[table] = true
	}
	for key, item := range a.counts {
		for _, table := range item.Value.(countCacheEntry).tables {
			if changed[table] {
				delete(a.counts, key)
				a.countOrder.Remove(item)
				break
			}
		}
	}
}
