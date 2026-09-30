package sqlite_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func createSourceAccount(t *testing.T, repo models.Repository, namespace string) *models.SourceAccount {
	t.Helper()
	var ret *models.SourceAccount
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceAccount.Create(ctx, namespace, "Account display name")
		return err
	}))
	return ret
}

func findSourceAccount(t *testing.T, repo models.Repository, id string) *models.SourceAccount {
	t.Helper()
	var ret *models.SourceAccount
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceAccount.Find(ctx, id)
		return err
	}))
	require.NotNil(t, ret)
	return ret
}

func accountEvidence() models.AccountIdentifierEvidence {
	when := time.Date(2026, 7, 1, 12, 0, 1, 123456789, time.FixedZone("capture", -7*3600))
	return models.AccountIdentifierEvidence{Key: "capture:synthetic-author", Basis: "captured-author", Origin: "gallery-dl",
		Details: json.RawMessage(`{"id":98765432109876543210,"role":"author"}`), FirstObserved: when, LastObserved: when}
}

func observeAccount(t *testing.T, repo models.Repository, id string, ref models.AccountReference, e models.AccountIdentifierEvidence) *models.AccountIdentifier {
	t.Helper()
	var ret *models.AccountIdentifier
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceAccount.ObserveIdentifier(ctx, id, ref, e)
		return err
	}))
	return ret
}

func TestSourceAccountMigrationPreservesExistingIdentities(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "before-accounts.sqlite")
	buildLegacyDatabase(t, path, 86, true)
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	m, err := sqlite.NewMigrator(db)
	require.NoError(t, err)
	for version := m.CurrentSchemaVersion(); version < sqlite.NativeSchemaBaseline+4; version = m.CurrentSchemaVersion() {
		require.NoError(t, m.RunMigration(context.Background(), m.GetNextMigrationVersion(version)))
	}
	m.Close()
	raw := openRawDB(t, path)
	_, err = raw.Exec(archiveIdentityFixture)
	require.NoError(t, err)
	var before string
	require.NoError(t, raw.QueryRow("SELECT uuid FROM archive_entities WHERE performer_id=71").Scan(&before))
	require.NoError(t, raw.Close())
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	defer db.Close()
	require.Equal(t, before, archiveFind(t, db.Repository(), models.ArchivePerformer, 71).UUID)
	raw = openRawDB(t, path)
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_accounts"), "migration must not infer scrape accounts from library performers")
	require.Equal(t, uint(6), queryUint(t, raw, "SELECT count(*) FROM archive_entities"))
}

