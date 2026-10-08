package sqlite_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stashapp/stash/pkg/stashbox"
	"github.com/stretchr/testify/require"
)

func removeProviderMetadataSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	var exists bool
	require.NoError(t, raw.QueryRow("SELECT EXISTS(SELECT 1 FROM native_migration_history WHERE version=1000096)").Scan(&exists))
	if exists {
		_, err := raw.Exec("DROP TABLE provider_metadata_imports; DELETE FROM native_migration_history WHERE version=1000096")
		require.NoError(t, err)
	}
}

func providerHistory(t *testing.T, repo models.Repository, id string) []models.ProviderMetadataImport {
	t.Helper()
	var ret []models.ProviderMetadataImport
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.ProviderMetadata.History(ctx, id, 0, 100)
		return err
	}))
	return ret
}

func providerRecord(t *testing.T, repo models.Repository, id string, endpoint string, fields ...string) *models.ProviderMetadataImport {
	t.Helper()
	var ret *models.ProviderMetadataImport
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		entity, err := repo.ArchiveEntity.Find(ctx, id)
		if err != nil {
			return err
		}
		ret, err = repo.ProviderMetadata.Record(ctx, models.ProviderMetadataImportInput{EntityUUID: id, ExpectedEntityRevision: entity.Revision,
			Endpoint: endpoint, RemoteID: "remote-1", Operation: "review", Fields: fields})
		return err
	}))
	return ret
}

func TestProviderMetadataRetainsAcceptedValuesAndDistinctProviders(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	entity := archiveFind(t, repo, models.ArchivePerformer, 71)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		p := models.NewPerformerPartial()
		p.Name = models.NewOptionalString("Accepted remote name")
		p.Aliases = &models.UpdatePerformerAliases{Values: []models.PerformerAlias{{Alias: "local alias", IgnoreAutoTag: true}, {Alias: "remote alias"}}, Mode: models.RelationshipUpdateModeSet}
		_, err := repo.Performer.UpdatePartial(ctx, 71, p)
		if err != nil {
			return err
		}
		remote := "remote-1"
		return stashbox.RecordMetadataImport(ctx, repo, models.ArchivePerformer, 71, "https://first.invalid/graphql", &remote, "batch", stashbox.PerformerImportFields(p, false))
	}))
	before := providerHistory(t, repo, entity.UUID)
	require.Len(t, before, 1)
	var values map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(before[0].Values, &values))
	require.Equal(t, json.RawMessage(`"Accepted remote name"`), values["name"])
	require.Contains(t, string(values["aliases"]), "local alias")
	require.Contains(t, string(values["aliases"]), "remote alias")
	require.Len(t, values, 2)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		p := models.NewPerformerPartial()
		p.Name = models.NewOptionalString("Later manual name")
		_, err := repo.Performer.UpdatePartial(ctx, 71, p)
		return err
	}))
	require.Equal(t, before, providerHistory(t, repo, entity.UUID))
	second := providerRecord(t, repo, entity.UUID, "https://second.invalid/graphql", "name")
	require.JSONEq(t, `{"name":"Later manual name"}`, string(second.Values))
	require.NotEqual(t, before[0].Signature, second.Signature)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	after := providerHistory(t, repo, entity.UUID)
	require.Len(t, after, 2)
	require.Equal(t, before[0], after[0])
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		page, err := repo.ProviderMetadata.History(ctx, entity.UUID, before[0].Sequence, 1)
		require.NoError(t, err)
		require.Equal(t, []models.ProviderMetadataImport{*second}, page)
		return nil
	}))
}

func TestProviderMetadataReadsNativeRelationsAndArtworkDigest(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	db.SetBlobStoreOptions(sqlite.BlobStoreOptions{UseDatabase: true})
	scene := archiveFind(t, repo, models.ArchiveScene, 31)
	p := archiveFind(t, repo, models.ArchivePerformer, 71)
	result := providerRecord(t, repo, scene.UUID, "https://provider.invalid/graphql", "title", "performers", "date")
	require.JSONEq(t, `{"title":"Kept title","performers":["`+p.UUID+`"],"date":null}`, string(result.Values))
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return repo.Performer.UpdateImage(ctx, 71, []byte("image fixture bytes"))
	}))
	image := providerRecord(t, repo, p.UUID, "https://provider.invalid/graphql", "image")
	var values map[string]map[string]any
	require.NoError(t, json.Unmarshal(image.Values, &values))
	require.Len(t, values["image"]["sha256"], 64)
	require.EqualValues(t, len("image fixture bytes"), values["image"]["bytes"])
	require.NotContains(t, string(image.Values), "image fixture bytes")
}

