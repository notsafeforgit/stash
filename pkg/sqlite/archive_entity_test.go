package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

const archiveIdentityFixture = `
INSERT INTO performers(id, created_at, updated_at) VALUES
 (71, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP), (72, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
INSERT INTO performer_names(performer_id, name, position) VALUES (71, 'Shared name', 0), (72, 'Shared name', 0);
INSERT INTO scenes(id, title, created_at, updated_at) VALUES
 (31, 'Kept title', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP), (32, 'Kept title', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
INSERT INTO images(id, title, created_at, updated_at) VALUES (41, 'Kept image', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
INSERT INTO folders(id, path, basename, mod_time, created_at, updated_at) VALUES
 (1, '/identity-fixture', 'identity-fixture', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
INSERT INTO files(id, parent_folder_id, basename, size, mod_time, created_at, updated_at) VALUES
 (21, 1, 'video.mp4', 1000, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
INSERT INTO performers_scenes(performer_id, scene_id) VALUES (71, 31);
`

func archiveTestDatabase(t *testing.T) (*sqlite.Database, models.Repository) {
	t.Helper()
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "identities.sqlite")
	writeEmptyNativeFixture(t, path)
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(path))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, archiveIdentityFixture, nil)
		return err
	}))
	return db, repo
}

func archiveFind(t *testing.T, repo models.Repository, kind models.ArchiveEntityKind, id int) *models.ArchiveEntity {
	t.Helper()
	var ret *models.ArchiveEntity
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		var err error
		ret, err = repo.ArchiveEntity.FindByLocalID(ctx, kind, id)
		return err
	}))
	require.NotNil(t, ret)
	return ret
}

func TestArchiveIdentityMigrationPreservesLibraryRows(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "before-identities.sqlite")
	buildLegacyDatabase(t, path, 86, true)
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	m, err := sqlite.NewMigrator(db)
	require.NoError(t, err)
	for version := m.CurrentSchemaVersion(); version < sqlite.NativeSchemaBaseline+3; version = m.CurrentSchemaVersion() {
		require.NoError(t, m.RunMigration(context.Background(), m.GetNextMigrationVersion(version)))
	}
	m.Close()
	raw := openRawDB(t, path)
	_, err = raw.Exec(archiveIdentityFixture)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	defer db.Close()
	repo := db.Repository()
	seen := make(map[string]bool)
	for kind, ids := range map[models.ArchiveEntityKind][]int{
		models.ArchivePerformer: {71, 72}, models.ArchiveScene: {31, 32},
		models.ArchiveImage: {41}, models.ArchiveFile: {21},
	} {
		for _, id := range ids {
			entity := archiveFind(t, repo, kind, id)
			require.Equal(t, models.ArchiveEntityActive, entity.State)
			require.Equal(t, 1, entity.Revision)
			require.Equal(t, id, *entity.LocalID)
			parsed, err := uuid.Parse(entity.UUID)
			require.NoError(t, err)
			require.Equal(t, uuid.Version(4), parsed.Version())
			require.False(t, seen[entity.UUID], "identical names/titles must not collapse identities")
			seen[entity.UUID] = true
		}
	}
	raw = openRawDB(t, path)
	defer raw.Close()
	require.Equal(t, uint(6), queryUint(t, raw, "SELECT count(*) FROM archive_entities"))
	require.Equal(t, uint(2), queryUint(t, raw, "SELECT count(*) FROM scenes WHERE title='Kept title'"))
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM performers_scenes WHERE performer_id=71 AND scene_id=31"))
	for _, column := range []string{"performer_id", "scene_id", "image_id", "file_id"} {
		var id, parent, unused int
		var plan string
		require.NoError(t, raw.QueryRow("EXPLAIN QUERY PLAN SELECT * FROM archive_entities WHERE "+column+"=1").Scan(&id, &parent, &unused, &plan))
		require.Contains(t, plan, "USING INDEX archive_entities_"+strings.TrimSuffix(column, "_id"))
	}
}

