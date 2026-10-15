package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	encodinghex "encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sourceadapter"
)

func originalPostDisplay(platform string, metadata models.SourcePostMetadata) *models.SourcePostDisplay {
	title, truncated := sourceadapter.PostTitle(platform, metadata, 512)
	if truncated {
		title += "…"
	}
	ret := &models.SourcePostDisplay{Title: title}
	if metadata.OriginalText != nil {
		ret.Description = *metadata.OriginalText
	}
	return ret
}

// Display is a projection. Never write translated text into retained metadata.
// A result must match the exact source bytes and the chosen target language.
func (s *SourceTranslationStore) Display(ctx context.Context, post, platform string, metadata models.SourcePostMetadata) (*models.SourcePostDisplay, error) {
	var language string
	err := dbWrapper.Get(ctx, &language, `WITH root AS (SELECT canonical_uuid FROM source_post_identities WHERE post_uuid=?)
SELECT d.target_language FROM root JOIN source_post_translation_display d ON d.post_uuid=root.canonical_uuid
UNION ALL
SELECT min(d.target_language) FROM root JOIN source_post_identities i ON i.canonical_uuid=root.canonical_uuid
JOIN source_post_translation_display d ON d.post_uuid=i.post_uuid
WHERE NOT EXISTS(SELECT 1 FROM source_post_translation_display selected WHERE selected.post_uuid=root.canonical_uuid)
HAVING count(*)>0 AND min(d.target_language)=max(d.target_language) LIMIT 1`, post)
	if errors.Is(err, sql.ErrNoRows) {
		return originalPostDisplay(platform, metadata), nil
	}
	if err != nil {
		return nil, err
	}
	translated := false
	for _, field := range []**string{&metadata.Title, &metadata.OriginalText} {
		if *field == nil || strings.TrimSpace(**field) == "" {
			continue
		}
		value, err := translationForText(ctx, post, **field, language)
		if err != nil {
			return nil, err
		}
		if value != nil && value.TranslatedText != **field {
			text := value.TranslatedText
			*field, translated = &text, true
		}
	}
	ret := originalPostDisplay(platform, metadata)
	ret.Language, ret.Translated = language, translated
	return ret, nil
}

func translationForText(ctx context.Context, post, original, language string) (*models.SourceTranslation, error) {
	digest := sha256.Sum256([]byte(original))
	var id string
	err := dbWrapper.Get(ctx, &id, `SELECT t.uuid FROM source_post_identities root
JOIN source_post_identities i ON i.canonical_uuid=root.canonical_uuid
JOIN source_translation_evidence e ON e.post_uuid=i.post_uuid
JOIN source_translations t ON t.uuid=e.translation_uuid
WHERE root.post_uuid=? AND t.original_sha256=? AND t.target_language=? AND t.original_text=?
ORDER BY coalesce(julianday(nullif(e.captured_at,'')),julianday(e.recorded_at)) DESC,e.uuid DESC LIMIT 1`, post, encodinghex.EncodeToString(digest[:]), language, original)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return (&SourceTranslationStore{}).Find(ctx, id)
}