func TestSourceAccountIdentifiersPreserveAmbiguityAndQualifiedScopes(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	one := createSourceAccount(t, repo, "native:instagram")
	two := createSourceAccount(t, repo, "native:instagram")
	ref := models.AccountReference{Namespace: "native:instagram", Kind: "handle", Value: "Reused.Handle"}
	for _, acct := range []*models.SourceAccount{one, two} {
		observeAccount(t, repo, acct.UUID, ref, accountEvidence())
		observeAccount(t, repo, acct.UUID, models.AccountReference{Namespace: "native:instagram", Kind: "id", Value: acct.UUID}, accountEvidence())
	}
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		accounts, err := repo.SourceAccount.Lookup(ctx, ref, "", 1)
		require.NoError(t, err)
		require.Len(t, accounts, 1)
		next, err := repo.SourceAccount.Lookup(ctx, ref, accounts[0].UUID, 1)
		require.NoError(t, err)
		require.Len(t, next, 1)
		require.NotEqual(t, accounts[0].UUID, next[0].UUID)
		all, err := repo.SourceAccount.Lookup(ctx, ref, "", 100)
		require.NoError(t, err)
		require.Len(t, all, 2, "a reused handle must not silently unify source accounts")
		for _, acct := range all {
			decision, err := repo.SourceAccount.Ownership(ctx, acct.UUID)
			require.NoError(t, err)
			require.Nil(t, decision, "publisher accounts do not infer depicted performers or owners")
		}
		return nil
	}))
	native := createSourceAccount(t, repo, "native:onlyfans")
	mirror := createSourceAccount(t, repo, "mirror:coomer:onlyfans")
	for _, item := range []struct {
		account *models.SourceAccount
		kind    string
	}{{native, "id"}, {mirror, "user"}} {
		ref := models.AccountReference{Namespace: item.account.Namespace, Kind: item.kind, Value: "ExactOpaqueID"}
		observeAccount(t, repo, item.account.UUID, ref, accountEvidence())
		require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
			matches, err := repo.SourceAccount.Lookup(ctx, ref, "", 10)
			require.NoError(t, err)
			require.Len(t, matches, 1)
			require.Equal(t, item.account.UUID, matches[0].UUID)
			ref.Value = "exactopaqueid"
			matches, err = repo.SourceAccount.Lookup(ctx, ref, "", 10)
			require.NoError(t, err)
			require.Empty(t, matches, "opaque IDs retain case")
			return nil
		}))
	}
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Equal(t, uint(2), queryUint(t, raw, "SELECT count(*) FROM performers"))
	rows, err := raw.Query(`EXPLAIN QUERY PLAN SELECT a.* FROM source_account_identifiers i
JOIN source_accounts a ON a.uuid=i.account_uuid WHERE i.namespace='native:instagram' AND i.kind='handle' AND i.value='reused.handle' AND i.account_uuid>'' ORDER BY i.account_uuid LIMIT 10`)
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
	require.Contains(t, plans, "SEARCH i USING COVERING INDEX source_account_identifiers_lookup (namespace=? AND kind=? AND value=? AND account_uuid>?)")
}

func TestSourceAccountEvidenceReplayConflictAndTimestampPrecision(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	account := createSourceAccount(t, repo, "native:reddit")
	ref := models.AccountReference{Namespace: "native:reddit", Kind: "id", Value: "t2_account"}
	evidence := accountEvidence()
	identifier := observeAccount(t, repo, account.UUID, ref, evidence)
	revision := findSourceAccount(t, repo, account.UUID).Revision
	require.Equal(t, account.Revision+1, revision)
	// A later delivery of identical evidence extends its interval without
	// changing review semantics or losing subsecond capture precision.
	evidence.LastObserved = evidence.LastObserved.Add(9 * time.Nanosecond)
	evidence.Details = json.RawMessage(`{ "role": "author", "id": 98765432109876543210 }`)
	require.Equal(t, identifier.UUID, observeAccount(t, repo, account.UUID, ref, evidence).UUID)
	require.Equal(t, revision, findSourceAccount(t, repo, account.UUID).Revision)
	evidence.FirstObserved = evidence.FirstObserved.Add(-time.Hour)
	observeAccount(t, repo, account.UUID, ref, evidence)
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		rows, err := repo.SourceAccount.Evidence(ctx, identifier.UUID, "", 10)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.JSONEq(t, string(evidence.Details), string(rows[0].Details))
		require.Contains(t, string(rows[0].Details), "98765432109876543210")
		require.True(t, evidence.FirstObserved.Equal(rows[0].FirstObserved))
		require.True(t, evidence.LastObserved.Equal(rows[0].LastObserved))
		return nil
	}))
	evidence.Details = json.RawMessage(`{"id":"another-account"}`)
	err := repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.SourceAccount.ObserveIdentifier(ctx, account.UUID, ref, evidence)
		return err
	})
	require.ErrorIs(t, err, models.ErrAccountEvidenceConflict)
	require.Equal(t, revision, findSourceAccount(t, repo, account.UUID).Revision)
	path := db.DatabasePath()
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(path))
	require.Equal(t, revision, findSourceAccount(t, db.Repository(), account.UUID).Revision)
}

