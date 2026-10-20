package covermigration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/previewimage"
	"github.com/stretchr/testify/require"
)

func TestFrozenCoverManifestSemantics(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	require.NoError(t, os.WriteFile(source, []byte("video"), 0600))
	info, err := os.Stat(source)
	require.NoError(t, err)
	key := SourceKey(source, info.Size(), info.ModTime().UnixNano(), "artwork")
	current, err := previewimage.SourceKey(source, "artwork")
	require.NoError(t, err)
	require.Equal(t, current, key, "v1 historical key must match the original recipe")
	parent := filepath.Join(root, "1", "7", "cover", key)
	require.NoError(t, os.MkdirAll(parent, 0700))
	for _, tc := range []struct {
		manifest string
		want     bool
		at       float64
	}{
		{fmt.Sprintf(`{"version":1,"key":%q,"at":0}`, key), true, 0},
		{fmt.Sprintf(`{"version":1,"key":%q,"at":12.345,"variants":[]}`, key), true, 12.345},
		{fmt.Sprintf(`{"version":1,"key":%q}`, key), false, 0},
		{fmt.Sprintf(`{"version":2,"key":%q,"at":10}`, key), false, 0},
		{`{"version":1,"key":"wrong","at":10}`, false, 0},
		{fmt.Sprintf(`{"version":1,"key":%q,"at":-1}`, key), false, 0},
		{fmt.Sprintf(`{"version":1,"key":%q,"at":1e999}`, key), false, 0},
		{`{"version":1`, false, 0},
	} {
		require.NoError(t, os.WriteFile(filepath.Join(parent, "manifest.json"), []byte(tc.manifest), 0600))
		claims, err := Claims(t.Context(), root, 7)
		require.NoError(t, err)
		if tc.want {
			require.Len(t, claims, 1)
			require.Equal(t, key, claims[0].Key)
			require.Equal(t, tc.at, claims[0].At)
		} else {
			require.Empty(t, claims)
		}
	}
	require.NoError(t, os.WriteFile(filepath.Join(parent, "manifest.json"), []byte(strings.Repeat(" ", 65537)), 0600))
	_, err = Claims(t.Context(), root, 7)
	require.ErrorContains(t, err, "64 KiB")
	require.NoError(t, os.Remove(filepath.Join(parent, "manifest.json")))
	require.NoError(t, os.Mkdir(filepath.Join(parent, "manifest.json"), 0700))
	_, err = Claims(t.Context(), root, 7)
	require.ErrorContains(t, err, "not a regular file")
}

func TestCoverClaimCaptureIsBounded(t *testing.T) {
	root := t.TempDir()
	for i := range 257 {
		key := fmt.Sprintf("%064x", i)
		dir := filepath.Join(root, "1", "1", "cover", key)
		require.NoError(t, os.MkdirAll(dir, 0700))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "manifest.json"), fmt.Appendf(nil, `{"version":1,"key":%q,"at":1}`, key), 0600))
	}
	_, err := Claims(t.Context(), root, 1)
	require.ErrorContains(t, err, "256")
}
