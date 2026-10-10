package sqlite_test

import (
	"bytes"
	"context"
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

func mediaImportFixture(t *testing.T, count int, alter func([]map[string]any)) (*catalogSnapshotFixture, models.CatalogMediaBinding) {
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
		key := []any{}
		for _, column := range f.manifest.Tables[table].Key {
			key = append(key, values[column])
		}
		rows = append(rows, map[string]any{"table": table, "key": key, "values": values})
	}
	for i := range count {
		asset := fmt.Sprintf("path:source-%03d", i)
		path := fmt.Sprintf("unavailable-%03d.mp4", i)
		if i == 0 {
			path = "video.mp4"
		}
		add("assets", map[string]any{"asset_id": asset, "digest_algorithm": nil, "digest": nil, "byte_size": 1000, "created_at": f.manifest.CapturedAt})
		add("files", map[string]any{"relpath": path, "asset_id": asset, "state": "present", "byte_size": 1000, "mtime_ns": nil, "first_observed": f.manifest.CapturedAt, "survivor_relpath": nil, "role": "local"})
		add("appearances", map[string]any{"post_key": "reddit:post:album", "attachment_key": path, "asset_id": asset, "source_media_id": nil, "position": nil, "source_relpath": path})
	}
	if count > 0 {
		add("files", map[string]any{"relpath": "converted.gif", "asset_id": "path:source-000", "state": "missing", "byte_size": 321, "mtime_ns": 1, "first_observed": f.manifest.CapturedAt, "survivor_relpath": "video.mp4", "role": "converted-source"})
		add("appearances", map[string]any{"post_key": "reddit:post:album", "attachment_key": "converted", "asset_id": "path:source-000", "source_media_id": "original-media-id", "position": 9, "source_relpath": "converted.gif"})
	}
	if alter != nil {
		alter(rows)
	}
	receiveCatalogFixtureRows(t, f, rows)
	return f, bindCatalogMediaFixture(t, f)
}

func bindCatalogMediaFixture(t *testing.T, f *catalogSnapshotFixture) models.CatalogMediaBinding {
	t.Helper()
	attachmentSQL(t, f.db, `INSERT INTO scenes_files(scene_id,file_id,"primary") VALUES(31,21,1)`)
	root := putMediaRoot(t, f.repo, models.MediaRootInput{Origin: "migration", MediaRootDefinition: models.MediaRootDefinition{Label: "Reviewed old library root", State: "disabled"}})
	var snapshot *models.CatalogSnapshot
	var collection *models.SourceCollection
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		snapshot, err = f.repo.CatalogSnapshot.Find(ctx, f.manifest.UUID)
		if err != nil {
			return err
		}
		collection, err = f.repo.SourceCollection.Find(ctx, snapshot.CollectionUUID)
		return err
	}))
	binding := models.CatalogMediaBinding{SnapshotUUID: f.manifest.UUID, ManifestSHA256: f.sha, RootUUID: root.UUID, RootRevision: root.Revision, CollectionRevision: collection.Revision, LibraryRootPath: "/identity-fixture"}
	return binding
}

func beginMediaImport(t *testing.T, f *catalogSnapshotFixture, binding models.CatalogMediaBinding) *models.CatalogMediaImport {
	t.Helper()
	var ret *models.CatalogMediaImport
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = f.repo.CatalogMediaImport.Begin(ctx, binding, catalogImportNow.Add(5*time.Hour))
		return err
	}))
	return ret
}

func advanceMediaImport(t *testing.T, f *catalogSnapshotFixture, after int64) *models.CatalogMediaImport {
	t.Helper()
	var ret *models.CatalogMediaImport
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = f.repo.CatalogMediaImport.Advance(ctx, f.manifest.UUID, f.sha, after, catalogImportNow.Add(6*time.Hour))
		return err
	}))
	return ret
}

func finishMediaImport(t *testing.T, f *catalogSnapshotFixture, prior *models.CatalogMediaImport) *models.CatalogMediaImport {
	t.Helper()
	for prior.State == "running" {
		prior = advanceMediaImport(t, f, prior.ProcessedRecords)
	}
	return prior
}

