package sqlite_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func sourceTestPost(t *testing.T, repo models.Repository, key models.SourcePostIdentifier, id string) *models.SourcePost {
	t.Helper()
	var post *models.SourcePost
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		post, err = repo.SourceEvidence.EnsurePost(ctx, key, id)
		return err
	}))
	return post
}

func sourceTestCapture(t *testing.T, post string, num int, bio string) models.SourceCaptureInput {
	t.Helper()
	raw, err := json.Marshal(map[string]interface{}{
		"category": "twitter", "tweet_id": "post-one", "content": strings.Repeat("Shared post text. ", 60),
		"author": map[string]interface{}{"id": json.Number("98765432109876543210"), "name": "Example", "description": bio, "followers_count": num},
		"num":    num, "filename": "attachment", "extension": "mp4", "_url": "https://example.test/original.mp4",
	})
	require.NoError(t, err)
	retained, err := archive.RetainSourcePayload(raw)
	require.NoError(t, err)
	payload, err := archive.PrepareRetainedCapture("gallery-dl", "twitter", retained)
	require.NoError(t, err)
	version, title, basis := "fixture-extractor", "Shared title", "source"
	return models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: post, Origin: "gallery-dl", Platform: "twitter",
		CapturedAt: time.Date(2026, 9, 1, 2, 3, 4, 123456789, time.FixedZone("capture", -7*3600)), ExtractorVersion: &version,
		RetentionPolicy: archive.SourceRetentionVersion, Metadata: models.SourcePostMetadata{Title: &title, DateBasis: &basis}, Payload: *payload}
}

func recordSourceTestCapture(t *testing.T, repo models.Repository, input models.SourceCaptureInput) *models.SourceCapture {
	t.Helper()
	var capture *models.SourceCapture
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		capture, err = repo.SourceEvidence.RecordCapture(ctx, input)
		return err
	}))
	return capture
}

func TestSourceEvidenceMigrationPreservesDirectLibraryAndAccounts(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "before-evidence.sqlite")
	buildLegacyDatabase(t, path, 86, true)
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	m, err := sqlite.NewMigrator(db)
	require.NoError(t, err)
	for version := m.CurrentSchemaVersion(); version < sqlite.NativeSchemaBaseline+5; version = m.CurrentSchemaVersion() {
		require.NoError(t, m.RunMigration(context.Background(), m.GetNextMigrationVersion(version)))
	}
	m.Close()
	raw := openRawDB(t, path)
	_, err = raw.Exec(archiveIdentityFixture)
	require.NoError(t, err)
	accountID := uuid.NewString()
	_, err = raw.Exec("INSERT INTO source_accounts(uuid, namespace, label) VALUES (?, 'native:reddit', 'Existing account')", accountID)
	require.NoError(t, err)
	var identity string
	require.NoError(t, raw.QueryRow("SELECT uuid FROM archive_entities WHERE performer_id=71").Scan(&identity))
	require.NoError(t, raw.Close())
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	defer db.Close()
	require.Equal(t, identity, archiveFind(t, db.Repository(), models.ArchivePerformer, 71).UUID)
	require.Equal(t, "Existing account", findSourceAccount(t, db.Repository(), accountID).Label)
	raw = openRawDB(t, path)
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_posts"), "direct scans do not require invented source posts")
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM performers_scenes WHERE performer_id=71 AND scene_id=31"))
	require.Equal(t, uint(6), queryUint(t, raw, "SELECT count(*) FROM archive_entities"))
}

