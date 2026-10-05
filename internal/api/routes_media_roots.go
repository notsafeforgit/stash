package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

// Probe only opens an existing directory. It creates neither a logical root nor
// a folder, and cannot grant a producer access. The returned binding is checked
// again when a changed binding or reactivation is saved.
func (rs *nativeArchiveRoutes) probeRoot(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ServerPath string `json:"server_path"`
	}
	if err := readIngestJSON(w, r, 12288, &input); err != nil {
		ingestError(w, err)
		return
	}
	binding, err := archive.ProbeMediaRoot(input.ServerPath)
	if err != nil {
		rootBindingError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, binding)
}

func rootBindingError(w http.ResponseWriter, err error) {
	ingestJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_root_binding", "message": err.Error()})
}

func (rs *nativeArchiveRoutes) putRoot(w http.ResponseWriter, r *http.Request) {
	var input struct {
		UUID             string                   `json:"uuid"`
		ExpectedRevision int                      `json:"expected_revision"`
		Label            string                   `json:"label"`
		State            string                   `json:"state"`
		Binding          *models.MediaRootBinding `json:"binding"`
		Reason           string                   `json:"reason"`
	}
	if err := readIngestJSON(w, r, 24576, &input); err != nil {
		ingestError(w, err)
		return
	}
	id := chi.URLParam(r, "root")
	if (id != "" && !ingest.ValidUUID(id)) || input.ExpectedRevision < 0 || (input.UUID != "" && input.UUID != id) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result *models.MediaRoot
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.MediaRoot.Put(ctx, models.MediaRootInput{
			UUID: id, ExpectedRevision: input.ExpectedRevision,
			MediaRootDefinition: models.MediaRootDefinition{Label: input.Label, State: input.State, Binding: input.Binding},
			Origin:              "review", Reason: input.Reason,
		})
		return err
	})
	if errors.Is(err, models.ErrMediaRootBindingInvalid) {
		rootBindingError(w, err)
		return
	}
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) rootHistory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "root")
	after, limit, err := accountReviewPage(r)
	revision := 0
	if after != "" && err == nil {
		revision, err = strconv.Atoi(after)
	}
	if err != nil || revision < 0 || !ingest.ValidUUID(id) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result []models.MediaRootRevision
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		root, err := rs.repo.MediaRoot.Find(ctx, id)
		if err != nil {
			return err
		}
		if root == nil {
			return ingest.ErrNotFound
		}
		result, err = rs.repo.MediaRoot.History(ctx, id, revision, limit)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}