func TestArchiveIdentityEditsRestartAndIDReuse(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	ctx := context.Background()
	old := archiveFind(t, repo, models.ArchiveScene, 31)
	require.NoError(t, repo.WithTxn(ctx, func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, "UPDATE scenes SET title='Edited title' WHERE id=31", nil)
		return err
	}))
	edited := archiveFind(t, repo, models.ArchiveScene, 31)
	require.Equal(t, old.UUID, edited.UUID)
	require.Greater(t, edited.Revision, old.Revision)
	path := db.DatabasePath()
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(path))
	require.Equal(t, edited, archiveFind(t, db.Repository(), models.ArchiveScene, 31))
	require.NoError(t, repo.WithTxn(ctx, func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `DELETE FROM scenes WHERE id=31;
INSERT INTO scenes(id, title, created_at, updated_at) VALUES (31, 'Different item', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, nil)
		return err
	}))
	replacement := archiveFind(t, repo, models.ArchiveScene, 31)
	require.NotEqual(t, old.UUID, replacement.UUID)
	require.NoError(t, repo.WithReadTxn(ctx, func(ctx context.Context) error {
		deleted, err := repo.ArchiveEntity.Resolve(ctx, old.UUID)
		require.NoError(t, err)
		require.Equal(t, models.ArchiveEntityDeleted, deleted.State)
		require.Nil(t, deleted.LocalID)
		require.Equal(t, 31, *deleted.OriginalID)
		require.NotNil(t, deleted.RetiredAt)
		return nil
	}))
}

func TestArchiveIdentityMergeAndCatalogUUIDAdoption(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	ctx := context.Background()
	source := archiveFind(t, repo, models.ArchivePerformer, 71)
	require.NoError(t, repo.WithTxn(ctx, func(ctx context.Context) error {
		return repo.Performer.Merge(ctx, []int{71}, 72)
	}))
	catalogUUID := "09913370-244c-43c9-aee4-f230905da543"
	destination := archiveFind(t, repo, models.ArchivePerformer, 72)
	require.NoError(t, repo.WithTxn(ctx, func(ctx context.Context) error {
		_, err := repo.ArchiveEntity.AdoptUUID(ctx, destination.UUID, catalogUUID, destination.Revision)
		return err
	}))
	require.Equal(t, catalogUUID, archiveFind(t, repo, models.ArchivePerformer, 72).UUID)
	require.NoError(t, repo.WithReadTxn(ctx, func(ctx context.Context) error {
		for _, value := range []string{source.UUID, destination.UUID, catalogUUID} {
			resolved, err := repo.ArchiveEntity.Resolve(ctx, value)
			require.NoError(t, err)
			require.Equal(t, catalogUUID, resolved.UUID)
			require.Equal(t, 72, *resolved.LocalID)
		}
		return nil
	}))
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM performers_scenes WHERE performer_id=72 AND scene_id=31"))
	// Deleting the survivor does not make any of its old UUIDs resolve to a
	// subsequently reused local ID.
	require.NoError(t, repo.WithTxn(ctx, func(ctx context.Context) error { return repo.Performer.Destroy(ctx, 72) }))
	require.NoError(t, repo.WithReadTxn(ctx, func(ctx context.Context) error {
		ret, err := repo.ArchiveEntity.Resolve(ctx, source.UUID)
		require.NoError(t, err)
		require.Equal(t, models.ArchiveEntityDeleted, ret.State)
		return nil
	}))
}

func TestArchiveIdentityRejectsStaleConflictingAndCyclicChanges(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	ctx := context.Background()
	p := archiveFind(t, repo, models.ArchivePerformer, 71)
	other := archiveFind(t, repo, models.ArchivePerformer, 72)
	scene := archiveFind(t, repo, models.ArchiveScene, 31)
	for _, tc := range []struct {
		value    string
		revision int
	}{
		{other.UUID, p.Revision}, {uuid.NewString(), p.Revision - 1},
	} {
		err := repo.WithTxn(ctx, func(ctx context.Context) error {
			_, err := repo.ArchiveEntity.AdoptUUID(ctx, p.UUID, tc.value, tc.revision)
			return err
		})
		require.ErrorIs(t, err, models.ErrArchiveIdentityConflict)
	}
	err := repo.WithTxn(ctx, func(ctx context.Context) error {
		return repo.ArchiveEntity.Redirect(ctx, p.UUID, scene.UUID, p.Revision)
	})
	require.ErrorContains(t, err, "same kind")
	require.NoError(t, repo.WithTxn(ctx, func(ctx context.Context) error {
		_, err := repo.ArchiveEntity.AdoptUUID(ctx, p.UUID, "54fdc03a-3b7b-4c9b-8892-603a1b2d004b", p.Revision)
		return err
	}))
	current := archiveFind(t, repo, models.ArchivePerformer, 71)
	err = repo.WithTxn(ctx, func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `UPDATE archive_entities SET performer_id=NULL, state='redirected', redirect_to=?, retired_at=CURRENT_TIMESTAMP WHERE uuid=?`, []interface{}{p.UUID, current.UUID})
		return err
	})
	require.ErrorContains(t, err, "redirect cycle")
	err = repo.WithTxn(ctx, func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, "UPDATE archive_entities SET redirect_to=? WHERE uuid=?", []interface{}{scene.UUID, p.UUID})
		return err
	})
	require.ErrorContains(t, err, "kinds differ")
	require.Equal(t, current, archiveFind(t, repo, models.ArchivePerformer, 71))
}

func TestArchiveIdentityAdoptionFailureRollsBackAndScenesRetainRedirect(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	ctx := context.Background()
	source := archiveFind(t, repo, models.ArchiveScene, 31)
	destination := archiveFind(t, repo, models.ArchiveScene, 32)
	err := repo.WithTxn(ctx, func(ctx context.Context) error {
		return repo.ArchiveEntity.Redirect(ctx, source.UUID, destination.UUID, source.Revision)
	})
	require.ErrorContains(t, err, "source record without a current identity")
	require.Equal(t, source, archiveFind(t, repo, models.ArchiveScene, 31))
	imported := uuid.NewString()
	err = repo.WithTxn(ctx, func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `CREATE TRIGGER fail_identity_alias BEFORE INSERT ON archive_entities
WHEN NEW.state='redirected' BEGIN SELECT RAISE(ABORT, 'injected identity failure'); END`, nil)
		if err != nil {
			return err
		}
		_, err = repo.ArchiveEntity.AdoptUUID(ctx, source.UUID, imported, source.Revision)
		return err
	})
	require.ErrorContains(t, err, "injected identity failure")
	require.Equal(t, source, archiveFind(t, repo, models.ArchiveScene, 31))
	require.NoError(t, repo.WithTxn(ctx, func(ctx context.Context) error {
		if err := repo.Scene.RedirectMergedIdentities(ctx, []int{31}, 32); err != nil {
			return err
		}
		return repo.Scene.Destroy(ctx, 31)
	}))
	require.NoError(t, repo.WithReadTxn(ctx, func(ctx context.Context) error {
		resolved, err := repo.ArchiveEntity.Resolve(ctx, source.UUID)
		require.NoError(t, err)
		require.Equal(t, destination.UUID, resolved.UUID)
		absent, err := repo.ArchiveEntity.Find(ctx, imported)
		require.NoError(t, err)
		require.Nil(t, absent)
		return nil
	}))
}
