package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

type SourceCleanupIntentStore struct{}

func (s *SourceCleanupIntentStore) Find(ctx context.Context, id string) (*models.SourceCleanupIntent, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	ret := &models.SourceCleanupIntent{}
	err := dbWrapper.Get(ctx, ret, "SELECT * FROM source_cleanup_intents WHERE uuid=?", id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return ret, err
}

func (s *SourceCleanupIntentStore) CollectionIntents(ctx context.Context, collection, after string, limit int) ([]models.SourceCleanupIntent, error) {
	if !validSourceRunUUID(collection) {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	after, limit, err := sourceDefinitionPage(after, limit)
	if err != nil {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	ret := []models.SourceCleanupIntent{}
	err = dbWrapper.Select(ctx, &ret, "SELECT * FROM source_cleanup_intents WHERE collection_uuid=? AND uuid>? ORDER BY uuid LIMIT ?", collection, after, limit)
	return ret, err
}

func catalogCleanupIntentID(snapshot string, ordinal int64) string {
	return scrape.RegistryImportUUID(snapshot, "catalog-cleanup-intent:v1", strconv.FormatInt(ordinal, 10))
}

func catalogCleanupRecord(ctx context.Context, w *catalogRelationsWork, row catalogEvidenceRow, record *scrape.CatalogSnapshotRecord, stamp string) (*models.CatalogCleanupRecord, error) {
	ret := &models.CatalogCleanupRecord{Ordinal: row.Ordinal, Outcome: "review"}
	boundary, err := time.Parse(time.RFC3339Nano, w.snapshot.CapturedAt)
	if err != nil {
		return nil, err
	}
	prepared, reason := scrape.PrepareCatalogCleanupIntent(record.Values, boundary)
	if reason != "" {
		ret.Reason = reason
		return ret, nil
	}
	id := catalogCleanupIntentID(w.snapshot.UUID, row.Ordinal)
	_, err = dbWrapper.Exec(ctx, `INSERT INTO source_cleanup_intents(uuid,collection_uuid,collection_revision,kind,state,reference_namespace,reference_value,requested_at,origin,recorded_at)
 VALUES(?,?,1,'background_targets','held',?,?,?,'migration',?)`, id, w.snapshot.CollectionUUID,
		"legacy:catalog:"+w.snapshot.SourceUUID+":"+w.snapshot.CatalogID, prepared.PostKey, prepared.RequestedAt, stamp)
	if err != nil {
		return nil, err
	}
	ret.IntentUUID, ret.Outcome = &id, "held"
	return ret, nil
}
