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

type AutomationTranslationImportStore struct{}

const automationTranslationColumns = `r.ordinal,e.source_table,e.source_key,e.data_sha256,r.job_ordinal,r.request_uuid,r.cache_uuid,r.post_uuid,r.post_reference,r.collection_uuid,r.collection_revision,r.target_uuid,r.target_revision,r.evidence_uuid,r.disposition,r.outcome,r.reason`
const automationTranslationJoins = ` FROM automation_translation_records r JOIN automation_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal`

type automationTranslationWork struct{ automationImportWork }

func automationTranslationReview(r *models.AutomationTranslationRecord, reason string) *models.AutomationTranslationRecord {
	r.Outcome, r.Disposition, r.Reason = "review", "review", reason
	return r
}

func (w *automationTranslationWork) job(ctx context.Context, row catalogEvidenceRow, record *scrape.CatalogSnapshotRecord) (*models.AutomationTranslationRecord, error) {
	r := &models.AutomationTranslationRecord{Ordinal: row.Ordinal}
	prepared, reason := scrape.PrepareAutomationTranslationJob(record.Values, w.captured)
	if reason != "" {
		return automationTranslationReview(r, reason), nil
	}
	store := &TranslationWorkStore{}
	request, err := store.RetainRequest(ctx, prepared.Request)
	if err != nil {
		return nil, err
	}
	r.RequestUUID = &request.UUID
	if prepared.Cache != nil {
		cache, err := store.RetainCache(ctx, *prepared.Cache)
		if errors.Is(err, models.ErrTranslationWorkConflict) {
			return automationTranslationReview(r, "native_cache_conflict"), nil
		}
		if err != nil {
			return nil, err
		}
		r.CacheUUID = &cache.UUID
	}
	r.Outcome, r.Disposition = "mapped", prepared.Disposition
	return r, nil
}

