package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func accountPreview(t *testing.T, repo models.Repository, source, destination string) *models.AccountConsolidationPreview {
	t.Helper()
	var ret *models.AccountConsolidationPreview
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceAccount.PreviewConsolidation(ctx, source, destination)
		return err
	}))
	return ret
}
func consolidateAccount(repo models.Repository, input models.AccountConsolidationInput) (*models.AccountConsolidation, error) {
	var ret *models.AccountConsolidation
	err := repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceAccount.Consolidate(ctx, input)
		return err
	})
	return ret, err
}
func consolidationInput(preview *models.AccountConsolidationPreview) models.AccountConsolidationInput {
	return models.AccountConsolidationInput{UUID: uuid.NewString(), SourceUUID: preview.Source.UUID, DestinationUUID: preview.Destination.UUID,
		Signature: preview.Signature, OwnershipMode: "preserve", Origin: "review", Reason: "Reviewed captured account identifiers"}
}
func decideAccountOwner(t *testing.T, repo models.Repository, account string, state models.AccountOwnershipState, performer *models.ArchiveEntity) {
	t.Helper()
	current := findSourceAccount(t, repo, account)
	input := models.AccountOwnershipInput{AccountUUID: account, ExpectedAccountRevision: current.Revision, State: state, Origin: "review"}
	if performer != nil {
		input.PerformerUUID = performer.UUID
		input.ExpectedPerformerRevision = performer.Revision
	}
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error { _, err := repo.SourceAccount.DecideOwnership(ctx, input); return err }))
}