func TestProviderMetadataSupportsRetainedProviderEntityFields(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	db.SetBlobStoreOptions(sqlite.BlobStoreOptions{UseDatabase: true})
	var studioID, tagID int
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		parent := models.NewCreateStudioInput()
		parent.Name = "Parent studio"
		if err := repo.Studio.Create(ctx, &parent); err != nil {
			return err
		}
		studio := models.NewCreateStudioInput()
		studio.Name, studio.ParentID = "Child studio", &parent.ID
		if err := repo.Studio.Create(ctx, &studio); err != nil {
			return err
		}
		studioID = studio.ID
		tag := models.NewTag()
		tag.Name = "Provider tag"
		if err := repo.Tag.Create(ctx, &models.CreateTagInput{Tag: &tag}); err != nil {
			return err
		}
		tagID = tag.ID
		p := models.NewPerformerPartial()
		date, err := models.ParseDate("1990")
		if err != nil {
			return err
		}
		p.Birthdate = models.NewOptionalDate(date)
		_, err = repo.Performer.UpdatePartial(ctx, 71, p)
		return err
	}))
	for kind, id := range map[models.ArchiveEntityKind]int{models.ArchiveScene: 31, models.ArchivePerformer: 71, models.ArchiveStudio: studioID, models.ArchiveTag: tagID} {
		t.Run(string(kind), func(t *testing.T) {
			entity := archiveFind(t, repo, kind, id)
			result := providerRecord(t, repo, entity.UUID, "https://provider.invalid/graphql", models.ProviderMetadataFields(kind)...)
			var values map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(result.Values, &values))
			require.Len(t, values, len(models.ProviderMetadataFields(kind)))
			if kind == models.ArchivePerformer {
				require.Equal(t, json.RawMessage(`"1990"`), values["birthdate"])
			}
			if kind == models.ArchiveStudio {
				var parentUUID string
				require.NoError(t, json.Unmarshal(values["parent"], &parentUUID))
				require.NotEqual(t, uuid.Nil.String(), parentUUID)
				require.Len(t, parentUUID, 36)
			}
		})
	}
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
}

func TestProviderMetadataValidationRollsBackTheAcceptedEdit(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	entity := archiveFind(t, repo, models.ArchivePerformer, 71)
	for _, scenario := range []string{"unknown field", "empty fields", "duplicate field", "credentials", "query secret", "invalid operation", "stale revision", "invalid remote id"} {
		t.Run(scenario, func(t *testing.T) {
			err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
				p := models.NewPerformerPartial()
				p.Name = models.NewOptionalString("Must roll back")
				if _, err := repo.Performer.UpdatePartial(ctx, 71, p); err != nil {
					return err
				}
				current, err := repo.ArchiveEntity.Find(ctx, entity.UUID)
				if err != nil {
					return err
				}
				input := models.ProviderMetadataImportInput{EntityUUID: entity.UUID, ExpectedEntityRevision: current.Revision,
					Endpoint: "https://provider.invalid/graphql", RemoteID: "remote-1", Operation: "review", Fields: []string{"name"}}
				switch scenario {
				case "unknown field":
					input.Fields = []string{"stash_ids"}
				case "empty fields":
					input.Fields = nil
				case "duplicate field":
					input.Fields = []string{"name", "name"}
				case "credentials":
					input.Endpoint = "https://username:secret@provider.invalid/graphql"
				case "query secret":
					input.Endpoint += "?apikey=secret"
				case "invalid operation":
					input.Operation = "invented"
				case "stale revision":
					input.ExpectedEntityRevision = current.Revision + 1
				case "invalid remote id":
					input.RemoteID = " "
				}
				_, err = repo.ProviderMetadata.Record(ctx, input)
				return err
			})
			require.Error(t, err)
			require.Empty(t, providerHistory(t, repo, entity.UUID))
			require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				p, err := repo.Performer.Find(ctx, 71)
				require.NoError(t, err)
				require.Equal(t, "Shared name", p.Name)
				return nil
			}))
		})
	}
}

func TestProviderMetadataSurvivesIdentityAdoptionAndDeletion(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	entity := archiveFind(t, repo, models.ArchivePerformer, 71)
	before := providerRecord(t, repo, entity.UUID, "https://provider.invalid/graphql", "name")
	adopted := uuid.NewString()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.ArchiveEntity.AdoptUUID(ctx, entity.UUID, adopted, entity.Revision)
		return err
	}))
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Performer.Destroy(ctx, 71) }))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	after := providerHistory(t, repo, adopted)
	require.Len(t, after, 1)
	require.Equal(t, entity.UUID, after[0].OriginalEntityUUID)
	require.Equal(t, adopted, after[0].EntityUUID)
	require.Equal(t, before.Signature, after[0].Signature)
}

