package models

import "time"

// AttachmentDownloadTransfer combines one transfer's original start and terminal
// reports. Sequence is the first server receipt ID, not a source clock or a claim
// about which run began most recently. Library availability remains independent.
type AttachmentDownloadTransfer struct {
	Sequence           int64      `json:"sequence"`
	AttachmentUUID     string     `json:"attachment_uuid"`
	ProducerUUID       string     `json:"producer_uuid"`
	RunUUID            string     `json:"run_uuid"`
	Fence              int64      `json:"fence"`
	TransferSequence   int64      `json:"transfer_sequence"`
	CaptureEventUUID   string     `json:"capture_event_uuid"`
	CollectionUUID     string     `json:"collection_uuid"`
	CollectionRevision int        `json:"collection_revision"`
	CollectionLabel    string     `json:"collection_label"`
	RootUUID           string     `json:"root_uuid"`
	RootLabel          string     `json:"root_label"`
	State              string     `json:"state"`
	ReportedState      string     `json:"reported_state"`
	ReasonCode         string     `json:"reason_code,omitempty"`
	StartEventUUID     string     `json:"start_event_uuid,omitempty"`
	TerminalEventUUID  string     `json:"terminal_event_uuid,omitempty"`
	StartedAt          *time.Time `json:"started_at"`
	FinishedAt         *time.Time `json:"finished_at"`
	FirstRecordedAt    time.Time  `json:"first_recorded_at"`
	LastRecordedAt     time.Time  `json:"last_recorded_at"`
	FileEventUUID      string     `json:"file_event_uuid,omitempty"`
	VerificationJob    string     `json:"verification_job_uuid,omitempty"`
	VerificationState  string     `json:"verification_state,omitempty"`
}

type AttachmentDownloadPage struct {
	AttachmentUUID string                       `json:"attachment_uuid"`
	CheckedAt      time.Time                    `json:"checked_at"`
	Transfers      []AttachmentDownloadTransfer `json:"transfers"`
	NextBefore     *int64                       `json:"next_before"`
}
