package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

func discoveryReview(r *models.AutomationDiscoveryRecord, reason string) *models.AutomationDiscoveryRecord {
	r.Outcome, r.Disposition, r.Reason = "review", "review", reason
	return r
}

func (w *automationDiscoveryWork) source(ctx context.Context, table string, key []any) (*catalogEvidenceRow, *scrape.CatalogSnapshotRecord, error) {
	encoded, err := archive.EncodeSourceJSON(key)
	if err != nil {
		return nil, nil, err
	}
	var row catalogEvidenceRow
	err = dbWrapper.Get(ctx, &row, `SELECT ordinal,data,data_sha256 FROM automation_snapshot_records
 WHERE snapshot_uuid=? AND source_table=? AND source_key=?`, w.snapshot.UUID, table, string(encoded))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	record, err := w.decode(row)
	return &row, record, err
}

func (w *automationDiscoveryWork) account(ctx context.Context, key, platform string, r *models.AutomationDiscoveryRecord) (string, error) {
	var account string
	err := dbWrapper.Get(ctx, &account, `SELECT account_uuid FROM catalog_account_mappings
 WHERE source_uuid=? AND import_uuid=? AND account_key=?`, w.snapshot.SourceUUID, w.snapshot.RegistryImportUUID, key)
	if errors.Is(err, sql.ErrNoRows) {
		return "legacy_discovery_account_unmapped", nil
	}
	if err != nil {
		return "", err
	}
	item, err := (&SourceAccountStore{}).Find(ctx, account)
	if err != nil {
		return "", err
	}
	if item == nil || item.Namespace != "native:"+platform {
		return "legacy_discovery_account_namespace", nil
	}
	// Keep the original mapping even if a later reviewed consolidation redirects
	// it. Importing discovery does not assert a new alias or performer owner.
	r.AccountUUID = &account
	return "", nil
}

func (w *automationDiscoveryWork) post(ctx context.Context, catalog, key string, r *models.AutomationDiscoveryRecord) (string, error) {
	// Share the existing frozen catalog/collection and post-alias resolution.
	var mapped models.AutomationEnrichmentRecord
	reason, err := (&automationEnrichmentWork{w.automationImportWork}).post(ctx, catalog, key, &mapped)
	r.PostUUID, r.PostReference, r.CatalogSnapshotUUID = mapped.PostUUID, mapped.PostReference, mapped.CatalogSnapshotUUID
	r.CollectionUUID, r.CollectionRevision = mapped.CollectionUUID, mapped.CollectionRevision
	return reason, err
}

func (w *automationDiscoveryWork) accountTarget(ctx context.Context, prepared *scrape.AutomationDiscoveryPrepared, r *models.AutomationDiscoveryRecord) (string, error) {
	row, source, err := w.source(ctx, "discovery_accounts", []any{prepared.JobKey})
	if err != nil {
		return "", err
	}
	if row == nil {
		return "legacy_discovery_account_job_missing", nil
	}
	r.AccountOrdinal = &row.Ordinal
	account, reason := scrape.PrepareAutomationDiscovery(source.Table, source.Values, w.captured)
	if reason != "" {
		return "legacy_discovery_account_job_invalid", nil
	}
	if account.Platform != prepared.Platform || account.AccountKey != prepared.AccountKey {
		return "legacy_discovery_account_target_conflict", nil
	}
	if account.Status == "complete" {
		return "legacy_discovery_listing_already_complete", nil
	}
	return w.account(ctx, account.AccountKey, account.Platform, r)
}

func (w *automationDiscoveryWork) enrichment(ctx context.Context, prepared *scrape.AutomationDiscoveryPrepared, r *models.AutomationDiscoveryRecord) (string, error) {
	row, _, err := w.source(ctx, "enrichment_jobs", []any{prepared.CatalogID, prepared.PostKey, 1})
	if err != nil {
		return "", err
	}
	if row == nil {
		return "legacy_discovery_enrichment_missing", nil
	}
	r.EnrichmentOrdinal = &row.Ordinal
	e, err := (&AutomationEnrichmentImportStore{}).Record(ctx, w.snapshot.UUID, row.Ordinal)
	if err != nil {
		return "", err
	}
	if e == nil {
		return "", models.ErrAutomationSnapshotConflict
	}
	if e.PostUUID == nil || e.CollectionUUID == nil || *e.PostUUID != *r.PostUUID || *e.CollectionUUID != *r.CollectionUUID || e.CollectionRevision == nil || *e.CollectionRevision != 1 {
		return "legacy_discovery_enrichment_scope", nil
	}
	if e.Outcome != "mapped" {
		return "legacy_discovery_enrichment_requires_review", nil
	}
	switch prepared.Disposition {
	case "lookup":
		if e.URLUUID == nil || e.TargetUUID == nil {
			return "legacy_discovery_lookup_not_retained", nil
		}
		var raw string
		if err := dbWrapper.Get(ctx, &raw, "SELECT url FROM source_post_urls WHERE uuid=? AND post_uuid=?", *e.URLUUID, *r.PostUUID); err != nil {
			return "", err
		}
		if raw != prepared.Projection.CandidateURL {
			return "legacy_discovery_lookup_changed", nil
		}
	case "historical_completion":
		if e.ReceiptUUID == nil && e.CaptureUUID == nil {
			return "legacy_discovery_completion_requires_evidence", nil
		}
	case "source_present":
		if e.CaptureUUID == nil {
			return "legacy_discovery_completion_requires_evidence", nil
		}
	case "coalesced":
		if e.AliasOrdinal == nil || e.Disposition != "coalesced" {
			return "legacy_discovery_alias_requires_evidence", nil
		}
	}
	return "", nil
}

