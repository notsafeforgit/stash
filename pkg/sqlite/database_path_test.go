package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestDatabaseLiteralPathsSurviveReopenAndClosedBackup(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"library #first.sqlite", "library #second.sqlite", "library %2F.sqlite", "library & café.sqlite"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(directory, name)
			writeEmptyNativeFixture(t, path)
			db := sqlite.NewDatabase()
			t.Cleanup(func() { require.NoError(t, db.Close()) })
			require.NoError(t, db.Open(path))
			attachmentSQL(t, db, "INSERT INTO scenes(id,title,created_at,updated_at) VALUES(31,?,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)", name)
			require.NoError(t, db.Close())

			// Read the named file independently. A URI fragment must not send
			// the write to a shared, truncated filename outside that file.
			raw := openRawDB(t, path)
			var title string
			require.NoError(t, raw.QueryRow("SELECT title FROM scenes WHERE id=31").Scan(&title))
			require.Equal(t, name, title)
			require.NoError(t, raw.Close())

			backup := filepath.Join(directory, "backup "+name)
			require.NoError(t, db.Backup(backup), "a closed database must reopen the same literal path for backup")
			for _, selected := range []string{path, backup} {
				require.NoError(t, db.Open(selected))
				repo := db.Repository()
				require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
					scene, err := repo.Scene.Find(ctx, 31)
					require.NoError(t, err)
					require.NotNil(t, scene)
					require.Equal(t, name, scene.Title)
					return nil
				}))
				require.NoError(t, db.Close())
			}
		})
	}
	require.NoFileExists(t, filepath.Join(directory, "library "), "opening a literal hash must not create a truncated database")
}
