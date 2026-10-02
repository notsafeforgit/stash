package sqlite_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

const relationTwitterAccount = "twitter:id:9007199254740993"
const relationMirrorAccount = "mirror:coomer:onlyfans:user:42"
const relationTwitterPost = "twitter:post:123"

// Rebuild a real received snapshot, retaining the capture reconstruction
// fixture while exercising every relationship family and multiple batches.
func relationFixture(t *testing.T, alter func([]map[string]any)) *catalogSnapshotFixture {
	t.Helper()
	f := snapshotFixture(t)
	var rows []map[string]any
	for i := range f.manifest.Chunks {
		for _, line := range bytes.Split(bytes.TrimSpace(f.chunk(t, i)), []byte("\n")) {
			value, err := archive.DecodeJSONObject(line, scrape.CatalogChunkLimit)
			require.NoError(t, err)
			rows = append(rows, value)
		}
	}
	add := func(table string, values map[string]any) {
		key := []any{}
		for _, name := range f.manifest.Tables[table].Key {
			key = append(key, values[name])
		}
		rows = append(rows, map[string]any{"table": table, "key": key, "values": values})
	}
	stamp := f.manifest.CapturedAt
	for _, pair := range []struct{ key, platform, id, basis string }{{relationTwitterAccount, "twitter", "9007199254740993", "source-id"}, {relationMirrorAccount, "onlyfans", "42", "mirror-user"}} {
		add("accounts", map[string]any{"account_key": pair.key, "platform": pair.platform, "source_id": pair.id, "identity_basis": pair.basis, "created_at": stamp})
	}
	for _, pair := range []struct{ key, handle string }{{relationTwitterAccount, "PriorHandle"}, {relationMirrorAccount, "Mirror Display Name"}, {"reddit:handle:juniper", "Juniper"}} {
		add("handles", map[string]any{"account_key": pair.key, "handle": pair.handle, "first_observed": "2025-01-02T03:04:05.123456789-07:00"})
	}
	add("posts", map[string]any{"post_key": relationTwitterPost, "platform": "twitter", "source_id": "123", "identity_basis": "source-id", "account_key": relationTwitterAccount, "created_at": stamp})
	add("posts", map[string]any{"post_key": "twitter:post:124", "platform": "twitter", "source_id": "124", "identity_basis": "source-id", "account_key": nil, "created_at": stamp})
	add("posts", map[string]any{"post_key": "onlyfans:post:42/5", "platform": "onlyfans", "source_id": "42/5", "identity_basis": "source-url", "account_key": relationMirrorAccount, "created_at": stamp})
	add("post_urls", map[string]any{"post_key": "onlyfans:post:42/5", "url": "https://coomer.st/onlyfans/user/42/post/5"})
	for i := 0; i < 55; i++ {
		add("post_urls", map[string]any{"post_key": relationTwitterPost, "url": fmt.Sprintf("https://example.test/post/123?copy=%02d", i)})
	}
	f.manifest.Tables["post_aliases"] = scrape.CatalogSnapshotTable{Key: []string{"alias_key"}, Columns: []scrape.CatalogSnapshotColumn{{CID: 0, Name: "alias_key", Type: "TEXT", PK: 1}, {CID: 1, Name: "post_key", Type: "TEXT", NotNull: 1}}}
	// Schema SQL is retained data, never executed by the import protocol.
	entry := f.manifest.Schema[0]
	entry.Type, entry.Name, entry.Table, entry.SQL = "table", "post_aliases", "post_aliases", "CREATE TABLE post_aliases(alias_key TEXT PRIMARY KEY,post_key TEXT NOT NULL REFERENCES posts(post_key))"
	f.manifest.Schema = append(f.manifest.Schema, entry)
	f.manifest.References["post_aliases.post_key"] = 0
	add("post_aliases", map[string]any{"alias_key": "twitter:post:original-local-alias", "post_key": relationTwitterPost})
	if alter != nil {
		alter(rows)
	}
	refreshFixtureCaptureInventory(t, f, rows)
	sort.Slice(rows, func(i, j int) bool {
		return scrape.CatalogRecordAfter(rows[j]["table"].(string), rows[j]["key"].([]any), rows[i]["table"].(string), rows[i]["key"].([]any))
	})
	byTable := map[string][]byte{}
	for key := range f.manifest.References {
		f.manifest.References[key] = 0
	}
	var chunk []byte
	f.chunks = [][]byte{}
	f.manifest.Chunks = nil
	for i, row := range rows {
		body, err := archive.EncodeSourceJSON(row)
		require.NoError(t, err)
		body = append(body, '\n')
		table := row["table"].(string)
		byTable[table] = append(byTable[table], body...)
		chunk = append(chunk, body...)
		for reference := range f.manifest.References {
			parts := strings.Split(reference, ".")
			if parts[0] == table && row["values"].(map[string]any)[parts[1]] != nil {
				f.manifest.References[reference]++
			}
		}
		if i%2 == 1 || i == len(rows)-1 {
			count := 2
			if i%2 == 0 {
				count = 1
			}
			f.manifest.Chunks = append(f.manifest.Chunks, scrape.CatalogSnapshotChunk{File: fmt.Sprintf("records-%06d.jsonl", len(f.chunks)), Rows: count, Bytes: len(chunk), SHA256: scrape.CatalogSnapshotSHA(chunk)})
			f.chunks = append(f.chunks, chunk)
			chunk = nil
		}
	}
	for name, table := range f.manifest.Tables {
		body := byTable[name]
		table.Rows = int64(bytes.Count(body, []byte("\n")))
		table.Bytes = int64(len(body))
		table.SHA256 = scrape.CatalogSnapshotSHA(body)
		f.manifest.Tables[name] = table
	}
	f.manifest.Records = int64(len(rows))
	var err error
	f.body, err = archive.EncodeSourceJSON(f.manifest)
	require.NoError(t, err)
	f.sha = scrape.CatalogSnapshotSHA(f.body)
	f.manifest, err = scrape.PrepareCatalogSnapshot(f.body, f.sha)
	require.NoError(t, err)
	f.begin(t)
	for i := range f.chunks {
		f.receive(t, i)
	}
	parent := advanceEvidence(t, f, 0)
	require.Contains(t, []string{"mapped", "review"}, parent.State)
	return f
}

