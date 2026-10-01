package sqlite_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func publisherCapture(t *testing.T, repo models.Repository, namespace, platform, raw string) *models.SourceCapture {
	t.Helper()
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: namespace, Value: uuid.NewString()}, "")
	retained, err := archive.RetainSourcePayload([]byte(raw))
	require.NoError(t, err)
	payload, err := archive.PrepareRetainedCapture("gallery-dl", platform, retained)
	require.NoError(t, err)
	return recordSourceTestCapture(t, repo, models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: post.UUID, Origin: "gallery-dl", Platform: platform,
		CapturedAt: time.Date(2026, 9, 30, 1, 2, 3, 4, time.UTC), RetentionPolicy: archive.SourceRetentionVersion, Payload: *payload})
}

func publisherPreview(t *testing.T, repo models.Repository, capture, target string) *models.CapturePublisherPreview {
	t.Helper()
	var ret *models.CapturePublisherPreview
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		var err error
		ret, err = repo.CapturePublisher.Preview(ctx, capture, target)
		return err
	}))
	return ret
}

func publisherInput(preview *models.CapturePublisherPreview, action string) models.CapturePublisherInput {
	ret := models.CapturePublisherInput{UUID: uuid.NewString(), CaptureUUID: preview.CaptureUUID, ExpectedSignature: preview.Signature, Action: action}
	if action != "automatic" {
		ret.Origin = "review"
	}
	if action == "link" && preview.Target != nil {
		ret.AccountUUID = preview.Target.UUID
	}
	return ret
}

func applyPublisher(repo models.Repository, input models.CapturePublisherInput) (*models.CapturePublisherDecision, error) {
	var ret *models.CapturePublisherDecision
	err := repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		ret, err = repo.CapturePublisher.Apply(ctx, input)
		return err
	})
	return ret, err
}

func TestCapturePublisherCreatesOneAccountAndRetainsAttributionBoundary(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	capture := publisherCapture(t, repo, "native:twitter", "twitter", `{"category":"twitter","tweet_id":"one","author":{"id":98765432109876543210,"name":"Publisher","description":"Profile"},"content":"Caption"}`)
	preview := publisherPreview(t, repo, capture.UUID, "")
	require.Equal(t, "create", preview.Action)
	input := publisherInput(preview, "automatic")
	first, err := applyPublisher(repo, input)
	require.NoError(t, err)
	require.Equal(t, "linked", first.State)
	require.Equal(t, "capture", first.Origin)
	require.Equal(t, archive.CapturedAccountPolicy, first.Policy)
	require.NotNil(t, first.AccountUUID)
	account := findSourceAccount(t, repo, *first.AccountUUID)
	require.Equal(t, "Publisher", account.Label)
	require.Equal(t, "native:twitter", account.Namespace)
	replay, err := applyPublisher(repo, input)
	require.NoError(t, err)
	require.Equal(t, first, replay)
	bad := input
	bad.Reason = "changed request"
	_, err = applyPublisher(repo, bad)
	require.ErrorIs(t, err, models.ErrCapturePublisherReplay)
	preserved := publisherPreview(t, repo, capture.UUID, "")
	require.Equal(t, "preserve", preserved.Action)
	_, err = applyPublisher(repo, publisherInput(preserved, "automatic"))
	require.ErrorIs(t, err, models.ErrCapturePublisherConflict)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM source_accounts"))
	require.Equal(t, uint(2), queryUint(t, raw, "SELECT count(*) FROM source_account_identifiers"))
	require.Equal(t, uint(2), queryUint(t, raw, "SELECT count(*) FROM capture_publisher_claims"))
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM capture_publisher_decisions"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM account_performer_links"))
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM performers_scenes"), "publisher resolution does not infer depicted performers")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM performers_images"))
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		ids, err := repo.SourceAccount.Identifiers(ctx, account.UUID, "", 100)
		require.NoError(t, err)
		require.Len(t, ids, 2)
		for _, id := range ids {
			evidence, err := repo.SourceAccount.Evidence(ctx, id.UUID, "", 100)
			require.NoError(t, err)
			require.Len(t, evidence, 1)
			var details map[string]string
			require.NoError(t, json.Unmarshal(evidence[0].Details, &details))
			require.Equal(t, capture.UUID, details["capture_uuid"])
			require.Equal(t, capture.CapturedAt, evidence[0].FirstObserved)
		}
		return nil
	}))
}

