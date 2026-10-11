package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
)

func subscriptionFilter(r *http.Request) (models.SourceSubscriptionFilter, error) {
	q := r.URL.Query()
	f := models.SourceSubscriptionFilter{CollectionUUID: q.Get("collection"), AccountUUID: q.Get("account"), PerformerUUID: q.Get("performer"),
		RootUUID: q.Get("root"), Namespace: q.Get("namespace"), EnabledOnly: q.Get("enabled") == "true", After: q.Get("after"), Limit: 50}
	if q.Get("enabled") != "" && q.Get("enabled") != "true" && q.Get("enabled") != "false" {
		return f, ingest.ErrInvalid
	}
	for key, out := range map[string]*int{"limit": &f.Limit, "offset": &f.Offset} {
		if q.Get(key) != "" {
			n, err := strconv.Atoi(q.Get(key))
			if err != nil {
				return f, ingest.ErrInvalid
			}
			*out = n
		}
	}
	return f, nil
}

func (rs *nativeArchiveRoutes) subscriptions(w http.ResponseWriter, r *http.Request) {
	f, err := subscriptionFilter(r)
	if err != nil {
		ingestError(w, err)
		return
	}
	var result *models.SourceSubscriptionPage
	err = rs.repo.WithReadTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceSubscription.List(ctx, f)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) putSubscription(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ExpectedRevision *int  `json:"expected_revision"`
		Enabled          *bool `json:"enabled"`
	}
	if err := readIngestJSON(w, r, 1024, &body); err != nil {
		ingestError(w, err)
		return
	}
	if body.ExpectedRevision == nil || body.Enabled == nil {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	input := models.SourceSubscriptionInput{ExpectedRevision: *body.ExpectedRevision, Enabled: *body.Enabled}
	id := chi.URLParam(r, "collection")
	if !ingest.ValidUUID(id) {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	var result *models.SourceSubscription
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		result, err = rs.repo.SourceSubscription.Put(ctx, id, input)
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, result)
}

func (rs *nativeArchiveRoutes) stopPerformerMonitoring(w http.ResponseWriter, r *http.Request) {
	var input struct{}
	if err := readIngestJSON(w, r, 1024, &input); err != nil {
		ingestError(w, err)
		return
	}
	var count int
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		var err error
		count, err = rs.repo.SourceSubscription.StopPerformer(ctx, chi.URLParam(r, "entity"))
		return err
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, map[string]int{"stopped": count})
}

func (rs *nativeArchiveRoutes) bindRunSubscription(w http.ResponseWriter, r *http.Request) {
	var input models.SourceRunSubscriptionInput
	if err := readIngestJSON(w, r, 1024, &input); err != nil {
		ingestError(w, err)
		return
	}
	err := rs.repo.WithTxn(r.Context(), func(ctx context.Context) error {
		return rs.repo.SourceSubscription.BindRun(ctx, chi.URLParam(r, "run"), input)
	})
	if err != nil {
		nativeArchiveError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, input)
}
