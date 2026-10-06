package ingest

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/models"
)

const MaxFileEventBytes = 16384

type FileEvent struct {
	Protocol           int                      `json:"protocol"`
	ProducerUUID       string                   `json:"producer_uuid"`
	EventUUID          string                   `json:"event_uuid"`
	RunUUID            string                   `json:"run_uuid"`
	CollectionUUID     string                   `json:"collection_uuid"`
	CollectionRevision int                      `json:"collection_revision"`
	RootUUID           string                   `json:"root_uuid"`
	Kind               string                   `json:"kind"`
	ObservedAt         time.Time                `json:"observed_at"`
	RelativePath       string                   `json:"relative_path"`
	Size               int64                    `json:"size"`
	SHA256             string                   `json:"sha256"`
	MediaKind          models.ArchiveEntityKind `json:"media_kind"`
	Source             *FileEventSource         `json:"source,omitempty"`
}

type FileEventSource struct {
	CaptureEventUUID string        `json:"capture_event_uuid"`
	Attachment       PostReference `json:"attachment"`
}

// FileWork is created by admission and stored in the immutable job arguments.
// Producers never choose file UUIDs, generations, path fences or source UUIDs.
type FileWork struct {
	Version      int               `json:"version"`
	ProducerUUID string            `json:"producer_uuid"`
	EventUUID    string            `json:"event_uuid"`
	ObservedAt   time.Time         `json:"observed_at"`
	Size         int64             `json:"size"`
	SHA256       string            `json:"sha256"`
	Publication  IntakePublication `json:"publication"`
	Manual       *ManualFileWork   `json:"manual,omitempty"`
}

func validateFileEvent(event FileEvent) error {
	if event.Protocol != ProtocolVersion || event.Kind != "file.completed" {
		return ErrUnsupported
	}
	for _, id := range []string{event.ProducerUUID, event.EventUUID, event.RunUUID, event.CollectionUUID, event.RootUUID} {
		if !ValidUUID(id) {
			return ErrInvalid
		}
	}
	if event.CollectionRevision < 1 || event.ObservedAt.IsZero() || event.ObservedAt.UTC().Year() < 1 || event.ObservedAt.UTC().Year() > 9999 ||
		event.Size < 1 || !archive.ValidSHA256(event.SHA256) || !archive.ValidRootRelativePath(event.RelativePath, false) || strings.HasSuffix(strings.ToLower(event.RelativePath), ".part") {
		return ErrInvalid
	}
	if event.MediaKind != models.ArchiveScene && event.MediaKind != models.ArchiveImage {
		return ErrUnsupported
	}
	if event.Source != nil && !ValidUUID(event.Source.CaptureEventUUID) {
		return ErrInvalid
	}
	return nil
}

