package sqlite_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

// Older migration fixtures remove subsequent schema objects before exercising
// their own upgrade. Restore the actual scalar triggers from schema 1000012.
func removeMetadataCurrentValuesSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	var present bool
	require.NoError(t, raw.QueryRow("SELECT EXISTS(SELECT 1 FROM native_migration_history WHERE version=1000141)").Scan(&present))
	if !present {
		return
	}
	_, err := raw.Exec(`DROP TRIGGER metadata_field_retire_superseded;
DROP TRIGGER metadata_field_decision_cleanup;
DROP INDEX metadata_file_edit_keeps_decision;
DELETE FROM native_migration_history WHERE version=1000141;`)
	require.NoError(t, err)
	prior, err := os.ReadFile("migrations/1000012_metadata_field_choices.up.sql")
	require.NoError(t, err)
	for _, match := range regexp.MustCompile(`(?s)CREATE TRIGGER (metadata_(?:scene|image|gallery)_\w+_library).*?END;`).FindAllStringSubmatch(string(prior), -1) {
		_, err := raw.Exec("DROP TRIGGER " + match[1] + ";" + match[0])
		require.NoError(t, err)
	}
}

func TestMetadataCurrentValuesSuppressOrdinaryRepeatedSaves(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	attachmentSQL(t, db, archiveMetadataFixture)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	for _, kind := range []models.ArchiveEntityKind{models.ArchiveScene, models.ArchiveImage, models.ArchiveGallery} {
		id, table := 31, "scenes"
		switch kind {
		case models.ArchiveImage:
			id, table = 41, "images"
		case models.ArchiveGallery:
			id, table = createArchiveGallery(t, repo, "A gallery").ID, "galleries"
		}
		entity := archiveFind(t, repo, kind, id)
		for _, field := range models.MetadataFields(kind) {
			if field.ReferenceKind != "" || field.Type == "urls" || field.Type == "custom_fields" {
				continue
			}
			column := field.Name
			if column == "rating100" {
				column = "rating"
			}
			for _, empty := range []bool{false, true} {
				var value any = "Explicit text"
				switch field.Type {
				case "date":
					value = "2026-11-13"
				case "integer":
					value = 47
				case "boolean":
					value = true
				}
				if empty {
					switch field.Type {
					case "string":
						value = ""
					case "boolean":
						value = false
					default:
						value = nil
					}
				}
				query := "UPDATE " + table + " SET " + column + "=? WHERE id=?"
				_, err := raw.Exec(query, value, id)
				require.NoError(t, err)
				before := metadataState(t, repo, entity.UUID, field.Name)
				require.True(t, before.Protected)
				_, err = raw.Exec(query, value, id)
				require.NoError(t, err)
				after := metadataState(t, repo, entity.UUID, field.Name)
				require.Equal(t, before.Decision, after.Decision, "%s.%s empty=%v", kind, field.Name, empty)
				var count int
				require.NoError(t, raw.QueryRow("SELECT count(*) FROM metadata_field_decisions WHERE entity_uuid=? AND field=?", entity.UUID, field.Name).Scan(&count))
				require.Equal(t, 1, count)
			}
		}
	}
	// Relationship intent must still protect an inherited value, but repeating an
	// already explicit choice must leave both its UUID and selected value alone.
	scene := archiveFind(t, repo, models.ArchiveScene, 31)
	partial := models.ScenePartial{PerformerIDs: &models.UpdateIDs{IDs: []int{71}, Mode: models.RelationshipUpdateModeSet},
		URLs: &models.UpdateStrings{Values: []string{"https://example.test/post"}, Mode: models.RelationshipUpdateModeSet}}
	for _, field := range []string{"performers", "urls"} {
		require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := repo.Scene.UpdatePartial(ctx, 31, partial)
			return err
		}))
		before := metadataState(t, repo, scene.UUID, field)
		require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := repo.Scene.UpdatePartial(ctx, 31, partial)
			return err
		}))
		require.Equal(t, before.Decision, metadataState(t, repo, scene.UUID, field).Decision)
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestMetadataCurrentValuesMigrationPreservesHeadsAndPrunesHistory(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "before-current-values.sqlite")
	buildLegacyDatabase(t, path, 86, true)
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.ErrorAs(t, db.Open(path), &needed)
	m, err := sqlite.NewMigrator(db)
	require.NoError(t, err)
	for version := m.CurrentSchemaVersion(); version < 1000140; version = m.CurrentSchemaVersion() {
		require.NoError(t, m.RunMigration(t.Context(), m.GetNextMigrationVersion(version)))
	}
	m.Close()
	raw := openRawDB(t, path)
	defer raw.Close()
	_, err = raw.Exec(`INSERT INTO scenes(id,title,created_at,updated_at) VALUES (31,'Initial',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
UPDATE scenes SET title='Old value' WHERE id=31;
UPDATE scenes SET title='Current value' WHERE id=31;
UPDATE scenes SET title='Current value' WHERE id=31;`)
	require.NoError(t, err)
	before := map[string][][]any{}
	for _, table := range []string{"scenes", "images", "archive_entities", "metadata_field_heads"} {
		before[table] = albumJobRows(t, raw, table)
	}
	choices := queryUint(t, raw, "SELECT count(*) FROM metadata_field_decisions")
	require.Greater(t, choices, queryUint(t, raw, "SELECT count(*) FROM metadata_field_heads"))
	// An interrupted migration must leave both history and current state intact.
	migration, err := os.ReadFile("migrations/1000141_metadata_current_values.up.sql")
	require.NoError(t, err)
	tx, err := raw.Begin()
	require.NoError(t, err)
	_, err = tx.Exec(string(migration))
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	require.Equal(t, choices, queryUint(t, raw, "SELECT count(*) FROM metadata_field_decisions"))
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	defer db.Close()
	for table, rows := range before {
		require.Equal(t, rows, albumJobRows(t, raw, table), table)
	}
	require.Equal(t, queryUint(t, raw, "SELECT count(*) FROM metadata_field_heads"), queryUint(t, raw, "SELECT count(*) FROM metadata_field_decisions"))
	require.Equal(t, choices-queryUint(t, raw, "SELECT count(*) FROM metadata_field_decisions"), queryUint(t, raw, "SELECT json_extract(details,'$.retired_field_choices') FROM native_migration_history WHERE version=1000141"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(path))
}

