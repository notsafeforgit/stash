package sharing

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

type sourceFixture struct {
	service *Service
	db      *sqlite.Database
	t       *testing.T
}

func newSourceFixture(t *testing.T) sourceFixture {
	t.Helper()
	db := testShareDatabase(t)
	f := sourceFixture{service: New(db.Repository()), db: db, t: t}
	require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return f.service.Repo.Performer.Create(ctx, &models.CreatePerformerInput{Performer: &models.Performer{Name: "Performer", CreatedAt: time.Now(), UpdatedAt: time.Now()}})
	}))
	dir := t.TempDir()
	for _, name := range []string{"clip.mp4", "photo.jpg"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("media"), 0o600))
	}
	f.exec(`INSERT INTO folders(id,path,basename,mod_time,created_at,updated_at) VALUES(1,?,'media',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`, dir)
	f.exec(`INSERT INTO files(id,parent_folder_id,basename,size,mod_time,created_at,updated_at) VALUES
(1,1,'clip.mp4',5,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP),
(2,1,'photo.jpg',5,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO video_files(file_id,duration,video_codec,format,audio_codec,width,height,frame_rate,bit_rate) VALUES(1,1,'h264','mp4','aac',64,48,30,100);
INSERT INTO image_files(file_id,format,width,height) VALUES(2,'jpeg',64,48);
INSERT INTO studios(id,name,created_at,updated_at) VALUES(1,'Studio',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO tags(id,name,created_at,updated_at) VALUES(1,'Tag',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO scenes(id,title,studio_id,created_at,updated_at) VALUES(1,'Included scene',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO images(id,title,studio_id,created_at,updated_at) VALUES(1,'Included image',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO galleries(id,title,studio_id,created_at,updated_at) VALUES(1,'Included gallery',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO scenes_files(scene_id,file_id,"primary") VALUES(1,1,1);
INSERT INTO images_files(image_id,file_id,"primary") VALUES(1,2,1);
INSERT INTO performers_scenes(performer_id,scene_id) VALUES(1,1);
INSERT INTO performers_images(performer_id,image_id) VALUES(1,1);
INSERT INTO performers_galleries(performer_id,gallery_id) VALUES(1,1);
INSERT INTO scenes_tags(scene_id,tag_id) VALUES(1,1);
INSERT INTO images_tags(image_id,tag_id) VALUES(1,1);
INSERT INTO galleries_tags(gallery_id,tag_id) VALUES(1,1);
INSERT INTO galleries_images(gallery_id,image_id) VALUES(1,1);
INSERT INTO scenes_galleries(scene_id,gallery_id) VALUES(1,1);`)
	return f
}

func (f sourceFixture) exec(sql string, args ...any) {
	f.t.Helper()
	require.NoError(f.t, f.service.Repo.WithTxn(f.t.Context(), func(ctx context.Context) error { _, _, err := f.db.ExecSQL(ctx, sql, args); return err }))
}

func (f sourceFixture) identity(kind string, id int) string {
	f.t.Helper()
	var identity *models.ArchiveEntity
	require.NoError(f.t, f.service.Repo.WithReadTxn(f.t.Context(), func(ctx context.Context) error {
		var err error
		identity, err = f.service.Repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveEntityKind(strings.ToLower(kind)), id)
		return err
	}))
	require.NotNil(f.t, identity)
	return identity.UUID
}

func (f sourceFixture) create(source *models.ShareSource) *models.ShareRecord {
	f.t.Helper()
	row, _, err := f.service.CreateFromSource(f.t.Context(), "owner", Options{Label: "Shared set", ExpiresAt: time.Now().Add(time.Hour)}, nil, source)
	require.NoError(f.t, err)
	return row
}

func (f sourceFixture) item(row *models.ShareRecord, key string) (*models.ShareMedia, error) {
	item, file, _, err := f.service.Item(f.t.Context(), row, key)
	if err == nil {
		require.NotNil(f.t, item)
		require.NotNil(f.t, file)
	}
	return item, err
}

