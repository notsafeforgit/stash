package keyframecache

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	kindIndex = "index"
	kindStill = "still"
)

var defaultBudgets = map[string]int64{kindIndex: 1 << 20, kindStill: 1 << 20}

func TestStoreRoundTripsAndPersists(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "keyframes.sqlite")
	store := Open(path, defaultBudgets)
	missing, err := store.Get(ctx, kindIndex, "absent")
	require.NoError(t, err)
	require.Nil(t, missing)
	require.NoError(t, store.Put(ctx, kindIndex, "file", []byte{1, 2, 3}))
	require.NoError(t, store.Put(ctx, kindStill, "file", []byte{4}))
	require.NoError(t, store.Close())

	store = Open(path, defaultBudgets)
	defer store.Close()
	value, err := store.Get(ctx, kindIndex, "file")
	require.NoError(t, err)
	require.Equal(t, []byte{1, 2, 3}, value)
	value, err = store.Get(ctx, kindStill, "file")
	require.NoError(t, err)
	require.Equal(t, []byte{4}, value)
	_, err = store.Get(ctx, "other", "file")
	require.Error(t, err)
}

func TestStoreEvictsLeastRecentlyUsedWithinKind(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "keyframes.sqlite")
	// Each 400-byte entry accounts for about 470 bytes; ten fit in the budget.
	store := Open(path, map[string]int64{kindIndex: 4800, kindStill: 1 << 20})
	defer store.Close()
	clock := time.Unix(1_000_000, 0)
	store.now = func() time.Time { return clock }
	value := bytes.Repeat([]byte{7}, 400)
	require.NoError(t, store.Put(ctx, kindStill, "still", value))
	for i := range 10 {
		clock = clock.Add(time.Hour)
		require.NoError(t, store.Put(ctx, kindIndex, fmt.Sprint("entry-", i), value))
	}
	// Reading the oldest entry makes it the most recently used.
	clock = clock.Add(time.Hour)
	got, err := store.Get(ctx, kindIndex, "entry-0")
	require.NoError(t, err)
	require.Equal(t, value, got)
	clock = clock.Add(time.Hour)
	require.NoError(t, store.Put(ctx, kindIndex, "entry-10", value))

	require.LessOrEqual(t, store.totals[kindIndex], int64(4800))
	for _, key := range []string{"entry-0", "entry-10", "entry-9"} {
		got, err := store.Get(ctx, kindIndex, key)
		require.NoError(t, err)
		require.NotNil(t, got, key)
	}
	got, err = store.Get(ctx, kindIndex, "entry-1")
	require.NoError(t, err)
	require.Nil(t, got, "least recently used entry must be evicted")
	got, err = store.Get(ctx, kindStill, "still")
	require.NoError(t, err)
	require.NotNil(t, got, "index pressure must not evict stills")

	// Oversized values are skipped instead of flushing the cache.
	require.NoError(t, store.Put(ctx, kindIndex, "huge", bytes.Repeat([]byte{1}, 4000)))
	got, err = store.Get(ctx, kindIndex, "huge")
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestStoreDiscardsCorruptFile(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "keyframes.sqlite")
	require.NoError(t, os.WriteFile(path, bytes.Repeat([]byte("not a database "), 512), 0o600))
	store := Open(path, defaultBudgets)
	defer store.Close()
	value, err := store.Get(ctx, kindIndex, "file")
	require.NoError(t, err)
	require.Nil(t, value)
	require.NoError(t, store.Put(ctx, kindIndex, "file", []byte{9}))
	value, err = store.Get(ctx, kindIndex, "file")
	require.NoError(t, err)
	require.Equal(t, []byte{9}, value)
}

func TestStoreClosedBehavesAsUnavailable(t *testing.T) {
	store := Open(filepath.Join(t.TempDir(), "keyframes.sqlite"), defaultBudgets)
	require.NoError(t, store.Close())
	_, err := store.Get(context.Background(), kindIndex, "file")
	require.Error(t, err)
	require.Error(t, store.Put(context.Background(), kindIndex, "file", []byte{1}))
}
