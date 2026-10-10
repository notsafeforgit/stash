package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

// IntakePublication is server-owned work recorded after admission. Its stable
// UUID is qualified by the producer/event, rather than trusting an unqualified
// external event ID. Source is optional for purchased or directly scanned media.
type IntakePublication struct {
	UUID               string                   `json:"uuid"`
	CollectionUUID     string                   `json:"collection_uuid"`
	CollectionRevision int                      `json:"collection_revision"`
	PolicyRevision     int                      `json:"policy_revision"`
	Target             FileTarget               `json:"target"`
	Kind               models.ArchiveEntityKind `json:"media_kind"`
	Source             *IntakeSource            `json:"source,omitempty"`
	Transformation     *IntakeTransformation    `json:"transformation,omitempty"`
}

type IntakeSource struct {
	CaptureUUID    string `json:"capture_uuid"`
	AttachmentUUID string `json:"attachment_uuid"`
}

// IntakePublicationResult excludes local mount paths, captured payloads and
// credentials. It can be included in a durable job result after commit.
type IntakePublicationResult struct {
	FileUUID       string                   `json:"file_uuid"`
	Generation     int64                    `json:"generation"`
	ContentUUID    string                   `json:"content_uuid"`
	MediaUUID      string                   `json:"media_uuid"`
	MediaKind      models.ArchiveEntityKind `json:"media_kind"`
	MediaCreated   bool                     `json:"media_created"`
	FileLinked     bool                     `json:"file_linked"`
	SourceMedia    string                   `json:"source_media"`
	GalleryUUID    string                   `json:"gallery_uuid,omitempty"`
	Gallery        string                   `json:"gallery"`
	Review         []string                 `json:"review"`
	MetadataState  string                   `json:"metadata_state,omitempty"`
	MetadataFields []string                 `json:"metadata_fields,omitempty"`
	Conversion     *IntakeConversionResult  `json:"conversion,omitempty"`
}

type IntakeConversionResult struct {
	UUID         string            `json:"uuid"`
	ImageUUID    string            `json:"image_uuid"`
	ImageID      int               `json:"image_id"`
	FileUUID     string            `json:"file_uuid"`
	FileID       int               `json:"file_id"`
	Checksum     string            `json:"checksum"`
	Fingerprints map[string]string `json:"fingerprints"`
	GalleryUUIDs []string          `json:"gallery_uuids"`
}

type PublishedIntake struct {
	Media  *PublishedMedia
	Result IntakePublicationResult
}

