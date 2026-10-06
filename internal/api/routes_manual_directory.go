package api

import (
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

func (rs *nativeArchiveRoutes) manualDirectory(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit := 50
	valid := true
	for key, values := range query {
		if len(values) != 1 || (key != "directory" && key != "after" && key != "signature" && key != "q" && key != "limit") {
			valid = false
		}
	}
	if value := query.Get("limit"); value != "" {
		var err error
		limit, err = strconv.Atoi(value)
		valid = valid && err == nil && limit >= 1 && limit <= 100
	}
	search := query.Get("q")
	if !valid || len(search) > 256 || !utf8.ValidString(search) || strings.IndexFunc(search, unicode.IsControl) >= 0 {
		manualIntakeError(w, ingest.ErrInvalid)
		return
	}
	extensions := make(map[string]models.ArchiveEntityKind)
	settings := config.GetInstance()
	for _, ext := range settings.GetImageExtensions() {
		extensions[strings.TrimPrefix(strings.ToLower(ext), ".")] = models.ArchiveImage
	}
	for _, ext := range settings.GetVideoExtensions() {
		extensions[strings.TrimPrefix(strings.ToLower(ext), ".")] = models.ArchiveScene
	}
	page, err := ingest.New(rs.repo).ManualDirectory(r.Context(), chi.URLParam(r, "collection"), archive.MediaDirectoryInput{
		Directory: query.Get("directory"), Query: search, After: query.Get("after"), Signature: query.Get("signature"), Limit: limit, Extensions: extensions,
	})
	if err != nil {
		manualIntakeError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, page)
}
