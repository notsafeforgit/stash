package metadata

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

type policyURLReader struct {
	models.SourcePostLinksReaderWriter
	t     *testing.T
	rows  []models.SourcePostURL
	calls int
}

func (r *policyURLReader) CurrentURLs(_ context.Context, post, after string, limit int) ([]models.SourcePostURL, error) {
	require.Equal(r.t, "selected-post", post)
	require.Equal(r.t, 100, limit)
	start := r.calls * 100
	if start == 0 {
		require.Empty(r.t, after)
	} else {
		require.Equal(r.t, r.rows[start-1].UUID, after)
	}
	r.calls++
	return r.rows[start:min(start+limit, len(r.rows))], nil
}

func TestPolicySourceURLsAreCompleteOrExplicitlyUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name     string
		count    int
		urlBytes int
		complete bool
	}{
		{"empty", 0, 20, true}, {"multiple_pages", 207, 20, true},
		{"exact_limit", 4096, 20, true}, {"count_overflow", 4097, 20, false},
		{"byte_overflow", 200, 8150, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &policyURLReader{t: t}
			for i := range tc.count {
				reader.rows = append(reader.rows, models.SourcePostURL{UUID: fmt.Sprintf("id-%04d", i), URL: fmt.Sprintf("https://example.test/%04d/", tc.count-i) + strings.Repeat("x", tc.urlBytes)})
			}
			service := Service{Repo: models.Repository{SourcePostLinks: reader}}
			values, complete, err := service.sourcePostURLs(t.Context(), "selected-post")
			require.NoError(t, err)
			require.Equal(t, tc.complete, complete)
			require.LessOrEqual(t, reader.calls, 41)
			if complete {
				require.NotNil(t, values)
				require.Len(t, values, tc.count)
				for i := 1; i < len(values); i++ {
					require.Less(t, values[i-1], values[i])
				}
			} else {
				require.Nil(t, values, "an incomplete source set must never look like all URLs")
			}
		})
	}
}
