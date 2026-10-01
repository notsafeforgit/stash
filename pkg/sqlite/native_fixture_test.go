package sqlite_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

// Domain tests need separate databases, not another execution of every
// historical migration. Build an empty database through the real initializer
// once, close it to checkpoint the WAL, and copy only that empty state. Fixture
// entities are inserted afterwards, so every library receives fresh UUIDs.
// Migration/new-install tests continue constructing their actual input schemas.
var emptyNativeFixture = sync.OnceValues(func() ([]byte, error) {
	directory, err := os.MkdirTemp("", "stash-empty-native-fixture-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, "empty.sqlite")
	db := sqlite.NewDatabase()
	if err := db.Open(path); err != nil {
		db.Close()
		return nil, err
	}
	if err := db.Close(); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
})

func writeEmptyNativeFixture(t *testing.T, path string) {
	t.Helper()
	body, err := emptyNativeFixture()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, body, 0600))
}

func TestNativeArchiveFixturesHaveIndependentDataAndIdentities(t *testing.T) {
	one, first := archiveTestDatabase(t)
	two, second := archiveTestDatabase(t)
	require.Equal(t, sqlite.GetRequiredSchemaVersion(), one.Version())
	require.Equal(t, sqlite.GetRequiredSchemaVersion(), two.Version())
	for _, item := range []struct {
		kind models.ArchiveEntityKind
		id   int
	}{
		{models.ArchivePerformer, 71}, {models.ArchiveScene, 31}, {models.ArchiveImage, 41}, {models.ArchiveFile, 21},
	} {
		require.NotEqual(t, archiveFind(t, first, item.kind, item.id).UUID, archiveFind(t, second, item.kind, item.id).UUID)
	}
	account := createSourceAccount(t, first, "native:reddit")
	require.NoError(t, first.WithTxn(context.Background(), func(ctx context.Context) error {
		_, _, err := one.ExecSQL(ctx, "UPDATE scenes SET title='First library only' WHERE id=31", nil)
		return err
	}))
	require.NoError(t, second.WithReadTxn(context.Background(), func(ctx context.Context) error {
		missing, err := second.SourceAccount.Find(ctx, account.UUID)
		require.NoError(t, err)
		require.Nil(t, missing)
		scene, err := second.Scene.Find(ctx, 31)
		require.NoError(t, err)
		require.Equal(t, "Kept title", scene.Title)
		return nil
	}))
	raw := openRawDB(t, two.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}
