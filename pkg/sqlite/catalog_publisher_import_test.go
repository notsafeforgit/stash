package sqlite_test

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

// The fixture still enters through snapshot reception and the real evidence
// importer. Rebuild its capture manifest when a test changes original payloads.
func refreshFixtureCaptureInventory(t *testing.T, f *catalogSnapshotFixture, rows []map[string]any) {
	t.Helper()
	platforms := map[string]string{}
	profiles := map[string]map[string]any{}
	children := map[string][]map[string]any{}
	var observations []map[string]any
	for _, row := range rows {
		value := row["values"].(map[string]any)
		switch row["table"] {
		case "posts":
			platforms[value["post_key"].(string)] = value["platform"].(string)
		case "observations":
			observations = append(observations, value)
		case "observation_details":
			key := value["observation_id"].(string)
			children[key] = append(children[key], value)
		case "account_snapshots":
			profile, err := scrape.CatalogProfile(value)
			require.NoError(t, err)
			profiles[value["snapshot_id"].(string)] = profile
		}
	}
	sort.Slice(observations, func(i, j int) bool {
		left, right := observations[i], observations[j]
		if left["post_key"] != right["post_key"] {
			return left["post_key"].(string) < right["post_key"].(string)
		}
		return left["observation_id"].(string) < right["observation_id"].(string)
	})
	inventory := &f.manifest.Captures
	inventory.Count, inventory.Flat, inventory.ProfileRefs, inventory.MaxBytes = 0, 0, 0, 0
	var headers bytes.Buffer
	for _, observation := range observations {
		details := children[observation["observation_id"].(string)]
		sort.Slice(details, func(i, j int) bool {
			if details[i]["captured_at"] != details[j]["captured_at"] {
				return details[i]["captured_at"].(string) < details[j]["captured_at"].(string)
			}
			return details[i]["capture_id"].(string) < details[j]["capture_id"].(string)
		})
		if len(details) == 0 {
			details = []map[string]any{nil}
		}
		for _, detail := range details {
			capture, err := scrape.ReconstructCatalogCapture(observation, detail, platforms[observation["post_key"].(string)], func(key string) (map[string]any, error) { return profiles[key], nil })
			require.NoError(t, err)
			headers.Write(capture.Header)
			headers.WriteByte('\n')
			inventory.Count++
			if capture.Flat {
				inventory.Flat++
			}
			inventory.ProfileRefs += int64(capture.ProfileReferences)
			inventory.MaxBytes = max(inventory.MaxBytes, len(capture.Payload))
		}
	}
	inventory.SHA256 = scrape.CatalogSnapshotSHA(headers.Bytes())
}

func publisherImportFixture(t *testing.T, scenario string) *catalogSnapshotFixture {
	t.Helper()
	f := relationFixture(t, func(rows []map[string]any) {
		for _, row := range rows {
			values := row["values"].(map[string]any)
			if row["table"] == "observations" && values["observation_id"] == "shared" {
				body, err := archive.DecodeJSONObject([]byte(values["payload_json"].(string)), archive.MaxSourcePayloadBytes)
				require.NoError(t, err)
				body["author_fullname"], body["author"] = "t2_actual_publisher", "ActualPublisher"
				if scenario == "invalid" {
					body["author_fullname"] = true
				}
				encoded, err := archive.EncodeSourceJSON(body)
				require.NoError(t, err)
				values["payload_json"] = string(encoded)
			}
			if scenario == "unmapped" && row["table"] == "posts" && values["post_key"] == "reddit:post:album" {
				values["source_id"], values["identity_basis"] = "different", "source-id"
			}
		}
	})
	relations := advanceRelations(t, f, 0)
	for relations.State == "running" {
		relations = advanceRelations(t, f, relations.LastOrdinal)
	}
	return f
}

func advancePublishers(t *testing.T, f *catalogSnapshotFixture, after int64) *models.CatalogPublisherImport {
	t.Helper()
	var result *models.CatalogPublisherImport
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = f.repo.CatalogPublisherImport.Advance(ctx, f.manifest.UUID, f.sha, after, catalogImportNow.Add(3*time.Hour))
		return err
	}))
	return result
}