func TestShareSourceSnapshotAndOngoingMembership(t *testing.T) {
	f := newSourceFixture(t)
	filter := &models.ShareFilter{Kind: "SCENE", Q: "Included"}
	snapshot := f.create(&models.ShareSource{Mode: "SNAPSHOT", Filter: filter})
	ongoing := f.create(&models.ShareSource{Mode: "ONGOING", Filter: filter})
	originalUUID := f.identity("SCENE", 1)
	f.exec(`INSERT INTO scenes(id,title,created_at,updated_at) VALUES(2,'Included later',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO scenes_files(scene_id,file_id,"primary") VALUES(2,1,1);`)
	fixed, _, err := f.service.Content(t.Context(), snapshot, "")
	require.NoError(t, err)
	require.Len(t, fixed.Media, 1)
	live, _, err := f.service.Content(t.Context(), ongoing, "")
	require.NoError(t, err)
	require.Len(t, live.Media, 2)
	f.exec(`UPDATE scenes SET title='No longer matching' WHERE id=1`)
	_, err = f.item(ongoing, "scene-"+originalUUID)
	require.ErrorIs(t, err, ErrUnavailable, "every delivery rechecks the filter")
	_, err = f.item(snapshot, "scene-"+originalUUID)
	require.NoError(t, err, "snapshot membership survives metadata changes")
	f.exec(`DELETE FROM scenes WHERE id=1;
INSERT INTO scenes(id,title,created_at,updated_at) VALUES(1,'Included replacement',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO scenes_files(scene_id,file_id,"primary") VALUES(1,1,1);`)
	fixed, _, err = f.service.Content(t.Context(), snapshot, "")
	require.NoError(t, err)
	require.Empty(t, fixed.Media, "reusing both local ID and file cannot revive a deleted selection")
	_, err = f.item(ongoing, "scene-"+originalUUID)
	require.ErrorIs(t, err, ErrUnavailable)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	f.service = New(f.db.Repository())
	reopened, err := f.service.Find(t.Context(), ongoing.ID)
	require.NoError(t, err)
	live, _, err = f.service.Content(t.Context(), reopened, "")
	require.NoError(t, err)
	require.Len(t, live.Media, 2)
}

func TestShareEntitySourcesAndGalleryMembership(t *testing.T) {
	for _, kind := range []string{"PERFORMER", "STUDIO", "TAG"} {
		t.Run(kind, func(t *testing.T) {
			f := newSourceFixture(t)
			source := &models.ShareSource{Mode: "ONGOING", Entity: &models.ShareEntitySource{Kind: kind, UUID: f.identity(kind, 1)}, MediaTypes: []string{"SCENE", "IMAGE", "GALLERY"}}
			row := f.create(source)
			content, _, err := f.service.Content(t.Context(), row, "")
			require.NoError(t, err)
			require.Len(t, content.Entries, 3)
			require.Len(t, content.Media, 2, "overlapping gallery media is deduplicated")
			require.Len(t, content.Entries[2].MediaKeys, 2, "galleries include videos and images")
			source.MediaTypes = []string{"GALLERY"}
			galleryOnly := f.create(source)
			source.Mode = "SNAPSHOT"
			frozenGallery := f.create(source)
			imageKey := "image-" + f.identity("IMAGE", 1)
			_, err = f.item(galleryOnly, imageKey)
			require.NoError(t, err)
			f.exec(`DELETE FROM galleries_images WHERE gallery_id=1`)
			_, err = f.item(galleryOnly, imageKey)
			require.ErrorIs(t, err, ErrUnavailable)
			_, err = f.item(frozenGallery, imageKey)
			require.NoError(t, err, "snapshot keeps original gallery membership")
			f.exec(`DELETE FROM galleries WHERE id=1`)
			content, _, err = f.service.Content(t.Context(), frozenGallery, "")
			require.NoError(t, err)
			require.Empty(t, content.Media)
			_, err = f.item(frozenGallery, imageKey)
			require.ErrorIs(t, err, ErrUnavailable, "deleting a gallery revokes its otherwise-unselected media")
			content, _, err = f.service.Content(t.Context(), row, "")
			require.NoError(t, err)
			require.Len(t, content.Media, 2, "independently matching media remains shared")
		})
	}
}

