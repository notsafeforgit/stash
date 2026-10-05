package sqlite_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestNativeReadSnapshotPinsViewWhileWritersContinue(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "library.sqlite")
	writeEmptyNativeFixture(t, path)
	raw := openRawDB(t, path)
	defer raw.Close()
	raw.SetMaxOpenConns(1)
	_, err := raw.Exec("PRAGMA journal_mode=WAL; PRAGMA busy_timeout=100; CREATE TABLE snapshot_test_padding(body BLOB); INSERT INTO snapshot_test_padding VALUES(zeroblob(4194304))")
	require.NoError(t, err)
	output := filepath.Join(t.TempDir(), "snapshot.sqlite")
	captured, checks := false, 0
	checkSpace := func(int64) error {
		require.True(t, captured)
		checks++
		if checks <= 3 {
			// The first allocation check and later copy steps must permit writes.
			_, err := raw.Exec("INSERT INTO snapshot_test_padding(body) VALUES('after capture')")
			return err
		}
		return nil
	}
	err = sqlite.CaptureNativeSnapshot(t.Context(), path, output, checkSpace, func(c *sqlite.NativeCheckpoint) error {
		_, err := raw.Exec("INSERT INTO snapshot_test_padding(body) VALUES('during capture')")
		require.ErrorContains(t, err, "locked")
		require.Empty(t, c.CommittedDeletionIDs())
		captured = true
		return nil
	})
	require.NoError(t, err)
	require.Greater(t, checks, 3)
	copyDB := openRawDB(t, output)
	defer copyDB.Close()
	require.EqualValues(t, 1, queryUint(t, copyDB, "SELECT count(*) FROM snapshot_test_padding"))
	require.EqualValues(t, 4, queryUint(t, raw, "SELECT count(*) FROM snapshot_test_padding"))
}

func TestNativeReadSnapshotCancellationAndNonWAL(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "library.sqlite")
	writeEmptyNativeFixture(t, path)
	raw := openRawDB(t, path)
	defer raw.Close()
	_, err := raw.Exec("PRAGMA journal_mode=DELETE")
	require.NoError(t, err)
	output := filepath.Join(t.TempDir(), "snapshot.sqlite")
	require.ErrorContains(t, sqlite.CaptureNativeSnapshot(t.Context(), path, output, nil, func(*sqlite.NativeCheckpoint) error {
		t.Fatal("non-WAL source reached capture")
		return nil
	}), "WAL")
	require.NoFileExists(t, output)
	_, err = raw.Exec("PRAGMA journal_mode=WAL")
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	err = sqlite.CaptureNativeSnapshot(ctx, path, output, func(int64) error { cancel(); return nil }, func(*sqlite.NativeCheckpoint) error { return nil })
	require.ErrorIs(t, err, context.Canceled)
	require.NoFileExists(t, output)
	_, err = raw.Exec("CREATE TABLE cancellation_released_guard(id INTEGER)")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(output, []byte("keep existing"), 0600))
	require.Error(t, sqlite.CaptureNativeSnapshot(t.Context(), path, output, nil, func(*sqlite.NativeCheckpoint) error { return nil }))
	body, err := os.ReadFile(output)
	require.NoError(t, err)
	require.Equal(t, "keep existing", string(body))
}