func advanceRelations(t *testing.T, f *catalogSnapshotFixture, after int64) *models.CatalogRelationsImport {
	t.Helper()
	var result *models.CatalogRelationsImport
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = f.repo.CatalogRelationsImport.Advance(ctx, f.manifest.UUID, f.sha, after, catalogImportNow.Add(2*time.Hour))
		return err
	}))
	return result
}

func TestCatalogRelationsImportRetainsScopeAndOriginalEvidence(t *testing.T) {
	f := relationFixture(t, nil)
	first := advanceRelations(t, f, 0)
	require.Equal(t, "running", first.State)
	require.EqualValues(t, 50, first.ProcessedRecords)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	result := advanceRelations(t, f, first.LastOrdinal)
	require.Equal(t, "review", result.State)
	require.EqualValues(t, 3, result.ReviewRecords)
	require.EqualValues(t, 1, result.UnassignedRecords)
	require.Equal(t, result.TotalRecords, result.ProcessedRecords)
	require.False(t, result.Imported)
	require.Equal(t, result, advanceRelations(t, f, result.LastOrdinal))
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		rows, err := f.repo.CatalogRelationsImport.Records(ctx, f.manifest.UUID, 0, 100)
		require.NoError(t, err)
		require.Len(t, rows, int(result.TotalRecords))
		for _, row := range rows {
			detail, err := f.repo.CatalogRelationsImport.Record(ctx, f.manifest.UUID, row.Ordinal)
			require.NoError(t, err)
			require.Equal(t, row, detail.CatalogRelationRecord)
			var values map[string]any
			require.NoError(t, json.Unmarshal(detail.SourceValues, &values))
			if row.Table == "accounts" && row.Outcome == "mapped" {
				ids, err := f.repo.SourceAccount.Identifiers(ctx, *row.AccountUUID, "", 100)
				require.NoError(t, err)
				for _, id := range ids {
					if id.UUID == *row.IdentifierUUID {
						require.Equal(t, "legacy_key", id.Reference.Kind)
						require.Equal(t, values["account_key"], id.Reference.Value)
					}
				}
			}
			if row.Table == "handles" && values["account_key"] == relationMirrorAccount {
				ids, err := f.repo.SourceAccount.Identifiers(ctx, *row.AccountUUID, "", 100)
				require.NoError(t, err)
				for _, id := range ids {
					if id.UUID == *row.IdentifierUUID {
						require.Equal(t, "legacy_label", id.Reference.Kind)
						require.Equal(t, "Mirror Display Name", id.Reference.Value)
					}
				}
			}
			if row.Table == "post_aliases" {
				key, err := scrape.CatalogLocalPostReference(f.manifest.SourceUUID, f.manifest.CatalogID, values["alias_key"].(string))
				require.NoError(t, err)
				post, err := f.repo.SourceEvidence.FindPostByIdentifier(ctx, key)
				require.NoError(t, err)
				require.Equal(t, *row.PostUUID, post.UUID)
			}
		}
		return nil
	}))
	raw := openRawDB(t, f.db.DatabasePath())
	require.EqualValues(t, 56, queryUint(t, raw, "SELECT count(*) FROM source_post_urls"))
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM source_post_account_claims"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM capture_publisher_decisions"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_collection_media_intake"))
	for _, table := range []string{"catalog_relation_records", "catalog_relations_imports"} {
		_, err := raw.Exec("UPDATE " + table + " SET snapshot_uuid=snapshot_uuid")
		require.ErrorContains(t, err, "immutable")
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.NoError(t, raw.Close())
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	require.Equal(t, result, advanceRelations(t, f, result.LastOrdinal))
	output := filepath.Join(t.TempDir(), "anonymous.sqlite")
	export, err := sqlite.NewAnonymiser(f.db, output)
	require.NoError(t, err)
	require.NoError(t, export.Anonymise(t.Context()))
	raw = openRawDB(t, output)
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM catalog_relation_records"))
}

