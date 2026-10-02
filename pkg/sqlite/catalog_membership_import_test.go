package sqlite_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func membershipImportFixture(t *testing.T, count int, alter func([]map[string]any)) *catalogSnapshotFixture {
	t.Helper()
	f := snapshotFixture(t)
	var rows []map[string]any
	for i := range f.manifest.Chunks {
		for _, line := range bytes.Split(bytes.TrimSpace(f.chunk(t, i)), []byte("\n")) {
			row, err := archive.DecodeJSONObject(line, scrape.CatalogChunkLimit)
			require.NoError(t, err)
			rows = append(rows, row)
		}
	}
	add := func(table string, values map[string]any) {
		var key []any
		for _, name := range f.manifest.Tables[table].Key {
			key = append(key, values[name])
		}
		rows = append(rows, map[string]any{"table": table, "key": key, "values": values})
	}
	for i := range count {
		postKey := "reddit:post:album"
		if i > 0 {
			id := fmt.Sprintf("album-%03d", i)
			postKey = "reddit:post:" + id
			add("posts", map[string]any{"post_key": postKey, "platform": "reddit", "source_id": id, "identity_basis": "source", "account_key": nil, "created_at": f.manifest.CapturedAt})
		}
		group := []struct{ key, kind, label string }{
			{"directory:Publisher, reddit", "creator", "Publisher, reddit"},
			{"directory:Purchased videos", "collection", "Purchased videos"},
			{"reddit:subreddit:aggregator", "subreddit", "Aggregator"},
		}[i%3]
		add("memberships", map[string]any{"post_key": postKey, "collection_key": group.key, "kind": group.kind, "label": group.label})
	}
	if alter != nil {
		alter(rows)
	}
	return receiveCatalogFixtureRows(t, f, rows)
}

func advanceMembershipImport(t *testing.T, f *catalogSnapshotFixture, after int64) *models.CatalogMembershipImport {
	t.Helper()
	var result *models.CatalogMembershipImport
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		result, err = f.repo.CatalogMembershipImport.Advance(ctx, f.manifest.UUID, f.sha, after, catalogImportNow.Add(5*time.Hour))
		return err
	}))
	return result
}

func TestCatalogMembershipImportPreservesGroupsWithoutAttribution(t *testing.T) {
	f := membershipImportFixture(t, 55, nil)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	untouched := map[string]uint{}
	for _, table := range []string{"source_accounts", "account_performer_decisions", "capture_publisher_decisions", "performers_scenes", "performers_images", "source_attachments", "source_captures", "galleries", "files"} {
		untouched[table] = queryUint(t, raw, "SELECT count(*) FROM "+table)
	}
	initialRevisions := queryUint(t, raw, "SELECT sum(revision) FROM source_posts")
	first := advanceMembershipImport(t, f, 0)
	require.Equal(t, "running", first.State)
	require.EqualValues(t, 50, first.ProcessedRecords)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	result := advanceMembershipImport(t, f, first.LastOrdinal)
	require.Equal(t, "mapped", result.State)
	require.False(t, result.Imported)
	require.EqualValues(t, 55, result.MappedRecords)
	require.Equal(t, result, advanceMembershipImport(t, f, result.LastOrdinal))
	require.EqualValues(t, 3, queryUint(t, raw, "SELECT count(*) FROM catalog_membership_groups"))
	require.EqualValues(t, 55, queryUint(t, raw, "SELECT count(*) FROM source_collection_post_evidence"))
	require.Equal(t, initialRevisions+55, queryUint(t, raw, "SELECT sum(revision) FROM source_posts"))
	require.EqualValues(t, 3, queryUint(t, raw, `SELECT count(*) FROM catalog_membership_groups g JOIN source_collection_revisions c ON c.collection_uuid=g.collection_uuid AND c.revision=g.collection_revision
 WHERE c.state='disabled' AND c.target_url='' AND c.root_uuid IS NULL AND c.account_uuid IS NULL`))
	for table, count := range untouched {
		require.Equal(t, count, queryUint(t, raw, "SELECT count(*) FROM "+table), table)
	}
	var recorded models.CollectionPostMembership
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		page, err := f.repo.CatalogMembershipImport.Records(ctx, f.manifest.UUID, 0, 1)
		require.NoError(t, err)
		require.Len(t, page, 1)
		next, err := f.repo.CatalogMembershipImport.Records(ctx, f.manifest.UUID, page[0].Ordinal, 100)
		require.NoError(t, err)
		require.Len(t, next, 54)
		for _, r := range append(page, next...) {
			detail, err := f.repo.CatalogMembershipImport.Record(ctx, f.manifest.UUID, r.Ordinal)
			require.NoError(t, err)
			require.Equal(t, r, detail.CatalogMembershipRecord)
			var values map[string]any
			require.NoError(t, json.Unmarshal(detail.SourceValues, &values))
			require.Len(t, values, 4)
		}
		memberships, err := f.repo.SourceCollection.PostMemberships(ctx, *page[0].PostUUID, "", 100)
		require.NoError(t, err)
		require.Len(t, memberships, 1)
		recorded = memberships[0]
		memberships, err = f.repo.SourceCollection.Memberships(ctx, recorded.CollectionUUID, "", 1)
		require.NoError(t, err)
		require.Len(t, memberships, 1)
		remainder, err := f.repo.SourceCollection.Memberships(ctx, recorded.CollectionUUID, memberships[0].UUID, 100)
		require.NoError(t, err)
		require.Greater(t, len(remainder), 10)
		return nil
	}))
	// Later review can rename or retire the group without rewriting its
	// historical definition or the observed memberships bound to it.
	putSourceCollection(t, f.repo, models.SourceCollectionInput{UUID: recorded.CollectionUUID, ExpectedRevision: 1, Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Reviewed grouping", Kind: "collection", State: "retired"}})
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		replay, err := f.repo.SourceCollection.RecordPostMembership(ctx, recorded)
		require.Equal(t, &recorded, replay)
		return err
	}))
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		changed := recorded
		changed.CollectionRevision++
		_, err := f.repo.SourceCollection.RecordPostMembership(ctx, changed)
		return err
	})
	require.ErrorIs(t, err, models.ErrSourcePostEvidenceReplay)
	attachmentSQL(t, f.db, "UPDATE source_posts SET state='forgotten'")
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.SourceCollection.RecordPostMembership(ctx, recorded)
		return err
	}))
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		recorded.UUID = uuid.NewString()
		_, err := f.repo.SourceCollection.RecordPostMembership(ctx, recorded)
		return err
	})
	require.ErrorIs(t, err, models.ErrSourcePostForgotten)
	output := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymiser, err := sqlite.NewAnonymiser(f.db, output)
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(t.Context()))
	anonymous := openRawDB(t, output)
	defer anonymous.Close()
	for _, table := range []string{"catalog_membership_records", "catalog_membership_imports", "catalog_membership_groups", "source_collection_post_evidence"} {
		require.Zero(t, queryUint(t, anonymous, "SELECT count(*) FROM "+table))
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestCatalogMembershipImportRetainsAmbiguousAndUnknownDefinitions(t *testing.T) {
	f := membershipImportFixture(t, 4, func(rows []map[string]any) {
		for _, r := range rows {
			if r["table"] != "memberships" {
				continue
			}
			v := r["values"].(map[string]any)
			switch v["post_key"] {
			case "reddit:post:album-001":
				v["kind"] = "unrecognized"
			case "reddit:post:album-003":
				v["label"] = "Conflicting label"
			}
		}
	})
	result := advanceMembershipImport(t, f, 0)
	require.Equal(t, "review", result.State)
	require.EqualValues(t, 2, result.MappedRecords)
	require.EqualValues(t, 2, result.ReviewRecords)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM catalog_membership_records WHERE reason='collection_definition_conflicts'"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM catalog_membership_records WHERE reason='unsupported_collection_identity'"))
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM catalog_membership_groups"))
}

