package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

const ProtocolVersion = 1
const MaxEventBytes = 5 << 20
const MaxBatchBytes = 16 << 20
const MaxBatchEvents = 8

type PostReference struct {
	Namespace string `json:"namespace"`
	Value     string `json:"value"`
}

type CaptureEvent struct {
	Protocol           int                       `json:"protocol"`
	ProducerUUID       string                    `json:"producer_uuid"`
	EventUUID          string                    `json:"event_uuid"`
	RunUUID            string                    `json:"run_uuid"`
	CollectionUUID     string                    `json:"collection_uuid"`
	CollectionRevision int                       `json:"collection_revision"`
	RootUUID           *string                   `json:"root_uuid"`
	Kind               string                    `json:"kind"`
	ObservedAt         time.Time                 `json:"observed_at"`
	ExtractorVersion   string                    `json:"extractor_version"`
	RetentionPolicy    string                    `json:"retention_policy"`
	Post               PostReference             `json:"post"`
	Metadata           models.SourcePostMetadata `json:"metadata"`
	Source             json.RawMessage           `json:"source"`
}

type CaptureResult struct {
	Status        string   `json:"status"`
	Publisher     string   `json:"publisher"`
	Album         string   `json:"album"`
	ManifestUUID  string   `json:"manifest_uuid,omitempty"`
	Review        []string `json:"review"`
	MediaIngested bool     `json:"media_ingested"`
}

// StrictJSON preserves exact large numbers and rejects duplicate keys, unknown
// envelope fields, invalid Unicode, and excessive depth before typed decoding.
func StrictJSON(raw []byte, limit int, output interface{}) error {
	if _, err := archive.DecodeJSONObject(raw, limit); err != nil {
		return fmt.Errorf("%w: malformed JSON", ErrInvalid)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("%w: envelope does not match its schema", ErrInvalid)
	}
	return nil
}

type preparedCapture struct {
	payload *models.SourceCapturePayload
	album   *archive.CapturedAlbum
}

func prepareCapture(event CaptureEvent) (*preparedCapture, error) {
	if event.Protocol != ProtocolVersion || event.Kind != "source.capture" || event.RetentionPolicy != archive.SourceRetentionVersion {
		return nil, ErrUnsupported
	}
	for _, id := range []string{event.ProducerUUID, event.EventUUID, event.RunUUID, event.CollectionUUID} {
		if !ValidUUID(id) {
			return nil, ErrInvalid
		}
	}
	if event.RootUUID != nil && !ValidUUID(*event.RootUUID) {
		return nil, ErrInvalid
	}
	if event.CollectionRevision < 1 || event.ObservedAt.IsZero() || event.ObservedAt.UTC().Year() < 1 || event.ObservedAt.UTC().Year() > 9999 || len(event.ExtractorVersion) == 0 || len(event.ExtractorVersion) > 128 || strings.ContainsAny(event.ExtractorVersion, "\r\n\x00") {
		return nil, ErrInvalid
	}
	retained, err := archive.RetainSourcePayload(event.Source)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid source payload", ErrInvalid)
	}
	tree, err := archive.DecodeJSONObject(event.Source, archive.MaxSourcePayloadBytes)
	if err != nil {
		return nil, ErrInvalid
	}
	canonical, err := archive.EncodeSourceJSON(tree)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(retained, canonical) {
		return nil, fmt.Errorf("%w: source does not satisfy retention policy", ErrInvalid)
	}
	post, err := archive.ExtractCapturedPost(retained)
	if err != nil {
		return nil, fmt.Errorf("%w: contradictory post identity", ErrInvalid)
	}
	if post == nil {
		return nil, ErrUnsupported
	}
	if post.Namespace != event.Post.Namespace || post.Value != event.Post.Value {
		return nil, fmt.Errorf("%w: post identity does not match captured evidence", ErrInvalid)
	}
	album, err := archive.ExtractCapturedAlbum(retained)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid album evidence", ErrInvalid)
	}
	if album != nil && album.Post != *post {
		return nil, ErrInvalid
	}
	metadata, err := json.Marshal(event.Metadata)
	if err != nil || len(metadata) > 262144 {
		return nil, fmt.Errorf("%w: metadata exceeds 256 KiB", ErrInvalid)
	}
	payload, err := archive.PrepareRetainedCapture("gallery-dl", strings.TrimPrefix(post.Namespace, "native:"), retained)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid retained capture", ErrInvalid)
	}
	return &preparedCapture{payload: payload, album: album}, nil
}