func TestCapturePublisherReusesIDsAndKeepsHandleCandidatesForReview(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	handle := createSourceAccount(t, repo, "native:twitter")
	observeAccount(t, repo, handle.UUID, models.AccountReference{Namespace: handle.Namespace, Kind: "handle", Value: "old-name"}, accountEvidence())
	capture := publisherCapture(t, repo, "native:twitter", "twitter", `{"category":"twitter","author":{"id":"123","name":"Old-Name"}}`)
	preview := publisherPreview(t, repo, capture.UUID, "")
	require.Equal(t, "review", preview.Action)
	require.Contains(t, preview.Conflicts, "locator_only_candidates")
	_, err := applyPublisher(repo, publisherInput(preview, "automatic"))
	require.ErrorIs(t, err, models.ErrCapturePublisherConflict)
	// A reviewer can establish the account pair; its captured identifier claims
	// then support a later handle change without selecting a namesake account.
	linked, err := applyPublisher(repo, publisherInput(publisherPreview(t, repo, capture.UUID, handle.UUID), "link"))
	require.NoError(t, err)
	require.Equal(t, handle.UUID, *linked.AccountUUID)
	other := createSourceAccount(t, repo, "native:twitter")
	observeAccount(t, repo, other.UUID, models.AccountReference{Namespace: other.Namespace, Kind: "handle", Value: "new-name"}, accountEvidence())
	later := publisherCapture(t, repo, "native:twitter", "twitter", `{"category":"twitter","author":{"id":"123","name":"New-Name"}}`)
	automatic := publisherPreview(t, repo, later.UUID, "")
	require.Equal(t, "link", automatic.Action)
	require.Equal(t, handle.UUID, *automatic.AccountUUID)
	require.Len(t, automatic.Candidates, 2)
	resolved, err := applyPublisher(repo, publisherInput(automatic, "automatic"))
	require.NoError(t, err)
	require.Equal(t, handle.UUID, *resolved.AccountUUID)
	// A handle-only record is not silently treated as the same account. A
	// reviewer may instead choose a new account after seeing the existing match.
	reused := publisherCapture(t, repo, "native:twitter", "twitter", `{"category":"twitter","author":{"id":"456","name":"New-Name"}}`)
	review := publisherPreview(t, repo, reused.UUID, "")
	require.Equal(t, "review", review.Action)
	fresh, err := applyPublisher(repo, publisherInput(review, "create"))
	require.NoError(t, err)
	require.NotEqual(t, handle.UUID, *fresh.AccountUUID)
	require.NotEqual(t, other.UUID, *fresh.AccountUUID)
	conflict := publisherPreview(t, repo, reused.UUID, handle.UUID)
	require.Contains(t, conflict.Conflicts, "target_stable_id_conflict")
	_, err = applyPublisher(repo, publisherInput(conflict, "link"))
	require.ErrorIs(t, err, models.ErrCapturePublisherConflict)
}

