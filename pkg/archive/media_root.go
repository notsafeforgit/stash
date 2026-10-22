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
	var identity string
	expected := binding.DirectoryIdentity
	switch {
	case check && binding.PersistentIdentity != "":
		expected = binding.PersistentIdentity
		identity, err = fsutil.OpenPersistentFileIdentity(directory)
	case check:
		identity, err = mediaFileIdentity(directory, binding)
	default:
		identity, err = fsutil.OpenPersistentFileIdentity(directory)
	}
	directory.Close()
	if err != nil || (check && identity != expected) {
		root.Close()
		if err == nil {
			err = errors.New("media root directory changed or its mount is unavailable; review the binding")
		}
		return nil, "", err
	}
	return root, identity, nil
}

// A binding chooses the identity format for its file and directory snapshots as
// well as its mount check. Retained previews, jobs and discovery fingerprints
// for an older binding must not change merely because Stash was upgraded.
func mediaFileIdentity(file *os.File, binding models.MediaRootBinding) (string, error) {
	if binding.PersistentIdentity != "" {
		return fsutil.OpenUpgradedFileIdentity(file, binding.DirectoryIdentity, binding.PersistentIdentity)
	}
	if strings.HasPrefix(binding.DirectoryIdentity, fsutil.PersistentFileIdentityPrefix) {
		return fsutil.OpenPersistentFileIdentity(file)
	}
	return fsutil.OpenFileIdentity(file)
}

// UpgradeMediaRootIdentity requires an explicit review: a legacy binding alone
// cannot prove which filesystem occupied its device number before a reboot.
// The path and inode must still agree, and the reviewed persistent identity is
// checked against the open directory before it can authorize future reads.
func UpgradeMediaRootIdentity(binding models.MediaRootBinding, reviewed models.MediaRootBinding) (*models.MediaRootBinding, error) {
	if binding.Path != reviewed.Path || reviewed.PersistentIdentity != "" ||
		!fsutil.ValidFileIdentityUpgrade(binding.DirectoryIdentity, reviewed.DirectoryIdentity) {
		return nil, errors.New("identity upgrade requires the same legacy root path and inode")
	}
	if err := VerifyMediaRootBinding(reviewed); err != nil {
		return nil, err
	}
	ret := binding
	ret.PersistentIdentity = reviewed.DirectoryIdentity
	return &ret, nil
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

// RequireMediaRootPathAbsent checks the original path of a completed
// postprocessor conversion. A dangling symlink is present, not an absent file.
func RequireMediaRootPathAbsent(definition models.MediaRoot, relative string) error {
	if definition.State != "active" || definition.Binding == nil || !ValidRootRelativePath(relative, false) {
		return errors.New("conversion requires an active bound media root")
	}
	root, _, err := openPinnedRoot(*definition.Binding, true)
	if err != nil {
		return err
	}
	defer root.Close()
	_, err = root.Lstat(filepath.FromSlash(relative))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err == nil {
		return errors.New("conversion original path is still present")
	}
	return err
}