func TestSourceAccountOwnershipHistoryUnlinkAndStaleReview(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	account := createSourceAccount(t, repo, "native:twitter")
	performer := archiveFind(t, repo, models.ArchivePerformer, 71)
	input := models.AccountOwnershipInput{AccountUUID: account.UUID, ExpectedAccountRevision: account.Revision,
		State: models.AccountOwnershipLinked, PerformerUUID: performer.UUID, ExpectedPerformerRevision: performer.Revision, Origin: "review", Reason: "Verified profile URL"}
	apply := func() error {
		return repo.WithTxn(context.Background(), func(ctx context.Context) error {
			_, err := repo.SourceAccount.DecideOwnership(ctx, input)
			return err
		})
	}
	require.NoError(t, apply())
	require.ErrorIs(t, apply(), models.ErrSourceAccountConflict, "a repeated stale action cannot create another decision")
	input.ExpectedAccountRevision++
	input.ExpectedPerformerRevision--
	require.ErrorIs(t, apply(), models.ErrSourceAccountConflict)
	input.ExpectedPerformerRevision = 0
	input.PerformerUUID = ""
	input.State = models.AccountOwnershipUnlinked
	require.NoError(t, apply())
	observeAccount(t, repo, account.UUID, models.AccountReference{Namespace: "native:twitter", Kind: "id", Value: "90"}, accountEvidence())
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		choice, err := repo.SourceAccount.Ownership(ctx, account.UUID)
		require.NoError(t, err)
		require.Equal(t, models.AccountOwnershipUnlinked, choice.State, "new evidence cannot overwrite an explicit unlink")
		require.Nil(t, choice.PerformerUUID)
		return nil
	}))
	input.State = models.AccountOwnershipUndecided
	input.ExpectedAccountRevision = findSourceAccount(t, repo, account.UUID).Revision
	input.Origin = "profile-url"
	input.State = models.AccountOwnershipLinked
	input.PerformerUUID = performer.UUID
	input.ExpectedPerformerRevision = performer.Revision
	require.ErrorIs(t, apply(), models.ErrSourceAccountConflict, "a discovered profile must not reverse an explicit unlink")
	input.Origin = "review"
	input.State = models.AccountOwnershipUndecided
	input.PerformerUUID = ""
	input.ExpectedPerformerRevision = 0
	require.NoError(t, apply())
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		history, err := repo.SourceAccount.OwnershipHistory(ctx, account.UUID, 0, 100)
		require.NoError(t, err)
		require.Len(t, history, 3)
		require.Equal(t, performer.UUID, *history[0].PerformerUUID)
		require.Equal(t, models.AccountOwnershipUnlinked, history[1].State)
		require.Equal(t, models.AccountOwnershipUndecided, history[2].State)
		next, err := repo.SourceAccount.OwnershipHistory(ctx, account.UUID, history[1].Revision, 1)
		require.NoError(t, err)
		require.Equal(t, history[2:], next)
		return nil
	}))
}