func TestCapturePublisherAmbiguityAndScope(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	one, two := createSourceAccount(t, repo, "native:tiktok"), createSourceAccount(t, repo, "native:tiktok")
	for _, account := range []*models.SourceAccount{one, two} {
		observeAccount(t, repo, account.UUID, models.AccountReference{Namespace: account.Namespace, Kind: "id", Value: "123"}, accountEvidence())
	}
	capture := publisherCapture(t, repo, "native:tiktok", "tiktok", `{"category":"tiktok","author":{"id":"123","secUid":"opaque","uniqueId":"handle"}}`)
	preview := publisherPreview(t, repo, capture.UUID, "")
	require.Equal(t, "review", preview.Action)
	require.Contains(t, preview.Conflicts, "ambiguous_stable_id")
	for _, action := range []string{"automatic", "create"} {
		_, err := applyPublisher(repo, publisherInput(preview, action))
		require.ErrorIs(t, err, models.ErrCapturePublisherConflict)
	}
	chosen, err := applyPublisher(repo, publisherInput(publisherPreview(t, repo, capture.UUID, one.UUID), "link"))
	require.NoError(t, err)
	require.Equal(t, one.UUID, *chosen.AccountUUID)
	// A contradicting secondary ID on the selected account also needs review.
	later := publisherCapture(t, repo, "native:tiktok", "tiktok", `{"category":"tiktok","author":{"secUid":"different","id":"123","uniqueId":"handle"}}`)
	bad := publisherPreview(t, repo, later.UUID, one.UUID)
	require.Contains(t, bad.Conflicts, "target_stable_id_conflict")
	_, err = applyPublisher(repo, publisherInput(bad, "link"))
	require.ErrorIs(t, err, models.ErrCapturePublisherConflict)
	mirror := createSourceAccount(t, repo, "mirror:coomer:tiktok")
	crossService := publisherPreview(t, repo, capture.UUID, mirror.UUID)
	require.Contains(t, crossService.Conflicts, "target_namespace_mismatch")
	_, err = applyPublisher(repo, publisherInput(crossService, "link"))
	require.ErrorIs(t, err, models.ErrCapturePublisherConflict)
	wrongPost := publisherCapture(t, repo, "native:reddit", "twitter", `{"category":"twitter","author":{"id":"123","name":"example"}}`)
	scope := publisherPreview(t, repo, wrongPost.UUID, "")
	require.Contains(t, scope.Conflicts, "captured_namespace_mismatch")
	_, err = applyPublisher(repo, publisherInput(scope, "automatic"))
	require.ErrorIs(t, err, models.ErrCapturePublisherConflict)
}

func TestCapturePublisherReviewStalenessAndUnrelatedCaptures(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	body := `{"category":"twitter","author":{"id":"stable","name":"Publisher"}}`
	one := publisherCapture(t, repo, "native:twitter", "twitter", body)
	two := publisherCapture(t, repo, "native:twitter", "twitter", body)
	old := publisherInput(publisherPreview(t, repo, one.UUID, ""), "automatic")
	first, err := applyPublisher(repo, publisherInput(publisherPreview(t, repo, two.UUID, ""), "automatic"))
	require.NoError(t, err)
	_, err = applyPublisher(repo, old)
	require.ErrorIs(t, err, models.ErrCapturePublisherConflict, "another capture allocated this identity")
	fresh := publisherPreview(t, repo, one.UUID, "")
	require.Equal(t, "link", fresh.Action)
	three := publisherCapture(t, repo, "native:twitter", "twitter", body)
	_, err = applyPublisher(repo, publisherInput(publisherPreview(t, repo, three.UUID, ""), "automatic"))
	require.NoError(t, err)
	require.Equal(t, fresh.Signature, publisherPreview(t, repo, one.UUID, "").Signature, "unrelated observation counters must not churn the review signature")
	_, err = applyPublisher(repo, publisherInput(fresh, "automatic"))
	require.NoError(t, err)
	// A new competing identity is relevant, including when source observations
	// for other accounts are changing concurrently.
	four := publisherCapture(t, repo, "native:twitter", "twitter", body)
	before := publisherInput(publisherPreview(t, repo, four.UUID, ""), "automatic")
	competing := createSourceAccount(t, repo, "native:twitter")
	observeAccount(t, repo, competing.UUID, models.AccountReference{Namespace: competing.Namespace, Kind: "id", Value: "stable"}, accountEvidence())
	_, err = applyPublisher(repo, before)
	require.ErrorIs(t, err, models.ErrCapturePublisherConflict)
	current := publisherPreview(t, repo, four.UUID, *first.AccountUUID)
	require.Equal(t, "review", current.Action)
	_, err = applyPublisher(repo, publisherInput(current, "link"))
	require.NoError(t, err)
}

