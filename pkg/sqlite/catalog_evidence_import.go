package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

type CatalogEvidenceImportStore struct{}

func (s *CatalogEvidenceImportStore) Find(ctx context.Context, id string) (*models.CatalogEvidenceImport, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	ret := &models.CatalogEvidenceImport{}
	if err := dbWrapper.Get(ctx, ret, "SELECT * FROM catalog_evidence_imports WHERE snapshot_uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return ret, nil
}

type catalogEvidenceRow struct {
	Ordinal int64  `db:"ordinal"`
	Data    string `db:"data"`
	SHA256  string `db:"data_sha256"`
}

type catalogEvidenceWork struct {
	snapshot *models.CatalogSnapshot
	manifest *scrape.CatalogSnapshotManifest
	bytes    int
}

func (w *catalogEvidenceWork) decode(row catalogEvidenceRow) (*scrape.CatalogSnapshotRecord, error) {
	if scrape.CatalogSnapshotSHA([]byte(row.Data)) != row.SHA256 {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	w.bytes += len(row.Data)
	return w.manifest.Record([]byte(row.Data))
}

func (w *catalogEvidenceWork) load(ctx context.Context, table, key string) (catalogEvidenceRow, *scrape.CatalogSnapshotRecord, error) {
	encoded, err := archive.EncodeSourceJSON([]any{key})
	if err != nil {
		return catalogEvidenceRow{}, nil, err
	}
	var row catalogEvidenceRow
	err = dbWrapper.Get(ctx, &row, `SELECT ordinal,data,data_sha256 FROM catalog_snapshot_records WHERE snapshot_uuid=? AND source_table=? AND source_key=?`, w.snapshot.UUID, table, string(encoded))
	if errors.Is(err, sql.ErrNoRows) {
		return row, nil, models.ErrCatalogSnapshotInvalid
	}
	if err != nil {
		return row, nil, err
	}
	record, err := w.decode(row)
	return row, record, err
}

func (w *catalogEvidenceWork) profile(ctx context.Context, key string) (map[string]any, error) {
	_, record, err := w.load(ctx, "account_snapshots", key)
	if err != nil {
		return nil, err
	}
	return scrape.CatalogProfile(record.Values)
}

type catalogEvidencePost struct {
	PostUUID *string `db:"post_uuid"`
	Basis    string  `db:"basis"`
	Reason   string  `db:"reason"`
}

func (w *catalogEvidenceWork) post(ctx context.Context, key string, captured *models.SourcePostIdentifier) (*catalogEvidencePost, error) {
	row, record, err := w.load(ctx, "posts", key)
	if err != nil {
		return nil, err
	}
	var prior catalogEvidencePost
	err = dbWrapper.Get(ctx, &prior, "SELECT post_uuid,basis,reason FROM catalog_evidence_posts WHERE snapshot_uuid=? AND source_key=?", w.snapshot.UUID, record.KeyJSON)
	if err == nil {
		return w.checkPost(ctx, &prior, captured)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var urlRows []catalogEvidenceRow
	if err := dbWrapper.Select(ctx, &urlRows, `SELECT ordinal,CASE WHEN length(CAST(data AS BLOB))<=16384 THEN data ELSE '' END AS data,data_sha256 FROM catalog_snapshot_records
 WHERE snapshot_uuid=? AND source_table='post_urls' AND json_extract(data,'$.values.post_key')=? ORDER BY ordinal LIMIT 101`, w.snapshot.UUID, key); err != nil {
		return nil, err
	}
	urls := make([]string, 0, len(urlRows))
	urlReason := ""
	for _, stored := range urlRows {
		if stored.Data == "" {
			urlReason = "source_post_url_record_exceeds_limit"
			continue
		}
		url, err := w.decode(stored)
		if err != nil {
			return nil, err
		}
		text, ok := url.Values["url"].(string)
		if !ok {
			return nil, models.ErrCatalogSnapshotInvalid
		}
		urls = append(urls, text)
	}
	identity, err := scrape.CatalogPostReference(w.snapshot.SourceUUID, w.snapshot.CatalogID, record.Values, urls)
	if err != nil {
		return nil, err
	}
	result := &catalogEvidencePost{Basis: identity.Basis}
	switch {
	case urlReason != "":
		result.Reason = urlReason
	case len(urlRows) > 100:
		result.Reason = "source_post_url_candidates_exceed_limit"
	case identity.Basis == "conflicting_mirror_identity":
		result.Reason = identity.Basis
	}
	if captured != nil {
		if identity.Identifier == identity.Legacy {
			identity.Identifier, result.Basis = *captured, "captured_source_id"
		} else if identity.Identifier != *captured {
			result.Reason = "captured_and_catalog_post_ids_disagree"
		}
	}
	store := &SourceEvidenceStore{}
	if result.Reason == "" {
		legacy, err := store.FindPostByIdentifier(ctx, identity.Legacy)
		if err != nil {
			return nil, err
		}
		post, err := store.FindPostByIdentifier(ctx, identity.Identifier)
		if err != nil {
			return nil, err
		}
		if legacy != nil && post != nil && legacy.UUID != post.UUID {
			result.Reason = "native_post_identifiers_disagree"
		} else {
			if post == nil {
				post = legacy
			}
			if post == nil {
				encoded, err := archive.EncodeSourceJSON([]string{identity.Identifier.Namespace, identity.Identifier.Value})
				if err != nil {
					return nil, err
				}
				id := scrape.RegistryImportUUID(w.snapshot.SourceUUID, "source-post", string(encoded))
				if occupied, err := store.FindPost(ctx, id); err != nil {
					return nil, err
				} else if occupied != nil {
					result.Reason = "reserved_post_uuid_in_use"
				} else {
					post, err = store.EnsurePost(ctx, identity.Identifier, id)
					if err != nil {
						return nil, err
					}
				}
			}
			if post != nil {
				result.PostUUID = &post.UUID
				if post.State != "active" {
					result.Reason = "source_post_forgotten"
				} else if identity.Identifier != identity.Legacy {
					if _, err := w.checkPost(ctx, result, &identity.Identifier); err != nil {
						return nil, err
					}
				}
				if result.Reason == "" {
					post, err = store.FindPost(ctx, post.UUID)
					if err != nil {
						return nil, err
					}
					for _, identifier := range []models.SourcePostIdentifier{identity.Identifier, identity.Legacy} {
						if err := store.AddPostIdentifier(ctx, post.UUID, identifier, post.Revision); err != nil {
							return nil, err
						}
						post, err = store.FindPost(ctx, post.UUID)
						if err != nil {
							return nil, err
						}
					}
				}
			}
		}
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO catalog_evidence_posts(snapshot_uuid,source_key,source_ordinal,data_sha256,post_uuid,basis,reason) VALUES(?,?,?,?,?,?,?)`,
		w.snapshot.UUID, record.KeyJSON, row.Ordinal, row.SHA256, result.PostUUID, result.Basis, result.Reason); err != nil {
		return nil, err
	}
	return result, nil
}

func (w *catalogEvidenceWork) checkPost(ctx context.Context, mapped *catalogEvidencePost, captured *models.SourcePostIdentifier) (*catalogEvidencePost, error) {
	if mapped.Reason != "" || mapped.PostUUID == nil {
		return mapped, nil
	}
	store := &SourceEvidenceStore{}
	post, err := store.FindPost(ctx, *mapped.PostUUID)
	if err != nil {
		return nil, err
	}
	if post == nil || post.State != "active" {
		mapped.Reason = "source_post_forgotten"
		return mapped, nil
	}
	if captured == nil {
		return mapped, nil
	}
	known, err := store.FindPostByIdentifier(ctx, *captured)
	if err != nil {
		return nil, err
	}
	if known != nil {
		if known.UUID != post.UUID {
			mapped.Reason = "captured_post_requires_identity_review"
		}
		return mapped, nil
	}
	// One local placeholder may gain its first captured service ID. A second
	// differing service ID must not silently merge crossposts into that post.
	var hasServiceID bool
	if err := dbWrapper.Get(ctx, &hasServiceID, "SELECT EXISTS(SELECT 1 FROM source_post_identifiers WHERE post_uuid=? AND namespace NOT LIKE 'legacy:catalog:%')", post.UUID); err != nil {
		return nil, err
	}
	if hasServiceID {
		mapped.Reason = "captured_post_requires_identity_review"
		return mapped, nil
	}
	if err := store.AddPostIdentifier(ctx, post.UUID, *captured, post.Revision); err != nil {
		return nil, err
	}
	return mapped, nil
}

func (w *catalogEvidenceWork) record(ctx context.Context, row catalogEvidenceRow, record *scrape.CatalogSnapshotRecord) (*models.CatalogEvidenceRecord, error) {
	result := &models.CatalogEvidenceRecord{Ordinal: row.Ordinal, Table: record.Table, Key: record.KeyJSON, SHA256: row.SHA256, Outcome: "mapped"}
	store := &SourceEvidenceStore{}
	if record.Table == "account_snapshots" {
		profile, err := scrape.CatalogProfile(record.Values)
		if err != nil {
			return nil, err
		}
		body, err := archive.EncodeSourceJSON(profile)
		if err != nil {
			return nil, err
		}
		retained, err := store.RetainProfile(ctx, "native:"+record.Values["platform"].(string), body)
		if err != nil {
			return nil, err
		}
		result.ProfileHash = &retained.Hash
		return result, nil
	}
	if record.Table == "posts" {
		key, ok := record.Values["post_key"].(string)
		if !ok || key == "" {
			return nil, models.ErrCatalogSnapshotInvalid
		}
		post, err := w.post(ctx, key, nil)
		if err != nil {
			return nil, err
		}
		result.PostUUID, result.Reason = post.PostUUID, post.Reason
		if result.Reason != "" {
			result.Outcome = "review"
		}
		return result, nil
	}
	observation, detail := record.Values, map[string]any(nil)
	if record.Table == "observation_details" {
		key, ok := record.Values["observation_id"].(string)
		if !ok {
			return nil, models.ErrCatalogSnapshotInvalid
		}
		_, parent, err := w.load(ctx, "observations", key)
		if err != nil {
			return nil, err
		}
		observation, detail = parent.Values, record.Values
	} else {
		var children bool
		if err := dbWrapper.Get(ctx, &children, `SELECT EXISTS(SELECT 1 FROM catalog_snapshot_records WHERE snapshot_uuid=? AND source_table='observation_details' AND json_extract(data,'$.values.observation_id')=?)`, w.snapshot.UUID, observation["observation_id"]); err != nil {
			return nil, err
		}
		if children {
			result.Outcome, result.Reason = "shared", "shared_observation_has_original_detail_captures"
			return result, nil
		}
	}
	key, ok := observation["post_key"].(string)
	if !ok {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	_, postRow, err := w.load(ctx, "posts", key)
	if err != nil {
		return nil, err
	}
	platform, ok := postRow.Values["platform"].(string)
	if !ok {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	capture, err := scrape.ReconstructCatalogCapture(observation, detail, platform, func(key string) (map[string]any, error) { return w.profile(ctx, key) })
	if err != nil {
		return nil, err
	}
	header := scrape.CatalogSnapshotSHA(capture.Header)
	result.HeaderSHA256 = &header
	identity, err := archive.ExtractCapturedPost(capture.Payload)
	if err != nil {
		result.Outcome, result.Reason = "review", "conflicting_captured_post_ids"
		return result, nil
	}
	post, err := w.post(ctx, key, identity)
	if err != nil {
		return nil, err
	}
	result.PostUUID, result.Reason = post.PostUUID, post.Reason
	if result.Reason != "" || result.PostUUID == nil {
		result.Outcome = "review"
		return result, nil
	}
	input, err := capture.NativeInput(*result.PostUUID, uuid.NewSHA1(uuid.MustParse(w.snapshot.SourceUUID), []byte(capture.CaptureID)).String())
	if err != nil {
		return nil, err
	}
	_, revision, err := canonicalCaptureInput(&input)
	if err != nil {
		return nil, err
	}
	signature, err := captureSignature(input, revision, sourceDigest(input.Payload.Patch), input.Payload.Refs)
	if err != nil {
		return nil, err
	}
	// Equal old IDs alone do not establish equal events. Bind the complete
	// canonical capture signature; exact copies across catalogs can share one.
	input.UUID = scrape.RegistryImportUUID(w.snapshot.SourceUUID, "catalog-capture", capture.CaptureID+"/"+signature)
	retained, err := store.RecordCapture(ctx, input)
	if err != nil {
		return nil, err
	}
	if err := (&SourceCollectionStore{}).RecordCapture(ctx, models.CollectionCapture{CaptureUUID: retained.UUID, CollectionUUID: w.snapshot.CollectionUUID, CollectionRevision: 1}); err != nil {
		return nil, err
	}
	result.CaptureUUID = &retained.UUID
	return result, nil
}

func (s *CatalogEvidenceImportStore) Advance(ctx context.Context, id, expected string, after int64, now time.Time) (*models.CatalogEvidenceImport, error) {
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
	var body []byte
	if err := dbWrapper.Get(ctx, &body, "SELECT manifest FROM catalog_snapshots WHERE uuid=?", id); err != nil {
		return nil, err
	}
	manifest, err := scrape.PrepareCatalogSnapshot(body, expected)
	if err != nil {
		return nil, err
	}
	work := &catalogEvidenceWork{snapshot: snapshot, manifest: manifest}
	complete := snapshotAtomicWrite(ctx)
	stamp := now.UTC().Format(time.RFC3339Nano)
	if prior == nil {
		var originalCollection bool
		if err := dbWrapper.Get(ctx, &originalCollection, `SELECT EXISTS(SELECT 1 FROM source_collection_revisions WHERE collection_uuid=? AND revision=1 AND kind='legacy_catalog' AND origin='migration')`, snapshot.CollectionUUID); err != nil {
			return nil, err
		}
		if !originalCollection {
			return nil, models.ErrCatalogSnapshotConflict
		}
		var total int64
		for _, table := range scrape.CatalogEvidenceTables() {
			total += manifest.Tables[table].Rows
		}
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO catalog_evidence_imports(snapshot_uuid,manifest_sha256,state,source_records,created_at,updated_at) VALUES(?,?,'running',?,?,?)`, id, expected, total, stamp, stamp); err != nil {
			return nil, err
		}
		prior, err = s.Find(ctx, id)
		if err != nil {
			return nil, err
		}
	}
	for count := 0; count < 50 && work.bytes < 16<<20; count++ {
		var row catalogEvidenceRow
		err := dbWrapper.Get(ctx, &row, `SELECT ordinal,data,data_sha256 FROM catalog_snapshot_records WHERE snapshot_uuid=? AND ordinal>?
 AND source_table IN ('account_snapshots','posts','observations','observation_details') ORDER BY ordinal LIMIT 1`, id, prior.LastOrdinal)
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
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO catalog_evidence_records(snapshot_uuid,ordinal,source_table,source_key,data_sha256,outcome,reason,post_uuid,capture_uuid,profile_hash,header_sha256) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			id, outcome.Ordinal, outcome.Table, outcome.Key, outcome.SHA256, outcome.Outcome, outcome.Reason, outcome.PostUUID, outcome.CaptureUUID, outcome.ProfileHash, outcome.HeaderSHA256); err != nil {
			return nil, err
		}
		prior.LastOrdinal = row.Ordinal
		prior.ProcessedRecords++
		if outcome.Outcome == "review" {
			prior.ReviewRecords++
		}
		if outcome.CaptureUUID != nil {
			prior.CaptureMappings++
		}
		if outcome.ProfileHash != nil {
			prior.ProfileMappings++
		}
	}
	if _, err := dbWrapper.Exec(ctx, `UPDATE catalog_evidence_imports SET state=?,last_ordinal=?,processed_records=?,review_records=?,capture_mappings=?,profile_mappings=?,updated_at=? WHERE snapshot_uuid=?`,
		prior.State, prior.LastOrdinal, prior.ProcessedRecords, prior.ReviewRecords, prior.CaptureMappings, prior.ProfileMappings, stamp, id); err != nil {
		return nil, err
	}
	result, err := s.Find(ctx, id)
	*complete = err == nil
	return result, err
}

func (s *CatalogEvidenceImportStore) Records(ctx context.Context, id string, after int64, limit int) ([]models.CatalogEvidenceRecord, error) {
	if !validSourceRunUUID(id) || after < 0 || limit < 1 || limit > 100 {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	result := []models.CatalogEvidenceRecord{}
	err := dbWrapper.Select(ctx, &result, `SELECT ordinal,source_table,source_key,data_sha256,outcome,reason,post_uuid,capture_uuid,profile_hash,header_sha256 FROM catalog_evidence_records WHERE snapshot_uuid=? AND ordinal>? ORDER BY ordinal LIMIT ?`, id, after, limit)
	return result, err
}
