package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

type TranslationPolicyStore struct{}

type translationPolicyRow struct {
	CollectionUUID     string    `db:"collection_uuid"`
	Revision           int       `db:"revision"`
	CollectionRevision int       `db:"collection_revision"`
	Definition         string    `db:"definition"`
	Origin             string    `db:"origin"`
	Reason             string    `db:"reason"`
	CreatedAt          time.Time `db:"created_at"`
}

func (r translationPolicyRow) resolve() (*models.TranslationPolicy, error) {
	definition, err := archive.DecodeTranslationPolicy([]byte(r.Definition))
	if err != nil {
		return nil, err
	}
	return &models.TranslationPolicy{CollectionUUID: r.CollectionUUID, Revision: r.Revision, CollectionRevision: r.CollectionRevision,
		Definition: definition, Origin: r.Origin, Reason: r.Reason, CreatedAt: r.CreatedAt}, nil
}

func (s *TranslationPolicyStore) Find(ctx context.Context, collection string) (*models.TranslationPolicy, error) {
	if !validSourceRunUUID(collection) {
		return nil, models.ErrTranslationPolicyInvalid
	}
	var row translationPolicyRow
	err := dbWrapper.Get(ctx, &row, `SELECT r.* FROM translation_policies p JOIN translation_policy_revisions r
 ON r.collection_uuid=p.collection_uuid AND r.revision=p.revision WHERE p.collection_uuid=?`, collection)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return row.resolve()
}

func (s *TranslationPolicyStore) History(ctx context.Context, collection string, after, limit int) ([]*models.TranslationPolicy, error) {
	limit, err := sourcePageLimit(limit)
	if err != nil || !validSourceRunUUID(collection) || after < 0 {
		return nil, models.ErrTranslationPolicyInvalid
	}
	var rows []translationPolicyRow
	if err := dbWrapper.Select(ctx, &rows, `SELECT * FROM translation_policy_revisions WHERE collection_uuid=? AND revision>? ORDER BY revision LIMIT ?`, collection, after, limit); err != nil {
		return nil, err
	}
	ret := make([]*models.TranslationPolicy, 0, len(rows))
	for _, row := range rows {
		policy, err := row.resolve()
		if err != nil {
			return nil, err
		}
		ret = append(ret, policy)
	}
	return ret, nil
}

