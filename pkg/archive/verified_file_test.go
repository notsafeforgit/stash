package archive

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func verifiedMediaFixture(t *testing.T) (models.MediaRoot, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "media")
	require.NoError(t, os.Mkdir(dir, 0700))
	path := filepath.Join(dir, "clip.mp4")
	require.NoError(t, os.WriteFile(path, []byte("original media"), 0600))
	binding, err := ProbeMediaRoot(dir)
	require.NoError(t, err)
	return models.MediaRoot{UUID: "root", MediaRootDefinition: models.MediaRootDefinition{State: "active", Binding: binding}}, path
}

func TestVerifyMediaFileClaimsAndCancellation(t *testing.T) {
	root, _ := verifiedMediaFixture(t)
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte("original media")))
	size := int64(len("original media"))
	v, err := VerifyMediaFile(t.Context(), root, "clip.mp4", &size, digest)
	require.NoError(t, err)
	defer v.Close()
	require.Equal(t, digest, v.SHA256)
	require.Equal(t, size, v.Snapshot.Size)
	body, err := io.ReadAll(v.File)
	require.NoError(t, err)
	require.Equal(t, "original media", string(body), "hashing must not move the shared descriptor offset")
	require.NoError(t, v.Revalidate(t.Context(), root))

	size++
	_, err = VerifyMediaFile(t.Context(), root, "clip.mp4", &size, digest)
	require.ErrorIs(t, err, ErrMediaFileDigest)
	_, err = VerifyMediaFile(t.Context(), root, "clip.mp4", nil, fmt.Sprintf("%x", sha256.Sum256(nil)))
	require.ErrorIs(t, err, ErrMediaFileDigest)
	for _, value := range []string{"bad", "ABCD", digest + "00"} {
		_, err = VerifyMediaFile(t.Context(), root, "clip.mp4", nil, value)
		require.Error(t, err)
	}
	size = -1
	_, err = VerifyMediaFile(t.Context(), root, "clip.mp4", &size, "")
	require.Error(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = VerifyMediaFile(ctx, root, "clip.mp4", nil, "")
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, v.Revalidate(ctx, root), context.Canceled)
}

func TestVerifiedMediaRejectsChangedFileOrRoot(t *testing.T) {
	changes := map[string]func(*testing.T, *models.MediaRoot, string){
		"append": func(t *testing.T, root *models.MediaRoot, path string) {
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
			require.NoError(t, err)
			defer f.Close()
			_, err = f.WriteString(" more")
			require.NoError(t, err)
		},
		"truncate": func(t *testing.T, root *models.MediaRoot, path string) { require.NoError(t, os.Truncate(path, 3)) },
		"replace": func(t *testing.T, root *models.MediaRoot, path string) {
			require.NoError(t, os.Rename(path, path+"-old"))
			require.NoError(t, os.WriteFile(path, []byte("original media"), 0600))
		},
		"restore mtime": func(t *testing.T, root *models.MediaRoot, path string) {
			info, err := os.Stat(path)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, []byte("modified media"), 0600))
			require.NoError(t, os.Chtimes(path, time.Now(), info.ModTime()))
		},
		"disable root":   func(t *testing.T, root *models.MediaRoot, path string) { root.State = "disabled" },
		"different root": func(t *testing.T, root *models.MediaRoot, path string) { root.UUID = "different" },
		"rebind root": func(t *testing.T, root *models.MediaRoot, path string) {
			binding := *root.Binding
			binding.Path += "-other"
			root.Binding = &binding
		},
		"replace root directory": func(t *testing.T, root *models.MediaRoot, path string) {
			dir := filepath.Dir(path)
			require.NoError(t, os.Rename(dir, dir+"-old"))
			require.NoError(t, os.Mkdir(dir, 0700))
			require.NoError(t, os.WriteFile(path, []byte("original media"), 0600))
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			root, path := verifiedMediaFixture(t)
			v, err := VerifyMediaFile(t.Context(), root, "clip.mp4", nil, "")
			require.NoError(t, err)
			defer v.Close()
			change(t, &root, path)
			require.Error(t, v.Revalidate(t.Context(), root))
		})
	}
}

func TestVerifiedMediaRejectsRetargetedSymlink(t *testing.T) {
	root, path := verifiedMediaFixture(t)
	link := filepath.Join(filepath.Dir(path), "linked.mp4")
	if err := os.Symlink("clip.mp4", link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	v, err := VerifyMediaFile(t.Context(), root, "linked.mp4", nil, "")
	require.NoError(t, err)
	defer v.Close()
	require.NoError(t, os.WriteFile(path+"-other", []byte("original media"), 0600))
	require.NoError(t, os.Remove(link))
	require.NoError(t, os.Symlink("clip.mp4-other", link))
	require.ErrorIs(t, v.Revalidate(t.Context(), root), ErrMediaFileChanged)
}
