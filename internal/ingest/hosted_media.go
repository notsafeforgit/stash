package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
)

// An archive skip can have no file at the Reddit download path. Reuse the
// already verified Redgifs item by its captured service ID, never by a caption,
// username or filename. This adds no download receipt or copy of the file.
func (s *Service) reuseHostedRedgifs(ctx context.Context, capture *models.SourceCapture, collection *models.SourceCollection, result *CaptureResult) error {
	if capture.Platform != "reddit" || collection.RootUUID == nil || result.Album != "selected" {
		return nil
	}
	root, err := s.Repo.MediaRoot.Find(ctx, *collection.RootUUID)
	if err != nil || root == nil || root.Binding == nil || root.State != "active" {
		return err
	}
	policy, err := s.Repo.MetadataPolicy.Find(ctx, collection.UUID)
	if err != nil {
		return err
	}
	policyRevision := 0
	if policy != nil {
		policyRevision = policy.Revision
	}
	selection, err := s.Repo.SourceAttachment.Selection(ctx, capture.PostUUID)
	if err != nil || selection == nil {
		return err
	}
	reused := false
	for _, entry := range selection.Entries {
		attachment := entry.Attachment
		if attachment.Reference.Namespace != "native:redgifs" {
			continue
		}
		present, err := s.Repo.SourceAttachment.InCapture(ctx, capture.UUID, attachment.UUID)
		if err != nil {
			return err
		}
		if !present {
			continue
		}
		proof, media, err := s.hostedRedgifsFile(ctx, attachment.Reference)
		if err != nil {
			return err
		}
		if proof == nil || proof.RootUUID != root.UUID ||
			(entry.MediaKind == "video" && media.Kind != models.ArchiveScene) ||
			(entry.MediaKind == "image" && media.Kind != models.ArchiveImage) {
			continue
		}
		fileEntity, err := s.Repo.ArchiveEntity.Resolve(ctx, proof.FileUUID)
		if err != nil {
			return err
		}
		if fileEntity == nil || fileEntity.State != models.ArchiveEntityActive || fileEntity.LocalID == nil {
			continue
		}
		files, err := s.Repo.File.Find(ctx, models.FileID(*fileEntity.LocalID))
		if err != nil {
			return err
		}
		if len(files) != 1 || files[0].Base().ZipFileID != nil {
			continue
		}
		relative, err := filepath.Rel(root.Binding.Path, files[0].Base().Path)
		if err != nil || !archive.ValidRootRelativePath(relative, false) ||
			!collectionIncludesFile(collection, FileTarget{RootUUID: root.UUID, RelativePath: relative}) {
			continue
		}
		association, err := s.Repo.SourcePostMedia.Association(ctx, capture.PostUUID, media.UUID)
		if err != nil {
			return err
		}
		choice, err := s.Repo.SourceAttachment.MediaDecision(ctx, attachment.UUID)
		if err != nil {
			return err
		}
		if association.Suppressed() || (choice != nil && (choice.State != "linked" || choice.MediaUUID == nil)) {
			continue
		}
		if choice != nil {
			chosen, err := s.Repo.ArchiveEntity.Resolve(ctx, *choice.MediaUUID)
			if err != nil {
				return err
			}
			if chosen == nil || chosen.UUID != media.UUID {
				continue
			}
		} else {
			details, err := json.Marshal(map[string]any{"content_uuid": proof.Content.UUID, "generation": proof.Generation,
				"reused_hosted_reference": attachment.Reference})
			if err != nil {
				return err
			}
			_, err = s.Repo.SourceAttachment.RecordMediaEvidence(ctx, models.SourceMediaEvidence{
				UUID:     uuid.NewSHA1(uuid.MustParse(capture.UUID), []byte("hosted-media:"+attachment.UUID+":"+proof.Content.UUID)).String(),
				PostUUID: capture.PostUUID, CaptureUUID: capture.UUID, AttachmentUUID: attachment.UUID,
				MediaUUID: media.UUID, FileUUID: &proof.FileUUID, Basis: "verified-bytes", Details: details,
			})
			if err != nil {
				return err
			}
			attachmentNow, err := s.Repo.SourceAttachment.Find(ctx, attachment.UUID)
			if err != nil {
				return err
			}
			_, err = s.Repo.SourceAttachment.DecideMedia(ctx, models.AttachmentMediaDecisionInput{
				AttachmentUUID: attachment.UUID, ExpectedAttachmentRevision: attachmentNow.Revision,
				MediaUUID: media.UUID, ExpectedMediaRevision: media.Revision, State: "linked", Origin: "ingest",
				Reason: "Reddit links the already verified Redgifs item",
			})
			if errors.Is(err, models.ErrAmbiguousSourceMedia) {
				result.Review = append(result.Review, "hosted_attachment_media")
				continue
			}
			if err != nil {
				return err
			}
		}
		preview, err := (metadata.Service{Repo: s.Repo}).Apply(ctx, metadata.Input{
			CollectionUUID: collection.UUID, CollectionRevision: collection.Revision, PolicyRevision: policyRevision,
			EntityUUID: media.UUID, RelativePath: relative, Source: &metadata.Source{CaptureUUID: capture.UUID, AttachmentUUID: attachment.UUID},
		}, "")
		if err != nil {
			return err
		}
		for _, field := range preview.ReviewFields() {
			result.Review = append(result.Review, "metadata:"+field)
		}
		if fields := preview.AppliedFields(); len(fields) > 0 {
			if err := models.NotifyEntityUpdate(ctx, media.Kind, *media.LocalID, fields); err != nil {
				return err
			}
		}
		reused = true
	}
	if reused {
		publication := &PublishedIntake{}
		if err := publishIntakeGallery(ctx, s.Repo, capture.PostUUID, publication); err != nil {
			return err
		}
		result.Review = append(result.Review, publication.Result.Review...)
	}
	return nil
}

