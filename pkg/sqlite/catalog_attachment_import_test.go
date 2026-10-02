package sqlite_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func attachmentImportFixture(t *testing.T, scenario string) *catalogSnapshotFixture {
	t.Helper()
	return relationFixture(t, func(rows []map[string]any) {
		for _, row := range rows {
			v := row["values"].(map[string]any)
			if row["table"] == "observations" && v["observation_id"] == "shared" {
				body, err := archive.DecodeJSONObject([]byte(v["payload_json"].(string)), archive.MaxSourcePayloadBytes)
				require.NoError(t, err)
				body["gallery_data"] = map[string]any{"items": []any{map[string]any{"media_id": "first"}, map[string]any{"media_id": "second"}}}
				body["media_metadata"] = map[string]any{"first": map[string]any{"e": "Image"}, "second": map[string]any{"e": "RedditVideo"}}
				switch scenario {
				case "invalid":
					body["gallery_data"] = map[string]any{"items": "invalid"}
				case "partial":
					body["gallery_data"] = map[string]any{"items": []any{map[string]any{"media_id": "first"}, nil}}
				case "many_conflicts":
					items := []any{}
					for i := range 200 {
						items = append(items, map[string]any{"media_id": strconv.Itoa(i)})
					}
					body["gallery_data"] = map[string]any{"items": items}
				case "single", "missing":
					delete(body, "gallery_data")
					if scenario == "single" {
						body["url"] = "https://i.redd.it/first.jpg"
					} else {
						body["num"], body["count"] = 1, 4
					}
				case "twitter":
					delete(body, "gallery_data")
					delete(body, "id")
					body["category"], body["tweet_id"] = "twitter", "123"
					body["extended_entities"] = map[string]any{"media": []any{map[string]any{"id_str": "first", "type": "photo"}, map[string]any{"id_str": "second", "type": "video"}}}
					v["post_key"] = relationTwitterPost
				}
				encoded, err := archive.EncodeSourceJSON(body)
				require.NoError(t, err)
				v["payload_json"] = string(encoded)
			}
			if row["table"] == "observation_details" && v["capture_id"] == "capture-1" && (scenario == "conflict" || scenario == "partial" || scenario == "many_conflicts") {
				items := []any{map[string]any{"media_id": "second"}, map[string]any{"media_id": "first"}}
				if scenario == "partial" {
					items = []any{nil, map[string]any{"media_id": "second"}}
				}
				if scenario == "many_conflicts" {
					items = nil
					for i := range 200 {
						items = append(items, map[string]any{"media_id": strconv.Itoa(199 - i)})
					}
				}
				body, err := archive.EncodeSourceJSON(map[string]any{"gallery_data": map[string]any{"items": items}, "filename": "1", "num": 2})
				require.NoError(t, err)
				v["payload_patch"] = string(body)
			}
			if scenario == "unmapped" && row["table"] == "posts" && v["post_key"] == "reddit:post:album" {
				v["source_id"], v["identity_basis"] = "different", "source-id"
			}
		}
	})
}

func advanceAttachments(t *testing.T, f *catalogSnapshotFixture, after int64) *models.CatalogAttachmentImport {
	t.Helper()
	var result *models.CatalogAttachmentImport
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = f.repo.CatalogAttachmentImport.Advance(ctx, f.manifest.UUID, f.sha, after, catalogImportNow.Add(4*time.Hour))
		return err
	}))
	return result
}

func importedAttachmentCapture(t *testing.T, f *catalogSnapshotFixture) *models.SourceCapture {
	t.Helper()
	var capture *models.SourceCapture
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		rows, err := f.repo.CatalogEvidenceImport.Records(ctx, f.manifest.UUID, 0, 100)
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.Table == "observation_details" {
				capture, err = f.repo.SourceEvidence.FindCapture(ctx, *row.CaptureUUID)
				return err
			}
		}
		return nil
	}))
	require.NotNil(t, capture)
	return capture
}

