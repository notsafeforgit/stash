package models

import (
	"errors"
	"time"
)

// AttachmentDownloadInput records a report about one producer transfer. Its
// source capture, worker attempt and optional file receipt are immutable scope.
// A downloaded report does not assert verified or currently available media.
type AttachmentDownloadInput struct {
	ProducerUUID     string    `json:"producer_uuid"`
	EventUUID        string    `json:"event_uuid"`
	RunUUID          string    `json:"run_uuid"`
	Fence            int64     `json:"fence"`
	OwnerUUID        string    `json:"owner_uuid"`
	TransferSequence int64     `json:"transfer_sequence"`
	CaptureEventUUID string    `json:"capture_event_uuid"`
	AttachmentUUID   string    `json:"attachment_uuid"`
	State            string    `json:"reported_state"`
	FileEventUUID    string    `json:"file_event_uuid,omitempty"`
	ReasonCode       string    `json:"reason_code,omitempty"`
	ObservedAt       time.Time `json:"observed_at"`
}

type AttachmentDownloadReport struct {
	AttachmentDownloadInput
	Sequence          int64     `json:"sequence"`
	RecordedAt        time.Time `json:"recorded_at"`
	CollectionUUID    string    `json:"collection_uuid"`
	RootUUID          string    `json:"root_uuid"`
	TransferState     string    `json:"transfer_state"`
	VerificationState string    `json:"verification_state,omitempty"`
	VerificationJob   string    `json:"verification_job_uuid,omitempty"`
}

var (
	ErrAttachmentDownloadInvalid  = errors.New("invalid attachment download report")
	ErrAttachmentDownloadConflict = errors.New("attachment transfer already has a different report or scope")
)
