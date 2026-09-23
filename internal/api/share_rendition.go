package api

import (
	"net/http"
	"strings"

	"github.com/stashapp/stash/internal/static"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/utils"
)

func (rs *shareRoutes) rendition(w http.ResponseWriter, r *http.Request) {
	item, f, scene, err := rs.item(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if rs.existingRendition(w, r, item, f, scene, strings.HasSuffix(r.URL.Path, "/thumbnail")) {
		return
	}
	if _, ok := f.(*models.ImageFile); ok {
		rs.serveOriginal(w, r, item, f, false)
	} else {
		// Shares reuse library artwork. A missing video cover does not create
		// another persistent rendition solely for sharing.
		utils.ServeImage(w, r, static.ReadAll(static.DefaultSceneImage))
	}
}
