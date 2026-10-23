package sharing

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestLargeGalleryPreparationResumesFrozenMembershipAfterFailureAndRestart(t *testing.T) {
	f := newSourceFixture(t)
	f.exec(`WITH RECURSIVE ids(id) AS (SELECT 2 UNION ALL SELECT id+1 FROM ids WHERE id<2055)
INSERT INTO images(id,title,created_at,updated_at) SELECT id,'Original title',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP FROM ids;
INSERT INTO images_files(image_id,file_id,"primary") SELECT id,2,1 FROM images WHERE id>1;
INSERT INTO galleries_images(gallery_id,image_id) SELECT 1,id FROM images WHERE id>1;
DELETE FROM scenes_galleries;`)
	row, secret, err := f.service.Create(t.Context(), "owner", Options{Label: "Large gallery", ExpiresAt: time.Now().Add(time.Hour)}, []Target{{Kind: "GALLERY", ID: 1}})
	require.NoError(t, err)
	require.Empty(t, secret)
	require.Equal(t, 2055, row.MediaCount)
	require.Equal(t, "PREPARING", row.PreparationState)
	_, _, err = f.service.Exchange(t.Context(), row.ID, row.Token)
	require.ErrorIs(t, err, ErrUnavailable)
	_, err = f.service.AuthenticateLink(t.Context(), row.ID, row.Token)
	require.ErrorIs(t, err, ErrUnavailable)
	_, err = f.service.Rotate(t.Context(), row.ID)
	require.ErrorIs(t, err, ErrUnavailable)
	require.NoError(t, f.service.PrepareBatch(t.Context(), row))
	first := row
	row, err = f.service.Find(t.Context(), row.ID)
	require.NoError(t, err)
	require.Equal(t, PreparationBatchSize, row.PreparedCount)
	// A second process with the old revision cannot advance the checkpoint.
	require.NoError(t, f.service.PrepareBatch(t.Context(), first))
	unchanged, err := f.service.Find(t.Context(), row.ID)
	require.NoError(t, err)
	require.Equal(t, row.PreparationRevision, unchanged.PreparationRevision)
	var files []models.File
	require.NoError(t, f.service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		files, err = f.service.Repo.File.Find(ctx, 2)
		return err
	}))
	path := files[0].Base().Path
	require.NoError(t, os.Rename(path, path+".offline"))
	require.NoError(t, f.service.PrepareBatch(t.Context(), row))
	row, err = f.service.Find(t.Context(), row.ID)
	require.NoError(t, err)
	require.Equal(t, "FAILED", row.PreparationState)
	require.Equal(t, PreparationBatchSize, row.PreparedCount)
	require.NotEmpty(t, row.PreparationError)
	require.Empty(t, SavedLinkSecret(row))
	f.exec(`DELETE FROM galleries_images WHERE image_id>100;
UPDATE images SET title='Changed later' WHERE id>1;
INSERT INTO images(id,title,created_at,updated_at) VALUES(2056,'Added later',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
INSERT INTO images_files(image_id,file_id,"primary") VALUES(2056,2,1);
INSERT INTO galleries_images(gallery_id,image_id) VALUES(1,2056);`)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	f.service = New(f.db.Repository())
	row, err = f.service.Find(t.Context(), row.ID)
	require.NoError(t, err)
	require.Equal(t, "FAILED", row.PreparationState)
	require.NoError(t, os.Rename(path+".offline", path))
	row, err = f.service.Retry(t.Context(), row.ID)
	require.NoError(t, err)
	require.Equal(t, PreparationBatchSize, row.PreparedCount)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- f.service.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	require.Eventually(t, func() bool {
		row, err = f.service.Find(t.Context(), row.ID)
		require.NoError(t, err)
		return row.PreparationState != "PREPARING"
	}, 20*time.Second, 20*time.Millisecond)
	require.Equal(t, "READY", row.PreparationState, row.PreparationError)
	require.Equal(t, 2055, row.PreparedCount)
	require.NotEmpty(t, SavedLinkSecret(row))
	gallery := "gallery-" + f.identity("GALLERY", 1)
	for _, scope := range []string{"", gallery} {
		keys := map[string]bool{}
		cursor := ""
		pages := 0
		for {
			page, next, err := f.service.Content(t.Context(), row, cursor, scope)
			require.NoError(t, err)
			require.LessOrEqual(t, len(page.Media), PageSize+1)
			for _, item := range page.Media {
				keys[item.Key] = true
				if item.EntityID > 1 {
					require.Equal(t, "Original title", item.Title)
				}
			}
			if scope != "" {
				require.Len(t, page.Entries, 1)
				require.Equal(t, 2055, page.Entries[0].MediaCount)
				require.LessOrEqual(t, len(page.Entries[0].MediaKeys), PageSize)
			}
			pages++
			if next == "" {
				break
			}
			cursor = next
			require.Less(t, pages, 100)
		}
		require.Len(t, keys, 2055)
	}
	_, err = f.item(row, "image-"+f.identity("IMAGE", 2056))
	require.ErrorIs(t, err, ErrUnavailable)
	last := "image-" + f.identity("IMAGE", 2055)
	page, _, err := f.service.Content(t.Context(), row, "", gallery, last)
	require.NoError(t, err)
	require.Contains(t, page.Entries[0].MediaKeys, last, "deep links can open an item beyond the first page")
	_, cursor, err := f.service.Content(t.Context(), row, "", gallery)
	require.NoError(t, err)
	_, _, err = f.service.Content(t.Context(), row, cursor, "")
	require.ErrorContains(t, err, "cursor")
	f.exec(`DELETE FROM images WHERE id=2055`)
	_, err = f.item(row, last)
	require.ErrorIs(t, err, ErrUnavailable)
	f.exec(`DELETE FROM galleries WHERE id=1`)
	_, err = f.item(row, "image-"+f.identity("IMAGE", 1))
	require.ErrorIs(t, err, ErrUnavailable)
}

