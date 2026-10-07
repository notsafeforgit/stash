package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func activityPage(r *http.Request, filterKeys ...string) (models.ArchiveActivityPage, error) {
	page := models.ArchiveActivityPage{Limit: 50}
	allowed := map[string]bool{"before": true, "limit": true}
	for _, key := range filterKeys {
		allowed[key] = true
	}
	for key, values := range r.URL.Query() {
		if !allowed[key] || len(values) != 1 || values[0] == "" {
			return page, models.ErrArchiveActivityInvalid
		}
	}
	var err error
	if value := r.URL.Query().Get("before"); value != "" {
		page.Before, err = strconv.ParseInt(value, 10, 64)
	}
	if err == nil {
		if value := r.URL.Query().Get("limit"); value != "" {
			page.Limit, err = strconv.Atoi(value)
		}
	}
	if err != nil || page.Before < 0 || page.Limit < 1 || page.Limit > 100 {
		return page, models.ErrArchiveActivityInvalid
	}
	return page, nil
}

func activityError(w http.ResponseWriter, err error) {
	if errors.Is(err, models.ErrArchiveActivityInvalid) {
		ingestJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_activity_request"})
		return
	}
	ingestError(w, err)
}

func (rs *nativeArchiveRoutes) activityJobs(w http.ResponseWriter, r *http.Request) {
	page, err := activityPage(r, "kind", "state")
	if err != nil {
		activityError(w, err)
		return
	}
	filter := models.ArchiveJobActivityFilter{ArchiveActivityPage: page, Kind: r.URL.Query().Get("kind"), State: r.URL.Query().Get("state")}
	var rows []models.ArchiveJobActivity
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		rows, err = rs.repo.ArchiveActivity.Jobs(ctx, filter)
		return err
	})
	if err != nil {
		activityError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, rows)
}

func (rs *nativeArchiveRoutes) activityJob(w http.ResponseWriter, r *http.Request) {
	var row *models.ArchiveJobActivity
	var detail *activityJobDetail
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		row, err = rs.repo.ArchiveActivity.Job(ctx, chi.URLParam(r, "job"))
		if err != nil || row == nil {
			return err
		}
		detail, err = rs.describeActivityJob(ctx, row)
		return err
	})
	if err != nil {
		activityError(w, err)
		return
	}
	if row == nil {
		ingestError(w, ingest.ErrNotFound)
		return
	}
	ingestJSON(w, http.StatusOK, detail)
}

func (rs *nativeArchiveRoutes) activityRuns(w http.ResponseWriter, r *http.Request) {
	page, err := activityPage(r, "collection", "state")
	if err != nil {
		activityError(w, err)
		return
	}
	filter := models.SourceRunActivityFilter{ArchiveActivityPage: page, CollectionUUID: r.URL.Query().Get("collection"), State: r.URL.Query().Get("state")}
	var rows []models.SourceRunActivity
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		rows, err = rs.repo.ArchiveActivity.Runs(ctx, filter)
		return err
	})
	if err != nil {
		activityError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, rows)
}

func (rs *nativeArchiveRoutes) activityRun(w http.ResponseWriter, r *http.Request) {
	var result struct {
		Summary    *models.SourceRunActivity `json:"summary"`
		Pending    []models.SourceWindow     `json:"pending"`
		Completed  []models.SourceWindow     `json:"completed"`
		Window     *models.SourceWindow      `json:"window"`
		Progress   models.SourceRunProgress  `json:"progress"`
		RootUUID   *string                   `json:"root_uuid"`
		PathPrefix string                    `json:"path_prefix"`
		Recovery   *models.SourceRunRecovery `json:"recovery"`
	}
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result.Summary, err = rs.repo.ArchiveActivity.Run(ctx, chi.URLParam(r, "run"))
		if err != nil || result.Summary == nil {
			return err
		}
		run, err := rs.repo.SourceRun.Find(ctx, result.Summary.UUID)
		if err != nil {
			return err
		}
		result.Pending = append([]models.SourceWindow{}, run.Pending...)
		result.Completed = append([]models.SourceWindow{}, run.Completed...)
		result.Window, result.Progress, result.RootUUID = run.Window, run.Progress, run.RootUUID
		result.PathPrefix, result.Recovery = run.PathPrefix, run.Recovery
		return nil
	})
	if err != nil {
		activityError(w, err)
		return
	}
	if result.Summary == nil {
		ingestError(w, ingest.ErrNotFound)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) activityJobAttempts(w http.ResponseWriter, r *http.Request) {
	rs.activityAttempts(w, r, false)
}

func (rs *nativeArchiveRoutes) activityRunAttempts(w http.ResponseWriter, r *http.Request) {
	rs.activityAttempts(w, r, true)
}

func (rs *nativeArchiveRoutes) activityAttempts(w http.ResponseWriter, r *http.Request, sourceRun bool) {
	page, err := activityPage(r)
	if err != nil {
		activityError(w, err)
		return
	}
	var rows []models.ArchiveActivityAttempt
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		if sourceRun {
			row, err := rs.repo.ArchiveActivity.Run(ctx, chi.URLParam(r, "run"))
			if err != nil {
				return err
			}
			if row == nil {
				return ingest.ErrNotFound
			}
			rows, err = rs.repo.ArchiveActivity.RunAttempts(ctx, row.UUID, page)
			return err
		}
		row, err := rs.repo.ArchiveActivity.Job(ctx, chi.URLParam(r, "job"))
		if err != nil {
			return err
		}
		if row == nil {
			return ingest.ErrNotFound
		}
		rows, err = rs.repo.ArchiveActivity.JobAttempts(ctx, row.UUID, page)
		return err
	})
	if err != nil {
		activityError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, rows)
}
