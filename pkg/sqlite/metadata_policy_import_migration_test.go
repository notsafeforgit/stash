package sqlite_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeMetadataPolicyImportSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeMetadataNameSchema(t, raw)
	_, err := raw.Exec(`DROP TABLE metadata_policy_import_documents;
 DROP TABLE metadata_policy_imports;
 DELETE FROM native_migration_history WHERE version=1000078;`)
	require.NoError(t, err)
}

func TestMetadataPolicyImportMigrationPreservesPoliciesAndDocuments(t *testing.T) {
	for _, collision := range []string{"", "metadata_policy_imports", "metadata_policy_import_documents"} {
		t.Run(collision, func(t *testing.T) {
			f := newMetadataPolicyFixture(t)
			f.put(t, 0, models.MetadataPolicyRule{OnCreate: true, FilenameTitleFallback: true})
			doc := retainDocument(t, f.repo, models.SourceDocumentInput{Content: []byte("<movie/>"), Encoding: "utf-8", Parser: "fixture", ParseStatus: "valid", Warnings: json.RawMessage(`[]`), Parsed: json.RawMessage(`{}`)})
			recordDocumentSource(t, f.repo, models.SourceDocumentSource{UUID: uuid.NewString(), DocumentUUID: doc.UUID, CollectionUUID: f.collection.UUID, CollectionRevision: f.collection.Revision, RelativePath: "folder.nfo", Origin: "migration", Details: json.RawMessage(`{}`)})
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			removeMetadataPolicyImportSchema(t, raw)
			_, err := raw.Exec("UPDATE schema_migrations SET version=1000077,dirty=0")
			require.NoError(t, err)
			before := map[string][][]any{}
			for _, table := range []string{"scenes", "archive_entities", "source_collections", "source_collection_revisions", "metadata_policies", "metadata_policy_revisions", "source_documents", "source_document_contents", "source_document_sources", "native_migration_history"} {
				before[table] = albumJobRows(t, raw, table)
			}
			if collision != "" {
				_, err := raw.Exec("CREATE TABLE " + collision + "(original TEXT); INSERT INTO " + collision + " VALUES('retained unknown input')")
				require.NoError(t, err)
			}
			var needed *sqlite.MigrationNeededError
			require.True(t, errors.As(f.db.Open(f.db.DatabasePath()), &needed))
			err = f.db.RunAllMigrations()
			if collision != "" {
				require.Error(t, err)
				var original string
				require.NoError(t, raw.QueryRow("SELECT original FROM "+collision).Scan(&original))
				require.Equal(t, "retained unknown input", original)
				require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM schema_migrations WHERE dirty=1"))
				if collision == "metadata_policy_import_documents" {
					require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='metadata_policy_imports'"), "migration failure must roll back preceding statements")
				}
			} else {
				require.NoError(t, err)
				require.NoError(t, f.db.ReInitialise())
				require.Equal(t, f.db.AppSchemaVersion(), f.db.Version())
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM metadata_policy_imports"))
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM metadata_policy_import_documents"))
				require.EqualValues(t, len(before["native_migration_history"])+int(f.db.AppSchemaVersion()-1000077), queryUint(t, raw, "SELECT count(*) FROM native_migration_history"))
				delete(before, "native_migration_history")
			}
			for table, rows := range before {
				require.Equal(t, rows, albumJobRows(t, raw, table), table)
			}
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
		})
	}
}

func TestMetadataPolicyImportCorruptProvenanceIsRejectedByAudit(t *testing.T) {
	for _, change := range []string{
		"binding=json_set(binding,'$.document.plugin_version','altered')",
		"plan=json_set(plan,'$.collection.label','altered')",
	} {
		t.Run(change, func(t *testing.T) {
			f := newMetadataPolicyFixture(t)
			input := policyImportInput(t, f)
			plan := previewPolicyImport(t, f, input)
			_, err := applyPolicyImport(f, input, plan.PlanSHA256)
			require.NoError(t, err)
			require.NoError(t, f.db.Close())
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			var trigger string
			require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='metadata_policy_import_immutable'").Scan(&trigger))
			_, err = raw.Exec("DROP TRIGGER metadata_policy_import_immutable; UPDATE metadata_policy_imports SET " + change + "; " + trigger)
			require.NoError(t, err)
			require.Error(t, f.db.AuditForTesting(f.db.DatabasePath()), "restored provenance must match the retained digests")
		})
	}
}