func TestSourceAccountOwnershipSurvivesPerformerMergeAdoptionAndDeletion(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	account := createSourceAccount(t, repo, "native:instagram")
	source := archiveFind(t, repo, models.ArchivePerformer, 71)
	ctx := context.Background()
	input := models.AccountOwnershipInput{AccountUUID: account.UUID, ExpectedAccountRevision: account.Revision,
		State: models.AccountOwnershipLinked, PerformerUUID: source.UUID, ExpectedPerformerRevision: source.Revision, Origin: "review"}
	require.NoError(t, repo.WithTxn(ctx, func(ctx context.Context) error {
		_, err := repo.SourceAccount.DecideOwnership(ctx, input)
		return err
	}))
	require.NoError(t, repo.WithTxn(ctx, func(ctx context.Context) error { return repo.Performer.Merge(ctx, []int{71}, 72) }))
	input.ExpectedAccountRevision++
	err := repo.WithTxn(ctx, func(ctx context.Context) error {
		_, err := repo.SourceAccount.DecideOwnership(ctx, input)
		return err
	})
	require.ErrorIs(t, err, models.ErrSourceAccountConflict, "reviewing an old performer identity cannot apply after a merge")
	dest := archiveFind(t, repo, models.ArchivePerformer, 72)
	imported := "190cb2e7-2f02-447f-b8ed-92e761318d66"
	require.NoError(t, repo.WithTxn(ctx, func(ctx context.Context) error {
		_, err := repo.ArchiveEntity.AdoptUUID(ctx, dest.UUID, imported, dest.Revision)
		return err
	}))
	require.NoError(t, repo.WithReadTxn(ctx, func(ctx context.Context) error {
		choice, err := repo.SourceAccount.Ownership(ctx, account.UUID)
		require.NoError(t, err)
		require.Equal(t, source.UUID, *choice.PerformerUUID)
		resolved, err := repo.ArchiveEntity.Resolve(ctx, *choice.PerformerUUID)
		require.NoError(t, err)
		require.Equal(t, imported, resolved.UUID)
		require.Equal(t, 72, *resolved.LocalID)
		return nil
	}))
	// A link directly targeting the adopted UUID also follows its FK update.
	input.PerformerUUID = imported
	input.ExpectedPerformerRevision = archiveFind(t, repo, models.ArchivePerformer, 72).Revision
	require.NoError(t, repo.WithTxn(ctx, func(ctx context.Context) error {
		_, err := repo.SourceAccount.DecideOwnership(ctx, input)
		return err
	}))
	adopted := archiveFind(t, repo, models.ArchivePerformer, 72)
	newUUID := "c5b3b512-27d6-4c2c-a8e5-a49cbb06e260"
	require.NoError(t, repo.WithTxn(ctx, func(ctx context.Context) error {
		_, err := repo.ArchiveEntity.AdoptUUID(ctx, adopted.UUID, newUUID, adopted.Revision)
		return err
	}))
	require.NoError(t, repo.WithTxn(ctx, func(ctx context.Context) error { return repo.Performer.Destroy(ctx, 72) }))
	require.NoError(t, repo.WithReadTxn(ctx, func(ctx context.Context) error {
		choice, err := repo.SourceAccount.Ownership(ctx, account.UUID)
		require.NoError(t, err)
		require.Equal(t, newUUID, *choice.PerformerUUID)
		resolved, err := repo.ArchiveEntity.Resolve(ctx, *choice.PerformerUUID)
		require.NoError(t, err)
		require.Equal(t, models.ArchiveEntityDeleted, resolved.State)
		return nil
	}))
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	rows, err := raw.Query("PRAGMA foreign_key_check")
	require.NoError(t, err)
	defer rows.Close()
	require.False(t, rows.Next())
	require.NoError(t, rows.Err())
}

func TestSourceAccountOwnershipHeadFailureRollsBackDecision(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	account := createSourceAccount(t, repo, "native:reddit")
	ctx := context.Background()
	require.NoError(t, repo.WithTxn(ctx, func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `CREATE TRIGGER fail_account_head BEFORE INSERT ON account_performer_links
BEGIN SELECT RAISE(ABORT, 'injected ownership failure'); END`, nil)
		return err
	}))
	err := repo.WithTxn(ctx, func(ctx context.Context) error {
		_, err := repo.SourceAccount.DecideOwnership(ctx, models.AccountOwnershipInput{
			AccountUUID: account.UUID, ExpectedAccountRevision: account.Revision, State: models.AccountOwnershipUnlinked, Origin: "review"})
		return err
	})
	require.ErrorContains(t, err, "injected ownership failure")
	require.Equal(t, account, findSourceAccount(t, repo, account.UUID))
	require.NoError(t, repo.WithReadTxn(ctx, func(ctx context.Context) error {
		history, err := repo.SourceAccount.OwnershipHistory(ctx, account.UUID, 0, 10)
		require.NoError(t, err)
		require.Empty(t, history)
		choice, err := repo.SourceAccount.Ownership(ctx, account.UUID)
		require.NoError(t, err)
		require.Nil(t, choice)
		return nil
	}))
}

