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

func TestGalleryConsolidationHTTPPreviewApplyAndAlias(t *testing.T) {
	_, repo, get := sourcePostBrowserHTTPFixture(t)
	var ids []string
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		image := models.NewImage()
		if err := repo.Image.Create(ctx, &models.CreateImageInput{Image: &image}); err != nil {
			return err
		}
		for _, title := range []string{"First post", "Second post"} {
			g := models.NewGallery()
			g.Title = title
			g.Origin = models.GalleryOriginSource
			if err := repo.Gallery.Create(ctx, &models.CreateGalleryInput{Gallery: &g}); err != nil {
				return err
			}
			if err := repo.Gallery.AddImages(ctx, g.ID, image.ID); err != nil {
				return err
			}
			e, err := repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveGallery, g.ID)
			if err != nil {
				return err
			}
			ids = append(ids, e.UUID)
		}
		return nil
	}))
	path := "/entities/" + ids[1] + "/gallery-consolidation"
	var p models.GalleryConsolidationPreview
	require.NoError(t, json.Unmarshal(get(path, 200).Body.Bytes(), &p))
	require.Len(t, p.Galleries, 2)
	notified := 0
	routes := &nativeArchiveRoutes{repo: repo, notifyMetadata: func(ctx context.Context, input metadata.Input, fields []string) error {
		require.Equal(t, ids[0], input.EntityUUID)
		require.Contains(t, fields, "alternate_titles")
		notified++
		return nil
	}}
	apply := func(signature string, status int) {
		body, err := json.Marshal(map[string]string{"signature": signature})
		require.NoError(t, err)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		routes.router().ServeHTTP(w, r)
		require.Equal(t, status, w.Code, w.Body.String())
	}
	apply("stale", 409)
	require.Zero(t, notified)
	apply(p.Signature, 200)
	require.Equal(t, 1, notified)
	require.NoError(t, json.Unmarshal(get(path, 200).Body.Bytes(), &p))
	require.Len(t, p.Galleries, 1)
	require.Equal(t, ids[0], p.GalleryUUID)
	apply(p.Signature, 200)
	require.Equal(t, 1, notified, "replay must not emit another change")
	w := httptest.NewRecorder()
	routes.routerWithSourceTracking(false).ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	require.NotEqual(t, http.StatusOK, w.Code)
}
