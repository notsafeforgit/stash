package sqlite

import (
	"context"

	"github.com/jmoiron/sqlx"
)

type customMigrationFunc func(ctx context.Context, db *sqlx.DB) error

type transactionalMigrationFunc func(ctx context.Context, tx *sqlx.Tx) error

// RegisterTransactionalMigration runs a data conversion after its SQL script
// in the same transaction, including the schema version update. A failure or
// interruption rolls the entire upgrade back so it can be retried normally.
func RegisterTransactionalMigration(schemaVersion uint, fn transactionalMigrationFunc) {
	transactionalMigrations[schemaVersion] = fn
}

var transactionalMigrations = make(map[uint]transactionalMigrationFunc)

type newDatabaseMigrationKey struct{}

// IsNewDatabaseMigration distinguishes schema creation from importing an old
// library. Historical config rewrites must not run against a modern config
// merely because the user selected a new database path.
func IsNewDatabaseMigration(ctx context.Context) bool {
	value, _ := ctx.Value(newDatabaseMigrationKey{}).(bool)
	return value
}

func RegisterPostMigration(schemaVersion uint, fn customMigrationFunc) {
	v := postMigrations[schemaVersion]
	v = append(v, fn)
	postMigrations[schemaVersion] = v
}

func RegisterPreMigration(schemaVersion uint, fn customMigrationFunc) {
	v := preMigrations[schemaVersion]
	v = append(v, fn)
	preMigrations[schemaVersion] = v
}

var postMigrations = make(map[uint][]customMigrationFunc)
var preMigrations = make(map[uint][]customMigrationFunc)