func TestMetadataCurrentValuesRetiresAutomaticReferencesAndProvenance(t *testing.T) {
	f := newMetadataPolicyFixture(t)
	performer := archiveFind(t, f.repo, models.ArchivePerformer, 71)
	rule := models.MetadataPolicyRule{OnCreate: true, OnExisting: true, FilenameTitleFallback: true,
		Mappings: map[string]models.MetadataMapping{"performers": {Value: []byte(`["` + performer.UUID + `"]`)}}}
	policy := f.put(t, 0, rule)
	_, err := applyPolicy(f.repo, f.input(policy), "")
	require.NoError(t, err)
	prior := metadataState(t, f.repo, f.entity.UUID, "performers")
	require.NotNil(t, prior.Decision.Policy)
	_, err = applyMetadata(f.repo, metadataInput(prior, "clear", "review", ""), false)
	require.NoError(t, err)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	for _, table := range []string{"metadata_field_decisions", "metadata_field_references", "metadata_decision_policies", "metadata_decision_post_media"} {
		column := "decision_uuid"
		if table == "metadata_field_decisions" {
			column = "uuid"
		}
		var count int
		require.NoError(t, raw.QueryRow("SELECT count(*) FROM "+table+" WHERE "+column+"=?", prior.Decision.UUID).Scan(&count))
		require.Zero(t, count, table)
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}
