package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

type CatalogRelationsImportStore struct{}

const catalogRelationTablesSQL = "('accounts','handles','posts','post_urls','post_aliases')"
const catalogRelationColumns = `ordinal,source_table,source_key,data_sha256,outcome,reason,account_uuid,identifier_uuid,identifier_evidence_key,post_uuid,url_evidence_uuid,post_identifier_evidence_uuid,account_claim_uuid`

func (s *CatalogRelationsImportStore) Find(ctx context.Context, id string) (*models.CatalogRelationsImport, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	ret := &models.CatalogRelationsImport{}
	if err := dbWrapper.Get(ctx, ret, "SELECT * FROM catalog_relations_imports WHERE snapshot_uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return ret, nil
}

type catalogRelationsWork struct{ catalogEvidenceWork }

func (w *catalogRelationsWork) evidence(row catalogEvidenceRow, record *scrape.CatalogSnapshotRecord) (models.SourcePostEvidence, error) {
	stamp, err := time.Parse(time.RFC3339Nano, w.snapshot.CapturedAt)
	if err != nil {
		return models.SourcePostEvidence{}, models.ErrCatalogSnapshotInvalid
	}
	details, err := archive.EncodeSourceJSON(map[string]any{"snapshot_uuid": w.snapshot.UUID, "catalog_id": w.snapshot.CatalogID, "source_uuid": w.snapshot.SourceUUID, "source_table": record.Table, "source_key": record.Key, "source_sha256": row.SHA256})
	if err != nil {
		return models.SourcePostEvidence{}, err
	}
	id := scrape.RegistryImportUUID(w.snapshot.UUID, "catalog-relation", strconv.FormatInt(row.Ordinal, 10))
	return models.SourcePostEvidence{UUID: id, Origin: "migration", Basis: "catalog-" + strings.ReplaceAll(record.Table, "_", "-"), ObservedAt: stamp, Details: details}, nil
}

func catalogRelationReview(r *models.CatalogRelationRecord, reason string) *models.CatalogRelationRecord {
	r.Outcome, r.Reason = "review", reason
	return r
}

func (w *catalogRelationsWork) registryAccount(ctx context.Context, key string) (*models.SourceAccount, error) {
	var id string
	err := dbWrapper.Get(ctx, &id, "SELECT account_uuid FROM catalog_account_mappings WHERE source_uuid=? AND import_uuid=? AND account_key=?", w.snapshot.SourceUUID, w.snapshot.RegistryImportUUID, key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return (&SourceAccountStore{}).Find(ctx, id)
}

func (w *catalogRelationsWork) importedAccount(ctx context.Context, key string) (*models.SourceAccount, string, error) {
	body, err := archive.EncodeSourceJSON([]string{key})
	if err != nil {
		return nil, "", err
	}
	var row struct {
		Account *string `db:"account_uuid"`
		Outcome string  `db:"outcome"`
		Reason  string  `db:"reason"`
	}
	err = dbWrapper.Get(ctx, &row, "SELECT account_uuid,outcome,reason FROM catalog_relation_records WHERE snapshot_uuid=? AND source_table='accounts' AND source_key=?", w.snapshot.UUID, string(body))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "legacy_account_row_missing", nil
	}
	if err != nil {
		return nil, "", err
	}
	if row.Outcome != "mapped" || row.Account == nil {
		return nil, "legacy_account_requires_review", nil
	}
	account, err := (&SourceAccountStore{}).Find(ctx, *row.Account)
	if err != nil {
		return nil, "", err
	}
	if account == nil {
		return nil, "", models.ErrCatalogSnapshotInvalid
	}
	return account, "", nil
}

func (w *catalogRelationsWork) nativePost(ctx context.Context, key string) (*models.SourcePost, string, error) {
	body, err := archive.EncodeSourceJSON([]string{key})
	if err != nil {
		return nil, "", err
	}
	var mapped catalogEvidencePost
	err = dbWrapper.Get(ctx, &mapped, "SELECT post_uuid,basis,reason FROM catalog_evidence_posts WHERE snapshot_uuid=? AND source_key=?", w.snapshot.UUID, string(body))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "legacy_post_mapping_missing", nil
	}
	if err != nil {
		return nil, "", err
	}
	if mapped.Reason != "" || mapped.PostUUID == nil {
		return nil, "legacy_post_requires_review", nil
	}
	post, err := (&SourceEvidenceStore{}).FindPost(ctx, *mapped.PostUUID)
	if err != nil {
		return nil, "", err
	}
	if post == nil {
		return nil, "", models.ErrCatalogSnapshotInvalid
	}
	if post.State != "active" {
		return post, "post_forgotten", nil
	}
	return post, "", nil
}

