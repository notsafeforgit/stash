package api

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/entityid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func libraryTestUUID(t *testing.T, repo models.Repository, kind models.ArchiveEntityKind, id int) string {
	t.Helper()
	var entity *models.ArchiveEntity
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		var err error
		entity, err = repo.ArchiveEntity.FindByLocalID(ctx, kind, id)
		return err
	}))
	require.NotNil(t, entity)
	return entity.UUID
}

func libraryTestLocal(t *testing.T, repo models.Repository, kind models.ArchiveEntityKind, id string) int {
	t.Helper()
	var entity *models.ArchiveEntity
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		entity, err = repo.ArchiveEntity.Resolve(ctx, id)
		return err
	}))
	require.NotNil(t, entity)
	require.Equal(t, kind, entity.Kind)
	require.NotNil(t, entity.LocalID)
	return *entity.LocalID
}

func TestLibraryUUIDGraphQLBoundary(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "library.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	ids := map[models.ArchiveEntityKind]int{}
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
		for _, kind := range []models.FilterMode{models.FilterModeScenes, models.FilterModeImages, models.FilterModePerformers, models.FilterModeStudios, models.FilterModeTags, models.FilterModeGroups, models.FilterModeGalleries} {
			id, _, err := createBulkCustomFieldFixture(ctx, db, kind, "UUID fixture")
			if err != nil {
				return err
			}
			ids[models.ArchiveEntityKind(libraryModeKind(string(kind)))] = id
		}
		return nil
	}))
	public := map[models.ArchiveEntityKind]string{}
	for kind, id := range ids {
		public[kind] = libraryTestUUID(t, repo, kind, id)
	}
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `INSERT INTO folders(id,path,basename,mod_time,created_at,updated_at) VALUES(901,'/uuid-fixture','uuid-fixture',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO files(id,parent_folder_id,basename,size,mod_time,created_at,updated_at) VALUES(902,901,'file.bin',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);`, nil)
		if err != nil {
			return err
		}
		_, _, err = db.ExecSQL(ctx, `INSERT INTO galleries_images(gallery_id,image_id) VALUES(?,?)`, []any{ids[models.ArchiveGallery], ids[models.ArchiveImage]})
		return err
	}))
	call := nativeResolverCaller(t, &Resolver{repository: repo, hookExecutor: &mockHookExecutor{}})
	for kind, id := range public {
		name := strings.ToUpper(string(kind[:1])) + string(kind[1:])
		query := `query($id: ID!) { item: find` + name + `(id: $id) { alias: id } }`
		result := call(query, map[string]any{"id": id})
		require.JSONEq(t, `{"data":{"item":{"alias":"`+id+`"}}}`, result, string(kind))
		for _, invalid := range []any{ids[kind], "1", "00000000-0000-0000-0000-000000000000"} {
			result = call(query, map[string]any{"id": invalid})
			require.Contains(t, result, `"errors"`, result)
		}
	}
	result := call(`query($id: ID!) { findPerformer(id: $id) { id } }`, map[string]any{"id": public[models.ArchiveScene]})
	require.Contains(t, result, "wrong kind")
	result = call(`query($ids: [ID!]) { findScenes(ids: $ids) { scenes { id } count } }`, map[string]any{"ids": []string{public[models.ArchiveScene]}})
	require.NotContains(t, result, `"errors"`, result)
	require.Contains(t, result, public[models.ArchiveScene])
	criterion := func(id string) map[string]any {
		return map[string]any{"root": map[string]any{"condition": map[string]any{"field": "performers", "value": map[string]any{"value": []string{id}, "modifier": "INCLUDES"}}}}
	}
	result = call(`query($input: FilterASTInput!) { findScenes(scene_filter_ast:$input) { count } }`, map[string]any{"input": criterion(public[models.ArchivePerformer])})
	require.NotContains(t, result, `"errors"`, result)
	result = call(`query($input: FilterASTInput!) { findScenes(scene_filter_ast:$input) { count } }`, map[string]any{"input": criterion(public[models.ArchiveTag])})
	require.Contains(t, result, "wrong kind")
	result = call(`mutation($input: SceneUpdateInput!) { sceneUpdate(input:$input) { id title } }`, map[string]any{"input": map[string]any{"id": public[models.ArchiveScene], "title": "Changed through UUID", "performer_ids": []string{public[models.ArchivePerformer]}}})
	var response map[string]any
	require.NoError(t, json.Unmarshal([]byte(result), &response))
	require.NotContains(t, response, "errors", result)
	require.Contains(t, result, public[models.ArchiveScene])
	// Other library records use the same typed identity boundary, including
	// references nested in input objects and interface-valued file results.
	folderID := libraryTestUUID(t, repo, models.ArchiveFolder, 901)
	fileID := libraryTestUUID(t, repo, models.ArchiveFile, 902)
	result = call(`query($id:ID!){ findFile(id:$id){ id parent_folder_id parent_folder { id } } }`, map[string]any{"id": fileID})
	require.JSONEq(t, `{"data":{"findFile":{"id":"`+fileID+`","parent_folder_id":"`+folderID+`","parent_folder":{"id":"`+folderID+`"}}}}`, result)
	result = call(`query($input:FileFilterType!){findFiles(file_filter:$input){count files{id}}}`, map[string]any{"input": map[string]any{"parent_folder": map[string]any{"value": []string{folderID}, "modifier": "INCLUDES"}}})
	require.NotContains(t, result, `"errors"`, result)
	require.Contains(t, result, fileID)
	marker := call(`mutation($scene:ID!,$tag:ID!){sceneMarkerCreate(input:{scene_id:$scene,primary_tag_id:$tag,title:"Marker",seconds:1}){id scene{id} primary_tag{id}}}`, map[string]any{"scene": public[models.ArchiveScene], "tag": public[models.ArchiveTag]})
	require.NotContains(t, marker, `"errors"`, marker)
	chapter := call(`mutation($gallery:ID!){galleryChapterCreate(input:{gallery_id:$gallery,title:"Chapter",image_index:1}){id gallery{id}}}`, map[string]any{"gallery": public[models.ArchiveGallery]})
	require.NotContains(t, chapter, `"errors"`, chapter)
	var created struct {
		Data struct {
			Chapter struct{ ID string } `json:"galleryChapterCreate"`
		}
	}
	require.NoError(t, json.Unmarshal([]byte(chapter), &created))
	result = call(`mutation($id:ID!){galleryChapterDestroy(id:$id)}`, map[string]any{"id": created.Data.Chapter.ID})
	require.JSONEq(t, `{"data":{"galleryChapterDestroy":true}}`, result)
	// Unknown UUIDs never widen a list query, and deleting/reusing the storage
	// key cannot make an old public ID refer to the replacement.
	oldImage := public[models.ArchiveImage]
	ctx := entityid.WithRepository(t.Context(), repo)
	require.NoError(t, repo.WithTxn(ctx, func(ctx context.Context) error {
		if err := repo.Image.Destroy(ctx, ids[models.ArchiveImage]); err != nil {
			return err
		}
		require.Equal(t, oldImage, entityid.Recorded(ctx, models.ArchiveImage, ids[models.ArchiveImage]))
		_, _, err := db.ExecSQL(ctx, `INSERT INTO images(id,title,created_at,updated_at) VALUES(?,'Replacement',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, []any{ids[models.ArchiveImage]})
		return err
	}))
	require.NotEqual(t, oldImage, libraryTestUUID(t, repo, models.ArchiveImage, ids[models.ArchiveImage]))
	result = call(`query($ids:[ID!]){findImages(ids:$ids){count images{id}}}`, map[string]any{"ids": []string{oldImage, "1e393a50-75cb-47c9-b419-af2cd99fbb7c"}})
	require.JSONEq(t, `{"data":{"findImages":{"count":0,"images":[]}}}`, result)
	result = call(`mutation($id:ID!){imageUpdate(input:{id:$id,title:"Must not update replacement"}){id}}`, map[string]any{"id": oldImage})
	require.Contains(t, result, `"errors"`)
}
