package ingest_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func (f manualFileFixture) scanCollection(t *testing.T, prefix, kind, state string) *models.SourceCollection {
	t.Helper()
	var ret *models.SourceCollection
	require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = f.service.Repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
			Label: "Folder scope", Kind: kind, State: state, RootUUID: &f.root.UUID, PathPrefix: prefix}})
		return err
	}))
	return ret
}

func (f manualFileFixture) scanPolicy(t *testing.T, collection *models.SourceCollection, revision int, scans, enabled bool) {
	t.Helper()
	require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.service.Repo.MetadataPolicy.Put(ctx, models.MetadataPolicyInput{
			CollectionUUID: collection.UUID, ExpectedCollectionRevision: collection.Revision, ExpectedRevision: revision, Origin: "review",
			Definition: models.MetadataPolicyDefinition{ApplyToScans: scans, Enabled: enabled,
				Rules: map[models.ArchiveEntityKind]models.MetadataPolicyRule{models.ArchiveImage: {OnCreate: true, FilenameTitleFallback: true, Mappings: map[string]models.MetadataMapping{}}}}})
		return err
	}))
}

func TestManualScanScopeUsesSpecificPoliciesAndCurrentSourceBoundaries(t *testing.T) {
	f := newManualFileFixture(t, models.ArchiveImage)
	scope := func(directory string) *ingest.ManualScanScope {
		ret, err := f.service.ManualScanScope(t.Context(), f.collection.UUID, directory)
		require.NoError(t, err)
		return ret
	}
	require.Equal(t, f.collection.UUID, scope("a").CollectionUUID)
	// A same-root policy takes precedence over a policy-free fallback scope.
	rootPolicy := f.scanCollection(t, ".", "directory", "active")
	f.scanPolicy(t, rootPolicy, 0, true, true)
	require.Equal(t, rootPolicy.UUID, scope("a").CollectionUUID)
	child := f.scanCollection(t, "a", "directory", "active")
	f.scanPolicy(t, child, 0, true, true)
	require.Equal(t, child.UUID, scope("a").CollectionUUID, "single-character prefixes are deeper than the root")
	grandchild := f.scanCollection(t, "a/nested", "directory", "active")
	f.scanPolicy(t, grandchild, 0, false, true)
	require.Equal(t, child.UUID, scope("a/nested").CollectionUUID)
	f.scanPolicy(t, grandchild, 1, true, false)
	require.Equal(t, grandchild.UUID, scope("a/nested").CollectionUUID, "disabled policies mask inherited metadata")
	conflict := f.scanCollection(t, "a/nested", "manual_batch", "active")
	f.scanPolicy(t, conflict, 0, true, true)
	require.Equal(t, "ambiguous_directory", scope("a/nested").BlockedReason)
	deep := f.scanCollection(t, "a/nested/resolved", "directory", "active")
	f.scanPolicy(t, deep, 0, true, true)
	require.Equal(t, deep.UUID, scope("a/nested/resolved").CollectionUUID)
	source := f.scanCollection(t, "a/nested/resolved/source", "legacy_catalog", "disabled")
	require.Equal(t, "source_folder", scope(source.PathPrefix+"/album").BlockedReason)
	require.Equal(t, deep.UUID, scope(source.PathPrefix+"-different").CollectionUUID, "prefixes match path components")
	// Only the current source binding is considered, not its earlier location.
	require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		definition := source.SourceCollectionDefinition
		definition.PathPrefix = "moved"
		definition.State = "retired"
		_, err := f.service.Repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: source.UUID, ExpectedRevision: source.Revision, Origin: "review", SourceCollectionDefinition: definition})
		return err
	}))
	require.Empty(t, scope(source.PathPrefix).BlockedReason)
	require.Equal(t, "source_folder", scope("moved").BlockedReason, "retirement does not turn source media into manual media")
	// Rootless imported memberships are deliberately not scan scopes.
	require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.service.Repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "migration", SourceCollectionDefinition: models.SourceCollectionDefinition{
			Label: "Historical membership", Kind: "legacy_catalog", State: "disabled"}})
		return err
	}))
	require.Empty(t, scope("other").BlockedReason)
	_, err := f.service.ManualScanScope(t.Context(), child.UUID, "elsewhere")
	require.ErrorIs(t, err, ingest.ErrInvalid)
	f.scanPolicy(t, child, 1, false, true)
	childScope, err := f.service.ManualScanScope(t.Context(), child.UUID, "a")
	require.NoError(t, err)
	require.Equal(t, "policy_not_for_scans", childScope.BlockedReason)
}

