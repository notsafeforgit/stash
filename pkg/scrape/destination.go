package scrape

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

// Destination checks the server's mount and resolves existing symlinks, while
// allowing gallery-dl to create a missing destination after claiming work.
// Workers still hold the shared filesystem download lock during actual writes.
func Destination(root *models.MediaRoot, prefix string) (string, string, error) {
	if root == nil {
		return "", "", nil
	}
	if root.Binding == nil || !archive.ValidRootRelativePath(prefix, true) {
		return "", "", models.ErrSourceDefinitionConflict
	}
	if err := archive.VerifyMediaRootBinding(*root.Binding); err != nil {
		return "", "", err
	}
	base, err := filepath.EvalSymlinks(root.Binding.Path)
	if err != nil {
		return "", "", err
	}
	current := filepath.Join(base, filepath.FromSlash(prefix))
	var missing []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			info, err := os.Stat(resolved)
			if err != nil {
				return "", "", err
			}
			if !info.IsDir() {
				return "", "", models.ErrSourceDefinitionConflict
			}
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			rel, err := filepath.Rel(base, resolved)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
				return "", "", models.ErrSourceDefinitionConflict
			}
			return filepath.ToSlash(resolved), filepath.ToSlash(rel), nil
		}
		if !errors.Is(err, os.ErrNotExist) || current == base {
			return "", "", err
		}
		// A dangling symlink must not masquerade as an uncreated directory.
		if info, statErr := os.Lstat(current); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", "", models.ErrSourceDefinitionConflict
		}
		missing = append(missing, filepath.Base(current))
		current = filepath.Dir(current)
	}
}
