package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeCaptureContextSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeCheckpointHandoffSchema(t, raw)
	_, err := raw.Exec(`DROP TABLE source_capture_contexts; DROP INDEX source_captures_with_context;
 DELETE FROM native_migration_history WHERE version=1000063`)
	require.NoError(t, err)
}

func contextCaptureInput(t *testing.T, parent *models.SourceCapture, category, path string) models.SourceCaptureInput {
	t.Helper()
	raw, err := archive.RestoreCapture(parent.Payload)
	require.NoError(t, err)
	data, err := archive.DecodeJSONObject(raw, archive.MaxSourcePayloadBytes)
	require.NoError(t, err)
	raw, err = archive.EncodeSourceJSON(map[string]any{"category": category, "id": "child", path[1:]: data,
		"source_extractor_url": "https://media.invalid/child", "_url": "https://media.invalid/full.jpg"})
	require.NoError(t, err)
	payload, err := archive.PrepareRetainedCapture("gallery-dl", parent.Platform, raw)
	require.NoError(t, err)
	metadata, err := archive.CapturedMetadata(raw)
	require.NoError(t, err)
	input := models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: parent.PostUUID, Origin: "gallery-dl", Platform: parent.Platform,
		CapturedAt: time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC), RetentionPolicy: archive.CaptureContextPolicy, Payload: *payload, Metadata: metadata}
	if !parent.CapturedAt.IsZero() {
		input.CapturedAt = parent.CapturedAt.Add(24 * time.Hour)
	}
	input.Contexts = []models.SourceCaptureContext{{CaptureUUID: input.UUID, Path: path, ParentUUID: parent.UUID}}
	return input
}

func captureContextFixture(t *testing.T, known bool) (*sqlite.Database, models.Repository, *models.SourceCapture, models.SourceCaptureInput) {
	t.Helper()
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "saved"}, "")
	payload, err := archive.PrepareRetainedCapture("gallery-dl", "reddit", []byte(`{"category":"reddit","id":"saved","title":"Original caption","author":"OriginalName","author_fullname":"t2_actor","media_metadata":{"one":{"p":["old preview"]}}}`))
	require.NoError(t, err)
	stamp := time.Date(2026, 9, 1, 1, 2, 3, 4, time.UTC)
	input := models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: post.UUID, Origin: "legacy-enrichment", Platform: "reddit",
		CapturedAt: stamp, RetentionPolicy: "legacy-retained-v1", Payload: *payload}
	if !known {
		input.CapturedAt, input.RecordedAt = time.Time{}, &stamp
	}
	parent := recordSourceTestCapture(t, repo, input)
	return db, repo, parent, contextCaptureInput(t, parent, "redgifs", "/_reddit")
}

func TestCaptureContextUsesOriginalPublisherTimeAndRetainsOlderPayload(t *testing.T) {
	for _, known := range []bool{true, false} {
		name := "unrecorded"
		if known {
			name = "known"
		}
		t.Run(name, func(t *testing.T) {
			db, repo, parent, input := captureContextFixture(t, known)
			child := recordSourceTestCapture(t, repo, input)
			require.Equal(t, input.Contexts, child.Contexts)
			require.Equal(t, input.CapturedAt, child.CapturedAt, "the child's actual fetch time remains intact")
			require.Equal(t, child, recordSourceTestCapture(t, repo, input))
			raw, err := archive.RestoreCapture(child.Payload)
			require.NoError(t, err)
			require.Contains(t, string(raw), "old preview", "current reduction must not rewrite accepted context")
			preview := publisherPreview(t, repo, child.UUID, "")
			require.NotNil(t, preview.Observation)
			require.Equal(t, parent.UUID, preview.Observation.CaptureUUID)
			var decision *models.CapturePublisherDecision
			if known {
				require.True(t, parent.CapturedAt.Equal(*preview.Observation.CapturedAt))
				require.Equal(t, "create", preview.Action)
				decision, err = applyPublisher(repo, publisherInput(preview, "automatic"))
				require.NoError(t, err)
			} else {
				require.Nil(t, preview.Observation.CapturedAt)
				require.Equal(t, "review", preview.Action)
				require.Contains(t, preview.Conflicts, "observation_time_unrecorded")
				_, err := applyPublisher(repo, publisherInput(preview, "automatic"))
				require.ErrorIs(t, err, models.ErrCapturePublisherConflict)
				account := createSourceAccount(t, repo, "native:reddit")
				decision, err = applyPublisher(repo, publisherInput(publisherPreview(t, repo, child.UUID, account.UUID), "link"))
				require.NoError(t, err)
			}
			require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				ids, err := repo.SourceAccount.Identifiers(ctx, *decision.AccountUUID, "", 100)
				require.NoError(t, err)
				if !known {
					require.Empty(t, ids, "unknown old observation must not acquire today's identity dates")
				}
				for _, id := range ids {
					evidence, err := repo.SourceAccount.Evidence(ctx, id.UUID, "", 100)
					require.NoError(t, err)
					require.Len(t, evidence, 1)
					require.True(t, parent.CapturedAt.Equal(evidence[0].FirstObserved))
					require.True(t, parent.CapturedAt.Equal(evidence[0].LastObserved))
					require.Contains(t, string(evidence[0].Details), parent.UUID)
				}
				return nil
			}))
			backup := filepath.Join(t.TempDir(), "context.sqlite")
			require.NoError(t, db.Backup(backup))
			require.NoError(t, db.Close())
			require.NoError(t, db.Open(backup))
			require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				got, err := repo.SourceEvidence.FindCapture(ctx, child.UUID)
				require.Equal(t, child, got)
				return err
			}))
			anonymous, err := sqlite.NewAnonymiser(db, filepath.Join(t.TempDir(), "anonymous.sqlite"))
			require.NoError(t, err)
			require.NoError(t, anonymous.Anonymise(t.Context()))
		})
	}
}