func TestSourceAccountOwnershipDatabaseGuards(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	account := createSourceAccount(t, repo, "native:reddit")
	other := createSourceAccount(t, repo, "native:reddit")
	performer := archiveFind(t, repo, models.ArchivePerformer, 71)
	scene := archiveFind(t, repo, models.ArchiveScene, 31)
	ctx := context.Background()
	var decisions []*models.AccountOwnershipDecision
	for _, state := range []models.AccountOwnershipState{models.AccountOwnershipLinked, models.AccountOwnershipUnlinked} {
		input := models.AccountOwnershipInput{AccountUUID: account.UUID, ExpectedAccountRevision: account.Revision, State: state, Origin: "review"}
		if state == models.AccountOwnershipLinked {
			input.PerformerUUID, input.ExpectedPerformerRevision = performer.UUID, performer.Revision
		}
		require.NoError(t, repo.WithTxn(ctx, func(ctx context.Context) error {
			d, err := repo.SourceAccount.DecideOwnership(ctx, input)
			decisions = append(decisions, d)
			return err
		}))
		account.Revision++
	}
	for _, tc := range []struct {
		query   string
		args    []interface{}
		message string
	}{
		{"UPDATE account_performer_decisions SET reason='rewritten' WHERE uuid=?", []interface{}{decisions[0].UUID}, "immutable"},
		{"UPDATE account_performer_links SET decision_uuid=? WHERE account_uuid=?", []interface{}{decisions[0].UUID, account.UUID}, "backwards"},
		{"INSERT INTO account_performer_links(account_uuid, decision_uuid) VALUES (?, ?)", []interface{}{other.UUID, decisions[1].UUID}, "FOREIGN KEY"},
		{"INSERT INTO account_performer_decisions(uuid, account_uuid, revision, state, performer_uuid, origin) VALUES ('invalid-choice', ?, 100, 'linked', ?, 'review')", []interface{}{account.UUID, scene.UUID}, "requires a performer"},
	} {
		err := repo.WithTxn(ctx, func(ctx context.Context) error {
			_, _, err := db.ExecSQL(ctx, tc.query, tc.args)
			return err
		})
		require.ErrorContains(t, err, tc.message)
	}
	require.NoError(t, repo.WithReadTxn(ctx, func(ctx context.Context) error {
		current, err := repo.SourceAccount.Ownership(ctx, account.UUID)
		require.NoError(t, err)
		require.Equal(t, decisions[1], current)
		return nil
	}))
}

func TestSourceAccountRejectsInvalidEvidenceWithoutPartialClaims(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	account := createSourceAccount(t, repo, "native:reddit")
	ref := models.AccountReference{Namespace: "native:reddit", Kind: "id", Value: "t2_author"}
	for _, body := range []string{`null`, `[]`, `{"bad":}`, `"not-object"`, `{"id":1,"id":2}`, `{"nested":[{"id":1,"id":2}]}`, "{\"invalid\":\"\xff\"}"} {
		evidence := accountEvidence()
		evidence.Details = []byte(body)
		err := repo.WithTxn(context.Background(), func(ctx context.Context) error {
			_, err := repo.SourceAccount.ObserveIdentifier(ctx, account.UUID, ref, evidence)
			return err
		})
		require.Error(t, err)
	}
	ref.Namespace = "native:twitter"
	err := repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.SourceAccount.ObserveIdentifier(ctx, account.UUID, ref, accountEvidence())
		return err
	})
	require.ErrorContains(t, err, "different service namespace")
	require.Equal(t, account, findSourceAccount(t, repo, account.UUID))
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		ids, err := repo.SourceAccount.Identifiers(ctx, account.UUID, "", 50)
		require.NoError(t, err)
		require.Empty(t, ids)
		return nil
	}))
}
