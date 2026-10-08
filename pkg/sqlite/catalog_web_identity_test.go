package sqlite_test

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stretchr/testify/require"
)

// Exercise the existing importer rather than changing old catalog identity
// rules. A fresh snapshot can add captured native identifiers to a previously
// catalog-local post while its UUID, original capture and captured history
// survive. Snapshot replay retains its import proof.
func TestCatalogWebCaptureReusesImportedPostIdentity(t *testing.T) {
	for _, fixture := range []struct {
		platform, id, payload string
	}{
		{"jpgfish", "AbCd", `{"category":"jpgfish","id":"AbCd","url":"https://cdn.example/image.jpg"}`},
		{"imglike", "AbCd", `{"category":"imglike","id":"AbCd"}`},
		{"putmega", "AbCd", `{"category":"putmega","id":"AbCd"}`},
		{"leakgallery", "Creator/123", `{"category":"leakgallery","creator":"Creator","id":123}`},
		{"ytdl:thisvid", "123", `{"category":"ytdl","extractor_key":"ThisVid","id":"123"}`},
		{"ytdl:example.invalid", "video", `{"category":"ytdl-generic","extractor_key":"Generic","webpage_url":"https://example.invalid/posts/one","id":"video"}`},
	} {
		t.Run(fixture.platform, func(t *testing.T) {
			f := snapshotFixture(t)
			key := fixture.platform + ":post:" + fixture.id
			legacy, err := scrape.CatalogLocalPostReference(f.manifest.SourceUUID, f.manifest.CatalogID, key)
			require.NoError(t, err)
			oldPost := sourceTestPost(t, f.repo, legacy, "")
			retained, err := archive.PrepareRetainedCapture("legacy-nfo", fixture.platform, []byte(`{"nfo_fields":{"title":["Original title"]}}`))
			require.NoError(t, err)
			var oldCapture *models.SourceCapture
			require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				oldCapture, err = f.repo.SourceEvidence.RecordCapture(ctx, models.SourceCaptureInput{
					UUID: uuid.NewString(), PostUUID: oldPost.UUID, Origin: "legacy-nfo", Platform: fixture.platform,
					CapturedAt: catalogImportNow.Add(-time.Hour), RetentionPolicy: "legacy-retained-v1", Payload: *retained,
				})
				return err
			}))
			originalCapture, err := json.Marshal(oldCapture)
			require.NoError(t, err)
			newRows := webIdentityRows(t, f, fixture.platform, fixture.id, key, "gallery-dl", fixture.payload)
			receiveCatalogFixtureRows(t, f, newRows)
			snapshotUUID, snapshotSHA := f.manifest.UUID, f.sha
			var originalRecords []byte
			require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				records, err := f.repo.CatalogEvidenceImport.Records(ctx, snapshotUUID, 0, 100)
				require.NoError(t, err)
				originalRecords, err = json.Marshal(records)
				return err
			}))
			ref, err := archive.ExtractCapturedPost([]byte(fixture.payload))
			require.NoError(t, err)
			require.NotNil(t, ref)
			for range 2 {
				require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
					native, err := f.repo.SourceEvidence.FindPostByIdentifier(ctx, *ref)
					require.NoError(t, err)
					require.NotNil(t, native)
					require.Equal(t, oldPost.UUID, native.UUID)
					old, err := f.repo.SourceEvidence.FindPostByIdentifier(ctx, legacy)
					require.NoError(t, err)
					require.Equal(t, native.UUID, old.UUID)
					snapshot, err := f.repo.CatalogSnapshot.Find(ctx, snapshotUUID)
					require.NoError(t, err)
					require.Equal(t, snapshotSHA, snapshot.ManifestSHA256)
					records, err := f.repo.CatalogEvidenceImport.Records(ctx, snapshotUUID, 0, 100)
					require.NoError(t, err)
					encoded, err := json.Marshal(records)
					require.NoError(t, err)
					require.Equal(t, originalRecords, encoded)
					capture, err := f.repo.SourceEvidence.FindCapture(ctx, oldCapture.UUID)
					require.NoError(t, err)
					body, err := json.Marshal(capture)
					require.NoError(t, err)
					require.Equal(t, originalCapture, body)

					return nil
				}))
				require.NoError(t, f.db.Close())
				require.NoError(t, f.db.Open(f.db.DatabasePath()))
			}
			var importedState *models.CatalogEvidenceImport
			require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				state, err := f.repo.CatalogEvidenceImport.Find(ctx, snapshotUUID)
				importedState = state
				require.NoError(t, err)
				require.Equal(t, "mapped", state.State)
				require.Zero(t, state.ReviewRecords)
				return nil
			}))
			// Reopening and replaying the same immutable snapshot does not
			// create another post/capture or change its import proof.
			f.begin(t)
			for i := range f.chunks {
				f.receive(t, i)
			}
			require.Equal(t, importedState, advanceEvidence(t, f, importedState.LastOrdinal))
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_posts"))
			require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM source_captures"))
		})
	}
}

func webIdentityRows(t *testing.T, f *catalogSnapshotFixture, platform, id, key, origin, payload string) []map[string]any {
	t.Helper()
	var observation map[string]any
	var rows []map[string]any
	// Use the declared observation columns; no source SQL or catalog file is
	// edited. These rows go through the real manifest/hash/receipt protocol.
	for i := range f.manifest.Chunks {
		for _, line := range bytes.Split(bytes.TrimSpace(f.chunk(t, i)), []byte("\n")) {
			row, err := archive.DecodeJSONObject(line, scrape.CatalogChunkLimit)
			require.NoError(t, err)
			if row["table"] == "catalog_info" {
				rows = append(rows, row)
			}
			if row["table"] == "observations" {
				observation = row["values"].(map[string]any)
			}
		}
	}
	require.NotNil(t, observation)
	observation["post_key"], observation["observation_id"] = key, origin
	observation["origin"], observation["payload_json"], observation["account_refs_json"] = origin, payload, "[]"
	return append(rows, []map[string]any{
		{"table": "posts", "key": []any{key}, "values": map[string]any{
			"post_key": key, "platform": platform, "source_id": id, "identity_basis": "source-id",
			"account_key": nil, "created_at": f.manifest.CapturedAt}},
		{"table": "observations", "key": []any{origin}, "values": observation},
	}...)
}