func TestCapturePublisherExplicitUnlinkInheritanceAndConsolidation(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	capture := publisherCapture(t, repo, "native:reddit", "reddit", `{"category":"reddit","author_fullname":"t2_author","author":"Example"}`)
	original := publisherInput(publisherPreview(t, repo, capture.UUID, ""), "automatic")
	linked, err := applyPublisher(repo, original)
	require.NoError(t, err)
	unlinked, err := applyPublisher(repo, publisherInput(publisherPreview(t, repo, capture.UUID, ""), "unlink"))
	require.NoError(t, err)
	require.Nil(t, unlinked.AccountUUID)
	replay, err := applyPublisher(repo, original)
	require.NoError(t, err)
	require.Equal(t, linked, replay, "old request replay does not reapply it over an explicit unlink")
	preview := publisherPreview(t, repo, capture.UUID, "")
	require.Equal(t, "preserve", preview.Action)
	require.Equal(t, "unlinked", preview.Current.State)
	_, err = applyPublisher(repo, publisherInput(preview, "automatic"))
	require.ErrorIs(t, err, models.ErrCapturePublisherConflict)
	_, err = applyPublisher(repo, publisherInput(preview, "inherit"))
	require.NoError(t, err)
	inherited, err := applyPublisher(repo, publisherInput(publisherPreview(t, repo, capture.UUID, ""), "automatic"))
	require.NoError(t, err)
	require.Equal(t, 4, inherited.Revision)
	survivor := createSourceAccount(t, repo, "native:reddit")
	_, err = consolidateAccount(repo, consolidationInput(accountPreview(t, repo, *linked.AccountUUID, survivor.UUID)))
	require.NoError(t, err)
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		current, err := repo.CapturePublisher.Current(ctx, capture.UUID)
		require.NoError(t, err)
		require.Equal(t, *linked.AccountUUID, *current.AccountUUID)
		require.Equal(t, survivor.UUID, *current.CanonicalAccountUUID)
		history, err := repo.CapturePublisher.History(ctx, capture.UUID, 0, 2)
		require.NoError(t, err)
		require.Len(t, history, 2)
		next, err := repo.CapturePublisher.History(ctx, capture.UUID, history[1].Revision, 2)
		require.NoError(t, err)
		require.Len(t, next, 2)
		accounts, err := repo.CapturePublisher.PostAccounts(ctx, capture.PostUUID, "", 1)
		require.NoError(t, err)
		require.Len(t, accounts, 1)
		require.Equal(t, survivor.UUID, accounts[0].UUID)
		end, err := repo.CapturePublisher.PostAccounts(ctx, capture.PostUUID, accounts[0].UUID, 1)
		require.NoError(t, err)
		require.Empty(t, end)
		return nil
	}))
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	for _, query := range []string{"UPDATE capture_publisher_decisions SET reason='rewrite'", "UPDATE capture_publisher_claims SET evidence_key='rewrite'", "DELETE FROM capture_publisher_heads"} {
		_, err := raw.Exec(query)
		require.Error(t, err, query)
	}
	_, err = raw.Exec("UPDATE capture_publisher_heads SET decision_uuid=? WHERE capture_uuid=?", linked.UUID, capture.UUID)
	require.ErrorContains(t, err, "backwards")
	_, err = raw.Exec("UPDATE source_posts SET state='forgotten' WHERE uuid=?", capture.PostUUID)
	require.NoError(t, err)
	_, err = applyPublisher(repo, original)
	require.NoError(t, err, "successful request replay survives source retirement")
	_, err = applyPublisher(repo, publisherInput(publisherPreview(t, repo, capture.UUID, ""), "unlink"))
	require.ErrorIs(t, err, models.ErrCapturePublisherConflict)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestCapturePublisherMissingMalformedAndQualifiedServices(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	for _, tc := range []struct{ ns, platform, raw string }{
		{"native:bluesky", "bluesky", `{"category":"bluesky","author":{"did":"did:plc:author","handle":"example.test"}}`},
		{"native:instagram", "instagram", `{"category":"instagram","owner_id":"123","username":"Example"}`},
		{"native:onlyfans", "onlyfans", `{"category":"onlyfans","author":{"id":"123","username":"Example"}}`},
		{"native:patreon", "patreon", `{"category":"patreon","creator":{"id":"123","name":"Display"}}`},
		{"native:fansly", "fansly", `{"category":"fansly","author":{"id":"123","username":"Example"}}`},
		{"mirror:coomer:onlyfans", "coomer", `{"category":"coomer","service":"onlyfans","user":"123","username":"Display"}`},
		{"mirror:kemono:patreon", "kemono", `{"category":"kemono","service":"patreon","user":"123","username":"Display"}`},
		{"native:unfamiliar", "unfamiliar", `{"category":"unfamiliar","author":{"id":"123","name":"Display"}}`},
	} {
		capture := publisherCapture(t, repo, tc.ns, tc.platform, tc.raw)
		preview := publisherPreview(t, repo, capture.UUID, "")
		require.Equal(t, "create", preview.Action, tc.ns)
		decision, err := applyPublisher(repo, publisherInput(preview, "automatic"))
		require.NoError(t, err)
		require.Equal(t, tc.ns, findSourceAccount(t, repo, *decision.AccountUUID).Namespace)
	}
	for _, body := range []string{`{"category":"reddit","author":"name-only"}`, `{"category":"reddit","author_fullname":true,"author":"malformed"}`} {
		capture := publisherCapture(t, repo, "native:reddit", "reddit", body)
		preview := publisherPreview(t, repo, capture.UUID, "")
		require.Equal(t, "unavailable", preview.Action)
		_, err := applyPublisher(repo, publisherInput(preview, "automatic"))
		require.ErrorIs(t, err, models.ErrCapturePublisherConflict)
		chosen := createSourceAccount(t, repo, "native:reddit")
		decision, err := applyPublisher(repo, publisherInput(publisherPreview(t, repo, capture.UUID, chosen.UUID), "link"))
		require.NoError(t, err)
		require.Equal(t, "explicit-publisher-v1", decision.Policy)
	}
}