func TestCatalogRelationsImportConflictsAndLateFailure(t *testing.T) {
	f := relationFixture(t, nil)
	for _, test := range []struct {
		sha   string
		after int64
	}{{strings.Repeat("0", 64), 0}, {f.sha, 1}} {
		require.ErrorIs(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := f.repo.CatalogRelationsImport.Advance(ctx, f.manifest.UUID, test.sha, test.after, catalogImportNow)
			return err
		}), models.ErrCatalogSnapshotConflict)
	}
	key, err := scrape.CatalogLocalPostReference(f.manifest.SourceUUID, f.manifest.CatalogID, "twitter:post:original-local-alias")
	require.NoError(t, err)
	other := sourceTestPost(t, f.repo, key, uuid.NewString())
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := f.db.ExecSQL(ctx, "CREATE TRIGGER fail_catalog_relation BEFORE INSERT ON catalog_relation_records WHEN NEW.source_table='post_urls' BEGIN SELECT RAISE(ABORT,'fixture relation failure'); END", nil)
		return err
	}))
	require.ErrorContains(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.CatalogRelationsImport.Advance(ctx, f.manifest.UUID, f.sha, 0, catalogImportNow)
		require.ErrorContains(t, err, "fixture relation failure")
		return nil
	}), "did not finish atomically")
	raw := openRawDB(t, f.db.DatabasePath())
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM catalog_relation_records"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_post_url_evidence"))
	_, err = raw.Exec("DROP TRIGGER fail_catalog_relation")
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	progress := advanceRelations(t, f, 0)
	require.ErrorIs(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.CatalogRelationsImport.Advance(ctx, f.manifest.UUID, f.sha, 0, catalogImportNow)
		return err
	}), models.ErrCatalogSnapshotConflict)
	progress = advanceRelations(t, f, progress.LastOrdinal)
	require.EqualValues(t, 4, progress.ReviewRecords)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		post, err := f.repo.SourceEvidence.FindPostByIdentifier(ctx, key)
		require.NoError(t, err)
		require.Equal(t, other.UUID, post.UUID)
		return nil
	}))
}

