package sqlite_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeEnrichmentDiscoverySchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	_, err := raw.Exec(`DROP TABLE enrichment_discovery_resolutions; DROP INDEX automation_discovery_lookup;
 DELETE FROM native_migration_history WHERE version=1000070`)
	require.NoError(t, err)
}

func newDiscoveryExecutionFixture(t *testing.T, alter func(map[string]any)) *enrichmentExecutionFixture {
	t.Helper()
	catalog := snapshotFixture(t)
	rows := documentImportRows(t, catalog)
	const key = "legacy:post:filename-lookup"
	var post map[string]any
	for _, row := range rows {
		if row["table"] == "posts" {
			post = maps.Clone(row["values"].(map[string]any))
		}
	}
	require.NotNil(t, post)
	post["post_key"], post["source_id"], post["identity_basis"] = key, nil, "legacy-nfo-unverified"
	rows = append(rows, map[string]any{"table": "posts", "key": []any{key}, "values": post})
	catalog = receiveCatalogFixtureRows(t, catalog, rows)
	advanceCatalogEnrichment(t, catalog, 0)
	job := legacyEnrichmentJob(key, "pending")
	job["url"] = "https://www.reddit.com/comments/abc123"
	lookup := legacyDiscoveryTarget(t, map[string]any{"account_key": "reddit:handle:juniper"}, key, "lookup")
	var evidence map[string]any
	require.NoError(t, json.Unmarshal([]byte(lookup["evidence_json"].(string)), &evidence))
	evidence["candidate_url"] = job["url"]
	if alter != nil {
		alter(evidence)
	}
	encoded, err := json.Marshal(evidence)
	require.NoError(t, err)
	lookup["evidence_json"] = string(encoded)
	automation := enrichmentAutomationFixture(t, catalog, map[string][]map[string]any{"enrichment_jobs": {job}, "discovery_targets": {lookup}})
	advanceAutomationEnrichment(t, automation, 0)
	advanceDiscovery(t, automation, 0)
	f := &enrichmentExecutionFixture{db: catalog.db, repo: catalog.repo, now: automationImportNow.Add(48 * time.Hour)}
	var targetID, collectionID string
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.NoError(t, raw.QueryRow("SELECT e.target_uuid,e.collection_uuid FROM automation_enrichment_records e WHERE e.snapshot_uuid=? AND e.disposition='held'", automation.manifest.UUID).Scan(&targetID, &collectionID))
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		f.collection, err = f.repo.SourceCollection.Find(ctx, collectionID)
		return err
	}))
	definition := f.collection.SourceCollectionDefinition
	definition.State = "active"
	f.collection = putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: collectionID, ExpectedRevision: f.collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
	held := readEnrichmentTarget(t, f.repo, targetID)
	plan := enrichmentActivationPlan(t, f.repo, models.EnrichmentActivationInput{UUID: uuid.NewString(), SnapshotUUID: automation.manifest.UUID, ManifestSHA256: automation.sha,
		Targets: []models.EnrichmentActivationSelection{enrichmentSelection(held, f.collection.Revision)}})
	_, err = activateEnrichmentPlan(f.repo, plan, f.now)
	require.NoError(t, err)
	f.target = readEnrichmentTarget(t, f.repo, plan.Entries[0].ReleasedTargetUUID)
	f.service = ingest.New(f.repo)
	f.worker = ingest.NewEnrichmentCoordinator(f.service)
	f.worker.Now = func() time.Time { return f.now }
	for i := range f.producers {
		require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			var err error
			f.producers[i], err = f.repo.Ingest.CreateProducer(ctx, "Discovery metadata worker")
			return err
		}))
		_, f.tokens[i], err = f.service.IssueCredential(t.Context(), f.producers[i].UUID, []models.IngestScope{{CollectionUUID: collectionID}}, nil)
		require.NoError(t, err)
	}
	parent := 0
	f.complete, err = json.Marshal(archive.EnrichmentTranscript{Schema: archive.EnrichmentTranscriptSchema, URL: f.target.URL,
		RetentionPolicy: archive.SourceRetentionVersion, ExtractorVersion: "1.32.15-dev", Pending: []archive.EnrichmentReference{}, Unresolved: []archive.EnrichmentReference{},
		Records: []archive.EnrichmentRecord{
			{Kind: "post", Patch: map[string]any{"category": "reddit", "id": "abc123", "author": "juniper", "author_fullname": "t2_juniper", "title": "Fetched original title", "source_extractor_url": f.target.URL}, Removed: []string{}, ObservedAt: "2026-10-03T01:00:00Z"},
			{Kind: "post", Parent: &parent, Patch: map[string]any{"category": "imgur", "id": "child", "source_extractor_url": "https://imgur.com/child"}, Removed: []string{}, ObservedAt: "2026-10-03T01:00:01Z"},
		}})
	require.NoError(t, err)
	return f
}

