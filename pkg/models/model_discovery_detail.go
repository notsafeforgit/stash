package models

import "encoding/json"

// A detail comparison retains references to its original listing and fetched
// transcript. Corroboration alone cannot authorize identity publication.
type DiscoveryDetailEvidence struct {
	Policy           string               `json:"policy"`
	Post             SourcePostIdentifier `json:"post"`
	URL              string               `json:"url"`
	PageSHA256       string               `json:"page_sha256"`
	TranscriptSHA256 string               `json:"transcript_sha256"`
	Status           string               `json:"status"`
	Basis            string               `json:"basis,omitempty"`
	WitnessOrdinal   *int                 `json:"witness_ordinal,omitempty"`
	RecordOrdinals   []int                `json:"record_ordinals"`
	PendingCount     int                  `json:"pending_count"`
	UnresolvedCount  int                  `json:"unresolved_count"`
}

type DiscoveryDetailPreviewInput struct {
	TargetUUID             string          `json:"-"`
	ExpectedTargetRevision int             `json:"expected_target_revision"`
	CandidateSequence      int64           `json:"candidate_sequence"`
	ExtractorVersion       string          `json:"extractor_version"`
	Body                   json.RawMessage `json:"body"`
}

// Supplied preview bytes have no authenticated delivery receipt. They cannot
// change a candidate, clear a blocker, publish metadata or complete source work.
type DiscoveryDetailPreview struct {
	PreviewOnly       bool                    `json:"preview_only"`
	TargetUUID        string                  `json:"target_uuid"`
	TargetRevision    int                     `json:"target_revision"`
	CandidateSequence int64                   `json:"candidate_sequence"`
	Evidence          DiscoveryDetailEvidence `json:"evidence"`
	Blockers          []string                `json:"blockers"`
}
