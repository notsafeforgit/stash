package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/file"
	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/hash/md5"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stashapp/stash/pkg/sqlite/blob"
	_ "github.com/stashapp/stash/pkg/sqlite/migrations"
	"github.com/stretchr/testify/require"
)

type blobExistenceFunc func(context.Context, string) (bool, error)

func (f blobExistenceFunc) EntryExists(ctx context.Context, checksum string) (bool, error) {
	return f(ctx, checksum)
}

func blobCleanupFixture(t *testing.T) (*sqlite.Database, *CleanGeneratedJob, string, []byte) {
	t.Helper()
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "library.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	root := t.TempDir()
	db.SetBlobStoreOptions(sqlite.BlobStoreOptions{UseFilesystem: true, Path: root})
	data := []byte("unused original artwork")
	checksum := md5.FromBytes(data)
	require.NoError(t, blob.NewFilesystemStore(root, &file.OsFS{}).Write(t.Context(), checksum, data))
	path := filepath.Join(root, fsutil.GetIntraDir(checksum, 2, 2), checksum)
	j := &CleanGeneratedJob{Repository: db.Repository(), BlobCleaner: db.Blobs}
	return db, j, path, data
}

func TestCleanBlobFileRechecksConcurrentReference(t *testing.T) {
	db, j, path, data := blobCleanupFixture(t)
	observed, resume := make(chan struct{}), make(chan struct{})
	first := true
	j.BlobCleaner = blobExistenceFunc(func(ctx context.Context, checksum string) (bool, error) {
		exists, err := db.Blobs.EntryExists(ctx, checksum)
		if first {
			first = false
			close(observed)
			<-resume
		}
		return exists, err
	})
	done := make(chan error, 1)
	go func() { done <- j.cleanBlobFile(t.Context(), path) }()
	<-observed
	r := db.Repository()
	err := r.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := db.Blobs.Write(ctx, data)
		return err
	})
	close(resume)
	require.NoError(t, err)
	require.NoError(t, <-done)
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, data, got)
}

func TestCleanBlobFileWaitsForNativeCheckpoint(t *testing.T) {
	db, j, path, _ := blobCleanupFixture(t)
	observed := make(chan struct{})
	first := true
	j.BlobCleaner = blobExistenceFunc(func(ctx context.Context, checksum string) (bool, error) {
		exists, err := db.Blobs.EntryExists(ctx, checksum)
		if first {
			first = false
			close(observed)
		}
		return exists, err
	})
	done := make(chan error, 1)
	require.NoError(t, sqlite.WithNativeCheckpoint(t.Context(), db.DatabasePath(), func(*sqlite.NativeCheckpoint) error {
		go func() { done <- j.cleanBlobFile(t.Context(), path) }()
		<-observed
		select {
		case err := <-done:
			t.Fatalf("cleanup escaped checkpoint writer guard: %v", err)
		case <-time.After(100 * time.Millisecond):
		}
		require.FileExists(t, path)
		return nil
	}))
	require.NoError(t, <-done)
	require.NoFileExists(t, path)
	require.NoDirExists(t, db.FileDeletionJournalPath())
}

type blobCleanupWriteKey struct{}

type rejectBlobCleanupCommit struct {
	*sqlite.Database
	beforeReject func()
}

func (m rejectBlobCleanupCommit) Begin(ctx context.Context, writable bool) (context.Context, error) {
	ctx, err := m.Database.Begin(ctx, writable)
	if err != nil {
		return nil, err
	}
	return context.WithValue(ctx, blobCleanupWriteKey{}, writable), nil
}

func (m rejectBlobCleanupCommit) Commit(ctx context.Context) error {
	if writable, _ := ctx.Value(blobCleanupWriteKey{}).(bool); writable {
		m.beforeReject()
		return errors.New("reject cleanup commit")
	}
	return m.Database.Commit(ctx)
}

func TestCleanBlobFileDryRunAndRollback(t *testing.T) {
	db, j, path, data := blobCleanupFixture(t)
	j.Options.DryRun = true
	require.NoError(t, j.cleanBlobFile(t.Context(), path))
	require.FileExists(t, path)
	j.Options.DryRun = false
	rejected := false
	j.Repository.TxnManager = rejectBlobCleanupCommit{db, func() {
		rejected = true
		require.NoFileExists(t, path)
		require.DirExists(t, db.FileDeletionJournalPath())
	}}
	require.ErrorContains(t, j.cleanBlobFile(t.Context(), path), "reject cleanup commit")
	require.True(t, rejected)
	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, data, got)
	require.NoDirExists(t, db.FileDeletionJournalPath())
}
