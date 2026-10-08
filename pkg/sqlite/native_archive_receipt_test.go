package sqlite

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stretchr/testify/require"
)

// The offline verifier must reconstruct the original normalized request even
// after the producer releases its HTTP body. Both runtimes consume this corpus.
func TestNativeArchiveSourceRunDigests(t *testing.T) {
	body, err := os.ReadFile("../../integrations/archive/tests/fixtures/source-run-digests.json")
	require.NoError(t, err)
	var cases []struct {
		Name         string                  `json:"name"`
		Input        models.SourceRunRequest `json:"input"`
		NativeSHA256 string                  `json:"native_sha256"`
	}
	require.NoError(t, json.Unmarshal(body, &cases))
	require.Len(t, cases, 8)
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			input := tc.Input
			input.Window, err = scrape.NormalizeWindow(input.Window)
			require.NoError(t, err)
			digest, err := sourceRunHash(input)
			require.NoError(t, err)
			require.Equal(t, tc.NativeSHA256, digest)
		})
	}
}
