package sqlite

import (
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

func validateTranslationWorkSchema(conn *sqlx.DB, allowImportedCompletion bool) error {
	for _, name := range []string{"translation_requests", "translation_request_immutable", "translation_cache", "translation_cache_immutable", "translation_cache_scope",
		"translation_targets", "translation_targets_request", "translation_targets_post", "translation_targets_request_state", "translation_targets_post_state", "translation_targets_ready",
		"translation_target_initial", "translation_target_active", "translation_target_identity", "translation_target_scope", "translation_target_history", "translation_target_history_immutable", "translation_target_history_insert", "translation_target_history_update"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	for _, field := range []struct{ table, column string }{{"translation_requests", "created_at"}, {"translation_cache", "recorded_at"}, {"translation_targets", "not_before"},
		{"translation_targets", "created_at"}, {"translation_targets", "updated_at"}, {"translation_target_history", "not_before"}, {"translation_target_history", "recorded_at"}} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM pragma_table_info(?) WHERE name=? AND upper(type)='DATETIME')", field.table, field.column); err != nil {
			return err
		}
		if !found {
			return errors.New("native database has an invalid translation work timestamp type")
		}
	}
	var invalid bool
	err := conn.Get(&invalid, `SELECT EXISTS(SELECT 1 FROM translation_cache c LEFT JOIN translation_requests r ON r.uuid=c.request_uuid
 LEFT JOIN source_translations s ON s.uuid=c.translation_uuid WHERE r.uuid IS NULL OR (c.translation_uuid IS NOT NULL AND s.uuid IS NULL))
 OR EXISTS(SELECT 1 FROM translation_targets t
 LEFT JOIN translation_requests r ON r.uuid=t.request_uuid LEFT JOIN source_posts p ON p.uuid=t.post_uuid
 LEFT JOIN source_collection_revisions collection ON collection.collection_uuid=t.collection_uuid AND collection.revision=t.collection_revision
 LEFT JOIN translation_cache c ON c.uuid=t.cache_uuid LEFT JOIN source_translation_evidence e ON e.uuid=t.evidence_uuid
 WHERE r.uuid IS NULL OR p.uuid IS NULL OR (t.collection_uuid IS NOT NULL AND collection.collection_uuid IS NULL)
 OR (t.cache_uuid IS NOT NULL AND (c.uuid IS NULL OR c.request_uuid IS NOT t.request_uuid))
 OR (t.state='completed' AND ((c.status='no_text' AND t.evidence_uuid IS NOT NULL) OR (c.status!='no_text' AND
  (e.uuid IS NULL OR e.translation_uuid IS NOT c.translation_uuid OR e.post_uuid IS NOT t.post_uuid
   OR e.collection_uuid IS NOT t.collection_uuid OR e.collection_revision IS NOT t.collection_revision
   OR NOT ((e.origin='worker' AND e.provenance=('native-translation:'||t.field) AND e.captured_at IS c.captured_at)
    OR (? AND t.origin='migration' AND e.origin='migration' AND e.provenance=('automation-translation:'||t.field) AND e.captured_at=''))
   OR json_extract(e.details,'$.target_uuid') IS NOT t.uuid OR json_extract(e.details,'$.request_uuid') IS NOT t.request_uuid
   OR json_extract(e.details,'$.cache_uuid') IS NOT c.uuid OR json_extract(e.details,'$.field') IS NOT t.field))))
 OR t.revision!=(SELECT count(*) FROM translation_target_history h WHERE h.target_uuid=t.uuid)
 OR t.revision!=(SELECT max(revision) FROM translation_target_history h WHERE h.target_uuid=t.uuid)
 OR NOT EXISTS(SELECT 1 FROM translation_target_history h WHERE h.target_uuid=t.uuid AND h.revision=t.revision
  AND h.state=t.state AND h.priority=t.priority AND h.not_before=t.not_before AND h.cache_uuid IS t.cache_uuid
  AND h.evidence_uuid IS t.evidence_uuid AND h.reason=t.reason AND h.recorded_at=t.updated_at))
 OR EXISTS(SELECT 1 FROM translation_target_history h LEFT JOIN translation_targets t ON t.uuid=h.target_uuid
 LEFT JOIN translation_target_history previous ON previous.target_uuid=h.target_uuid AND previous.revision=h.revision-1
 WHERE t.uuid IS NULL OR (h.revision<t.revision AND h.state NOT IN ('held','pending'))
 OR (h.state IN ('held','pending') AND (h.cache_uuid IS NOT NULL OR h.evidence_uuid IS NOT NULL OR h.reason!=''))
 OR (h.revision=1 AND h.recorded_at IS NOT t.created_at)
 OR (h.revision>1 AND (previous.revision IS NULL OR h.recorded_at<previous.recorded_at)))`, allowImportedCompletion)
	if err != nil {
		return err
	}
	if invalid {
		return errors.New("native database has invalid translation work scope or history")
	}
	if err := validateTranslationRequests(conn); err != nil {
		return err
	}
	if err := validateTranslationCaches(conn); err != nil {
		return err
	}
	return validateTranslationTargets(conn)
}

