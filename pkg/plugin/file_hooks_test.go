package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/dop251/goja"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/mocks"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stashapp/stash/pkg/txn"
	"github.com/stretchr/testify/require"
)

type capturedFileHook struct {
	id     int
	kind   hook.TriggerEnum
	input  interface{}
	fields []string
}

type fileHooksRecorder struct {
	enabled bool
	calls   []capturedFileHook
}

func (r *fileHooksRecorder) HasHooks(hook.TriggerEnum) bool { return r.enabled }
func (r *fileHooksRecorder) ExecutePostHooks(_ context.Context, id int, kind hook.TriggerEnum, input interface{}, fields []string) {
	r.calls = append(r.calls, capturedFileHook{id, kind, input, fields})
}

type hookFileStore struct {
	models.FileReaderWriter
	file     models.File
	captions []*models.VideoCaption
	writeErr error
	reads    int
}

func (s *hookFileStore) Find(context.Context, ...models.FileID) ([]models.File, error) {
	s.reads++
	if s.file == nil {
		return nil, nil
	}
	return []models.File{s.file}, nil
}
func (s *hookFileStore) GetCaptions(context.Context, models.FileID) ([]*models.VideoCaption, error) {
	return s.captions, nil
}
func (s *hookFileStore) Destroy(context.Context, models.FileID) error {
	if s.writeErr == nil {
		s.file = nil
	}
	return s.writeErr
}
func (s *hookFileStore) Update(_ context.Context, f models.File) error {
	if s.writeErr == nil {
		s.file = f
	}
	return s.writeErr
}
func (s *hookFileStore) UpdateCaptions(_ context.Context, _ models.FileID, captions []*models.VideoCaption) error {
	s.captions = captions
	return s.writeErr
}

func TestFileDestroyHookPreservesIdentityAndRequiresCommit(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback"}[rollback], func(t *testing.T) {
			zipID := models.FileID(9)
			store := &hookFileStore{file: &models.BaseFile{
				ID: 12, Path: "/library/archive.zip/member.mp4", Basename: "member.mp4",
				DirEntry:     models.DirEntry{ZipFileID: &zipID},
				Fingerprints: models.Fingerprints{{Type: "md5", Fingerprint: "checksum"}},
			}}
			hooks := &fileHooksRecorder{enabled: true}
			r := WithFileHooks(store, hooks)
			err := txn.WithTxn(context.Background(), mocks.NewDatabase(), func(ctx context.Context) error {
				require.NoError(t, r.Destroy(ctx, 12))
				require.Empty(t, hooks.calls, "must not run inside the transaction")
				if rollback {
					return errors.New("later write failed")
				}
				return nil
			})
			if rollback {
				require.Error(t, err)
				require.Empty(t, hooks.calls)
				return
			}
			require.NoError(t, err)
			require.Len(t, hooks.calls, 1)
			call := hooks.calls[0]
			require.Equal(t, 12, call.id)
			require.Equal(t, hook.FileDestroyPost, call.kind)
			input := call.input.(FileDestroyInput)
			require.Equal(t, "12", input.ID)
			require.Equal(t, "/library/archive.zip/member.mp4", input.Path)
			require.Equal(t, "9", *input.ZipFileID)
			require.Equal(t, "checksum", input.Fingerprints["md5"])
		})
	}
}

func TestFileHooksSkipFailedWritesAndUnusedSnapshots(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		store := &hookFileStore{file: &models.BaseFile{ID: 12}, writeErr: errors.New("failed")}
		hooks := &fileHooksRecorder{enabled: enabled}
		r := WithFileHooks(store, hooks)
		err := txn.WithTxn(context.Background(), mocks.NewDatabase(), func(ctx context.Context) error {
			return r.Destroy(ctx, 12)
		})
		require.Error(t, err)
		require.Empty(t, hooks.calls)
		if !enabled {
			require.Zero(t, store.reads)
		}
	}
}

func TestFileUpdateHookSnapshotsChangedFields(t *testing.T) {
	store := &hookFileStore{file: &models.BaseFile{ID: 12, Path: "/library/old.mp4", Basename: "old.mp4"}}
	hooks := &fileHooksRecorder{enabled: true}
	r := WithFileHooks(store, hooks)
	err := txn.WithTxn(context.Background(), mocks.NewDatabase(), func(ctx context.Context) error {
		return r.Update(ctx, &models.BaseFile{ID: 12, Path: "/library/new.mp4", Basename: "new.mp4"})
	})
	require.NoError(t, err)
	require.Len(t, hooks.calls, 1)
	call := hooks.calls[0]
	require.Equal(t, hook.FileUpdatePost, call.kind)
	require.Equal(t, []string{"basename", "path"}, call.fields)
	input := call.input.(FileUpdateInput)
	require.Equal(t, "/library/old.mp4", input.Before["path"])
	require.Equal(t, "/library/new.mp4", input.After["path"])

	hooks.calls = nil
	err = txn.WithTxn(context.Background(), mocks.NewDatabase(), func(ctx context.Context) error {
		return r.Update(ctx, store.file)
	})
	require.NoError(t, err)
	require.Empty(t, hooks.calls, "unchanged files must not generate an event")
}

func TestFileSnapshotSupportsRawAndJavaScriptPlugins(t *testing.T) {
	const size int64 = 9007199254740993
	store := &hookFileStore{file: &models.BaseFile{ID: 12, Path: "/library/file.mp4", Size: size}}
	hooks := &fileHooksRecorder{enabled: true}
	r := &fileHookRepository{FileReaderWriter: store, hooks: hooks}
	snapshot, err := r.snapshot(context.Background(), 12)
	require.NoError(t, err)
	data, err := json.Marshal(snapshot)
	require.NoError(t, err)
	require.Contains(t, string(data), `"size":9007199254740993`)
	vm := goja.New()
	require.NoError(t, vm.Set("snapshot", snapshot))
	value, err := vm.RunString(`snapshot.path === "/library/file.mp4" && typeof snapshot.size === "number" && typeof snapshot.fingerprints === "object"`)
	require.NoError(t, err)
	require.True(t, value.ToBoolean())
}

func TestFileCaptionUpdateHook(t *testing.T) {
	store := &hookFileStore{file: &models.BaseFile{ID: 12}}
	hooks := &fileHooksRecorder{enabled: true}
	r := WithFileHooks(store, hooks)
	err := txn.WithTxn(context.Background(), mocks.NewDatabase(), func(ctx context.Context) error {
		return r.UpdateCaptions(ctx, 12, []*models.VideoCaption{{}})
	})
	require.NoError(t, err)
	require.Len(t, hooks.calls, 1)
	require.Equal(t, []string{"captions"}, hooks.calls[0].fields)
}