func TestDiscoveryPublicationPreservesPostAndProvesIdentityAfterRestart(t *testing.T) {
	f := newDiscoveryExecutionFixture(t, nil)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	before := map[string][][]any{}
	for _, table := range []string{"scenes", "images", "performers", "source_media_evidence", "catalog_evidence_posts", "automation_discovery_records"} {
		before[table] = albumJobRows(t, raw, table)
	}
	job := f.admit(t)
	running := f.claim(t, job.UUID, 0)
	head, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.complete)
	require.NoError(t, err)
	publication, err := f.worker.Publish(t.Context(), f.tokens[0], running.Lease(), head.Revision, head.Digest)
	require.NoError(t, err)
	require.Equal(t, 2, publication.CaptureCount)
	described, err := f.worker.Describe(t.Context(), f.tokens[0], job.UUID)
	require.NoError(t, err)
	require.NotNil(t, described.DiscoveryResolution)
	proof := described.DiscoveryResolution
	require.Equal(t, "captured-account-and-post-id", proof.Basis)
	require.Equal(t, f.target.PostUUID, proof.PostUUID)
	require.Equal(t, "abc123", proof.Value)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		post, err := f.repo.SourceEvidence.FindPostByIdentifier(ctx, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "abc123"})
		require.NoError(t, err)
		require.Equal(t, f.target.PostUUID, post.UUID)
		require.Greater(t, post.Revision, proof.PostRevision)
		return nil
	}))
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	var identifiers int
	require.NoError(t, raw.QueryRow("SELECT count(*) FROM source_post_identifiers WHERE post_uuid=?", f.target.PostUUID).Scan(&identifiers))
	require.Equal(t, 2, identifiers)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	f.repo = f.db.Repository()
	f.worker.Service = ingest.New(f.repo)
	f.now = f.now.Add(24 * time.Hour)
	replayed, err := f.worker.Publish(t.Context(), f.tokens[0], running.Lease(), head.Revision, head.Digest)
	require.NoError(t, err)
	require.Equal(t, publication, replayed)
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM enrichment_discovery_resolutions"))
	anonymousPath := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymous, err := sqlite.NewAnonymiser(f.db, anonymousPath)
	require.NoError(t, err)
	require.NoError(t, anonymous.Anonymise(t.Context()))
	check := sqlite.NewDatabase()
	require.NoError(t, check.Open(anonymousPath))
	require.NoError(t, check.Close())
}

func TestDiscoveryPublicationConflictsKeepEvidenceForReview(t *testing.T) {
	for _, mode := range []string{"unverified", "owned_by_another_post"} {
		t.Run(mode, func(t *testing.T) {
			f := newDiscoveryExecutionFixture(t, func(e map[string]any) {
				if mode == "unverified" {
					e["account_key"] = nil
				}
			})
			if mode == "owned_by_another_post" {
				sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "abc123"}, "")
			}
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			before := albumJobRows(t, raw, "source_post_identifiers")
			job := f.admit(t)
			running := f.claim(t, job.UUID, 0)
			head, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.complete)
			require.NoError(t, err)
			_, err = f.worker.Publish(t.Context(), f.tokens[0], running.Lease(), head.Revision, head.Digest)
			require.ErrorIs(t, err, models.ErrEnrichmentIdentityReview)
			require.Equal(t, before, albumJobRows(t, raw, "source_post_identifiers"))
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM enrichment_discovery_resolutions"))
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM enrichment_publications"))
			retained, err := f.worker.CheckpointHead(t.Context(), f.tokens[0], job.UUID)
			require.NoError(t, err)
			require.NotNil(t, retained)
			current, err := f.worker.Find(t.Context(), f.tokens[0], job.UUID)
			require.NoError(t, err)
			require.Equal(t, "failed", current.State)
		})
	}
}

