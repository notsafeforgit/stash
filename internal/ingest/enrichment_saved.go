package ingest

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sourceadapter"
)

// A missing/denied individual post need not retain an enrichment gap when the
// library already has its media and descriptive fields. Exclusion preserves the
// failed attempt; it never fabricates a capture or successful fetch. Account
// subscriptions and download recovery are separate and remain untouched.
func (c *EnrichmentCoordinator) excludeUnavailableSavedPost(ctx context.Context, job *models.ArchiveJob, code string) ([]string, error) {
	if job.State != "failed" || (code != "access_denied" && code != "not_found") {
		return nil, nil
	}
	repo := c.Service.Repo
	work, err := archive.DecodeEnrichmentJob(job)
	if err != nil {
		return nil, err
	}
	if work.Handoff != nil {
		return nil, nil
	}
	head, err := repo.EnrichmentJob.CheckpointHead(ctx, job.UUID)
	if err != nil || head != nil {
		return nil, err // A partial transcript can still own unresolved child work.
	}
	target, err := repo.EnrichmentWork.Target(ctx, work.TargetUUID)
	if err != nil {
		return nil, err
	}
	if target == nil || target.State != "pending" || target.Revision != work.TargetRevision {
		return nil, nil
	}
	content, err := repo.SourceEvidence.PostContent(ctx, work.PostUUID)
	if err != nil {
		return nil, err
	}
	reviewed := target.Reason == "reviewed_single_retry_saved_metadata"
	if content == nil {
		return nil, nil
	}
	if !reviewed && (content.Title == nil || strings.TrimSpace(*content.Title) == "" ||
		content.OriginalText == nil || strings.TrimSpace(*content.OriginalText) == "" || savedPostDate(content.PublishedAt) == "") {
		return nil, nil
	}
	items, err := repo.SourcePostMedia.MediaForPost(ctx, work.PostUUID, "", 100)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 || len(items) == 100 {
		return nil, nil
	}
	for _, item := range items {
		if item.Media == nil || item.Media.State != models.ArchiveEntityActive || item.Media.LocalID == nil ||
			item.Association == nil || item.Association.State != "linked" || item.Association.PostState != "active" {
			return nil, nil
		}
		var files []models.File
		switch item.Media.Kind {
		case models.ArchiveScene:
			values, err := repo.Scene.GetFiles(ctx, *item.Media.LocalID)
			if err != nil {
				return nil, err
			}
			for _, value := range values {
				files = append(files, value)
			}
		case models.ArchiveImage:
			values, err := repo.Image.GetFiles(ctx, *item.Media.LocalID)
			if err != nil {
				return nil, err
			}
			files = append(files, values...)
		default:
			return nil, nil
		}
		if len(files) == 0 || len(files) > 32 {
			return nil, nil
		}
		for _, file := range files {
			if file.Base().ZipFileID != nil {
				return nil, nil
			}
			info, err := os.Lstat(file.Base().Path)
			if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() != file.Base().Size {
				return nil, nil
			}
		}
	}
	var filled []string
	if content.DateBasis != nil && *content.DateBasis == sourceadapter.TwitterDateBasis {
		date := savedPostDate(content.PublishedAt)
		value, err := json.Marshal(date)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			state, err := repo.MetadataField.State(ctx, item.Media.UUID, "date")
			if err != nil {
				return nil, err
			}
			if date == "" || !metadata.CanFillBlank(state) {
				continue
			}
			_, err = repo.MetadataField.Decide(ctx, models.MetadataFieldDecisionInput{
				EntityUUID: item.Media.UUID, ExpectedEntityRevision: state.Entity.Revision,
				Field: "date", Mode: "inherit", Origin: "review", Value: value,
				Reason: "Reviewed fallback derives the UTC publication date from the X Snowflake ID for source post " + work.PostUUID,
			})
			if err != nil {
				return nil, err
			}
			filled = append(filled, item.Media.UUID)
		}
	}
	schedule := target.EnrichmentSchedule
	schedule.State, schedule.Reason = "excluded", "saved_metadata_sufficient"
	if reviewed {
		schedule.Reason = "reviewed_saved_metadata_accepted"
	}
	_, err = repo.EnrichmentWork.Schedule(ctx, target.UUID, target.Revision, schedule, c.Now())
	return filled, err
}

type SavedEnrichmentResolution struct {
	Target      *models.EnrichmentTarget `json:"target"`
	DatesFilled []string                 `json:"dates_filled"`
}

// AcceptSaved reevaluates an already failed metadata attempt using current local
// evidence. The caller owns the transaction, including notifications. Retrying
// a committed request returns its excluded target without another field write.
func (c *EnrichmentCoordinator) AcceptSaved(ctx context.Context, id string, expected int) (*SavedEnrichmentResolution, error) {
	if !ValidUUID(id) || expected < 1 {
		return nil, models.ErrEnrichmentInvalid
	}
	repo := c.Service.Repo
	target, err := repo.EnrichmentWork.Target(ctx, id)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, ErrNotFound
	}
	result := &SavedEnrichmentResolution{Target: target, DatesFilled: []string{}}
	if target.Revision == expected+1 && target.State == "excluded" &&
		(target.Reason == "saved_metadata_sufficient" || target.Reason == "reviewed_saved_metadata_accepted") {
		return result, nil
	}
	if target.Revision != expected || target.State != "pending" {
		return nil, models.ErrEnrichmentConflict
	}
	binding, err := repo.EnrichmentJob.TargetBinding(ctx, id, expected)
	if err != nil {
		return nil, err
	}
	if binding == nil {
		return nil, models.ErrEnrichmentConflict
	}
	job, err := repo.ArchiveJob.Find(ctx, binding.JobUUID)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, models.ErrEnrichmentConflict
	}
	result.DatesFilled, err = c.excludeUnavailableSavedPost(ctx, job, job.ErrorCode)
	if err != nil {
		return nil, err
	}
	result.Target, err = repo.EnrichmentWork.Target(ctx, id)
	if err == nil && result.Target.State != "excluded" {
		return nil, models.ErrEnrichmentConflict
	}
	return result, err
}