func TestCatalogPublisherImportUsesCapturedIDsWithoutAttributingPerformers(t *testing.T) {
	f := publisherImportFixture(t, "")
	const attributionSQL = "SELECT 'scene',performer_id,scene_id FROM performers_scenes UNION ALL SELECT 'image',performer_id,image_id FROM performers_images ORDER BY 1,2,3"
	var originalAttribution [][]any
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		_, originalAttribution, err = f.db.QuerySQL(ctx, attributionSQL, nil)
		return err
	}))
	result := advancePublishers(t, f, 0)
	require.Equal(t, "mapped", result.State)
	require.False(t, result.Imported)
	require.EqualValues(t, 3, result.TotalRecords)
	require.Equal(t, result.TotalRecords, result.ProcessedRecords)
	require.EqualValues(t, 2, result.LinkedRecords)
	require.EqualValues(t, 1, result.CreatedAccounts)
	require.EqualValues(t, 1, result.UnavailableRecords)
	require.Equal(t, result, advancePublishers(t, f, result.LastOrdinal))
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		rows, err := f.repo.CatalogPublisherImport.Records(ctx, f.manifest.UUID, 0, 100)
		require.NoError(t, err)
		require.Len(t, rows, 3, "shared observation is not an extra capture")
		var account string
		for _, row := range rows {
			if row.Outcome == "linked" {
				require.NotNil(t, row.AccountUUID)
				if account == "" {
					account = *row.AccountUUID
				}
				require.Equal(t, account, *row.AccountUUID)
				current, err := f.repo.CapturePublisher.Current(ctx, *row.CaptureUUID)
				require.NoError(t, err)
				require.Equal(t, current.UUID, *row.DecisionUUID)
			}
			detail, err := f.repo.CatalogPublisherImport.Record(ctx, f.manifest.UUID, row.Ordinal)
			require.NoError(t, err)
			require.Equal(t, row, detail.CatalogPublisherRecord)
			var view map[string]any
			require.NoError(t, json.Unmarshal(detail.Context, &view))
			require.Equal(t, archive.CapturedAccountPolicy, view["policy"])
		}
		owner, err := f.repo.SourceAccount.Find(ctx, account)
		require.NoError(t, err)
		require.Equal(t, "ActualPublisher", owner.Label, "feed owner profile cannot replace captured publisher")
		performer, err := f.repo.Performer.Find(ctx, 71)
		require.NoError(t, err)
		require.Equal(t, "Shared name", performer.Name)
		_, currentAttribution, err := f.db.QuerySQL(ctx, attributionSQL, nil)
		require.NoError(t, err)
		require.Equal(t, originalAttribution, currentAttribution)
		return nil
	}))
	raw := openRawDB(t, f.db.DatabasePath())
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM capture_publisher_decisions"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_collection_media_intake"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.NoError(t, raw.Close())
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	require.Equal(t, result, advancePublishers(t, f, result.LastOrdinal))
	out := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymous, err := sqlite.NewAnonymiser(f.db, out)
	require.NoError(t, err)
	require.NoError(t, anonymous.Anonymise(t.Context()))
	raw = openRawDB(t, out)
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM catalog_publisher_records"))
}

func TestCatalogPublisherImportRetainsReviewAndUnavailableOutcomes(t *testing.T) {
	for _, scenario := range []string{"invalid", "unmapped", "handle_only", "ambiguous_id", "forgotten"} {
		t.Run(scenario, func(t *testing.T) {
			f := publisherImportFixture(t, scenario)
			switch scenario {
			case "handle_only", "ambiguous_id":
				kind, value, count := "handle", "ActualPublisher", 1
				if scenario == "ambiguous_id" {
					kind, value, count = "id", "t2_actual_publisher", 2
				}
				for range count {
					account := createSourceAccount(t, f.repo, "native:reddit")
					observeAccount(t, f.repo, account.UUID, models.AccountReference{Namespace: account.Namespace, Kind: kind, Value: value}, accountEvidence())
				}
			case "forgotten":
				require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
					_, _, err := f.db.ExecSQL(ctx, "UPDATE source_posts SET state='forgotten' WHERE uuid IN (SELECT post_uuid FROM source_captures)", nil)
					return err
				}))
			}
			result := advancePublishers(t, f, 0)
			require.Zero(t, result.LinkedRecords)
			require.Zero(t, result.CreatedAccounts)
			switch scenario {
			case "forgotten":
				require.EqualValues(t, 3, result.UnavailableRecords)
			case "unmapped":
				require.EqualValues(t, 3, result.ReviewRecords)
				require.Zero(t, result.UnavailableRecords)
			default:
				require.Equal(t, "review", result.State)
				require.EqualValues(t, 2, result.ReviewRecords)
				require.EqualValues(t, 1, result.UnavailableRecords)
			}
			require.NoError(t, f.db.Close())
			require.NoError(t, f.db.Open(f.db.DatabasePath()))
		})
	}
}