func TestCatalogMediaImportMapsSharedAssetsSurvivorsAndUnavailablePosts(t *testing.T) {
	f, binding := mediaImportFixture(t, 2, nil)
	initial := beginMediaImport(t, f, binding)
	require.Equal(t, initial, beginMediaImport(t, f, binding))
	result := finishMediaImport(t, f, initial)
	require.Equal(t, "mapped", result.State)
	require.False(t, result.Imported)
	require.EqualValues(t, 8, result.ProcessedRecords)
	require.EqualValues(t, 2, result.MatchedFiles)
	require.EqualValues(t, 2, result.MediaAssociations)
	require.EqualValues(t, 2, result.UnavailableRecords)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM source_content_claims"))
	require.EqualValues(t, 3, queryUint(t, raw, "SELECT count(*) FROM source_file_observations"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM files"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM media_contents"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_attachments"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_file_observations WHERE state='missing' AND role='converted-source' AND size=321 AND survivor_path='video.mp4'"))
	require.EqualValues(t, 3, queryUint(t, raw, "SELECT count(*) FROM source_post_file_evidence"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM source_post_file_evidence WHERE json_extract(details,'$.source_media_id')='original-media-id' AND json_extract(details,'$.position')=9"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM galleries"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	require.Equal(t, result, advanceMediaImport(t, f, result.ProcessedRecords))
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		rows, err := f.repo.CatalogMediaImport.Records(ctx, f.manifest.UUID, 0, 2)
		require.NoError(t, err)
		require.Len(t, rows, 2)
		next, err := f.repo.CatalogMediaImport.Records(ctx, f.manifest.UUID, rows[1].Ordinal, 100)
		require.NoError(t, err)
		require.Len(t, next, 6)
		for _, row := range append(rows, next...) {
			detail, err := f.repo.CatalogMediaImport.Record(ctx, f.manifest.UUID, row.Ordinal)
			require.NoError(t, err)
			require.Equal(t, row, detail.CatalogMediaRecord)
		}
		return nil
	}))
	output := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymiser, err := sqlite.NewAnonymiser(f.db, output)
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(t.Context()))
	anonymous := openRawDB(t, output)
	defer anonymous.Close()
	for _, table := range []string{"catalog_media_records", "catalog_media_imports", "source_content_claims", "source_file_matches", "source_file_observations", "source_post_file_evidence"} {
		require.Zero(t, queryUint(t, anonymous, "SELECT count(*) FROM "+table))
	}
	require.EqualValues(t, 8, queryUint(t, raw, "SELECT count(*) FROM catalog_media_records"))
}

func TestCatalogMediaImportCheckpointsAcrossPhasesAndRejectsRebinding(t *testing.T) {
	f, binding := mediaImportFixture(t, 51, nil)
	beginMediaImport(t, f, binding)
	first := advanceMediaImport(t, f, 0)
	require.Equal(t, "assets", first.Phase)
	require.EqualValues(t, 50, first.ProcessedRecords)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	changed := binding
	changed.LibraryRootPath = "/another-mount"
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.CatalogMediaImport.Begin(ctx, changed, catalogImportNow)
		return err
	})
	require.ErrorIs(t, err, models.ErrCatalogSnapshotConflict)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.CatalogMediaImport.Advance(ctx, f.manifest.UUID, f.sha, 0, catalogImportNow)
		return err
	})
	require.ErrorIs(t, err, models.ErrCatalogSnapshotConflict)
	second := advanceMediaImport(t, f, 50)
	require.Equal(t, "files", second.Phase)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	result := finishMediaImport(t, f, second)
	require.EqualValues(t, 155, result.ProcessedRecords)
	require.EqualValues(t, 100, result.UnavailableRecords)
	require.Equal(t, "complete", result.Phase)
	require.Equal(t, result, beginMediaImport(t, f, binding))
}

func TestCatalogMediaImportRetainsConflictWithoutChangingLibrary(t *testing.T) {
	for _, scenario := range []string{"size", "mtime", "path", "pending", "asset", "owner", "forgotten"} {
		t.Run(scenario, func(t *testing.T) {
			f, binding := mediaImportFixture(t, 1, func(rows []map[string]any) {
				for _, row := range rows {
					v := row["values"].(map[string]any)
					if row["table"] == "files" && v["relpath"] == "video.mp4" {
						switch scenario {
						case "size":
							v["byte_size"] = 999
						case "mtime":
							v["mtime_ns"] = 1
						case "path":
							v["relpath"] = "../video.mp4"
							row["key"] = []any{v["relpath"]}
						case "pending":
							v["state"] = "pending"
						case "asset":
							v["asset_id"] = nil
						}
					}
				}
			})
			if scenario == "owner" {
				attachmentSQL(t, f.db, `INSERT INTO scenes_files(scene_id,file_id,"primary") VALUES(32,21,1)`)
			}
			if scenario == "forgotten" {
				attachmentSQL(t, f.db, "UPDATE source_posts SET state='forgotten'")
			}
			result := finishMediaImport(t, f, beginMediaImport(t, f, binding))
			if scenario == "pending" {
				require.Equal(t, "mapped", result.State)
				require.EqualValues(t, 2, result.UnavailableRecords)
			} else {
				require.Equal(t, "review", result.State)
			}
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM files"))
			require.NoError(t, f.db.Close())
			require.NoError(t, f.db.Open(f.db.DatabasePath()))
		})
	}
}

