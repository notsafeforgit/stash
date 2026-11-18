package sqlite_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
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

const (
	lookupCatalog = "c_11111111111111111111111111111111"
	lookupPostKey = "legacy:post:filename-lookup"
	lookupURL     = "https://www.reddit.com/comments/abc123"
)

// lookupSourceValues is one original finder row as the frozen automation
// database held it: the legacy post key and its corroborating evidence.
func lookupSourceValues(t *testing.T, alter func(map[string]any)) string {
	t.Helper()
	evidence := map[string]any{"titles": []string{"Original distinctive title"}, "texts": []string{}, "dates": []string{"2025-01-01"},
		"paths": []string{"Historical album/photo.jpg"}, "urls": []string{}, "platform": "reddit", "account_key": "reddit:handle:juniper",
		"candidate_url": lookupURL, "strict_filename_id": false}
	if alter != nil {
		alter(evidence)
	}
	encoded, err := json.Marshal(evidence)
	require.NoError(t, err)
	values, err := json.Marshal(map[string]any{"catalog_id": lookupCatalog, "post_key": lookupPostKey, "job_key": nil, "evidence_json": string(encoded), "status": "lookup"})
	require.NoError(t, err)
	return string(values)
}

// newLookupExecutionFixture enriches a legacy-only post that has a retained
// finder lookup for the target URL.
func newLookupExecutionFixture(t *testing.T, alter func(map[string]any)) *enrichmentExecutionFixture {
	t.Helper()
	f := newEnrichmentExecutionFixtureForPost(t, models.SourcePostIdentifier{Namespace: "legacy:catalog:" + lookupCatalog, Value: lookupPostKey}, lookupURL,
		models.SourceCollectionDefinition{Label: "Fixture feed", Kind: "feed", Namespace: "native:reddit", State: "active", TargetURL: "https://www.reddit.com/user/juniper"})
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec("INSERT INTO enrichment_identity_lookups(post_uuid,collection_uuid,url,source_values) VALUES(?,?,?,?)",
		f.target.PostUUID, f.collection.UUID, lookupURL, lookupSourceValues(t, alter))
	require.NoError(t, err)
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

func publishLookupFixture(t *testing.T, f *enrichmentExecutionFixture) (*models.ArchiveJob, *models.EnrichmentPublication) {
	t.Helper()
	job := f.admit(t)
	running := f.claim(t, job.UUID, 0)
	head, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.complete)
	require.NoError(t, err)
	publication, err := f.worker.Publish(t.Context(), f.tokens[0], running.Lease(), head.Revision, head.Digest)
	require.NoError(t, err)
	return job, publication
}

