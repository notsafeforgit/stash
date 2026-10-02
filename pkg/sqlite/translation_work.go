package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

type TranslationWorkStore struct{}

func translationWorkAtomic(ctx context.Context) *bool {
	complete := false
	txn.AddPreCommitHook(ctx, func(context.Context) error {
		if !complete {
			return models.ErrTranslationWorkAtomic
		}
		return nil
	})
	return &complete
}

func validateTranslationRequest(value *models.TranslationRequest) error {
	expected, err := archive.PrepareTranslationRequest(value.TranslationRequestInput)
	if err != nil || expected.UUID != value.UUID || expected.OriginalSHA256 != value.OriginalSHA256 {
		return models.ErrSourcePayloadCorrupt
	}
	return nil
}

func (s *TranslationWorkStore) Request(ctx context.Context, id string) (*models.TranslationRequest, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrTranslationWorkInvalid
	}
	ret := &models.TranslationRequest{}
	err := dbWrapper.Get(ctx, ret, "SELECT * FROM translation_requests WHERE uuid=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ret, validateTranslationRequest(ret)
}

func (s *TranslationWorkStore) RetainRequest(ctx context.Context, input models.TranslationRequestInput) (*models.TranslationRequest, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	prepared, err := archive.PrepareTranslationRequest(input)
	if err != nil {
		return nil, err
	}
	prior, err := s.Request(ctx, prepared.UUID)
	if err != nil || prior != nil {
		return prior, err
	}
	complete := translationWorkAtomic(ctx)
	_, err = dbWrapper.Exec(ctx, `INSERT INTO translation_requests(uuid,original_text,original_sha256,target_language,policy) VALUES(?,?,?,?,?)`,
		prepared.UUID, prepared.OriginalText, prepared.OriginalSHA256, prepared.TargetLanguage, prepared.Policy)
	if err != nil {
		return nil, err
	}
	ret, err := s.Request(ctx, prepared.UUID)
	*complete = err == nil
	return ret, err
}

func (s *TranslationWorkStore) Cache(ctx context.Context, request string) (*models.TranslationCache, error) {
	if !validSourceRunUUID(request) {
		return nil, models.ErrTranslationWorkInvalid
	}
	ret := &models.TranslationCache{}
	err := dbWrapper.Get(ctx, ret, "SELECT * FROM translation_cache WHERE request_uuid=?", request)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	input, err := s.Request(ctx, request)
	if err != nil || input == nil {
		return nil, errors.Join(models.ErrSourcePayloadCorrupt, err)
	}
	var translated *models.SourceTranslation
	if ret.TranslationUUID != nil {
		translated, err = (&SourceTranslationStore{}).Find(ctx, *ret.TranslationUUID)
		if err != nil || translated == nil {
			return nil, errors.Join(models.ErrSourcePayloadCorrupt, err)
		}
	}
	return ret, validateTranslationCache(input, ret, translated)
}

func validateTranslationCache(request *models.TranslationRequest, cached *models.TranslationCache, result *models.SourceTranslation) error {
	input := models.TranslationCacheInput{RequestUUID: request.UUID, Status: cached.Status, CapturedAt: cached.CapturedAt, Origin: cached.Origin}
	if result != nil {
		if result.OriginalText == nil || *result.OriginalText != request.OriginalText || result.TargetLanguage == nil || *result.TargetLanguage != request.TargetLanguage {
			return models.ErrSourcePayloadCorrupt
		}
		input.TranslatedText, input.SourceLanguage, input.Provider = &result.TranslatedText, result.SourceLanguage, result.Provider
	}
	expected, _, err := archive.PrepareTranslationCache(request, input)
	if err != nil || expected.UUID != cached.UUID || !reflect.DeepEqual(expected.TranslationUUID, cached.TranslationUUID) {
		return models.ErrSourcePayloadCorrupt
	}
	return nil
}