func TestCatalogAttachmentImportSharesListsAndCombinesPartialEvidence(t *testing.T) {
	for _, scenario := range []string{"reddit", "twitter", "partial", "single"} {
		t.Run(scenario, func(t *testing.T) {
			f := attachmentImportFixture(t, scenario)
			capture := importedAttachmentCapture(t, f)
			before := map[string][][]any{}
			require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				for _, table := range []string{"scenes", "images", "galleries", "performers_scenes", "performers_images"} {
					_, rows, err := f.db.QuerySQL(ctx, "SELECT * FROM "+table+" ORDER BY rowid", nil)
					if err != nil {
						return err
					}
					before[table] = rows
				}
				return nil
			}))
			result := advanceAttachments(t, f, 0)
			require.Equal(t, "mapped", result.State)
			require.False(t, result.Imported)
			require.EqualValues(t, 3, result.TotalRecords)
			require.EqualValues(t, 2, result.MappedRecords)
			require.EqualValues(t, 1, result.UnavailableRecords)
			changes := 1
			if scenario == "partial" {
				changes = 2
			}
			require.EqualValues(t, changes, result.ChangedSelections)
			require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				selection, err := f.repo.SourceAttachment.Selection(ctx, capture.PostUUID)
				require.NoError(t, err)
				require.Equal(t, "migration", selection.Decision.Origin)
				require.Equal(t, scenario != "single", selection.IsAlbum())
				require.Equal(t, scenario != "partial", selection.Complete)
				require.Equal(t, "first", selection.Entries[0].Attachment.Reference.Value)
				if scenario != "single" {
					require.Len(t, selection.Entries, 2)
					require.Equal(t, "second", selection.Entries[1].Attachment.Reference.Value)
					require.Equal(t, "video", selection.Entries[1].MediaKind)
				}
				rows, err := f.repo.CatalogAttachmentImport.Records(ctx, f.manifest.UUID, 0, 100)
				require.NoError(t, err)
				require.Len(t, rows, 3, "shared observation parent is not an extra capture")
				for _, row := range rows {
					detail, err := f.repo.CatalogAttachmentImport.Record(ctx, f.manifest.UUID, row.Ordinal)
					require.NoError(t, err)
					require.Equal(t, row, detail.CatalogAttachmentRecord)
					var view map[string]any
					require.NoError(t, json.Unmarshal(detail.Context, &view))
					require.Equal(t, archive.CapturedAlbumPolicy, view["policy"])
				}
				for table, rows := range before {
					_, current, err := f.db.QuerySQL(ctx, "SELECT * FROM "+table+" ORDER BY rowid", nil)
					require.NoError(t, err)
					require.Equal(t, rows, current, table)
				}
				return nil
			}))
			raw := openRawDB(t, f.db.DatabasePath())
			require.EqualValues(t, changes, queryUint(t, raw, "SELECT count(*) FROM source_attachment_manifests"))
			require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM source_capture_attachment_manifests"))
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_media_evidence"))
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
			require.NoError(t, raw.Close())
			require.NoError(t, f.db.Close())
			require.NoError(t, f.db.Open(f.db.DatabasePath()))
			require.Equal(t, result, advanceAttachments(t, f, result.LastOrdinal))
		})
	}
}

func TestCatalogAttachmentImportRetainsConflictsAndUnavailableEvidence(t *testing.T) {
	for _, scenario := range []string{"invalid", "conflict", "many_conflicts", "missing", "unmapped", "forgotten"} {
		t.Run(scenario, func(t *testing.T) {
			f := attachmentImportFixture(t, scenario)
			if scenario == "forgotten" {
				require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
					_, _, err := f.db.ExecSQL(ctx, "UPDATE source_posts SET state='forgotten'", nil)
					return err
				}))
			}
			result := advanceAttachments(t, f, 0)
			switch scenario {
			case "invalid":
				require.EqualValues(t, 2, result.ReviewRecords)
			case "conflict", "many_conflicts":
				require.EqualValues(t, 1, result.MappedRecords)
				require.EqualValues(t, 1, result.ReviewRecords)
				require.EqualValues(t, 1, result.ChangedSelections)
			case "missing", "forgotten":
				require.EqualValues(t, 3, result.UnavailableRecords)
			case "unmapped":
				require.EqualValues(t, 3, result.ReviewRecords)
			}
			if scenario == "many_conflicts" {
				require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
					rows, err := f.repo.CatalogAttachmentImport.Records(ctx, f.manifest.UUID, 0, 100)
					require.NoError(t, err)
					for _, row := range rows {
						if row.Outcome != "review" {
							continue
						}
						detail, err := f.repo.CatalogAttachmentImport.Record(ctx, f.manifest.UUID, row.Ordinal)
						require.NoError(t, err)
						var view struct {
							Conflicts []map[string]any `json:"conflicts"`
							Truncated bool             `json:"conflicts_truncated"`
						}
						require.NoError(t, json.Unmarshal(detail.Context, &view))
						require.Len(t, view.Conflicts, 128)
						require.Equal(t, "position", view.Conflicts[0]["kind"])
						require.EqualValues(t, 0, view.Conflicts[0]["position"])
						require.True(t, view.Truncated)
					}
					return nil
				}))
			}
			require.NoError(t, f.db.Close())
			require.NoError(t, f.db.Open(f.db.DatabasePath()))
		})
	}
}