func TestLookupPublicationPreservesPostAndProvesIdentityAfterRestart(t *testing.T) {
	f := newLookupExecutionFixture(t, nil)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	before := map[string][][]any{}
	for _, table := range []string{"scenes", "images", "performers", "source_media_evidence", "enrichment_identity_lookups"} {
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
	require.NotNil(t, described.IdentityResolution)
	proof := described.IdentityResolution
	require.Equal(t, "captured-account-and-post-id", proof.Basis)
	require.Equal(t, f.target.PostUUID, proof.PostUUID)
	require.Equal(t, "abc123", proof.Value)
	require.EqualValues(t, 1, proof.LookupID)
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
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM source_post_identifiers WHERE post_uuid='"+f.target.PostUUID+"'"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	f.repo = f.db.Repository()
	f.worker.Service = ingest.New(f.repo)
	f.now = f.now.Add(24 * time.Hour)
	replayed, err := f.worker.Publish(t.Context(), f.tokens[0], running.Lease(), head.Revision, head.Digest)
	require.NoError(t, err)
	require.Equal(t, publication, replayed)
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM enrichment_identity_resolutions"))
	anonymousPath := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymous, err := sqlite.NewAnonymiser(f.db, anonymousPath)
	require.NoError(t, err)
	require.NoError(t, anonymous.Anonymise(t.Context()))
	check := sqlite.NewDatabase()
	require.NoError(t, check.Open(anonymousPath))
	require.NoError(t, check.Close())
	anonymised := openRawDB(t, anonymousPath)
	defer anonymised.Close()
	require.Zero(t, queryUint(t, anonymised, "SELECT count(*) FROM enrichment_identity_lookups"), "finder values are private evidence")
}

func TestLookupPublicationConflictsKeepEvidenceForReview(t *testing.T) {
	for _, mode := range []string{"unverified", "unverified_existing_post", "no_lookup"} {
		t.Run(mode, func(t *testing.T) {
			f := newLookupExecutionFixture(t, func(e map[string]any) {
				if mode != "no_lookup" {
					e["account_key"] = nil
				}
			})
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			switch mode {
			case "unverified_existing_post":
				sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "abc123"}, "")
			case "no_lookup":
				_, err := raw.Exec("DELETE FROM enrichment_identity_lookups")
				require.NoError(t, err)
			}
			before := albumJobRows(t, raw, "source_post_identifiers")
			job := f.admit(t)
			running := f.claim(t, job.UUID, 0)
			head, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.complete)
			require.NoError(t, err)
			_, err = f.worker.Publish(t.Context(), f.tokens[0], running.Lease(), head.Revision, head.Digest)
			require.ErrorIs(t, err, models.ErrEnrichmentIdentityReview)
			require.Equal(t, before, albumJobRows(t, raw, "source_post_identifiers"))
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM enrichment_identity_resolutions"))
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

func TestLookupPublicationRollsBackIdentityWithIncompletePublication(t *testing.T) {
	for _, mode := range []string{"publication_write", "lease_expiry", "standalone_resolution", "later_record_mismatch"} {
		t.Run(mode, func(t *testing.T) {
			f := newLookupExecutionFixture(t, nil)
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
			for _, table := range []string{"source_posts", "source_post_identifiers", "source_post_identifier_evidence", "source_captures", "source_post_revisions", "source_accounts", "source_collection_captures", "enrichment_identity_resolutions", "enrichment_publications", "enrichment_published_records", "enrichment_completions", "archive_jobs", "enrichment_targets", "enrichment_checkpoint_records"} {
				before[table] = albumJobRows(t, raw, table)
			}
			switch mode {
			case "publication_write":
				_, err = raw.Exec("CREATE TRIGGER reject_lookup_publication BEFORE INSERT ON enrichment_published_records WHEN NEW.ordinal=1 BEGIN SELECT RAISE(ABORT,'injected publication failure'); END")
				require.NoError(t, err)
			case "lease_expiry":
				f.service.Repo.EnrichmentJob = enrichmentPublicationBeforeCommit{f.repo.EnrichmentJob, func(context.Context) error { f.now = f.now.Add(2 * time.Minute); return nil }}
			}
			if mode == "standalone_resolution" {
				err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
					proof, err := f.repo.EnrichmentJob.ResolveLookupIdentity(ctx, models.EnrichmentJobLease{ArchiveJobLease: running.Lease(), ProducerUUID: f.producers[0].UUID}, head.Revision, head.Digest, 0, f.now)
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

func TestLookupPublicationRejectsChangedProofOnAudit(t *testing.T) {
	for _, change := range []string{"UPDATE enrichment_identity_resolutions SET basis='strict-filename-id'",
		"UPDATE enrichment_identity_resolutions SET post_revision=post_revision+1",
		"UPDATE enrichment_identity_resolutions SET created_at='2026-10-01T00:00:00Z'",
		"UPDATE enrichment_identity_lookups SET source_values=replace(source_values,'reddit:handle:juniper','reddit:handle:someone')",
		"UPDATE enrichment_identity_lookups SET url='https://www.reddit.com/comments/other'"} {
		t.Run(change, func(t *testing.T) {
			f := newLookupExecutionFixture(t, nil)
			publishLookupFixture(t, f)
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			var guard string
			require.NoError(t, raw.QueryRow("SELECT name FROM sqlite_schema WHERE type='trigger' AND tbl_name=? AND name LIKE '%_immutable'", strings.Fields(change)[1]).Scan(&guard))
			var definition string
			require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name=?", guard).Scan(&definition))
			_, err := raw.Exec("DROP TRIGGER " + guard + ";" + change + ";" + definition)
			require.NoError(t, err)
			require.ErrorIs(t, f.db.AuditForTesting(f.db.DatabasePath()), models.ErrSourcePayloadCorrupt)
		})
	}
}

// The retirement migration keeps exactly the mapped lookup rows a pending
// legacy-only enrichment can still need, then ends unreleased imported work.
func TestImportRetirementMigrationRetainsLookupsAndExcludesHeldTargets(t *testing.T) {
	f := newLookupExecutionFixture(t, nil)
	other := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "legacy:catalog:" + lookupCatalog, Value: "legacy:post:held"}, "")
	otherURL, err := observePostURL(f.repo, models.SourcePostURLInput{SourcePostEvidence: postLinkEvidence(other.UUID), URL: "https://www.reddit.com/comments/held"})
	require.NoError(t, err)
	path := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, path)
	defer raw.Close()
	removeImportRetirementSchema(t, raw)
	_, err = raw.Exec("UPDATE schema_migrations SET version=1000141,dirty=0")
	require.NoError(t, err)
	for _, name := range []string{"enrichment_identity_lookups", "enrichment_identity_resolutions"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='"+name+"'"))
	}

	// Frozen automation input at 1000141: one mapped lookup, one review row and
	// one lookup from an unfinished import that must not be retained.
	abs, err := filepath.Abs(path)
	require.NoError(t, err)
	unchecked, err := sql.Open("sqlite3ex", (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs), RawQuery: "_fk=false"}).String())
	require.NoError(t, err)
	defer unchecked.Close()
	unchecked.SetMaxOpenConns(1)
	_, err = unchecked.Exec("DROP TRIGGER automation_discovery_record_scope")
	require.NoError(t, err)
	values := lookupSourceValues(t, nil)
	for _, snapshot := range []struct {
		uuid, state string
	}{{uuid.NewString(), "review"}, {uuid.NewString(), "running"}} {
		processed, review := 2, 1
		if snapshot.state == "running" {
			processed, review = 1, 0
		}
		_, err = unchecked.Exec(`INSERT INTO automation_discovery_imports(snapshot_uuid,manifest_sha256,policy,state,last_ordinal,source_records,processed_records,mapped_records,review_records,created_at,updated_at)
 VALUES(?,?,'automation-discovery-v1',?,2,2,?,1,?,'2026-10-08T00:00:00Z','2026-10-08T00:00:00Z')`, snapshot.uuid, strings.Repeat("a", 64), snapshot.state, processed, review)
		require.NoError(t, err)
		for ordinal, disposition := range map[int]string{1: "lookup", 2: "review"} {
			data := `{"values":` + values + `}`
			sum := sha256.Sum256([]byte(data))
			_, err = unchecked.Exec(`INSERT INTO automation_snapshot_records(snapshot_uuid,ordinal,chunk_index,source_table,source_key,data,byte_count,data_sha256)
 VALUES(?,?,0,'discovery_targets',?,?,?,?)`, snapshot.uuid, ordinal, `["`+lookupCatalog+`","key`+string(rune('0'+ordinal))+`"]`, data, len(data), hex.EncodeToString(sum[:]))
			require.NoError(t, err)
			outcome, reason := "mapped", ""
			if disposition == "review" {
				outcome, reason = "review", "candidate_conflict"
			}
			_, err = unchecked.Exec(`INSERT INTO automation_discovery_records(snapshot_uuid,ordinal,enrichment_ordinal,post_uuid,post_reference,collection_uuid,collection_revision,candidate_url,disposition,outcome,reason)
 VALUES(?,?,1,?,?,?,1,?,?,?,?)`, snapshot.uuid, ordinal, f.target.PostUUID, strings.Repeat("r", 69), f.collection.UUID, lookupURL, disposition, outcome, reason)
			require.NoError(t, err)
		}
	}
	held, err := archive.EnrichmentTargetIdentity(models.EnrichmentTargetInput{PostUUID: other.UUID, URLUUID: otherURL.URLUUID,
		CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, Policy: models.EnrichmentGalleryMetadataV1, Origin: "migration"})
	require.NoError(t, err)
	_, err = raw.Exec(`INSERT INTO enrichment_targets(uuid,post_uuid,url_uuid,collection_uuid,collection_revision,policy,origin,state,priority,not_before,reason,created_at,updated_at)
 VALUES(?,?,?,?,?,'gallery-dl-metadata-v1','migration','held',10,?,'',?,?)`, held, other.UUID, otherURL.URLUUID, f.collection.UUID, f.collection.Revision, f.now, f.now, f.now)
	require.NoError(t, err)
	reviewTarget := albumJobRows(t, raw, "enrichment_targets")

	var needed *sqlite.MigrationNeededError
	require.ErrorAs(t, f.db.Open(path), &needed)
	require.NoError(t, f.db.RunAllMigrations())
	require.NoError(t, f.db.Open(path))
	require.NoError(t, f.db.AuditForTesting(path))
	f.repo = f.db.Repository()
	f.service = ingest.New(f.repo)
	f.worker.Service = f.service

	var lookups []struct {
		Post, Collection, URL, Values string
	}
	rows, err := raw.Query("SELECT post_uuid,collection_uuid,url,source_values FROM enrichment_identity_lookups")
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var row struct{ Post, Collection, URL, Values string }
		require.NoError(t, rows.Scan(&row.Post, &row.Collection, &row.URL, &row.Values))
		lookups = append(lookups, row)
	}
	require.NoError(t, rows.Err())
	require.Len(t, lookups, 1, "only mapped lookups from finished imports are retained")
	require.Equal(t, f.target.PostUUID, lookups[0].Post)
	require.Equal(t, f.collection.UUID, lookups[0].Collection)
	require.Equal(t, lookupURL, lookups[0].URL)
	require.JSONEq(t, values, lookups[0].Values)

	target := readEnrichmentTarget(t, f.repo, held)
	require.Equal(t, "excluded", target.State)
	require.Equal(t, "import_retired", target.Reason)
	require.Equal(t, 2, target.Revision)
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM enrichment_target_history WHERE target_uuid='"+held+"'"))
	require.Len(t, albumJobRows(t, raw, "enrichment_targets"), len(reviewTarget))
	require.Equal(t, "pending", readEnrichmentTarget(t, f.repo, f.target.UUID).State, "native work is unchanged")
	for _, name := range []string{"automation_snapshots", "automation_discovery_records", "catalog_snapshots", "discovery_listings", "checkpoint_handoffs",
		"enrichment_discovery_resolutions", "source_post_import_labels", "catalog_document_pending_imports", "discovery_listing_success"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='"+name+"'"), name)
	}
	var details string
	require.NoError(t, raw.QueryRow("SELECT details FROM native_migration_history WHERE version=1000142").Scan(&details))
	require.JSONEq(t, `{"identity_lookups":1,"retired_discovery_resolutions":0,"excluded_held_targets":1,"removed_discovery_jobs":0,
 "removed_discovery_policy_upgrades":0,"catalog_snapshots":0,"automation_snapshots":0}`, details)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))

	// The retained lookup still corroborates the pending legacy-only target.
	_, publication := publishLookupFixture(t, f)
	require.Equal(t, 2, publication.CaptureCount)
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM enrichment_identity_resolutions"))
	require.False(t, errors.Is(f.db.AuditForTesting(path), models.ErrSourcePayloadCorrupt))
}
