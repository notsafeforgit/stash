package ingest

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/stashapp/stash/pkg/models"
)

const MaxAttachmentDownloadBytes = 16384

// AttachmentDownloadEvent reports one producer transfer, not verified library
// availability. CaptureEventUUID identifies the transfer's retained source
// evidence. TransferSequence orders that producer's transfers within an owned
// run attempt, independently of delayed delivery or the producer's clock.
//
// Authentication, the historical attempt owner, capture/attachment scope and a
// downloaded event's file receipt must also be checked in the accepting write.
type AttachmentDownloadEvent struct {
	Protocol           int           `json:"protocol"`
	ProducerUUID       string        `json:"producer_uuid"`
	EventUUID          string        `json:"event_uuid"`
	RunUUID            string        `json:"run_uuid"`
	CollectionUUID     string        `json:"collection_uuid"`
	CollectionRevision int           `json:"collection_revision"`
	RootUUID           string        `json:"root_uuid"`
	Kind               string        `json:"kind"`
	ObservedAt         time.Time     `json:"observed_at"`
	OwnerUUID          string        `json:"owner_uuid"`
	Fence              int64         `json:"fence"`
	TransferSequence   int64         `json:"transfer_sequence"`
	CaptureEventUUID   string        `json:"capture_event_uuid"`
	Attachment         PostReference `json:"attachment"`
	State              string        `json:"state"`
	FileEventUUID      string        `json:"file_event_uuid,omitempty"`
	ReasonCode         string        `json:"reason_code,omitempty"`
}

func validDownloadReference(ref PostReference) bool {
	for _, part := range []string{ref.Namespace, ref.Value} {
		if len(part) == 0 || len(part) > 1024 || !utf8.ValidString(part) || strings.IndexFunc(part, unicode.IsControl) >= 0 {
			return false
		}
	}
	return true
}

func decodeAttachmentDownload(raw []byte) (*AttachmentDownloadEvent, error) {
	var event AttachmentDownloadEvent
	if err := StrictJSON(raw, MaxAttachmentDownloadBytes, &event); err != nil {
		return nil, err
	}
	if event.Protocol != ProtocolVersion || event.Kind != "attachment.download" {
		return nil, ErrUnsupported
	}
	for _, id := range []string{event.ProducerUUID, event.EventUUID, event.RunUUID, event.CollectionUUID, event.RootUUID, event.OwnerUUID, event.CaptureEventUUID} {
		if !ValidUUID(id) {
			return nil, ErrInvalid
		}
	}
	const maxExactJSONInteger = 1<<53 - 1
	if event.CollectionRevision < 1 || event.Fence < 1 || event.Fence > maxExactJSONInteger ||
		event.TransferSequence < 1 || event.TransferSequence > maxExactJSONInteger ||
		event.ObservedAt.IsZero() || event.ObservedAt.UTC().Year() < 1 || event.ObservedAt.UTC().Year() > 9999 ||
		!validDownloadReference(event.Attachment) {
		return nil, ErrInvalid
	}
	if event.FileEventUUID != "" && !ValidUUID(event.FileEventUUID) {
		return nil, ErrInvalid
	}
	switch event.State {
	case "started":
		if event.FileEventUUID != "" || event.ReasonCode != "" {
			return nil, ErrInvalid
		}
	case "downloaded":
		if event.FileEventUUID == "" || event.ReasonCode != "" {
			return nil, ErrInvalid
		}
	case "failed":
		if event.FileEventUUID != "" || (event.ReasonCode != "download_failed" && event.ReasonCode != "postprocess_failed" && event.ReasonCode != "source_failure") {
			return nil, ErrInvalid
		}
	case "excluded":
		if event.FileEventUUID != "" || (event.ReasonCode != "unsupported_media" && event.ReasonCode != "filter") {
			return nil, ErrInvalid
		}
	case "skipped":
		if event.FileEventUUID != "" || (event.ReasonCode != "archive_entry_without_file" && event.ReasonCode != "existing_without_file") {
			return nil, ErrInvalid
		}
	default:
		return nil, ErrInvalid
	}
	return &event, nil
}