// PublishIntake combines media, collection provenance, attachment evidence and
// source album membership in the caller's managed transaction. Authentication
// occurs at admission; publication rechecks both the recorded and current
// collection's active root/path scope. Native field policies run in this same
// transaction; generated assets, after-success notifications and the completion
// receipt remain caller work.
// A publisher account never becomes a depicted performer implicitly.
func (p *PreparedMedia) PublishIntake(ctx context.Context, repo models.Repository, input IntakePublication) (*PublishedIntake, error) {
	if !ValidUUID(input.UUID) {
		return nil, ErrInvalid
	}
	if err := validatePublicationCollection(ctx, repo, input); err != nil {
		return nil, err
	}
	var selected *models.ArchiveEntity
	if input.Source != nil {
		if !ValidUUID(input.Source.CaptureUUID) || !ValidUUID(input.Source.AttachmentUUID) {
			return nil, ErrInvalid
		}
		present, err := repo.SourceCollection.HasCapture(ctx, models.CollectionCapture{
			CollectionUUID: input.CollectionUUID, CollectionRevision: input.CollectionRevision, CaptureUUID: input.Source.CaptureUUID,
		})
		if err != nil {
			return nil, err
		}
		if !present {
			return nil, ErrForbidden
		}
		choice, err := repo.SourceAttachment.MediaDecision(ctx, input.Source.AttachmentUUID)
		if err != nil {
			return nil, err
		}
		if choice != nil && choice.State == "linked" {
			selected, err = repo.ArchiveEntity.Resolve(ctx, *choice.MediaUUID)
			if err != nil {
				return nil, err
			}
			if selected == nil || selected.State != models.ArchiveEntityActive {
				return nil, models.ErrSourceAttachmentConflict
			}
			attachment, err := repo.SourceAttachment.Find(ctx, input.Source.AttachmentUUID)
			if err != nil {
				return nil, err
			}
			if attachment == nil {
				return nil, models.ErrSourceAttachmentConflict
			}
			association, err := repo.SourcePostMedia.Association(ctx, attachment.PostUUID, selected.UUID)
			if err != nil {
				return nil, err
			}
			if association.Suppressed() {
				selected = nil
			}
		}
	}
	media, conversion, err := p.publishIntakeMedia(ctx, repo, input, selected)
	if err != nil {
		return nil, err
	}
	if _, err := repo.SourceCollection.RecordMediaIntake(ctx, models.CollectionMediaIntake{
		UUID: input.UUID, CollectionUUID: input.CollectionUUID, CollectionRevision: input.CollectionRevision, MediaUUID: media.Media.UUID, Origin: "ingest",
	}); err != nil {
		return nil, err
	}
	ret := &PublishedIntake{Media: media, Result: IntakePublicationResult{
		FileUUID: media.File.Identity.UUID, Generation: media.File.File.Base().Generation, ContentUUID: media.File.Proof.Content.UUID,
		MediaUUID: media.Media.UUID, MediaKind: media.Media.Kind, MediaCreated: media.Created, FileLinked: media.FileLinked,
		SourceMedia: "not_applicable", Gallery: "not_applicable", Review: []string{},
		Conversion: conversion,
	}}
	if input.Source != nil {
		if err := publishIntakeSource(ctx, repo, input, ret); err != nil {
			return nil, err
		}
	}
	policyInput := metadata.Input{CollectionUUID: input.CollectionUUID, CollectionRevision: input.CollectionRevision,
		PolicyRevision: input.PolicyRevision, EntityUUID: media.Media.UUID, RelativePath: input.Target.RelativePath, Created: media.Created}
	if input.Source != nil && ret.Result.SourceMedia == "linked" {
		policyInput.Source = &metadata.Source{CaptureUUID: input.Source.CaptureUUID, AttachmentUUID: input.Source.AttachmentUUID}
	}
	policyResult, err := (metadata.Service{Repo: repo}).Apply(ctx, policyInput, "")
	if err != nil {
		return nil, err
	}
	ret.Result.MetadataState, ret.Result.MetadataFields = policyResult.State, policyResult.AppliedFields()
	if len(ret.Result.MetadataFields) == 0 {
		ret.Result.MetadataFields = nil // Keep the checkpoint identical after JSON replay.
	}
	for _, field := range policyResult.ReviewFields() {
		ret.Result.Review = append(ret.Result.Review, "metadata:"+field)
	}
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error { return validatePublicationCollection(ctx, repo, input) })
	return ret, nil
}

func collectionIncludesFile(collection *models.SourceCollection, target FileTarget) bool {
	if collection == nil || collection.State != "active" || collection.RootUUID == nil || *collection.RootUUID != target.RootUUID {
		return false
	}
	return collection.PathPrefix == "." || strings.HasPrefix(target.RelativePath, collection.PathPrefix+"/")
}

func validatePublicationCollection(ctx context.Context, repo models.Repository, input IntakePublication) error {
	if !ValidUUID(input.CollectionUUID) || input.CollectionRevision <= 0 || !archive.ValidRootRelativePath(input.Target.RelativePath, false) || !validIntakeTransformation(input) {
		return ErrInvalid
	}
	current, err := repo.SourceCollection.Find(ctx, input.CollectionUUID)
	if err != nil {
		return err
	}
	if !collectionIncludesFile(current, input.Target) || input.CollectionRevision > current.Revision {
		return ErrDefinition
	}
	if input.CollectionRevision != current.Revision {
		history, err := repo.SourceCollection.History(ctx, input.CollectionUUID, input.CollectionRevision-1, 1)
		if err != nil {
			return err
		}
		if len(history) != 1 || history[0].Revision != input.CollectionRevision || !collectionIncludesFile(&history[0].SourceCollection, input.Target) {
			return ErrDefinition
		}
	}
	return nil
}

