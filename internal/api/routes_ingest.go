package api

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

const ingestPath = "/api/v3/ingest"

type ingestRoutes struct{ service *ingest.Service }

// A producer token reaches only this router. Session cookies, general API keys,
// query tokens, GraphQL and the application fallback are never part of its auth.
func withIngestRoutes(private http.Handler, service *ingest.Service) http.Handler {
	producer := (&ingestRoutes{service: service}).router()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == ingestPath || strings.HasPrefix(r.URL.Path, ingestPath+"/") {
			producer.ServeHTTP(w, r)
			return
		}
		private.ServeHTTP(w, r)
	})
}

func (rs *ingestRoutes) router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(ingestHeaders)
	r.Route(ingestPath, func(r chi.Router) {
		r.Get("/capabilities", rs.capabilities)
		r.Post("/batches", rs.batch)
		r.Get("/receipts/{event}", rs.receipt)
	})
	return r
}

func ingestHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Stash-Ingest-Version", "1")
		// Exact routes and header-only authentication keep encoded aliases and
		// proxy path normalization from crossing the application boundary.
		if r.URL.RawPath != "" || r.URL.RawQuery != "" || strings.Contains(r.URL.Path, "\\") || path.Clean(r.URL.Path) != r.URL.Path {
			ingestError(w, ingest.ErrInvalid)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func ingestToken(r *http.Request) string {
	values := r.Header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(values[0], "Bearer ")
}

func ingestJSON(w http.ResponseWriter, status int, value interface{}) {
	encoded, err := json.Marshal(value)
	if err != nil {
		status = http.StatusServiceUnavailable
		encoded = []byte(`{"error":"temporarily_unavailable"}`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(encoded)
}

func ingestErrorCode(err error) (int, string) {
	switch {
	case errors.Is(err, ingest.ErrUnauthorized):
		return http.StatusUnauthorized, "unauthorized"
	case errors.Is(err, ingest.ErrForbidden):
		return http.StatusForbidden, "outside_scope"
	case errors.Is(err, ingest.ErrNotFound):
		return http.StatusNotFound, "not_found"
	case errors.Is(err, ingest.ErrUnsupported):
		return http.StatusUnprocessableEntity, "unsupported"
	case ingest.IsConflict(err), errors.Is(err, models.ErrSourceDefinitionConflict):
		return http.StatusConflict, "conflict"
	case errors.Is(err, ingest.ErrInvalid):
		return http.StatusBadRequest, "invalid_event"
	default:
		return http.StatusServiceUnavailable, "temporarily_unavailable"
	}
}

func ingestError(w http.ResponseWriter, err error) {
	status, code := ingestErrorCode(err)
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Bearer realm="stash-ingest"`)
	}
	ingestJSON(w, status, map[string]string{"error": code})
}

func readIngestJSON(w http.ResponseWriter, r *http.Request, limit int, output interface{}) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || r.Header.Get("Content-Encoding") != "" {
		return ingest.ErrInvalid
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, int64(limit)))
	if err != nil {
		return ingest.ErrInvalid
	}
	return ingest.StrictJSON(body, limit, output)
}

func (rs *ingestRoutes) capabilities(w http.ResponseWriter, r *http.Request) {
	credential, err := rs.service.Authenticate(r.Context(), ingestToken(r))
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, map[string]interface{}{
		"protocol": ingest.ProtocolVersion, "producer_uuid": credential.ProducerUUID, "scopes": credential.Scopes,
		"kinds": []string{"source.capture"}, "post_namespaces": []string{"native:reddit", "native:twitter"},
		"retention_policy": archive.SourceRetentionVersion, "max_event_bytes": ingest.MaxEventBytes, "max_batch_bytes": ingest.MaxBatchBytes, "max_batch_events": ingest.MaxBatchEvents,
		"file_ingestion": false, "receipt_semantics": "source evidence committed; no file download or media scan acknowledgement",
	})
}

type ingestBatch struct {
	Events []ingestBatchEvent `json:"events"`
}
type ingestBatchEvent struct {
	Digest string          `json:"sha256"`
	Event  json.RawMessage `json:"event"`
}
type ingestBatchResult struct {
	Index   int                   `json:"index"`
	Status  int                   `json:"status"`
	Error   string                `json:"error,omitempty"`
	Receipt *models.IngestReceipt `json:"receipt,omitempty"`
}

func (rs *ingestRoutes) batch(w http.ResponseWriter, r *http.Request) {
	token := ingestToken(r)
	if _, err := rs.service.Authenticate(r.Context(), token); err != nil {
		ingestError(w, err)
		return
	}
	var batch ingestBatch
	if err := readIngestJSON(w, r, ingest.MaxBatchBytes, &batch); err != nil {
		ingestError(w, err)
		return
	}
	if len(batch.Events) < 1 || len(batch.Events) > ingest.MaxBatchEvents {
		ingestError(w, ingest.ErrInvalid)
		return
	}
	results := make([]ingestBatchResult, 0, len(batch.Events))
	for index, item := range batch.Events {
		receipt, err := rs.service.Capture(r.Context(), token, item.Event, item.Digest)
		result := ingestBatchResult{Index: index, Status: http.StatusOK, Receipt: receipt}
		if err != nil {
			result.Status, result.Error = ingestErrorCode(err)
		}
		results = append(results, result)
	}
	ingestJSON(w, http.StatusOK, map[string]interface{}{"results": results})
}

func (rs *ingestRoutes) receipt(w http.ResponseWriter, r *http.Request) {
	receipt, err := rs.service.Receipt(r.Context(), ingestToken(r), chi.URLParam(r, "event"))
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, receipt)
}