func TestPreparationRevocationAndFailedGrantDoNotBlockOtherShares(t *testing.T) {
	f := newSourceFixture(t)
	f.exec(`WITH RECURSIVE ids(id) AS (SELECT 2 UNION ALL SELECT id+1 FROM ids WHERE id<60)
INSERT INTO scenes(id,title,created_at,updated_at) SELECT id,'Included',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP FROM ids;
INSERT INTO scenes_files(scene_id,file_id,"primary") SELECT id,1,1 FROM scenes WHERE id>1;`)
	row := f.create(&models.ShareSource{Mode: "SNAPSHOT", Filter: &models.ShareFilter{Kind: "SCENE"}})
	require.NoError(t, f.service.Revoke(t.Context(), row.ID))
	require.NoError(t, f.service.PrepareBatch(t.Context(), row))
	saved, err := f.service.Find(t.Context(), row.ID)
	require.NoError(t, err)
	require.Zero(t, saved.PreparedCount)
	require.Empty(t, saved.Token)
	require.NoError(t, f.service.Delete(t.Context(), row.ID), "grant deletion cascades immutable membership")
	f.exec(`DELETE FROM images_files WHERE image_id=1`)
	bad, _, err := f.service.Create(t.Context(), "owner", Options{Label: "Missing file", ExpiresAt: time.Now().Add(time.Hour)}, []Target{{Kind: "IMAGE", ID: 1}})
	require.NoError(t, err)
	require.Equal(t, "FAILED", bad.PreparationState)
	good := f.create(&models.ShareSource{Mode: "SNAPSHOT", Filter: &models.ShareFilter{Kind: "SCENE"}})
	var next *models.ShareRecord
	require.NoError(t, f.service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		next, err = f.service.Repo.Share.NextPreparation(ctx, time.Now().Unix())
		return err
	}))
	require.Equal(t, good.ID, next.ID)
	good.ExpiresAt = time.Now().Unix() - 1
	require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error { return f.service.Repo.Share.Update(ctx, good) }))
	require.NoError(t, f.service.PrepareBatch(t.Context(), next))
	saved, err = f.service.Find(t.Context(), good.ID)
	require.NoError(t, err)
	require.Zero(t, saved.PreparedCount)
}

func TestSnapshotPagesKeepEveryMediaTypeReachable(t *testing.T) {
	f := newSourceFixture(t)
	f.exec(`WITH RECURSIVE ids(id) AS (SELECT 2 UNION ALL SELECT id+1 FROM ids WHERE id<110)
INSERT INTO scenes(id,title,created_at,updated_at) SELECT id,'Video',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP FROM ids;
INSERT INTO scenes_files(scene_id,file_id,"primary") SELECT id,1,1 FROM scenes WHERE id>1;`)
	targets := []Target{}
	for id := 1; id <= 110; id++ {
		targets = append(targets, Target{Kind: "SCENE", ID: id})
	}
	targets = append(targets, Target{Kind: "IMAGE", ID: 1}, Target{Kind: "GALLERY", ID: 1})
	row, _, err := f.service.Create(t.Context(), "owner", Options{Label: "Mixed types", ExpiresAt: time.Now().Add(time.Hour)}, targets)
	require.NoError(t, err)
	for row.PreparationState == "PREPARING" {
		require.NoError(t, f.service.PrepareBatch(t.Context(), row))
		row, err = f.service.Find(t.Context(), row.ID)
		require.NoError(t, err)
	}
	require.Equal(t, "READY", row.PreparationState, row.PreparationError)
	page, cursor, err := f.service.Content(t.Context(), row, "")
	require.NoError(t, err)
	require.NotEmpty(t, cursor)
	images, galleries := 0, 0
	for _, item := range page.Media {
		if item.Kind == "IMAGE" {
			images++
		}
	}
	for _, entry := range page.Entries {
		if entry.Kind == "GALLERY" {
			galleries++
		}
	}
	require.Equal(t, 1, images, "images must not wait behind all the videos")
	require.Equal(t, 1, galleries, "galleries must appear on the first page")
	page, _, err = f.service.Content(t.Context(), row, cursor)
	require.NoError(t, err)
	for _, item := range page.Media {
		require.Equal(t, "SCENE", item.Kind, "finished type cursors must not restart")
	}
	page, _, err = f.service.Content(t.Context(), row, "", "gallery-missing")
	require.NoError(t, err)
	require.Empty(t, page.Media)
	require.Empty(t, page.Entries)
}
