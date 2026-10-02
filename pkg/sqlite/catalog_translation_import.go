package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

type CatalogTranslationImportStore struct{}

const catalogTranslationPolicy = "catalog-translations-v1"
const catalogTranslationColumns = `r.ordinal,e.source_key,e.data_sha256,r.post_uuid,r.translation_uuid,r.evidence_uuid,r.input_hash_state,r.outcome,r.reason`
const catalogTranslationJoins = ` FROM catalog_translation_records r JOIN catalog_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal`

const catalogTranslationInputHashAlgorithm = "catalog-json-sha256-v1"

func catalogTranslationHashState(original, declared *string) (string, error) {
	if declared == nil {
		return "missing", nil
	}
	if original == nil {
		return "unverifiable", nil
	}
	body, err := scrape.LegacyCatalogJSON(*original, 6*archive.MaxTranslationTextBytes+2)
	if err != nil {
		return "", err
	}
	if scrape.CatalogSnapshotSHA(body) == *declared {
		return "verified", nil
	}
	return "mismatch", nil
}

func catalogTranslationRecord(ctx context.Context, w *catalogRelationsWork, progress *models.CatalogTranslationImport, row catalogEvidenceRow, record *scrape.CatalogSnapshotRecord) (*models.CatalogTranslationRecord, error) {
	r := &models.CatalogTranslationRecord{Ordinal: row.Ordinal, Outcome: "review"}
	v := record.Values
	input := models.SourceTranslationInput{}
	var declared *string
	for key, target := range map[string]**string{"original_text": &input.OriginalText, "source_language": &input.SourceLanguage,
		"target_language": &input.TargetLanguage, "provider": &input.Provider, "input_hash": &declared} {
		if v[key] != nil {
			value, ok := v[key].(string)
			if !ok {
				r.Reason = "invalid_translation_values"
				return r, nil
			}
			*target = &value
		}
	}
	var ok bool
	input.TranslatedText, ok = v["translated_text"].(string)
	if !ok {
		r.Reason = "invalid_translation_values"
		return r, nil
	}
	store := &SourceTranslationStore{}
	translation, err := store.Retain(ctx, input)
	if errors.Is(err, models.ErrSourceTranslationInvalid) {
		r.Reason = "invalid_translation_values"
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	r.TranslationUUID = &translation.UUID
	r.InputHashState, err = catalogTranslationHashState(input.OriginalText, declared)
	if err != nil {
		return nil, err
	}
	key, keyOK := v["post_key"].(string)
	provenance, provenanceOK := v["provenance"].(string)
	captured, capturedOK := v["captured_at"].(string)
	if !keyOK || key == "" || !provenanceOK || !capturedOK {
		r.Reason = "invalid_translation_provenance"
		return r, nil
	}
	post, reason, err := w.nativePost(ctx, key)
	if err != nil {
		return nil, err
	}
	if post != nil {
		r.PostUUID = &post.UUID
	}
	if reason != "" && reason != "post_forgotten" {
		r.Reason = reason
		return r, nil
	}
	if post == nil {
		r.Reason = "post_requires_review"
		return r, nil
	}
	details, err := archive.EncodeSourceJSON(map[string]any{"policy": catalogTranslationPolicy, "registry_source_uuid": w.snapshot.SourceUUID,
		"catalog_id": w.snapshot.CatalogID, "legacy_translation_id": v["translation_id"], "legacy_post_key": key})
	if err != nil {
		return nil, err
	}
	identity, err := archive.EncodeSourceJSON([]any{w.snapshot.CatalogID, progress.CollectionUUID, progress.CollectionRevision, post.UUID, v})
	if err != nil {
		return nil, err
	}
	evidence := models.SourceTranslationEvidence{UUID: scrape.RegistryImportUUID(w.snapshot.SourceUUID, "catalog-translation-evidence:v1", scrape.CatalogSnapshotSHA(identity)),
		TranslationUUID: translation.UUID, PostUUID: post.UUID, CollectionUUID: &progress.CollectionUUID, CollectionRevision: &progress.CollectionRevision,
		Provenance: provenance, DeclaredInputHash: declared, CapturedAt: captured, Origin: "migration", Details: details}
	if declared != nil {
		evidence.InputHashAlgorithm = catalogTranslationInputHashAlgorithm
	}
	saved, err := store.RecordEvidence(ctx, evidence)
	if errors.Is(err, models.ErrSourceTranslationInvalid) {
		r.Reason = "invalid_translation_provenance"
		return r, nil
	}
	if errors.Is(err, models.ErrSourcePostForgotten) {
		r.Reason = "post_forgotten"
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	r.EvidenceUUID = &saved.UUID
	if r.InputHashState == "mismatch" {
		r.Reason = "declared_input_hash_conflicts"
	} else {
		r.Outcome = "mapped"
	}
	return r, nil
}

func (s *CatalogTranslationImportStore) Find(ctx context.Context, id string) (*models.CatalogTranslationImport, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	ret := &models.CatalogTranslationImport{}
	if err := dbWrapper.Get(ctx, ret, "SELECT * FROM catalog_translation_imports WHERE snapshot_uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return ret, nil
}

func (s *CatalogTranslationImportStore) Advance(ctx context.Context, id, expected string, after int64, now time.Time) (*models.CatalogTranslationImport, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validSourceRunUUID(id) || !archive.ValidSHA256(expected) || after < 0 || !validJobTime(now) {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	prior, err := s.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if (prior == nil && after != 0) || (prior != nil && (prior.LastOrdinal != after || prior.ManifestSHA256 != expected || prior.Policy != catalogTranslationPolicy)) {
		return nil, models.ErrCatalogSnapshotConflict
	}
	if prior != nil && prior.State != "running" {
		return prior, nil
	}
	// Received snapshot and completed post evidence are required. Translation
	// retention neither applies entity metadata nor starts provider work.
	w, err := loadCompletedCatalogRelations(ctx, id, expected)
	if err != nil {
		return nil, err
	}
	complete := snapshotAtomicWrite(ctx)
	stamp := now.UTC().Format(time.RFC3339Nano)
	if prior == nil {
		var historical bool
		if err := dbWrapper.Get(ctx, &historical, "SELECT EXISTS(SELECT 1 FROM source_collection_revisions WHERE collection_uuid=? AND revision=1 AND origin='migration')", w.snapshot.CollectionUUID); err != nil {
			return nil, err
		}
		if !historical {
			return nil, models.ErrCatalogSnapshotConflict
		}
		_, err := dbWrapper.Exec(ctx, `INSERT INTO catalog_translation_imports(snapshot_uuid,manifest_sha256,collection_uuid,collection_revision,policy,state,source_records,created_at,updated_at)
 VALUES(?,?,?,1,?,'running',?,?,?)`, id, expected, w.snapshot.CollectionUUID, catalogTranslationPolicy, w.manifest.Tables["translations"].Rows, stamp, stamp)
		if err != nil {
			return nil, err
		}
		prior, err = s.Find(ctx, id)
		if err != nil {
			return nil, err
		}
	}
	for count := 0; count < 50 && w.bytes < 16<<20; count++ {
		var row catalogEvidenceRow
		err := dbWrapper.Get(ctx, &row, "SELECT ordinal,data,data_sha256 FROM catalog_snapshot_records WHERE snapshot_uuid=? AND source_table='translations' AND ordinal>? ORDER BY ordinal LIMIT 1", id, prior.LastOrdinal)
		if errors.Is(err, sql.ErrNoRows) {
			if prior.ProcessedRecords != prior.TotalRecords {
				return nil, models.ErrCatalogSnapshotInvalid
			}
			prior.State = "mapped"
			if prior.ReviewRecords > 0 {
				prior.State = "review"
			}
			break
		}
		if err != nil {
			return nil, err
		}
		record, err := w.decode(row)
		if err != nil {
			return nil, err
		}
		result, err := catalogTranslationRecord(ctx, w, prior, row, record)
		if err != nil {
			return nil, err
		}
		_, err = dbWrapper.Exec(ctx, `INSERT INTO catalog_translation_records(snapshot_uuid,ordinal,post_uuid,translation_uuid,evidence_uuid,input_hash_state,outcome,reason) VALUES(?,?,?,?,?,?,?,?)`, id, row.Ordinal, result.PostUUID, result.TranslationUUID, result.EvidenceUUID, result.InputHashState, result.Outcome, result.Reason)
		if err != nil {
			return nil, err
		}
		prior.ProcessedRecords++
		prior.LastOrdinal = row.Ordinal
		if result.Outcome == "mapped" {
			prior.MappedRecords++
		} else {
			prior.ReviewRecords++
		}
	}
	_, err = dbWrapper.Exec(ctx, `UPDATE catalog_translation_imports SET state=?,last_ordinal=?,processed_records=?,mapped_records=?,review_records=?,updated_at=? WHERE snapshot_uuid=?`, prior.State, prior.LastOrdinal, prior.ProcessedRecords, prior.MappedRecords, prior.ReviewRecords, stamp, id)
	if err != nil {
		return nil, err
	}
	ret, err := s.Find(ctx, id)
	*complete = err == nil
	return ret, err
}

func (s *CatalogTranslationImportStore) Records(ctx context.Context, id string, after int64, limit int) ([]models.CatalogTranslationRecord, error) {
	if !validSourceRunUUID(id) || after < 0 || limit < 1 || limit > 100 {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	columns := strings.Replace(catalogTranslationColumns, "e.source_key", `CASE WHEN length(CAST(e.source_key AS BLOB))<=8192 THEN e.source_key ELSE '' END AS source_key,length(CAST(e.source_key AS BLOB))>8192 AS key_omitted`, 1)
	ret := []models.CatalogTranslationRecord{}
	err := dbWrapper.Select(ctx, &ret, "SELECT "+columns+catalogTranslationJoins+" WHERE r.snapshot_uuid=? AND r.ordinal>? ORDER BY r.ordinal LIMIT ?", id, after, limit)
	return ret, err
}

func (s *CatalogTranslationImportStore) Record(ctx context.Context, id string, ordinal int64) (*models.CatalogTranslationRecordDetails, error) {
	if !validSourceRunUUID(id) || ordinal < 1 {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	var row struct {
		models.CatalogTranslationRecord
		Data string `db:"data"`
	}
	err := dbWrapper.Get(ctx, &row, "SELECT "+catalogTranslationColumns+",e.data"+catalogTranslationJoins+" WHERE r.snapshot_uuid=? AND r.ordinal=?", id, ordinal)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	object, err := archive.DecodeJSONObject([]byte(row.Data), scrape.CatalogChunkLimit)
	if err != nil || scrape.CatalogSnapshotSHA([]byte(row.Data)) != row.SHA256 {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	values, err := archive.EncodeSourceJSON(object["values"])
	return &models.CatalogTranslationRecordDetails{CatalogTranslationRecord: row.CatalogTranslationRecord, SourceValues: values}, err
}
