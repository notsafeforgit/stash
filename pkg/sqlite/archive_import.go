package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/stashapp/stash/pkg/models"
)

type ArchiveImportStore struct{}

const importSnapshotColumns = `s.uuid,s.source_uuid,s.registry_import_uuid,s.captured_at,s.state,s.chunk_count,s.next_chunk,
s.record_count,s.received_records,s.byte_count,s.received_bytes,s.created_at,s.updated_at`

func importSnapshotSelect(kind string) (string, error) {
	switch kind {
	case "catalog":
		// UUID paging starts at the snapshot key. Only the current collection
		// label is joined; manifests, input rows and source bodies stay unloaded.
		return `SELECT 'catalog' AS kind,` + importSnapshotColumns + `,s.collection_uuid,c.label AS collection_label
FROM catalog_snapshots s CROSS JOIN source_collections b ON b.uuid=s.collection_uuid
CROSS JOIN source_collection_revisions c ON c.collection_uuid=b.uuid AND c.revision=b.revision`, nil
	case "automation":
		return `SELECT 'automation' AS kind,` + importSnapshotColumns + `,NULL AS collection_uuid,NULL AS collection_label
FROM automation_snapshots s`, nil
	default:
		return "", models.ErrArchiveImportInvalid
	}
}

func (s *ArchiveImportStore) Snapshots(ctx context.Context, filter models.ArchiveImportFilter) ([]models.ArchiveImportSnapshot, error) {
	query, err := importSnapshotSelect(filter.Kind)
	if err != nil || filter.Limit < 1 || filter.Limit > 100 || (filter.After != "" && !validSourceRunUUID(filter.After)) {
		return nil, models.ErrArchiveImportInvalid
	}
	result := []models.ArchiveImportSnapshot{}
	err = dbWrapper.Select(ctx, &result, query+" WHERE s.uuid>? ORDER BY s.uuid LIMIT ?", filter.After, filter.Limit)
	return result, err
}

type importFamily struct{ name, table string }

func importFamilies(kind string) []importFamily {
	if kind == "catalog" {
		return []importFamily{
			{"evidence", "catalog_evidence_imports"},
			{"relations", "catalog_relations_imports"},
			{"publishers", "catalog_publisher_imports"},
			{"attachments", "catalog_attachment_imports"},
			{"media", "catalog_media_imports"},
			{"memberships", "catalog_membership_imports"},
			{"documents", "catalog_document_imports"},
			{"translations", "catalog_translation_imports"},
			{"enrichment", "catalog_enrichment_imports"},
			{"file_history", "catalog_file_history_imports"},
			{"cleanup", "catalog_cleanup_imports"},
		}
	}
	return []importFamily{
		{"translations", "automation_translation_imports"},
		{"enrichment", "automation_enrichment_imports"},
		{"discovery", "automation_discovery_imports"},
		{"checkpoints", "automation_checkpoint_imports"},
	}
}

func (s *ArchiveImportStore) Snapshot(ctx context.Context, kind, id string) (*models.ArchiveImportDetails, error) {
	query, err := importSnapshotSelect(kind)
	if err != nil || !validSourceRunUUID(id) {
		return nil, models.ErrArchiveImportInvalid
	}
	result := &models.ArchiveImportDetails{Families: []models.ArchiveImportFamily{}}
	if err := dbWrapper.Get(ctx, &result.Snapshot, query+" WHERE s.uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	// A fixed number of primary-key lookups. Family progress is immutable once
	// finished; its review count is deliberately not called a pending count.
	for _, family := range importFamilies(kind) {
		progress := &models.ArchiveImportProgress{}
		err := dbWrapper.Get(ctx, progress, `SELECT state,source_records,processed_records,review_records,updated_at FROM `+family.table+` WHERE snapshot_uuid=?`, id)
		if errors.Is(err, sql.ErrNoRows) {
			progress = nil
		} else if err != nil {
			return nil, err
		}
		result.Families = append(result.Families, models.ArchiveImportFamily{Name: family.name, Progress: progress})
	}
	return result, nil
}
