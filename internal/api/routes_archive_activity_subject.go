package api

import (
	"context"
	"errors"
	"strconv"

	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/gallery"
	"github.com/stashapp/stash/pkg/models"
)

type activitySubject struct {
	Kind           string `json:"kind"`
	UUID           string `json:"uuid"`
	RequestedUUID  string `json:"requested_uuid"`
	Title          string `json:"title"`
	TitleTruncated bool   `json:"title_truncated"`
	State          string `json:"state"`
	Revision       int    `json:"revision"`
	LocalID        *int   `json:"local_id"`
}

type activityJobDetail struct {
	Summary           *models.ArchiveJobActivity `json:"summary"`
	Subjects          []activitySubject          `json:"subjects"`
	ContextAvailable  bool                       `json:"context_available"`
	RelativePath      string                     `json:"relative_path"`
	ManualRequestUUID string                     `json:"manual_request_uuid"`
	MergeRequestUUID  string                     `json:"merge_request_uuid"`
}

func (rs *nativeArchiveRoutes) describeActivityJob(ctx context.Context, summary *models.ArchiveJobActivity) (*activityJobDetail, error) {
	ret := &activityJobDetail{Summary: summary, Subjects: []activitySubject{}}
	refs, err := rs.repo.ArchiveActivity.JobReferences(ctx, summary.UUID)
	if err != nil {
		return nil, err
	}
	// Only these owning services read versioned worker arguments. Other work
	// resolves its normalized original target bindings above.
	if summary.Kind == models.ArchiveJobVerifyMedia || summary.Kind == models.ArchiveJobBackfillAlbum || summary.Kind == models.ArchiveJobNotifyPostMerge {
		current, err := rs.repo.ArchiveJob.Find(ctx, summary.UUID)
		if err != nil {
			return nil, err
		}
		switch summary.Kind {
		case models.ArchiveJobVerifyMedia:
			value, err := ingest.DescribeFileJob(current)
			if err != nil {
				if errors.Is(err, ingest.ErrInvalid) || errors.Is(err, ingest.ErrNotFound) {
					return ret, nil
				}
				return nil, err
			}
			ret.RelativePath, ret.ManualRequestUUID = value.RelativePath, value.ManualRequestUUID
			refs = append(refs, models.ArchiveActivityReference{Kind: "collection", UUID: value.CollectionUUID, Revision: value.CollectionRevision})
			if value.MediaUUID != "" {
				refs = append(refs, models.ArchiveActivityReference{Kind: "media", UUID: value.MediaUUID})
			}
		case models.ArchiveJobBackfillAlbum:
			value, err := gallery.DescribeAlbumJob(current)
			if err != nil {
				if errors.Is(err, gallery.ErrAlbumWorkInvalid) || errors.Is(err, gallery.ErrAlbumWorkNotFound) {
					return ret, nil
				}
				return nil, err
			}
			refs = append(refs, models.ArchiveActivityReference{Kind: "post", UUID: value.PostUUID})
			if value.Publication != nil && value.Publication.GalleryUUID != "" {
				refs = append(refs, models.ArchiveActivityReference{Kind: "media", UUID: value.Publication.GalleryUUID})
			}
		case models.ArchiveJobNotifyPostMerge:
			work, err := models.ParsePostConsolidationNotification(current.Arguments)
			if err != nil {
				return ret, nil
			}
			review, err := rs.repo.SourceEvidence.ConsolidationReview(ctx, work.ReviewUUID)
			if err != nil {
				return nil, err
			}
			if review == nil {
				return ret, nil
			}
			ret.MergeRequestUUID = work.ReviewUUID
			refs = append(refs, models.ArchiveActivityReference{Kind: "post", UUID: review.Request.SourceUUID}, models.ArchiveActivityReference{Kind: "post", UUID: review.Request.DestinationUUID})
			if review.Result.Gallery.GalleryUUID != "" {
				refs = append(refs, models.ArchiveActivityReference{Kind: "media", UUID: review.Result.Gallery.GalleryUUID})
			}
		}
	}
	ret.ContextAvailable = len(refs) > 0
	seen := map[string]bool{}
	for _, ref := range refs {
		subject, err := rs.activitySubject(ctx, ref)
		if err != nil {
			return nil, err
		}
		if subject == nil {
			ret.ContextAvailable = false
			continue
		}
		key := subject.Kind + ":" + subject.UUID
		if subject.Kind == "collection" {
			key += ":" + strconv.Itoa(subject.Revision)
		}
		if !seen[key] {
			ret.Subjects = append(ret.Subjects, *subject)
			seen[key] = true
		}
	}
	return ret, nil
}

func (rs *nativeArchiveRoutes) activitySubject(ctx context.Context, ref models.ArchiveActivityReference) (*activitySubject, error) {
	ret := &activitySubject{Kind: ref.Kind, UUID: ref.UUID, RequestedUUID: ref.UUID}
	switch ref.Kind {
	case "post":
		post, err := rs.repo.SourceEvidence.PostSummary(ctx, ref.UUID)
		if err != nil || post == nil {
			return nil, err
		}
		ret.UUID, ret.State, ret.Revision = post.UUID, post.State, post.Revision
		if post.LatestCapture != nil && post.LatestCapture.Title != nil {
			ret.Title = *post.LatestCapture.Title
			ret.TitleTruncated = post.LatestCapture.TitleTruncated
		}
	case "collection":
		if ref.Revision < 1 {
			return nil, models.ErrSourcePayloadCorrupt
		}
		rows, err := rs.repo.SourceCollection.History(ctx, ref.UUID, ref.Revision-1, 1)
		if err != nil {
			return nil, err
		}
		if len(rows) != 1 || rows[0].Revision != ref.Revision {
			return nil, nil
		}
		ret.Title, ret.State, ret.Revision = rows[0].Label, rows[0].State, rows[0].Revision
	case "media":
		entity, err := rs.repo.ArchiveEntity.Resolve(ctx, ref.UUID)
		if err != nil || entity == nil {
			return nil, err
		}
		ret.Kind, ret.UUID, ret.State, ret.Revision, ret.LocalID = string(entity.Kind), entity.UUID, string(entity.State), entity.Revision, entity.LocalID
	default:
		return nil, models.ErrSourcePayloadCorrupt
	}
	return ret, nil
}