// FileCompleted accepts a final file claim into durable verification work.
// The returned immutable receipt acknowledges the queue transaction, never the
// producer's SHA-256 claim or a completed media import. Hashing/probing is work
// for the leased worker, outside the accepting transaction.
func (s *Service) FileCompleted(ctx context.Context, token string, raw []byte, digest string) (*models.IngestReceipt, error) {
	if _, err := s.Authenticate(ctx, token); err != nil {
		return nil, err
	}
	if Digest(raw) != digest {
		return nil, ErrInvalid
	}
	var event FileEvent
	if err := StrictJSON(raw, MaxFileEventBytes, &event); err != nil {
		return nil, err
	}
	if !ValidUUID(event.EventUUID) || !ValidUUID(event.ProducerUUID) {
		return nil, ErrInvalid
	}
	var result *models.IngestReceipt
	err := s.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, err := s.authenticate(ctx, token)
		if err != nil {
			return err
		}
		if credential.ProducerUUID != event.ProducerUUID {
			return ErrForbidden
		}
		result, err = s.Repo.Ingest.FindReceipt(ctx, event.ProducerUUID, event.EventUUID)
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
		if err := validateFileEvent(event); err != nil {
			return err
		}
		if !permitted(credential, event.CollectionUUID, &event.RootUUID) {
			return ErrForbidden
		}
		intakeUUID := uuid.NewSHA1(uuid.MustParse(event.ProducerUUID), []byte("file.completed\x00"+event.EventUUID)).String()
		publication := IntakePublication{UUID: intakeUUID, CollectionUUID: event.CollectionUUID, CollectionRevision: event.CollectionRevision,
			Target: FileTarget{RootUUID: event.RootUUID, RelativePath: event.RelativePath}, Kind: event.MediaKind}
		if err := validatePublicationCollection(ctx, s.Repo, publication); err != nil {
			return err
		}
		policy, err := s.Repo.MetadataPolicy.Find(ctx, event.CollectionUUID)
		if err != nil {
			return err
		}
		if policy != nil {
			publication.PolicyRevision = policy.Revision
		}
		root, err := s.Repo.MediaRoot.Find(ctx, event.RootUUID)
		if err != nil {
			return err
		}
		if root == nil || root.State != "active" || root.Binding == nil {
			return ErrDefinition
		}
		// Confined opening checks the reviewed mount and rejects missing,
		// partial, nonregular or escaping paths before accepting the claim.
		opened, err := archive.OpenMediaRootFile(*root, event.RelativePath)
		if err != nil {
			return err
		}
		if err := opened.Close(); err != nil {
			return err
		}
		sensitive, err := fsutil.IsFsPathCaseSensitive(filepath.Join(root.Binding.Path, filepath.FromSlash(event.RelativePath)))
		if err != nil {
			return err
		}
		target, err := CaptureFileTarget(ctx, s.Repo, *root, event.RelativePath, sensitive)
		if err != nil {
			return err
		}
		publication.Target = *target
		var postUUID, captureUUID string
		if event.Source != nil {
			captureReceipt, err := s.Repo.Ingest.FindReceipt(ctx, event.ProducerUUID, event.Source.CaptureEventUUID)
			if err != nil {
				return err
			}
			if captureReceipt == nil {
				return ErrNotFound
			}
			if captureReceipt.Kind != "source.capture" || captureReceipt.CollectionUUID != event.CollectionUUID || captureReceipt.CollectionRevision != event.CollectionRevision ||
				captureReceipt.RootUUID == nil || *captureReceipt.RootUUID != event.RootUUID {
				return ErrForbidden
			}
			attachment, err := s.Repo.SourceAttachment.Lookup(ctx, captureReceipt.PostUUID, models.SourcePostIdentifier{Namespace: event.Source.Attachment.Namespace, Value: event.Source.Attachment.Value})
			if err != nil {
				return err
			}
			if attachment == nil {
				return ErrNotFound
			}
			present, err := s.Repo.SourceAttachment.InCapture(ctx, captureReceipt.CaptureUUID, attachment.UUID)
			if err != nil {
				return err
			}
			if !present {
				return ErrInvalid
			}
			publication.Source = &IntakeSource{CaptureUUID: captureReceipt.CaptureUUID, AttachmentUUID: attachment.UUID}
			postUUID, captureUUID = captureReceipt.PostUUID, captureReceipt.CaptureUUID
		}
		work := FileWork{Version: 1, ProducerUUID: event.ProducerUUID, EventUUID: event.EventUUID, ObservedAt: event.ObservedAt,
			Size: event.Size, SHA256: event.SHA256, Publication: publication}
		args, err := json.Marshal(work)
		if err != nil {
			return err
		}
		resource := target.PathFence.Path
		if !sensitive {
			resource = strings.ToLower(resource)
		}
		job, err := s.Repo.ArchiveJob.Submit(ctx, models.ArchiveJobSubmission{RequestUUID: intakeUUID, Kind: models.ArchiveJobVerifyMedia,
			WorkKey: Digest(args), ResourceKey: Digest([]byte(resource)), Arguments: args, MaxAttempts: 8}, time.Now(), 10000)
		if err != nil {
			return err
		}
		accepted, err := json.Marshal(struct {
			Status        string `json:"status"`
			MediaIngested bool   `json:"media_ingested"`
		}{Status: "queued"})
		if err != nil {
			return err
		}
		result, err = s.Repo.Ingest.RecordReceipt(ctx, models.IngestReceipt{ProducerUUID: event.ProducerUUID, EventUUID: event.EventUUID, Digest: digest,
			CredentialUUID: credential.UUID, CollectionUUID: event.CollectionUUID, CollectionRevision: event.CollectionRevision, RootUUID: &event.RootUUID,
			RunUUID: event.RunUUID, Kind: event.Kind, PostUUID: postUUID, CaptureUUID: captureUUID, JobUUID: job.UUID, Result: accepted})
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

type ReceiptStatus struct {
	Receipt   *models.IngestReceipt `json:"receipt"`
	State     string                `json:"state"`
	Attempt   int64                 `json:"attempt"`
	ErrorCode string                `json:"error,omitempty"`
	Result    json.RawMessage       `json:"result"`
}

// ReceiptStatus exposes only the submitting producer's permitted result. It
// excludes job arguments, local paths, worker identity and credential verifiers.
func (s *Service) ReceiptStatus(ctx context.Context, token, event string) (*ReceiptStatus, error) {
	if !ValidUUID(event) {
		return nil, ErrInvalid
	}
	var result *ReceiptStatus
	err := s.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		credential, err := s.authenticate(ctx, token)
		if err != nil {
			return err
		}
		receipt, err := s.Repo.Ingest.FindReceipt(ctx, credential.ProducerUUID, event)
		if err != nil {
			return err
		}
		if receipt == nil {
			return ErrNotFound
		}
		if !permitted(credential, receipt.CollectionUUID, receipt.RootUUID) {
			return ErrForbidden
		}
		result = &ReceiptStatus{Receipt: receipt, State: "succeeded", Result: receipt.Result}
		if receipt.JobUUID != "" {
			job, err := s.Repo.ArchiveJob.Find(ctx, receipt.JobUUID)
			if err != nil {
				return err
			}
			if job == nil {
				return ErrNotFound
			}
			result.State, result.Attempt, result.ErrorCode, result.Result = job.State, job.Fence, job.ErrorCode, job.Result
			if job.State != "succeeded" {
				var progress fileProgress
				if err := StrictJSON(job.Progress, 16384, &progress); err != nil {
					return err
				}
				result.Result, err = json.Marshal(fileCompletion{RegistrationCommitted: progress.Publication != nil, Publication: progress.Publication})
				if err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