func TestCatalogAttachmentImportPreservesReviewedSelections(t *testing.T) {
	for _, mode := range []string{"pinned", "disabled", "different_manifest"} {
		t.Run(mode, func(t *testing.T) {
			f := attachmentImportFixture(t, "reddit")
			capture := importedAttachmentCapture(t, f)
			raw, err := archive.RestoreCapture(capture.Payload)
			require.NoError(t, err)
			album, err := archive.ExtractCapturedAlbum(raw)
			require.NoError(t, err)
			album.Manifest.CaptureUUID = capture.UUID
			if mode == "different_manifest" {
				album.Manifest.Entries[0].Reference.Value = "reviewed-different"
			}
			recordAttachmentManifest(t, f.repo, album.Manifest)
			var prior *models.AttachmentSelection
			if mode != "different_manifest" {
				input := models.AttachmentSelectionInput{PostUUID: capture.PostUUID, ExpectedPostRevision: selectionPost(t, f.repo, capture.PostUUID).Revision, Mode: mode, Origin: "review", CaptureUUID: capture.UUID}
				if mode == "disabled" {
					input.CaptureUUID = ""
				}
				prior = applySelection(t, f.repo, input)
			}
			result := advanceAttachments(t, f, 0)
			if mode == "different_manifest" {
				require.EqualValues(t, 1, result.ReviewRecords)
				require.EqualValues(t, 1, result.MappedRecords)
			} else {
				require.EqualValues(t, 2, result.PreservedRecords)
				require.Zero(t, result.ChangedSelections)
				require.Equal(t, prior, previewSelection(t, f.repo, capture.PostUUID, capture.UUID).Current)
			}
			require.NoError(t, f.db.Close())
			require.NoError(t, f.db.Open(f.db.DatabasePath()))
		})
	}
}

func TestCatalogAttachmentImportRollbackBindingsAndAnonymisation(t *testing.T) {
	f := attachmentImportFixture(t, "reddit")
	raw := openRawDB(t, f.db.DatabasePath())
	_, err := raw.Exec("CREATE TRIGGER fail_attachment_receipt BEFORE INSERT ON catalog_attachment_records WHEN NEW.outcome='mapped' BEGIN SELECT RAISE(ABORT,'fixture attachment receipt failure'); END")
	require.NoError(t, err)
	require.ErrorContains(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.CatalogAttachmentImport.Advance(ctx, f.manifest.UUID, f.sha, 0, catalogImportNow)
		require.ErrorContains(t, err, "fixture attachment receipt failure")
		return nil
	}), "did not finish atomically")
	for _, table := range []string{"source_attachments", "source_attachment_manifests", "post_attachment_decisions", "catalog_attachment_imports"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
	_, err = raw.Exec("DROP TRIGGER fail_attachment_receipt")
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	result := advanceAttachments(t, f, 0)
	for _, test := range []struct {
		sha   string
		after int64
	}{{strings.Repeat("0", 64), result.LastOrdinal}, {f.sha, 0}} {
		require.ErrorIs(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := f.repo.CatalogAttachmentImport.Advance(ctx, f.manifest.UUID, test.sha, test.after, catalogImportNow)
			return err
		}), models.ErrCatalogSnapshotConflict)
	}
	out := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymous, err := sqlite.NewAnonymiser(f.db, out)
	require.NoError(t, err)
	require.NoError(t, anonymous.Anonymise(t.Context()))
	raw = openRawDB(t, out)
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM catalog_attachment_records"))
}

func TestCatalogAttachmentImportRequiresEvidenceAndValidStartupReceipts(t *testing.T) {
	f := receivedEvidenceFixture(t)
	require.ErrorIs(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.CatalogAttachmentImport.Advance(ctx, f.manifest.UUID, f.sha, 0, catalogImportNow)
		return err
	}), models.ErrCatalogSnapshotConflict)
	f = attachmentImportFixture(t, "reddit")
	advanceAttachments(t, f, 0)
	raw := openRawDB(t, f.db.DatabasePath())
	var guard string
	require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='catalog_attachment_record_immutable'").Scan(&guard))
	_, err := raw.Exec("DROP TRIGGER catalog_attachment_record_immutable")
	require.NoError(t, err)
	_, err = raw.Exec(`UPDATE catalog_attachment_records SET context_json='{"policy":"foreign-policy"}' WHERE outcome='mapped'`)
	require.NoError(t, err)
	_, err = raw.Exec(guard)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	require.NoError(t, f.db.Close())
	require.ErrorContains(t, f.db.Open(f.db.DatabasePath()), "incomplete catalog attachment import")
}
