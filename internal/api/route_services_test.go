package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/previewimage"
	"github.com/stretchr/testify/require"
)

type testPreviewService struct{ scene int }

func (*testPreviewService) PreviewImageStore() previewimage.Store { return previewimage.Store{} }
func (s *testPreviewService) ScenePreviewImage(scene *models.Scene) *previewimage.Manifest {
	s.scene = scene.ID
	return nil
}
func (*testPreviewService) ImagePreviewImage(*models.Image) *previewimage.Manifest { return nil }
func (*testPreviewService) MarkerPreviewImage(*models.Scene, *models.SceneMarker) *previewimage.Manifest {
	return nil
}

func TestRouteServicesDoNotReadAnotherServersState(t *testing.T) {
	for _, host := range []string{"https://first.test", "https://second.test"} {
		cfg := config.NewEmpty()
		cfg.SetString(config.ExternalHost, host)
		handler := BaseURLMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, host+"/mounted", r.Context().Value(BaseURLCtxKey))
		}))
		r := httptest.NewRequest("GET", "http://internal/", nil)
		r.Header.Set("X-Forwarded-Prefix", "/mounted")
		handler.ServeHTTP(httptest.NewRecorder(), r)
	}
	preview := &testPreviewService{}
	routes := sceneRoutes{previewImages: preview}
	r := httptest.NewRequest("GET", "/preview", nil)
	r = r.WithContext(context.WithValue(r.Context(), sceneKey, &models.Scene{ID: 42}))
	w := httptest.NewRecorder()
	routes.PreviewImage(w, r)
	require.Equal(t, 42, preview.scene)
	require.Equal(t, http.StatusNotFound, w.Code)
	var served string
	downloads := downloadsRoutes{serve: func(hash string, w http.ResponseWriter, _ *http.Request) { served = hash; w.WriteHeader(204) }}
	w = httptest.NewRecorder()
	downloads.Routes().ServeHTTP(w, httptest.NewRequest("GET", "/test-id/file.zip", nil))
	require.Equal(t, "test-id", served)
	require.Equal(t, 204, w.Code)
}