func TestProviderMetadataMigrationAndForeignCollision(t *testing.T) {
	for _, collision := range []bool{false, true} {
		t.Run(map[bool]string{false: "preserves existing data", true: "rejects unknown table"}[collision], func(t *testing.T) {
			db, _ := archiveTestDatabase(t)
			require.NoError(t, db.Close())
			raw := openRawDB(t, db.DatabasePath())
			defer raw.Close()
			before := albumJobRows(t, raw, "archive_entities")
			removeProviderMetadataSchema(t, raw)
			_, err := raw.Exec("UPDATE schema_migrations SET version=1000095,dirty=0")
			require.NoError(t, err)
			if collision {
				_, err = raw.Exec("CREATE TABLE provider_metadata_imports(private_data TEXT); INSERT INTO provider_metadata_imports VALUES('retained')")
				require.NoError(t, err)
			}
			var needed *sqlite.MigrationNeededError
			require.ErrorAs(t, db.Open(db.DatabasePath()), &needed)
			err = db.RunAllMigrations()
			if collision {
				require.ErrorContains(t, err, "destination provider_metadata_imports already exists")
				var value string
				require.NoError(t, raw.QueryRow("SELECT private_data FROM provider_metadata_imports").Scan(&value))
				require.Equal(t, "retained", value)
			} else {
				require.NoError(t, err)
				require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM provider_metadata_imports"))
			}
			require.Equal(t, before, albumJobRows(t, raw, "archive_entities"))
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
		})
	}
}

func TestProviderMetadataRejectsTamperingAndAnonymisesPrivateData(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	entity := archiveFind(t, repo, models.ArchivePerformer, 71)
	providerRecord(t, repo, entity.UUID, "https://private-provider.invalid/graphql", "name")
	destination := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymiser, err := sqlite.NewAnonymiser(db, destination)
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(t.Context()))
	rawAnon := openRawDB(t, destination)
	require.Zero(t, queryUint(t, rawAnon, "SELECT count(*) FROM provider_metadata_imports"))
	require.NoError(t, rawAnon.Close())
	require.NoError(t, db.Close())
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	_, err = raw.Exec("UPDATE provider_metadata_imports SET remote_id='changed'")
	require.Error(t, err)
	var guard string
	require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='provider_metadata_import_immutable'").Scan(&guard))
	_, err = raw.Exec("DROP TRIGGER provider_metadata_import_immutable; UPDATE provider_metadata_imports SET remote_id='changed';" + guard)
	require.NoError(t, err)
	require.ErrorIs(t, db.Open(db.DatabasePath()), models.ErrSourcePayloadCorrupt)
	var remote string
	require.NoError(t, raw.QueryRow("SELECT remote_id FROM provider_metadata_imports").Scan(&remote))
	require.Equal(t, "changed", remote)
}

// A failing operation must roll back both the entity and its import receipt.
func TestProviderMetadataReceiptRollsBackWithItsTransaction(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	entity := archiveFind(t, repo, models.ArchivePerformer, 71)
	want := errors.New("later operation failed")
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.ProviderMetadata.Record(ctx, models.ProviderMetadataImportInput{EntityUUID: entity.UUID, ExpectedEntityRevision: entity.Revision,
			Endpoint: "https://provider.invalid/graphql", RemoteID: "remote-1", Operation: "batch", Fields: []string{"name"}})
		if err != nil {
			return err
		}
		return want
	})
	require.ErrorIs(t, err, want)
	require.Empty(t, providerHistory(t, repo, entity.UUID))
}

func TestProviderMetadataCannotCommitAnEditAfterIgnoringReceiptFailure(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	entity := archiveFind(t, repo, models.ArchivePerformer, 71)
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
		p := models.NewPerformerPartial()
		p.Name = models.NewOptionalString("Must roll back")
		if _, err := repo.Performer.UpdatePartial(ctx, 71, p); err != nil {
			return err
		}
		_, err := repo.ProviderMetadata.Record(ctx, models.ProviderMetadataImportInput{EntityUUID: entity.UUID, ExpectedEntityRevision: entity.Revision,
			Endpoint: "https://provider.invalid/graphql", RemoteID: "remote-1", Operation: "batch", Fields: []string{"unknown"}})
		require.Error(t, err)
		return nil // even an incorrectly swallowed error cannot commit the edit
	})
	require.Error(t, err)
	require.Empty(t, providerHistory(t, repo, entity.UUID))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		p, err := repo.Performer.Find(ctx, 71)
		require.NoError(t, err)
		require.Equal(t, "Shared name", p.Name)
		return nil
	}))
}

func TestProviderMetadataRemainsOnMergedIdentity(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	from, to := archiveFind(t, repo, models.ArchivePerformer, 71), archiveFind(t, repo, models.ArchivePerformer, 72)
	before := providerRecord(t, repo, from.UUID, "https://provider.invalid/graphql", "name")
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Performer.Merge(ctx, []int{71}, 72) }))
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	require.Equal(t, []models.ProviderMetadataImport{*before}, providerHistory(t, repo, from.UUID))
	require.Empty(t, providerHistory(t, repo, to.UUID))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		resolved, err := repo.ArchiveEntity.Resolve(ctx, from.UUID)
		require.NoError(t, err)
		require.Equal(t, to.UUID, resolved.UUID)
		return nil
	}))
}
