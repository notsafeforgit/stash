package file

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ValidateMoveBasename accepts one ordinary filename, never a path. Check both
// separators so the same API request cannot acquire different platform meaning.
func ValidateMoveBasename(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\:\x00") ||
		filepath.IsAbs(name) || filepath.VolumeName(name) != "" {
		return errors.New("move basename must be a single filename")
	}
	return nil
}

func pathWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && filepath.IsLocal(relative)
}

// managedMovePath resolves directory aliases, then verifies both the configured
// spelling and its physical location. Missing tail directories are allowed, but
// the configured root itself must be online. A child symlink cannot escape it.
func managedMovePath(path string, roots []string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("file moves require absolute paths")
	}
	path = filepath.Clean(path)
	for _, root := range roots {
		root = filepath.Clean(root)
		if !filepath.IsAbs(root) || !pathWithin(root, path) {
			continue
		}
		physicalRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			return "", err
		}
		ancestor := path
		var tail []string
		for {
			physical, err := filepath.EvalSymlinks(ancestor)
			if err == nil {
				for i := len(tail) - 1; i >= 0; i-- {
					physical = filepath.Join(physical, tail[i])
				}
				if !pathWithin(physicalRoot, physical) {
					return "", errors.New("file move path escapes its library root through a symlink")
				}
				return physical, nil
			}
			if !errors.Is(err, os.ErrNotExist) || ancestor == root {
				return "", err
			}
			tail = append(tail, filepath.Base(ancestor))
			ancestor = filepath.Dir(ancestor)
		}
	}
	return "", fmt.Errorf("file move path %q is outside configured library roots", path)
}
