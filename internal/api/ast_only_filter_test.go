package api

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestStorageASTFiltersAndRetiredContracts(t *testing.T) {
	config.InitializeEmpty()
	t.Cleanup(func() { config.InitializeEmpty() })
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "filters.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `INSERT INTO folders(id,path,basename,mod_time,created_at,updated_at) VALUES
  (11,'/first','first',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),
  (12,'/second','second',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
  INSERT INTO files(id,parent_folder_id,basename,size,mod_time,created_at,updated_at) VALUES
  (21,11,'alpha.jpg',100,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),
  (22,12,'beta.mp4',200,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
  INSERT INTO images(id,title,created_at,updated_at) VALUES(31,'Linked image',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
  INSERT INTO images_files(image_id,file_id,"primary") VALUES(31,21,1);`, nil)
		return err
	}))
	call := nativeResolverCaller(t, &Resolver{repository: repo})
	condition := func(field string, value any) map[string]any {
		return map[string]any{"condition": map[string]any{"field": field, "value": value}}
	}
	criterion := func(value any, modifier string) map[string]any {
		return map[string]any{"value": value, "modifier": modifier}
	}
	group := func(operator string, children ...map[string]any) map[string]any {
		return map[string]any{"group": map[string]any{"operator": operator, "children": children}}
	}
	query := func(kind string, root map[string]any) string {
		name := "findFiles"
		if kind == "folder" {
			name = "findFolders"
		}
		return call(`query($ast:FilterASTInput!){`+name+`(`+kind+`_filter_ast:$ast,filter:{per_page:1,sort:"path",direction:ASC}){count}}`, map[string]any{"ast": map[string]any{"root": root}})
	}
	// Count predicates and text predicates must remain independent OR branches.
	require.JSONEq(t, `{"data":{"findFiles":{"count":2}}}`, query("file", group("OR", condition("image_count", criterion(0, "GREATER_THAN")), condition("basename", criterion("beta.mp4", "EQUALS")))))
	require.JSONEq(t, `{"data":{"findFiles":{"count":1}}}`, query("file", group("AND", condition("image_count", criterion(0, "GREATER_THAN")), condition("basename", criterion("alpha.jpg", "EQUALS")))))
	folderID := libraryTestUUID(t, repo, models.ArchiveFolder, 11)
	require.JSONEq(t, `{"data":{"findFiles":{"count":1}}}`, query("file", condition("parent_folder", criterion([]string{folderID}, "INCLUDES"))))
	require.Contains(t, query("file", condition("parent_folder", criterion([]string{"11"}, "INCLUDES"))), `"errors"`)
	require.JSONEq(t, `{"data":{"findFolders":{"count":2}}}`, query("folder", condition("parent_folder", map[string]any{"modifier": "IS_NULL"})))
	nested := map[string]any{"root": condition("basename", criterion("alpha.jpg", "EQUALS"))}
	require.JSONEq(t, `{"data":{"findFolders":{"count":1}}}`, query("folder", condition("files", nested)))
	imageID := libraryTestUUID(t, repo, models.ArchiveImage, 31)
	require.JSONEq(t, `{"data":{"findFiles":{"count":1}}}`, query("file", condition("images", map[string]any{"root": condition("title", criterion("Linked image", "EQUALS"))})))
	require.Contains(t, query("file", condition("images", map[string]any{"root": condition("tags", criterion([]string{imageID}, "INCLUDES"))})), "wrong kind")
	for _, kind := range []string{"file", "folder"} {
		for _, root := range []map[string]any{
			condition("AND", map[string]any{"path": criterion("x", "EQUALS")}),
			condition("path", map[string]any{"modifier": "EQUALS", "value": "x", "OR": map[string]any{}}),
			condition("path", nil),
		} {
			require.Contains(t, query(kind, root), `"errors"`)
		}
	}
	for _, raw := range []string{
		`query{findFiles(file_filter:{path:{value:"alpha",modifier:INCLUDES}}){count}}`,
		`query{findFolders(folder_filter:{parent_folder:{modifier:IS_NULL}}){count}}`,
		`query{findScenes(scene_filter:{organized:true}){count}}`,
		`query{findImages(image_filter:{organized:true}){count}}`,
	} {
		require.Contains(t, call(raw, nil), `"errors"`)
	}
	schema := newLibrarySchema(Config{Resolvers: &Resolver{repository: repo}}).Schema()
	for _, kind := range []string{"Scene", "Image", "Performer", "Studio", "Group", "Gallery", "Tag", "SceneMarker", "File", "Folder"} {
		require.Nil(t, schema.Types[kind+"FilterType"])
	}
}
