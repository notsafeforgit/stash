package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (rs *ingestRoutes) readyDiscoveryCollections(w http.ResponseWriter, r *http.Request) {
	var input struct {
		After string `json:"after"`
		Limit int    `json:"limit"`
	}
	if err := readIngestJSON(w, r, 1024, &input); err != nil {
		ingestError(w, err)
		return
	}
	value, err := rs.discoveryWorker().ReadyCollections(r.Context(), ingestToken(r), input.After, input.Limit)
	writeDiscoveryWorker(w, value, err)
}

func (rs *ingestRoutes) readyDiscoveryJobs(w http.ResponseWriter, r *http.Request) {
	var input struct {
		PolicySHA256     string `json:"policy_sha256"`
		ExtractorVersion string `json:"extractor_version"`
		After            int64  `json:"after"`
		Limit            int    `json:"limit"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	value, err := rs.discoveryWorker().ReadyJobs(r.Context(), ingestToken(r), chi.URLParam(r, "collection"), input.PolicySHA256, input.ExtractorVersion, input.After, input.Limit)
	writeDiscoveryWorker(w, value, err)
}

func (rs *ingestRoutes) readyDiscoveryListings(w http.ResponseWriter, r *http.Request) {
	var input struct {
		PolicySHA256     string `json:"policy_sha256"`
		ExtractorVersion string `json:"extractor_version"`
		After            string `json:"after"`
		Limit            int    `json:"limit"`
	}
	if err := readIngestJSON(w, r, 4096, &input); err != nil {
		ingestError(w, err)
		return
	}
	value, err := rs.discoveryWorker().ReadyListings(r.Context(), ingestToken(r), chi.URLParam(r, "collection"), input.PolicySHA256, input.ExtractorVersion, input.After, input.Limit)
	writeDiscoveryWorker(w, value, err)
}