func (w *automationTranslationWork) target(ctx context.Context, row catalogEvidenceRow, record *scrape.CatalogSnapshotRecord, now time.Time) (*models.AutomationTranslationRecord, error) {
	r := &models.AutomationTranslationRecord{Ordinal: row.Ordinal}
	input, reason := scrape.PrepareAutomationTranslationTarget(record.Values)
	if reason != "" {
		return automationTranslationReview(r, reason), nil
	}
	key, err := archive.EncodeSourceJSON([]string{input.JobKey})
	if err != nil {
		return nil, err
	}
	var job struct {
		catalogEvidenceRow
		RequestUUID *string `db:"request_uuid"`
		CacheUUID   *string `db:"cache_uuid"`
		Outcome     *string `db:"outcome"`
	}
	err = dbWrapper.Get(ctx, &job, `SELECT e.ordinal,e.data,e.data_sha256,r.request_uuid,r.cache_uuid,r.outcome
 FROM automation_snapshot_records e LEFT JOIN automation_translation_records r ON r.snapshot_uuid=e.snapshot_uuid AND r.ordinal=e.ordinal
 WHERE e.snapshot_uuid=? AND e.source_table='translation_jobs' AND e.source_key=?`, w.snapshot.UUID, string(key))
	if errors.Is(err, sql.ErrNoRows) {
		return automationTranslationReview(r, "legacy_job_missing"), nil
	}
	if err != nil {
		return nil, err
	}
	if job.Outcome == nil {
		return nil, models.ErrAutomationSnapshotInvalid
	}
	r.JobOrdinal, r.RequestUUID, r.CacheUUID = &job.Ordinal, job.RequestUUID, job.CacheUUID
	if *job.Outcome != "mapped" {
		return automationTranslationReview(r, "legacy_job_requires_review"), nil
	}
	jobRecord, err := w.decode(job.catalogEvidenceRow)
	if err != nil {
		return nil, err
	}
	prepared, reason := scrape.PrepareAutomationTranslationJob(jobRecord.Values, w.captured)
	if reason != "" || r.RequestUUID == nil {
		return nil, models.ErrAutomationSnapshotInvalid
	}
	ref, err := scrape.CatalogLocalPostReference(w.snapshot.SourceUUID, input.CatalogID, input.PostKey)
	if err != nil {
		return automationTranslationReview(r, "invalid_legacy_post_reference"), nil
	}
	r.PostReference = ref.Value
	var collection string
	err = dbWrapper.Get(ctx, &collection, `SELECT m.collection_uuid FROM catalog_collection_mappings m
 JOIN source_collection_revisions c ON c.collection_uuid=m.collection_uuid AND c.revision=1 AND c.origin='migration'
 WHERE m.source_uuid=? AND m.catalog_id=? AND m.import_uuid=?`, w.snapshot.SourceUUID, input.CatalogID, w.snapshot.RegistryImportUUID)
	if errors.Is(err, sql.ErrNoRows) {
		return automationTranslationReview(r, "legacy_collection_unmapped"), nil
	}
	if err != nil {
		return nil, err
	}
	revision := 1
	r.CollectionUUID, r.CollectionRevision = &collection, &revision
	var incomplete bool
	if err := dbWrapper.Get(ctx, &incomplete, `SELECT EXISTS(SELECT 1 FROM catalog_snapshots s
 LEFT JOIN catalog_evidence_imports i ON i.snapshot_uuid=s.uuid WHERE s.source_uuid=? AND s.catalog_id=?
 AND (s.state!='received' OR i.state IS NULL OR i.state='running'))`, w.snapshot.SourceUUID, input.CatalogID); err != nil {
		return nil, err
	}
	if incomplete {
		return nil, models.ErrAutomationSnapshotConflict
	}
	post, err := (&SourceEvidenceStore{}).FindPostByIdentifier(ctx, ref)
	if err != nil {
		return nil, err
	}
	if post == nil {
		return automationTranslationReview(r, "legacy_post_unmatched"), nil
	}
	r.PostUUID = &post.UUID
	if post.State != "active" {
		return automationTranslationReview(r, "post_forgotten"), nil
	}
	targetInput := models.TranslationTargetInput{RequestUUID: *r.RequestUUID, PostUUID: post.UUID, CollectionUUID: &collection, CollectionRevision: &revision, Field: input.Field, Origin: "migration"}
	id, err := archive.TranslationTargetIdentity(targetInput)
	if err != nil {
		return nil, err
	}
	store := &TranslationWorkStore{}
	prior, err := store.Target(ctx, id)
	if err != nil {
		return nil, err
	}
	owned := false
	if prior != nil && input.Applied && r.CacheUUID != nil {
		owned, err = importedHeldTranslationTarget(ctx, w.snapshot.UUID, prior)
		if err != nil {
			return nil, err
		}
	}
	if prior != nil && !owned {
		r.TargetUUID, r.TargetRevision = &prior.UUID, &prior.Revision
		r.Outcome, r.Disposition = "mapped", "preserved"
		return r, nil
	}
	if input.Applied && r.CacheUUID == nil {
		return automationTranslationReview(r, "historical_completion_requires_evidence"), nil
	}
	target := prior
	if target == nil {
		target, err = store.RetainTarget(ctx, targetInput, models.TranslationTargetSchedule{State: "held", Priority: prepared.Priority, NotBefore: prepared.NotBefore}, now)
		if err != nil {
			return nil, err
		}
	}
	r.Disposition = "held"
	if input.Applied {
		cache, err := store.Cache(ctx, target.RequestUUID)
		if err != nil {
			return nil, err
		}
		if cache == nil || cache.UUID != *r.CacheUUID {
			return nil, models.ErrAutomationSnapshotInvalid
		}
		target, err = completeImportedTranslationTarget(ctx, w.snapshot.UUID, row.Ordinal, target, cache, now)
		if err != nil {
			return nil, err
		}
		r.Disposition, r.EvidenceUUID = "completed", target.EvidenceUUID
	}
	r.TargetUUID, r.TargetRevision, r.Outcome = &target.UUID, &target.Revision, "mapped"
	return r, nil
}

// Aliases can map multiple original rows to one target. A later historical
// completion may finish an untouched hold created by this same import; it may
// not override a preexisting target or a later native scheduling decision.
func importedHeldTranslationTarget(ctx context.Context, snapshot string, target *models.TranslationTarget) (bool, error) {
	if target.Origin != "migration" || target.State != "held" || target.Revision != 1 {
		return false, nil
	}
	var found bool
	err := dbWrapper.Get(ctx, &found, `SELECT EXISTS(SELECT 1 FROM automation_translation_records
 WHERE snapshot_uuid=? AND target_uuid=? AND target_revision=1 AND disposition='held')`, snapshot, target.UUID)
	return found, err
}

