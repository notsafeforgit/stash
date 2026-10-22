package sqlite

import (
	"context"
	"fmt"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

// UpgradeIdentity records an operator's confirmation that an existing root is
// still the same filesystem and directory. It strengthens the mount check while
// retaining the revision and snapshot identities of already admitted work.
// No ordinary read, probe, or schema migration makes this assertion implicitly.
func (s *MediaRootStore) UpgradeIdentity(ctx context.Context, id string, revision int, reviewed models.MediaRootBinding, reason string) (*models.MediaRoot, error) {
	if revision < 1 || !validAccountText(reason, 4096, false) {
		return nil, models.ErrSourceDefinitionInvalid
	}
	current, err := s.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if current == nil || current.Revision != revision || current.State != "active" || current.Binding == nil {
		return nil, models.ErrSourceDefinitionConflict
	}
	upgraded, err := archive.UpgradeMediaRootIdentity(*current.Binding, reviewed)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", models.ErrMediaRootBindingInvalid, err)
	}
	if current.IdentityUpgrade != nil {
		if current.IdentityUpgrade.PersistentIdentity != upgraded.PersistentIdentity || current.IdentityUpgrade.Reason != reason {
			return nil, models.ErrSourceDefinitionConflict
		}
		return current, nil // lost acknowledgement, with no duplicate review
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO media_root_identity_upgrades(root_uuid,root_revision,persistent_identity,reason) VALUES(?,?,?,?)`, current.UUID, revision, upgraded.PersistentIdentity, reason); err != nil {
		return nil, err
	}
	return s.Find(ctx, current.UUID)
}