func TestCatalogMediaImportRechecksFileGenerationBetweenPhases(t *testing.T) {
	f, binding := mediaImportFixture(t, 51, nil)
	beginMediaImport(t, f, binding)
	first := advanceMediaImport(t, f, 0)
	second := advanceMediaImport(t, f, first.ProcessedRecords)
	require.Equal(t, "files", second.Phase)
	require.EqualValues(t, 1, second.MatchedFiles, "the converted survivor was matched before the file changed")
	attachmentSQL(t, f.db, "UPDATE files SET size=size+1 WHERE id=21")
	result := finishMediaImport(t, f, second)
	require.Equal(t, "review", result.State)
	require.Zero(t, result.MediaAssociations, "the old file match cannot authorize a new post/media association")
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM catalog_media_records WHERE reason='matched_file_changed'"))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

func TestCatalogMediaImportRejectsStaleDefinitionBinding(t *testing.T) {
	f, binding := mediaImportFixture(t, 0, nil)
	for _, changed := range []models.CatalogMediaBinding{
		{SnapshotUUID: binding.SnapshotUUID, ManifestSHA256: binding.ManifestSHA256, RootUUID: uuid.NewString(), RootRevision: 1, CollectionRevision: binding.CollectionRevision, LibraryRootPath: binding.LibraryRootPath},
		{SnapshotUUID: binding.SnapshotUUID, ManifestSHA256: binding.ManifestSHA256, RootUUID: binding.RootUUID, RootRevision: 99, CollectionRevision: binding.CollectionRevision, LibraryRootPath: binding.LibraryRootPath},
	} {
		err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := f.repo.CatalogMediaImport.Begin(ctx, changed, catalogImportNow)
			return err
		})
		require.ErrorIs(t, err, models.ErrCatalogSnapshotConflict)
	}
	result := finishMediaImport(t, f, beginMediaImport(t, f, binding))
	require.Zero(t, result.ProcessedRecords)
	require.Equal(t, "mapped", result.State)
}

func TestCatalogMediaImportRollsBackDomainWritesWhenReceiptFails(t *testing.T) {
	f, binding := mediaImportFixture(t, 1, nil)
	beginMediaImport(t, f, binding)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec("CREATE TRIGGER fail_media_receipt BEFORE INSERT ON catalog_media_records BEGIN SELECT RAISE(ABORT,'fixture media receipt failure'); END")
	require.NoError(t, err)
	require.ErrorContains(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.CatalogMediaImport.Advance(ctx, f.manifest.UUID, f.sha, 0, catalogImportNow)
		require.ErrorContains(t, err, "fixture media receipt failure")
		return nil
	}), "did not finish atomically")
	for _, table := range []string{"catalog_media_records", "source_content_claims", "source_file_observations", "source_file_matches", "source_post_file_evidence"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
	_, err = raw.Exec("DROP TRIGGER fail_media_receipt")
	require.NoError(t, err)
	result := finishMediaImport(t, f, beginMediaImport(t, f, binding))
	require.Equal(t, "mapped", result.State)
}

func TestCatalogMediaImportIncludesArchiveIdentityForZipMembers(t *testing.T) {
	f, binding := mediaImportFixture(t, 1, func(rows []map[string]any) {
		for _, row := range rows {
			v := row["values"].(map[string]any)
			if row["table"] == "files" {
				v["relpath"] = "album.zip/" + v["relpath"].(string)
				row["key"] = []any{v["relpath"]}
				if v["survivor_relpath"] != nil {
					v["survivor_relpath"] = "album.zip/" + v["survivor_relpath"].(string)
				}
			}
			if row["table"] == "appearances" {
				v["source_relpath"] = "album.zip/" + v["source_relpath"].(string)
			}
		}
	})
	attachmentSQL(t, f.db, `INSERT INTO files(id,parent_folder_id,basename,size,mod_time,created_at,updated_at) VALUES(22,1,'album.zip',9999,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
 INSERT INTO folders(id,path,basename,zip_file_id,mod_time,created_at,updated_at) VALUES(2,'/identity-fixture/album.zip','album.zip',22,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
 UPDATE files SET parent_folder_id=2,zip_file_id=22 WHERE id=21;`)
	result := finishMediaImport(t, f, beginMediaImport(t, f, binding))
	require.Equal(t, "mapped", result.State)
	require.EqualValues(t, 2, result.MediaAssociations)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM source_file_observations WHERE archive_path='album.zip'"))
	require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM source_file_matches WHERE archive_file_uuid IS NOT NULL AND archive_generation=1"))
	attachmentSQL(t, f.db, "UPDATE files SET size=size+1 WHERE id=22")
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	require.Equal(t, result, advanceMediaImport(t, f, result.ProcessedRecords))
}

func TestCatalogMediaImportAuditRejectsCorruptReceiptsWithoutWriting(t *testing.T) {
	f, binding := mediaImportFixture(t, 1, nil)
	finishMediaImport(t, f, beginMediaImport(t, f, binding))
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, f.db.DatabasePath())
	var guard string
	require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='catalog_media_record_immutable'").Scan(&guard))
	_, err := raw.Exec(`DROP TRIGGER catalog_media_record_immutable;UPDATE catalog_media_records SET context_json='{"policy":"foreign"}'`)
	require.NoError(t, err)
	_, err = raw.Exec(guard)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	before, err := os.ReadFile(f.db.DatabasePath())
	require.NoError(t, err)
	require.ErrorContains(t, f.db.AuditForTesting(f.db.DatabasePath()), "invalid catalog media import")
	after, err := os.ReadFile(f.db.DatabasePath())
	require.NoError(t, err)
	require.Equal(t, before, after)
}
