package api

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestSourcePostComparisonHTTPPreservesScopeAndHasNoMutationReceipt(t *testing.T) {
	_, repo, get := sourcePostBrowserHTTPFixture(t)
	ids := []string{}
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		for i := range 2 {
			post, err := repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "native:instagram", Value: fmt.Sprint(i)}, "")
			if err != nil {
				return err
			}
			ids = append(ids, post.UUID)
		}
		return nil
	}))
	base := "/posts/" + ids[0] + "/comparison"
	body := get(base+"?other="+ids[1], 200).Body.Bytes()
	var result models.SourcePostComparison
	require.NoError(t, json.Unmarshal(body, &result))
	require.Equal(t, ids[0], result.Left.UUID)
	require.Equal(t, ids[1], result.Right.UUID)
	require.Equal(t, 1, result.Left.Revision)
	require.Equal(t, 1, result.Right.Revision)
	require.Len(t, result.Conflicts, 1)
	require.Equal(t, "source_identifier", result.Conflicts[0].Kind)
	for _, excluded := range []string{"ready", "digest", "request_uuid", "settings", "payload"} {
		require.NotContains(t, string(body), excluded)
	}
	for _, query := range []string{"", "?other=bad", "?other=" + ids[0], "?other=" + uuid.Nil.String(), "?other=" + ids[1] + "&other=" + ids[1], "?other=" + ids[1] + "&limit=1", "?other=" + ids[1] + "&x=%ZZ"} {
		get(base+query, 400)
	}
	get(base+"?other="+uuid.NewString(), 404)
	get("/posts/invalid/comparison?other="+ids[1], 400)
	require.JSONEq(t, string(body), get(base+"?other="+ids[1], 200).Body.String())
}

func TestSourcePostComparisonHTTPRejectsTruncatedIdentitySets(t *testing.T) {
	_, repo, get := sourcePostBrowserHTTPFixture(t)
	var left, right string
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		a, err := repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "legacy:catalog:example", Value: "a"}, "")
		if err != nil {
			return err
		}
		left = a.UUID
		b, err := repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "b"}, "")
		if err != nil {
			return err
		}
		right = b.UUID
		for i := range 512 {
			post, err := repo.SourceEvidence.FindPost(ctx, left)
			if err != nil {
				return err
			}
			if err := repo.SourceEvidence.AddPostIdentifier(ctx, left, models.SourcePostIdentifier{Namespace: "legacy:catalog:example", Value: fmt.Sprint(i)}, post.Revision); err != nil {
				return err
			}
		}
		return nil
	}))
	result := get("/posts/"+left+"/comparison?other="+right, 422)
	require.Contains(t, result.Body.String(), "post_comparison_limit")
	require.NotContains(t, result.Body.String(), "identifiers")
}
