package sqlite_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func albumJobRows(t *testing.T, db *sql.DB, table string) [][]any {
	t.Helper()
	tx, err := db.BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true})
	require.NoError(t, err)
	defer func() { require.NoError(t, tx.Rollback()) }()
	// A second connection may have rebuilt this table during migration. Step a
	// catalog query on the pinned connection before preparing SELECT *: SQLite
	// otherwise exposes the old column count until that statement's first step.
	var objects int
	require.NoError(t, tx.QueryRow("SELECT count(*) FROM sqlite_schema").Scan(&objects))
	order := "rowid"
	var definition string
	require.NoError(t, tx.QueryRow("SELECT sql FROM sqlite_schema WHERE type='table' AND name=?", table).Scan(&definition))
	if strings.Contains(strings.ToUpper(definition), "WITHOUT ROWID") {
		keys, err := tx.Query(`SELECT name FROM pragma_table_info(?) WHERE pk>0 ORDER BY pk`, table)
		require.NoError(t, err)
		defer keys.Close()
		var columns []string
		for keys.Next() {
			var column string
			require.NoError(t, keys.Scan(&column))
			columns = append(columns, `"`+strings.ReplaceAll(column, `"`, `""`)+`"`)
		}
		require.NoError(t, keys.Err())
		require.NoError(t, keys.Close())
		require.NotEmpty(t, columns)
		order = strings.Join(columns, ",")
	}
	projection := "*"
	if table == "archive_jobs" {
		// Compare the retained pre-107 columns. The interruption migration
		// separately verifies the newly derived failure counter.
		projection = "id,uuid,kind,work_key,resource_key,arguments,state,revision,priority,fence,max_attempts,available_at_ms,owner_uuid,lease_until_ms,progress,result,error_code,created_at_ms,updated_at_ms"
	}
	rows, err := tx.Query("SELECT " + projection + " FROM " + table + " ORDER BY " + order)
	require.NoError(t, err)
	defer rows.Close()
	columns, err := rows.Columns()
	require.NoError(t, err)
	ret := [][]any{}
	for rows.Next() {
		row := make([]any, len(columns))
		pointers := make([]any, len(row))
		for i := range row {
			pointers[i] = &row[i]
		}
		require.NoError(t, rows.Scan(pointers...))
		ret = append(ret, row)
		if table == "source_captures" && !slices.Contains(columns, "recorded_at") {
			// Old observed captures acquire an explicit null recording time.
			ret[len(ret)-1] = append(row, nil)
		}
	}
	require.NoError(t, rows.Err())
	return ret
}

func TestAlbumJobRowsReadsColumnsAfterExternalSchemaChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "columns.sqlite")
	reader := openRawDB(t, path)
	defer reader.Close()
	reader.SetMaxOpenConns(1)
	_, err := reader.Exec("CREATE TABLE evidence (original TEXT); INSERT INTO evidence VALUES('retained')")
	require.NoError(t, err)
	require.Equal(t, [][]any{{"retained"}}, albumJobRows(t, reader, "evidence"))
	writer := openRawDB(t, path)
	defer writer.Close()
	_, err = writer.Exec("ALTER TABLE evidence ADD COLUMN added TEXT; UPDATE evidence SET added='new column'")
	require.NoError(t, err)
	require.Equal(t, [][]any{{"retained", "new column"}}, albumJobRows(t, reader, "evidence"))
}