func (s *Service) Capture(ctx context.Context, token string, raw []byte, digest string) (*models.IngestReceipt, error) {
	// Preparation does no database or filesystem writes. Authenticate first so
	// malformed bodies are not an unauthenticated source parsing service.
	if _, err := s.Authenticate(ctx, token); err != nil {
		return nil, err
	}
	if Digest(raw) != digest {
		return nil, fmt.Errorf("%w: event SHA-256 mismatch", ErrInvalid)
	}
	var event CaptureEvent
	if err := StrictJSON(raw, MaxEventBytes, &event); err != nil {
		return nil, err
	}
	if !ValidUUID(event.ProducerUUID) || !ValidUUID(event.EventUUID) {
		return nil, ErrInvalid
	}
	// Check replay before the current normalizer runs. A durable acknowledgement
	// remains valid after policies change or the source is subsequently forgotten.
	var previous *models.IngestReceipt
	err := s.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		credential, err := s.authenticate(ctx, token)
		if err != nil {
			return err
		}
		if credential.ProducerUUID != event.ProducerUUID {
			return ErrForbidden
		}
		previous, err = s.Repo.Ingest.FindReceipt(ctx, event.ProducerUUID, event.EventUUID)
		if err != nil || previous == nil {
			return err
		}
		if !permitted(credential, previous.CollectionUUID, previous.RootUUID) {
			return ErrForbidden
		}
		if previous.Digest != digest {
			return models.ErrIngestReplay
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if previous != nil {
		return previous, nil
	}
	prepared, err := prepareCapture(event)
	if err != nil {
		return nil, err
	}
	var receipt *models.IngestReceipt
	err = s.Repo.WithTxn(ctx, func(ctx context.Context) error {
		credential, err := s.authenticate(ctx, token)
		if err != nil {
			return err
		}
		if credential.ProducerUUID != event.ProducerUUID || !permitted(credential, event.CollectionUUID, event.RootUUID) {
			return ErrForbidden
		}
		receipt, err = s.Repo.Ingest.FindReceipt(ctx, event.ProducerUUID, event.EventUUID)
		if err != nil {
			return err
		}
		if receipt != nil {
			if receipt.Digest != digest {
				return models.ErrIngestReplay
			}
			return nil
		}
		collection, err := s.Repo.SourceCollection.Find(ctx, event.CollectionUUID)
		if err != nil {
			return err
		}
		if collection == nil || collection.State != "active" || event.CollectionRevision > collection.Revision {
			return ErrDefinition
		}
		// An offline outbox pins the definition used when it captured the source.
		// Later label/target edits must not require rewriting its durable bytes.
		if collection.Revision != event.CollectionRevision {
			history, err := s.Repo.SourceCollection.History(ctx, collection.UUID, event.CollectionRevision-1, 1)
			if err != nil {
				return err
			}
			if len(history) != 1 || history[0].Revision != event.CollectionRevision {
				return ErrDefinition
			}
			collection = &history[0].SourceCollection
		}
		if collection.State != "active" || !reflect.DeepEqual(collection.RootUUID, event.RootUUID) {
			return ErrDefinition
		}
		if collection.Namespace != "" && collection.Namespace != event.Post.Namespace {
			return ErrForbidden
		}
		if event.RootUUID != nil {
			root, err := s.Repo.MediaRoot.Find(ctx, *event.RootUUID)
			if err != nil {
				return err
			}
			if root == nil || root.State != "active" {
				return ErrDefinition
			}
		}
		post, err := s.Repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: event.Post.Namespace, Value: event.Post.Value}, "")
		if err != nil {
			return err
		}
		if post.State != "active" {
			return models.ErrSourcePostForgotten
		}
		captureID := uuid.NewSHA1(uuid.MustParse(event.ProducerUUID), []byte("source.capture\x00"+event.EventUUID)).String()
		capture, err := s.Repo.SourceEvidence.RecordCapture(ctx, models.SourceCaptureInput{UUID: captureID, PostUUID: post.UUID, Origin: "gallery-dl", Platform: strings.TrimPrefix(event.Post.Namespace, "native:"), CapturedAt: event.ObservedAt, ExtractorVersion: &event.ExtractorVersion, RetentionPolicy: event.RetentionPolicy, Metadata: event.Metadata, Payload: *prepared.payload})
		if err != nil {
			return err
		}
		if err := s.Repo.SourceCollection.RecordCapture(ctx, models.CollectionCapture{CaptureUUID: capture.UUID, CollectionUUID: collection.UUID, CollectionRevision: collection.Revision}); err != nil {
			return err
		}
		result := CaptureResult{Status: "committed", Publisher: "unavailable", Album: "unavailable", Review: []string{}}
		if err := s.capturePublisher(ctx, capture.UUID, &result); err != nil {
			return err
		}
		if prepared.album != nil {
			if err := s.captureAlbum(ctx, capture, prepared.album, &result); err != nil {
				return err
			}
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			return err
		}
		receipt, err = s.Repo.Ingest.RecordReceipt(ctx, models.IngestReceipt{ProducerUUID: event.ProducerUUID, EventUUID: event.EventUUID, Digest: digest, CredentialUUID: credential.UUID, CollectionUUID: collection.UUID, CollectionRevision: collection.Revision, RootUUID: event.RootUUID, RunUUID: event.RunUUID, Kind: event.Kind, PostUUID: post.UUID, CaptureUUID: capture.UUID, Result: encoded})
		return err
	})
	if err != nil {
		return nil, err
	}
	return receipt, nil
}