func TestDiscoveryPublicationRollsBackIdentityWithIncompletePublication(t *testing.T) {
	for _, mode := range []string{"publication_write", "lease_expiry", "standalone_resolution", "later_record_mismatch"} {
		t.Run(mode, func(t *testing.T) {
			f := newDiscoveryExecutionFixture(t, nil)
			body := f.complete
			if mode == "later_record_mismatch" {
				transcript, err := archive.ParseEnrichmentTranscript(body)
				require.NoError(t, err)
				transcript.Records[1] = archive.EnrichmentRecord{Kind: "post", Removed: []string{}, ObservedAt: "2026-10-03T01:00:01Z",
					Patch: map[string]any{"category": "reddit", "id": "other", "source_extractor_url": "https://www.reddit.com/comments/other"}}
				body, err = json.Marshal(transcript)
				require.NoError(t, err)
			}
			job := f.admit(t)
			running := f.claim(t, job.UUID, 0)
			head, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, body)
			require.NoError(t, err)
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			before := map[string][][]any{}
			for _, table := range []string{"source_posts", "source_post_identifiers", "source_post_identifier_evidence", "source_captures", "source_post_revisions", "source_accounts", "source_collection_captures", "enrichment_discovery_resolutions", "enrichment_publications", "enrichment_published_records", "enrichment_completions", "archive_jobs", "enrichment_targets", "enrichment_checkpoint_records"} {
				before[table] = albumJobRows(t, raw, table)
			}
			switch mode {
			case "publication_write":
				_, err = raw.Exec("CREATE TRIGGER reject_discovery_publication BEFORE INSERT ON enrichment_published_records WHEN NEW.ordinal=1 BEGIN SELECT RAISE(ABORT,'injected publication failure'); END")
				require.NoError(t, err)
			case "lease_expiry":
				f.service.Repo.EnrichmentJob = enrichmentPublicationBeforeCommit{f.repo.EnrichmentJob, func(context.Context) error { f.now = f.now.Add(2 * time.Minute); return nil }}
			}
			if mode == "standalone_resolution" {
				err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
					proof, err := f.repo.EnrichmentJob.ResolveDiscoveryIdentity(ctx, models.EnrichmentJobLease{ArchiveJobLease: running.Lease(), ProducerUUID: f.producers[0].UUID}, head.Revision, head.Digest, 0, f.now)
					require.NoError(t, err)
					require.NotNil(t, proof)
					return nil // A caller cannot commit the identifier without publication.
				})
			} else {
				_, err = f.worker.Publish(t.Context(), f.tokens[0], running.Lease(), head.Revision, head.Digest)
			}
			require.Error(t, err)
			for table, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, table), table)
			}
			retained, err := f.worker.CheckpointHead(t.Context(), f.tokens[0], job.UUID)
			require.NoError(t, err)
			require.Equal(t, head.Digest, retained.Digest)
		})
	}
}

func TestDiscoveryPublicationRejectsChangedProofOnStartup(t *testing.T) {
	for _, change := range []string{"basis='strict-filename-id'", "post_revision=post_revision+1", "created_at='2026-10-01T00:00:00Z'"} {
		t.Run(change, func(t *testing.T) {
			f := newDiscoveryExecutionFixture(t, nil)
			job := f.admit(t)
			running := f.claim(t, job.UUID, 0)
			head, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.complete)
			require.NoError(t, err)
			_, err = f.worker.Publish(t.Context(), f.tokens[0], running.Lease(), head.Revision, head.Digest)
			require.NoError(t, err)
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			var guard string
			require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='enrichment_discovery_resolution_immutable'").Scan(&guard))
			_, err = raw.Exec("DROP TRIGGER enrichment_discovery_resolution_immutable; UPDATE enrichment_discovery_resolutions SET " + change + ";" + guard)
			require.NoError(t, err)
			require.ErrorIs(t, f.db.Open(f.db.DatabasePath()), models.ErrSourcePayloadCorrupt)
		})
	}
}

func TestDiscoveryPublicationMigrationPreservesQueuedWorkAndRefusesCollision(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(fmt.Sprint(collision), func(t *testing.T) {
			f := newDiscoveryExecutionFixture(t, nil)
			job := f.admit(t)
			running := f.claim(t, job.UUID, 0)
			_, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.complete)
			require.NoError(t, err)
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			before := map[string][][]any{}
			for _, table := range []string{"scenes", "images", "performers", "source_posts", "source_post_identifiers", "archive_jobs", "enrichment_targets", "enrichment_checkpoints", "enrichment_checkpoint_records", "automation_discovery_records", "automation_snapshot_records"} {
				before[table] = albumJobRows(t, raw, table)
			}
			removeEnrichmentDiscoverySchema(t, raw)
			_, err = raw.Exec("UPDATE schema_migrations SET version=1000069,dirty=0")
			require.NoError(t, err)
			if collision {
				_, err = raw.Exec("CREATE TABLE enrichment_discovery_resolutions(original TEXT); INSERT INTO enrichment_discovery_resolutions VALUES('retained')")
				require.NoError(t, err)
			}
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(f.db.Open(f.db.DatabasePath()), &needed))
			if collision {
				require.Error(t, f.db.RunAllMigrations())
				var original string
				require.NoError(t, raw.QueryRow("SELECT original FROM enrichment_discovery_resolutions").Scan(&original))
				require.Equal(t, "retained", original)
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='automation_discovery_lookup'"))
			} else {
				require.NoError(t, f.db.RunAllMigrations())
				require.NoError(t, f.db.ReInitialise())
			}
			for table, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, table), table)
			}
		})
	}
}