func TestManualScanRejectsSourceRegistrationAfterPreviewOrAdmission(t *testing.T) {
	for _, admitted := range []bool{false, true} {
		name := "after_preview"
		if admitted {
			name = "after_admission"
		}
		t.Run(name, func(t *testing.T) {
			f := newManualFileFixture(t, models.ArchiveImage)
			require.NoError(t, os.Mkdir(filepath.Join(f.root.Binding.Path, "source"), 0700))
			f.input.RelativePath = "source/" + f.input.RelativePath
			destination := filepath.Join(f.root.Binding.Path, filepath.FromSlash(f.input.RelativePath))
			require.NoError(t, os.Rename(f.path, destination))
			f.path = destination
			f.input.ScanCollectionUUID = f.collection.UUID
			request := f.request(t)
			if admitted {
				_, err := f.service.SubmitManualFile(t.Context(), request)
				require.NoError(t, err)
			}
			f.scanCollection(t, "source", "feed", "disabled")
			if admitted {
				require.NoError(t, f.db.Close())
				require.NoError(t, f.db.Open(f.db.DatabasePath()))
				recovered, err := f.service.SubmitManualFile(t.Context(), request)
				require.NoError(t, err, "saved admission remains recoverable after the scope changes")
				require.Equal(t, "queued", recovered.State)
				processed, err := f.worker(t, func(context.Context, ingest.FileWork, ingest.IntakePublicationResult, ingest.FileEffectGuard) error {
					t.Fatal("changed source ownership must prevent publication and effects")
					return nil
				}).ProcessNext(t.Context())
				require.NoError(t, err)
				require.True(t, processed)
				status := f.status(t, request.RequestUUID)
				require.Equal(t, "failed", status.State)
				require.Equal(t, "file_scope_changed", status.ErrorCode)
				require.False(t, status.RegistrationCommitted)
			} else {
				_, err := f.service.SubmitManualFile(t.Context(), request)
				require.ErrorIs(t, err, ingest.ErrManualFileChanged)
			}
			require.NoError(t, f.service.Repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				file, err := f.service.Repo.File.FindByPath(ctx, f.path, true)
				require.Nil(t, file)
				return err
			}))
			f.input.ScanCollectionUUID = ""
			_, err := f.service.PreviewManualFile(t.Context(), f.input)
			require.NoError(t, err, "explicit user imports remain possible")
		})
	}
}

func TestManualScanWholeRootAccessUsesObservedFoldersAndProtectsExactProducerFiles(t *testing.T) {
	f := newManualFileFixture(t, models.ArchiveImage)
	source := f.scanCollection(t, ".", "legacy_catalog", "active")
	observe := func(relative string) {
		require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := f.service.Repo.SourceFile.RecordObservation(ctx, models.SourceFileObservation{UUID: uuid.NewString(),
				CollectionUUID: source.UUID, CollectionRevision: source.Revision, RootUUID: f.root.UUID, RootRevision: f.root.Revision,
				RelativePath: relative, State: "present", Role: "local", Origin: "migration", ObservedAt: time.Now(), Details: json.RawMessage(`{}`)})
			return err
		}))
	}
	for _, relative := range []string{"old-handle/a.jpg", "new-handle/b.jpg", "shared/source/c.jpg", "日本語/d.jpg"} {
		observe(relative)
	}
	for _, directory := range []string{"old-handle", "new-handle", "shared/source", "日本語"} {
		scope, err := f.service.ManualScanScope(t.Context(), f.collection.UUID, directory)
		require.NoError(t, err)
		require.Equal(t, "source_folder", scope.BlockedReason, directory)
	}
	for _, directory := range []string{".", "new-purchases", "shared", "old-handle-different"} {
		scope, err := f.service.ManualScanScope(t.Context(), f.collection.UUID, directory)
		require.NoError(t, err)
		require.Equal(t, f.collection.UUID, scope.CollectionUUID, directory)
	}
	f.input.ScanCollectionUUID = f.collection.UUID
	_, err := f.service.PreviewManualFile(t.Context(), f.input)
	require.NoError(t, err, "root-wide source access does not exclude a new manual file")
	observe(f.input.RelativePath)
	_, err = f.service.PreviewManualFile(t.Context(), f.input)
	require.ErrorIs(t, err, ingest.ErrManualFileChanged, "a known source file at the root is still excluded")
	f.input.ScanCollectionUUID = ""
	_, err = f.service.PreviewManualFile(t.Context(), f.input)
	require.NoError(t, err, "explicit review remains available")
}