// This is deliberately private to the frozen importer. Historical completion
// carries its original receipt, not a fabricated worker lease or execution.
func completeImportedTranslationTarget(ctx context.Context, snapshot string, ordinal int64, target *models.TranslationTarget, cache *models.TranslationCache, now time.Time) (*models.TranslationTarget, error) {
	if target.Origin != "migration" || target.State != "held" || target.Revision != 1 || now.Before(target.CreatedAt) {
		return nil, models.ErrAutomationSnapshotConflict
	}
	if !target.CreatedAt.Equal(now) {
		owned, err := importedHeldTranslationTarget(ctx, snapshot, target)
		if err != nil {
			return nil, err
		}
		if !owned {
			return nil, models.ErrAutomationSnapshotConflict
		}
	}
	var evidenceID *string
	if cache.TranslationUUID != nil {
		details, err := archive.EncodeSourceJSON(map[string]any{"policy": scrape.AutomationTranslationPolicy, "snapshot_uuid": snapshot, "source_ordinal": ordinal,
			"request_uuid": target.RequestUUID, "target_uuid": target.UUID, "cache_uuid": cache.UUID, "field": target.Field})
		if err != nil {
			return nil, err
		}
		evidence, err := (&SourceTranslationStore{}).RecordEvidence(ctx, models.SourceTranslationEvidence{
			UUID: scrape.RegistryImportUUID(snapshot, "automation-translation-evidence:v1", target.UUID), TranslationUUID: *cache.TranslationUUID,
			PostUUID: target.PostUUID, CollectionUUID: target.CollectionUUID, CollectionRevision: target.CollectionRevision,
			Provenance: "automation-translation:" + target.Field, Origin: "migration", Details: details})
		if err != nil {
			return nil, err
		}
		evidenceID = &evidence.UUID
	}
	_, err := dbWrapper.Exec(ctx, "UPDATE translation_targets SET state='completed',cache_uuid=?,evidence_uuid=?,revision=revision+1,updated_at=? WHERE uuid=?", cache.UUID, evidenceID, now.UTC(), target.UUID)
	if err != nil {
		return nil, err
	}
	return (&TranslationWorkStore{}).Target(ctx, target.UUID)
}

func (s *AutomationTranslationImportStore) Find(ctx context.Context, id string) (*models.AutomationTranslationImport, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrAutomationSnapshotInvalid
	}
	ret := &models.AutomationTranslationImport{}
	if err := dbWrapper.Get(ctx, ret, "SELECT * FROM automation_translation_imports WHERE snapshot_uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return ret, nil
}

