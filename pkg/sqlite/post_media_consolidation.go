package sqlite

import (
	"cmp"
	"context"
	"slices"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

const consolidatedPostMediaQuery = `SELECT d.* FROM source_post_identities i
CROSS JOIN post_media_links l ON l.post_uuid=i.post_uuid
JOIN post_media_decisions d ON d.uuid=l.decision_uuid
WHERE i.canonical_uuid=? AND l.media_uuid IN `

// The encompassing post merge supplies the complete reviewed set of choices.
// It synchronizes the gallery only after every media/attachment choice is settled.
// This primitive intentionally has no independent HTTP mutation route.
func publishConsolidatedPostMedia(ctx context.Context, input models.SourcePostMediaInput, consolidation string) (*models.SourcePostMediaDecision, error) {
	if consolidation == "" {
		return nil, models.ErrSourcePostMediaInvalid
	}
	return (&SourcePostMediaStore{}).decideWithConsolidation(ctx, input, false, consolidation)
}

func validatePostMediaConsolidationScope(ctx context.Context, post, consolidation string) error {
	valid, err := currentPostConsolidation(ctx, post, consolidation)
	if err != nil {
		return err
	}
	if !valid {
		return models.ErrSourcePostMediaConflict
	}
	return nil
}

func currentPostConsolidation(ctx context.Context, post, consolidation string) (bool, error) {
	var valid bool
	err := dbWrapper.Get(ctx, &valid, `SELECT EXISTS(SELECT 1 FROM source_post_consolidations c
JOIN source_post_identities i ON i.post_uuid=c.destination_uuid AND i.canonical_uuid=i.post_uuid
WHERE c.uuid=? AND c.destination_uuid=?
AND c.sequence=(SELECT max(sequence) FROM source_post_consolidations WHERE destination_uuid=i.post_uuid))`, consolidation, post)
	return valid, err
}

func consolidatedPostMediaRows(ctx context.Context, post string, ids []string) ([]sourcePostMediaRow, error) {
	args := []interface{}{post}
	for _, id := range ids {
		args = append(args, id)
	}
	args = append(args, maxPostComparisonChoices+1)
	var rows []sourcePostMediaRow
	if err := dbWrapper.Select(ctx, &rows, consolidatedPostMediaQuery+getInBinding(len(ids))+" LIMIT ?", args...); err != nil {
		return nil, err
	}
	if len(rows) > maxPostComparisonChoices {
		return nil, models.ErrSourcePostIdentityLimit
	}
	slices.SortFunc(rows, func(a, b sourcePostMediaRow) int {
		if result := cmp.Compare(a.PostUUID, b.PostUUID); result != 0 {
			return result
		}
		return cmp.Compare(a.PostRevision, b.PostRevision)
	})
	return rows, nil
}

func postConsolidationCommitGuard(ctx context.Context, failure error) func() {
	finished := false
	txn.AddPreCommitHook(ctx, func(context.Context) error {
		if !finished {
			return failure
		}
		return nil
	})
	return func() { finished = true }
}
