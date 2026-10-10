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
	"github.com/stashapp/stash/pkg/scrape"
)

const ingestPath = "/api/v3/ingest"

type ingestRoutes struct {
	service       *ingest.Service
	fileIngestion bool
	runs          *ingest.RunCoordinator
	enrichment    *ingest.EnrichmentCoordinator
	discovery     *ingest.DiscoveryCoordinator
	detail        *ingest.DiscoveryDetailCoordinator
}

// A producer token reaches only this router. Session cookies, general API keys,
// query tokens, GraphQL and the application fallback are never part of its auth.
func withIngestRoutes(private http.Handler, service *ingest.Service, fileIngestion bool) http.Handler {
	producer := (&ingestRoutes{service: service, fileIngestion: fileIngestion}).router()
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
		r.Post("/collections/lookup", rs.lookupCollections)
		r.Post("/batches", rs.batch)
		r.Get("/receipts/{event}", rs.receipt)
		r.Get("/receipts/{event}/status", rs.receiptStatus)
		r.Post("/runs", rs.submitRun)
		r.Post("/runs/list", rs.sourceRuns)
		r.Post("/runs/ready", rs.readySourceRuns)
		r.Post("/backfills/status", rs.backfillStatus)
		r.Post("/backfills/complete", rs.completeBackfill)
		r.Get("/runs/{run}", rs.sourceRun)
		r.Post("/runs/{run}/attempts", rs.sourceRunAttempts)
		r.Post("/runs/{run}/claim", rs.claimRun)
		r.Post("/runs/{run}/lease", rs.changeRunLease)
		r.Post("/runs/{run}/source", rs.reserveRunSource)
		rs.enrichmentRoutes(r)
		rs.discoveryRoutes(r)
		rs.discoveryDetailRoutes(r)
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
	case errors.Is(err, models.ErrArchiveJobCapacity), errors.Is(err, models.ErrSourceRunCapacity):
		return http.StatusTooManyRequests, "queue_full"
	case errors.Is(err, models.ErrSourceRunLease):
		return http.StatusConflict, "lease_lost"
	case errors.Is(err, models.ErrAttachmentDownloadConflict):
		return http.StatusConflict, "conflict"
	case errors.Is(err, models.ErrAttachmentDownloadInvalid):
		return http.StatusBadRequest, "invalid_event"
	case errors.Is(err, models.ErrBackfillIncomplete):
		return http.StatusConflict, "backfill_incomplete"
	case errors.Is(err, models.ErrSourceRunConflict), errors.Is(err, models.ErrBackfillConflict), errors.Is(err, models.ErrScanJournalConflict), errors.Is(err, models.ErrCatalogIdentityImportConflict), errors.Is(err, models.ErrCatalogRegistryImportConflict), errors.Is(err, models.ErrCatalogSnapshotConflict), errors.Is(err, models.ErrAutomationSnapshotConflict):
		return http.StatusConflict, "conflict"
	case ingest.IsConflict(err), errors.Is(err, models.ErrSourceDefinitionConflict), errors.Is(err, models.ErrFilePathChanged), errors.Is(err, models.ErrFileGenerationConflict):
		return http.StatusConflict, "conflict"
	case errors.Is(err, ingest.ErrInvalid), errors.Is(err, models.ErrSourceRunInvalid), errors.Is(err, models.ErrBackfillInvalid), errors.Is(err, models.ErrScanJournalInvalid), errors.Is(err, models.ErrCatalogIdentityImportInvalid), errors.Is(err, models.ErrCatalogRegistryImportInvalid), errors.Is(err, models.ErrCatalogSnapshotInvalid), errors.Is(err, models.ErrAutomationSnapshotInvalid):
		return http.StatusBadRequest, "invalid_event"
	case errors.Is(err, models.ErrSourceFileHistoryInvalid), errors.Is(err, models.ErrSourceFileEvidenceInvalid):
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
	kinds := []string{"source.capture", "attachment.download"}
	if rs.fileIngestion {
		kinds = append(kinds, "file.completed")
	}
	ingestJSON(w, http.StatusOK, map[string]interface{}{
		"protocol": ingest.ProtocolVersion, "producer_uuid": credential.ProducerUUID, "scopes": credential.Scopes,
		"root_uuids": credential.RootUUIDs,
		"kinds":      kinds, "post_namespaces": []string{"native:reddit", "native:twitter", "native:bluesky", "native:tiktok", "native:instagram", "native:patreon", "native:fansly", "native:tumblr", "native:jpgfish", "native:imglike", "native:putmega", "native:leakgallery"},
		"post_namespace_prefixes": []string{"mirror:coomer:", "mirror:kemono:", "ytdl:"},
		"retention_policy":        archive.SourceRetentionVersion, "max_event_bytes": ingest.MaxEventBytes, "max_batch_bytes": ingest.MaxBatchBytes, "max_batch_events": ingest.MaxBatchEvents,
		"attachment_download_protocol": 1, "max_attachment_download_bytes": ingest.MaxAttachmentDownloadBytes,
		"max_file_event_bytes": ingest.MaxFileEventBytes, "file_ingestion": rs.fileIngestion,
		"source_runs": true, "source_run_protocol": 1, "source_run_submission_receipts": true, "source_run_dispatch": true,
		"source_run_recovery_protocol":            1,
		"source_run_pacing_protocol":              1,
		"source_run_origins_protocol":             1,
		"source_run_fairness_protocol":            1,
		"source_run_traversal_protocol":           1,
		"source_run_policy_upgrade_protocol":      1,
		"metadata_worker_policy_upgrade_protocol": 1,
		"enrichment_collections_protocol":         1,
		"source_backfill_protocol":                1,
		"profile_source_protocol":                 1,
		"collection_lookup":                       true,
		"enrichment_protocol":                     2,
		"enrichment_dispatch_protocol":            1,
		"enrichment_source_pacing_protocol":       1,
		"max_enrichment_checkpoint_bytes":         archive.MaxEnrichmentTranscriptBytes,
		"discovery_protocol":                      1,
		"discovery_readiness_protocol":            1,
		"discovery_dispatch_protocol":             1,
		"discovery_collections_protocol":          1,
		"discovery_source_pacing_protocol":        1,
		"discovery_detail_protocol":               1,
		"discovery_detail_collections_protocol":   1,
		"discovery_detail_admission_protocol":     1,
		"max_discovery_detail_bytes":              archive.MaxEnrichmentTranscriptBytes,
		"max_discovery_page_bytes":                archive.MaxDiscoveryPageBytes,
		"receipt_semantics":                       "source.capture commits source evidence; attachment.download retains transfer reports; file.completed queues verification; poll file receipt status for media completion",
	})
}

