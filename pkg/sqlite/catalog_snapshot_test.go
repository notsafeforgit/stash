package sqlite_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

type catalogSnapshotFixture struct {
	db       *sqlite.Database
	repo     models.Repository
	body     []byte
	manifest *scrape.CatalogSnapshotManifest
	sha      string
	chunks   [][]byte
}

func snapshotFixture(t *testing.T) *catalogSnapshotFixture {
	t.Helper()
	db, repo, registry, _ := registryFixture(t, func(d *registryTestDocument) {
		d.Tables["catalogs"] = append(d.Tables["catalogs"], map[string]any{"id": "c_11111111111111111111111111111111", "kind": "collection", "label": "Historical album", "owner_key": "directory:Historical album", "created_at": "2025-01-01T00:00:00Z", "redirect_to": nil})
	})
	registryApply(t, repo, registry, registryPreview(t, repo, registry))
	body, err := os.ReadFile("../scrape/testdata/catalog_snapshot/manifest.json")
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(body, &m))
	m["registry_source_uuid"] = registry.SourceUUID
	body, err = archive.EncodeSourceJSON(m)
	require.NoError(t, err)
	manifest, err := scrape.PrepareCatalogSnapshot(body, scrape.CatalogSnapshotSHA(body))
	require.NoError(t, err)
	return &catalogSnapshotFixture{db: db, repo: repo, body: body, manifest: manifest, sha: scrape.CatalogSnapshotSHA(body)}
}

func (f *catalogSnapshotFixture) begin(t *testing.T) *models.CatalogSnapshot {
	t.Helper()
	var result *models.CatalogSnapshot
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = f.repo.CatalogSnapshot.Begin(ctx, f.body, f.sha, catalogImportNow)
		return err
	}))
	return result
}

func (f *catalogSnapshotFixture) chunk(t *testing.T, index int) []byte {
	t.Helper()
	if f.chunks != nil {
		return f.chunks[index]
	}
	body, err := os.ReadFile(filepath.Join("../scrape/testdata/catalog_snapshot", f.manifest.Chunks[index].File))
	require.NoError(t, err)
	return body
}

func (f *catalogSnapshotFixture) receive(t *testing.T, index int) *models.CatalogSnapshot {
	t.Helper()
	var result *models.CatalogSnapshot
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = f.repo.CatalogSnapshot.Receive(ctx, f.manifest.UUID, f.sha, index, f.chunk(t, index), catalogImportNow.Add(time.Minute))
		return err
	}))
	return result
}

func TestCatalogSnapshotResumesAfterRestartWithoutNativeChanges(t *testing.T) {
	f := snapshotFixture(t)
	start := f.begin(t)
	require.Equal(t, "receiving", start.State)
	require.False(t, start.Imported)
	require.Len(t, start.PendingFamilies, len(f.manifest.Tables))
	first := f.receive(t, 0)
	require.Equal(t, first, f.receive(t, 0))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	require.Equal(t, first, f.begin(t))
	require.Equal(t, first, f.receive(t, 0))
	last := first
	var original []byte
	for i := range f.manifest.Chunks {
		last = f.receive(t, i)
		original = append(original, f.chunk(t, i)...)
	}
	require.Equal(t, "received", last.State)
	require.False(t, last.Imported)
	require.Equal(t, f.manifest.Records, last.ReceivedRecords)
	require.EqualValues(t, len(original), last.ReceivedBytes)
	require.Equal(t, start.PendingFamilies, last.PendingFamilies)
	require.Equal(t, last, f.begin(t))
	require.Equal(t, last, f.receive(t, 0))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	raw := openRawDB(t, f.db.DatabasePath())
	var lines []string
	rows, err := raw.Query("SELECT data FROM catalog_snapshot_records ORDER BY ordinal")
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		lines = append(lines, line)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, original, []byte(strings.Join(lines, "")))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_posts"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_captures"))
	_, err = raw.Exec("UPDATE catalog_snapshot_records SET data='{}'")
	require.ErrorContains(t, err, "immutable")
	require.NoError(t, raw.Close())
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		p, err := f.repo.Performer.Find(ctx, 71)
		require.NoError(t, err)
		require.Equal(t, "Shared name", p.Name)
		return nil
	}))
	output := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymiser, err := sqlite.NewAnonymiser(f.db, output)
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(t.Context()))
	raw = openRawDB(t, output)
	defer raw.Close()
	for _, table := range []string{"catalog_snapshots", "catalog_snapshot_tables", "catalog_snapshot_chunks", "catalog_snapshot_records"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
}

