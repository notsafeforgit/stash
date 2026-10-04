package models

import (
	"context"
	"time"
)

type DiscoveryPublicationInput struct {
	TargetUUID             string `json:"target_uuid"`
	ExpectedTargetRevision int    `json:"expected_target_revision"`
	DetailJobUUID          string `json:"detail_job_uuid,omitempty"`
}

// A publication accepts one verified post identity and publishes the selected
// page's observations (or the selected detail transcript) through native capture
// services. Other retained pages are still discovery evidence; this receipt does
// not release them or finish import. DetailJobUUID identifies the record source.
type DiscoveryMatchPublication struct {
	TargetUUID      string    `json:"target_uuid" db:"target_uuid"`
	TargetRevision  int       `json:"target_revision" db:"target_revision"`
	ListingUUID     string    `json:"listing_uuid" db:"listing_uuid"`
	PageOrdinal     int       `json:"page_ordinal" db:"page_ordinal"`
	PageSHA256      string    `json:"page_sha256" db:"page_sha256"`
	PostUUID        string    `json:"post_uuid" db:"post_uuid"`
	PostRevision    int       `json:"post_revision" db:"post_revision"`
	Namespace       string    `json:"namespace" db:"namespace"`
	Value           string    `json:"value" db:"value"`
	Policy          string    `json:"policy" db:"policy"`
	Basis           string    `json:"basis" db:"basis"`
	WitnessOrdinal  int       `json:"witness_ordinal" db:"witness_ordinal"`
	EvidenceUUID    string    `json:"evidence_uuid" db:"evidence_uuid"`
	URLEvidenceUUID string    `json:"url_evidence_uuid" db:"url_evidence_uuid"`
	RecordCount     int       `json:"record_count" db:"record_count"`
	CaptureCount    int       `json:"capture_count" db:"capture_count"`
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
	DetailJobUUID   *string   `json:"detail_job_uuid,omitempty" db:"detail_job_uuid"`
}

type DiscoveryPublishedRecord struct {
	TargetUUID  string `json:"target_uuid" db:"target_uuid"`
	Ordinal     int    `json:"record_ordinal" db:"ordinal"`
	CaptureUUID string `json:"capture_uuid" db:"capture_uuid"`
}

// The caller supplies shared capture-domain effects within the same managed
// transaction. The repository verifies every resulting capture and provenance
// association before allowing identity and publication to commit.
type DiscoveryCaptureWriter func(context.Context, SourceCaptureInput, *SourceCollection) error

type PreparedDiscoveryPublication interface {
	Publish(context.Context, DiscoveryCaptureWriter, time.Time) (*DiscoveryMatchPublication, error)
}