func TestShareSourceUUIDFilterScopeAndValidation(t *testing.T) {
	f := newSourceFixture(t)
	uuid := f.identity("PERFORMER", 1)
	ast := &models.FilterAST{Root: &models.FilterASTNode{Condition: &models.FilterASTCondition{Field: "performers", Value: map[string]any{"value": []any{uuid}, "modifier": "INCLUDES"}}}}
	row := f.create(&models.ShareSource{Mode: "ONGOING", Filter: &models.ShareFilter{Kind: "SCENE", AST: ast}})
	require.Contains(t, string(row.SourceJSON), uuid)
	f.exec(`DELETE FROM performers WHERE id=1; INSERT INTO performers(id,created_at,updated_at) VALUES(1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO performers_scenes(performer_id,scene_id) VALUES(1,1);`)
	content, _, err := f.service.Content(t.Context(), row, "")
	require.NoError(t, err)
	require.Empty(t, content.Media)
	_, err = f.item(row, "scene-"+f.identity("SCENE", 1))
	require.ErrorIs(t, err, ErrUnavailable)
	for _, source := range []*models.ShareSource{
		{Mode: "ONGOING"},
		{Mode: "ONGOING", Filter: &models.ShareFilter{Kind: "PERFORMER"}},
		{Mode: "ONGOING", Filter: &models.ShareFilter{Kind: "SCENE", AST: &models.FilterAST{Root: &models.FilterASTNode{Condition: &models.FilterASTCondition{Field: "not_a_field", Value: true}}}}},
		{Mode: "ONGOING", Entity: &models.ShareEntitySource{Kind: "TAG", UUID: f.identity("TAG", 1)}},
	} {
		_, _, err := f.service.CreateFromSource(t.Context(), "owner", Options{Label: "Invalid", ExpiresAt: time.Now().Add(time.Hour)}, nil, source)
		require.Error(t, err)
	}
	require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := f.db.ExecSQL(ctx, `UPDATE shares SET source='{}' WHERE id=?`, []any{row.ID})
		require.ErrorContains(t, err, "immutable")
		return nil
	}))
}

func TestShareSourcePaginationAndLargeSnapshot(t *testing.T) {
	f := newSourceFixture(t)
	f.exec(`WITH RECURSIVE ids(id) AS (SELECT 2 UNION ALL SELECT id+1 FROM ids WHERE id<2051)
INSERT INTO scenes(id,title,created_at,updated_at) SELECT id,'Included scene',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP FROM ids;
INSERT INTO scenes_files(scene_id,file_id,"primary") SELECT id,1,1 FROM scenes WHERE id>1;`)
	filter := &models.ShareFilter{Kind: "SCENE", Q: "Included"}
	fixed := f.create(&models.ShareSource{Mode: "SNAPSHOT", Filter: filter})
	require.Equal(t, "PREPARING", fixed.PreparationState)
	require.Equal(t, 2051, fixed.MediaCount)
	require.Empty(t, SavedLinkSecret(fixed))
	_, _, err := f.service.Content(t.Context(), fixed, "")
	require.ErrorIs(t, err, ErrUnavailable)
	for fixed.PreparationState == "PREPARING" {
		require.NoError(t, f.service.PrepareBatch(t.Context(), fixed))
		fixed, err = f.service.Find(t.Context(), fixed.ID)
		require.NoError(t, err)
	}
	require.Equal(t, "READY", fixed.PreparationState, fixed.PreparationError)
	require.NotEmpty(t, SavedLinkSecret(fixed))
	row := f.create(&models.ShareSource{Mode: "ONGOING", Filter: filter})
	keys := map[string]bool{}
	cursor := ""
	for {
		page, next, err := f.service.Content(t.Context(), row, cursor)
		require.NoError(t, err)
		require.LessOrEqual(t, len(page.Media), PageSize)
		for _, media := range page.Media {
			require.False(t, keys[media.Key])
			keys[media.Key] = true
		}
		if next == "" {
			break
		}
		cursor = next
	}
	require.Len(t, keys, 2051)
	key := "scene-" + f.identity("SCENE", 2051)
	focused, _, err := f.service.Content(t.Context(), row, "", key)
	require.NoError(t, err)
	require.Len(t, focused.Media, PageSize+1)
	require.Equal(t, key, focused.Media[len(focused.Media)-1].Key)
	unrelated := "image-" + f.identity("IMAGE", 1)
	focused, _, err = f.service.Content(t.Context(), row, "", unrelated)
	require.NoError(t, err)
	require.Len(t, focused.Media, PageSize, "focus cannot widen the share")
	_, _, err = f.service.Content(t.Context(), row, "not-a-cursor")
	require.Error(t, err)
	stored, err := Source(row)
	require.NoError(t, err)
	data, err := json.Marshal(stored)
	require.NoError(t, err)
	require.NotContains(t, string(data), "media_keys")
}
