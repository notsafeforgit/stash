package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stashapp/stash/internal/sharing"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestShareSourceGraphQLRetainsUUIDFilters(t *testing.T) {
	rs, _, existing, secret := shareHTTPFixture(t)
	repo := rs.service.Repo
	call := nativeResolverCaller(t, &Resolver{repository: repo, shares: rs.service})
	galleryUUID := libraryTestUUID(t, repo, models.ArchiveGallery, 4)
	input := map[string]any{
		"label": "Filtered share", "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		"source": map[string]any{"mode": "ONGOING", "filter": map[string]any{"kind": "IMAGE", "filter_ast": map[string]any{"root": map[string]any{"condition": map[string]any{"field": "galleries", "value": map[string]any{"value": []string{galleryUUID}, "modifier": "EXCLUDES"}}}}}},
	}
	query := `mutation($input:MediaShareCreateInput!){mediaShareCreate(input:$input){share{id mode source_kind media_count selection_count} url}}`
	result := call(query, map[string]any{"input": input})
	require.NotContains(t, result, `"errors"`, result)
	var response struct {
		Data struct {
			Create struct {
				Share MediaShare `json:"share"`
			} `json:"mediaShareCreate"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(result), &response))
	share := response.Data.Create.Share
	require.Equal(t, ShareMembershipOngoing, share.Mode)
	require.Equal(t, ShareSourceKindFilter, share.SourceKind)
	require.Nil(t, share.MediaCount)
	row, err := rs.service.Find(t.Context(), share.ID)
	require.NoError(t, err)
	require.Contains(t, string(row.SourceJSON), galleryUUID)
	content, _, err := rs.service.Content(t.Context(), row, "")
	require.NoError(t, err)
	require.Empty(t, content.Media)
	input["targets"] = []map[string]any{{"kind": "IMAGE", "id": libraryTestUUID(t, repo, models.ArchiveImage, 1)}}
	require.Contains(t, call(query, map[string]any{"input": input}), "choose either selected items or a share source")
	_, err = rs.service.AuthenticateLink(t.Context(), existing.ID, secret)
	require.NoError(t, err, "existing links remain independent")
}

func TestShareOngoingHTTPRechecksEveryDelivery(t *testing.T) {
	rs, handler, _, _, db := shareHTTPMediaDatabaseFixture(t, false)
	sharedPreviewManager(t, rs)
	row, secret, err := rs.service.CreateFromSource(t.Context(), "owner", sharing.Options{Label: "Live", ExpiresAt: time.Now().Add(time.Hour)}, nil, &models.ShareSource{Mode: "ONGOING", Filter: &models.ShareFilter{Kind: "IMAGE", Q: "Private title"}})
	require.NoError(t, err)
	cookie := exchangeShareHTTP(t, handler, row, secret)
	base := "/share/" + row.ID + "/"
	get := func(endpoint string) int {
		return sharedPreviewRequest(handler, http.MethodGet, base+endpoint, cookie).Code
	}
	require.Equal(t, 200, get("content"))
	key := "image-" + libraryTestUUID(t, rs.service.Repo, models.ArchiveImage, 1)
	require.Equal(t, 200, get("media/"+key+"/image"))
	// The grant row and guest cookie are unchanged. Removing the match denies
	// originals, thumbnails, previews and all video routes independently.
	require.NoError(t, rs.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, _, err := db.ExecSQL(ctx, `UPDATE images SET title='No longer matching' WHERE id=1`, nil)
		return err
	}))
	response := sharedPreviewRequest(handler, http.MethodGet, base+"content", cookie)
	require.Equal(t, 200, response.Code)
	var content publicShareContent
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &content))
	require.Empty(t, content.Media)
	for _, endpoint := range []string{"media/" + key + "/image", "media/" + key + "/thumbnail", "media/" + key + "/preview-image", "media/" + key + "/stream.master.m3u8"} {
		require.Equal(t, 404, get(endpoint), endpoint)
	}
}
