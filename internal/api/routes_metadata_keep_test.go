package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestNativeMetadataKeepHTTPReplayDoesNotNotifyOrChangeFields(t *testing.T) {
	_, repo, input := nativeMetadataReviewFixture(t)
	handler := (&nativeArchiveRoutes{repo: repo, notifyMetadata: func(context.Context, metadata.Input, []string) error {
		t.Error("keeping current metadata must not dispatch a metadata-change notification")
		return nil
	}}).router()
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	before := request(http.MethodGet, "/entities/"+input.EntityUUID+"/metadata-fields", nil)
	require.Equal(t, http.StatusOK, before.Code, before.Body.String())
	w := request(http.MethodPost, "/metadata-file-edits/preview", input)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var preview models.MetadataFileEditPreview
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &preview))
	apply := models.MetadataFileEditApplyInput{MetadataFileEditInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest, KeepCurrent: true}
	var receipt models.MetadataFileEditReview
	for retry := 0; retry < 2; retry++ {
		w = request(http.MethodPost, "/metadata-file-edits/apply", apply)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var result struct {
			Review   models.MetadataFileEditReview `json:"review"`
			Replayed bool                          `json:"replayed"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		require.Equal(t, retry == 1, result.Replayed)
		require.True(t, result.Review.KeptCurrent)
		require.Equal(t, apply, result.Review.Request)
		require.NotContains(t, w.Body.String(), "decision_uuid")
		if retry == 0 {
			receipt = result.Review
		} else {
			require.Equal(t, receipt, result.Review)
		}
	}
	w = request(http.MethodGet, "/metadata-file-edits/requests/"+apply.RequestUUID, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var recovered models.MetadataFileEditReview
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &recovered))
	require.Equal(t, receipt, recovered)
	after := request(http.MethodGet, "/entities/"+input.EntityUUID+"/metadata-fields", nil)
	require.Equal(t, http.StatusOK, after.Code, after.Body.String())
	require.JSONEq(t, before.Body.String(), after.Body.String())
	apply.KeepCurrent = false
	w = request(http.MethodPost, "/metadata-file-edits/apply", apply)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
}
