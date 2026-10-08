package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestNativeProviderMetadataHistoryHTTP(t *testing.T) {
	_, repo, input := nativeMetadataReviewFixture(t)
	var original *models.ProviderMetadataImport
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		entity, err := repo.ArchiveEntity.Find(ctx, input.EntityUUID)
		if err != nil {
			return err
		}
		original, err = repo.ProviderMetadata.Record(ctx, models.ProviderMetadataImportInput{EntityUUID: entity.UUID, ExpectedEntityRevision: entity.Revision,
			Endpoint: "https://provider.invalid/graphql", RemoteID: "remote-scene", Operation: "identify", Fields: []string{"title"}})
		return err
	}))
	handler := (&nativeArchiveRoutes{repo: repo}).router()
	request := func(path string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		return response
	}
	path := "/entities/" + input.EntityUUID + "/provider-metadata-history"
	w := request(path + "?limit=1")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var result []models.ProviderMetadataImport
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Len(t, result, 1)
	require.Equal(t, original.UUID, result[0].UUID)
	require.Equal(t, original.Signature, result[0].Signature)
	require.JSONEq(t, `{"title":"Curated"}`, string(result[0].Values))
	w = request(path + "?after=" + strconv.Itoa(original.Sequence))
	require.Equal(t, http.StatusOK, w.Code)
	require.JSONEq(t, `[]`, w.Body.String())
	for _, suffix := range []string{"?after=-1", "?after=no", "?limit=0", "?limit=101"} {
		require.Equal(t, http.StatusBadRequest, request(path+suffix).Code, suffix)
	}
	require.Equal(t, http.StatusBadRequest, request("/entities/not-a-uuid/provider-metadata-history").Code)
	require.Equal(t, http.StatusNotFound, request("/entities/"+uuid.NewString()+"/provider-metadata-history").Code)
}