func TestCatalogPublisherImportPreservesUnlinksAndRollsBackLateFailures(t *testing.T) {
	f := publisherImportFixture(t, "")
	var capture string
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		rows, err := f.repo.CatalogEvidenceImport.Records(ctx, f.manifest.UUID, 0, 100)
		require.NoError(t, err)
		for _, row := range rows {
			if row.Table == "observation_details" {
				capture = *row.CaptureUUID
				break
			}
		}
		return nil
	}))
	decision, err := applyPublisher(f.repo, publisherInput(publisherPreview(t, f.repo, capture, ""), "unlink"))
	require.NoError(t, err)
	raw := openRawDB(t, f.db.DatabasePath())
	before := queryUint(t, raw, "SELECT count(*) FROM source_accounts")
	_, err = raw.Exec("CREATE TRIGGER fail_publisher_receipt BEFORE INSERT ON catalog_publisher_records WHEN NEW.outcome='linked' BEGIN SELECT RAISE(ABORT,'fixture publisher receipt failure'); END")
	require.NoError(t, err)
	require.ErrorContains(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.CatalogPublisherImport.Advance(ctx, f.manifest.UUID, f.sha, 0, catalogImportNow)
		require.ErrorContains(t, err, "fixture publisher receipt failure")
		return nil
	}), "did not finish atomically")
	require.Equal(t, before, queryUint(t, raw, "SELECT count(*) FROM source_accounts"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM capture_publisher_decisions"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM catalog_publisher_imports"))
	_, err = raw.Exec("DROP TRIGGER fail_publisher_receipt")
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	result := advancePublishers(t, f, 0)
	require.EqualValues(t, 1, result.PreservedRecords)
	require.EqualValues(t, 1, result.LinkedRecords)
	require.EqualValues(t, 1, result.CreatedAccounts)
	require.Equal(t, decision, publisherPreview(t, f.repo, capture, "").Current)
	for _, test := range []struct {
		sha   string
		after int64
	}{{strings.Repeat("0", 64), result.LastOrdinal}, {f.sha, 0}} {
		require.ErrorIs(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := f.repo.CatalogPublisherImport.Advance(ctx, f.manifest.UUID, test.sha, test.after, catalogImportNow)
			return err
		}), models.ErrCatalogSnapshotConflict)
	}
}

func TestCatalogPublisherImportRequiresRelations(t *testing.T) {
	f := receivedEvidenceFixture(t)
	advanceEvidence(t, f, 0)
	require.ErrorIs(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.CatalogPublisherImport.Advance(ctx, f.manifest.UUID, f.sha, 0, catalogImportNow)
		return err
	}), models.ErrCatalogSnapshotConflict)
}

func TestCatalogPublisherImportStartupRejectsChangedDecisionContext(t *testing.T) {
	f := publisherImportFixture(t, "")
	advancePublishers(t, f, 0)
	raw := openRawDB(t, f.db.DatabasePath())
	var guard string
	require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='catalog_publisher_record_immutable'").Scan(&guard))
	_, err := raw.Exec("DROP TRIGGER catalog_publisher_record_immutable")
	require.NoError(t, err)
	_, err = raw.Exec(`UPDATE catalog_publisher_records SET context_json='{"policy":"foreign-policy"}' WHERE outcome='linked'`)
	require.NoError(t, err)
	_, err = raw.Exec(guard)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	require.NoError(t, f.db.Close())
	require.ErrorContains(t, f.db.Open(f.db.DatabasePath()), "incomplete catalog publisher import")
}