func (s *Service) hostedRedgifsFile(ctx context.Context, ref models.SourcePostIdentifier) (*models.FileContentVerification, *models.ArchiveEntity, error) {
	post, err := s.Repo.SourceEvidence.FindPostByIdentifier(ctx, ref)
	if err != nil || post == nil || post.State != "active" {
		return nil, nil, err
	}
	attachment, err := s.Repo.SourceAttachment.Lookup(ctx, post.UUID, ref)
	if err != nil || attachment == nil {
		return nil, nil, err
	}
	choice, err := s.Repo.SourceAttachment.MediaDecision(ctx, attachment.UUID)
	if err != nil || choice == nil || choice.State != "linked" || choice.MediaUUID == nil {
		return nil, nil, err
	}
	media, err := s.Repo.ArchiveEntity.Resolve(ctx, *choice.MediaUUID)
	if err != nil || media == nil || media.State != models.ArchiveEntityActive {
		return nil, nil, err
	}
	association, err := s.Repo.SourcePostMedia.Association(ctx, post.UUID, media.UUID)
	if err != nil || association.Suppressed() {
		return nil, nil, err
	}
	evidence, err := s.Repo.SourceAttachment.MediaEvidence(ctx, attachment.UUID, "", 100)
	if err != nil || len(evidence) == 100 {
		return nil, nil, err // A truncated candidate set cannot establish reuse.
	}
	for _, item := range evidence {
		if item.FileUUID == nil || item.Basis != "verified-bytes" {
			continue
		}
		var saved struct {
			ContentUUID string `json:"content_uuid"`
			Generation  int64  `json:"generation"`
		}
		if json.Unmarshal(item.Details, &saved) != nil {
			continue
		}
		proof, err := s.Repo.FileContent.Current(ctx, *item.FileUUID)
		if err != nil {
			return nil, nil, err
		}
		if proof == nil || proof.Generation != saved.Generation || proof.Content.UUID != saved.ContentUUID {
			continue
		}
		owned, err := s.Repo.FileContent.HasOwner(ctx, proof.FileUUID, media.UUID)
		if err != nil {
			return nil, nil, err
		}
		if owned {
			return proof, media, nil
		}
	}
	return nil, nil, nil
}
