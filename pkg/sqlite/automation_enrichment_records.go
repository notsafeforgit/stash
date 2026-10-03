package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

func automationEnrichmentReview(r *models.AutomationEnrichmentRecord, reason string) *models.AutomationEnrichmentRecord {
	r.Outcome, r.Disposition, r.Reason = "review", "review", reason
	return r
}

func (w *automationEnrichmentWork) record(ctx context.Context, row catalogEvidenceRow, record *scrape.CatalogSnapshotRecord, now time.Time) (*models.AutomationEnrichmentRecord, error) {
	r := &models.AutomationEnrichmentRecord{Ordinal: row.Ordinal, Outcome: "mapped"}
	switch record.Table {
	case "enrichment_jobs":
		return w.job(ctx, row, record, now)
	case "enrichment_cooldowns":
		prepared, reason := scrape.PrepareAutomationEnrichmentCooldown(record.Values, w.captured)
		if reason != "" {
			return automationEnrichmentReview(r, reason), nil
		}
		r.Disposition, r.CooldownKind, r.CooldownValue, r.CooldownReason, r.CooldownUntil = "cooldown", prepared.Kind, prepared.Value, prepared.Reason, &prepared.Until
	case "enrichment_seed_progress":
		prepared, reason := scrape.PrepareAutomationEnrichmentSeed(record.Values)
		if reason != "" {
			return automationEnrichmentReview(r, reason), nil
		}
		if reason, err := w.collection(ctx, prepared.CatalogID, r); err != nil {
			return nil, err
		} else if reason != "" {
			return automationEnrichmentReview(r, reason), nil
		}
		counts, err := archive.EncodeSourceJSON(prepared.Counts)
		if err != nil {
			return nil, err
		}
		countsJSON := json.RawMessage(counts)
		r.Disposition, r.SeedLastPostKey, r.SeedComplete, r.SeedCounts = "seed_progress", &prepared.LastPostKey, &prepared.Complete, &countsJSON
	case "enrichment_source_progress":
		prepared, reason := scrape.PrepareAutomationEnrichmentSourceProgress(record.Values, w.captured)
		if reason != "" {
			return automationEnrichmentReview(r, reason), nil
		}
		r.Disposition, r.SourcePlatform, r.SourceLastAttempt = "source_progress", prepared.Platform, &prepared.LastAttempt
	default:
		return nil, models.ErrAutomationSnapshotInvalid
	}
	return r, nil
}

func (w *automationEnrichmentWork) collection(ctx context.Context, catalog string, r *models.AutomationEnrichmentRecord) (string, error) {
	var collection string
	err := dbWrapper.Get(ctx, &collection, `SELECT m.collection_uuid FROM catalog_collection_mappings m
 JOIN source_collection_revisions c ON c.collection_uuid=m.collection_uuid AND c.revision=1 AND c.origin='migration'
 WHERE m.source_uuid=? AND m.catalog_id=? AND m.import_uuid=?`, w.snapshot.SourceUUID, catalog, w.snapshot.RegistryImportUUID)
	if errors.Is(err, sql.ErrNoRows) {
		return "legacy_collection_unmapped", nil
	}
	if err != nil {
		return "", err
	}
	revision := 1
	r.CollectionUUID, r.CollectionRevision = &collection, &revision
	var snapshot struct {
		UUID     string  `db:"uuid"`
		State    string  `db:"state"`
		Evidence *string `db:"evidence"`
		Receipts *string `db:"receipts"`
	}
	err = dbWrapper.Get(ctx, &snapshot, `SELECT s.uuid,s.state,e.state AS evidence,r.state AS receipts
 FROM catalog_snapshots s LEFT JOIN catalog_evidence_imports e ON e.snapshot_uuid=s.uuid
 LEFT JOIN catalog_enrichment_imports r ON r.snapshot_uuid=s.uuid WHERE s.source_uuid=? AND s.catalog_id=?`, w.snapshot.SourceUUID, catalog)
	if errors.Is(err, sql.ErrNoRows) {
		return "legacy_catalog_snapshot_missing", nil
	}
	if err != nil {
		return "", err
	}
	if snapshot.State != "received" || snapshot.Evidence == nil || *snapshot.Evidence == "running" || snapshot.Receipts == nil || *snapshot.Receipts == "running" {
		return "", models.ErrAutomationSnapshotConflict
	}
	r.CatalogSnapshotUUID = &snapshot.UUID
	return "", nil
}

