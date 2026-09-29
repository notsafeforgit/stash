package api

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/file"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stashapp/stash/pkg/scene"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

type notificationCall struct {
	id     int
	kind   hook.TriggerEnum
	input  interface{}
	fields []string
}

type notificationRecorder struct {
	calls []notificationCall
	check func(context.Context, notificationCall)
}

func (*notificationRecorder) HasHooks(hook.TriggerEnum) bool { return true }
func (h *notificationRecorder) ExecutePostHooks(ctx context.Context, id int, kind hook.TriggerEnum, input interface{}, fields []string) {
	call := notificationCall{id: id, kind: kind, input: input, fields: fields}
	h.calls = append(h.calls, call)
	if h.check != nil {
		h.check(ctx, call)
	}
}

func pluginNotificationRepository(t *testing.T) models.Repository {
	t.Helper()
	config.InitializeEmpty()
	t.Cleanup(func() { config.InitializeEmpty() })
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "stash.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db.Repository()
}

func TestSceneFieldNotificationsIncludeCustomFieldsAndExplicitClears(t *testing.T) {
	repo := pluginNotificationRepository(t)
	s := models.NewScene()
	s.Title, s.Details = "Before", "Clear this"
	require.NoError(t, repo.WithTxn(testCtx, func(ctx context.Context) error { return repo.Scene.Create(ctx, &s, nil) }))
	hooks := &notificationRecorder{}
	hooks.check = func(ctx context.Context, call notificationCall) {
		require.Equal(t, hook.SceneUpdatePost, call.kind)
		require.NoError(t, repo.WithReadTxn(ctx, func(ctx context.Context) error {
			updated, err := repo.Scene.Find(ctx, s.ID)
			require.NoError(t, err)
			require.Equal(t, "After", updated.Title)
			require.Empty(t, updated.Details)
			return nil
		}))
	}
	r := &mutationResolver{&Resolver{repository: repo, hookExecutor: hooks}}
	input := models.SceneUpdateInput{
		ID: strconv.Itoa(s.ID), Title: PtrString("After"), Rating100: PtrInt(80),
		CustomFields: &models.CustomFieldsInput{Partial: map[string]interface{}{"catalog_note": "edited"}},
	}
	ctx := withGqlContext(testCtx, map[string]interface{}{"input": map[string]interface{}{
		"id": input.ID, "title": "After", "details": nil, "rating100": 80,
		"custom_fields": map[string]interface{}{"partial": map[string]interface{}{"catalog_note": "edited"}},
	}})
	_, err := r.SceneUpdate(ctx, input)
	require.NoError(t, err)
	require.Len(t, hooks.calls, 1)
	for _, field := range []string{"title", "details", "rating100", "custom_fields"} {
		require.Contains(t, hooks.calls[0].fields, field)
	}
	require.NotContains(t, hooks.calls[0].fields, "performer_ids")
}

func TestSpecializedSceneEditsNotifyOnlyAfterSuccess(t *testing.T) {
	repo := pluginNotificationRepository(t)
	s := models.NewScene()
	s.Title = "Activity target"
	require.NoError(t, repo.WithTxn(testCtx, func(ctx context.Context) error { return repo.Scene.Create(ctx, &s, nil) }))
	hooks := &notificationRecorder{}
	r := &mutationResolver{&Resolver{repository: repo, hookExecutor: hooks}}
	id := strconv.Itoa(s.ID)
	resume := 12.5
	ok, err := r.SceneSaveActivity(testCtx, id, &resume, nil)
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, hooks.calls, 1)
	require.Equal(t, []string{"resume_time"}, hooks.calls[0].fields)

	hooks.calls = nil
	_, err = r.SceneSaveActivity(testCtx, id, nil, nil)
	require.NoError(t, err)
	require.Empty(t, hooks.calls)
	_, err = r.SceneSaveActivity(testCtx, "999999", &resume, nil)
	require.Error(t, err)
	require.Empty(t, hooks.calls)

	_, err = r.SceneAddPlay(testCtx, id, nil)
	require.NoError(t, err)
	require.Len(t, hooks.calls, 1)
	require.ElementsMatch(t, []string{"play_count", "play_history", "last_played_at"}, hooks.calls[0].fields)
	hooks.calls = nil
	_, err = r.SceneAddO(testCtx, id, nil)
	require.NoError(t, err)
	require.Len(t, hooks.calls, 1)
	require.ElementsMatch(t, []string{"o_counter", "o_history"}, hooks.calls[0].fields)
}

func TestFileDeletionNotificationsAcrossEntryAndSceneDeletion(t *testing.T) {
	for _, mode := range []string{"entry only", "scene and file", "scene only", "shared file"} {
		t.Run(mode, func(t *testing.T) {
			repo := pluginNotificationRepository(t)
			hooks := &notificationRecorder{}
			repo.File = plugin.WithFileHooks(repo.File, hooks)
			dir := t.TempDir()
			path := filepath.Join(dir, "scene.mp4")
			require.NoError(t, os.WriteFile(path, []byte("fixture"), 0600))
			folder := &models.Folder{Path: dir}
			f := &models.VideoFile{BaseFile: &models.BaseFile{Basename: "scene.mp4", Path: path}}
			s := models.NewScene()
			s.Title = "Deletion target"
			require.NoError(t, repo.WithTxn(testCtx, func(ctx context.Context) error {
				if err := repo.Folder.Create(ctx, folder); err != nil {
					return err
				}
				f.ParentFolderID = folder.ID
				if err := repo.File.Create(ctx, f); err != nil {
					return err
				}
				if mode == "entry only" {
					return nil
				}
				if mode == "shared file" {
					other := models.NewScene()
					other.Title = "Keep shared file"
					if err := repo.Scene.Create(ctx, &other, []models.FileID{f.ID}); err != nil {
						return err
					}
				}
				return repo.Scene.Create(ctx, &s, []models.FileID{f.ID})
			}))
			hooks.check = func(ctx context.Context, call notificationCall) {
				require.NoError(t, repo.WithReadTxn(ctx, func(ctx context.Context) error {
					files, err := repo.File.Find(ctx, models.FileID(call.id))
					require.ErrorIs(t, err, sql.ErrNoRows)
					require.Empty(t, files, "hook runs after the deletion is committed")
					return nil
				}))
			}
			if mode == "entry only" {
				r := &mutationResolver{&Resolver{repository: repo, hookExecutor: hooks}}
				_, err := r.DestroyFiles(testCtx, []string{strconv.Itoa(int(f.ID))})
				require.NoError(t, err)
			} else {
				service := &scene.Service{Repository: repo.Scene, MarkerRepository: repo.SceneMarker, File: repo.File}
				deleter := &scene.FileDeleter{Deleter: file.NewDeleter()}
				require.NoError(t, repo.WithTxn(testCtx, func(ctx context.Context) error {
					return service.Destroy(ctx, &s, deleter, false, mode != "scene only", false)
				}))
				deleter.Commit()
			}
			if mode == "scene only" || mode == "shared file" {
				require.Empty(t, hooks.calls)
			} else {
				require.Len(t, hooks.calls, 1)
				require.Equal(t, hook.FileDestroyPost, hooks.calls[0].kind)
				require.Equal(t, path, hooks.calls[0].input.(plugin.FileDestroyInput).Path)
			}
			if mode == "scene and file" {
				require.NoFileExists(t, path)
			} else {
				require.FileExists(t, path)
			}
		})
	}
}