func (s *TranslationWorkStore) RetainCache(ctx context.Context, input models.TranslationCacheInput) (*models.TranslationCache, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	request, err := s.Request(ctx, input.RequestUUID)
	if err != nil {
		return nil, err
	}
	prepared, result, err := archive.PrepareTranslationCache(request, input)
	if err != nil {
		return nil, err
	}
	prior, err := s.Cache(ctx, request.UUID)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		if prior.UUID != prepared.UUID {
			return nil, models.ErrTranslationWorkConflict
		}
		return prior, nil
	}
	complete := translationWorkAtomic(ctx)
	if result != nil {
		if _, err := (&SourceTranslationStore{}).Retain(ctx, result.SourceTranslationInput); err != nil {
			return nil, err
		}
	}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO translation_cache(uuid,request_uuid,status,translation_uuid,captured_at,origin) VALUES(?,?,?,?,?,?)`,
		prepared.UUID, prepared.RequestUUID, prepared.Status, prepared.TranslationUUID, prepared.CapturedAt, prepared.Origin)
	if err != nil {
		return nil, err
	}
	ret, err := s.Cache(ctx, request.UUID)
	*complete = err == nil
	return ret, err
}

func (s *TranslationWorkStore) Target(ctx context.Context, id string) (*models.TranslationTarget, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrTranslationWorkInvalid
	}
	ret := &models.TranslationTarget{}
	err := dbWrapper.Get(ctx, ret, "SELECT * FROM translation_targets WHERE uuid=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return ret, err
}

func prepareTranslationSchedule(input models.TranslationTargetSchedule, now time.Time) (models.TranslationTargetSchedule, error) {
	if input.NotBefore.IsZero() {
		input.NotBefore = now
	}
	input.NotBefore = input.NotBefore.UTC()
	if (input.State != "held" && input.State != "pending") || input.Priority < 0 || input.Priority > 100 || !validJobTime(input.NotBefore) || !validJobTime(now) {
		return input, models.ErrTranslationWorkInvalid
	}
	return input, nil
}

func (s *TranslationWorkStore) RetainTarget(ctx context.Context, input models.TranslationTargetInput, schedule models.TranslationTargetSchedule, now time.Time) (*models.TranslationTarget, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	id, err := archive.TranslationTargetIdentity(input)
	if err != nil {
		return nil, err
	}
	schedule, err = prepareTranslationSchedule(schedule, now)
	if err != nil {
		return nil, err
	}
	prior, err := s.Target(ctx, id)
	if err != nil || prior != nil {
		return prior, err
	}
	if _, err := activePostLink(ctx, input.PostUUID); err != nil {
		return nil, err
	}
	request, err := s.Request(ctx, input.RequestUUID)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, models.ErrTranslationWorkInvalid
	}
	if input.CollectionUUID != nil {
		var exists bool
		if err := dbWrapper.Get(ctx, &exists, "SELECT EXISTS(SELECT 1 FROM source_collection_revisions WHERE collection_uuid=? AND revision=?)", *input.CollectionUUID, *input.CollectionRevision); err != nil {
			return nil, err
		}
		if !exists {
			return nil, models.ErrTranslationWorkInvalid
		}
	}
	complete := translationWorkAtomic(ctx)
	_, err = dbWrapper.Exec(ctx, `INSERT INTO translation_targets(uuid,request_uuid,post_uuid,collection_uuid,collection_revision,field,origin,state,priority,not_before,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, input.RequestUUID, input.PostUUID, input.CollectionUUID, input.CollectionRevision, input.Field, input.Origin,
		schedule.State, schedule.Priority, schedule.NotBefore, now.UTC(), now.UTC())
	if err != nil {
		return nil, err
	}
	ret, err := s.Target(ctx, id)
	*complete = err == nil
	return ret, err
}

func (s *TranslationWorkStore) ScheduleTarget(ctx context.Context, id string, expected int, schedule models.TranslationTargetSchedule, now time.Time) (*models.TranslationTarget, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	schedule, err := prepareTranslationSchedule(schedule, now)
	if err != nil {
		return nil, err
	}
	prior, err := s.Target(ctx, id)
	if err != nil {
		return nil, err
	}
	if prior == nil || prior.Revision != expected || (prior.State != "held" && prior.State != "pending") || now.Before(prior.UpdatedAt) {
		return nil, models.ErrTranslationWorkConflict
	}
	if schedule.State == prior.State && schedule.Priority == prior.Priority && schedule.NotBefore.Equal(prior.NotBefore) {
		return prior, nil
	}
	complete := translationWorkAtomic(ctx)
	_, err = dbWrapper.Exec(ctx, `UPDATE translation_targets SET state=?,priority=?,not_before=?,revision=revision+1,updated_at=? WHERE uuid=?`, schedule.State, schedule.Priority, schedule.NotBefore, now.UTC(), id)
	if err != nil {
		return nil, err
	}
	ret, err := s.Target(ctx, id)
	*complete = err == nil
	return ret, err
}

