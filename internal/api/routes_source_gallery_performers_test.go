package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestSourceGalleryPerformerReconciliationHTTP(t *testing.T) {
	db, repo, _ := sourcePostBrowserHTTPFixture(t)
	image := models.NewImage()
	performer := models.NewPerformer()
	performer.Name = "Profile performer"
	g := models.NewGallery()
	g.Origin, g.Title = models.GalleryOriginSource, "Older album"
	var galleryUUID string
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		require.NoError(t, repo.Performer.Create(ctx, &models.CreatePerformerInput{Performer: &performer}))
		require.NoError(t, repo.Image.Create(ctx, &models.CreateImageInput{Image: &image}))
		require.NoError(t, repo.Gallery.Create(ctx, &models.CreateGalleryInput{Gallery: &g}))
		require.NoError(t, repo.Gallery.AddImages(ctx, g.ID, image.ID))
		e, err := repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveGallery, g.ID)
		require.NoError(t, err)
		galleryUUID = e.UUID
		_, _, err = db.ExecSQL(ctx, `INSERT INTO performers_images(image_id,performer_id) VALUES(?,?);
DELETE FROM source_gallery_performer_pending`, []interface{}{image.ID, performer.ID})
		return err
	}))
	notified := 0
	routes := &nativeArchiveRoutes{repo: repo, notifyMetadata: func(ctx context.Context, input metadata.Input, fields []string) error {
		require.Equal(t, galleryUUID, input.EntityUUID)
		require.Equal(t, []string{"performer_ids"}, fields)
		notified++
		return nil
	}}
	const path = "/source-galleries/performers/reconcile"
	post := func(handler http.Handler, ids []string, status int) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(map[string]any{"gallery_uuids": ids})
		require.NoError(t, err)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(w, r)
		require.Equal(t, status, w.Code, w.Body.String())
		return w
	}
	for _, ids := range [][]string{nil, {galleryUUID, galleryUUID}, {"invalid"}, make([]string, 101)} {
		post(routes.router(), ids, http.StatusBadRequest)
	}
	require.Zero(t, notified)
	require.Contains(t, post(routes.router(), []string{galleryUUID}, http.StatusOK).Body.String(), `"status":"updated"`)
	require.Equal(t, 1, notified)
	require.Contains(t, post(routes.router(), []string{galleryUUID}, http.StatusOK).Body.String(), `"status":"unchanged"`)
	require.Equal(t, 1, notified)
	post(routes.routerWithSourceTracking(false), []string{galleryUUID}, http.StatusServiceUnavailable)
}
