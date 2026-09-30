//go:build integration
// +build integration

package sqlite_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestAnonymiser_Anonymise(t *testing.T) {
	f, err := os.CreateTemp("", "*.sqlite")
	if err != nil {
		t.Errorf("Could not create temporary file: %v", err)
		return
	}

	f.Close()
	defer os.Remove(f.Name())

	// use existing database
	anonymiser, err := sqlite.NewAnonymiser(db, f.Name())
	if err != nil {
		t.Errorf("Could not create anonymiser: %v", err)
		return
	}

	if err := anonymiser.Anonymise(context.Background()); err != nil {
		t.Errorf("Could not anonymise: %v", err)
		return
	}

	t.Logf("Anonymised database written to %s", f.Name())

}

func TestAnonymiserRemovesNativeFilterEvidence(t *testing.T) {
	config.InitializeEmpty()
	dir := t.TempDir()
	path := filepath.Join(dir, "source.sqlite")
	source := sqlite.NewDatabase()
	require.NoError(t, source.Open(path))
	require.NoError(t, source.Close())
	raw := openRawDB(t, path)
	_, err := raw.Exec(`
INSERT INTO saved_filters(name, mode, find_filter, filter_ast, ui_options)
VALUES ('private-filter-value', 'SCENES', '{"q":"private-filter-value"}', '', '{"private":"private-filter-value"}');
INSERT INTO default_filters(view, mode, find_filter) VALUES ('scenes', 'SCENES', '{"q":"private-filter-value"}');
INSERT INTO configuration_migrations(name, source_json, target_json)
VALUES ('default-filters-v1', '{"private":"private-filter-value"}', '{"private":"private-filter-value"}');
INSERT INTO default_filter_import_conflicts(view, migration_name, evidence)
VALUES ('scenes', 'default-filters-v1', '{"private":"private-filter-value"}');
UPDATE native_migration_history SET details = '{"private":"private-filter-value"}';
`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	require.NoError(t, source.Open(path))
	defer source.Close()
	output := filepath.Join(dir, "anonymous.sqlite")
	anonymiser, err := sqlite.NewAnonymiser(source, output)
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(context.Background()))
	contents, err := os.ReadFile(output)
	require.NoError(t, err)
	require.NotContains(t, string(contents), "private-filter-value", "VACUUM must also remove deleted evidence from free pages")
	// Only the export was anonymised.
	raw = openRawDB(t, path)
	defer raw.Close()
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM default_filter_import_conflicts"))
}

func TestAnonymiserRekeysArchiveUUIDsAndKeepsRedirects(t *testing.T) {
	source, repo := archiveTestDatabase(t)
	ctx := context.Background()
	before := archiveFind(t, repo, models.ArchivePerformer, 71)
	survivor := archiveFind(t, repo, models.ArchivePerformer, 72)
	require.NoError(t, repo.WithTxn(ctx, func(ctx context.Context) error { return repo.Performer.Merge(ctx, []int{71}, 72) }))
	output := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymiser, err := sqlite.NewAnonymiser(source, output)
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(ctx))
	contents, err := os.ReadFile(output)
	require.NoError(t, err)
	for _, value := range []string{before.UUID, survivor.UUID} {
		require.NotContains(t, string(contents), value)
	}
	raw := openRawDB(t, output)
	defer raw.Close()
	require.Equal(t, uint(1), queryUint(t, raw, `SELECT count(*) FROM archive_entities source
JOIN archive_entities target ON target.uuid=source.redirect_to
WHERE source.original_id=71 AND source.kind='performer' AND source.state='redirected' AND target.performer_id=72`))
	rows, err := raw.Query("PRAGMA foreign_key_check")
	require.NoError(t, err)
	defer rows.Close()
	require.False(t, rows.Next())
	require.NoError(t, rows.Err())
	require.Equal(t, survivor.UUID, archiveFind(t, repo, models.ArchivePerformer, 72).UUID)
}

func TestAnonymiserRemovesSourceAccountEvidence(t *testing.T) {
	source, repo := archiveTestDatabase(t)
	account := createSourceAccount(t, repo, "native:reddit")
	evidence := accountEvidence()
	evidence.Details = []byte(`{"private":"private-account-evidence"}`)
	observeAccount(t, repo, account.UUID, models.AccountReference{Namespace: "native:reddit", Kind: "handle", Value: "private-account-handle"}, evidence)
	performer := archiveFind(t, repo, models.ArchivePerformer, 71)
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.SourceAccount.DecideOwnership(ctx, models.AccountOwnershipInput{AccountUUID: account.UUID,
			ExpectedAccountRevision: account.Revision + 1, State: models.AccountOwnershipLinked, PerformerUUID: performer.UUID,
			ExpectedPerformerRevision: performer.Revision, Origin: "review", Reason: "private-account-choice"})
		return err
	}))
	output := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymiser, err := sqlite.NewAnonymiser(source, output)
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(context.Background()))
	contents, err := os.ReadFile(output)
	require.NoError(t, err)
	for _, value := range []string{account.UUID, performer.UUID, "private-account-"} {
		require.NotContains(t, string(contents), value)
	}
	require.Equal(t, account.UUID, findSourceAccount(t, repo, account.UUID).UUID)
	raw := openRawDB(t, output)
	defer raw.Close()
	for _, table := range []string{"source_accounts", "source_account_identifiers", "source_account_identifier_evidence", "account_performer_decisions", "account_performer_links"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
}
