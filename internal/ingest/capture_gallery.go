package ingest

import (
	"context"
	"errors"
	"reflect"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

var ErrCaptureDependency = errors.New("linked gallery awaits its source capture")

func (s *Service) allowLinkedGalleryCapture(ctx context.Context, event CaptureEvent, collection *models.SourceCollection) error {
	if collection.Namespace != "native:reddit" || event.Post.Namespace != "native:redgifs" || event.RootUUID == nil {
		return ErrForbidden
	}
	ref, err := archive.CapturedRedgifsGalleryLink(event.Source)
	if err != nil || ref == nil {
		return ErrForbidden
	}
	run, err := s.Repo.SourceRun.Find(ctx, event.RunUUID)
	if err != nil {
		return err
	}
	if run == nil || run.Fence == 0 || run.Operation != "download" || run.CollectionUUID != event.CollectionUUID ||
		run.CollectionRevision != event.CollectionRevision || !reflect.DeepEqual(run.RootUUID, event.RootUUID) {
		return ErrForbidden
	}
	parents, err := s.Repo.Ingest.CaptureParents(ctx, models.IngestReceipt{
		ProducerUUID: event.ProducerUUID, CollectionUUID: event.CollectionUUID, CollectionRevision: event.CollectionRevision,
		RootUUID: event.RootUUID, RunUUID: event.RunUUID,
	}, *ref, 100)
	if err != nil {
		return err
	}
	if len(parents) == 0 {
		return ErrCaptureDependency
	}
	for _, id := range parents {
		capture, err := s.Repo.SourceEvidence.FindCapture(ctx, id)
		if err != nil {
			return err
		}
		if capture == nil || capture.Platform != "reddit" {
			continue
		}
		raw, err := archive.RestoreCapture(capture.Payload)
		if err != nil {
			return err
		}
		allowed, err := archive.CapturedRedgifsGalleryMember(raw, event.Source)
		if err != nil {
			return err
		}
		if allowed {
			return nil
		}
	}
	return ErrForbidden
}
