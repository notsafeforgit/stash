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
	"github.com/stashapp/stash/pkg/translation"
)

func (rs *nativeArchiveRoutes) translationService() *translation.Service {
	if rs.translations != nil {
		return rs.translations
	}
	return translation.New(rs.repo)
}

func translationWorkError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, models.ErrTranslationWorkInvalid):
		ingestJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_translation_work"})
	case errors.Is(err, translation.ErrNotFound):
		ingestError(w, ingest.ErrNotFound)
	case errors.Is(err, models.ErrTranslationWorkConflict), errors.Is(err, models.ErrArchiveJobConflict), errors.Is(err, models.ErrArchiveJobLease), errors.Is(err, models.ErrSourcePostForgotten):
		ingestJSON(w, http.StatusConflict, map[string]string{"error": "translation_work_changed"})
	default:
		ingestError(w, err)
	}
}

func writeTranslationWork(w http.ResponseWriter, value any, err error) {
	if err != nil {
		translationWorkError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, value)
}

func (rs *nativeArchiveRoutes) createTranslationTarget(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Request            models.TranslationRequestInput   `json:"request"`
		Field              string                           `json:"field"`
		CollectionUUID     *string                          `json:"collection_uuid"`
		CollectionRevision *int                             `json:"collection_revision"`
		Schedule           models.TranslationTargetSchedule `json:"schedule"`
	}
	if err := readIngestJSON(w, r, 6*1024*1024, &input); err != nil {
		ingestError(w, err)
		return
	}
	var ret *models.TranslationTarget
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		postID := chi.URLParam(r, "post")
		if !ingest.ValidUUID(postID) {
			return ingest.ErrInvalid
		}
		post, err := rs.repo.SourceEvidence.FindPost(ctx, postID)
		if err != nil {
			return err
		}
		if post == nil {
			return ingest.ErrNotFound
		}
		request, err := rs.repo.TranslationWork.RetainRequest(ctx, input.Request)
		if err != nil {
			return err
		}
		ret, err = rs.repo.TranslationWork.RetainTarget(ctx, models.TranslationTargetInput{RequestUUID: request.UUID, PostUUID: post.UUID,
			CollectionUUID: input.CollectionUUID, CollectionRevision: input.CollectionRevision, Field: input.Field, Origin: "review"}, input.Schedule, rs.translationService().Durable.Now())
		return err
	})
	writeTranslationWork(w, ret, err)
}

func (rs *nativeArchiveRoutes) scheduleTranslationTarget(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Revision int                              `json:"expected_revision"`
		Schedule models.TranslationTargetSchedule `json:"schedule"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	var ret *models.TranslationTarget
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		ret, err = rs.repo.TranslationWork.ScheduleTarget(ctx, chi.URLParam(r, "target"), input.Revision, input.Schedule, rs.translationService().Durable.Now())
		return err
	})
	writeTranslationWork(w, ret, err)
}

func (rs *nativeArchiveRoutes) retryTranslationTarget(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Revision int `json:"expected_revision"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	var ret *models.TranslationTarget
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		ret, err = rs.repo.TranslationWork.RetryTarget(ctx, chi.URLParam(r, "target"), input.Revision, rs.translationService().Durable.Now())
		return err
	})
	writeTranslationWork(w, ret, err)
}

func (rs *nativeArchiveRoutes) translationTargetJob(w http.ResponseWriter, r *http.Request) {
	var ret *models.TranslationJobTarget
	err := rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		target, err := rs.repo.TranslationWork.Target(ctx, chi.URLParam(r, "target"))
		if err != nil {
			return err
		}
		if target == nil {
			return ingest.ErrNotFound
		}
		revision := target.Revision
		if value := r.URL.Query().Get("revision"); value != "" {
			revision, err = strconv.Atoi(value)
			if err != nil || revision < 1 || revision > target.Revision {
				return ingest.ErrInvalid
			}
		}
		ret, err = rs.repo.TranslationWork.TargetBinding(ctx, target.UUID, revision)
		return err
	})
	writeTranslationWork(w, ret, err)
}

func (rs *nativeArchiveRoutes) admitTranslation(w http.ResponseWriter, r *http.Request) {
	var input struct{}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	ret, err := rs.translationService().Admit(r.Context())
	if err != nil {
		translationWorkError(w, err)
		return
	}
	code := http.StatusOK
	if ret != nil {
		code = http.StatusAccepted
	}
	ingestJSON(w, code, ret)
}

func (rs *nativeArchiveRoutes) translationJob(w http.ResponseWriter, r *http.Request) {
	ret, err := rs.translationService().Status(r.Context(), chi.URLParam(r, "job"))
	writeTranslationWork(w, ret, err)
}

func (rs *nativeArchiveRoutes) cancelTranslationJob(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Revision int64 `json:"expected_revision"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	ret, err := rs.translationService().Cancel(r.Context(), chi.URLParam(r, "job"), input.Revision)
	writeTranslationWork(w, ret, err)
}

func (rs *nativeArchiveRoutes) translationJobAttempts(w http.ResponseWriter, r *http.Request) {
	after, limit, err := albumPage(r)
	if err != nil {
		ingestError(w, err)
		return
	}
	id := chi.URLParam(r, "job")
	if _, err := rs.translationService().Status(r.Context(), id); err != nil {
		translationWorkError(w, err)
		return
	}
	var ret []models.ArchiveJobAttempt
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		ret, err = rs.repo.ArchiveJob.Attempts(ctx, id, after, limit)
		return err
	})
	writeTranslationWork(w, ret, err)
}

func (rs *nativeArchiveRoutes) translationJobHistory(w http.ResponseWriter, r *http.Request) {
	after, limit, err := albumPage(r)
	if err != nil {
		ingestError(w, err)
		return
	}
	var ret []models.ArchiveJob
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		request, err := rs.repo.TranslationWork.Request(ctx, chi.URLParam(r, "request"))
		if err != nil {
			return err
		}
		if request == nil {
			return ingest.ErrNotFound
		}
		ret, err = rs.repo.ArchiveJob.ResourceHistory(ctx, models.ArchiveJobTranslateText, archive.TranslationResource(request.UUID), after, limit)
		return err
	})
	writeTranslationWork(w, ret, err)
}