func (s *Service) capturePublisher(ctx context.Context, capture string, result *CaptureResult) error {
	preview, err := s.Repo.CapturePublisher.Preview(ctx, capture, "")
	if err != nil {
		return err
	}
	result.Publisher = preview.Action
	switch preview.Action {
	case "link", "create":
		_, err = s.Repo.CapturePublisher.Apply(ctx, models.CapturePublisherInput{UUID: uuid.NewString(), CaptureUUID: capture, ExpectedSignature: preview.Signature, Action: "automatic"})
		result.Publisher = "linked"
	case "review":
		result.Review = append(result.Review, "publisher_identity")
	case "unavailable":
		if preview.IdentityError != "" {
			result.Review = append(result.Review, "publisher_identity")
		}
	}
	return err
}

func (s *Service) captureAlbum(ctx context.Context, capture *models.SourceCapture, album *archive.CapturedAlbum, result *CaptureResult) error {
	input := album.Manifest
	input.CaptureUUID = capture.UUID
	manifest, err := s.Repo.SourceAttachment.RecordManifest(ctx, input)
	if err != nil {
		return err
	}
	result.ManifestUUID = manifest.UUID
	preview, err := s.Repo.SourceAttachment.PreviewSelection(ctx, capture.PostUUID, capture.UUID)
	if err != nil {
		return err
	}
	switch {
	case preview.Protected:
		result.Album = "protected"
	case len(preview.Conflicts) > 0:
		result.Album = "review"
		result.Review = append(result.Review, "album_membership")
	default:
		result.Album = "selected"
		if preview.Changed {
			_, err = s.Repo.SourceAttachment.DecideSelection(ctx, models.AttachmentSelectionInput{PostUUID: capture.PostUUID, ExpectedPostRevision: preview.PostRevision, CaptureUUID: capture.UUID, Mode: "automatic", Origin: "ingest"})
		}
	}
	return err
}

func IsConflict(err error) bool {
	return errors.Is(err, models.ErrIngestReplay) || errors.Is(err, ErrDefinition) || errors.Is(err, models.ErrSourcePostForgotten) || errors.Is(err, models.ErrSourceCaptureReplay)
}