func TestSourceEvidencePostIdentifiersPreserveNamespacesAndUUIDs(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	key := models.SourcePostIdentifier{Namespace: "native:onlyfans", Value: "OpaquePostID"}
	id := uuid.NewString()
	post := sourceTestPost(t, repo, key, strings.ToUpper(id))
	require.Equal(t, id, post.UUID)
	require.Equal(t, post, sourceTestPost(t, repo, key, ""))
	mirror := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "mirror:coomer:onlyfans", Value: key.Value}, "")
	require.NotEqual(t, post.UUID, mirror.UUID)
	for _, input := range []struct {
		key models.SourcePostIdentifier
		id  string
	}{{key, uuid.NewString()}, {models.SourcePostIdentifier{Namespace: key.Namespace, Value: "Another"}, id}} {
		err := repo.WithTxn(context.Background(), func(ctx context.Context) error {
			_, err := repo.SourceEvidence.EnsurePost(ctx, input.key, input.id)
			return err
		})
		require.ErrorIs(t, err, models.ErrSourcePostConflict)
	}
	alias := models.SourcePostIdentifier{Namespace: "legacy:catalog:post", Value: "old-catalog-key"}
	apply := func(revision int, key models.SourcePostIdentifier) error {
		return repo.WithTxn(context.Background(), func(ctx context.Context) error {
			return repo.SourceEvidence.AddPostIdentifier(ctx, id, key, revision)
		})
	}
	require.NoError(t, apply(post.Revision, alias))
	require.ErrorIs(t, apply(post.Revision, alias), models.ErrSourcePostConflict)
	require.NoError(t, apply(post.Revision+1, alias))
	require.ErrorIs(t, apply(post.Revision+1, models.SourcePostIdentifier{Namespace: "mirror:coomer:onlyfans", Value: key.Value}), models.ErrSourcePostConflict)
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		first, err := repo.SourceEvidence.PostIdentifiers(ctx, id, nil, 1)
		require.NoError(t, err)
		require.Equal(t, []models.SourcePostIdentifier{alias}, first)
		next, err := repo.SourceEvidence.PostIdentifiers(ctx, id, &first[0], 1)
		require.NoError(t, err)
		require.Equal(t, []models.SourcePostIdentifier{key}, next)
		lower, err := repo.SourceEvidence.FindPostByIdentifier(ctx, models.SourcePostIdentifier{Namespace: key.Namespace, Value: strings.ToLower(key.Value)})
		require.NoError(t, err)
		require.Nil(t, lower, "opaque post IDs retain case")
		return nil
	}))
}

func TestSourceEvidenceSharesPostAndProfileBodiesWithLosslessCaptures(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	key := models.SourcePostIdentifier{Namespace: "native:twitter", Value: "post-one"}
	post := sourceTestPost(t, repo, key, "")
	one := sourceTestCapture(t, post.UUID, 1, "Original biography")
	two := sourceTestCapture(t, post.UUID, 2, "Original biography")
	changed := sourceTestCapture(t, post.UUID, 3, "Edited biography")
	a := recordSourceTestCapture(t, repo, one)
	b := recordSourceTestCapture(t, repo, two)
	c := recordSourceTestCapture(t, repo, changed)
	require.Equal(t, a.RevisionUUID, b.RevisionUUID)
	require.Equal(t, a.RevisionUUID, c.RevisionUUID, "profile edits must not duplicate shared post text")
	require.Equal(t, a.Payload.Profiles, b.Payload.Profiles)
	require.NotEqual(t, a.Payload.Profiles, c.Payload.Profiles)
	otherPost := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "post-two"}, "")
	other := sourceTestCapture(t, otherPost.UUID, 1, "Edited biography")
	other.Payload.Shared = json.RawMessage(strings.ReplaceAll(string(other.Payload.Shared), "post-one", "post-two"))
	recordSourceTestCapture(t, repo, other)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Equal(t, uint(2), queryUint(t, raw, "SELECT count(*) FROM source_post_revisions"))
	require.Equal(t, uint(2), queryUint(t, raw, "SELECT count(*) FROM source_profile_bodies"))
	require.Equal(t, uint(4), queryUint(t, raw, "SELECT count(*) FROM source_capture_profiles"))
	require.Positive(t, queryUint(t, raw, "SELECT count(*) FROM source_payloads WHERE encoding='gzip'"))
	for _, input := range []models.SourceCaptureInput{one, two, changed, other} {
		require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
			capture, err := repo.SourceEvidence.FindCapture(ctx, input.UUID)
			require.NoError(t, err)
			require.NotNil(t, capture)
			want, err := archive.RestoreCapture(&input.Payload)
			require.NoError(t, err)
			got, err := archive.RestoreCapture(capture.Payload)
			require.NoError(t, err)
			require.Equal(t, want, got)
			require.Contains(t, string(got), "98765432109876543210")
			require.Equal(t, input.Metadata, capture.Metadata)
			require.Equal(t, input.ExtractorVersion, capture.ExtractorVersion)
			require.True(t, input.CapturedAt.Equal(capture.CapturedAt))
			return nil
		}))
	}
}

