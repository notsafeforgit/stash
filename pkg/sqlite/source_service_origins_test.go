package sqlite_test

import (
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeSourceServiceOriginsSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	_, err := raw.Exec(`DROP TRIGGER source_run_attempt_pacing_origin;
DROP TRIGGER source_run_attempt_pacing_origin_immutable;
DROP TRIGGER source_run_attempt_pacing_bind;
ALTER TABLE source_run_attempt_pacing DROP COLUMN source_origin;
DELETE FROM native_migration_history WHERE version=1000099`)
	require.NoError(t, err)
	prior, err := os.ReadFile("migrations/1000055_source_run_services.up.sql")
	require.NoError(t, err)
	start := strings.Index(string(prior), "CREATE TRIGGER source_run_attempt_pacing_bind")
	end := strings.Index(string(prior), "CREATE TRIGGER source_run_attempt_pacing_current")
	require.Greater(t, end, start)
	_, err = raw.Exec(string(prior[start:end]))
	require.NoError(t, err)
}

func TestSourceServiceOriginsSurviveRetryAndRestart(t *testing.T) {
	f := newSourceRunFixture(t)
	r := f.claim(t, f.submit(t, f.request()))
	reservation, err := f.coordinator.ReserveSource(t.Context(), f.token, r.Lease(), "https://CDN.Example.Invalid.:443/path?access=secret")
	require.NoError(t, err)
	require.True(t, reservation.Ready)
	require.Equal(t, "host:cdn.example.invalid", reservation.Scope)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	var origin string
	require.NoError(t, raw.QueryRow("SELECT source_origin FROM source_run_attempt_pacing WHERE run_uuid=? AND scope=?", r.UUID, reservation.Scope).Scan(&origin))
	require.Equal(t, "https://cdn.example.invalid/", origin)
	// Scope ownership and origin evidence cannot be rewritten by a caller.
	_, err = raw.Exec("UPDATE source_run_attempt_pacing SET source_origin=? WHERE run_uuid=? AND scope=?", "https://other.invalid/", r.UUID, reservation.Scope)
	require.Error(t, err)
	_, err = raw.Exec("INSERT INTO source_pacing(scope) VALUES('host:other.invalid')")
	require.NoError(t, err)
	for _, invalid := range []any{nil, "https://cdn.example.invalid/", "https://other.invalid/path?secret=value"} {
		_, err = raw.Exec("INSERT INTO source_run_attempt_pacing(run_uuid,fence,scope,reserved,source_origin) VALUES(?,?,'host:other.invalid',0,?)", r.UUID, r.Fence, invalid)
		require.Error(t, err)
	}
	queued, err := f.coordinator.Finish(t.Context(), f.token, r.Lease(), models.SourceRunOutcome{State: "retry", ErrorCode: "timeout", ErrorScope: reservation.Scope})
	require.NoError(t, err)
	path := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(path))
	f.repo = f.db.Repository()
	f.service = ingest.New(f.repo)
	f.coordinator = ingest.NewRunCoordinator(f.service)
	f.coordinator.Now = func() time.Time { return f.now }
	f.now = queued.AvailableAt.Add(time.Hour)
	retried := f.claim(t, queued)
	require.NotNil(t, retried)
	require.EqualValues(t, 2, retried.Fence)
	require.NoError(t, raw.QueryRow("SELECT source_origin FROM source_run_attempt_pacing WHERE run_uuid=? AND fence=2 AND scope=? AND reserved=1", r.UUID, reservation.Scope).Scan(&origin))
	require.Equal(t, "https://cdn.example.invalid/", origin)
	replay, err := f.coordinator.ReserveSource(t.Context(), f.token, retried.Lease(), "https://cdn.example.invalid/next?different=secret")
	require.NoError(t, err)
	require.True(t, replay.Ready)
	_, err = f.coordinator.ReserveSource(t.Context(), f.token, r.Lease(), "https://new.invalid/")
	require.ErrorIs(t, err, models.ErrSourceRunLease)
	f.finish(t, retried, "succeeded")
	attempts, err := f.coordinator.Attempts(t.Context(), f.token, r.UUID, 0)
	require.NoError(t, err)
	require.Equal(t, reservation.Scope, attempts[0].ErrorScope)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestSourceServiceOriginsPreserveGlobalPacingAcrossDifferentParents(t *testing.T) {
	f := newEnrichmentExecutionFixture(t)
	a := newPacingDownload(t, f, "https://one.invalid/feed")
	b := newPacingDownload(t, f, "https://two.invalid/feed")
	one, two := a.claim(t), b.claim(t)
	require.NotNil(t, one)
	require.NotNil(t, two)
	first, err := a.coordinator.ReserveSource(t.Context(), a.token, one.Lease(), "https://media.provider.invalid/signed?a=1")
	require.NoError(t, err)
	require.True(t, first.Ready)
	cooling, err := a.coordinator.Finish(t.Context(), a.token, one.Lease(), models.SourceRunOutcome{State: "retry", ErrorCode: "timeout", ErrorScope: first.Scope})
	require.NoError(t, err)
	second, err := b.coordinator.ReserveSource(t.Context(), b.token, two.Lease(), "https://media.provider.invalid/other?b=2")
	require.NoError(t, err)
	require.False(t, second.Ready, "a provider cooldown applies across independent downloads")
	other, err := b.coordinator.ReserveSource(t.Context(), b.token, two.Lease(), "https://unrelated.provider.invalid/media")
	require.NoError(t, err)
	require.True(t, other.Ready, "unknown sibling hosts are not guessed to be one service")
	_, err = b.coordinator.Finish(t.Context(), b.token, two.Lease(), models.SourceRunOutcome{State: "retry", ErrorCode: "timeout", ErrorScope: second.Scope})
	require.ErrorIs(t, err, models.ErrSourceRunInvalid)
	_, err = b.coordinator.Finish(t.Context(), b.token, two.Lease(), models.SourceRunOutcome{State: "retry", ErrorCode: "source_busy", ErrorScope: second.Scope})
	require.NoError(t, err)
	f.now = cooling.AvailableAt.Add(time.Hour)
	retry := b.claim(t)
	require.NotNil(t, retry)
	ready, err := b.coordinator.ReserveSource(t.Context(), b.token, retry.Lease(), "https://media.provider.invalid/retry")
	require.NoError(t, err)
	require.True(t, ready.Ready)
}

