package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func pathFence(t *testing.T, repo models.Repository, path string, sensitive bool) *models.FilePathFence {
	t.Helper()
	var ret *models.FilePathFence
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.FilePath.Snapshot(ctx, path, sensitive)
		return err
	}))
	return ret
}

func TestFilePathFenceRetainsRemovalsAcrossRecreationAndRollback(t *testing.T) {
	f := newFileContentFixture(t)
	path := f.file.Base().Path
	before := pathFence(t, f.repo, path, true)
	require.Zero(t, before.Revision)
	rollback := errors.New("rollback fixture")
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		if err := f.repo.File.Destroy(ctx, f.file.Base().ID); err != nil {
			return err
		}
		require.ErrorIs(t, f.repo.FilePath.Check(ctx, *before), models.ErrFilePathChanged)
		return rollback
	})
	require.ErrorIs(t, err, rollback)
	require.Equal(t, before, pathFence(t, f.repo, path, true))
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error { return f.repo.File.Destroy(ctx, f.file.Base().ID) }))
	removed := pathFence(t, f.repo, path, true)
	require.EqualValues(t, 1, removed.Revision)
	replacement := f.addFile(t, "first.mp4", []byte("new contents"))
	require.Equal(t, removed, pathFence(t, f.repo, path, true), "recreation never resets the removal history")
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error { return f.repo.File.Destroy(ctx, replacement.Base().ID) }))
	require.EqualValues(t, 2, pathFence(t, f.repo, path, true).Revision)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err = raw.Exec("UPDATE file_path_fences SET revision=1")
	require.ErrorContains(t, err, "must advance")
}

func TestFilePathFenceTracksFileAndFolderMovesWithoutMetadataNoise(t *testing.T) {
	f := newFileContentFixture(t)
	original := f.file.Base().Path
	base := f.file.Base()
	base.Size++
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error { return f.repo.File.Update(ctx, f.file) }))
	require.Zero(t, pathFence(t, f.repo, original, true).Revision, "content changes use file generations, not removal history")
	base.Basename = "renamed.mp4"
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error { return f.repo.File.Update(ctx, f.file) }))
	renamed := filepath.Join(filepath.Dir(original), base.Basename)
	require.EqualValues(t, 1, pathFence(t, f.repo, original, true).Revision)
	require.Zero(t, pathFence(t, f.repo, renamed, true).Revision)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := f.db.ExecSQL(ctx, "UPDATE folders SET path=path||'-moved' WHERE id=?", []interface{}{base.ParentFolderID})
		return err
	}))
	require.EqualValues(t, 1, pathFence(t, f.repo, renamed, true).Revision)
	require.Zero(t, pathFence(t, f.repo, filepath.Join(filepath.Dir(original)+"-moved", base.Basename), true).Revision)
}

func TestFilePathFenceCaseInsensitiveLookupsSumSpellingHistories(t *testing.T) {
	f := newFileContentFixture(t)
	first := f.file.Base().Path
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error { return f.repo.File.Destroy(ctx, f.file.Base().ID) }))
	upper := f.addFile(t, "FIRST.MP4", []byte("upper"))
	before := pathFence(t, f.repo, first, false)
	require.EqualValues(t, 1, before.Revision)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error { return f.repo.File.Destroy(ctx, upper.Base().ID) }))
	require.EqualValues(t, 2, pathFence(t, f.repo, first, false).Revision, "max(revision) would miss this second spelling's first removal")
	require.EqualValues(t, 1, pathFence(t, f.repo, first, true).Revision)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	rows, err := raw.Query("EXPLAIN QUERY PLAN SELECT coalesce(sum(revision),0) FROM file_path_fences WHERE folder_path=? COLLATE NOCASE AND basename=? COLLATE NOCASE", filepath.Dir(first), filepath.Base(first))
	require.NoError(t, err)
	defer rows.Close()
	var plan string
	for rows.Next() {
		var a, b, c int
		var detail string
		require.NoError(t, rows.Scan(&a, &b, &c, &detail))
		plan += detail
	}
	require.NoError(t, rows.Err())
	require.Contains(t, plan, "SEARCH file_path_fences USING COVERING INDEX file_path_fences_folded")
}

func TestFilePathFenceAnonymisationRemovesPathsCapturedByRewriteTriggers(t *testing.T) {
	f := newFileContentFixture(t)
	out := filepath.Join(t.TempDir(), "anonymous-paths.sqlite")
	anonymiser, err := sqlite.NewAnonymiser(f.db, out)
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(t.Context()))
	raw := openRawDB(t, out)
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM file_path_fences"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestFilePathFenceMigrationAndStartupGuards(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema19.sqlite")
	buildLegacyDatabase(t, path, 86, true)
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	m, err := sqlite.NewMigrator(db)
	require.NoError(t, err)
	for version := m.CurrentSchemaVersion(); version < sqlite.NativeSchemaBaseline+19; version = m.CurrentSchemaVersion() {
		require.NoError(t, m.RunMigration(t.Context(), m.GetNextMigrationVersion(version)))
	}
	m.Close()
	raw := openRawDB(t, path)
	defer raw.Close()
	_, err = raw.Exec(`INSERT INTO folders(id,path,basename,mod_time,created_at,updated_at)
VALUES(1,'/path-fixture','path-fixture',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO files(id,parent_folder_id,basename,size,mod_time,created_at,updated_at)
VALUES(21,1,'retained.mp4',1000,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	defer db.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM file_path_fences"), "migration cannot fabricate old removal history")
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT generation FROM files WHERE id=21"))
	repo := db.Repository()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return db.File.Destroy(ctx, 21) }))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM file_path_fences"))
	for _, name := range []string{"file_path_removed", "file_path_moved", "file_path_folder_moved", "file_path_fences_folded"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "incomplete.sqlite")
			writeEmptyNativeFixture(t, path)
			raw := openRawDB(t, path)
			kind := "TRIGGER"
			if name == "file_path_fences_folded" {
				kind = "INDEX"
			}
			_, err := raw.Exec("DROP " + kind + " " + name)
			require.NoError(t, err)
			require.NoError(t, raw.Close())
			db := sqlite.NewDatabase()
			require.ErrorContains(t, db.Open(path), "missing "+name)
		})
	}
}
