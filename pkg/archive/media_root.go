package archive

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/models"
)

// ValidRootRelativePath uses a canonical portable separator. The directory
// itself is allowed for a collection prefix, never as a file completion path.
func ValidRootRelativePath(value string, directory bool) bool {
	return value != "" && len(value) <= 4096 && utf8.ValidString(value) &&
		!strings.Contains(value, "\\") && !strings.HasPrefix(value, "/") &&
		strings.IndexFunc(value, unicode.IsControl) < 0 && path.Clean(value) == value &&
		value != ".." && !strings.HasPrefix(value, "../") && (directory || value != ".")
}

func openPinnedRoot(binding models.MediaRootBinding, check bool) (*os.Root, string, error) {
	if !filepath.IsAbs(binding.Path) || len(binding.Path) > 4096 || !utf8.ValidString(binding.Path) || strings.IndexFunc(binding.Path, unicode.IsControl) >= 0 {
		return nil, "", errors.New("media root requires an absolute local path")
	}
	root, err := os.OpenRoot(binding.Path)
	if err != nil {
		return nil, "", err
	}
	directory, err := root.Open(".")
	if err != nil {
		root.Close()
		return nil, "", err
	}
	identity, err := fsutil.OpenFileIdentity(directory)
	directory.Close()
	if err != nil || (check && identity != binding.DirectoryIdentity) {
		root.Close()
		if err == nil {
			err = errors.New("media root directory changed or its mount is unavailable; review the binding")
		}
		return nil, "", err
	}
	return root, identity, nil
}

func ProbeMediaRoot(localPath string) (*models.MediaRootBinding, error) {
	root, identity, err := openPinnedRoot(models.MediaRootBinding{Path: localPath}, false)
	if err != nil {
		return nil, err
	}
	root.Close()
	return &models.MediaRootBinding{Path: filepath.Clean(localPath), DirectoryIdentity: identity}, nil
}

func VerifyMediaRootBinding(binding models.MediaRootBinding) error {
	root, _, err := openPinnedRoot(binding, true)
	if err != nil {
		return err
	}
	return root.Close()
}

// OpenMediaRootFile returns an open, regular file beneath the reviewed mount.
// Callers hash/probe this descriptor and verify stability before acknowledging
// completion. Root binding alone is not producer authorization or proof that a
// downloader has finished writing the file.
func OpenMediaRootFile(definition models.MediaRoot, relative string) (*os.File, error) {
	if definition.State != "active" || definition.Binding == nil {
		return nil, errors.New("media root is inactive or unbound")
	}
	if !ValidRootRelativePath(relative, false) || !filepath.IsLocal(filepath.FromSlash(relative)) || strings.HasSuffix(strings.ToLower(relative), ".part") {
		return nil, errors.New("invalid or incomplete media path")
	}
	root, _, err := openPinnedRoot(*definition.Binding, true)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return fsutil.OpenRootRegularFile(root, filepath.FromSlash(relative))
}
