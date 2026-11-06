package ingest

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
)

// SavedFilePost is server-selected, immutable admission evidence. It preserves
// an existing post without inventing a fresh capture or attachment order.
type SavedFilePost struct {
	PostUUID string                    `json:"post_uuid"`
	Evidence []string                  `json:"evidence"`
	Metadata models.SourcePostMetadata `json:"metadata"`
	URLs     []string                  `json:"urls"`
}

func savedFilePosts(ctx context.Context, repo models.Repository, target FileTarget, size int64) ([]SavedFilePost, error) {
	observations, err := repo.SourceFile.LocationObservations(ctx, target.RootUUID, nil, target.RelativePath, "", 100)
	if err != nil {
		return nil, err
	}
	if len(observations) == 100 {
		return nil, ErrManualFileChanged
	}
	posts := map[string]*SavedFilePost{}
	for _, observation := range observations {
		if observation.State != "present" || observation.Role != "local" || observation.Size == nil || *observation.Size != size {
			continue
		}
		evidence, err := repo.SourceFile.ObservationPosts(ctx, observation.UUID, "", 100)
		if err != nil {
			return nil, err
		}
		if len(evidence) == 100 {
			return nil, ErrManualFileChanged
		}
		for _, item := range evidence {
			identity, err := repo.SourceEvidence.PostIdentity(ctx, item.PostUUID)
			if err != nil {
				return nil, err
			}
			if identity == nil {
				return nil, ErrManualFileChanged
			}
			post, err := repo.SourceEvidence.FindPost(ctx, identity.CanonicalUUID)
			if err != nil {
				return nil, err
			}
			if post == nil || post.State != "active" {
				return nil, ErrManualFileChanged
			}
			saved := posts[post.UUID]
			if saved == nil {
				content, err := repo.SourceEvidence.PostContent(ctx, post.UUID)
				if err != nil {
					return nil, err
				}
				if content == nil {
					continue
				}
				urls, err := repo.SourcePostLinks.CurrentURLs(ctx, post.UUID, "", 100)
				if err != nil {
					return nil, err
				}
				if len(urls) == 100 {
					return nil, ErrManualFileChanged
				}
				saved = &SavedFilePost{PostUUID: post.UUID, Metadata: *content, URLs: []string{}}
				for _, url := range urls {
					saved.URLs = append(saved.URLs, url.URL)
				}
				sort.Strings(saved.URLs)
				posts[post.UUID] = saved
				if len(posts) > 32 {
					return nil, ErrManualFileChanged
				}
			}
			saved.Evidence = append(saved.Evidence, item.UUID)
			if len(saved.Evidence) > 100 {
				return nil, ErrManualFileChanged
			}
		}
	}
	var result []SavedFilePost
	for _, post := range posts {
		sort.Strings(post.Evidence)
		result = append(result, *post)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].PostUUID < result[j].PostUUID })
	return result, nil
}

func savedPostDate(value *string) string {
	if value == nil {
		return ""
	}
	for _, format := range []string{time.RFC3339Nano, "2006-01-02"} {
		if parsed, err := time.Parse(format, *value); err == nil && parsed.Year() > 0 {
			return parsed.Format("2006-01-02")
		}
	}
	return ""
}

func applySavedFilePosts(ctx context.Context, repo models.Repository, input IntakePublication, ret *PublishedIntake) error {
	if len(input.SavedPosts) == 0 {
		return nil
	}
	mediaID := ret.Result.MediaUUID
	// Fill all fields before gallery synchronization establishes commit guards.
	for _, saved := range input.SavedPosts {
		choice, err := repo.SourcePostMedia.Association(ctx, saved.PostUUID, mediaID)
		if err != nil {
			return err
		}
		if choice.Suppressed() {
			return models.ErrSourcePostMediaConflict
		}
		date := savedPostDate(saved.Metadata.PublishedAt)
		for _, item := range []struct {
			field string
			value *string
		}{
			{"title", saved.Metadata.Title}, {"details", saved.Metadata.OriginalText}, {"date", &date},
		} {
			if item.value == nil || strings.TrimSpace(*item.value) == "" {
				continue
			}
			state, err := repo.MetadataField.State(ctx, mediaID, item.field)
			if err != nil {
				return err
			}
			filename := item.field == "title" && state.Origin == "filename" && !state.Pending && state.Decision == nil
			if !metadata.CanFillBlank(state) && !filename {
				continue
			}
			value, err := json.Marshal(*item.value)
			if err != nil {
				return err
			}
			if _, err := repo.MetadataField.Decide(ctx, models.MetadataFieldDecisionInput{
				EntityUUID: mediaID, ExpectedEntityRevision: state.Entity.Revision, Field: item.field,
				Mode: "set", Value: value, Origin: "review", Reason: "Reviewed duplicate intake: fill blanks from retained exact-path source metadata.",
			}); err != nil {
				return err
			}
			ret.Result.MetadataFields = append(ret.Result.MetadataFields, item.field)
		}
		state, err := repo.MetadataField.State(ctx, mediaID, "urls")
		if err != nil {
			return err
		}
		if state.Mode == "clear" || state.Pending {
			continue
		}
		var urls []string
		if err := json.Unmarshal(state.Value, &urls); err != nil {
			return err
		}
		before := len(urls)
		for _, url := range saved.URLs {
			if !slices.Contains(urls, url) {
				urls = append(urls, url)
			}
		}
		if len(urls) != before {
			value, err := json.Marshal(urls)
			if err != nil {
				return err
			}
			if _, err := repo.MetadataField.Decide(ctx, models.MetadataFieldDecisionInput{
				EntityUUID: mediaID, ExpectedEntityRevision: state.Entity.Revision, Field: "urls",
				Mode: "set", Value: value, Origin: "review", Reason: "Reviewed duplicate intake: preserve retained source URLs.",
			}); err != nil {
				return err
			}
			ret.Result.MetadataFields = append(ret.Result.MetadataFields, "urls")
		}
	}
	for _, saved := range input.SavedPosts {
		choice, err := repo.SourcePostMedia.Association(ctx, saved.PostUUID, mediaID)
		if err != nil {
			return err
		}
		if choice.Suppressed() {
			return models.ErrSourcePostMediaConflict
		}
		if choice.State == "linked" {
			continue
		}
		decisions := []string{}
		for _, decision := range choice.Decisions {
			decisions = append(decisions, decision.UUID)
		}
		if _, err := repo.SourcePostMedia.Decide(ctx, models.SourcePostMediaInput{
			UUID:     uuid.NewSHA1(uuid.MustParse(input.UUID), []byte("saved-post/"+saved.PostUUID)).String(),
			PostUUID: saved.PostUUID, MediaUUID: mediaID, ExpectedPostRevision: choice.PostRevision,
			ExpectedMediaRevision: choice.MediaRevision, ExpectedDecisions: decisions, State: "linked", Origin: "review",
			Reason: "Reviewed equal-byte duplicate intake: preserve the retained exact-path post association.",
		}); err != nil {
			return err
		}
	}
	ret.Result.SourceMedia = "linked"
	sort.Strings(ret.Result.MetadataFields)
	ret.Result.MetadataFields = slices.Compact(ret.Result.MetadataFields)
	return nil
}
