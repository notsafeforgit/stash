package paths

import "path/filepath"

// PreviewImagesPath is the standalone v3 still-image store. Legacy generated
// paths remain adapters and can be retired without moving the new store.
func (gp *generatedPaths) PreviewImagesPath() string {
	return filepath.Join(filepath.Dir(gp.Screenshots), "preview_images")
}
