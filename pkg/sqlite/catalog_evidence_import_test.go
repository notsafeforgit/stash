package sqlite_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stretchr/testify/require"
)

func receivedEvidenceFixture(t *testing.T) *catalogSnapshotFixture {
	t.Helper()
	f := snapshotFixture(t)
	f.begin(t)
	for i := range f.manifest.Chunks {
		f.receive(t, i)
	}
	return f
}

func advanceEvidence(t *testing.T, f *catalogSnapshotFixture, after int64) *models.CatalogEvidenceImport {
	t.Helper()
	var result *models.CatalogEvidenceImport
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = f.repo.CatalogEvidenceImport.Advance(ctx, f.manifest.UUID, f.sha, after, catalogImportNow.Add(time.Hour))
		return err
	}))
	return result
}

func TestCatalogEvidenceImportRetainsActualCapturesAndSharedProfiles(t *testing.T) {
	f := receivedEvidenceFixture(t)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		snapshot, err := f.repo.CatalogSnapshot.Find(ctx, f.manifest.UUID)
		require.NoError(t, err)
		collection, err := f.repo.SourceCollection.Find(ctx, snapshot.CollectionUUID)
		require.NoError(t, err)
		definition := collection.SourceCollectionDefinition
		definition.State, definition.Label = "retired", "Later native choice"
		_, err = f.repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: collection.UUID, ExpectedRevision: collection.Revision,
			SourceCollectionDefinition: definition, Origin: "review", Reason: "Fixture preserves a later collection decision"})
		return err
	}))
	result := advanceEvidence(t, f, 0)
	require.Equal(t, "mapped", result.State)
	require.EqualValues(t, 6, result.ProcessedRecords)
	require.EqualValues(t, 3, result.CaptureMappings)
	require.EqualValues(t, 1, result.ProfileMappings)
	require.Zero(t, result.ReviewRecords)
	require.False(t, result.Imported)
	require.Equal(t, result, advanceEvidence(t, f, result.LastOrdinal))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	require.Equal(t, result, advanceEvidence(t, f, result.LastOrdinal))
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		records, err := f.repo.CatalogEvidenceImport.Records(ctx, f.manifest.UUID, 0, 100)
		require.NoError(t, err)
		require.Len(t, records, 6)
		var nativeProfiles, shared int
		for _, record := range records {
			if record.Outcome == "shared" {
				shared++
				require.Equal(t, `["shared"]`, record.Key)
				require.Nil(t, record.CaptureUUID)
			}
			if record.CaptureUUID == nil {
				continue
			}
			capture, err := f.repo.SourceEvidence.FindCapture(ctx, *record.CaptureUUID)
			require.NoError(t, err)
			require.Equal(t, "legacy-retained-v1", capture.RetentionPolicy)
			require.Equal(t, *record.PostUUID, capture.PostUUID)
			payload, err := archive.RestoreCapture(capture.Payload)
			require.NoError(t, err)
			var value map[string]any
			require.NoError(t, json.Unmarshal(payload, &value))
			if value["user"] != nil {
				nativeProfiles++
				require.Equal(t, "9007199254740993", value["user"].(map[string]any)["id"])
			}
		}
		require.Equal(t, 2, nativeProfiles)
		require.Equal(t, 1, shared)
		p, err := f.repo.Performer.Find(ctx, 71)
		require.NoError(t, err)
		require.Equal(t, "Shared name", p.Name)
		return nil
	}))
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_posts"))
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM source_post_revisions"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_profile_bodies"))
	require.EqualValues(t, 3, queryUint(t, raw, "SELECT count(*) FROM source_captures"))
	require.EqualValues(t, 3, queryUint(t, raw, "SELECT count(*) FROM source_collection_captures WHERE collection_revision=1"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_collection_media_intake"))
	for _, query := range []string{"UPDATE catalog_evidence_records SET reason='rewrite'", "UPDATE catalog_evidence_posts SET basis='rewrite'", "UPDATE catalog_evidence_imports SET state='running'"} {
		_, err := raw.Exec(query)
		require.ErrorContains(t, err, "immutable")
	}
}

func TestCatalogEvidenceImportChecksSnapshotAndProgress(t *testing.T) {
	f := snapshotFixture(t)
	f.begin(t)
	require.ErrorIs(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.CatalogEvidenceImport.Advance(ctx, f.manifest.UUID, f.sha, 0, catalogImportNow)
		return err
	}), models.ErrCatalogSnapshotConflict)
	for i := range f.manifest.Chunks {
		f.receive(t, i)
	}
	for _, test := range []struct {
		sha   string
		after int64
	}{{strings.Repeat("0", 64), 0}, {f.sha, 1}} {
		require.ErrorIs(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := f.repo.CatalogEvidenceImport.Advance(ctx, f.manifest.UUID, test.sha, test.after, catalogImportNow)
			return err
		}), models.ErrCatalogSnapshotConflict)
	}
	advanceEvidence(t, f, 0)
	require.ErrorIs(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.CatalogEvidenceImport.Advance(ctx, f.manifest.UUID, f.sha, 0, catalogImportNow)
		return err
	}), models.ErrCatalogSnapshotConflict)
}

