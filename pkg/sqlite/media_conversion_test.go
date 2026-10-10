package sqlite_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/file"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/jsonschema"
	"github.com/stashapp/stash/pkg/models/paths"
	"github.com/stashapp/stash/pkg/scene"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeMediaConversionSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeSourceSiteConcurrencySchema(t, raw)
	if queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='media_conversions'") == 0 {
		return
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM media_conversions"), "older fixtures cannot represent a completed conversion")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM scenes_o_dates WHERE o_date IS NULL"), "older fixtures cannot represent undated counts")
	_, err := raw.Exec(`DROP TRIGGER archive_entity_redirect_update;
 DROP TRIGGER source_gallery_scenes_galleries_insert;
 DROP TABLE media_conversions;
 CREATE TABLE scenes_o_dates_old(scene_id INTEGER NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,o_date DATETIME NOT NULL);
 INSERT INTO scenes_o_dates_old SELECT scene_id,o_date FROM scenes_o_dates;
 DROP TABLE scenes_o_dates;
 ALTER TABLE scenes_o_dates_old RENAME TO scenes_o_dates;
 CREATE INDEX index_scenes_o_dates ON scenes_o_dates(scene_id);
 DELETE FROM native_migration_history WHERE version=1000109;`)
	require.NoError(t, err)
	for _, old := range []struct{ path, name string }{
		{"migrations/1000011_metadata_identities.up.sql", "archive_entity_redirect_update"},
		{"migrations/1000010_source_galleries.up.sql", "source_gallery_scenes_galleries_insert"},
	} {
		body, err := os.ReadFile(old.path)
		require.NoError(t, err)
		start := strings.Index(string(body), "CREATE TRIGGER "+old.name+" ")
		require.NotEqual(t, -1, start)
		definition := string(body[start:])
		end := strings.Index(definition, "\nEND;")
		require.NotEqual(t, -1, end)
		_, err = raw.Exec(definition[:end+len("\nEND;")])
		require.NoError(t, err)
	}
}

func TestSceneMergeRetainsUndatedCounterWithoutInventedEvents(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	date := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	generated := paths.NewPaths(t.TempDir(), t.TempDir())
	service := scene.Service{File: repo.File, Repository: repo.Scene, MarkerRepository: repo.SceneMarker,
		Paths: &generated, Config: config.GetInstance()}
	deleter := &scene.FileDeleter{Deleter: file.NewDeleter(), Paths: &generated}
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		require.NoError(t, repo.Scene.AddUndatedO(ctx, 31, 2))
		require.NoError(t, repo.Scene.AddUndatedO(ctx, 32, 3))
		_, err := repo.Scene.AddO(ctx, 32, []time.Time{date})
		require.NoError(t, err)
		deleter.RegisterHooks(ctx)
		return service.Merge(ctx, []int{32}, 31, deleter, scene.MergeOptions{IncludeOHistory: true, ScenePartial: models.NewScenePartial()})
	}))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		count, err := repo.Scene.GetOCount(ctx, 31)
		require.NoError(t, err)
		require.Equal(t, 6, count)
		dates, err := repo.Scene.GetODates(ctx, 31)
		require.NoError(t, err)
		require.Equal(t, []time.Time{date}, dates)
		return nil
	}))
}

func TestMediaConversionMigrationPreservesDatedHistory(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	dates := []time.Time{time.Date(2025, 1, 1, 2, 0, 0, 0, time.UTC), time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)}
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.Scene.AddO(ctx, 31, dates)
		return err
	}))
	path := db.DatabasePath()
	require.NoError(t, db.Close())
	raw := openRawDB(t, path)
	removeMediaConversionSchema(t, raw)
	_, err := raw.Exec("UPDATE schema_migrations SET version=1000108")
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	require.Error(t, db.Open(path))
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	repo = db.Repository()
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		actual, err := repo.Scene.GetODates(ctx, 31)
		require.NoError(t, err)
		require.ElementsMatch(t, dates, actual)
		return nil
	}))
}

func TestSceneJSONRetainsUndatedCounterSeparatelyFromHistory(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	db.SetBlobStoreOptions(sqlite.BlobStoreOptions{UseDatabase: true})
	date := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		require.NoError(t, repo.Scene.AddUndatedO(ctx, 31, 3))
		_, err := repo.Scene.AddO(ctx, 31, []time.Time{date})
		require.NoError(t, err)
		original, err := repo.Scene.Find(ctx, 31)
		require.NoError(t, err)
		require.NoError(t, original.LoadURLs(ctx, repo.Scene))
		require.NoError(t, original.LoadFiles(ctx, repo.Scene))
		require.NoError(t, original.LoadStashIDs(ctx, repo.Scene))
		exported, err := scene.ToBasicJSON(ctx, repo.Scene, original)
		require.NoError(t, err)
		require.Equal(t, 3, exported.UndatedOCount)
		require.Len(t, exported.OHistory, 1)
		body, err := json.Marshal(exported)
		require.NoError(t, err)
		var decoded jsonschema.Scene
		require.NoError(t, json.Unmarshal(body, &decoded))
		importer := scene.Importer{ReaderWriter: repo.Scene, FileFinder: repo.File, Input: decoded, ID: 32}
		require.NoError(t, importer.PreImport(ctx))
		require.NoError(t, importer.PostImport(ctx, 32))
		count, err := repo.Scene.GetOCount(ctx, 32)
		require.NoError(t, err)
		require.Equal(t, 4, count)
		dates, err := repo.Scene.GetODates(ctx, 32)
		require.NoError(t, err)
		require.Equal(t, []time.Time{date}, dates)
		return nil
	}))
}