func TestManualScanProducerAdmissionWinsBeforeManualPublication(t *testing.T) {
	f := newIntakePublicationFixture(t, true)
	var base *models.SourceCollection
	require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		base, err = f.service.Repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
			Label: "Automatic local files", Kind: "directory", State: "active", RootUUID: &f.root.UUID, PathPrefix: "."}})
		return err
	}))
	input := ingest.ManualFileInput{ScanCollectionUUID: base.UUID, CollectionUUID: base.UUID, RelativePath: "image.png", MediaKind: models.ArchiveImage}
	preview, err := f.service.PreviewManualFile(t.Context(), input)
	require.NoError(t, err)
	request := ingest.ManualFileRequest{ManualFileInput: input, RequestUUID: uuid.NewString(), Signature: preview.Signature}
	_, err = f.service.SubmitManualFile(t.Context(), request)
	require.NoError(t, err)
	event := f.fileEvent(t)
	_, err = f.submitFile(t, event)
	require.NoError(t, err)
	_, err = f.service.PreviewManualFile(t.Context(), input)
	require.ErrorIs(t, err, ingest.ErrManualFileChanged, "producer admission reserves a new destination before registration")
	worker := f.worker(t, func(ctx context.Context, work ingest.FileWork, _ ingest.IntakePublicationResult, guard ingest.FileEffectGuard) error {
		require.Nil(t, work.Manual, "the automatic manual job must stop before publication")
		return guard(ctx)
	})
	processed, err := worker.ProcessNext(t.Context())
	require.NoError(t, err)
	require.True(t, processed)
	manual, err := f.service.ManualFileStatus(t.Context(), request.RequestUUID)
	require.NoError(t, err)
	require.Equal(t, "failed", manual.State)
	require.False(t, manual.RegistrationCommitted)
	processed, err = worker.ProcessNext(t.Context())
	require.NoError(t, err)
	require.True(t, processed)
	status, _ := f.fileStatus(t, event.EventUUID)
	require.Equal(t, "succeeded", status.State)
	_, err = f.service.PreviewManualFile(t.Context(), input)
	require.ErrorIs(t, err, ingest.ErrManualFileChanged, "completed producer work remains source provenance")
}

func TestManualScanImportsUsingSelectedChildAndFencesBaseRevision(t *testing.T) {
	f := newManualFileFixture(t, models.ArchiveImage)
	child := f.scanCollection(t, "a", "directory", "active")
	f.scanPolicy(t, child, 0, true, true)
	require.NoError(t, os.Mkdir(filepath.Join(f.root.Binding.Path, "a"), 0700))
	f.input.RelativePath = "a/" + f.input.RelativePath
	destination := filepath.Join(f.root.Binding.Path, filepath.FromSlash(f.input.RelativePath))
	require.NoError(t, os.Rename(f.path, destination))
	f.path = destination
	f.input.ScanCollectionUUID, f.input.CollectionUUID = f.collection.UUID, child.UUID
	stale := f.request(t)
	require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		definition := f.collection.SourceCollectionDefinition
		definition.Label = "Revised manual scope"
		var err error
		f.collection, err = f.service.Repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, Origin: "review", SourceCollectionDefinition: definition})
		return err
	}))
	_, err := f.service.SubmitManualFile(t.Context(), stale)
	require.ErrorIs(t, err, ingest.ErrManualFileChanged)
	request := f.request(t)
	_, err = f.service.SubmitManualFile(t.Context(), request)
	require.NoError(t, err)
	processed, err := f.worker(t, func(ctx context.Context, work ingest.FileWork, _ ingest.IntakePublicationResult, guard ingest.FileEffectGuard) error {
		require.Equal(t, child.UUID, work.Publication.CollectionUUID)
		require.Equal(t, f.collection.Revision, work.Manual.ScanCollectionRevision)
		return guard(ctx)
	}).ProcessNext(t.Context())
	require.NoError(t, err)
	require.True(t, processed)
	status := f.status(t, request.RequestUUID)
	require.Equal(t, "succeeded", status.State)
	require.Equal(t, "filename", intakeField(t, f.service.Repo, status.Publication.MediaUUID, "title").Origin)
}