func TestAccountConsolidationRetainsEvidenceOwnershipAndReplay(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	from, to := createSourceAccount(t, repo, "native:reddit"), createSourceAccount(t, repo, "native:reddit")
	ref := models.AccountReference{Namespace: "native:reddit", Kind: "handle", Value: "ExampleAuthor"}
	identifier := observeAccount(t, repo, from.UUID, ref, accountEvidence())
	observeAccount(t, repo, to.UUID, models.AccountReference{Namespace: "native:reddit", Kind: "id", Value: "t2_example"}, accountEvidence())
	p := archiveFind(t, repo, models.ArchivePerformer, 72)
	decideAccountOwner(t, repo, from.UUID, models.AccountOwnershipLinked, p)
	preview := accountPreview(t, repo, from.UUID, to.UUID)
	require.Len(t, preview.Members, 2)
	require.Len(t, preview.Identifiers, 2)
	require.Equal(t, p.UUID, preview.DefaultOwnership.PerformerUUID)
	input := consolidationInput(preview)
	event, err := consolidateAccount(repo, input)
	require.NoError(t, err)
	require.Equal(t, to.UUID, *findSourceAccount(t, repo, from.UUID).RedirectTo)
	require.Equal(t, to.UUID, findSourceAccount(t, repo, from.UUID).CanonicalUUID)
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		resolved, err := repo.SourceAccount.Resolve(ctx, from.UUID)
		require.NoError(t, err)
		require.Equal(t, to.UUID, resolved.UUID)
		matches, err := repo.SourceAccount.Lookup(ctx, ref, "", 10)
		require.NoError(t, err)
		require.Len(t, matches, 1)
		require.Equal(t, to.UUID, matches[0].UUID)
		ids, err := repo.SourceAccount.Identifiers(ctx, from.UUID, "", 10)
		require.NoError(t, err)
		require.Len(t, ids, 2)
		for _, id := range ids {
			require.Equal(t, to.UUID, id.CanonicalAccountUUID)
			if id.UUID == identifier.UUID {
				require.Equal(t, from.UUID, id.AccountUUID)
			}
		}
		choice, err := repo.SourceAccount.Ownership(ctx, from.UUID)
		require.NoError(t, err)
		require.Equal(t, to.UUID, choice.AccountUUID)
		require.Equal(t, p.UUID, *choice.PerformerUUID)
		history, err := repo.SourceAccount.OwnershipHistory(ctx, from.UUID, 0, 10)
		require.NoError(t, err)
		require.Len(t, history, 1)
		require.Equal(t, from.UUID, history[0].AccountUUID)
		changes, err := repo.SourceAccount.ConsolidationHistory(ctx, to.UUID, 0, 10)
		require.NoError(t, err)
		require.Equal(t, []models.AccountConsolidation{*event}, changes)
		return nil
	}))
	before := findSourceAccount(t, repo, to.UUID)
	e := accountEvidence()
	e.Key = "another-capture"
	observed := observeAccount(t, repo, from.UUID, ref, e)
	require.Equal(t, identifier.UUID, observed.UUID)
	require.Equal(t, to.UUID, observed.CanonicalAccountUUID)
	require.Equal(t, before.Revision+1, findSourceAccount(t, repo, to.UUID).Revision)
	replay, err := consolidateAccount(repo, input)
	require.NoError(t, err)
	require.Equal(t, event, replay)
	input.Reason = "Changed input"
	_, err = consolidateAccount(repo, input)
	require.ErrorIs(t, err, models.ErrAccountConsolidationReplay)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM source_account_consolidations"))
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM performers_scenes WHERE scene_id=31 AND performer_id=71"), "account ownership must not replace depicted performers")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestAccountConsolidationConflictsRequireExplicitResolution(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	from, to := createSourceAccount(t, repo, "native:instagram"), createSourceAccount(t, repo, "native:instagram")
	for i, account := range []*models.SourceAccount{from, to} {
		observeAccount(t, repo, account.UUID, models.AccountReference{Namespace: account.Namespace, Kind: "id", Value: []string{"first-id", "other-id"}[i]}, accountEvidence())
	}
	p := archiveFind(t, repo, models.ArchivePerformer, 71)
	decideAccountOwner(t, repo, from.UUID, models.AccountOwnershipLinked, p)
	decideAccountOwner(t, repo, to.UUID, models.AccountOwnershipUnlinked, nil)
	preview := accountPreview(t, repo, from.UUID, to.UUID)
	require.Nil(t, preview.DefaultOwnership)
	require.Len(t, preview.IdentifierConflicts, 1)
	input := consolidationInput(preview)
	_, err := consolidateAccount(repo, input)
	require.ErrorIs(t, err, models.ErrAccountIdentifierResolution)
	input.AcceptIdentifierConflicts = true
	_, err = consolidateAccount(repo, input)
	require.ErrorIs(t, err, models.ErrAccountOwnershipResolution)
	require.Equal(t, preview.Source, findSourceAccount(t, repo, from.UUID))
	require.Equal(t, preview.Destination, findSourceAccount(t, repo, to.UUID))
	input.OwnershipMode = "choose"
	input.Ownership = models.AccountOwnershipSelection{State: models.AccountOwnershipUnlinked}
	event, err := consolidateAccount(repo, input)
	require.NoError(t, err)
	require.True(t, event.AcceptedIdentifierConflicts)
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		choice, err := repo.SourceAccount.Ownership(ctx, from.UUID)
		require.NoError(t, err)
		require.Equal(t, models.AccountOwnershipUnlinked, choice.State)
		history, err := repo.SourceAccount.OwnershipHistory(ctx, from.UUID, 0, 10)
		require.NoError(t, err)
		require.Equal(t, models.AccountOwnershipLinked, history[0].State)
		return nil
	}))
	// Native and mirror namespaces never collapse based on identical spelling.
	mirror := createSourceAccount(t, repo, "mirror:coomer:instagram")
	require.Error(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.SourceAccount.PreviewConsolidation(ctx, to.UUID, mirror.UUID)
		return err
	}))
}

