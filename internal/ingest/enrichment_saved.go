package ingest

import (
	"context"
	"os"
	"strings"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

// A missing/denied individual post need not retain an enrichment gap when the
// library already has its media and descriptive fields. Exclusion preserves the
// failed attempt; it never fabricates a capture or successful fetch. Account
// subscriptions and download recovery are separate and remain untouched.
func (c *EnrichmentCoordinator) excludeUnavailableSavedPost(ctx context.Context, job *models.ArchiveJob, code string) error {
	if job.State != "failed" || (code != "access_denied" && code != "not_found") {
		return nil
	}
	repo := c.Service.Repo
	work, err := archive.DecodeEnrichmentJob(job)
	if err != nil {
		return err
	}
	if work.Handoff != nil {
		return nil
	}
	head, err := repo.EnrichmentJob.CheckpointHead(ctx, job.UUID)
	if err != nil || head != nil {
		return err // A partial transcript can still own unresolved child work.
	}
	target, err := repo.EnrichmentWork.Target(ctx, work.TargetUUID)
	if err != nil {
		return err
	}
	if target == nil || target.State != "pending" || target.Revision != work.TargetRevision {
		return nil
	}
	content, err := repo.SourceEvidence.PostContent(ctx, work.PostUUID)
	if err != nil {
		return err
	}
	if content == nil || content.Title == nil || strings.TrimSpace(*content.Title) == "" ||
		content.OriginalText == nil || strings.TrimSpace(*content.OriginalText) == "" || savedPostDate(content.PublishedAt) == "" {
		return nil
	}
	items, err := repo.SourcePostMedia.MediaForPost(ctx, work.PostUUID, "", 100)
	if err != nil {
		return err
	}
	if len(items) == 0 || len(items) == 100 {
		return nil
	}
	for _, item := range items {
		if item.Media == nil || item.Media.State != models.ArchiveEntityActive || item.Media.LocalID == nil ||
			item.Association == nil || item.Association.State != "linked" || item.Association.PostState != "active" {
			return nil
		}
		var files []models.File
		switch item.Media.Kind {
		case models.ArchiveScene:
			values, err := repo.Scene.GetFiles(ctx, *item.Media.LocalID)
			if err != nil {
				return err
			}
			for _, value := range values {
				files = append(files, value)
			}
		case models.ArchiveImage:
			values, err := repo.Image.GetFiles(ctx, *item.Media.LocalID)
			if err != nil {
				return err
			}
			files = append(files, values...)
		default:
			return nil
		}
		if len(files) == 0 || len(files) > 32 {
			return nil
		}
		for _, file := range files {
			if file.Base().ZipFileID != nil {
				return nil
			}
			info, err := os.Lstat(file.Base().Path)
			if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() != file.Base().Size {
				return nil
			}
		}
	}
	schedule := target.EnrichmentSchedule
	schedule.State, schedule.Reason = "excluded", "saved_metadata_sufficient"
	_, err = repo.EnrichmentWork.Schedule(ctx, target.UUID, target.Revision, schedule, c.Now())
	return err
}