func TestCaptureContextFollowsNestedSourceButNotFeedOwner(t *testing.T) {
	_, repo, root, input := captureContextFixture(t, false)
	middle := recordSourceTestCapture(t, repo, input)
	child := recordSourceTestCapture(t, repo, contextCaptureInput(t, middle, "imgur", "/_parent"))
	preview := publisherPreview(t, repo, child.UUID, "")
	require.Equal(t, root.UUID, preview.Observation.CaptureUUID)
	require.Nil(t, preview.Observation.CapturedAt)
	// A social post can carry feed context while naming its own actual author.
	feed := contextCaptureInput(t, middle, "reddit", "/_parent")
	raw, err := archive.RestoreCapture(&feed.Payload)
	require.NoError(t, err)
	data, err := archive.DecodeJSONObject(raw, archive.MaxSourcePayloadBytes)
	require.NoError(t, err)
	data["author"], data["author_fullname"] = "ActualAuthor", "t2_another"
	raw, err = archive.EncodeSourceJSON(data)
	require.NoError(t, err)
	payload, err := archive.PrepareRetainedCapture("gallery-dl", "reddit", raw)
	require.NoError(t, err)
	feed.Payload = *payload
	actual := recordSourceTestCapture(t, repo, feed)
	preview = publisherPreview(t, repo, actual.UUID, "")
	require.Nil(t, preview.Observation, "feed owner does not supply the actual publisher's timestamp")
	require.Equal(t, "create", preview.Action)
}

func TestCaptureContextRejectsChangedBindingsAndFreshUnretainedFields(t *testing.T) {
	db, repo, parent, original := captureContextFixture(t, true)
	for name, change := range map[string]func(*models.SourceCaptureInput){
		"missing context": func(c *models.SourceCaptureInput) { c.Contexts = nil },
		"wrong capture":   func(c *models.SourceCaptureInput) { c.Contexts[0].CaptureUUID = parent.UUID },
		"missing parent":  func(c *models.SourceCaptureInput) { c.Contexts[0].ParentUUID = uuid.NewString() },
		"wrong path":      func(c *models.SourceCaptureInput) { c.Contexts[0].Path = "/author" },
		"changed policy":  func(c *models.SourceCaptureInput) { c.RetentionPolicy = archive.SourceRetentionVersion },
		"unknown new time": func(c *models.SourceCaptureInput) {
			stamp := c.CapturedAt
			c.RecordedAt, c.CapturedAt = &stamp, time.Time{}
		},
		"parent from future": func(c *models.SourceCaptureInput) { c.CapturedAt = parent.CapturedAt.Add(-time.Nanosecond) },
		"unretained new data": func(c *models.SourceCaptureInput) {
			raw, err := archive.RestoreCapture(&c.Payload)
			require.NoError(t, err)
			data, err := archive.DecodeJSONObject(raw, archive.MaxSourcePayloadBytes)
			require.NoError(t, err)
			data["cookies"] = "fixture-private-value"
			raw, err = archive.EncodeSourceJSON(data)
			require.NoError(t, err)
			payload, err := archive.PrepareRetainedCapture("gallery-dl", "reddit", raw)
			require.NoError(t, err)
			c.Payload = *payload
		},
	} {
		t.Run(name, func(t *testing.T) {
			input := original
			input.Contexts = append([]models.SourceCaptureContext{}, original.Contexts...)
			change(&input)
			require.Error(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
				_, err := repo.SourceEvidence.RecordCapture(ctx, input)
				return err
			}))
		})
	}
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_captures"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_capture_contexts"))
	child := recordSourceTestCapture(t, repo, original)
	changed := original
	changed.Contexts = []models.SourceCaptureContext{{CaptureUUID: child.UUID, Path: "/_reddit", ParentUUID: uuid.NewString()}}
	require.ErrorIs(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourceEvidence.RecordCapture(ctx, changed)
		return err
	}), models.ErrSourceCaptureReplay)
}

