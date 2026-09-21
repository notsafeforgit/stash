package sqlite

import (
	"context"
	"fmt"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestMediaSearchPreservesJoinedRowSemantics(t *testing.T) {
	db, err := sqlx.Open(sqlite3Driver, ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = db.Exec(`
CREATE TABLE scenes (id INTEGER PRIMARY KEY, title TEXT, details TEXT);
CREATE TABLE images (id INTEGER PRIMARY KEY, title TEXT, details TEXT);
CREATE TABLE folders (id INTEGER PRIMARY KEY, path TEXT);
CREATE TABLE files (id INTEGER PRIMARY KEY, parent_folder_id INTEGER, basename TEXT, size INTEGER);
CREATE TABLE video_files (file_id INTEGER, duration REAL);
CREATE TABLE image_files (file_id INTEGER, width INTEGER, height INTEGER);
CREATE TABLE scenes_files (scene_id INTEGER, file_id INTEGER);
CREATE TABLE images_files (image_id INTEGER, file_id INTEGER);
CREATE TABLE files_fingerprints (file_id INTEGER, fingerprint TEXT);
CREATE TABLE scene_markers (scene_id INTEGER, title TEXT);
INSERT INTO scenes VALUES (1, 'alpha beta', NULL), (2, '', 'detail'), (3, NULL, NULL), (4, '中文 café', NULL), (5, 'alpha', NULL);
INSERT INTO images SELECT * FROM scenes;
INSERT INTO folders VALUES (1, '/archive/2024'), (2, '/other');
INSERT INTO files VALUES (1, 1, 'alpha.mp4', 10), (2, 2, 'beta.mp4', 20), (3, 2, 'gamma.mp4', 30), (4, 2, 'alpha beta.mp4', 40);
INSERT INTO video_files VALUES (1, 1), (2, 2), (3, 3), (4, 4);
INSERT INTO image_files VALUES (1, 1000, 1000), (2, 2000, 1000), (3, 3000, 1000), (4, 4000, 1000);
INSERT INTO scenes_files VALUES (2, 1), (2, 2), (3, 4), (5, 3);
INSERT INTO images_files SELECT * FROM scenes_files;
INSERT INTO files_fingerprints VALUES (1, 'hash-alpha'), (1, 'hash-other'), (2, 'hash-beta'), (3, 'hash-gamma');
INSERT INTO scene_markers VALUES (2, 'marker-alpha'), (2, 'marker-beta'), (5, 'marker-only');
`)
	require.NoError(t, err)
	tx, err := db.Beginx()
	require.NoError(t, err)
	defer func() { require.NoError(t, tx.Rollback()) }()
	ctx := context.WithValue(context.Background(), txnKey, tx)
	for _, table := range []string{sceneTable, imageTable} {
		for _, search := range []string{"alpha", "2024", "detail", "hash-beta", "marker-only", "alpha beta", `"alpha beta"`, "alpha OR beta", "alpha -beta", "-alpha", "hash-alpha hash-other", "marker-alpha marker-beta", "2024 beta", "α", "中文", "café", "%", "_", "alpha OR 2024 -gamma", "unmatched"} {
			t.Run(table+"/"+search, func(t *testing.T) {
				sort := "id"
				all := -1
				find := &models.FindFilterType{Q: &search, Sort: &sort, PerPage: &all}
				repository, id := sceneRepository.repository, sceneIDColumn
				if table == imageTable {
					repository, id = imageRepository.repository, imageIDColumn
				}
				legacy := repository.newQuery()
				distinctIDs(&legacy, table)
				legacy.join(table+"_files", "", table+"_files."+id+" = "+table+".id")
				legacy.join("files", "", table+"_files.file_id = files.id")
				legacy.join("folders", "", "files.parent_folder_id = folders.id")
				legacy.join("files_fingerprints", "", "files_fingerprints.file_id = "+table+"_files.file_id")
				columns := []string{table + ".title", table + ".details", "folders.path || '/' || files.basename", "files_fingerprints.fingerprint"}
				if table == sceneTable {
					legacy.join("scene_markers", "", "scene_markers.scene_id = scenes.id")
					columns = append(columns, "scene_markers.title")
				}
				legacy.parseQueryString(columns, search)
				legacy.sortAndPagination = " ORDER BY " + table + ".id ASC"
				wantIDs, err := legacy.findIDs(ctx)
				require.NoError(t, err)
				// ID 2 has separate alpha/beta files and fingerprints. The
				// candidate set must not turn cross-row matches into results,
				// or include nonmatching files in the requested totals.
				var astQuery *queryBuilder
				var ast *models.FilterAST
				if table == sceneTable {
					store := &SceneStore{}
					opts := models.SceneQueryOptions{QueryOptions: models.QueryOptions{FindFilter: find, Count: true}, TotalDuration: true, TotalSize: true}
					want, err := store.queryGroupedFields(ctx, opts, legacy)
					require.NoError(t, err)
					got, err := store.Query(ctx, opts)
					require.NoError(t, err)
					require.Equal(t, wantIDs, got.IDs)
					require.Equal(t, want.Count, got.Count)
					require.Equal(t, want.TotalDuration, got.TotalDuration)
					require.Equal(t, want.TotalSize, got.TotalSize)
					astQuery, err = store.makeASTQuery(ctx, ast, find)
					require.NoError(t, err)
				} else {
					store := &ImageStore{}
					opts := models.ImageQueryOptions{QueryOptions: models.QueryOptions{FindFilter: find, Count: true}, Megapixels: true, TotalSize: true}
					want, err := store.queryGroupedFields(ctx, opts, legacy)
					require.NoError(t, err)
					got, err := store.Query(ctx, opts)
					require.NoError(t, err)
					require.Equal(t, wantIDs, got.IDs)
					require.Equal(t, want.Count, got.Count)
					require.Equal(t, want.Megapixels, got.Megapixels)
					require.Equal(t, want.TotalSize, got.TotalSize)
					astQuery, err = store.makeASTQuery(ctx, ast, find)
					require.NoError(t, err)
				}
				gotIDs, count, err := astQuery.executeFind(ctx)
				require.NoError(t, err)
				require.Equal(t, wantIDs, gotIDs)
				require.Equal(t, len(wantIDs), count)
			})
		}
	}

	// Broad terms must retain every match beyond the candidate budget.
	_, err = tx.Exec(fmt.Sprintf(`WITH RECURSIVE n(id) AS (SELECT 100 UNION ALL SELECT id+1 FROM n WHERE id < %d) INSERT INTO scenes SELECT id, 'common', '' FROM n`, mediaSearchCandidateLimit+200))
	require.NoError(t, err)
	search := "common"
	query := sceneRepository.newQuery()
	require.NoError(t, query.prefilterMediaSearch(ctx, sceneTable, &models.FindFilterType{Q: &search}, nil))
	require.Empty(t, query.whereClauses)
	// Restricted lists keep their existing query plan without a global probe.
	search = "alpha"
	filter := &filterBuilder{whereClauses: []sqlClause{makeClause("id = ?", 2)}}
	require.NoError(t, query.prefilterMediaSearch(ctx, sceneTable, &models.FindFilterType{Q: &search}, filter))
	require.Empty(t, query.whereClauses)
	require.NoError(t, query.prefilterMediaSearch(context.Background(), sceneTable, &models.FindFilterType{Q: &search}, nil))
	require.Empty(t, query.whereClauses)
}
