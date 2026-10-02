package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

type SourceTranslationStore struct{}

const translationEvidenceColumns = `e.uuid,e.translation_uuid,e.post_uuid,e.collection_uuid,e.collection_revision,e.provenance,e.declared_input_hash,e.input_hash_algorithm,e.captured_at,e.origin,CAST(e.details AS BLOB) AS details,e.recorded_at`

func translationAtomicWrite(ctx context.Context) *bool {
	complete := false
	txn.AddPreCommitHook(ctx, func(context.Context) error {
		if !complete {
			return models.ErrSourceTranslationAtomic
		}
		return nil
	})
	return &complete
}

func validateTranslation(result *models.SourceTranslation) error {
	expected, err := archive.PrepareSourceTranslation(result.SourceTranslationInput)
	if err != nil || !reflect.DeepEqual(expected, result) {
		return models.ErrSourcePayloadCorrupt
	}
	return nil
}

func (s *SourceTranslationStore) Retain(ctx context.Context, input models.SourceTranslationInput) (*models.SourceTranslation, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	result, err := archive.PrepareSourceTranslation(input)
	if err != nil {
		return nil, err
	}
	prior, err := s.Find(ctx, result.UUID)
	if err != nil || prior != nil {
		return prior, err
	}
	complete := translationAtomicWrite(ctx)
	_, err = dbWrapper.Exec(ctx, `INSERT INTO source_translations(uuid,original_sha256,original_text,translated_text,source_language,target_language,provider) VALUES(?,?,?,?,?,?,?)`,
		result.UUID, result.OriginalSHA256, result.OriginalText, result.TranslatedText, result.SourceLanguage, result.TargetLanguage, result.Provider)
	if err != nil {
		return nil, err
	}
	ret, err := s.Find(ctx, result.UUID)
	*complete = err == nil
	return ret, err
}

func (s *SourceTranslationStore) Find(ctx context.Context, id string) (*models.SourceTranslation, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrSourceTranslationInvalid
	}
	ret := &models.SourceTranslation{}
	err := dbWrapper.Get(ctx, ret, "SELECT * FROM source_translations WHERE uuid=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ret, validateTranslation(ret)
}

func normalizeTranslationEvidence(input *models.SourceTranslationEvidence) error {
	if !validSourceRunUUID(input.UUID) || !validSourceRunUUID(input.TranslationUUID) || !validSourceRunUUID(input.PostUUID) ||
		(input.CollectionUUID == nil) != (input.CollectionRevision == nil) ||
		(input.CollectionUUID != nil && (!validSourceRunUUID(*input.CollectionUUID) || *input.CollectionRevision < 1)) ||
		!validAccountText(input.Provenance, 4096, true) || !archive.ValidDocumentSourceTime(input.CapturedAt) ||
		(input.Origin != "migration" && input.Origin != "capture" && input.Origin != "review" && input.Origin != "worker") ||
		(input.DeclaredInputHash == nil) != (input.InputHashAlgorithm == "") ||
		!validAccountText(input.InputHashAlgorithm, 128, true) || strings.TrimSpace(input.InputHashAlgorithm) != input.InputHashAlgorithm ||
		(input.DeclaredInputHash != nil && (len(*input.DeclaredInputHash) > 4096 || !utf8.ValidString(*input.DeclaredInputHash))) {
		return models.ErrSourceTranslationInvalid
	}
	if len(input.Details) == 0 {
		input.Details = json.RawMessage(`{}`)
	}
	value, err := archive.DecodeJSONObject(input.Details, 65536)
	if err != nil {
		return models.ErrSourceTranslationInvalid
	}
	input.Details, err = archive.EncodeSourceJSON(value)
	if err != nil || len(input.Details) > 65536 {
		return models.ErrSourceTranslationInvalid
	}
	return nil
}

func (s *SourceTranslationStore) RecordEvidence(ctx context.Context, input models.SourceTranslationEvidence) (*models.SourceTranslationEvidence, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if err := normalizeTranslationEvidence(&input); err != nil {
		return nil, err
	}
	prior, err := s.Evidence(ctx, input.UUID)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		input.RecordedAt = prior.RecordedAt
		if !reflect.DeepEqual(prior, &input) {
			return nil, models.ErrSourceTranslationReplay
		}
		return prior, nil
	}
	if _, err := activePostLink(ctx, input.PostUUID); err != nil {
		return nil, err
	}
	complete := translationAtomicWrite(ctx)
	_, err = dbWrapper.Exec(ctx, `INSERT INTO source_translation_evidence(uuid,translation_uuid,post_uuid,collection_uuid,collection_revision,provenance,declared_input_hash,input_hash_algorithm,captured_at,origin,details) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		input.UUID, input.TranslationUUID, input.PostUUID, input.CollectionUUID, input.CollectionRevision, input.Provenance, input.DeclaredInputHash, input.InputHashAlgorithm, input.CapturedAt, input.Origin, string(input.Details))
	if err != nil {
		return nil, err
	}
	ret, err := s.Evidence(ctx, input.UUID)
	*complete = err == nil
	return ret, err
}

func (s *SourceTranslationStore) Evidence(ctx context.Context, id string) (*models.SourceTranslationEvidence, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrSourceTranslationInvalid
	}
	ret := &models.SourceTranslationEvidence{}
	err := dbWrapper.Get(ctx, ret, "SELECT "+translationEvidenceColumns+" FROM source_translation_evidence e WHERE e.uuid=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return ret, err
}

// These bounded summaries retain provenance without repeating either text body.
// A target-language filter never matches unknown languages implicitly.
func (s *SourceTranslationStore) PostEvidence(ctx context.Context, query models.SourceTranslationQuery) ([]models.SourceTranslationEvidence, error) {
	if !validSourceRunUUID(query.PostUUID) || (query.After != "" && !validSourceRunUUID(query.After)) ||
		(query.OriginalSHA256 != "" && !archive.ValidSHA256(query.OriginalSHA256)) ||
		(query.TargetLanguage != nil && (len(*query.TargetLanguage) > 128 || !utf8.ValidString(*query.TargetLanguage))) {
		return nil, models.ErrSourceTranslationInvalid
	}
	limit, err := sourcePageLimit(query.Limit)
	if err != nil {
		return nil, models.ErrSourceTranslationInvalid
	}
	where := "e.post_uuid=? AND e.uuid>?"
	args := []any{query.PostUUID, query.After}
	if query.OriginalSHA256 != "" {
		where += " AND t.original_sha256=?"
		args = append(args, query.OriginalSHA256)
	}
	if query.TargetLanguage != nil {
		where += " AND t.target_language=?"
		args = append(args, *query.TargetLanguage)
	}
	args = append(args, limit)
	ret := []models.SourceTranslationEvidence{}
	err = dbWrapper.Select(ctx, &ret, "SELECT "+translationEvidenceColumns+" FROM source_translation_evidence e JOIN source_translations t ON t.uuid=e.translation_uuid WHERE "+where+" ORDER BY e.uuid LIMIT ?", args...)
	return ret, err
}