// AttachmentDownload retains a producer's bounded lifecycle report. Historical
// attempt ownership remains sufficient after expiry, allowing offline delivery;
// current activity is derived when read, never asserted by the producer.
func (s *Service) AttachmentDownload(ctx context.Context, token string, raw []byte, digest string) (*models.IngestReceipt, error) {
	if _, err := s.Authenticate(ctx, token); err != nil {
		return nil, err
	}
	if Digest(raw) != digest {
		return nil, ErrInvalid
	}
	var envelope AttachmentDownloadEvent
	if err := StrictJSON(raw, MaxAttachmentDownloadBytes, &envelope); err != nil {
		return nil, err
	}
	if !ValidUUID(envelope.EventUUID) || !ValidUUID(envelope.ProducerUUID) {
		return nil, ErrInvalid
	}
	var result *models.IngestReceipt
	err := s.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, err := s.authenticate(ctx, token)
		if err != nil {
			return err
		}
		if credential.ProducerUUID != envelope.ProducerUUID {
			return ErrForbidden
		}
		result, err = s.Repo.Ingest.FindReceipt(ctx, envelope.ProducerUUID, envelope.EventUUID)
		if err != nil {
			return err
		}
		if result != nil {
			if !permitted(credential, result.CollectionUUID, result.RootUUID) {
				return ErrForbidden
			}
			if result.Digest != digest {
				return models.ErrIngestReplay
			}
			return nil
		}
		event, err := decodeAttachmentDownload(raw)
		if err != nil {
			return err
		}
		if !permitted(credential, event.CollectionUUID, &event.RootUUID) {
			return ErrForbidden
		}
		run, err := s.Repo.SourceRun.Find(ctx, event.RunUUID)
		if err != nil {
			return err
		}
		if run == nil {
			return ErrNotFound
		}
		if run.Operation != "download" || run.CollectionUUID != event.CollectionUUID || run.CollectionRevision != event.CollectionRevision || run.RootUUID == nil || *run.RootUUID != event.RootUUID {
			return ErrForbidden
		}
		attempts, err := s.Repo.SourceRun.Attempts(ctx, event.RunUUID, event.Fence-1, 1)
		if err != nil {
			return err
		}
		if len(attempts) != 1 || attempts[0].Fence != event.Fence || attempts[0].ProducerUUID != event.ProducerUUID || attempts[0].OwnerUUID != event.OwnerUUID {
			return ErrForbidden
		}
		capture, err := s.Repo.Ingest.FindReceipt(ctx, event.ProducerUUID, event.CaptureEventUUID)
		if err != nil {
			return err
		}
		if capture == nil {
			return ErrNotFound
		}
		if capture.Kind != "source.capture" || capture.RunUUID != event.RunUUID || capture.CollectionUUID != event.CollectionUUID || capture.CollectionRevision != event.CollectionRevision || capture.RootUUID == nil || *capture.RootUUID != event.RootUUID {
			return ErrForbidden
		}
		attachment, err := s.Repo.SourceAttachment.Lookup(ctx, capture.PostUUID, models.SourcePostIdentifier{Namespace: event.Attachment.Namespace, Value: event.Attachment.Value})
		if err != nil {
			return err
		}
		if attachment == nil {
			return ErrNotFound
		}
		present, err := s.Repo.SourceAttachment.InCapture(ctx, capture.CaptureUUID, attachment.UUID)
		if err != nil {
			return err
		}
		if !present {
			return ErrInvalid
		}
		if event.FileEventUUID != "" {
			file, err := s.Repo.Ingest.FindReceipt(ctx, event.ProducerUUID, event.FileEventUUID)
			if err != nil {
				return err
			}
			if file == nil {
				return ErrNotFound
			}
			if file.Kind != "file.completed" || file.RunUUID != event.RunUUID || file.CollectionUUID != event.CollectionUUID || file.CollectionRevision != event.CollectionRevision || file.RootUUID == nil || *file.RootUUID != event.RootUUID || file.CaptureUUID != capture.CaptureUUID {
				return ErrForbidden
			}
			job, err := s.Repo.ArchiveJob.Find(ctx, file.JobUUID)
			if err != nil {
				return err
			}
			if job == nil {
				return ErrNotFound
			}
			var work FileWork
			if err := json.Unmarshal(job.Arguments, &work); err != nil {
				return err
			}
			if work.Publication.Source == nil || work.Publication.Source.CaptureUUID != capture.CaptureUUID || work.Publication.Source.AttachmentUUID != attachment.UUID {
				return ErrForbidden
			}
		}
		err = s.Repo.SourceAttachment.RecordDownload(ctx, models.AttachmentDownloadInput{
			ProducerUUID: event.ProducerUUID, EventUUID: event.EventUUID, RunUUID: event.RunUUID, Fence: event.Fence,
			OwnerUUID: event.OwnerUUID, TransferSequence: event.TransferSequence, CaptureEventUUID: event.CaptureEventUUID,
			AttachmentUUID: attachment.UUID, State: event.State, FileEventUUID: event.FileEventUUID, ReasonCode: event.ReasonCode, ObservedAt: event.ObservedAt,
		})
		if err != nil {
			return err
		}
		body, err := json.Marshal(struct {
			Status         string `json:"status"`
			AttachmentUUID string `json:"attachment_uuid"`
			ReportedState  string `json:"reported_state"`
			MediaIngested  bool   `json:"media_ingested"`
		}{Status: "recorded", AttachmentUUID: attachment.UUID, ReportedState: event.State})
		if err != nil {
			return err
		}
		result, err = s.Repo.Ingest.RecordReceipt(ctx, models.IngestReceipt{ProducerUUID: event.ProducerUUID, EventUUID: event.EventUUID, Digest: digest,
			CredentialUUID: credential.UUID, CollectionUUID: event.CollectionUUID, CollectionRevision: event.CollectionRevision, RootUUID: &event.RootUUID,
			RunUUID: event.RunUUID, Kind: event.Kind, PostUUID: capture.PostUUID, CaptureUUID: capture.CaptureUUID, Result: body})
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