func TestAccountConsolidationStaleReviewAndNestedCanonicalLookup(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	a, b, c := createSourceAccount(t, repo, "native:twitter"), createSourceAccount(t, repo, "native:twitter"), createSourceAccount(t, repo, "native:twitter")
	ref := models.AccountReference{Namespace: a.Namespace, Kind: "handle", Value: "SameHandle"}
	for _, account := range []*models.SourceAccount{a, b, c} {
		observeAccount(t, repo, account.UUID, ref, accountEvidence())
	}
	preview := accountPreview(t, repo, a.UUID, b.UUID)
	input := consolidationInput(preview)
	observeAccount(t, repo, a.UUID, models.AccountReference{Namespace: a.Namespace, Kind: "id", Value: "123"}, accountEvidence())
	_, err := consolidateAccount(repo, input)
	require.ErrorIs(t, err, models.ErrSourceAccountConflict)
	p := archiveFind(t, repo, models.ArchivePerformer, 71)
	decideAccountOwner(t, repo, a.UUID, models.AccountOwnershipLinked, p)
	input = consolidationInput(accountPreview(t, repo, a.UUID, b.UUID))
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error { return repo.Performer.Merge(ctx, []int{71}, 72) }))
	_, err = consolidateAccount(repo, input)
	require.ErrorIs(t, err, models.ErrSourceAccountConflict)
	first, err := consolidateAccount(repo, consolidationInput(accountPreview(t, repo, a.UUID, b.UUID)))
	require.NoError(t, err)
	second, err := consolidateAccount(repo, consolidationInput(accountPreview(t, repo, b.UUID, c.UUID)))
	require.NoError(t, err)
	require.NotEqual(t, first.UUID, second.UUID)
	for _, account := range []*models.SourceAccount{a, b, c} {
		require.Equal(t, c.UUID, findSourceAccount(t, repo, account.UUID).CanonicalUUID)
	}
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		matches, err := repo.SourceAccount.Lookup(ctx, ref, "", 1)
		require.NoError(t, err)
		require.Len(t, matches, 1)
		require.Equal(t, c.UUID, matches[0].UUID)
		next, err := repo.SourceAccount.Lookup(ctx, ref, c.UUID, 1)
		require.NoError(t, err)
		require.Empty(t, next)
		ids, err := repo.SourceAccount.Identifiers(ctx, a.UUID, "", 10)
		require.NoError(t, err)
		require.Len(t, ids, 4)
		for _, id := range ids {
			require.Equal(t, c.UUID, id.CanonicalAccountUUID)
		}
		return nil
	}))
	err = repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.SourceAccount.DecideOwnership(ctx, models.AccountOwnershipInput{AccountUUID: a.UUID, ExpectedAccountRevision: findSourceAccount(t, repo, a.UUID).Revision, State: models.AccountOwnershipUndecided, Origin: "review"})
		return err
	})
	require.ErrorIs(t, err, models.ErrSourceAccountConflict)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	rows, err := raw.Query(`EXPLAIN QUERY PLAN SELECT DISTINCT a.* FROM source_account_identifiers i JOIN source_accounts a ON a.uuid=i.canonical_uuid
WHERE i.namespace='native:twitter' AND i.kind='handle' AND i.value='samehandle' AND i.canonical_uuid>'' ORDER BY i.canonical_uuid LIMIT 10`)
	require.NoError(t, err)
	defer rows.Close()
	var indexed bool
	for rows.Next() {
		var a, b, c int
		var plan string
		require.NoError(t, rows.Scan(&a, &b, &c, &plan))
		if plan == "SEARCH i USING COVERING INDEX source_account_identifiers_canonical (namespace=? AND kind=? AND value=? AND canonical_uuid>?)" {
			indexed = true
		}
		require.NotContains(t, plan, "SCAN source_accounts")
	}
	require.NoError(t, rows.Err())
	require.True(t, indexed)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestAccountConsolidationTikTokSecondaryIdentifiersRequireReview(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	a, b := createSourceAccount(t, repo, "native:tiktok"), createSourceAccount(t, repo, "native:tiktok")
	for i, account := range []*models.SourceAccount{a, b} {
		observeAccount(t, repo, account.UUID, models.AccountReference{Namespace: account.Namespace, Kind: "secUid", Value: []string{"Opaque-A", "Opaque-B"}[i]}, accountEvidence())
	}
	preview := accountPreview(t, repo, a.UUID, b.UUID)
	require.Equal(t, []models.AccountIdentifierConflict{{Namespace: "native:tiktok", Kind: "secUid", Values: []string{"Opaque-A", "Opaque-B"}}}, preview.IdentifierConflicts)
	input := consolidationInput(preview)
	_, err := consolidateAccount(repo, input)
	require.ErrorIs(t, err, models.ErrAccountIdentifierResolution)
	require.Equal(t, preview.Source, findSourceAccount(t, repo, a.UUID))
	require.Equal(t, preview.Destination, findSourceAccount(t, repo, b.UUID))
	input.AcceptIdentifierConflicts = true
	_, err = consolidateAccount(repo, input)
	require.NoError(t, err)
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		ids, err := repo.SourceAccount.Identifiers(ctx, b.UUID, "", 10)
		require.NoError(t, err)
		require.Len(t, ids, 2, "acknowledgement preserves both pieces of evidence")
		return nil
	}))
}