func queueTranslationDisplay(ctx context.Context, post, language string, adoptBefore *time.Time) error {
	canonical, err := currentSourcePost(ctx, post)
	if err != nil {
		return err
	}
	if canonical == nil || canonical.State != "active" {
		return models.ErrSourcePostForgotten
	}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO source_post_translation_display(post_uuid,target_language,adopt_before)
VALUES(?,?,?) ON CONFLICT(post_uuid) DO UPDATE SET target_language=excluded.target_language,pending=1,after_entity='',
adopt_before=coalesce(source_post_translation_display.adopt_before,excluded.adopt_before)`, canonical.UUID, language, adoptBefore)
	return err
}

// Explicit bounded backfill uses already captured text, not website requests.
// Cached requests publish immediately; uncached requests join the normal worker.
func (s *SourceTranslationStore) Backfill(ctx context.Context, input models.TranslationBackfillInput, now time.Time) ([]models.TranslationBackfillResult, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if len(input.Posts) < 1 || len(input.Posts) > 100 || input.Priority < 0 || input.Priority > 100 || (input.AdoptMatchingText && !input.ApplyToLibrary) {
		return nil, models.ErrTranslationWorkInvalid
	}
	if _, err := archive.PrepareTranslationRequest(models.TranslationRequestInput{TargetLanguage: input.TargetLanguage, Policy: input.ProviderPolicy}); err != nil {
		return nil, err
	}
	complete := translationWorkAtomic(ctx)
	ret := make([]models.TranslationBackfillResult, 0, len(input.Posts))
	for _, id := range input.Posts {
		post, err := currentSourcePost(ctx, id)
		if err != nil {
			return nil, err
		}
		if post == nil || post.State != "active" {
			return nil, models.ErrSourcePostForgotten
		}
		metadata, err := (&SourceEvidenceStore{}).PostContent(ctx, post.UUID)
		if err != nil {
			return nil, err
		}
		result := models.TranslationBackfillResult{PostUUID: post.UUID, Status: "no_text"}
		if input.ApplyToLibrary {
			var adopt *time.Time
			if input.AdoptMatchingText {
				adopt = &now
			}
			if err := queueTranslationDisplay(ctx, post.UUID, input.TargetLanguage, adopt); err != nil {
				return nil, err
			}
		}
		if metadata != nil {
			text, field := metadata.OriginalText, "caption"
			if text == nil || strings.TrimSpace(*text) == "" {
				text, field = metadata.Title, "title"
			}
			if text != nil && strings.TrimSpace(*text) != "" {
				// Imported/completed post evidence already authorizes this exact
				// result. Do not manufacture another target and identical capture.
				retained, err := translationForText(ctx, post.UUID, *text, input.TargetLanguage)
				if err != nil {
					return nil, err
				}
				if retained != nil {
					result.Status = "completed"
					ret = append(ret, result)
					continue
				}
				work := &TranslationWorkStore{}
				request, err := work.RetainRequest(ctx, models.TranslationRequestInput{OriginalText: *text, TargetLanguage: input.TargetLanguage, Policy: input.ProviderPolicy})
				if err != nil {
					return nil, err
				}
				target, err := work.RetainTarget(ctx, models.TranslationTargetInput{RequestUUID: request.UUID, PostUUID: post.UUID, Field: field, Origin: "review"}, models.TranslationTargetSchedule{State: "pending", Priority: input.Priority}, now)
				if err != nil {
					return nil, err
				}
				cache, err := work.Cache(ctx, request.UUID)
				if err != nil {
					return nil, err
				}
				if cache != nil && target.State == "pending" && !now.Before(target.NotBefore) {
					target, err = work.PublishTarget(ctx, target.UUID, target.Revision, now)
					if err != nil {
						return nil, err
					}
				}
				result.Status, result.TargetUUID = target.State, target.UUID
			}
		}
		ret = append(ret, result)
	}
	*complete = true
	return ret, nil
}

// The queue and cursor commit with field changes. A restart resumes the next
// page; no translation or full-library scan is repeated to propagate a result.
func (s *SourceTranslationStore) RefreshPending(ctx context.Context, limit int) (count int, retErr error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return 0, err
	}
	if limit < 1 || limit > 100 {
		return 0, models.ErrTranslationWorkInvalid
	}
	complete := translationWorkAtomic(ctx)
	defer func() { *complete = retErr == nil }()
	var pending struct {
		Post        string        `db:"post_uuid"`
		After       string        `db:"after_entity"`
		Language    string        `db:"target_language"`
		AdoptBefore NullTimestamp `db:"adopt_before"`
	}
	err := dbWrapper.Get(ctx, &pending, `SELECT post_uuid,target_language,after_entity,adopt_before FROM source_post_translation_display
WHERE pending=1 ORDER BY post_uuid LIMIT 1`)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	post, err := currentSourcePost(ctx, pending.Post)
	if err != nil {
		return 0, err
	}
	if post == nil || post.State != "active" || post.UUID != pending.Post {
		if post != nil && post.State == "active" && post.UUID != pending.Post {
			// A merged post keeps the canonical preference when present. Carry
			// its pending work over without changing either source text body.
			if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_post_translation_display(post_uuid,target_language,adopt_before)
VALUES(?,?,?) ON CONFLICT(post_uuid) DO UPDATE SET pending=1,after_entity=''`, post.UUID, pending.Language, pending.AdoptBefore.TimePtr()); err != nil {
				return 0, err
			}
		}
		_, err = dbWrapper.Exec(ctx, "UPDATE source_post_translation_display SET pending=0 WHERE post_uuid=?", pending.Post)
		return 1, err
	}
	media, err := (&SourcePostMediaStore{}).MediaForPost(ctx, post.UUID, pending.After, limit)
	if err != nil {
		return 0, err
	}
	ids := make([]string, 0, len(media)+1)
	for _, item := range media {
		if item.Media.State == models.ArchiveEntityActive && !item.Association.Suppressed() &&
			(item.Association.State == "linked" || item.HasRetainedEvidence || item.LinkedAttachments > 0) {
			ids = append(ids, item.Media.UUID)
		}
	}
	// Gallery links are indexed by post and include consolidated albums.
	var galleries []string
	if err := dbWrapper.Select(ctx, &galleries, `SELECT DISTINCT l.gallery_uuid FROM source_post_identities i
JOIN post_gallery_links l ON l.post_uuid=i.post_uuid JOIN archive_entities a ON a.uuid=l.gallery_uuid AND a.state='active'
JOIN galleries g ON g.id=a.gallery_id AND g.origin='source'
WHERE i.canonical_uuid=? AND l.gallery_uuid>? ORDER BY l.gallery_uuid LIMIT ?`, post.UUID, pending.After, limit); err != nil {
		return 0, err
	}
	ids = append(ids, galleries...)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	// Progress includes skipped/deleted media so a full page cannot loop.
	next := ""
	if len(media) == limit {
		next = media[len(media)-1].Media.UUID
	}
	if len(galleries) == limit && (next == "" || galleries[len(galleries)-1] < next) {
		next = galleries[len(galleries)-1]
	}
	if len(ids) > limit && (next == "" || ids[limit-1] < next) {
		next = ids[limit-1]
	}
	processed := 0
	for _, id := range ids {
		if next != "" && id > next {
			break
		}
		if err := s.refreshEntityText(ctx, post.UUID, id, pending.AdoptBefore.TimePtr()); err != nil {
			return 0, err
		}
		processed++
	}
	_, err = dbWrapper.Exec(ctx, `UPDATE source_post_translation_display SET after_entity=?,pending=? WHERE post_uuid=?`, next, next != "", pending.Post)
	return processed + 1, err
}

