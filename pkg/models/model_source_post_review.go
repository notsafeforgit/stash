package models

import "time"

// SourcePostMediaReview is a bounded, payload-free view for one library item.
// Retained evidence is a candidate, not a selected association. Attachment
// links are reported separately because an explicit post decision can suppress
// them without destroying their evidence or previously selected metadata.
type SourcePostMediaReview struct {
	Association         *SourcePostMediaAssociation `json:"association"`
	LatestCapture       *SourcePostReviewCapture    `json:"latest_capture"`
	URLs                []SourcePostURL             `json:"urls"`
	MoreURLs            bool                        `json:"more_urls"`
	HasRetainedEvidence bool                        `json:"has_retained_evidence"`
	LinkedAttachments   int                         `json:"linked_attachments"`
}

// The list title is an excerpt. Full shared revision metadata is available on
// demand. An unknown capture time remains null, distinct from import time.
type SourcePostReviewCapture struct {
	UUID           string     `json:"uuid"`
	RevisionUUID   string     `json:"revision_uuid"`
	Origin         string     `json:"origin"`
	Platform       string     `json:"platform"`
	CapturedAt     *time.Time `json:"captured_at"`
	RecordedAt     *time.Time `json:"recorded_at"`
	Title          *string    `json:"title"`
	TitleTruncated bool       `json:"title_truncated"`
	PublishedAt    *string    `json:"published_at"`
	DateBasis      *string    `json:"date_basis"`
}