func TestCatalogRelationsImportRequiresCompletedEvidence(t *testing.T) {
	f := receivedEvidenceFixture(t)
	require.ErrorIs(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.CatalogRelationsImport.Advance(ctx, f.manifest.UUID, f.sha, 0, catalogImportNow)
		return err
	}), models.ErrCatalogSnapshotConflict)
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		status, err := f.repo.CatalogRelationsImport.Find(ctx, f.manifest.UUID)
		require.NoError(t, err)
		require.Nil(t, status)
		return nil
	}))
}

func TestCatalogRelationsImportRetainsInvalidValuesAndForgottenPosts(t *testing.T) {
	for _, scenario := range []string{"forgotten", "wrong_account_namespace", "invalid_handle_time", "large_url"} {
		t.Run(scenario, func(t *testing.T) {
			f := relationFixture(t, func(rows []map[string]any) {
				for _, row := range rows {
					values := row["values"].(map[string]any)
					switch {
					case scenario == "wrong_account_namespace" && row["table"] == "posts" && values["post_key"] == relationTwitterPost:
						values["account_key"] = relationMirrorAccount
					case scenario == "invalid_handle_time" && row["table"] == "handles" && values["account_key"] == relationTwitterAccount:
						values["first_observed"] = "unknown"
					case scenario == "large_url" && row["table"] == "post_urls" && values["url"] == "https://example.test/post/123?copy=00":
						values["url"] = "https://example.test/" + strings.Repeat("x", 17000)
						row["key"] = []any{values["post_key"], values["url"]}
					}
				}
			})
			if scenario == "forgotten" {
				require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
					post, err := f.repo.SourceEvidence.FindPostByIdentifier(ctx, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "123"})
					require.NoError(t, err)
					_, _, err = f.db.ExecSQL(ctx, "UPDATE source_posts SET state='forgotten' WHERE uuid=?", []any{post.UUID})
					return err
				}))
			}
			result := advanceRelations(t, f, 0)
			for result.State == "running" {
				result = advanceRelations(t, f, result.LastOrdinal)
			}
			require.Equal(t, "review", result.State)
			reason := map[string]string{"forgotten": "post_forgotten", "wrong_account_namespace": "legacy_post_account_namespace_mismatch", "invalid_handle_time": "invalid_legacy_handle_time", "large_url": "legacy_relationship_key_exceeds_limit"}[scenario]
			require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				rows, err := f.repo.CatalogRelationsImport.Records(ctx, f.manifest.UUID, 0, 100)
				require.NoError(t, err)
				found := false
				for _, row := range rows {
					if row.Reason != reason {
						continue
					}
					found = true
					require.Nil(t, row.IdentifierUUID)
					require.Nil(t, row.URLEvidenceUUID)
					require.Nil(t, row.PostIdentifierEvidenceUUID)
					require.Nil(t, row.AccountClaimUUID)
					if scenario == "large_url" {
						require.True(t, row.KeyOmitted)
						require.Empty(t, row.Key)
						detail, err := f.repo.CatalogRelationsImport.Record(ctx, f.manifest.UUID, row.Ordinal)
						require.NoError(t, err)
						require.Contains(t, string(detail.SourceValues), strings.Repeat("x", 17000))
						require.False(t, detail.KeyOmitted)
					}
				}
				require.True(t, found, reason)
				return nil
			}))
			require.NoError(t, f.db.Close())
			require.NoError(t, f.db.Open(f.db.DatabasePath()))
		})
	}
}

func TestCatalogRelationsImportStartupRejectsIncompleteProgress(t *testing.T) {
	f := relationFixture(t, nil)
	advanceRelations(t, f, 0)
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, f.db.DatabasePath())
	_, err := raw.Exec("UPDATE catalog_relations_imports SET processed_records=processed_records+1,mapped_records=mapped_records+1")
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	require.ErrorContains(t, f.db.Open(f.db.DatabasePath()), "incomplete catalog relationship import receipts")
}