func (w *catalogRelationsWork) account(ctx context.Context, row catalogEvidenceRow, record *scrape.CatalogSnapshotRecord, result *models.CatalogRelationRecord) (*models.CatalogRelationRecord, error) {
	key, keyOK := record.Values["account_key"].(string)
	if !keyOK || key == "" {
		return catalogRelationReview(result, "invalid_legacy_account_key"), nil
	}
	evidence, err := w.evidence(row, record)
	if err != nil {
		return nil, err
	}
	var account *models.SourceAccount
	var reference models.AccountReference
	stamp := evidence.ObservedAt
	if record.Table == "accounts" {
		account, err = w.registryAccount(ctx, key)
		if err != nil {
			return nil, err
		}
		if account == nil {
			return catalogRelationReview(result, "legacy_account_not_mapped"), nil
		}
		platform, ok := record.Values["platform"].(string)
		validNamespace := account.Namespace == "native:"+platform || (strings.HasPrefix(account.Namespace, "mirror:") && strings.HasSuffix(account.Namespace, ":"+platform))
		if !ok || !validNamespace {
			return catalogRelationReview(result, "legacy_account_namespace_mismatch"), nil
		}
		// A legacy source-id may have come from a folder or mirror. Preserve
		// that key without promoting its source_id to a captured native ID.
		reference = models.AccountReference{Namespace: account.Namespace, Kind: "legacy_key", Value: key}
	} else {
		var reason string
		account, reason, err = w.importedAccount(ctx, key)
		if err != nil {
			return nil, err
		}
		if reason != "" {
			return catalogRelationReview(result, reason), nil
		}
		handle, ok := record.Values["handle"].(string)
		if !ok {
			return catalogRelationReview(result, "invalid_legacy_handle"), nil
		}
		observed, ok := record.Values["first_observed"].(string)
		if !ok {
			return catalogRelationReview(result, "invalid_legacy_handle_time"), nil
		}
		stamp, err = time.Parse(time.RFC3339Nano, observed)
		if err != nil || stamp.IsZero() || stamp.UTC().Year() < 1 || stamp.UTC().Year() > 9999 {
			return catalogRelationReview(result, "invalid_legacy_handle_time"), nil
		}
		kind := "handle"
		// Old mirror handle fields can contain display names.
		if strings.HasPrefix(account.Namespace, "mirror:") {
			kind = "legacy_label"
		}
		reference = models.AccountReference{Namespace: account.Namespace, Kind: kind, Value: handle}
	}
	result.AccountUUID = &account.UUID
	reference, err = archive.NormalizeAccountReference(reference)
	if err != nil {
		return catalogRelationReview(result, "invalid_legacy_account_reference"), nil
	}
	identifier, err := (&SourceAccountStore{}).ObserveIdentifier(ctx, account.UUID, reference, models.AccountIdentifierEvidence{Key: evidence.UUID, Basis: evidence.Basis, Origin: evidence.Origin, FirstObserved: stamp, LastObserved: stamp, Details: evidence.Details})
	if err != nil {
		return nil, err
	}
	result.IdentifierUUID = &identifier.UUID
	result.IdentifierEvidenceKey = &evidence.UUID
	return result, nil
}

