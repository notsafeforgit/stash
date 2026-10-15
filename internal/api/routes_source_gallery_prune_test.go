package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestSourceGalleryPruneHTTPValidationAndScope(t *testing.T) {
	_, repo, _ := sourcePostBrowserHTTPFixture(t)
	g := models.NewGallery()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return repo.Gallery.Create(ctx, &models.CreateGalleryInput{Gallery: &g})
	}))
	var identity *models.ArchiveEntity
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		identity, err = repo.ArchiveEntity.FindByLocalID(ctx, models.ArchiveGallery, g.ID)
		return err
	}))
	routes := &nativeArchiveRoutes{repo: repo}
	post := func(handler http.Handler, inputs []models.SourceGalleryPruneInput, status int) string {
		t.Helper()
		body, err := json.Marshal(map[string]any{"galleries": inputs})
		require.NoError(t, err)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/source-galleries/prune", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(w, r)
		require.Equal(t, status, w.Code, w.Body.String())
		return w.Body.String()
	}
	input := models.SourceGalleryPruneInput{GalleryUUID: identity.UUID, Revision: identity.Revision}
	for _, invalid := range [][]models.SourceGalleryPruneInput{nil, {input, input}, {{GalleryUUID: "bad", Revision: 1}}, {{GalleryUUID: uuid.NewString()}}, make([]models.SourceGalleryPruneInput, 101)} {
		post(routes.router(), invalid, http.StatusBadRequest)
	}
	require.Contains(t, post(routes.router(), []models.SourceGalleryPruneInput{input}, http.StatusOK), `"status":"protected"`)
	post(routes.routerWithSourceTracking(false), []models.SourceGalleryPruneInput{input}, http.StatusServiceUnavailable)
}