func (s *TranslationPolicyStore) Put(ctx context.Context, input models.TranslationPolicyInput) (*models.TranslationPolicy, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validSourceRunUUID(input.CollectionUUID) || input.ExpectedRevision < 0 || input.ExpectedCollectionRevision <= 0 ||
		(input.Origin != "review" && input.Origin != "migration") || !validAccountText(input.Reason, 4096, true) || archive.ValidateTranslationPolicy(input.Definition) != nil {
		return nil, models.ErrTranslationPolicyInvalid
	}
	collection, err := (&SourceCollectionStore{}).Find(ctx, input.CollectionUUID)
	if err != nil {
		return nil, err
	}
	if collection == nil || collection.State == "retired" || collection.Revision != input.ExpectedCollectionRevision {
		return nil, models.ErrTranslationPolicyConflict
	}
	prior, err := s.Find(ctx, collection.UUID)
	if err != nil {
		return nil, err
	}
	if (prior == nil && input.ExpectedRevision != 0) || (prior != nil && prior.Revision != input.ExpectedRevision) {
		return nil, models.ErrTranslationPolicyConflict
	}
	if prior != nil && prior.CollectionRevision == collection.Revision && prior.Definition == input.Definition {
		return prior, nil
	}
	definition, err := json.Marshal(input.Definition)
	if err != nil {
		return nil, err
	}
	complete := translationWorkAtomic(ctx)
	if prior == nil {
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO translation_policies(collection_uuid) VALUES(?)", collection.UUID); err != nil {
			return nil, err
		}
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO translation_policy_revisions(collection_uuid,revision,collection_revision,definition,origin,reason)
 VALUES(?,?,?,?,?,?)`, collection.UUID, input.ExpectedRevision+1, collection.Revision, string(definition), input.Origin, input.Reason); err != nil {
		return nil, err
	}
	ret, err := s.Find(ctx, collection.UUID)
	*complete = err == nil
	return ret, err
}

func validCaptureTranslationScope(input models.CollectionCapture) bool {
	return validSourceRunUUID(input.CaptureUUID) && validSourceRunUUID(input.CollectionUUID) && input.CollectionRevision > 0
}

func (s *TranslationPolicyStore) CaptureDecision(ctx context.Context, input models.CollectionCapture) (*models.CaptureTranslationDecision, error) {
	if !validCaptureTranslationScope(input) {
		return nil, models.ErrTranslationPolicyInvalid
	}
	var row struct {
		PolicyRevision *int      `db:"policy_revision"`
		Status         string    `db:"status"`
		Count          int       `db:"entry_count"`
		CreatedAt      time.Time `db:"created_at"`
	}
	err := dbWrapper.Get(ctx, &row, `SELECT policy_revision,status,entry_count,created_at FROM capture_translation_decisions
 WHERE collection_uuid=? AND capture_uuid=? AND collection_revision=?`, input.CollectionUUID, input.CaptureUUID, input.CollectionRevision)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	input.CreatedAt = row.CreatedAt
	ret := &models.CaptureTranslationDecision{CollectionCapture: input, PolicyRevision: row.PolicyRevision, Status: row.Status, Entries: []models.CaptureTranslationEntry{}}
	if err := dbWrapper.Select(ctx, &ret.Entries, `SELECT field,status,target_uuid,target_revision FROM capture_translation_entries
 WHERE collection_uuid=? AND capture_uuid=? AND collection_revision=? ORDER BY field`, input.CollectionUUID, input.CaptureUUID, input.CollectionRevision); err != nil {
		return nil, err
	}
	if row.Count != len(ret.Entries) || !validJobTime(row.CreatedAt) {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return ret, nil
}

// ScheduleCapture runs in the same transaction as acceptance of source
// evidence. It queues domain work only; provider admission/publication is later.
func (s *TranslationPolicyStore) ScheduleCapture(ctx context.Context, input models.CollectionCapture, now time.Time) (*models.CaptureTranslationDecision, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validCaptureTranslationScope(input) || !validJobTime(now) {
		return nil, models.ErrTranslationPolicyInvalid
	}
	prior, err := s.CaptureDecision(ctx, input)
	if err != nil || prior != nil {
		return prior, err
	}
	present, err := (&SourceCollectionStore{}).HasCapture(ctx, input)
	if err != nil {
		return nil, err
	}
	if !present {
		return nil, models.ErrTranslationPolicyInvalid
	}
	collection, err := (&SourceCollectionStore{}).Find(ctx, input.CollectionUUID)
	if err != nil {
		return nil, err
	}
	policy, err := s.Find(ctx, input.CollectionUUID)
	if err != nil {
		return nil, err
	}
	input.CreatedAt = now.UTC()
	ret := &models.CaptureTranslationDecision{CollectionCapture: input, Status: "no_policy", Entries: []models.CaptureTranslationEntry{}}
	if policy != nil {
		ret.PolicyRevision = &policy.Revision
		switch {
		case collection == nil || collection.State != "active" || collection.Revision != input.CollectionRevision || policy.CollectionRevision != input.CollectionRevision:
			ret.Status = "collection_changed"
		case !policy.Definition.Enabled:
			ret.Status = "disabled"
		default:
			ret.Status = "recorded"
		}
	}
	complete := translationWorkAtomic(ctx)
	if ret.Status == "recorded" {
		ret.Entries, err = s.captureEntries(ctx, input, policy.Definition, now)
		if err != nil {
			return nil, err
		}
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO capture_translation_decisions(collection_uuid,capture_uuid,collection_revision,policy_revision,status,entry_count,created_at)
 VALUES(?,?,?,?,?,?,?)`, input.CollectionUUID, input.CaptureUUID, input.CollectionRevision, ret.PolicyRevision, ret.Status, len(ret.Entries), input.CreatedAt); err != nil {
		return nil, err
	}
	for _, entry := range ret.Entries {
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO capture_translation_entries(collection_uuid,capture_uuid,collection_revision,field,status,target_uuid,target_revision)
 VALUES(?,?,?,?,?,?,?)`, input.CollectionUUID, input.CaptureUUID, input.CollectionRevision, entry.Field, entry.Status, entry.TargetUUID, entry.TargetRevision); err != nil {
			return nil, err
		}
	}
	result, err := s.CaptureDecision(ctx, input)
	*complete = err == nil
	return result, err
}

func (s *TranslationPolicyStore) captureEntries(ctx context.Context, scope models.CollectionCapture, policy models.TranslationPolicyDefinition, now time.Time) ([]models.CaptureTranslationEntry, error) {
	// Translation needs only the shared text metadata, not a reconstructed copy
	// of the full provider payload and profiles for each media capture.
	var source struct {
		PostUUID string `db:"post_uuid"`
		Metadata string `db:"metadata"`
	}
	if err := dbWrapper.Get(ctx, &source, `SELECT c.post_uuid,r.metadata FROM source_captures c
 JOIN source_post_revisions r ON r.uuid=c.revision_uuid WHERE c.uuid=?`, scope.CaptureUUID); err != nil {
		return nil, err
	}
	if _, err := activePostLink(ctx, source.PostUUID); err != nil {
		return nil, err
	}
	var metadata models.SourcePostMetadata
	if err := json.Unmarshal([]byte(source.Metadata), &metadata); err != nil {
		return nil, err
	}
	entries := []models.CaptureTranslationEntry{}
	work := &TranslationWorkStore{}
	for _, item := range []struct {
		field   string
		enabled bool
		text    *string
	}{{"caption", policy.Caption, metadata.OriginalText}, {"title", policy.Title, metadata.Title}} {
		if !item.enabled {
			continue
		}
		entry := models.CaptureTranslationEntry{Field: item.field, Status: "no_text"}
		if item.text != nil && strings.TrimSpace(*item.text) != "" {
			request, err := work.RetainRequest(ctx, models.TranslationRequestInput{OriginalText: *item.text, TargetLanguage: policy.TargetLanguage, Policy: policy.ProviderPolicy})
			if err != nil {
				return nil, err
			}
			input := models.TranslationTargetInput{RequestUUID: request.UUID, PostUUID: source.PostUUID, CollectionUUID: &scope.CollectionUUID, CollectionRevision: &scope.CollectionRevision, Field: item.field, Origin: "capture"}
			id, err := archive.TranslationTargetIdentity(input)
			if err != nil {
				return nil, err
			}
			target, err := work.Target(ctx, id)
			if err != nil {
				return nil, err
			}
			entry.Status = "retained"
			if target == nil {
				target, err = work.RetainTarget(ctx, input, models.TranslationTargetSchedule{State: "pending", Priority: policy.Priority}, now)
				if err != nil {
					return nil, err
				}
				entry.Status = "created"
			}
			if now.Before(target.UpdatedAt) {
				return nil, models.ErrTranslationWorkConflict
			}
			entry.TargetUUID, entry.TargetRevision = &target.UUID, &target.Revision
		}
		entries = append(entries, entry)
	}
	return entries, nil
}