func TestCatalogSnapshotRejectsRebindingGapsAndChangedBytes(t *testing.T) {
	f := snapshotFixture(t)
	f.begin(t)
	require.ErrorIs(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.CatalogSnapshot.Receive(ctx, f.manifest.UUID, f.sha, 1, f.chunk(t, 1), catalogImportNow)
		return err
	}), models.ErrCatalogSnapshotConflict)
	require.ErrorIs(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.CatalogSnapshot.Receive(ctx, f.manifest.UUID, f.sha, 0, bytes.Replace(f.chunk(t, 0), []byte("reddit"), []byte("reddix"), 1), catalogImportNow)
		return err
	}), models.ErrCatalogSnapshotInvalid)
	for _, field := range []string{"snapshot_uuid", "registry_source_uuid", "captured_at"} {
		var m map[string]any
		require.NoError(t, json.Unmarshal(f.body, &m))
		m[field] = uuid.NewString()
		if field == "captured_at" {
			m[field] = "2026-09-30T00:00:00Z"
		}
		body, err := archive.EncodeSourceJSON(m)
		require.NoError(t, err)
		require.ErrorIs(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := f.repo.CatalogSnapshot.Begin(ctx, body, scrape.CatalogSnapshotSHA(body), catalogImportNow)
			return err
		}), models.ErrCatalogSnapshotConflict)
	}
	f.receive(t, 0)
}

func TestCatalogSnapshotSwallowedChunkFailureRollsBack(t *testing.T) {
	f := snapshotFixture(t)
	f.begin(t)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := f.db.ExecSQL(ctx, `CREATE TRIGGER fail_catalog_chunk BEFORE INSERT ON catalog_snapshot_records WHEN NEW.ordinal=2 BEGIN SELECT RAISE(ABORT,'fixture late chunk failure'); END`, nil)
		return err
	}))
	require.ErrorContains(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.CatalogSnapshot.Receive(ctx, f.manifest.UUID, f.sha, 0, f.chunk(t, 0), catalogImportNow)
		require.ErrorContains(t, err, "fixture late chunk failure")
		return nil
	}), "catalog snapshot write did not finish atomically")
	require.Zero(t, f.begin(t).NextChunk)
	raw := openRawDB(t, f.db.DatabasePath())
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM catalog_snapshot_records"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM catalog_snapshot_chunks"))
	require.NoError(t, raw.Close())
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := f.db.ExecSQL(ctx, "DROP TRIGGER fail_catalog_chunk", nil)
		return err
	}))
	f.receive(t, 0)
}

func TestCatalogSnapshotTableDigestCannotBeReplacedByChunkDigest(t *testing.T) {
	f := snapshotFixture(t)
	var m map[string]any
	require.NoError(t, json.Unmarshal(f.body, &m))
	m["tables"].(map[string]any)["account_snapshots"].(map[string]any)["sha256"] = strings.Repeat("0", 64)
	var err error
	f.body, err = archive.EncodeSourceJSON(m)
	require.NoError(t, err)
	f.sha = scrape.CatalogSnapshotSHA(f.body)
	f.begin(t)
	require.ErrorIs(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.CatalogSnapshot.Receive(ctx, f.manifest.UUID, f.sha, 0, f.chunk(t, 0), catalogImportNow)
		return err
	}), models.ErrCatalogSnapshotInvalid)
	require.Zero(t, f.begin(t).NextChunk)
}