func TestCatalogMembershipImportRollbackAndEmptyInput(t *testing.T) {
	f := membershipImportFixture(t, 3, nil)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	before := queryUint(t, raw, "SELECT sum(revision) FROM source_posts")
	attachmentSQL(t, f.db, `CREATE TRIGGER fail_membership_receipt BEFORE INSERT ON catalog_membership_records BEGIN SELECT RAISE(ABORT,'injected receipt failure'); END`)
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.CatalogMembershipImport.Advance(ctx, f.manifest.UUID, f.sha, 0, catalogImportNow)
		require.ErrorContains(t, err, "injected receipt failure")
		return nil
	})
	require.ErrorContains(t, err, "did not finish atomically")
	for _, table := range []string{"catalog_membership_records", "catalog_membership_imports", "catalog_membership_groups", "source_collection_post_evidence"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
	require.Equal(t, before, queryUint(t, raw, "SELECT sum(revision) FROM source_posts"))
	attachmentSQL(t, f.db, "DROP TRIGGER fail_membership_receipt")
	require.EqualValues(t, 3, advanceMembershipImport(t, f, 0).MappedRecords)
	empty := membershipImportFixture(t, 0, nil)
	result := advanceMembershipImport(t, empty, 0)
	require.Equal(t, "mapped", result.State)
	require.Zero(t, result.ProcessedRecords)
	require.Equal(t, result, advanceMembershipImport(t, empty, result.LastOrdinal))
}

func TestCatalogMembershipStartupRejectsWrongCollectionWithoutWrites(t *testing.T) {
	f := membershipImportFixture(t, 3, nil)
	advanceMembershipImport(t, f, 0)
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, f.db.DatabasePath())
	var guard string
	require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='catalog_membership_record_immutable'").Scan(&guard))
	_, err := raw.Exec(`DROP TRIGGER catalog_membership_record_immutable;
 UPDATE catalog_membership_records SET collection_uuid=(SELECT collection_uuid FROM catalog_membership_groups ORDER BY collection_uuid LIMIT 1)`)
	require.NoError(t, err)
	_, err = raw.Exec(guard)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	before, err := os.ReadFile(f.db.DatabasePath())
	require.NoError(t, err)
	require.ErrorContains(t, f.db.Open(f.db.DatabasePath()), "invalid catalog membership")
	after, err := os.ReadFile(f.db.DatabasePath())
	require.NoError(t, err)
	require.Equal(t, before, after)
}