func publishIntakeSource(ctx context.Context, repo models.Repository, input IntakePublication, ret *PublishedIntake) error {
	source, media := input.Source, ret.Media
	attachment, err := repo.SourceAttachment.Find(ctx, source.AttachmentUUID)
	if err != nil {
		return err
	}
	if attachment == nil {
		return models.ErrSourceAttachmentConflict
	}
	details, err := json.Marshal(struct {
		ContentUUID    string                      `json:"content_uuid"`
		Generation     int64                       `json:"generation"`
		Transformation *fileTransformationEvidence `json:"transformation,omitempty"`
	}{media.File.Proof.Content.UUID, media.File.Proof.Generation, transformationEvidence(input.Transformation)})
	if err != nil {
		return err
	}
	evidenceUUID := uuid.NewSHA1(uuid.MustParse(input.UUID), []byte("source-media-evidence")).String()
	if _, err := repo.SourceAttachment.RecordMediaEvidence(ctx, models.SourceMediaEvidence{
		UUID: evidenceUUID, PostUUID: attachment.PostUUID, CaptureUUID: source.CaptureUUID, AttachmentUUID: source.AttachmentUUID,
		MediaUUID: media.Media.UUID, FileUUID: &media.File.Identity.UUID, Basis: "verified-bytes", Details: details,
	}); err != nil {
		return err
	}
	attachment, err = repo.SourceAttachment.Find(ctx, source.AttachmentUUID)
	if err != nil {
		return err
	}
	if attachment == nil {
		return models.ErrSourceAttachmentConflict
	}
	choice, err := repo.SourceAttachment.MediaDecision(ctx, attachment.UUID)
	if err != nil {
		return err
	}
	ret.Result.SourceMedia = "linked"
	postChoice, err := repo.SourcePostMedia.Association(ctx, attachment.PostUUID, media.Media.UUID)
	if err != nil {
		return err
	}
	switch {
	case postChoice.State == "unlinked":
		ret.Result.SourceMedia = "unlinked"
	case postChoice.State == "conflict":
		ret.Result.SourceMedia = "review"
	case choice != nil && choice.State == "unlinked":
		ret.Result.SourceMedia = "unlinked"
	case choice != nil && choice.State == "linked":
		chosen, err := repo.ArchiveEntity.Resolve(ctx, *choice.MediaUUID)
		if err != nil {
			return err
		}
		if chosen == nil || chosen.State != models.ArchiveEntityActive || chosen.UUID != media.Media.UUID {
			ret.Result.SourceMedia = "review"
		}
	default:
		_, err := repo.SourceAttachment.DecideMedia(ctx, models.AttachmentMediaDecisionInput{
			AttachmentUUID: attachment.UUID, ExpectedAttachmentRevision: attachment.Revision, State: "linked",
			MediaUUID: media.Media.UUID, ExpectedMediaRevision: media.Media.Revision, Origin: "ingest",
		})
		if errors.Is(err, models.ErrAmbiguousSourceMedia) {
			ret.Result.SourceMedia = "review"
		} else if err != nil {
			return err
		}
	}
	if ret.Result.SourceMedia == "review" {
		ret.Result.Review = append(ret.Result.Review, "attachment_media")
	}
	preview, err := repo.SourceGallery.Preview(ctx, attachment.PostUUID)
	if err != nil {
		return err
	}
	ret.Result.Gallery = preview.Action
	switch preview.Action {
	case "create", "sync":
		gallery, err := repo.SourceGallery.Sync(ctx, attachment.PostUUID, preview.Signature)
		if err != nil {
			return err
		}
		ret.Result.GalleryUUID, ret.Result.Gallery = gallery.GalleryUUID, gallery.Action
	case "review":
		ret.Result.Review = append(ret.Result.Review, "source_album")
	}
	expectedStatus := ret.Result.SourceMedia
	txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
		post, err := repo.SourceEvidence.FindPost(ctx, attachment.PostUUID)
		if err != nil {
			return err
		}
		if post == nil || post.State != "active" {
			return models.ErrSourcePostForgotten
		}
		association, err := repo.SourcePostMedia.Association(ctx, attachment.PostUUID, media.Media.UUID)
		if err != nil {
			return err
		}
		if association.State != postChoice.State {
			return models.ErrSourcePostMediaConflict
		}
		if association.Suppressed() {
			return nil
		}
		if expectedStatus == "review" {
			return nil
		}
		current, err := repo.SourceAttachment.MediaDecision(ctx, attachment.UUID)
		if err != nil {
			return err
		}
		if current == nil || current.State != expectedStatus {
			return models.ErrSourceAttachmentConflict
		}
		if expectedStatus == "linked" {
			selected, err := repo.ArchiveEntity.Resolve(ctx, *current.MediaUUID)
			if err != nil {
				return err
			}
			expected, err := repo.ArchiveEntity.Resolve(ctx, media.Media.UUID)
			if err != nil {
				return err
			}
			if selected == nil || expected == nil || selected.UUID != expected.UUID {
				return models.ErrSourceAttachmentConflict
			}
		}
		return nil
	})
	return nil
}
