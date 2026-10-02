package sqlite

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

type CatalogDocumentImportStore struct{}

const catalogDocumentPolicy = "catalog-documents-v1"
const catalogDocumentParser = "legacy-catalog-nfo-v1"
const catalogDocumentColumns = `r.ordinal,e.source_table,e.source_key,e.data_sha256,r.document_uuid,r.source_uuid,r.post_uuid,r.claim_uuid,r.head_uuid,r.selection_basis,r.outcome,r.reason`
const catalogDocumentJoins = ` FROM catalog_document_records r JOIN catalog_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal`

var catalogDocumentPhases = [...]string{"sidecar_documents", "sidecar_sources", "sidecars", "sidecar_heads", "complete"}

func (s *CatalogDocumentImportStore) Find(ctx context.Context, id string) (*models.CatalogDocumentImport, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	ret := &models.CatalogDocumentImport{}
	if err := dbWrapper.Get(ctx, ret, "SELECT * FROM catalog_document_imports WHERE snapshot_uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return ret, nil
}

type catalogDocumentWork struct {
	catalogRelationsWork
	progress *models.CatalogDocumentImport
}

func documentReview(r *models.CatalogDocumentRecord, reason string) error {
	r.Outcome, r.Reason = "review", reason
	return nil
}

func (w *catalogDocumentWork) details(basis string, postKey any) (json.RawMessage, error) {
	return archive.EncodeSourceJSON(map[string]any{"policy": catalogDocumentPolicy, "registry_source_uuid": w.snapshot.SourceUUID,
		"catalog_id": w.snapshot.CatalogID, "basis": basis, "legacy_post_key": postKey})
}

// Snapshot UUIDs, ordinals and local document IDs belong to import receipts.
// Equivalent historical facts share domain identities across later snapshots.
func (w *catalogDocumentWork) id(kind string, values ...any) (string, error) {
	body, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return scrape.RegistryImportUUID(w.snapshot.SourceUUID, "catalog-document:"+kind+":v1", scrape.CatalogSnapshotSHA(body)), nil
}

func (w *catalogDocumentWork) parent(ctx context.Context, table string, key ...any) (*models.CatalogDocumentRecord, error) {
	body, err := archive.EncodeSourceJSON(key)
	if err != nil {
		return nil, err
	}
	var ret models.CatalogDocumentRecord
	err = dbWrapper.Get(ctx, &ret, "SELECT "+catalogDocumentColumns+catalogDocumentJoins+" WHERE e.snapshot_uuid=? AND e.source_table=? AND e.source_key=?", w.snapshot.UUID, table, string(body))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &ret, err
}

func (w *catalogDocumentWork) document(ctx context.Context, record *scrape.CatalogSnapshotRecord, r *models.CatalogDocumentRecord) error {
	v := record.Values
	input := models.SourceDocumentInput{Parser: catalogDocumentParser}
	var ok bool
	for key, target := range map[string]*string{"encoding": &input.Encoding, "parse_status": &input.ParseStatus} {
		*target, ok = v[key].(string)
		if !ok {
			return documentReview(r, "invalid_document_interpretation")
		}
	}
	warnings, warningsOK := v["warnings_json"].(string)
	parsed, parsedOK := v["parsed_json"].(string)
	if !warningsOK || !parsedOK {
		return documentReview(r, "invalid_document_interpretation")
	}
	input.Warnings, input.Parsed = json.RawMessage(warnings), json.RawMessage(parsed)
	blob, ok := v["raw_content"].(map[string]any)
	if !ok {
		return models.ErrCatalogSnapshotInvalid
	}
	encoded, ok := blob["sqlite_blob_base64"].(string)
	if !ok {
		return models.ErrCatalogSnapshotInvalid
	}
	var err error
	input.Content, err = base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || scrape.CatalogSnapshotSHA(input.Content) != v["content_sha256"] {
		return models.ErrCatalogSnapshotInvalid
	}
	doc, err := (&SourceDocumentStore{}).Retain(ctx, input)
	if errors.Is(err, models.ErrSourceDocumentInvalid) {
		return documentReview(r, "invalid_document_interpretation")
	}
	if err != nil {
		return err
	}
	r.DocumentUUID = &doc.UUID
	return nil
}

func (w *catalogDocumentWork) source(ctx context.Context, record *scrape.CatalogSnapshotRecord, r *models.CatalogDocumentRecord) error {
	v := record.Values
	path, pathOK := v["relpath"].(string)
	stamp, stampOK := v["captured_at"].(string)
	if !pathOK || !stampOK || !archive.ValidDocumentPath(path) || !archive.ValidDocumentSourceTime(stamp) {
		return documentReview(r, "invalid_document_source")
	}
	store := &SourceDocumentStore{}
	if r.DocumentUUID == nil {
		parent, err := w.parent(ctx, "sidecar_documents", v["document_id"])
		if err != nil {
			return err
		}
		if parent == nil || parent.DocumentUUID == nil {
			return documentReview(r, "document_interpretation_requires_review")
		}
		r.DocumentUUID = parent.DocumentUUID
	}
	doc, err := store.Find(ctx, *r.DocumentUUID)
	if err != nil {
		return err
	}
	if doc == nil || doc.ContentSHA256 != v["content_sha256"] {
		return documentReview(r, "document_source_hash_conflicts")
	}
	if v["post_key"] != nil {
		key, ok := v["post_key"].(string)
		if !ok || key == "" {
			return documentReview(r, "invalid_document_post_key")
		}
		post, reason, err := w.nativePost(ctx, key)
		if err != nil {
			return err
		}
		if post != nil {
			r.PostUUID = &post.UUID
		}
		if reason != "" && reason != "post_forgotten" {
			return documentReview(r, reason)
		}
	}
	details, err := w.details("source", v["post_key"])
	if err != nil {
		return err
	}
	id, err := w.id("source", w.snapshot.CatalogID, w.progress.CollectionUUID, w.progress.CollectionRevision, doc.UUID, path, r.PostUUID, stamp, string(details))
	if err != nil {
		return err
	}
	source, err := store.RecordSource(ctx, models.SourceDocumentSource{UUID: id, DocumentUUID: doc.UUID, CollectionUUID: w.progress.CollectionUUID,
		CollectionRevision: w.progress.CollectionRevision, RelativePath: path, PostUUID: r.PostUUID, CapturedAt: stamp, Origin: "migration", Details: details})
	if errors.Is(err, models.ErrSourcePostForgotten) {
		return documentReview(r, "post_forgotten")
	}
	if err != nil {
		return err
	}
	r.SourceUUID = &source.UUID
	return w.fallback(ctx, record, source, r)
}

func (w *catalogDocumentWork) fallback(ctx context.Context, record *scrape.CatalogSnapshotRecord, source *models.SourceDocumentSource, r *models.CatalogDocumentRecord) error {
	key, err := archive.EncodeSourceJSON([]string{source.RelativePath})
	if err != nil {
		return err
	}
	var explicit bool
	if err := dbWrapper.Get(ctx, &explicit, "SELECT EXISTS(SELECT 1 FROM catalog_snapshot_records WHERE snapshot_uuid=? AND source_table='sidecar_heads' AND source_key=?)", w.snapshot.UUID, string(key)); err != nil {
		return err
	}
	if explicit {
		return nil
	}
	var ordinal int64
	err = dbWrapper.Get(ctx, &ordinal, `SELECT ordinal FROM catalog_snapshot_records
 WHERE snapshot_uuid=? AND source_table=? AND source_table IN ('sidecars','sidecar_sources') AND json_extract(data,'$.values.relpath')=?
 ORDER BY json_extract(data,'$.values.captured_at') DESC,json_extract(data,'$.values.content_sha256') DESC LIMIT 1`, w.snapshot.UUID, record.Table, source.RelativePath)
	if err != nil {
		return err
	}
	if ordinal != r.Ordinal {
		return nil
	}
	r.SelectionBasis = "legacy_fallback"
	return w.selectHead(ctx, source, source.CapturedAt, r)
}

func (w *catalogDocumentWork) head(ctx context.Context, record *scrape.CatalogSnapshotRecord, r *models.CatalogDocumentRecord) error {
	r.SelectionBasis = "explicit"
	path, pathOK := record.Values["relpath"].(string)
	stamp, stampOK := record.Values["observed_at"].(string)
	if !pathOK || !stampOK || !archive.ValidDocumentPath(path) || !archive.ValidDocumentSourceTime(stamp) {
		return documentReview(r, "invalid_document_head")
	}
	table := "sidecar_sources"
	if _, flat := w.manifest.Tables["sidecars"]; flat {
		table = "sidecars"
	}
	parent, err := w.parent(ctx, table, path, record.Values["content_sha256"])
	if err != nil {
		return err
	}
	if parent == nil || parent.SourceUUID == nil {
		return documentReview(r, "document_source_requires_review")
	}
	r.DocumentUUID, r.SourceUUID, r.PostUUID = parent.DocumentUUID, parent.SourceUUID, parent.PostUUID
	source, err := (&SourceDocumentStore{}).Source(ctx, *r.SourceUUID)
	if err != nil {
		return err
	}
	if source == nil {
		return models.ErrCatalogSnapshotInvalid
	}
	return w.selectHead(ctx, source, stamp, r)
}

func (w *catalogDocumentWork) selectHead(ctx context.Context, source *models.SourceDocumentSource, stamp string, r *models.CatalogDocumentRecord) error {
	details, err := w.details(r.SelectionBasis, nil)
	if err != nil {
		return err
	}
	id, err := w.id("head", w.snapshot.CatalogID, source.UUID, stamp, string(details))
	if err != nil {
		return err
	}
	store := &SourceDocumentStore{}
	claim, err := store.RecordHeadClaim(ctx, models.SourceDocumentHeadClaim{UUID: id, SourceUUID: source.UUID, CollectionUUID: source.CollectionUUID,
		RelativePath: source.RelativePath, ObservedAt: stamp, Origin: "migration", Details: details})
	if errors.Is(err, models.ErrSourcePostForgotten) {
		return documentReview(r, "post_forgotten")
	}
	if err != nil {
		return err
	}
	r.ClaimUUID = &claim.UUID
	head, err := store.Head(ctx, source.CollectionUUID, source.RelativePath)
	if err != nil {
		return err
	}
	if head == nil {
		head, err = store.DecideHead(ctx, models.SourceDocumentHeadInput{CollectionUUID: source.CollectionUUID, RelativePath: source.RelativePath, State: "linked",
			SourceUUID: source.UUID, ClaimUUID: claim.UUID, Origin: "migration", Reason: "Retained catalog document selection: " + r.SelectionBasis})
		if errors.Is(err, models.ErrSourcePostForgotten) {
			return documentReview(r, "post_forgotten")
		}
		if err != nil {
			return err
		}
	}
	r.HeadUUID = &head.UUID
	if head.State != "linked" || head.SourceUUID == nil || *head.SourceUUID != source.UUID {
		return documentReview(r, "native_document_selection_preserved")
	}
	return nil
}

func (s *CatalogDocumentImportStore) Advance(ctx context.Context, id, expected string, after int64, now time.Time) (*models.CatalogDocumentImport, error) {
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
	if (prior == nil && after != 0) || (prior != nil && (prior.ManifestSHA256 != expected || prior.ProcessedRecords != after || prior.Policy != catalogDocumentPolicy)) {
		return nil, models.ErrCatalogSnapshotConflict
	}
	if prior != nil && prior.State != "running" {
		return prior, nil
	}
	work, err := loadCompletedCatalogRelations(ctx, id, expected)
	if err != nil {
		return nil, err
	}
	complete := snapshotAtomicWrite(ctx)
	stamp := now.UTC().Format(time.RFC3339Nano)
	if prior == nil {
		// Registry-created revision 1 describes the historical catalog. Later
		// root bindings, renames or retirement cannot rewrite that provenance.
		var historical bool
		if err := dbWrapper.Get(ctx, &historical, "SELECT EXISTS(SELECT 1 FROM source_collection_revisions WHERE collection_uuid=? AND revision=1 AND origin='migration')", work.snapshot.CollectionUUID); err != nil {
			return nil, err
		}
		if !historical {
			return nil, models.ErrCatalogSnapshotConflict
		}
		var total int64
		for _, phase := range catalogDocumentPhases[:4] {
			total += work.manifest.Tables[phase].Rows
		}
		_, err = dbWrapper.Exec(ctx, `INSERT INTO catalog_document_imports(snapshot_uuid,manifest_sha256,collection_uuid,collection_revision,policy,state,phase,source_records,created_at,updated_at)
 VALUES(?,?,?,1,?,'running','sidecar_documents',?,?,?)`, id, expected, work.snapshot.CollectionUUID, catalogDocumentPolicy, total, stamp, stamp)
		if err != nil {
			return nil, err
		}
		prior, err = s.Find(ctx, id)
		if err != nil {
			return nil, err
		}
	}
	w := &catalogDocumentWork{catalogRelationsWork: *work, progress: prior}
	for count := 0; count < 50 && w.bytes < 16<<20 && prior.State == "running"; {
		var row catalogEvidenceRow
		err := dbWrapper.Get(ctx, &row, "SELECT ordinal,data,data_sha256 FROM catalog_snapshot_records WHERE snapshot_uuid=? AND source_table=? AND ordinal>? ORDER BY ordinal LIMIT 1", id, prior.Phase, prior.LastOrdinal)
		if errors.Is(err, sql.ErrNoRows) {
			for i, phase := range catalogDocumentPhases[:4] {
				if phase == prior.Phase {
					prior.Phase, prior.LastOrdinal = catalogDocumentPhases[i+1], 0
					break
				}
			}
			if prior.Phase == "complete" {
				prior.State = "mapped"
				if prior.ReviewRecords != 0 {
					prior.State = "review"
				}
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		record, err := w.decode(row)
		if err != nil {
			return nil, err
		}
		result := &models.CatalogDocumentRecord{Ordinal: row.Ordinal, Outcome: "mapped"}
		switch record.Table {
		case "sidecar_documents":
			err = w.document(ctx, record, result)
		case "sidecar_sources":
			err = w.source(ctx, record, result)
		case "sidecars":
			err = w.document(ctx, record, result)
			if err == nil && result.Outcome == "mapped" {
				err = w.source(ctx, record, result)
			}
		case "sidecar_heads":
			err = w.head(ctx, record, result)
		default:
			err = models.ErrCatalogSnapshotInvalid
		}
		if err != nil {
			return nil, err
		}
		_, err = dbWrapper.Exec(ctx, `INSERT INTO catalog_document_records(snapshot_uuid,ordinal,document_uuid,source_uuid,post_uuid,claim_uuid,head_uuid,selection_basis,outcome,reason) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			id, row.Ordinal, result.DocumentUUID, result.SourceUUID, result.PostUUID, result.ClaimUUID, result.HeadUUID, result.SelectionBasis, result.Outcome, result.Reason)
		if err != nil {
			return nil, err
		}
		prior.LastOrdinal, prior.ProcessedRecords = row.Ordinal, prior.ProcessedRecords+1
		if result.Outcome == "mapped" {
			prior.MappedRecords++
		} else {
			prior.ReviewRecords++
		}
		count++
	}
	if prior.State != "running" && prior.ProcessedRecords != prior.TotalRecords {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	_, err = dbWrapper.Exec(ctx, `UPDATE catalog_document_imports SET state=?,phase=?,last_ordinal=?,processed_records=?,mapped_records=?,review_records=?,updated_at=? WHERE snapshot_uuid=?`,
		prior.State, prior.Phase, prior.LastOrdinal, prior.ProcessedRecords, prior.MappedRecords, prior.ReviewRecords, stamp, id)
	if err != nil {
		return nil, err
	}
	ret, err := s.Find(ctx, id)
	*complete = err == nil
	return ret, err
}

func (s *CatalogDocumentImportStore) Records(ctx context.Context, id string, after int64, limit int) ([]models.CatalogDocumentRecord, error) {
	if !validSourceRunUUID(id) || after < 0 || limit < 1 || limit > 100 {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	columns := strings.Replace(catalogDocumentColumns, "e.source_key", `CASE WHEN length(CAST(e.source_key AS BLOB))<=8192 THEN e.source_key ELSE '' END AS source_key,length(CAST(e.source_key AS BLOB))>8192 AS key_omitted`, 1)
	ret := []models.CatalogDocumentRecord{}
	err := dbWrapper.Select(ctx, &ret, "SELECT "+columns+catalogDocumentJoins+" WHERE r.snapshot_uuid=? AND r.ordinal>? ORDER BY r.ordinal LIMIT ?", id, after, limit)
	return ret, err
}

func (s *CatalogDocumentImportStore) Record(ctx context.Context, id string, ordinal int64) (*models.CatalogDocumentRecordDetails, error) {
	if !validSourceRunUUID(id) || ordinal < 1 {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	var row struct {
		models.CatalogDocumentRecord
		Data string `db:"data"`
	}
	err := dbWrapper.Get(ctx, &row, "SELECT "+catalogDocumentColumns+",e.data"+catalogDocumentJoins+" WHERE r.snapshot_uuid=? AND r.ordinal=?", id, ordinal)
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
	return &models.CatalogDocumentRecordDetails{CatalogDocumentRecord: row.CatalogDocumentRecord, SourceValues: values}, err
}