func (w *automationEnrichmentWork) post(ctx context.Context, catalog, key string, r *models.AutomationEnrichmentRecord) (string, error) {
	ref, err := scrape.CatalogLocalPostReference(w.snapshot.SourceUUID, catalog, key)
	if err != nil {
		return "invalid_legacy_post_reference", nil
	}
	r.PostReference = ref.Value
	if reason, err := w.collection(ctx, catalog, r); err != nil || reason != "" {
		return reason, err
	}
	post, err := (&SourceEvidenceStore{}).FindPostByIdentifier(ctx, ref)
	if err != nil {
		return "", err
	}
	if post == nil {
		return "legacy_post_unmatched", nil
	}
	r.PostUUID = &post.UUID
	if post.State != "active" {
		return "post_forgotten", nil
	}
	return "", nil
}

func (w *automationEnrichmentWork) queueURL(ctx context.Context, raw string, r *models.AutomationEnrichmentRecord) error {
	var id string
	err := dbWrapper.Get(ctx, &id, "SELECT uuid FROM source_post_urls WHERE post_uuid=? AND url=?", *r.PostUUID, raw)
	if err == nil {
		r.URLUUID = &id
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	details, err := archive.EncodeSourceJSON(map[string]any{"policy": scrape.AutomationEnrichmentPolicy, "snapshot_uuid": w.snapshot.UUID,
		"source_ordinal": r.Ordinal, "time_basis": "snapshot-boundary"})
	if err != nil {
		return err
	}
	observed, err := (&SourcePostLinksStore{}).ObserveURL(ctx, models.SourcePostURLInput{
		SourcePostEvidence: models.SourcePostEvidence{UUID: scrape.RegistryImportUUID(w.snapshot.UUID, "automation-enrichment-url:v1", strconv.FormatInt(r.Ordinal, 10)),
			PostUUID: *r.PostUUID, Origin: "migration", Basis: "automation-enrichment-input", ObservedAt: w.captured, Details: details}, URL: raw})
	if err != nil {
		return err
	}
	r.URLUUID, r.URLEvidenceUUID = &observed.URLUUID, &observed.UUID
	return nil
}

func (w *automationEnrichmentWork) cooldowns(ctx context.Context, job *scrape.AutomationEnrichmentJob) ([]scrape.AutomationEnrichmentCooldown, bool, error) {
	// Scope resolution is literal and bounded. Unknown cooldown semantics block
	// activation candidates instead of silently dropping a possibly longer delay.
	var unresolved bool
	err := dbWrapper.Get(ctx, &unresolved, `SELECT EXISTS(SELECT 1 FROM automation_snapshot_records e
 LEFT JOIN automation_enrichment_records r ON r.snapshot_uuid=e.snapshot_uuid AND r.ordinal=e.ordinal
 WHERE e.snapshot_uuid=? AND e.source_table='enrichment_cooldowns' AND (r.outcome IS NULL OR r.outcome!='mapped'))`, w.snapshot.UUID)
	if err != nil {
		return nil, false, err
	}
	var ret []scrape.AutomationEnrichmentCooldown
	scopes := []string{"platform:" + job.Platform}
	if job.AccountKey != nil {
		scopes = append(scopes, "account:"+*job.AccountKey)
	}
	for _, scope := range scopes {
		key, err := archive.EncodeSourceJSON([]string{scope})
		if err != nil {
			return nil, false, err
		}
		var row struct {
			Kind   string    `db:"cooldown_kind"`
			Value  string    `db:"cooldown_value"`
			Reason string    `db:"cooldown_reason"`
			Until  time.Time `db:"cooldown_until"`
		}
		err = dbWrapper.Get(ctx, &row, `SELECT r.cooldown_kind,r.cooldown_value,r.cooldown_reason,r.cooldown_until
 FROM automation_snapshot_records e JOIN automation_enrichment_records r ON r.snapshot_uuid=e.snapshot_uuid AND r.ordinal=e.ordinal
 WHERE e.snapshot_uuid=? AND e.source_table='enrichment_cooldowns' AND e.source_key=? AND r.disposition='cooldown'`, w.snapshot.UUID, string(key))
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, false, err
		}
		ret = append(ret, scrape.AutomationEnrichmentCooldown{Kind: row.Kind, Value: row.Value, Reason: row.Reason, Until: row.Until})
	}
	return ret, unresolved, nil
}
