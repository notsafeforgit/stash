package sqlite

import (
	"context"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

func loadCompletedCatalogRelations(ctx context.Context, id, expected string) (*catalogRelationsWork, error) {
	snapshot, err := (&CatalogSnapshotStore{}).Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if snapshot == nil || snapshot.State != "received" || snapshot.ManifestSHA256 != expected {
		return nil, models.ErrCatalogSnapshotConflict
	}
	evidence, err := (&CatalogEvidenceImportStore{}).Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if evidence == nil || evidence.State == "running" || evidence.ManifestSHA256 != expected {
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
	return &catalogRelationsWork{catalogEvidenceWork{snapshot: snapshot, manifest: manifest}}, nil
}