func TestAccountConsolidationFailureGuardsAndAnonymisation(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	from, to := createSourceAccount(t, repo, "native:bluesky"), createSourceAccount(t, repo, "native:bluesky")
	input := consolidationInput(accountPreview(t, repo, from.UUID, to.UUID))
	input.Reason = "private-account-consolidation-reason"
	attachmentSQL(t, db, `CREATE TRIGGER reject_account_consolidation BEFORE INSERT ON source_account_consolidations BEGIN SELECT RAISE(ABORT,'late consolidation failure'); END;`)
	err := repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.SourceAccount.Consolidate(ctx, input)
		require.ErrorContains(t, err, "late consolidation failure")
		return nil
	})
	require.ErrorContains(t, err, "unfinished source account consolidation")
	require.Equal(t, from, findSourceAccount(t, repo, from.UUID))
	require.Equal(t, to, findSourceAccount(t, repo, to.UUID))
	attachmentSQL(t, db, "DROP TRIGGER reject_account_consolidation")
	event, err := consolidateAccount(repo, input)
	require.NoError(t, err)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	for _, query := range []string{
		"UPDATE source_accounts SET canonical_uuid=uuid WHERE uuid=?",
		"UPDATE source_accounts SET namespace='native:other' WHERE uuid=?",
	} {
		_, err := raw.Exec(query, from.UUID)
		require.Error(t, err)
	}
	_, err = raw.Exec("UPDATE source_account_consolidations SET reason='rewritten' WHERE uuid=?", event.UUID)
	require.Error(t, err)
	fresh := createSourceAccount(t, repo, "native:bluesky")
	_, err = raw.Exec("INSERT INTO source_account_consolidation_context(source_uuid,destination_uuid) VALUES(?,?)", fresh.UUID, to.UUID)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	require.ErrorContains(t, db.Open(db.DatabasePath()), "unfinished source account consolidation")
	_, err = raw.Exec("DELETE FROM source_account_consolidation_context")
	require.NoError(t, err)
	require.NoError(t, db.Open(db.DatabasePath()))
	out := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymiser, err := sqlite.NewAnonymiser(db, out)
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(context.Background()))
	anonymous := openRawDB(t, out)
	defer anonymous.Close()
	require.Zero(t, queryUint(t, anonymous, "SELECT count(*) FROM source_account_consolidations"))
	require.Zero(t, queryUint(t, anonymous, "SELECT count(*) FROM source_accounts"))
	require.Zero(t, queryUint(t, anonymous, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestAccountConsolidationMigrationPreservesExistingDecisions(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "before-account-consolidation.sqlite")
	buildLegacyDatabase(t, path, 86, true)
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	m, err := sqlite.NewMigrator(db)
	require.NoError(t, err)
	for version := m.CurrentSchemaVersion(); version < sqlite.NativeSchemaBaseline+13; version = m.CurrentSchemaVersion() {
		require.NoError(t, m.RunMigration(context.Background(), m.GetNextMigrationVersion(version)))
	}
	m.Close()
	raw := openRawDB(t, path)
	defer raw.Close()
	account, identifier, decision := uuid.NewString(), uuid.NewString(), uuid.NewString()
	_, err = raw.Exec("INSERT INTO source_accounts(uuid,namespace,label,revision) VALUES(?,'native:reddit','Original account',3)", account)
	require.NoError(t, err)
	_, err = raw.Exec("INSERT INTO source_account_identifiers(uuid,account_uuid,namespace,kind,value) VALUES(?,?,'native:reddit','handle','original')", identifier, account)
	require.NoError(t, err)
	_, err = raw.Exec("INSERT INTO account_performer_decisions(uuid,account_uuid,revision,state,origin) VALUES(?,?,3,'unlinked','review')", decision, account)
	require.NoError(t, err)
	_, err = raw.Exec("INSERT INTO account_performer_links(account_uuid,decision_uuid) VALUES(?,?)", account, decision)
	require.NoError(t, err)
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	defer db.Close()
	repo := db.Repository()
	current := findSourceAccount(t, repo, account)
	require.Equal(t, 3, current.Revision)
	require.Equal(t, account, current.CanonicalUUID)
	require.Nil(t, current.RedirectTo)
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		ids, err := repo.SourceAccount.Identifiers(ctx, account, "", 10)
		require.NoError(t, err)
		require.Len(t, ids, 1)
		require.Equal(t, identifier, ids[0].UUID)
		require.Equal(t, account, ids[0].CanonicalAccountUUID)
		choice, err := repo.SourceAccount.Ownership(ctx, account)
		require.NoError(t, err)
		require.Equal(t, decision, choice.UUID)
		require.Equal(t, models.AccountOwnershipUnlinked, choice.State)
		return nil
	}))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_account_consolidations"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestAccountConsolidationQualifiedServicesAndBoundedReview(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	for _, namespace := range []string{"native:reddit", "native:twitter", "native:instagram", "native:bluesky", "native:tiktok", "native:patreon", "native:onlyfans", "native:fansly", "mirror:coomer:onlyfans", "mirror:kemono:patreon", "native:unfamiliar-extractor"} {
		t.Run(namespace, func(t *testing.T) {
			a, b := createSourceAccount(t, repo, namespace), createSourceAccount(t, repo, namespace)
			handle := models.AccountReference{Namespace: namespace, Kind: "handle", Value: "CapturedName"}
			observeAccount(t, repo, a.UUID, handle, accountEvidence())
			kind := "id"
			if namespace == "mirror:coomer:onlyfans" || namespace == "mirror:kemono:patreon" {
				kind = "user"
			}
			observeAccount(t, repo, b.UUID, models.AccountReference{Namespace: namespace, Kind: kind, Value: "Opaque-ID"}, accountEvidence())
			preview := accountPreview(t, repo, a.UUID, b.UUID)
			require.Empty(t, preview.IdentifierConflicts)
			_, err := consolidateAccount(repo, consolidationInput(preview))
			require.NoError(t, err)
			require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
				matches, err := repo.SourceAccount.Lookup(ctx, handle, "", 10)
				require.NoError(t, err)
				require.Len(t, matches, 1)
				require.Equal(t, b.UUID, matches[0].UUID)
				return nil
			}))
		})
	}
	a, b := createSourceAccount(t, repo, "native:many"), createSourceAccount(t, repo, "native:many")
	attachmentSQL(t, db, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<8193)
INSERT INTO source_account_identifiers(uuid,account_uuid,canonical_uuid,namespace,kind,value)
SELECT printf('00000000-0000-4000-8000-%012d',i),?,?,'native:many','handle','handle-'||i FROM n`, a.UUID, a.UUID)
	err := repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.SourceAccount.PreviewConsolidation(ctx, a.UUID, b.UUID)
		return err
	})
	require.ErrorContains(t, err, "8192 identifiers")
	require.Equal(t, a, findSourceAccount(t, repo, a.UUID))
	require.Equal(t, b, findSourceAccount(t, repo, b.UUID))
}
