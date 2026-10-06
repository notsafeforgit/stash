package sqlite

import (
	"context"
	"slices"

	"github.com/stashapp/stash/pkg/models"
)

const consolidatedPostGalleriesQuery = `SELECT l.post_uuid,l.decision_uuid FROM source_post_identities i
JOIN post_gallery_links l ON l.post_uuid=i.post_uuid WHERE i.canonical_uuid=? ORDER BY i.post_uuid LIMIT ?`

type postConsolidationGalleryHead struct {
	PostUUID     string `db:"post_uuid"`
	DecisionUUID string `db:"decision_uuid"`
}

func consolidatedPostGalleryHeads(ctx context.Context, post string) ([]postConsolidationGalleryHead, error) {
	var heads []postConsolidationGalleryHead
	if err := dbWrapper.Select(ctx, &heads, consolidatedPostGalleriesQuery, post, maxPostIdentityMembers+1); err != nil {
		return nil, err
	}
	if len(heads) > maxPostIdentityMembers {
		return nil, models.ErrSourcePostIdentityLimit
	}
	return heads, nil
}

// The complete merge resolves gallery ownership once. Old choices remain
// immutable history; neither selected nor unselected gallery contents are
// deleted here. Synchronization follows only after source/media choices settle.
func publishConsolidatedPostGallery(ctx context.Context, input models.SourceGalleryChoiceInput, consolidation string, expected []string) (*models.SourceGalleryDecision, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	valid, err := currentPostConsolidation(ctx, input.PostUUID, consolidation)
	if err != nil {
		return nil, err
	}
	if !valid || len(expected) > maxPostIdentityMembers {
		return nil, models.ErrSourceGalleryConflict
	}
	expected = slices.Clone(expected)
	for _, id := range expected {
		if !validSourceRunUUID(id) {
			return nil, models.ErrSourceGalleryConflict
		}
	}
	slices.Sort(expected)
	heads, err := consolidatedPostGalleryHeads(ctx, input.PostUUID)
	if err != nil {
		return nil, err
	}
	current := make([]string, 0, len(heads))
	for _, head := range heads {
		current = append(current, head.DecisionUUID)
	}
	slices.Sort(current)
	if !slices.Equal(current, expected) {
		return nil, models.ErrSourceGalleryConflict
	}
	finish := postConsolidationCommitGuard(ctx, models.ErrSourceGalleryConflict)
	if len(heads) > 0 {
		args := make([]interface{}, 0, len(heads))
		for _, head := range heads {
			args = append(args, head.PostUUID)
		}
		if _, err := dbWrapper.Exec(ctx, "DELETE FROM post_gallery_links WHERE post_uuid IN "+getInBinding(len(args)), args...); err != nil {
			return nil, err
		}
	}
	ret, err := (&SourceGalleryStore{}).DecideAssociation(ctx, input)
	if err == nil {
		finish()
	}
	return ret, err
}

func postGallerySourceOwners(ctx context.Context, post string) (map[string]bool, error) {
	var ids []string
	if err := dbWrapper.Select(ctx, &ids, `SELECT post_uuid FROM source_post_identities
WHERE canonical_uuid=(SELECT canonical_uuid FROM source_post_identities WHERE post_uuid=?) LIMIT ?`, post, maxPostIdentityMembers+1); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, models.ErrSourcePayloadCorrupt
	}
	if len(ids) > maxPostIdentityMembers {
		return nil, models.ErrSourcePostIdentityLimit
	}
	ret := make(map[string]bool, len(ids))
	for _, id := range ids {
		ret[id] = true
	}
	return ret, nil
}
