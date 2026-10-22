package sqlite_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func legacyRootBinding(t *testing.T, path string) *models.MediaRootBinding {
	t.Helper()
	directory, err := os.Open(path)
	require.NoError(t, err)
	defer directory.Close()
	id, err := fsutil.OpenFileIdentity(directory)
	require.NoError(t, err)
	return &models.MediaRootBinding{Path: path, DirectoryIdentity: id}
}

func TestMediaRootIdentityUpgradeRetainsQueuedAndLeasedWork(t *testing.T) {
	for _, running := range []bool{false, true} {
		t.Run(map[bool]string{false: "queued", true: "running"}[running], func(t *testing.T) {
			f := newSourceRunFixture(t)
			definition := f.root.MediaRootDefinition
			definition.Binding = legacyRootBinding(t, f.root.Binding.Path)
			f.root = putMediaRoot(t, f.repo, models.MediaRootInput{UUID: f.root.UUID, ExpectedRevision: f.root.Revision, Origin: "review", MediaRootDefinition: definition})
			before := f.root
			req := f.request()
			run := f.submit(t, req)
			progress := models.SourceRunProgress{ItemsSeen: 3, Cursor: "saved-item"}
			if running {
				run = f.claim(t, run)
				var err error
				run, err = f.coordinator.Progress(t.Context(), f.token, run.Lease(), progress)
				require.NoError(t, err)
			}
			reviewed, err := archive.ProbeMediaRoot(before.Binding.Path)
			require.NoError(t, err)
			reason := "Reviewed same filesystem after device renumbering"
			var upgraded *models.MediaRoot
			require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				upgraded, err = f.repo.MediaRoot.UpgradeIdentity(ctx, before.UUID, before.Revision, *reviewed, reason)
				return err
			}))
			require.Equal(t, before.Revision, upgraded.Revision)
			require.Equal(t, before.Binding.DirectoryIdentity, upgraded.Binding.DirectoryIdentity)
			require.Equal(t, reviewed.DirectoryIdentity, upgraded.Binding.PersistentIdentity)
			require.Equal(t, run, f.find(t, run.UUID), "upgrade cannot rewrite admissions, windows, checkpoints, leases, or receipts")
			require.Equal(t, run.UUID, f.submit(t, req).UUID, "original producer request still replays")
			if !running {
				run = f.claim(t, run)
				require.NotNil(t, run, "queued work is not deferred as definition_changed")
			}
			run, err = f.coordinator.Renew(t.Context(), f.token, run.Lease(), time.Minute)
			require.NoError(t, err)
			if running {
				require.Equal(t, progress, run.Progress)
			}
			require.Equal(t, "succeeded", f.finish(t, run, "succeeded").State)
			require.NoError(t, f.db.Close())
			require.NoError(t, f.db.Open(f.db.DatabasePath()))
			require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				got, err := f.repo.MediaRoot.Find(ctx, before.UUID)
				require.NoError(t, err)
				require.Equal(t, upgraded, got)
				rows, err := f.repo.MediaRoot.History(ctx, before.UUID, before.Revision-1, 10)
				require.NoError(t, err)
				require.Len(t, rows, 1)
				require.Equal(t, before.Binding.DirectoryIdentity, rows[0].Binding.DirectoryIdentity)
				require.Equal(t, reason, rows[0].IdentityUpgrade.Reason)
				return err
			}))
		})
	}
}

func TestMediaRootIdentityUpgradeIsExplicitImmutableAndScoped(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	dir := t.TempDir()
	root := putMediaRoot(t, repo, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Legacy", State: "active", Binding: legacyRootBinding(t, dir)}})
	checked, err := archive.ProbeMediaRoot(dir)
	require.NoError(t, err)
	apply := func(revision int, binding models.MediaRootBinding, reason string) (*models.MediaRoot, error) {
		var result *models.MediaRoot
		err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
			var err error
			result, err = repo.MediaRoot.UpgradeIdentity(ctx, root.UUID, revision, binding, reason)
			return err
		})
		return result, err
	}
	_, err = apply(2, *checked, "Review")
	require.ErrorIs(t, err, models.ErrSourceDefinitionConflict)
	other, err := archive.ProbeMediaRoot(t.TempDir())
	require.NoError(t, err)
	_, err = apply(1, *other, "Review")
	require.ErrorIs(t, err, models.ErrMediaRootBindingInvalid)
	wrong := *checked
	wrong.DirectoryIdentity = other.DirectoryIdentity
	_, err = apply(1, wrong, "Review")
	require.ErrorIs(t, err, models.ErrMediaRootBindingInvalid)
	_, err = apply(1, *checked, "")
	require.ErrorIs(t, err, models.ErrSourceDefinitionInvalid)
	upgraded, err := apply(1, *checked, "Confirmed same filesystem")
	require.NoError(t, err)
	replay, err := apply(1, *checked, "Confirmed same filesystem")
	require.NoError(t, err)
	require.Equal(t, upgraded, replay)
	_, err = apply(1, *checked, "Changed review")
	require.ErrorIs(t, err, models.ErrSourceDefinitionConflict)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	_, err = raw.Exec("UPDATE media_root_identity_upgrades SET reason='changed'")
	require.ErrorContains(t, err, "immutable")
	_, err = raw.Exec("DELETE FROM media_root_identity_upgrades")
	require.ErrorContains(t, err, "retained")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	input := models.MediaRootInput{UUID: root.UUID, ExpectedRevision: 1, Origin: "review", MediaRootDefinition: upgraded.MediaRootDefinition}
	require.Equal(t, upgraded, putMediaRoot(t, repo, input), "idempotent root updates retain proof and revision")
	stale := input
	stale.Binding = root.Binding
	err = repo.WithTxn(t.Context(), func(ctx context.Context) error { _, err := repo.MediaRoot.Put(ctx, stale); return err })
	require.ErrorIs(t, err, models.ErrSourceDefinitionConflict, "stale clients cannot remove persistent filesystem evidence")
	input.Label = "Renamed"
	changed := putMediaRoot(t, repo, input)
	require.Equal(t, 2, changed.Revision)
	require.Equal(t, *checked, *changed.Binding, "a real definition change uses current identities")
	require.Nil(t, changed.IdentityUpgrade)
	_, err = apply(1, *checked, "Confirmed same filesystem")
	require.ErrorIs(t, err, models.ErrSourceDefinitionConflict)
	_, err = apply(2, *checked, "Not a legacy binding")
	require.ErrorIs(t, err, models.ErrMediaRootBindingInvalid)
	// Historical proof remains auditable but cannot authorize a later root.
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM media_root_identity_upgrades"))
	anonymiser, err := sqlite.NewAnonymiser(db, filepath.Join(t.TempDir(), "anonymous.sqlite"))
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(t.Context()))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM media_root_identity_upgrades"), "anonymisation only removes deployment evidence from the export")
}