func (s *AutomationTranslationImportStore) Advance(ctx context.Context, id, expected string, after int64, now time.Time) (*models.AutomationTranslationImport, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validSourceRunUUID(id) || !archive.ValidSHA256(expected) || after < 0 || !validJobTime(now) {
		return nil, models.ErrAutomationSnapshotInvalid
	}
	prior, err := s.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if (prior == nil && after != 0) || (prior != nil && (prior.LastOrdinal != after || prior.ManifestSHA256 != expected || prior.Policy != scrape.AutomationTranslationPolicy)) {
		return nil, models.ErrAutomationSnapshotConflict
	}
	if prior != nil && prior.State != "running" {
		return prior, nil
	}
	snapshot, err := (&AutomationSnapshotStore{}).Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if snapshot == nil || snapshot.State != "received" || snapshot.ManifestSHA256 != expected {
		return nil, models.ErrAutomationSnapshotConflict
	}
	var body []byte
	if err := dbWrapper.Get(ctx, &body, "SELECT manifest FROM automation_snapshots WHERE uuid=?", id); err != nil {
		return nil, err
	}
	manifest, err := scrape.PrepareAutomationSnapshot(body, expected)
	if err != nil {
		return nil, err
	}
	captured, _ := time.Parse(time.RFC3339Nano, manifest.CapturedAt)
	updated, _ := time.Parse(time.RFC3339Nano, snapshot.UpdatedAt)
	if prior != nil {
		updated, _ = time.Parse(time.RFC3339Nano, prior.UpdatedAt)
	}
	if now.Before(updated) {
		return nil, models.ErrAutomationSnapshotConflict
	}
	w := &automationTranslationWork{automationImportWork{snapshot, manifest, captured}}
	complete := snapshotAtomicWrite(ctx)
	stamp := now.UTC().Format(time.RFC3339Nano)
	if prior == nil {
		total := manifest.Tables["translation_jobs"].Rows + manifest.Tables["translation_targets"].Rows
		_, err := dbWrapper.Exec(ctx, `INSERT INTO automation_translation_imports(snapshot_uuid,manifest_sha256,policy,state,source_records,created_at,updated_at)
 VALUES(?,?,?,'running',?,?,?)`, id, expected, scrape.AutomationTranslationPolicy, total, stamp, stamp)
		if err != nil {
			return nil, err
		}
		prior, err = s.Find(ctx, id)
		if err != nil {
			return nil, err
		}
	}
	var bytesRead int
	for count := 0; count < 200; count++ {
		var row catalogEvidenceRow
		err := dbWrapper.Get(ctx, &row, `SELECT ordinal,data,data_sha256 FROM automation_snapshot_records INDEXED BY automation_translation_input
 WHERE snapshot_uuid=? AND ordinal>? AND source_table IN ('translation_jobs','translation_targets') ORDER BY ordinal LIMIT 1`, id, prior.LastOrdinal)
		if errors.Is(err, sql.ErrNoRows) {
			if prior.ProcessedRecords != prior.TotalRecords {
				return nil, models.ErrAutomationSnapshotInvalid
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
		if bytesRead+len(row.Data) > scrape.CatalogChunkLimit && count > 0 {
			break
		}
		bytesRead += len(row.Data)
		record, err := w.decode(row)
		if err != nil {
			return nil, err
		}
		var result *models.AutomationTranslationRecord
		if record.Table == "translation_jobs" {
			result, err = w.job(ctx, row, record)
		} else {
			result, err = w.target(ctx, row, record, now)
		}
		if err != nil {
			return nil, err
		}
		_, err = dbWrapper.Exec(ctx, `INSERT INTO automation_translation_records(snapshot_uuid,ordinal,job_ordinal,request_uuid,cache_uuid,post_uuid,post_reference,collection_uuid,collection_revision,target_uuid,target_revision,evidence_uuid,disposition,outcome,reason)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, row.Ordinal, result.JobOrdinal, result.RequestUUID, result.CacheUUID, result.PostUUID, result.PostReference,
			result.CollectionUUID, result.CollectionRevision, result.TargetUUID, result.TargetRevision, result.EvidenceUUID, result.Disposition, result.Outcome, result.Reason)
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
	_, err = dbWrapper.Exec(ctx, `UPDATE automation_translation_imports SET state=?,last_ordinal=?,processed_records=?,mapped_records=?,review_records=?,updated_at=? WHERE snapshot_uuid=?`,
		prior.State, prior.LastOrdinal, prior.ProcessedRecords, prior.MappedRecords, prior.ReviewRecords, stamp, id)
	if err != nil {
		return nil, err
	}
	ret, err := s.Find(ctx, id)
	*complete = err == nil
	return ret, err
}

func (s *AutomationTranslationImportStore) Records(ctx context.Context, id string, after int64, limit int) ([]models.AutomationTranslationRecord, error) {
	if !validSourceRunUUID(id) || after < 0 || limit < 1 || limit > 100 {
		return nil, models.ErrAutomationSnapshotInvalid
	}
	columns := strings.Replace(automationTranslationColumns, "e.source_key", `CASE WHEN length(CAST(e.source_key AS BLOB))<=8192 THEN e.source_key ELSE '' END AS source_key,length(CAST(e.source_key AS BLOB))>8192 AS key_omitted`, 1)
	ret := []models.AutomationTranslationRecord{}
	err := dbWrapper.Select(ctx, &ret, "SELECT "+columns+automationTranslationJoins+" WHERE r.snapshot_uuid=? AND r.ordinal>? ORDER BY r.ordinal LIMIT ?", id, after, limit)
	return ret, err
}

func (s *AutomationTranslationImportStore) Record(ctx context.Context, id string, ordinal int64) (*models.AutomationTranslationRecordDetails, error) {
	if !validSourceRunUUID(id) || ordinal < 1 {
		return nil, models.ErrAutomationSnapshotInvalid
	}
	var row struct {
		models.AutomationTranslationRecord
		Data string `db:"data"`
	}
	err := dbWrapper.Get(ctx, &row, "SELECT "+automationTranslationColumns+",e.data"+automationTranslationJoins+" WHERE r.snapshot_uuid=? AND r.ordinal=?", id, ordinal)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	object, err := archive.DecodeJSONObject([]byte(row.Data), scrape.CatalogChunkLimit)
	if err != nil || scrape.CatalogSnapshotSHA([]byte(row.Data)) != row.SHA256 {
		return nil, models.ErrAutomationSnapshotInvalid
	}
	values, err := archive.EncodeSourceJSON(object["values"])
	return &models.AutomationTranslationRecordDetails{AutomationTranslationRecord: row.AutomationTranslationRecord, SourceValues: values}, err
}