type ingestCollectionBinding struct {
	RetrievalURL       string `json:"retrieval_url,omitempty"`
	CollectionUUID     string `json:"collection_uuid"`
	CollectionRevision int    `json:"collection_revision"`
	State              string `json:"state"`
}

type ingestCollectionMatches struct {
	TargetURL  string                    `json:"target_url"`
	Candidates []ingestCollectionBinding `json:"candidates"`
	HasMore    bool                      `json:"has_more"`
}

func (rs *ingestRoutes) lookupCollections(w http.ResponseWriter, r *http.Request) {
	var input struct {
		RootUUID *string  `json:"root_uuid"`
		Targets  []string `json:"targets"`
	}
	if err := readIngestJSON(w, r, 512<<10, &input); err != nil {
		ingestError(w, err)
		return
	}
	result, err := rs.service.LookupCollections(r.Context(), ingestToken(r), input.RootUUID, input.Targets)
	if err != nil {
		ingestError(w, err)
		return
	}
	matches := make([]ingestCollectionMatches, 0, len(input.Targets))
	for _, target := range input.Targets {
		entry := ingestCollectionMatches{TargetURL: target, Candidates: make([]ingestCollectionBinding, 0)}
		profile := scrape.RedditProfileURL(target)
		for _, collection := range result {
			if collection.TargetURL == target || (collection.Namespace == "native:reddit" && profile != "" && profile == collection.TargetURL) {
				if len(entry.Candidates) == 128 {
					entry.HasMore = true
					continue
				}
				binding := ingestCollectionBinding{CollectionUUID: collection.UUID, CollectionRevision: collection.Revision, State: collection.State}
				if collection.TargetURL != target {
					binding.RetrievalURL = target
				}
				entry.Candidates = append(entry.Candidates, binding)
			}
		}
		matches = append(matches, entry)
	}
	ingestJSON(w, http.StatusOK, map[string]any{"root_uuid": input.RootUUID, "targets": matches})
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
		receipt, err := rs.accept(r, token, item)
		result := ingestBatchResult{Index: index, Status: http.StatusOK, Receipt: receipt}
		if err != nil {
			result.Status, result.Error = ingestErrorCode(err)
		} else if receipt.Kind == "file.completed" {
			result.Status = http.StatusAccepted
		}
		results = append(results, result)
	}
	ingestJSON(w, http.StatusOK, map[string]interface{}{"results": results})
}

func (rs *ingestRoutes) accept(r *http.Request, token string, item ingestBatchEvent) (*models.IngestReceipt, error) {
	if ingest.Digest(item.Event) != item.Digest {
		return nil, ingest.ErrInvalid
	}
	var envelope map[string]json.RawMessage
	if err := ingest.StrictJSON(item.Event, ingest.MaxEventBytes, &envelope); err != nil {
		return nil, err
	}
	var kind string
	if err := json.Unmarshal(envelope["kind"], &kind); err != nil {
		return nil, ingest.ErrInvalid
	}
	switch kind {
	case "source.capture":
		return rs.service.Capture(r.Context(), token, item.Event, item.Digest)
	case "attachment.download":
		return rs.service.AttachmentDownload(r.Context(), token, item.Event, item.Digest)
	case "file.completed":
		if rs.fileIngestion {
			return rs.service.FileCompleted(r.Context(), token, item.Event, item.Digest)
		}
		// An unavailable processor prevents new admissions, but cannot erase
		// an acknowledgement whose response was lost before a restart.
		var event ingest.FileEvent
		if err := ingest.StrictJSON(item.Event, ingest.MaxFileEventBytes, &event); err != nil {
			return nil, err
		}
		receipt, err := rs.service.Receipt(r.Context(), token, event.EventUUID)
		if errors.Is(err, ingest.ErrNotFound) {
			return nil, ingest.ErrUnsupported
		}
		if err != nil {
			return nil, err
		}
		if receipt.ProducerUUID != event.ProducerUUID {
			return nil, ingest.ErrForbidden
		}
		if receipt.Digest != item.Digest {
			return nil, models.ErrIngestReplay
		}
		return receipt, nil
	default:
		return nil, ingest.ErrUnsupported
	}
}

func (rs *ingestRoutes) receiptStatus(w http.ResponseWriter, r *http.Request) {
	status, err := rs.service.ReceiptStatus(r.Context(), ingestToken(r), chi.URLParam(r, "event"))
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, status)
}

func (rs *ingestRoutes) receipt(w http.ResponseWriter, r *http.Request) {
	receipt, err := rs.service.Receipt(r.Context(), ingestToken(r), chi.URLParam(r, "event"))
	if err != nil {
		ingestError(w, err)
		return
	}
	ingestJSON(w, http.StatusOK, receipt)
}
