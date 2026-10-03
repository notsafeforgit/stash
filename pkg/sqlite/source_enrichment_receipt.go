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

type SourceEnrichmentReceiptStore struct{}

func (s *SourceEnrichmentReceiptStore) Find(ctx context.Context, id string) (*models.SourceEnrichmentReceipt, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	ret := &models.SourceEnrichmentReceipt{}
	if err := dbWrapper.Get(ctx, ret, "SELECT * FROM source_enrichment_receipts WHERE uuid=?", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return ret, nil
}

func (s *SourceEnrichmentReceiptStore) PostReceipts(ctx context.Context, post, after string, limit int) ([]models.SourceEnrichmentReceipt, error) {
	if !validSourceRunUUID(post) || (after != "" && !validSourceRunUUID(after)) || limit < 1 || limit > 100 {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	ret := []models.SourceEnrichmentReceipt{}
	err := dbWrapper.Select(ctx, &ret, "SELECT * FROM source_enrichment_receipts WHERE post_uuid=? AND uuid>? ORDER BY uuid LIMIT ?", post, after, limit)
	return ret, err
}

// Receipt identity binds the assertion to its original catalog and native
// scope, including the exact completion timestamp. The import ledger retains
// the original source record; native job attempts are not synthesized.
func catalogEnrichmentReceiptID(source, catalog, collection, post string, v *scrape.CatalogEnrichmentReceipt, completed string) (string, error) {
	body, err := archive.EncodeSourceJSON([]any{catalog, collection, 1, post, v.PostKey, v.Version, completed, v.AttachmentLinksEnriched, v.UnresolvedChildren})
	if err != nil {
		return "", err
	}
	return scrape.RegistryImportUUID(source, catalogEnrichmentPolicy, scrape.CatalogSnapshotSHA(body)), nil
}

func catalogEnrichmentRecord(ctx context.Context, w *catalogRelationsWork, progress *models.CatalogEnrichmentImport, row catalogEvidenceRow, record *scrape.CatalogSnapshotRecord, stamp string) (*models.CatalogEnrichmentRecord, error) {
	r := &models.CatalogEnrichmentRecord{Ordinal: row.Ordinal, Outcome: "review"}
	captured, err := time.Parse(time.RFC3339Nano, w.snapshot.CapturedAt)
	if err != nil {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	v, reason := scrape.PrepareCatalogEnrichmentReceipt(record.Values, captured)
	if reason != "" {
		r.Reason = reason
		return r, nil
	}
	post, reason, err := w.nativePost(ctx, v.PostKey)
	if err != nil {
		return nil, err
	}
	if post != nil {
		r.PostUUID = &post.UUID
	}
	if reason != "" {
		r.Reason = reason
		return r, nil
	}
	if post == nil {
		return nil, models.ErrCatalogSnapshotInvalid
	}
	completed := record.Values["completed_at"].(string) // Checked by preparation.
	id, err := catalogEnrichmentReceiptID(w.snapshot.SourceUUID, w.snapshot.CatalogID, progress.CollectionUUID, post.UUID, v, completed)
	if err != nil {
		return nil, err
	}
	wanted := models.SourceEnrichmentReceipt{UUID: id, PostUUID: post.UUID, CollectionUUID: progress.CollectionUUID, CollectionRevision: 1,
		Origin: "migration", Policy: catalogEnrichmentPolicy, SourceVersion: v.Version, CompletedAt: completed,
		AttachmentLinksEnriched: v.AttachmentLinksEnriched, UnresolvedChildren: v.UnresolvedChildren, RecordedAt: stamp}
	prior, err := (&SourceEnrichmentReceiptStore{}).Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		wanted.RecordedAt = prior.RecordedAt
		if *prior != wanted {
			return nil, models.ErrCatalogSnapshotConflict
		}
	} else {
		_, err = dbWrapper.Exec(ctx, `INSERT INTO source_enrichment_receipts(uuid,post_uuid,collection_uuid,collection_revision,origin,policy,source_version,completed_at,attachment_links_enriched,unresolved_children,recorded_at)
 VALUES(?,?,?,1,'migration',?,?,?,?,?,?)`, id, post.UUID, progress.CollectionUUID, catalogEnrichmentPolicy, v.Version, completed, v.AttachmentLinksEnriched, v.UnresolvedChildren, stamp)
		if err != nil {
			return nil, err
		}
	}
	r.ReceiptUUID, r.Outcome = &id, "mapped"
	return r, nil
}
