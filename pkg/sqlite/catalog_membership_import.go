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

type CatalogMembershipImportStore struct{}

const catalogMembershipPolicy = "catalog-membership-v1"
const catalogMembershipColumns = `r.ordinal,e.source_key,e.data_sha256,r.post_uuid,r.collection_uuid,r.membership_uuid,r.outcome,r.reason`
const catalogMembershipJoins = ` FROM catalog_membership_records r JOIN catalog_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal`

func (s *CatalogMembershipImportStore) Find(ctx context.Context, id string) (*models.CatalogMembershipImport, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	ret := &models.CatalogMembershipImport{}
	if err := dbWrapper.Get(ctx, ret, "SELECT * FROM catalog_membership_imports WHERE snapshot_uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return ret, nil
}

type catalogMembershipGroup struct {
	SourceUUID         string `db:"source_uuid"`
	SourceKey          string `db:"source_key"`
	SourceKind         string `db:"source_kind"`
	SourceLabel        string `db:"source_label"`
	CollectionUUID     string `db:"collection_uuid"`
	CollectionRevision int    `db:"collection_revision"`
	FirstSnapshotUUID  string `db:"first_snapshot_uuid"`
}

func catalogMembershipDefinition(key, kind, label string) (*models.SourceCollectionDefinition, string) {
	if !validAccountText(key, 4096, false) || !sourceDefinitionText(label, "disabled", "") {
		return nil, "invalid_collection_definition"
	}
	definition := &models.SourceCollectionDefinition{Label: label, State: "disabled"}
	switch {
	case (kind == "creator" || kind == "collection") && strings.HasPrefix(key, "directory:") && len(key) > len("directory:"):
		// Legacy 'creator' was inferred from commas in a folder name. It is
		// directory membership evidence, never performer/account ownership.
		definition.Kind = "directory"
	case kind == "subreddit" && strings.HasPrefix(key, "reddit:subreddit:") && len(key) > len("reddit:subreddit:"):
		definition.Kind, definition.Namespace = "subreddit", "native:reddit"
	default:
		return nil, "unsupported_collection_identity"
	}
	return definition, ""
}

func catalogMembershipCollection(ctx context.Context, snapshot *models.CatalogSnapshot, key, kind, label string) (*catalogMembershipGroup, string, error) {
	definition, reason := catalogMembershipDefinition(key, kind, label)
	if reason != "" {
		return nil, reason, nil
	}
	var prior catalogMembershipGroup
	err := dbWrapper.Get(ctx, &prior, "SELECT * FROM catalog_membership_groups WHERE source_uuid=? AND source_key=?", snapshot.SourceUUID, key)
	if err == nil {
		if prior.SourceKind != kind || prior.SourceLabel != label {
			return &prior, "collection_definition_conflicts", nil
		}
		return &prior, "", nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, "", err
	}
	id := scrape.RegistryImportUUID(snapshot.SourceUUID, "catalog-membership-group", key)
	store := &SourceCollectionStore{}
	current, err := store.Find(ctx, id)
	if err != nil {
		return nil, "", err
	}
	if current != nil {
		return nil, "reserved_collection_uuid_in_use", nil
	}
	group, err := store.Put(ctx, models.SourceCollectionInput{UUID: id, Origin: "migration", Reason: "Historical catalog collection membership; no source activation or performer attribution", SourceCollectionDefinition: *definition})
	if err != nil {
		return nil, "", err
	}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO catalog_membership_groups(source_uuid,source_key,source_kind,source_label,collection_uuid,collection_revision,first_snapshot_uuid)
 VALUES(?,?,?,?,?,?,?)`, snapshot.SourceUUID, key, kind, label, group.UUID, group.Revision, snapshot.UUID)
	if err != nil {
		return nil, "", err
	}
	return &catalogMembershipGroup{SourceUUID: snapshot.SourceUUID, SourceKey: key, SourceKind: kind, SourceLabel: label, CollectionUUID: group.UUID, CollectionRevision: group.Revision, FirstSnapshotUUID: snapshot.UUID}, "", nil
}

func catalogMembershipRecord(ctx context.Context, w *catalogRelationsWork, row catalogEvidenceRow, record *scrape.CatalogSnapshotRecord) (*models.CatalogMembershipRecord, error) {
	result := &models.CatalogMembershipRecord{Ordinal: row.Ordinal, Outcome: "review"}
	postKey, postOK := record.Values["post_key"].(string)
	key, keyOK := record.Values["collection_key"].(string)
	kind, kindOK := record.Values["kind"].(string)
	label, labelOK := record.Values["label"].(string)
	if !postOK || !keyOK || !kindOK || !labelOK || record.Table != "memberships" {
		result.Reason = "invalid_collection_membership"
		return result, nil
	}
	post, reason, err := w.nativePost(ctx, postKey)
	if err != nil {
		return nil, err
	}
	if post != nil {
		result.PostUUID = &post.UUID
	}
	if reason != "" {
		result.Reason = reason
		return result, nil
	}
	group, reason, err := catalogMembershipCollection(ctx, w.snapshot, key, kind, label)
	if err != nil {
		return nil, err
	}
	if group != nil {
		result.CollectionUUID = &group.CollectionUUID
	}
	if reason != "" {
		result.Reason = reason
		return result, nil
	}
	stamp, err := time.Parse(time.RFC3339Nano, w.snapshot.CapturedAt)
	if err != nil {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	details, err := archive.EncodeSourceJSON(map[string]any{"snapshot_uuid": w.snapshot.UUID, "source_ordinal": row.Ordinal, "source_sha256": row.SHA256})
	if err != nil {
		return nil, err
	}
	membership, err := (&SourceCollectionStore{}).RecordPostMembership(ctx, models.CollectionPostMembership{
		SourcePostEvidence: models.SourcePostEvidence{UUID: scrape.RegistryImportUUID(w.snapshot.UUID, "catalog-membership", strconv.FormatInt(row.Ordinal, 10)), PostUUID: post.UUID, Origin: "migration", Basis: "catalog-membership", ObservedAt: stamp, Details: details},
		CollectionUUID:     group.CollectionUUID, CollectionRevision: group.CollectionRevision,
	})
	if err != nil {
		return nil, err
	}
	result.MembershipUUID, result.Outcome = &membership.UUID, "mapped"
	return result, nil
}

func (s *CatalogMembershipImportStore) Advance(ctx context.Context, id, expected string, after int64, now time.Time) (*models.CatalogMembershipImport, error) {
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
	if (prior == nil && after != 0) || (prior != nil && (prior.LastOrdinal != after || prior.ManifestSHA256 != expected || prior.Policy != catalogMembershipPolicy)) {
		return nil, models.ErrCatalogSnapshotConflict
	}
	if prior != nil && prior.State != "running" {
		return prior, nil
	}
	// The common loader checks the received manifest and completed evidence
	// pass. Historical collection membership does not depend on file intake.
	w, err := loadCompletedCatalogRelations(ctx, id, expected)
	if err != nil {
		return nil, err
	}
	complete := snapshotAtomicWrite(ctx)
	stamp := now.UTC().Format(time.RFC3339Nano)
	if prior == nil {
		_, err := dbWrapper.Exec(ctx, `INSERT INTO catalog_membership_imports(snapshot_uuid,manifest_sha256,policy,state,source_records,created_at,updated_at)
 VALUES(?,?,?,'running',?,?,?)`, id, expected, catalogMembershipPolicy, w.manifest.Tables["memberships"].Rows, stamp, stamp)
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
		err := dbWrapper.Get(ctx, &row, "SELECT ordinal,data,data_sha256 FROM catalog_snapshot_records WHERE snapshot_uuid=? AND source_table='memberships' AND ordinal>? ORDER BY ordinal LIMIT 1", id, prior.LastOrdinal)
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
		result, err := catalogMembershipRecord(ctx, w, row, record)
		if err != nil {
			return nil, err
		}
		_, err = dbWrapper.Exec(ctx, `INSERT INTO catalog_membership_records(snapshot_uuid,ordinal,post_uuid,collection_uuid,membership_uuid,outcome,reason) VALUES(?,?,?,?,?,?,?)`, id, row.Ordinal, result.PostUUID, result.CollectionUUID, result.MembershipUUID, result.Outcome, result.Reason)
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
	_, err = dbWrapper.Exec(ctx, `UPDATE catalog_membership_imports SET state=?,last_ordinal=?,processed_records=?,mapped_records=?,review_records=?,updated_at=? WHERE snapshot_uuid=?`, prior.State, prior.LastOrdinal, prior.ProcessedRecords, prior.MappedRecords, prior.ReviewRecords, stamp, id)
	if err != nil {
		return nil, err
	}
	ret, err := s.Find(ctx, id)
	*complete = err == nil
	return ret, err
}

func (s *CatalogMembershipImportStore) Records(ctx context.Context, id string, after int64, limit int) ([]models.CatalogMembershipRecord, error) {
	if !validSourceRunUUID(id) || after < 0 || limit < 1 || limit > 100 {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	columns := strings.Replace(catalogMembershipColumns, "e.source_key", `CASE WHEN length(CAST(e.source_key AS BLOB))<=8192 THEN e.source_key ELSE '' END AS source_key,length(CAST(e.source_key AS BLOB))>8192 AS key_omitted`, 1)
	ret := []models.CatalogMembershipRecord{}
	err := dbWrapper.Select(ctx, &ret, "SELECT "+columns+catalogMembershipJoins+" WHERE r.snapshot_uuid=? AND r.ordinal>? ORDER BY r.ordinal LIMIT ?", id, after, limit)
	return ret, err
}

func (s *CatalogMembershipImportStore) Record(ctx context.Context, id string, ordinal int64) (*models.CatalogMembershipRecordDetails, error) {
	if !validSourceRunUUID(id) || ordinal < 1 {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	var row struct {
		models.CatalogMembershipRecord
		Values string `db:"source_values"`
	}
	err := dbWrapper.Get(ctx, &row, "SELECT "+catalogMembershipColumns+",json_extract(e.data,'$.values') AS source_values"+catalogMembershipJoins+" WHERE r.snapshot_uuid=? AND r.ordinal=?", id, ordinal)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &models.CatalogMembershipRecordDetails{CatalogMembershipRecord: row.CatalogMembershipRecord, SourceValues: json.RawMessage(row.Values)}, nil
}