func (s *SourceTranslationStore) refreshEntityText(ctx context.Context, post, entity string, adoptBefore *time.Time) error {
	fields := &MetadataFieldStore{}
	for _, field := range []string{"title", "details"} {
		state, err := fields.State(ctx, entity, field)
		if err != nil {
			return err
		}
		if state.Mode == "clear" || state.Entity.State != models.ArchiveEntityActive {
			continue
		}
		var value string
		if json.Unmarshal(state.Value, &value) != nil {
			continue
		}
		capture := ""
		if state.Decision != nil && state.Decision.CaptureUUID != nil {
			capture = *state.Decision.CaptureUUID
		}
		adopt := state.Protected || capture == ""
		if adopt {
			if adoptBefore == nil || (state.Decision != nil && (state.Decision.CreatedAt.After(*adoptBefore) || state.Origin == "review")) {
				continue
			}
			// Only this explicit backfill may adopt a previously untracked field.
			if err := dbWrapper.Get(ctx, &capture, translationDisplayCaptureQuery, post); err != nil {
				return err
			}
		}
		owner, platform, original, err := sourceGalleryCaptureMetadata(ctx, capture)
		if err != nil {
			return err
		}
		canonical, err := currentSourcePost(ctx, owner)
		if err != nil {
			return err
		}
		if canonical == nil || canonical.UUID != post {
			continue
		}
		// Even an inherited custom expression is not replaced arbitrarily.
		if !sourceadapter.MatchesPostText(value, field, platform, original) {
			matched := false
			if adoptBefore != nil && field == "title" && (platform == "twitter" || platform == "x") {
				matched, err = matchesTwitterDownloadTitle(ctx, post, state.Entity, value)
				if err != nil {
					return err
				}
			}
			if !matched {
				continue
			}
		}
		display, err := s.Display(ctx, post, platform, original)
		if err != nil {
			return err
		}
		if !display.Translated {
			continue
		}
		replacement := display.Title
		if field == "details" {
			replacement = display.Description
		}
		if replacement == "" || replacement == value {
			continue
		}
		encoded, err := json.Marshal(replacement)
		if err != nil {
			return err
		}
		input := models.MetadataFieldDecisionInput{EntityUUID: entity, ExpectedEntityRevision: state.Entity.Revision,
			Field: field, Mode: "inherit", Origin: "source", CaptureUUID: capture, Value: encoded, Reason: "Translated inherited source text"}
		if adopt {
			input.Origin, input.Reason = "review", "Translate matching captured text during explicit backfill"
			_, err = fields.Decide(ctx, input)
		} else {
			input.Policy = state.Decision.Policy
			input.PostMediaDecisionUUID = state.Decision.PostMediaDecisionUUID
			_, err = fields.ApplyAutomatic(ctx, input)
		}
		if err != nil {
			return err
		}
	}
	return s.refreshAlternateTitles(ctx, post, entity)
}