// Publication is domain evidence, not a scene/image metadata edit. Callers
// performing scheduled work also fence this transaction with their job lease.
func (s *TranslationWorkStore) PublishTarget(ctx context.Context, id string, expected int, now time.Time) (*models.TranslationTarget, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validJobTime(now) {
		return nil, models.ErrTranslationWorkInvalid
	}
	target, err := s.Target(ctx, id)
	if err != nil {
		return nil, err
	}
	if target == nil || target.Revision != expected {
		return nil, models.ErrTranslationWorkConflict
	}
	if target.State == "completed" || target.State == "review" {
		return target, nil
	}
	if target.State != "pending" || now.Before(target.NotBefore) || now.Before(target.UpdatedAt) {
		return nil, models.ErrTranslationWorkConflict
	}
	cache, err := s.Cache(ctx, target.RequestUUID)
	if err != nil {
		return nil, err
	}
	_, err = activePostLink(ctx, target.PostUUID)
	state, reason := "completed", ""
	if errors.Is(err, models.ErrSourcePostForgotten) {
		state, reason = "review", "post_forgotten"
	} else if err != nil {
		return nil, err
	}
	if state == "completed" && cache == nil {
		return nil, models.ErrTranslationWorkConflict
	}
	complete := translationWorkAtomic(ctx)
	var cacheID *string
	if cache != nil {
		cacheID = &cache.UUID
	}
	var evidenceID *string
	if state == "completed" && cache.TranslationUUID != nil {
		details, err := json.Marshal(map[string]string{"request_uuid": target.RequestUUID, "target_uuid": target.UUID, "cache_uuid": cache.UUID, "field": target.Field})
		if err != nil {
			return nil, err
		}
		evidence, err := (&SourceTranslationStore{}).RecordEvidence(ctx, models.SourceTranslationEvidence{
			UUID:            uuid.NewSHA1(uuid.MustParse(target.UUID), []byte("translation-cache:"+cache.UUID)).String(),
			TranslationUUID: *cache.TranslationUUID, PostUUID: target.PostUUID, CollectionUUID: target.CollectionUUID, CollectionRevision: target.CollectionRevision,
			Provenance: "native-translation:" + target.Field, CapturedAt: cache.CapturedAt, Origin: "worker", Details: details})
		if err != nil {
			return nil, err
		}
		evidenceID = &evidence.UUID
	}
	_, err = dbWrapper.Exec(ctx, `UPDATE translation_targets SET state=?,cache_uuid=?,evidence_uuid=?,reason=?,revision=revision+1,updated_at=? WHERE uuid=?`,
		state, cacheID, evidenceID, reason, now.UTC(), id)
	if err != nil {
		return nil, err
	}
	ret, err := s.Target(ctx, id)
	*complete = err == nil
	return ret, err
}

func (s *TranslationWorkStore) Targets(ctx context.Context, q models.TranslationTargetQuery) ([]models.TranslationTarget, error) {
	if (q.RequestUUID == "") == (q.PostUUID == "") || (q.RequestUUID != "" && !validSourceRunUUID(q.RequestUUID)) ||
		(q.PostUUID != "" && !validSourceRunUUID(q.PostUUID)) || (q.After != "" && !validSourceRunUUID(q.After)) ||
		(q.State != "" && q.State != "held" && q.State != "pending" && q.State != "completed" && q.State != "review") {
		return nil, models.ErrTranslationWorkInvalid
	}
	limit, err := sourcePageLimit(q.Limit)
	if err != nil {
		return nil, models.ErrTranslationWorkInvalid
	}
	column, id := "request_uuid", q.RequestUUID
	if q.PostUUID != "" {
		column, id = "post_uuid", q.PostUUID
	}
	where, args := column+"=? AND uuid>?", []any{id, q.After}
	if q.State != "" {
		where += " AND state=?"
		args = append(args, q.State)
	}
	args = append(args, limit)
	ret := []models.TranslationTarget{}
	err = dbWrapper.Select(ctx, &ret, "SELECT * FROM translation_targets WHERE "+where+" ORDER BY uuid LIMIT ?", args...)
	return ret, err
}

func (s *TranslationWorkStore) TargetHistory(ctx context.Context, id string, after, limit int) ([]models.TranslationTargetHistory, error) {
	if !validSourceRunUUID(id) || after < 0 {
		return nil, models.ErrTranslationWorkInvalid
	}
	limit, err := sourcePageLimit(limit)
	if err != nil {
		return nil, models.ErrTranslationWorkInvalid
	}
	ret := []models.TranslationTargetHistory{}
	err = dbWrapper.Select(ctx, &ret, "SELECT * FROM translation_target_history WHERE target_uuid=? AND revision>? ORDER BY revision LIMIT ?", id, after, limit)
	return ret, err
}