func TestSourceEvidenceReplayAcrossRestartAndConflictingDelivery(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	key := models.SourcePostIdentifier{Namespace: "native:twitter", Value: "post"}
	post := sourceTestPost(t, repo, key, "")
	input := sourceTestCapture(t, post.UUID, 1, "Bio")
	first := recordSourceTestCapture(t, repo, input)
	require.Equal(t, first, recordSourceTestCapture(t, repo, input))
	path := db.DatabasePath()
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(path))
	repo = db.Repository()
	require.Equal(t, first, recordSourceTestCapture(t, repo, input))
	for _, change := range []func(*models.SourceCaptureInput){
		func(c *models.SourceCaptureInput) { c.CapturedAt = c.CapturedAt.Add(time.Nanosecond) },
		func(c *models.SourceCaptureInput) { c.Origin = "another-producer" },
		func(c *models.SourceCaptureInput) { c.Platform = "other-platform" },
		func(c *models.SourceCaptureInput) { title := "Different title"; c.Metadata.Title = &title },
		func(c *models.SourceCaptureInput) { c.Payload = sourceTestCapture(t, post.UUID, 2, "Bio").Payload },
		func(c *models.SourceCaptureInput) {
			c.Payload = sourceTestCapture(t, post.UUID, 1, "Changed bio").Payload
		},
	} {
		changed := input
		change(&changed)
		err := repo.WithTxn(context.Background(), func(ctx context.Context) error {
			_, err := repo.SourceEvidence.RecordCapture(ctx, changed)
			return err
		})
		require.ErrorIs(t, err, models.ErrSourceCaptureReplay)
	}
	require.Equal(t, post.Revision+1, sourceTestPost(t, repo, key, "").Revision)
	raw := openRawDB(t, path)
	defer raw.Close()
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM source_captures"))
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM source_post_revisions"))
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM source_profile_bodies"))
}

func TestSourceEvidenceRetentionValidationAndTrustedImport(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "legacy:catalog:post", Value: "old"}, "")
	input := sourceTestCapture(t, post.UUID, 1, "Bio")
	raw := []byte(`{"category":"twitter","author":{"name":"Historical","followers_count":42,"old_custom_field":true},"_old_private_field":"preserve on import"}`)
	payload, err := archive.PrepareRetainedCapture("gallery-dl", "twitter", raw)
	require.NoError(t, err)
	input.Payload = *payload
	err = repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.SourceEvidence.RecordCapture(ctx, input)
		return err
	})
	require.ErrorContains(t, err, "retention policy")
	input.RetentionPolicy = "legacy-retained-v1"
	stored := recordSourceTestCapture(t, repo, input)
	restored, err := archive.RestoreCapture(stored.Payload)
	require.NoError(t, err)
	require.JSONEq(t, string(raw), string(restored))
	input.UUID = uuid.NewString()
	input.RetentionPolicy = "unsupported-future-policy"
	err = repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.SourceEvidence.RecordCapture(ctx, input)
		return err
	})
	require.ErrorContains(t, err, "unsupported source retention")
}

func TestSourceEvidenceCaptureFailureIsAtomic(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	key := models.SourcePostIdentifier{Namespace: "native:twitter", Value: "post"}
	post := sourceTestPost(t, repo, key, "")
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec(`CREATE TRIGGER fail_capture_reference BEFORE INSERT ON source_capture_profiles BEGIN SELECT RAISE(ABORT, 'fixture failure'); END`)
	require.NoError(t, err)
	input := sourceTestCapture(t, post.UUID, 1, "Bio")
	err = repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.SourceEvidence.RecordCapture(ctx, input)
		return err
	})
	require.ErrorContains(t, err, "fixture failure")
	require.Equal(t, post, sourceTestPost(t, repo, key, ""))
	for _, table := range []string{"source_payloads", "source_profile_bodies", "source_post_revisions", "source_captures", "source_capture_profiles"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table), table)
	}
	_, err = raw.Exec("DROP TRIGGER fail_capture_reference")
	require.NoError(t, err)
	recordSourceTestCapture(t, repo, input)
}

func TestSourceEvidenceStorageGuardsAndCorruptionDetection(t *testing.T) {
	for _, fixture := range []struct{ name, corrupt string }{
		{"missing reference", "DELETE FROM source_capture_profiles"},
		{"damaged gzip", "DROP TRIGGER source_payload_immutable; UPDATE source_payloads SET data=x'1f8b000000' WHERE encoding='gzip'"},
		{"wrong checksum", "DROP TRIGGER source_payload_immutable; UPDATE source_payloads SET data=CAST(replace(CAST(data AS TEXT), 'Bio', 'Lie') AS BLOB) WHERE encoding='json'"},
		{"changed provenance", "DROP TRIGGER source_capture_immutable; UPDATE source_captures SET platform='changed'"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			db, repo := archiveTestDatabase(t)
			post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "post"}, "")
			input := sourceTestCapture(t, post.UUID, 1, "Bio")
			recordSourceTestCapture(t, repo, input)
			raw := openRawDB(t, db.DatabasePath())
			defer raw.Close()
			_, err := raw.Exec("UPDATE source_payloads SET byte_length=byte_length")
			require.ErrorContains(t, err, "immutable")
			_, err = raw.Exec("UPDATE source_captures SET origin='changed'")
			require.ErrorContains(t, err, "immutable")
			_, err = raw.Exec(fixture.corrupt)
			require.NoError(t, err)
			err = repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
				_, err := repo.SourceEvidence.FindCapture(ctx, input.UUID)
				return err
			})
			require.ErrorIs(t, err, models.ErrSourcePayloadCorrupt)
		})
	}
}

