package api

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/utils"
)

// Batch only the selected scenes for the public catalog. Each asset request
// still resolves and verifies its pinned primary file before delivering bytes.
func (rs *shareRoutes) previewScenes(ctx context.Context, media []models.ShareMedia) map[int]*models.Scene {
	var ids []int
	for _, item := range media {
		if item.Kind == "SCENE" {
			ids = append(ids, item.EntityID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	var scenes []*models.Scene
	if err := rs.service.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		var err error
		scenes, err = rs.service.Repo.Scene.FindByIDs(ctx, ids)
		return err
	}); err != nil {
		return nil
	}
	ret := make(map[int]*models.Scene, len(scenes))
	for _, scene := range scenes {
		ret[scene.ID] = scene
	}
	return ret
}

func (rs *shareRoutes) previewImage(w http.ResponseWriter, r *http.Request) {
	item, _, scene, err := rs.item(r)
	if err != nil || item.Kind != "SCENE" || scene == nil {
		http.NotFound(w, r)
		return
	}
	mgr := rs.server.manager
	// Reuse the manifest's exact-file/revision checks. The enclosing shareResponse
	// enforces no-store even though owner preview URLs are immutable and cached.
	servePreviewImage(w, r, mgr.PreviewImageStore(), scene.ID, "cover", mgr.ScenePreviewImage(scene), chi.URLParam(r, "previewFile"))
}

// Reuse existing library artwork through the share-authorized route.
func (rs *shareRoutes) existingRendition(w http.ResponseWriter, r *http.Request, item *models.ShareMedia, f models.File, scene *models.Scene, thumbnail bool) bool {
	mgr := rs.server.manager
	if item.Kind == "SCENE" && scene != nil {
		if manifest := mgr.ScenePreviewImage(scene); manifest != nil {
			variants := manifest.Variants
			if thumbnail && len(manifest.Thumbnail) > 0 {
				variants = manifest.Thumbnail
			}
			for _, variant := range variants {
				if variant.MIMEType == "image/jpeg" {
					path, allowed := mgr.PreviewImageStore().File(scene.ID, "cover", manifest, variant.File)
					if allowed != nil && serveSharedPreviewFile(w, r, path) {
						return true
					}
				}
			}
		}
		var cover []byte
		if err := rs.service.Repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
			var err error
			cover, err = rs.service.Repo.Scene.GetCover(ctx, scene.ID)
			return err
		}); err == nil && len(cover) > 0 {
			utils.ServeImage(w, r, cover)
			return true
		}
	} else if item.Kind == "IMAGE" && thumbnail {
		// Image.Checksum is the primary file's MD5. Use the already verified
		// file rather than looking up an entity that could have been replaced.
		if checksum := f.Base().Fingerprints.GetString(models.FingerprintTypeMD5); checksum != "" {
			return serveSharedPreviewFile(w, r, mgr.Paths.Generated.GetThumbnailPath(checksum, models.DefaultGthumbWidth))
		}
	}
	return false
}

func serveSharedPreviewFile(w http.ResponseWriter, r *http.Request, path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		return false
	}
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeContent(w, r, "image.jpg", time.Time{}, f)
	return true
}
