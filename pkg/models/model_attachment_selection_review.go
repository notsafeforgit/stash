package models

import (
	"errors"
	"time"
)

// Reviewing source order changes the selected evidence only. Gallery membership
// is a separate reviewed operation; no media is downloaded or reassigned here.
type AttachmentSelectionReviewInput struct {
	PostUUID     string `json:"post_uuid"`
	PostRevision int    `json:"post_revision"`
	Mode         string `json:"mode"`
	CaptureUUID  string `json:"capture_uuid,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

type AttachmentSelectionReviewEntry struct {
	Position       int                         `json:"position"`
	AttachmentUUID string                      `json:"attachment_uuid"`
	Reference      SourcePostIdentifierSummary `json:"reference"`
	MediaKind      string                      `json:"media_kind"`
}

type AttachmentSelectionReviewList struct {
	DecisionUUID  string                           `json:"decision_uuid,omitempty"`
	Revision      int                              `json:"revision,omitempty"`
	Mode          string                           `json:"mode"`
	Origin        string                           `json:"origin"`
	Reason        string                           `json:"reason"`
	CaptureUUID   *string                          `json:"capture_uuid,omitempty"`
	ManifestUUIDs []string                         `json:"manifest_uuids"`
	Complete      bool                             `json:"complete"`
	DeclaredAlbum bool                             `json:"declared_album"`
	ExpectedCount *int                             `json:"expected_count,omitempty"`
	Entries       []AttachmentSelectionReviewEntry `json:"entries"`
}

type AttachmentSelectionReviewPreview struct {
	Input    AttachmentSelectionReviewInput `json:"input"`
	Current  *AttachmentSelectionReviewList `json:"current"`
	Proposed AttachmentSelectionReviewList  `json:"proposed"`
	Changed  bool                           `json:"changed"`
	Digest   string                         `json:"digest"`
}

type AttachmentSelectionReviewApplyInput struct {
	AttachmentSelectionReviewInput
	RequestUUID string `json:"request_uuid"`
	Digest      string `json:"digest"`
}

type AttachmentSelectionReview struct {
	RequestUUID  string                              `json:"request_uuid"`
	DecisionUUID string                              `json:"decision_uuid"`
	Request      AttachmentSelectionReviewApplyInput `json:"request"`
	CreatedAt    time.Time                           `json:"created_at"`
}

// Each distinct immutable source list appears once, however many captures
// supplied it. CaptureUUID is one retained witness used to review that list.
type AttachmentSelectionReviewManifest struct {
	UUID          string `json:"uuid"`
	CaptureUUID   string `json:"capture_uuid"`
	Complete      bool   `json:"complete"`
	DeclaredAlbum bool   `json:"declared_album"`
	ExpectedCount *int   `json:"expected_count,omitempty"`
	EntryCount    int    `json:"entry_count"`
}

var (
	ErrAttachmentSelectionReviewInvalid = errors.New("invalid source-list selection review")
	ErrAttachmentSelectionReviewReplay  = errors.New("source-list review UUID has different contents")
)