func TestAlbumJobMigrationPreservesJobsAttemptsReceiptsAndGuards(t *testing.T) {
	fixture, repo := archiveTestDatabase(t)
	binding, err := archive.ProbeMediaRoot(t.TempDir())
	require.NoError(t, err)
	root := putMediaRoot(t, repo, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Intake", State: "active", Binding: binding}})
	collection := putSourceCollection(t, repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Intake", Kind: "directory", State: "active", RootUUID: &root.UUID, PathPrefix: "."}})
	var producer *models.IngestProducer
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		producer, err = repo.Ingest.CreateProducer(ctx, "Retained producer")
		return err
	}))
	credential, _, err := ingest.New(repo).IssueCredential(t.Context(), producer.UUID, []models.IngestScope{{CollectionUUID: collection.UUID, RootUUID: &root.UUID}}, nil)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "schema39.sqlite")
	buildLegacyDatabase(t, path, 86, true)
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	m, err := sqlite.NewMigrator(db)
	require.NoError(t, err)
	for v := m.CurrentSchemaVersion(); v < sqlite.NativeSchemaBaseline+39; v = m.CurrentSchemaVersion() {
		require.NoError(t, m.RunMigration(t.Context(), m.GetNextMigrationVersion(v)))
	}
	m.Close()
	raw := openRawDB(t, path)
	defer raw.Close()
	raw.SetMaxOpenConns(1)
	_, err = raw.Exec("ATTACH DATABASE ? AS source_fixture", fixture.DatabasePath())
	require.NoError(t, err)
	tx, err := raw.Begin()
	require.NoError(t, err)
	for _, table := range []string{"media_roots", "media_root_revisions", "source_collections", "source_collection_revisions", "ingest_producers", "ingest_credentials", "ingest_credential_scopes"} {
		copyHistoricalFixtureTable(t, tx, table)
	}
	require.NoError(t, tx.Commit())
	_, err = raw.Exec("DETACH DATABASE source_fixture")
	require.NoError(t, err)
	digest := strings.Repeat("a", 64)
	first := ""
	for i, state := range []string{"queued", "running", "succeeded", "failed", "cancelled"} {
		id, owner := uuid.NewString(), uuid.NewString()
		if i == 0 {
			first = id
		}
		_, err := raw.Exec(`INSERT INTO archive_jobs(uuid,kind,work_key,resource_key,arguments,priority,max_attempts,available_at_ms,created_at_ms,updated_at_ms)
 VALUES(?,'media.verify',?,?,'{"retained":true}',10,3,1,1,1)`, id, ingest.Digest([]byte(id)), ingest.Digest([]byte(owner)))
		require.NoError(t, err)
		_, err = raw.Exec("INSERT INTO archive_job_submissions VALUES(?,?,?,1)", uuid.NewString(), digest, id)
		require.NoError(t, err)
		if state != "queued" {
			_, err = raw.Exec("UPDATE archive_jobs SET state='running',revision=2,fence=1,owner_uuid=?,lease_until_ms=9999999999999,updated_at_ms=2 WHERE uuid=?", owner, id)
			require.NoError(t, err)
			_, err = raw.Exec("INSERT INTO archive_job_attempts(job_uuid,fence,owner_uuid,started_at_ms) VALUES(?,1,?,2)", id, owner)
			require.NoError(t, err)
			if state != "running" {
				_, err = raw.Exec(`UPDATE archive_job_attempts SET outcome=?,ended_at_ms=3,result='{"retained":true}' WHERE job_uuid=?`, state, id)
				require.NoError(t, err)
				_, err = raw.Exec(`UPDATE archive_jobs SET state=?,revision=3,owner_uuid=NULL,lease_until_ms=NULL,updated_at_ms=3,
 progress='{"registration_committed":true}',result='{"retained":true}' WHERE uuid=?`, state, id)
				require.NoError(t, err)
			}
		}
	}
	_, err = raw.Exec(`INSERT INTO ingest_receipts(producer_uuid,event_uuid,digest,credential_uuid,collection_uuid,collection_revision,root_uuid,run_uuid,kind,job_uuid,result)
 VALUES(?,?,?,?,?,1,?,?,'file.completed',?,'{"retained":true}')`, producer.UUID, uuid.NewString(), digest, credential.UUID, collection.UUID, root.UUID, uuid.NewString(), first)
	require.NoError(t, err)
	before := map[string][][]any{}
	for _, table := range []string{"archive_jobs", "archive_job_submissions", "archive_job_attempts", "ingest_receipts"} {
		before[table] = albumJobRows(t, raw, table)
	}
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	defer db.Close()
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	upgraded := db.Repository()
	durable := job.NewDurable(upgraded)
	input := jobSubmission("album", "album-resource")
	input.Kind, input.Arguments = models.ArchiveJobBackfillAlbum, json.RawMessage(`{"version":1}`)
	queued, err := durable.Submit(t.Context(), input)
	require.NoError(t, err)
	// The old receipt trigger must still inspect the new jobs table and must
	// never allow an album job to masquerade as verified-file intake.
	_, err = raw.Exec(`INSERT INTO ingest_receipts(producer_uuid,event_uuid,digest,credential_uuid,collection_uuid,collection_revision,root_uuid,run_uuid,kind,job_uuid,result)
 VALUES(?,?,?,?,?,1,?,?,'file.completed',?,'{}')`, producer.UUID, uuid.NewString(), digest, credential.UUID, collection.UUID, root.UUID, uuid.NewString(), queued.UUID)
	require.ErrorContains(t, err, "requires media verification work")
	claimed, err := durable.Claim(t.Context(), models.ArchiveJobBackfillAlbum, uuid.NewString(), time.Minute)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, queued.UUID, claimed.UUID)
	require.NoError(t, upgraded.WithReadTxn(t.Context(), func(ctx context.Context) error {
		rows, err := upgraded.ArchiveJob.ResourceHistory(ctx, models.ArchiveJobBackfillAlbum, input.ResourceKey, 0, 100)
		require.NoError(t, err)
		require.Equal(t, []models.ArchiveJob{*claimed}, rows)
		other, err := upgraded.ArchiveJob.ResourceHistory(ctx, models.ArchiveJobVerifyMedia, input.ResourceKey, 0, 100)
		require.NoError(t, err)
		require.Empty(t, other)
		return nil
	}))
	_, err = raw.Exec("UPDATE archive_jobs SET arguments='{}',revision=revision+1 WHERE uuid=?", claimed.UUID)
	require.ErrorContains(t, err, "identity is immutable")
	_, err = raw.Exec("UPDATE archive_job_submissions SET digest=?", strings.Repeat("b", 64))
	require.ErrorContains(t, err, "submission is immutable")
	_, err = raw.Exec("INSERT INTO archive_job_attempts(job_uuid,fence,owner_uuid,started_at_ms) VALUES(?,2,?,1)", claimed.UUID, claimed.OwnerUUID)
	require.ErrorContains(t, err, "requires owned running work")
	rows, err := raw.Query("EXPLAIN QUERY PLAN SELECT * FROM archive_jobs WHERE kind=? AND resource_key=? AND id>0 ORDER BY id LIMIT 10", models.ArchiveJobBackfillAlbum, input.ResourceKey)
	require.NoError(t, err)
	defer rows.Close()
	plan := ""
	for rows.Next() {
		var a, b, c int
		var detail string
		require.NoError(t, rows.Scan(&a, &b, &c, &detail))
		plan += detail
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	require.Contains(t, plan, "archive_jobs_resource_history")
	require.NotContains(t, plan, "SCAN archive_jobs")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}
