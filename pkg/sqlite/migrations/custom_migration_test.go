package migrations

import (
	"context"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
)

func TestCustomMigrationReturnsCommitFailure(t *testing.T) {
	db, err := sqlx.Open("sqlite3ex", ":memory:?_fk=true")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE parent (id INTEGER PRIMARY KEY);
CREATE TABLE child (parent_id INTEGER REFERENCES parent(id) DEFERRABLE INITIALLY DEFERRED);`)
	require.NoError(t, err)
	m := migrator{db: db}
	err = m.withTxn(context.Background(), func(tx *sqlx.Tx) error {
		_, err := tx.Exec("INSERT INTO child VALUES (42)")
		return err // the deferred constraint only fails when the commit executes
	})
	require.ErrorContains(t, err, "FOREIGN KEY constraint failed")
	var count int
	require.NoError(t, db.Get(&count, "SELECT COUNT(*) FROM child"))
	require.Zero(t, count)
}