const translationDisplayCaptureQuery = `SELECT c.uuid FROM source_post_identities i
JOIN source_captures c ON c.post_uuid=i.post_uuid JOIN source_post_revisions r ON r.uuid=c.revision_uuid
WHERE i.canonical_uuid=? ORDER BY (length(coalesce(json_extract(r.metadata,'$.original_text'),''))>0) DESC,
coalesce(c.captured_at,c.recorded_at) DESC,c.uuid DESC LIMIT 1`

func matchesTwitterDownloadTitle(ctx context.Context, post string, entity *models.ArchiveEntity, title string) (bool, error) {
	if entity.Kind == models.ArchiveGallery {
		fallback, err := sourcePostFallbackTitle(ctx, post, "twitter")
		return title == fallback || title == "Source album", err
	}
	var identifiers []string
	if err := dbWrapper.Select(ctx, &identifiers, `SELECT p.value FROM source_post_identities i
JOIN source_post_identifiers p ON p.post_uuid=i.post_uuid WHERE i.canonical_uuid=? AND p.namespace='native:twitter'`, post); err != nil {
		return false, err
	}
	table, column, err := metadataFileOwnerTable(entity.Kind)
	if err != nil {
		return false, err
	}
	var names []string
	if err := dbWrapper.Select(ctx, &names, `SELECT f.basename FROM `+table+` m JOIN files f ON f.id=m.file_id WHERE m.`+column+`=?`, *entity.LocalID); err != nil {
		return false, err
	}
	for _, name := range names {
		if title != name && title != strings.TrimSuffix(name, filepath.Ext(name)) {
			continue
		}
		for _, id := range identifiers {
			if name == id || strings.HasPrefix(name, id+"_") || strings.HasPrefix(name, id+".") {
				return true, nil
			}
		}
	}
	return false, nil
}

func (s *SourceTranslationStore) refreshAlternateTitles(ctx context.Context, post, entity string) error {
	owner, err := (&ArchiveEntityStore{}).Find(ctx, entity)
	if err != nil || owner == nil || owner.Kind != models.ArchiveGallery {
		return err
	}
	var capture string
	err = dbWrapper.Get(ctx, &capture, translationDisplayCaptureQuery, post)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, platform, original, err := sourceGalleryCaptureMetadata(ctx, capture)
	if err != nil {
		return err
	}
	display, err := s.Display(ctx, post, platform, original)
	if err != nil || !display.Translated || display.Title == "" {
		return err
	}
	candidates := []string{originalPostDisplay(platform, original).Title}
	if original.Title != nil {
		candidates = append(candidates, *original.Title)
	}
	if original.OriginalText != nil {
		line, _, _ := strings.Cut(strings.ReplaceAll(*original.OriginalText, "\\n", "\n"), "\n")
		candidates = append(candidates, strings.TrimSpace(line))
	}
	for _, title := range candidates {
		if title == "" || title == display.Title {
			continue
		}
		result, err := dbWrapper.Exec(ctx, "DELETE FROM gallery_alternate_titles WHERE gallery_id=? AND title=?", *owner.LocalID, title)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count > 0 {
			if _, err := dbWrapper.Exec(ctx, "INSERT OR IGNORE INTO gallery_alternate_titles(gallery_id,title) VALUES(?,?)", *owner.LocalID, display.Title); err != nil {
				return err
			}
		}
	}
	return nil
}