func (w *automationDiscoveryWork) record(ctx context.Context, row catalogEvidenceRow, source *scrape.CatalogSnapshotRecord) (*models.AutomationDiscoveryRecord, error) {
	r := &models.AutomationDiscoveryRecord{Ordinal: row.Ordinal, Outcome: "mapped"}
	prepared, reason := scrape.PrepareAutomationDiscovery(source.Table, source.Values, w.captured)
	if reason != "" {
		return discoveryReview(r, reason), nil
	}
	r.AutomationDiscoveryProjection, r.Disposition = prepared.Projection, prepared.Disposition
	if source.Table == "maintenance" {
		return r, nil
	}
	if source.Table == "discovery_accounts" {
		if reason, err := w.account(ctx, prepared.AccountKey, prepared.Platform, r); err != nil {
			return nil, err
		} else if reason != "" {
			return discoveryReview(r, reason), nil
		}
		job := scrape.AutomationEnrichmentJob{Platform: prepared.Platform, AccountKey: &prepared.AccountKey, NotBefore: *r.NotBefore}
		cooldowns, unresolved, err := (&automationEnrichmentWork{w.automationImportWork}).cooldowns(ctx, &job)
		if err != nil {
			return nil, err
		}
		deadline := scrape.EnrichmentNotBefore(job, cooldowns, time.Time{})
		r.NotBefore = &deadline
		if unresolved && prepared.Status != "complete" {
			return discoveryReview(r, "legacy_cooldowns_require_review"), nil
		}
		if len(prepared.StagedTarget) > 0 {
			target, record, err := w.source(ctx, "discovery_targets", []any{prepared.StagedTarget[0], prepared.StagedTarget[1]})
			if err != nil {
				return nil, err
			}
			if target == nil {
				return discoveryReview(r, "legacy_discovery_staged_target_missing"), nil
			}
			r.TargetOrdinal = &target.Ordinal
			parsed, reason := scrape.PrepareAutomationDiscovery(record.Table, record.Values, w.captured)
			if reason != "" || parsed.JobKey != prepared.JobKey || parsed.Status != "pending" {
				return discoveryReview(r, "legacy_discovery_staged_target_conflict"), nil
			}
			candidate, candidateRecord, err := w.source(ctx, "discovery_candidates", []any{prepared.StagedTarget[0], prepared.StagedTarget[1], prepared.StagedTarget[2]})
			if err != nil {
				return nil, err
			}
			if candidate == nil {
				return discoveryReview(r, "legacy_discovery_staged_candidate_missing"), nil
			}
			parsedCandidate, reason := scrape.PrepareAutomationDiscovery(candidateRecord.Table, candidateRecord.Values, w.captured)
			if reason != "" || parsedCandidate.Projection.CandidateBasis != "title-needs-verification" {
				return discoveryReview(r, "legacy_discovery_staged_candidate_conflict"), nil
			}
		}
		if r.StagedSHA256 != "" {
			return discoveryReview(r, "legacy_discovery_"+r.StagedKind+"_conversion"), nil
		}
		return r, nil
	}
	if reason, err := w.post(ctx, prepared.CatalogID, prepared.PostKey, r); err != nil {
		return nil, err
	} else if reason != "" {
		return discoveryReview(r, reason), nil
	}
	if source.Table == "discovery_candidates" {
		parent, record, err := w.source(ctx, "discovery_targets", []any{prepared.CatalogID, prepared.PostKey})
		if err != nil {
			return nil, err
		}
		if parent == nil {
			return discoveryReview(r, "legacy_discovery_target_missing"), nil
		}
		r.TargetOrdinal = &parent.Ordinal
		target, reason := scrape.PrepareAutomationDiscovery(record.Table, record.Values, w.captured)
		if reason != "" || target.Status != "pending" {
			return discoveryReview(r, "legacy_discovery_candidate_target_not_pending"), nil
		}
		scope, err := scrape.SourceScopeV1(r.CandidateURL)
		if err != nil || scope != "service:"+target.Platform {
			return discoveryReview(r, "legacy_discovery_candidate_service_conflict"), nil
		}
		if reason, err := w.accountTarget(ctx, target, r); err != nil {
			return nil, err
		} else if reason != "" {
			return discoveryReview(r, reason), nil
		}
		return r, nil
	}
	if prepared.Disposition == "review" {
		return discoveryReview(r, "legacy_discovery_"+prepared.Status), nil
	}
	if prepared.Status == "pending" {
		if reason, err := w.accountTarget(ctx, prepared, r); err != nil {
			return nil, err
		} else if reason != "" {
			return discoveryReview(r, reason), nil
		}
		return r, nil
	}
	if reason, err := w.enrichment(ctx, prepared, r); err != nil {
		return nil, err
	} else if reason != "" {
		return discoveryReview(r, reason), nil
	}
	return r, nil
}