func validateTranslationRequests(conn *sqlx.DB) error {
	rows, err := conn.Queryx("SELECT * FROM translation_requests")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var value models.TranslationRequest
		if err := rows.StructScan(&value); err != nil {
			return err
		}
		if !validJobTime(value.CreatedAt) {
			return models.ErrSourcePayloadCorrupt
		}
		if err := validateTranslationRequest(&value); err != nil {
			return err
		}
	}
	return rows.Err()
}

func validateTranslationCaches(conn *sqlx.DB) error {
	rows, err := conn.Queryx(`SELECT c.*,r.original_text,r.target_language,r.policy,s.original_text AS result_original,
 s.translated_text AS result_text,s.source_language AS result_language,s.target_language AS result_target,s.provider AS result_provider
 FROM translation_cache c JOIN translation_requests r ON r.uuid=c.request_uuid LEFT JOIN source_translations s ON s.uuid=c.translation_uuid`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row struct {
			models.TranslationCache
			models.TranslationRequestInput
			ResultOriginal *string `db:"result_original"`
			ResultText     *string `db:"result_text"`
			ResultLanguage *string `db:"result_language"`
			ResultTarget   *string `db:"result_target"`
			ResultProvider *string `db:"result_provider"`
		}
		if err := rows.StructScan(&row); err != nil {
			return err
		}
		request := &models.TranslationRequest{UUID: row.RequestUUID, TranslationRequestInput: row.TranslationRequestInput}
		var result *models.SourceTranslation
		if row.TranslationUUID != nil && row.ResultText != nil {
			result = &models.SourceTranslation{UUID: *row.TranslationUUID, SourceTranslationInput: models.SourceTranslationInput{
				OriginalText: row.ResultOriginal, TranslatedText: *row.ResultText, SourceLanguage: row.ResultLanguage, TargetLanguage: row.ResultTarget, Provider: row.ResultProvider}}
		}
		if !validJobTime(row.RecordedAt) {
			return models.ErrSourcePayloadCorrupt
		}
		if err := validateTranslationCache(request, &row.TranslationCache, result); err != nil {
			return err
		}
	}
	return rows.Err()
}

func validateTranslationTargets(conn *sqlx.DB) error {
	rows, err := conn.Queryx("SELECT * FROM translation_targets")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var value models.TranslationTarget
		if err := rows.StructScan(&value); err != nil {
			return err
		}
		id, err := archive.TranslationTargetIdentity(value.TranslationTargetInput)
		if err != nil || value.UUID != id || !validJobTime(value.NotBefore) || !validJobTime(value.CreatedAt) || !validJobTime(value.UpdatedAt) || value.UpdatedAt.Before(value.CreatedAt) {
			return models.ErrSourcePayloadCorrupt
		}
	}
	return rows.Err()
}
