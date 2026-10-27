package sqlite_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

// Reconstruct the actual prior receipt schema, not a relabelled current schema.
func removeAccountProfileSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	var exists bool
	require.NoError(t, raw.QueryRow("SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name='source_account_profiles')").Scan(&exists))
	if !exists {
		return
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_account_profiles"))
	old, err := os.ReadFile("migrations/1000087_attachment_downloads.up.sql")
	require.NoError(t, err)
	s := string(old)
	definition := strings.ReplaceAll(s[strings.Index(s, "CREATE TABLE ingest_receipts_next"):strings.Index(s, "INSERT INTO ingest_receipts_next")], "ingest_receipts_next", "ingest_receipts")
	guards := s[strings.Index(s, "CREATE INDEX ingest_receipts_capture"):strings.Index(s, "CREATE TRIGGER ingest_scope_receipts")]
	attachment := s[strings.Index(s, "CREATE TRIGGER attachment_download_receipt"):strings.Index(s, "INSERT INTO native_migration_history")]
	tx, err := raw.Begin()
	require.NoError(t, err)
	defer tx.Rollback()
	_, err = tx.Exec(`PRAGMA defer_foreign_keys=ON;
DROP TABLE account_observed_profile_urls;
DROP TABLE source_account_profiles;
CREATE TABLE before_profile_receipts AS SELECT * FROM ingest_receipts;
DROP TABLE ingest_receipts;` + definition + `
INSERT INTO ingest_receipts SELECT * FROM before_profile_receipts;
DROP TABLE before_profile_receipts;` + guards + attachment + `
CREATE INDEX ingest_receipts_collection ON ingest_receipts(collection_uuid);
DELETE FROM native_migration_history WHERE version=1000134;`)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
}

func TestAccountProfileMigrationPreservesReceiptsAndRefusesCollisions(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(map[bool]string{false: "preserve", true: "collision"}[collision], func(t *testing.T) {
			db, repo := archiveTestDatabase(t)
			collection := putSourceCollection(t, repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Fixture", Kind: "feed", State: "active", Namespace: "native:reddit"}})
			var producer *models.IngestProducer
			require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
				var err error
				producer, err = repo.Ingest.CreateProducer(ctx, "fixture")
				return err
			}))
			service := ingest.New(repo)
			_, token, err := service.IssueCredential(t.Context(), producer.UUID, []models.IngestScope{{CollectionUUID: collection.UUID}}, nil)
			require.NoError(t, err)
			event := ingest.CaptureEvent{Protocol: 1, ProducerUUID: producer.UUID, EventUUID: uuid.NewString(), RunUUID: uuid.NewString(), CollectionUUID: collection.UUID, CollectionRevision: collection.Revision, Kind: "source.capture", ObservedAt: time.Now().UTC(), ExtractorVersion: "fixture", RetentionPolicy: archive.SourceRetentionVersion, Post: ingest.PostReference{Namespace: "native:reddit", Value: "kept"}, Source: json.RawMessage(`{"category":"reddit","id":"kept","author":"example","author_fullname":"t2_123"}`)}
			body, err := json.Marshal(event)
			require.NoError(t, err)
			_, err = service.Capture(t.Context(), token, body, ingest.Digest(body))
			require.NoError(t, err)
			path := db.DatabasePath()
			require.NoError(t, db.Close())
			raw := openRawDB(t, path)
			defer raw.Close()
			before := albumJobRows(t, raw, "ingest_receipts")
			removeAccountProfileSchema(t, raw)
			_, err = raw.Exec("UPDATE schema_migrations SET version=1000133,dirty=0")
			require.NoError(t, err)
			if collision {
				_, err = raw.Exec("CREATE TABLE source_account_profiles(private_data TEXT); INSERT INTO source_account_profiles VALUES('kept')")
				require.NoError(t, err)
			}
			var needed *sqlite.MigrationNeededError
			require.ErrorAs(t, db.Open(path), &needed)
			err = db.RunAllMigrations()
			if collision {
				require.ErrorContains(t, err, "destination source_account_profiles already exists")
				var data string
				require.NoError(t, raw.QueryRow("SELECT private_data FROM source_account_profiles").Scan(&data))
				require.Equal(t, "kept", data)
			} else {
				require.NoError(t, err)
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_account_profiles"))
			}
			require.Equal(t, before, albumJobRows(t, raw, "ingest_receipts"))
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
		})
	}
}