func TestCatalogEvidenceImportLateErrorRollsBackEvenWhenSwallowed(t *testing.T) {
	f := receivedEvidenceFixture(t)
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := f.db.ExecSQL(ctx, `CREATE TRIGGER fail_catalog_evidence BEFORE INSERT ON catalog_evidence_records WHEN NEW.source_table='posts' BEGIN SELECT RAISE(ABORT,'fixture late mapping failure'); END`, nil)
		return err
	}))
	require.ErrorContains(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.CatalogEvidenceImport.Advance(ctx, f.manifest.UUID, f.sha, 0, catalogImportNow)
		require.ErrorContains(t, err, "fixture late mapping failure")
		return nil
	}), "did not finish atomically")
	raw := openRawDB(t, f.db.DatabasePath())
	for _, table := range []string{"source_posts", "source_captures", "source_profile_bodies", "source_payloads", "catalog_evidence_imports", "catalog_evidence_posts", "catalog_evidence_records"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table), table)
	}
	_, err := raw.Exec("DROP TRIGGER fail_catalog_evidence")
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	require.Equal(t, "mapped", advanceEvidence(t, f, 0).State)
}

func TestCatalogEvidenceImportPreservesForgottenAndConflictingPosts(t *testing.T) {
	for _, scenario := range []string{"forgotten", "different_service_id", "different_native_post"} {
		t.Run(scenario, func(t *testing.T) {
			f := receivedEvidenceFixture(t)
			row := map[string]any{"post_key": "reddit:post:album", "platform": "reddit", "source_id": "album", "identity_basis": "source"}
			identity, err := scrape.CatalogPostReference(f.manifest.SourceUUID, f.manifest.CatalogID, row, nil)
			require.NoError(t, err)
			legacy := sourceTestPost(t, f.repo, identity.Legacy, "")
			switch scenario {
			case "forgotten":
				raw := openRawDB(t, f.db.DatabasePath())
				_, err := raw.Exec("UPDATE source_posts SET state='forgotten' WHERE uuid=?", legacy.UUID)
				require.NoError(t, err)
				require.NoError(t, raw.Close())
			case "different_native_post":
				sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "album"}, "")
			default:
				require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
					return f.repo.SourceEvidence.AddPostIdentifier(ctx, legacy.UUID, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "different"}, legacy.Revision)
				}))
			}
			result := advanceEvidence(t, f, 0)
			require.Equal(t, "review", result.State)
			require.Positive(t, result.ReviewRecords)
			require.Zero(t, result.CaptureMappings)
			require.False(t, result.Imported)
			require.NoError(t, f.db.Close())
			require.NoError(t, f.db.Open(f.db.DatabasePath()))
		})
	}
}
