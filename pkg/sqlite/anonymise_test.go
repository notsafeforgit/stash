//go:build integration
// +build integration

package sqlite_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
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