func TestCapturePublisherLateFailureStartupAndAnonymisation(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	capture := publisherCapture(t, repo, "native:twitter", "twitter", `{"category":"twitter","author":{"id":"account","name":"Private publisher"}}`)
	input := publisherInput(publisherPreview(t, repo, capture.UUID, ""), "automatic")
	attachmentSQL(t, db, `CREATE TRIGGER reject_publisher_claim BEFORE INSERT ON capture_publisher_claims BEGIN SELECT RAISE(ABORT,'late publisher failure'); END;`)
	err := repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.CapturePublisher.Apply(ctx, input)
		require.ErrorContains(t, err, "late publisher failure")
		return nil
	})
	require.ErrorContains(t, err, "unfinished capture publisher write")
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	for _, table := range []string{"source_accounts", "source_account_identifiers", "capture_publisher_decisions", "capture_publisher_heads", "capture_publisher_write_context"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
	attachmentSQL(t, db, "DROP TRIGGER reject_publisher_claim")
	_, err = applyPublisher(repo, input)
	require.NoError(t, err)
	_, err = raw.Exec("INSERT INTO capture_publisher_write_context(request_uuid,capture_uuid) VALUES(?,?)", uuid.NewString(), capture.UUID)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	require.ErrorContains(t, db.Open(db.DatabasePath()), "unfinished capture publisher write")
	_, err = raw.Exec("DELETE FROM capture_publisher_write_context")
	require.NoError(t, err)
	require.NoError(t, db.Open(db.DatabasePath()))
	out := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anon, err := sqlite.NewAnonymiser(db, out)
	require.NoError(t, err)
	require.NoError(t, anon.Anonymise(context.Background()))
	anonymous := openRawDB(t, out)
	defer anonymous.Close()
	for _, table := range []string{"capture_publisher_decisions", "capture_publisher_heads", "capture_publisher_claims", "capture_publisher_write_context", "source_accounts"} {
		require.Zero(t, queryUint(t, anonymous, "SELECT count(*) FROM "+table))
	}
	require.Zero(t, queryUint(t, anonymous, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestCapturePublisherCandidatesRemainBounded(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		for i := 0; i < 102; i++ {
			account, err := repo.SourceAccount.Create(ctx, "native:twitter", fmt.Sprintf("Candidate %d", i))
			require.NoError(t, err)
			_, err = repo.SourceAccount.ObserveIdentifier(ctx, account.UUID, models.AccountReference{Namespace: account.Namespace, Kind: "handle", Value: "shared"}, accountEvidence())
			require.NoError(t, err)
		}
		return nil
	}))
	capture := publisherCapture(t, repo, "native:twitter", "twitter", `{"category":"twitter","author":{"id":"unseen","name":"shared"}}`)
	preview := publisherPreview(t, repo, capture.UUID, "")
	require.Equal(t, "review", preview.Action)
	require.Len(t, preview.Candidates, 100)
	require.True(t, preview.CandidatesTruncated)
	_, err := applyPublisher(repo, publisherInput(preview, "automatic"))
	require.ErrorIs(t, err, models.ErrCapturePublisherConflict)
	_, err = applyPublisher(repo, publisherInput(preview, "create"))
	require.NoError(t, err, "an explicit new-account choice can distinguish a reused handle")
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	rows, err := raw.Query(`EXPLAIN QUERY PLAN SELECT DISTINCT r.* FROM source_captures c INDEXED BY source_captures_scope
CROSS JOIN capture_publisher_heads h ON h.capture_uuid=c.uuid
CROSS JOIN capture_publisher_decisions d ON d.uuid=h.decision_uuid AND d.state='linked'
CROSS JOIN source_accounts a ON a.uuid=d.account_uuid CROSS JOIN source_accounts r ON r.uuid=a.canonical_uuid
WHERE c.post_uuid=? AND r.uuid>? ORDER BY r.uuid LIMIT 100`, capture.PostUUID, "")
	require.NoError(t, err)
	defer rows.Close()
	var plan string
	for rows.Next() {
		var a, b, c int
		var detail string
		require.NoError(t, rows.Scan(&a, &b, &c, &detail))
		plan += detail + "\n"
	}
	require.NoError(t, rows.Err())
	require.Contains(t, plan, "source_captures_scope")
	require.NotContains(t, plan, "SCAN d")
	require.NotContains(t, plan, "SCAN c")
	require.NotContains(t, plan, "SCAN source_accounts")
	require.True(t, strings.Contains(plan, "SEARCH"))
}