func TestSourceServiceOriginsMigrationPreservesHistoricalBindings(t *testing.T) {
	f := newSourceRunFixture(t)
	r := f.claim(t, f.submit(t, f.request()))
	_, err := f.coordinator.ReserveSource(t.Context(), f.token, r.Lease(), "https://redgifs.com/watch/example")
	require.NoError(t, err)
	path := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, path)
	defer raw.Close()
	removeSourceServiceOriginsSchema(t, raw)
	_, err = raw.Exec("UPDATE schema_migrations SET version=1000098")
	require.NoError(t, err)
	before := map[string][][]any{}
	for _, table := range []string{"source_runs", "source_run_attempts", "source_run_requests", "source_pacing"} {
		before[table] = albumJobRows(t, raw, table)
	}
	bindings := albumJobRows(t, raw, "source_run_attempt_pacing")
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(f.db.Open(path), &needed))
	require.NoError(t, f.db.RunAllMigrations())
	require.NoError(t, f.db.ReInitialise())
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	after := albumJobRows(t, raw, "source_run_attempt_pacing")
	require.Len(t, after, len(bindings))
	for i, row := range after {
		require.Nil(t, row[len(row)-1], "migration must not invent an observed origin")
		require.Equal(t, bindings[i], row[:len(row)-1])
	}
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(path))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestMixedSourceCollectionRunsRetainTheReviewedCollectionScope(t *testing.T) {
	f := newSourceRunFixture(t)
	definition := f.collection.SourceCollectionDefinition
	definition.Namespace, definition.TargetURL = "", "https://example.invalid/playlist"
	f.collection = putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
	input := f.request()
	input.Window.Basis, input.Window.Since = scrape.TraversalBasis, nil
	r := f.claim(t, f.submit(t, input))
	require.NotNil(t, r)
	require.Equal(t, f.collection.UUID, r.CollectionUUID)
	require.Equal(t, f.collection.Revision, r.CollectionRevision)
	require.Equal(t, f.root.UUID, *r.RootUUID)
	require.Equal(t, definition.TargetURL, r.TargetURL)
	require.Empty(t, f.collection.Namespace)
	f.finish(t, r, "succeeded")
	input.RequestUUID, input.Operation, input.Window.Basis = uuid.NewString(), "enrich", ""
	_, err := f.coordinator.Submit(t.Context(), f.token, input)
	require.ErrorIs(t, err, models.ErrSourceDefinitionConflict)
}
