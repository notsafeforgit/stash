package sqlite

import (
	"context"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

func normalizeProfileSource(ctx context.Context, input *models.SourceCollectionInput, id string) error {
	var alias bool
	if err := dbWrapper.Get(ctx, &alias, "SELECT EXISTS(SELECT 1 FROM source_collection_aliases WHERE alias_uuid=?)", id); err != nil {
		return err
	}
	if alias {
		return models.ErrSourceDefinitionConflict
	}
	if input.Namespace != "native:reddit" {
		return nil
	}
	profile := scrape.RedditProfileURL(input.TargetURL)
	if profile == "" {
		return nil
	}
	input.TargetURL, input.Kind = profile, "account"
	var duplicate bool
	if err := dbWrapper.Get(ctx, &duplicate, `SELECT EXISTS(SELECT 1 FROM source_collection_revisions r
JOIN source_collections c ON c.uuid=r.collection_uuid AND c.revision=r.revision
WHERE r.target_url=? AND r.root_uuid IS ? AND r.path_prefix=? AND c.uuid!=?
AND NOT EXISTS(SELECT 1 FROM source_collection_aliases a WHERE a.alias_uuid=c.uuid))`, profile, input.RootUUID, input.PathPrefix, id); err != nil {
		return err
	}
	if duplicate {
		return models.ErrSourceDefinitionConflict
	}
	return nil
}

// Frozen requests retain their original identities. Their permission to execute
// also depends on the single current profile subscription, including its scope.
func profileAliasActive(ctx context.Context, c *models.SourceCollection) error {
	var valid bool
	if err := dbWrapper.Get(ctx, &valid, `SELECT NOT EXISTS(SELECT 1 FROM source_collection_aliases WHERE alias_uuid=?)
OR EXISTS(SELECT 1 FROM source_collection_aliases a
JOIN source_collections s ON s.uuid=a.source_uuid
JOIN source_collection_revisions r ON r.collection_uuid=s.uuid AND r.revision=s.revision
WHERE a.alias_uuid=? AND r.state='active' AND r.target_url=? AND r.root_uuid IS ? AND r.path_prefix=?)`,
		c.UUID, c.UUID, scrape.RedditProfileURL(c.TargetURL), c.RootUUID, c.PathPrefix); err != nil {
		return err
	}
	if !valid {
		return models.ErrSourceDefinitionConflict
	}
	return nil
}