func TestCapturePublisherMigrationPreservesPopulatedSourceEvidence(t *testing.T) {
	fixture, repo := archiveTestDatabase(t)
	account := createSourceAccount(t, repo, "native:twitter")
	observeAccount(t, repo, account.UUID, models.AccountReference{Namespace: account.Namespace, Kind: "id", Value: "author"}, accountEvidence())
	capture := publisherCapture(t, repo, "native:twitter", "twitter", `{"category":"twitter","author":{"id":"author","name":"Example","description":"Retained profile"},"content":"Retained caption"}`)
	root := putMediaRoot(t, repo, models.MediaRootInput{Origin: "migration", MediaRootDefinition: models.MediaRootDefinition{Label: "Original root", State: "active"}})
	collection := putSourceCollection(t, repo, models.SourceCollectionInput{Origin: "migration", SourceCollectionDefinition: models.SourceCollectionDefinition{
		Label: "Original source", Kind: "account", Namespace: account.Namespace, State: "active", AccountUUID: &account.UUID, RootUUID: &root.UUID, PathPrefix: "Original"}})
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		return repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{CollectionUUID: collection.UUID, CollectionRevision: 1, CaptureUUID: capture.UUID})
	}))
	path := filepath.Join(t.TempDir(), "before-publishers.sqlite")
	buildLegacyDatabase(t, path, 86, true)
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	m, err := sqlite.NewMigrator(db)
	require.NoError(t, err)
	for version := m.CurrentSchemaVersion(); version < sqlite.NativeSchemaBaseline+15; version = m.CurrentSchemaVersion() {
		require.NoError(t, m.RunMigration(context.Background(), m.GetNextMigrationVersion(version)))
	}
	m.Close()
	raw := openRawDB(t, path)
	defer raw.Close()
	// Copy valid signed capture DATA into the actual historical schema. No
	// schema object from the current fixture is copied or relabelled backwards.
	raw.SetMaxOpenConns(1)
	_, err = raw.Exec("ATTACH DATABASE ? AS source_fixture", fixture.DatabasePath())
	require.NoError(t, err)
	tx, err := raw.Begin()
	require.NoError(t, err)
	_, err = tx.Exec(`INSERT INTO source_accounts(uuid,namespace,label,revision,created_at)
SELECT uuid,namespace,label,revision,created_at FROM source_fixture.source_accounts`)
	require.NoError(t, err)
	for _, table := range []string{"source_account_identifiers", "source_account_identifier_evidence", "media_roots", "media_root_revisions", "source_collections", "source_collection_revisions",
		"source_posts", "source_post_identifiers", "source_payloads", "source_profile_bodies", "source_post_revisions", "source_captures", "source_capture_profiles", "source_collection_captures"} {
		_, err := tx.Exec("INSERT INTO " + table + " SELECT * FROM source_fixture." + table)
		require.NoError(t, err, table)
	}
	require.NoError(t, tx.Commit())
	_, err = raw.Exec("DETACH DATABASE source_fixture")
	require.NoError(t, err)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='capture_publisher_decisions'"))
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	defer db.Close()
	upgraded := db.Repository()
	require.NoError(t, upgraded.WithReadTxn(context.Background(), func(ctx context.Context) error {
		retained, err := upgraded.SourceEvidence.FindCapture(ctx, capture.UUID)
		require.NoError(t, err)
		require.Equal(t, capture, retained)
		retainedRoot, err := upgraded.MediaRoot.Find(ctx, root.UUID)
		require.NoError(t, err)
		require.Equal(t, root, retainedRoot)
		retainedCollection, err := upgraded.SourceCollection.Find(ctx, collection.UUID)
		require.NoError(t, err)
		require.Equal(t, collection, retainedCollection)
		refs, err := upgraded.SourceCollection.Captures(ctx, collection.UUID, nil, 100)
		require.NoError(t, err)
		require.Len(t, refs, 1)
		require.Equal(t, capture.UUID, refs[0].CaptureUUID)
		current, err := upgraded.CapturePublisher.Current(ctx, capture.UUID)
		require.NoError(t, err)
		require.Nil(t, current, "migration must not invent publisher choices")
		return nil
	}))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM capture_publisher_decisions"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	preview := publisherPreview(t, upgraded, capture.UUID, "")
	require.Equal(t, "link", preview.Action)
	require.Equal(t, account.UUID, *preview.AccountUUID)
	_, err = applyPublisher(upgraded, publisherInput(preview, "automatic"))
	require.NoError(t, err)
}
