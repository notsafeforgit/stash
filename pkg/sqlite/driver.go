package sqlite

import (
	"database/sql"
	"database/sql/driver"
	"fmt"

	"github.com/WithoutPants/sortorder/casefolded"
	sqlite3 "github.com/mattn/go-sqlite3"
	"github.com/stashapp/stash/pkg/scrape"
)

const sqlite3Driver = "sqlite3ex"
const checkpointSQLiteDriver = "sqlite3checkpoint"

func init() {
	// register custom driver
	sql.Register(sqlite3Driver, &CustomSQLiteDriver{})
	sql.Register(checkpointSQLiteDriver, &checkpointDriver{})
}

// Checkpoint connections need the same functions/collations, but closing one
// must not run PRAGMA optimize against a live source after releasing its lock.
type checkpointDriver struct{}

func (*checkpointDriver) Open(dsn string) (driver.Conn, error) {
	conn, err := (&CustomSQLiteDriver{}).Open(dsn)
	if err != nil {
		return nil, err
	}
	return conn.(*CustomSQLiteConn).SQLiteConn, nil
}

type CustomSQLiteDriver struct{}

type CustomSQLiteConn struct {
	*sqlite3.SQLiteConn
}

func (d *CustomSQLiteDriver) Open(dsn string) (driver.Conn, error) {
	sqlite3Driver := &sqlite3.SQLiteDriver{
		ConnectHook: func(conn *sqlite3.SQLiteConn) error {
			funcs := map[string]interface{}{
				"regexp":            regexFn,
				"durationToTinyInt": durationToTinyIntFn,
				"basename":          basenameFn,
				"phash_distance":    phashDistanceFn,
				"source_scope_v1":   scrape.SourceScopeV1,
			}

			for name, fn := range funcs {
				if err := conn.RegisterFunc(name, fn, true); err != nil {
					return fmt.Errorf("error registering function %s: %v", name, err)
				}
			}

			// COLLATE NATURAL_CI - Case insensitive natural sort
			err := conn.RegisterCollation("NATURAL_CI", func(s string, s2 string) int {
				if casefolded.NaturalLess(s, s2) {
					return -1
				} else if casefolded.NaturalLess(s2, s) {
					return 1
				}
				return 0
			})

			if err != nil {
				return fmt.Errorf("error registering natural sort collation: %v", err)
			}

			return nil
		},
	}

	conn, err := sqlite3Driver.Open(dsn)
	if err != nil {
		return nil, err
	}

	return &CustomSQLiteConn{conn.(*sqlite3.SQLiteConn)}, nil
}

func (c *CustomSQLiteConn) Close() error {
	conn := c.SQLiteConn

	_, _ = conn.Exec("PRAGMA analysis_limit=1000; PRAGMA optimize;", []driver.Value{})

	return conn.Close()
}