func TestCaptureContextFailureCannotCommitHalfCapture(t *testing.T) {
	db, repo, _, input := captureContextFixture(t, false)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec(`CREATE TRIGGER fail_context_fixture BEFORE INSERT ON source_capture_contexts BEGIN SELECT RAISE(ABORT,'late context failure'); END`)
	require.NoError(t, err)
	err = repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourceEvidence.RecordCapture(ctx, input)
		require.ErrorContains(t, err, "late context failure")
		return nil // catching the storage error must not commit its partial capture
	})
	require.ErrorIs(t, err, models.ErrEnrichmentAtomic)
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_captures"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_capture_contexts"))
}

func TestCaptureContextStartupRejectsDeletedOrReboundContextWithoutWriting(t *testing.T) {
	for _, rebind := range []bool{false, true} {
		name := "deleted"
		if rebind {
			name = "rebound"
		}
		t.Run(name, func(t *testing.T) {
			db, repo, parent, input := captureContextFixture(t, false)
			recordSourceTestCapture(t, repo, input)
			otherInput := models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: parent.PostUUID, Origin: parent.Origin, Platform: parent.Platform,
				RecordedAt: parent.RecordedAt, RetentionPolicy: parent.RetentionPolicy, Payload: *parent.Payload}
			other := recordSourceTestCapture(t, repo, otherInput)
			require.NoError(t, db.Close())
			raw := openRawDB(t, db.DatabasePath())
			defer raw.Close()
			if rebind {
				var trigger string
				require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='source_capture_context_immutable'").Scan(&trigger))
				_, err := raw.Exec("DROP TRIGGER source_capture_context_immutable")
				require.NoError(t, err)
				_, err = raw.Exec("UPDATE source_capture_contexts SET parent_capture_uuid=?", other.UUID)
				require.NoError(t, err)
				_, err = raw.Exec(trigger)
				require.NoError(t, err)
			} else {
				_, err := raw.Exec("DELETE FROM source_capture_contexts")
				require.NoError(t, err)
			}
			before := albumJobRows(t, raw, "source_captures")
			links := albumJobRows(t, raw, "source_capture_contexts")
			require.ErrorIs(t, db.Open(db.DatabasePath()), models.ErrSourcePayloadCorrupt)
			require.Equal(t, before, albumJobRows(t, raw, "source_captures"))
			require.Equal(t, links, albumJobRows(t, raw, "source_capture_contexts"))
		})
	}
}

func TestCaptureContextMigrationPreservesPriorCapturesAndRollsBackCollision(t *testing.T) {
	for _, collision := range []bool{false, true} {
		name := "upgrade"
		if collision {
			name = "collision"
		}
		t.Run(name, func(t *testing.T) {
			db, _, _, _ := captureContextFixture(t, false)
			require.NoError(t, db.Close())
			raw := openRawDB(t, db.DatabasePath())
			defer raw.Close()
			before := map[string][][]any{}
			for _, table := range []string{"source_captures", "source_post_revisions", "source_profile_bodies", "source_capture_profiles", "source_payloads", "enrichment_checkpoint_releases"} {
				before[table] = albumJobRows(t, raw, table)
			}
			removeCaptureContextSchema(t, raw)
			_, err := raw.Exec("UPDATE schema_migrations SET version=1000062")
			require.NoError(t, err)
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(db.Open(db.DatabasePath()), &needed))
			if collision {
				_, err := raw.Exec("CREATE TABLE source_capture_contexts(retained TEXT); INSERT INTO source_capture_contexts VALUES('original')")
				require.NoError(t, err)
				require.Error(t, db.RunAllMigrations())
				require.Equal(t, [][]any{{"original"}}, albumJobRows(t, raw, "source_capture_contexts"))
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM native_migration_history WHERE version=1000063"))
			} else {
				require.NoError(t, db.RunAllMigrations())
				require.NoError(t, db.ReInitialise())
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_capture_contexts"))
			}
			for table, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, table), table)
			}
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
		})
	}
}