func TestSourceEvidenceForgottenPostsCannotBeResurrected(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	key := models.SourcePostIdentifier{Namespace: "native:twitter", Value: "forgotten"}
	post := sourceTestPost(t, repo, key, "")
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec("UPDATE source_posts SET state='forgotten' WHERE uuid=?", post.UUID)
	require.NoError(t, err)
	require.Equal(t, "forgotten", sourceTestPost(t, repo, key, "").State)
	err = repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.SourceEvidence.RecordCapture(ctx, sourceTestCapture(t, post.UUID, 1, "Bio"))
		return err
	})
	require.ErrorIs(t, err, models.ErrSourcePostForgotten)
	err = repo.WithTxn(context.Background(), func(ctx context.Context) error {
		return repo.SourceEvidence.AddPostIdentifier(ctx, post.UUID, models.SourcePostIdentifier{Namespace: key.Namespace, Value: "renamed"}, post.Revision)
	})
	require.ErrorIs(t, err, models.ErrSourcePostForgotten)
	_, err = raw.Exec("UPDATE source_posts SET state='active' WHERE uuid=?", post.UUID)
	require.ErrorContains(t, err, "cannot be resurrected")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_payloads"))
}

func TestSourceEvidenceRevisionForeignKeysAndImmutableReferences(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	one := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "one"}, "")
	two := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "two"}, "")
	capture := recordSourceTestCapture(t, repo, sourceTestCapture(t, one.UUID, 1, "Bio"))
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	copyCapture := func(post string) error {
		_, err := raw.Exec(`INSERT INTO source_captures(uuid, post_uuid, revision_uuid, origin, platform, captured_at, extractor_version, retention_policy, patch_digest, signature)
SELECT ?, ?, revision_uuid, origin, platform, captured_at, extractor_version, retention_policy, patch_digest, signature FROM source_captures WHERE uuid=?`, uuid.NewString(), post, capture.UUID)
		return err
	}
	require.ErrorContains(t, copyCapture(two.UUID), "FOREIGN KEY", "a capture cannot use another post's revision")
	for _, query := range []string{
		"UPDATE source_post_revisions SET metadata='{}'",
		"UPDATE source_profile_bodies SET namespace='native:reddit'",
		"UPDATE source_capture_profiles SET path='/other'",
		"UPDATE source_post_identifiers SET value='changed'",
	} {
		_, err := raw.Exec(query)
		require.Error(t, err)
	}
	_, err := raw.Exec("UPDATE source_posts SET state='forgotten' WHERE uuid=?", one.UUID)
	require.NoError(t, err)
	require.ErrorContains(t, copyCapture(one.UUID), "cannot receive captures", "database constraints also guard alternate writers")
}

func TestSourceEvidenceCapturePagesAreBoundedAndIndexed(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "post"}, "")
	for i := 0; i < 4; i++ {
		input := sourceTestCapture(t, post.UUID, i+1, "Bio")
		// Same timestamp exercises the UUID tie breaker across page boundaries.
		recordSourceTestCapture(t, repo, input)
	}
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		var cursor *models.SourceCaptureCursor
		seen := make(map[string]bool)
		for {
			page, err := repo.SourceEvidence.Captures(ctx, post.UUID, cursor, 2)
			require.NoError(t, err)
			if len(page) == 0 {
				break
			}
			for _, item := range page {
				require.False(t, seen[item.UUID])
				seen[item.UUID] = true
				require.Nil(t, item.Payload, "summary pages must not load source bodies")
				cursor = &models.SourceCaptureCursor{CapturedAt: item.CapturedAt, UUID: item.UUID}
			}
		}
		require.Len(t, seen, 4)
		for _, limit := range []int{-1, 101} {
			_, err := repo.SourceEvidence.Captures(ctx, post.UUID, nil, limit)
			require.Error(t, err)
		}
		return nil
	}))
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	rows, err := raw.Query(`EXPLAIN QUERY PLAN SELECT uuid FROM source_captures
WHERE post_uuid=? AND (captured_at, uuid) > (?, ?) ORDER BY captured_at, uuid LIMIT 2`, post.UUID, "", "")
	require.NoError(t, err)
	defer rows.Close()
	var plans []string
	for rows.Next() {
		var id, parent, unused int
		var plan string
		require.NoError(t, rows.Scan(&id, &parent, &unused, &plan))
		plans = append(plans, plan)
	}
	require.NoError(t, rows.Err())
	require.Contains(t, strings.Join(plans, "\n"), "USING COVERING INDEX source_captures_post")
}