func (w *catalogRelationsWork) record(ctx context.Context, row catalogEvidenceRow, record *scrape.CatalogSnapshotRecord) (*models.CatalogRelationRecord, error) {
	result := &models.CatalogRelationRecord{Ordinal: row.Ordinal, Table: record.Table, Key: record.KeyJSON, SHA256: row.SHA256, Outcome: "mapped"}
	if len(record.KeyJSON) > 16384 {
		return catalogRelationReview(result, "legacy_relationship_key_exceeds_limit"), nil
	}
	if record.Table == "accounts" || record.Table == "handles" {
		return w.account(ctx, row, record, result)
	}
	key, ok := record.Values["post_key"].(string)
	if !ok || key == "" {
		return catalogRelationReview(result, "invalid_legacy_post_key"), nil
	}
	post, reason, err := w.nativePost(ctx, key)
	if err != nil {
		return nil, err
	}
	if post != nil {
		result.PostUUID = &post.UUID
	}
	if reason != "" {
		return catalogRelationReview(result, reason), nil
	}
	evidence, err := w.evidence(row, record)
	if err != nil {
		return nil, err
	}
	evidence.PostUUID = post.UUID
	links := &SourcePostLinksStore{}
	switch record.Table {
	case "post_urls":
		value, ok := record.Values["url"].(string)
		if !ok {
			return catalogRelationReview(result, "invalid_legacy_post_url"), nil
		}
		stored, err := links.ObserveURL(ctx, models.SourcePostURLInput{SourcePostEvidence: evidence, URL: value})
		if errors.Is(err, models.ErrSourcePostEvidenceInvalid) {
			return catalogRelationReview(result, "invalid_legacy_post_url"), nil
		}
		if err != nil {
			return nil, err
		}
		result.URLEvidenceUUID = &stored.UUID
	case "post_aliases":
		value, ok := record.Values["alias_key"].(string)
		if !ok {
			return catalogRelationReview(result, "invalid_legacy_post_alias"), nil
		}
		identifier, err := scrape.CatalogLocalPostReference(w.snapshot.SourceUUID, w.snapshot.CatalogID, value)
		if err != nil {
			return catalogRelationReview(result, "invalid_legacy_post_alias"), nil
		}
		stored, err := links.ObserveIdentifier(ctx, models.SourcePostIdentifierInput{SourcePostEvidence: evidence, Identifier: identifier, ExpectedPostRevision: post.Revision})
		if errors.Is(err, models.ErrSourcePostConflict) {
			return catalogRelationReview(result, "post_alias_already_bound"), nil
		}
		if err != nil {
			return nil, err
		}
		result.PostIdentifierEvidenceUUID = &stored.UUID
	case "posts":
		if record.Values["account_key"] == nil {
			result.Outcome, result.Reason = "unassigned", "no_legacy_account"
			return result, nil
		}
		value, ok := record.Values["account_key"].(string)
		if !ok {
			return catalogRelationReview(result, "invalid_legacy_account_key"), nil
		}
		account, reason, err := w.importedAccount(ctx, value)
		if err != nil {
			return nil, err
		}
		if reason != "" {
			return catalogRelationReview(result, reason), nil
		}
		result.AccountUUID = &account.UUID
		allowed, err := publisherNamespaceAllowed(ctx, post.UUID, account.Namespace)
		if err != nil {
			return nil, err
		}
		if !allowed {
			return catalogRelationReview(result, "legacy_post_account_namespace_mismatch"), nil
		}
		stored, err := links.ClaimAccount(ctx, models.SourcePostAccountClaimInput{SourcePostEvidence: evidence, AccountUUID: account.UUID})
		if err != nil {
			return nil, err
		}
		result.AccountClaimUUID = &stored.UUID
	default:
		return nil, models.ErrCatalogSnapshotInvalid
	}
	return result, nil
}

