package sqlite

import "github.com/stashapp/stash/pkg/models"

// STAT4 can prefer a full scan and sort even for a small, unfiltered page.
// These covering indexes bound the work to the requested page and timestamp
// ties. Migration 6 and the startup reconciler ensure they are available.
func (qb *queryBuilder) useMediaBrowseIndex(find *models.FindFilterType) {
	if find == nil || find.GetSort("") != "created_at" || find.IsGetAll() || find.GetPageSize() == 0 {
		return
	}

	switch qb.from {
	case sceneTable:
		qb.browseIndex = "fork_scenes_created_at"
	case imageTable:
		qb.browseIndex = "fork_images_created_at"
	}
}

func (qb queryBuilder) mediaBrowseFrom(includeSortPagination bool) string {
	// Decide at rendering time: aggregate queries can add joins after the
	// original list query was built. Filtered queries and separately rendered
	// counts keep SQLite's choice of indexes unrelated to the sort order.
	if !includeSortPagination || qb.browseIndex == "" || len(qb.joins) != 0 ||
		len(qb.whereClauses) != 0 || len(qb.havingClauses) != 0 || len(qb.withClauses) != 0 {
		return qb.from
	}
	return qb.from + " INDEXED BY " + qb.browseIndex
}
