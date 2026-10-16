package api

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestFramePreviewsRejectInvalidRangesBeforeDecoding(t *testing.T) {
	scene := &models.Scene{Files: models.NewRelatedVideoFiles([]*models.VideoFile{{Duration: 10}})}
	for _, query := range []string{
		"", "start=-1&end=3&mode=frames", "start=NaN&end=3&mode=frames", "start=0&end=Inf&mode=samples",
		"start=2&end=1&mode=samples", "start=0&end=11&mode=samples", "start=0&end=1&mode=anything",
	} {
		r := httptest.NewRequest("GET", "/scene/id/frame-previews?"+query, nil)
		r = r.WithContext(context.WithValue(r.Context(), sceneKey, scene))
		w := httptest.NewRecorder()
		(sceneRoutes{}).FramePreviews(w, r)
		require.Equal(t, 400, w.Code, query)
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	}
}