func (s *CatalogRelationsImportStore) Advance(ctx context.Context, id, expected string, after int64, now time.Time) (*models.CatalogRelationsImport, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validSourceRunUUID(id) || !archive.ValidSHA256(expected) || after < 0 || !validJobTime(now) {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	snapshot, err := (&CatalogSnapshotStore{}).Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if snapshot == nil || snapshot.ManifestSHA256 != expected || snapshot.State != "received" {
		return nil, models.ErrCatalogSnapshotConflict
	}
	prior, err := s.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if (prior == nil && after != 0) || (prior != nil && prior.LastOrdinal != after) {
		return nil, models.ErrCatalogSnapshotConflict
	}
	if prior != nil && prior.State != "running" {
		return prior, nil
	}
	parent, err := (&CatalogEvidenceImportStore{}).Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if parent == nil || parent.State == "running" || parent.ManifestSHA256 != expected {
		return nil, models.ErrCatalogSnapshotConflict
	}
	var body []byte
	if err := dbWrapper.Get(ctx, &body, "SELECT manifest FROM catalog_snapshots WHERE uuid=?", id); err != nil {
		return nil, err
	}
	manifest, err := scrape.PrepareCatalogSnapshot(body, expected)
	if err != nil {
		return nil, err
	}
	work := &catalogRelationsWork{catalogEvidenceWork{snapshot: snapshot, manifest: manifest}}
	complete := snapshotAtomicWrite(ctx)
	stamp := now.UTC().Format(time.RFC3339Nano)
	if prior == nil {
		var total int64
		for _, table := range []string{"accounts", "handles", "posts", "post_urls", "post_aliases"} {
			total += manifest.Tables[table].Rows
		}
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO catalog_relations_imports(snapshot_uuid,manifest_sha256,state,source_records,created_at,updated_at) VALUES(?,?,'running',?,?,?)`, id, expected, total, stamp, stamp); err != nil {
			return nil, err
		}
		prior, err = s.Find(ctx, id)
		if err != nil {
			return nil, err
		}
	}
	for count := 0; count < 50 && work.bytes < 16<<20; count++ {
		var row catalogEvidenceRow
		err := dbWrapper.Get(ctx, &row, "SELECT ordinal,data,data_sha256 FROM catalog_snapshot_records WHERE snapshot_uuid=? AND ordinal>? AND source_table IN "+catalogRelationTablesSQL+" ORDER BY ordinal LIMIT 1", id, prior.LastOrdinal)
		if errors.Is(err, sql.ErrNoRows) {
			if prior.ProcessedRecords != prior.TotalRecords {
				return nil, models.ErrCatalogSnapshotInvalid
			}
			prior.State = "mapped"
			if prior.ReviewRecords != 0 {
				prior.State = "review"
			}
			break
		}
		if err != nil {
			return nil, err
		}
		record, err := work.decode(row)
		if err != nil {
			return nil, err
		}
		outcome, err := work.record(ctx, row, record)
		if err != nil {
			return nil, err
		}
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO catalog_relation_records(snapshot_uuid,ordinal,source_table,source_key,data_sha256,source_values,outcome,reason,account_uuid,identifier_uuid,identifier_evidence_key,post_uuid,url_evidence_uuid,post_identifier_evidence_uuid,account_claim_uuid) VALUES(?,?,?,?,?,json_extract(?,'$.values'),?,?,?,?,?,?,?,?,?)`, id, outcome.Ordinal, outcome.Table, outcome.Key, outcome.SHA256, row.Data, outcome.Outcome, outcome.Reason, outcome.AccountUUID, outcome.IdentifierUUID, outcome.IdentifierEvidenceKey, outcome.PostUUID, outcome.URLEvidenceUUID, outcome.PostIdentifierEvidenceUUID, outcome.AccountClaimUUID); err != nil {
			return nil, err
		}
		prior.LastOrdinal = row.Ordinal
		prior.ProcessedRecords++
		switch outcome.Outcome {
		case "mapped":
			prior.MappedRecords++
		case "review":
			prior.ReviewRecords++
		case "unassigned":
			prior.UnassignedRecords++
		}
	}
	if _, err := dbWrapper.Exec(ctx, `UPDATE catalog_relations_imports SET state=?,last_ordinal=?,processed_records=?,mapped_records=?,review_records=?,unassigned_records=?,updated_at=? WHERE snapshot_uuid=?`, prior.State, prior.LastOrdinal, prior.ProcessedRecords, prior.MappedRecords, prior.ReviewRecords, prior.UnassignedRecords, stamp, id); err != nil {
		return nil, err
	}
	result, err := s.Find(ctx, id)
	*complete = err == nil
	return result, err
}

func (s *CatalogRelationsImportStore) Records(ctx context.Context, id string, after int64, limit int) ([]models.CatalogRelationRecord, error) {
	if !validSourceRunUUID(id) || after < 0 || limit < 1 || limit > 100 {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	columns := strings.Replace(catalogRelationColumns, "source_key", `CASE WHEN length(CAST(source_key AS BLOB))<=8192 THEN source_key ELSE '' END AS source_key,length(CAST(source_key AS BLOB))>8192 AS key_omitted`, 1)
	result := []models.CatalogRelationRecord{}
	err := dbWrapper.Select(ctx, &result, "SELECT "+columns+" FROM catalog_relation_records WHERE snapshot_uuid=? AND ordinal>? ORDER BY ordinal LIMIT ?", id, after, limit)
	return result, err
}

func (s *CatalogRelationsImportStore) Record(ctx context.Context, id string, ordinal int64) (*models.CatalogRelationRecordDetails, error) {
	if !validSourceRunUUID(id) || ordinal < 1 {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	var row struct {
		models.CatalogRelationRecord
		Values string `db:"source_values"`
	}
	err := dbWrapper.Get(ctx, &row, "SELECT "+catalogRelationColumns+",source_values FROM catalog_relation_records WHERE snapshot_uuid=? AND ordinal=?", id, ordinal)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &models.CatalogRelationRecordDetails{CatalogRelationRecord: row.CatalogRelationRecord, SourceValues: json.RawMessage(row.Values)}, nil
}
