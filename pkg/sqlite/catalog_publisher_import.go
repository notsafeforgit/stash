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

type CatalogPublisherImportStore struct{}

const catalogPublisherColumns = `r.ordinal,e.source_table,e.source_key,e.data_sha256,e.post_uuid,r.capture_uuid,r.decision_uuid,d.account_uuid,a.canonical_uuid AS canonical_account_uuid,r.outcome,r.reason,r.created_account`
const catalogPublisherJoins = ` FROM catalog_publisher_records r JOIN catalog_evidence_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
 LEFT JOIN capture_publisher_decisions d ON d.uuid=r.decision_uuid LEFT JOIN source_accounts a ON a.uuid=d.account_uuid`

func (s *CatalogPublisherImportStore) Find(ctx context.Context, id string) (*models.CatalogPublisherImport, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	ret := &models.CatalogPublisherImport{}
	if err := dbWrapper.Get(ctx, ret, "SELECT * FROM catalog_publisher_imports WHERE snapshot_uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return ret, nil
}

type catalogPublisherContext struct {
	Policy              string   `json:"policy"`
	Action              string   `json:"action"`
	Signature           string   `json:"signature,omitempty"`
	Namespace           string   `json:"namespace,omitempty"`
	CandidateUUIDs      []string `json:"candidate_uuids"`
	CandidatesTruncated bool     `json:"candidates_truncated"`
	Conflicts           []string `json:"conflicts"`
	IdentityError       string   `json:"identity_error,omitempty"`
	SourceReason        string   `json:"source_reason,omitempty"`
	CurrentDecisionUUID *string  `json:"current_decision_uuid,omitempty"`
	SuggestedAccount    *string  `json:"suggested_account_uuid,omitempty"`
}

func importCatalogPublisher(ctx context.Context, snapshot string, row catalogCaptureSource) (*models.CatalogPublisherRecord, []byte, error) {
	result := &models.CatalogPublisherRecord{Ordinal: row.Ordinal, CaptureUUID: row.Capture}
	view := catalogPublisherContext{Policy: archive.CapturedAccountPolicy, CandidateUUIDs: []string{}, Conflicts: []string{}}
	if row.Capture == nil {
		result.Outcome, result.Reason = "review", "source_evidence_requires_review"
		view.Action, view.SourceReason = "unmapped", row.Reason
	} else {
		store := &CapturePublisherStore{}
		preview, err := store.Preview(ctx, *row.Capture, "")
		if err != nil {
			return nil, nil, err
		}
		view.Action, view.Signature = preview.Action, preview.Signature
		view.Conflicts, view.IdentityError = preview.Conflicts, preview.IdentityError
		view.CandidatesTruncated, view.SuggestedAccount = preview.CandidatesTruncated, preview.AccountUUID
		if preview.Identity != nil {
			view.Namespace = preview.Identity.Namespace
		}
		for _, candidate := range preview.Candidates {
			view.CandidateUUIDs = append(view.CandidateUUIDs, candidate.Account.UUID)
		}
		if preview.Current != nil {
			view.CurrentDecisionUUID = &preview.Current.UUID
			result.DecisionUUID = &preview.Current.UUID
		}
		switch preview.Action {
		case "link", "create":
			decision, err := store.Apply(ctx, models.CapturePublisherInput{
				UUID:        scrape.RegistryImportUUID(snapshot, "catalog-publisher", strconv.FormatInt(row.Ordinal, 10)),
				CaptureUUID: *row.Capture, ExpectedSignature: preview.Signature, Action: "automatic",
			})
			if err != nil {
				return nil, nil, err
			}
			result.Outcome, result.DecisionUUID, result.CreatedAccount = "linked", &decision.UUID, preview.Action == "create"
		case "preserve":
			result.Outcome, result.Reason = "preserved", "existing_publisher_decision"
		case "review":
			result.Outcome, result.Reason = "review", "publisher_identity_requires_review"
		case "unavailable":
			result.Outcome, result.Reason = "unavailable", "missing_captured_identity"
			if preview.PostState != "active" {
				result.Reason = "post_forgotten"
			} else if preview.IdentityError != "" {
				result.Outcome, result.Reason = "review", "invalid_captured_identity"
			}
		default:
			return nil, nil, models.ErrCatalogSnapshotInvalid
		}
	}
	body, err := archive.EncodeSourceJSON(view)
	if err != nil || len(body) > 65536 {
		return nil, nil, models.ErrCatalogSnapshotInvalid
	}
	return result, body, nil
}

func (s *CatalogPublisherImportStore) Advance(ctx context.Context, id, expected string, after int64, now time.Time) (*models.CatalogPublisherImport, error) {
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
	if prior != nil && prior.Policy != archive.CapturedAccountPolicy {
		return nil, models.ErrCatalogSnapshotConflict
	}
	evidence, err := (&CatalogEvidenceImportStore{}).Find(ctx, id)
	if err != nil {
		return nil, err
	}
	relations, err := (&CatalogRelationsImportStore{}).Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if evidence == nil || evidence.State == "running" || evidence.ManifestSHA256 != expected || relations == nil || relations.State == "running" || relations.ManifestSHA256 != expected {
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
	complete := snapshotAtomicWrite(ctx)
	stamp := now.UTC().Format(time.RFC3339Nano)
	if prior == nil {
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO catalog_publisher_imports(snapshot_uuid,manifest_sha256,policy,state,source_records,created_at,updated_at) VALUES(?,?,?,'running',?,?,?)`, id, expected, archive.CapturedAccountPolicy, manifest.Captures.Count, stamp, stamp); err != nil {
			return nil, err
		}
		prior, err = s.Find(ctx, id)
		if err != nil {
			return nil, err
		}
	}
	var usedBytes int64
	for count := 0; count < 50 && usedBytes < 16<<20; count++ {
		var row catalogCaptureSource
		err := dbWrapper.Get(ctx, &row, "SELECT ordinal,capture_uuid,reason FROM catalog_evidence_records WHERE snapshot_uuid=? AND ordinal>? AND "+catalogCaptureCandidates+" ORDER BY ordinal LIMIT 1", id, prior.LastOrdinal)
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
		if row.Capture != nil {
			size, err := catalogCapturePayloadBytes(ctx, *row.Capture)
			if err != nil {
				return nil, err
			}
			usedBytes += size
		}
		result, view, err := importCatalogPublisher(ctx, id, row)
		if err != nil {
			return nil, err
		}
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO catalog_publisher_records(snapshot_uuid,ordinal,capture_uuid,decision_uuid,outcome,reason,created_account,context_json) VALUES(?,?,?,?,?,?,?,?)`, id, row.Ordinal, result.CaptureUUID, result.DecisionUUID, result.Outcome, result.Reason, result.CreatedAccount, string(view)); err != nil {
			return nil, err
		}
		prior.LastOrdinal = row.Ordinal
		prior.ProcessedRecords++
		switch result.Outcome {
		case "linked":
			prior.LinkedRecords++
		case "preserved":
			prior.PreservedRecords++
		case "review":
			prior.ReviewRecords++
		case "unavailable":
			prior.UnavailableRecords++
		}
		if result.CreatedAccount {
			prior.CreatedAccounts++
		}
	}
	if _, err := dbWrapper.Exec(ctx, `UPDATE catalog_publisher_imports SET state=?,last_ordinal=?,processed_records=?,linked_records=?,preserved_records=?,review_records=?,unavailable_records=?,created_accounts=?,updated_at=? WHERE snapshot_uuid=?`, prior.State, prior.LastOrdinal, prior.ProcessedRecords, prior.LinkedRecords, prior.PreservedRecords, prior.ReviewRecords, prior.UnavailableRecords, prior.CreatedAccounts, stamp, id); err != nil {
		return nil, err
	}
	result, err := s.Find(ctx, id)
	*complete = err == nil
	return result, err
}

func (s *CatalogPublisherImportStore) Records(ctx context.Context, id string, after int64, limit int) ([]models.CatalogPublisherRecord, error) {
	if !validSourceRunUUID(id) || after < 0 || limit < 1 || limit > 100 {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	columns := strings.Replace(catalogPublisherColumns, "e.source_key", `CASE WHEN length(CAST(e.source_key AS BLOB))<=8192 THEN e.source_key ELSE '' END AS source_key,length(CAST(e.source_key AS BLOB))>8192 AS key_omitted`, 1)
	result := []models.CatalogPublisherRecord{}
	err := dbWrapper.Select(ctx, &result, "SELECT "+columns+catalogPublisherJoins+" WHERE r.snapshot_uuid=? AND r.ordinal>? ORDER BY r.ordinal LIMIT ?", id, after, limit)
	return result, err
}

func (s *CatalogPublisherImportStore) Record(ctx context.Context, id string, ordinal int64) (*models.CatalogPublisherRecordDetails, error) {
	if !validSourceRunUUID(id) || ordinal < 1 {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	var row struct {
		models.CatalogPublisherRecord
		Context string `db:"context_json"`
	}
	err := dbWrapper.Get(ctx, &row, "SELECT "+catalogPublisherColumns+",r.context_json"+catalogPublisherJoins+" WHERE r.snapshot_uuid=? AND r.ordinal=?", id, ordinal)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &models.CatalogPublisherRecordDetails{CatalogPublisherRecord: row.CatalogPublisherRecord, Context: json.RawMessage(row.Context)}, nil
}
