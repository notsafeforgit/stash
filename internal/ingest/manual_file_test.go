package ingest_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/file"
	imagefile "github.com/stashapp/stash/pkg/file/image"
	"github.com/stashapp/stash/pkg/file/video"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

type manualFileFixture struct {
	db         *sqlite.Database
	service    *ingest.Service
	root       *models.MediaRoot
	collection *models.SourceCollection
	input      ingest.ManualFileInput
	path       string
}

func newManualFileFixture(t *testing.T, kind models.ArchiveEntityKind) manualFileFixture {
	t.Helper()
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "manual.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	f := manualFileFixture{db: db, service: ingest.New(db.Repository())}
	dir := t.TempDir()
	name := "Purchased image.png"
	if kind == models.ArchiveScene {
		name = "Purchased video.mp4"
		binary, err := exec.LookPath("ffmpeg")
		require.NoError(t, err, "direct-file scene verification requires ffmpeg")
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		output, err := exec.CommandContext(ctx, binary, "-v", "error", "-f", "lavfi", "-i", "color=blue:s=32x32:r=2", "-t", "1", "-c:v", "mpeg4", "-y", filepath.Join(dir, name)).CombinedOutput()
		require.NoError(t, err, string(output))
	} else {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), intakePNG(t), 0600))
	}
	f.path = filepath.Join(dir, name)
	binding, err := archive.ProbeMediaRoot(dir)
	require.NoError(t, err)
	require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		f.root, err = f.service.Repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Local media", State: "active", Binding: binding}})
		if err != nil {
			return err
		}
		f.collection, err = f.service.Repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
			Label: "Purchased batch", Kind: "manual_batch", State: "active", RootUUID: &f.root.UUID, PathPrefix: "."}})
		return err
	}))
	f.input = ingest.ManualFileInput{CollectionUUID: f.collection.UUID, RelativePath: name, MediaKind: kind}
	return f
}

func (f manualFileFixture) worker(t *testing.T, effects func(context.Context, ingest.FileWork, ingest.IntakePublicationResult, ingest.FileEffectGuard) error) *ingest.FileWorker {
	t.Helper()
	return ingest.NewFileWorker(f.service, func(kind models.ArchiveEntityKind) *file.Scanner {
		var decorator file.Decorator = &imagefile.Decorator{FFProbe: intakeProbe(t)}
		if kind == models.ArchiveScene {
			decorator = &video.Decorator{FFProbe: intakeProbe(t)}
		}
		return &file.Scanner{FingerprintCalculator: intakeFingerprinter{}, FileDecorators: []file.Decorator{decorator}}
	}, effects)
}

func (f manualFileFixture) request(t *testing.T) ingest.ManualFileRequest {
	t.Helper()
	preview, err := f.service.PreviewManualFile(t.Context(), f.input)
	require.NoError(t, err)
	return ingest.ManualFileRequest{ManualFileInput: f.input, RequestUUID: uuid.NewString(), Signature: preview.Signature}
}

func (f manualFileFixture) status(t *testing.T, request string) *ingest.ManualFileStatus {
	t.Helper()
	result, err := f.service.ManualFileStatus(t.Context(), request)
	require.NoError(t, err)
	return result
}

func (f manualFileFixture) policy(t *testing.T, revision int, mappings map[string]models.MetadataMapping) *models.MetadataPolicy {
	t.Helper()
	var policy *models.MetadataPolicy
	require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		policy, err = f.service.Repo.MetadataPolicy.Put(ctx, models.MetadataPolicyInput{CollectionUUID: f.collection.UUID, ExpectedCollectionRevision: f.collection.Revision,
			ExpectedRevision: revision, Origin: "review", Definition: models.MetadataPolicyDefinition{Enabled: true, ApplyToScans: true,
				Rules: map[models.ArchiveEntityKind]models.MetadataPolicyRule{f.input.MediaKind: {OnCreate: true, OnExisting: true, FilenameTitleFallback: true, Mappings: mappings}}}})
		return err
	}))
	return policy
}

func TestManualFileDiscoveryVersionSurvivesRegistrationAndDetectsHiddenFileChanges(t *testing.T) {
	f := newManualFileFixture(t, models.ArchiveImage)
	before, err := f.service.PreviewManualFile(t.Context(), f.input)
	require.NoError(t, err)
	require.Regexp(t, "^[0-9a-f]{64}$", before.FileSignature)
	request := ingest.ManualFileRequest{ManualFileInput: f.input, RequestUUID: uuid.NewString(), Signature: before.Signature}
	_, err = f.service.SubmitManualFile(t.Context(), request)
	require.NoError(t, err)
	processed, err := f.worker(t, func(ctx context.Context, _ ingest.FileWork, _ ingest.IntakePublicationResult, guard ingest.FileEffectGuard) error {
		return guard(ctx)
	}).ProcessNext(t.Context())
	require.NoError(t, err)
	require.True(t, processed)
	require.Equal(t, "succeeded", f.status(t, request.RequestUUID).State)
	after, err := f.service.PreviewManualFile(t.Context(), f.input)
	require.NoError(t, err)
	require.NotEmpty(t, after.ExistingFileUUID)
	require.NotEqual(t, before.Signature, after.Signature)
	require.Equal(t, before.FileSignature, after.FileSignature, "registration alone must not trigger another scheduled import")
	f.policy(t, 0, map[string]models.MetadataMapping{})
	policy, err := f.service.PreviewManualFile(t.Context(), f.input)
	require.NoError(t, err)
	require.Equal(t, after.FileSignature, policy.FileSignature)
	require.NotEqual(t, after.PolicyRevision, policy.PolicyRevision)
	info, err := os.Stat(f.path)
	require.NoError(t, err)
	body, err := os.ReadFile(f.path)
	require.NoError(t, err)
	body[len(body)-1] ^= 1
	require.NoError(t, os.WriteFile(f.path, body, 0600))
	require.NoError(t, os.Chtimes(f.path, info.ModTime(), info.ModTime()))
	changed, err := f.service.PreviewManualFile(t.Context(), f.input)
	require.NoError(t, err)
	require.Equal(t, after.Size, changed.Size)
	require.Equal(t, after.ModifiedAt, changed.ModifiedAt)
	require.NotEqual(t, after.FileSignature, changed.FileSignature)
}

func TestManualFileImportsVideoAndImageWithPerformerPolicyWithoutProducer(t *testing.T) {
	for _, kind := range []models.ArchiveEntityKind{models.ArchiveScene, models.ArchiveImage} {
		t.Run(string(kind), func(t *testing.T) {
			f := newManualFileFixture(t, kind)
			repo := f.service.Repo
			var performer *models.ArchiveEntity
			require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
				p := models.NewPerformer()
				p.Name = "Purchased-only performer"
				if err := repo.Performer.Create(ctx, &models.CreatePerformerInput{Performer: &p}); err != nil {
					return err
				}
				var err error
				performer, err = repo.ArchiveEntity.FindByLocalID(ctx, models.ArchivePerformer, p.ID)
				return err
			}))
			f.policy(t, 0, map[string]models.MetadataMapping{"performers": {Value: json.RawMessage(`["` + performer.UUID + `"]`)}})
			request := f.request(t)
			require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				current, err := repo.File.FindByPath(ctx, f.path, true)
				require.Nil(t, current, "preview does not register media")
				return err
			}))
			accepted, err := f.service.SubmitManualFile(t.Context(), request)
			require.NoError(t, err)
			require.Equal(t, "queued", accepted.State)
			require.False(t, accepted.RegistrationCommitted)
			worker := f.worker(t, func(ctx context.Context, work ingest.FileWork, result ingest.IntakePublicationResult, guard ingest.FileEffectGuard) error {
				require.Empty(t, work.ProducerUUID)
				require.Empty(t, work.EventUUID)
				require.Nil(t, work.Publication.Source)
				require.Equal(t, request.RequestUUID, work.Manual.Request.RequestUUID)
				require.NoError(t, guard(ctx))
				status := f.status(t, request.RequestUUID)
				require.True(t, status.RegistrationCommitted)
				require.False(t, status.MediaIngested)
				return nil
			})
			processed, err := worker.ProcessNext(t.Context())
			require.NoError(t, err)
			require.True(t, processed)
			completed := f.status(t, request.RequestUUID)
			require.Equal(t, "succeeded", completed.State)
			require.True(t, completed.MediaIngested)
			media := completed.Publication.MediaUUID
			require.Equal(t, kind, completed.Publication.MediaKind)
			require.Equal(t, "filename", intakeField(t, repo, media, "title").Origin)
			require.JSONEq(t, `["`+performer.UUID+`"]`, string(intakeField(t, repo, media, "performers").Value))
			require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
				state, err := repo.MetadataField.State(ctx, media, "title")
				if err != nil {
					return err
				}
				_, err = repo.MetadataField.Decide(ctx, models.MetadataFieldDecisionInput{EntityUUID: media, ExpectedEntityRevision: state.Entity.Revision, Field: "title", Mode: "clear", Origin: "review"})
				return err
			}))
			// A separately reviewed rescan reuses the media and protects a clear.
			request = f.request(t)
			_, err = f.service.SubmitManualFile(t.Context(), request)
			require.NoError(t, err)
			_, err = worker.ProcessNext(t.Context())
			require.NoError(t, err)
			rescanned := f.status(t, request.RequestUUID)
			require.Equal(t, media, rescanned.Publication.MediaUUID)
			require.False(t, rescanned.Publication.MediaCreated)
			require.Equal(t, "clear", intakeField(t, repo, media, "title").Mode)
			require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				captures, err := repo.SourceCollection.Captures(ctx, f.collection.UUID, nil, 10)
				require.Empty(t, captures)
				if err != nil {
					return err
				}
				intakes, err := repo.SourceCollection.MediaIntake(ctx, f.collection.UUID, "", 10)
				require.Len(t, intakes, 2)
				return err
			}))
		})
	}
}

func TestManualFileLostResponseRecoversAfterFileRemovalAndRestart(t *testing.T) {
	f := newManualFileFixture(t, models.ArchiveImage)
	request := f.request(t)
	accepted, err := f.service.SubmitManualFile(t.Context(), request)
	require.NoError(t, err)
	require.NoError(t, os.Remove(f.path))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	recovered, err := f.service.SubmitManualFile(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, accepted, recovered)
	require.Equal(t, recovered, f.status(t, request.RequestUUID))
	request.RelativePath = "different.png"
	_, err = f.service.SubmitManualFile(t.Context(), request)
	require.ErrorIs(t, err, models.ErrArchiveJobConflict)
}

func TestManualFileRejectsStaleReviewBeforeQueueOrPublication(t *testing.T) {
	for _, when := range []string{"before admission", "after admission"} {
		for _, changed := range []string{"replaced file", "same-size overwrite", "policy", "root"} {
			t.Run(when+"/"+changed, func(t *testing.T) {
				f := newManualFileFixture(t, models.ArchiveImage)
				request := f.request(t)
				if when == "after admission" {
					_, err := f.service.SubmitManualFile(t.Context(), request)
					require.NoError(t, err)
				}
				switch changed {
				case "replaced file":
					require.NoError(t, os.Rename(f.path, f.path+".old"))
					require.NoError(t, os.WriteFile(f.path, intakePNG(t), 0600))
				case "same-size overwrite":
					info, err := os.Stat(f.path)
					require.NoError(t, err)
					require.NoError(t, os.WriteFile(f.path, intakePNG(t), 0600))
					require.NoError(t, os.Chtimes(f.path, info.ModTime(), info.ModTime()))
				case "policy":
					f.policy(t, 0, map[string]models.MetadataMapping{})
				case "root":
					require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
						definition := f.root.MediaRootDefinition
						definition.Label = "Changed after review"
						_, err := f.service.Repo.MediaRoot.Put(ctx, models.MediaRootInput{UUID: f.root.UUID, ExpectedRevision: f.root.Revision, Origin: "review", MediaRootDefinition: definition})
						return err
					}))
				}
				if when == "before admission" {
					_, err := f.service.SubmitManualFile(t.Context(), request)
					require.ErrorIs(t, err, ingest.ErrManualFileChanged)
					_, err = f.service.ManualFileStatus(t.Context(), request.RequestUUID)
					require.ErrorIs(t, err, ingest.ErrNotFound)
				} else {
					worker := f.worker(t, func(context.Context, ingest.FileWork, ingest.IntakePublicationResult, ingest.FileEffectGuard) error {
						t.Error("stale review must not publish or notify")
						return nil
					})
					_, err := worker.ProcessNext(t.Context())
					require.NoError(t, err)
					status := f.status(t, request.RequestUUID)
					require.Equal(t, "failed", status.State)
					require.False(t, status.RegistrationCommitted)
				}
			})
		}
	}
}

func TestManualFileResumesEffectsWithoutReapplyingMetadata(t *testing.T) {
	f := newManualFileFixture(t, models.ArchiveImage)
	policy := f.policy(t, 0, map[string]models.MetadataMapping{})
	request := f.request(t)
	_, err := f.service.SubmitManualFile(t.Context(), request)
	require.NoError(t, err)
	var eventID string
	worker := f.worker(t, func(_ context.Context, work ingest.FileWork, _ ingest.IntakePublicationResult, _ ingest.FileEffectGuard) error {
		eventID = work.Publication.UUID
		return errors.New("temporarily unavailable effects")
	})
	_, err = worker.ProcessNext(t.Context())
	require.NoError(t, err)
	pending := f.status(t, request.RequestUUID)
	require.Equal(t, "queued", pending.State)
	require.True(t, pending.RegistrationCommitted)
	f.policy(t, policy.Revision, map[string]models.MetadataMapping{"title": {Value: json.RawMessage(`"Later rule"`)}})
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	worker = f.worker(t, func(ctx context.Context, work ingest.FileWork, result ingest.IntakePublicationResult, guard ingest.FileEffectGuard) error {
		require.Equal(t, eventID, work.Publication.UUID)
		require.Equal(t, pending.Publication, &result)
		return guard(ctx)
	})
	worker.Durable.Now = func() time.Time { return time.Now().Add(time.Minute) }
	_, err = worker.ProcessNext(t.Context())
	require.NoError(t, err)
	completed := f.status(t, request.RequestUUID)
	require.True(t, completed.MediaIngested)
	require.EqualValues(t, 2, completed.Attempts)
	require.JSONEq(t, `"Purchased image"`, string(intakeField(t, f.service.Repo, completed.Publication.MediaUUID, "title").Value))
}

func TestManualFileCancellationPreservesCommittedRegistration(t *testing.T) {
	f := newManualFileFixture(t, models.ArchiveImage)
	request := f.request(t)
	_, err := f.service.SubmitManualFile(t.Context(), request)
	require.NoError(t, err)
	worker := f.worker(t, func(context.Context, ingest.FileWork, ingest.IntakePublicationResult, ingest.FileEffectGuard) error {
		return errors.New("effects offline")
	})
	_, err = worker.ProcessNext(t.Context())
	require.NoError(t, err)
	pending := f.status(t, request.RequestUUID)
	require.True(t, pending.RegistrationCommitted)
	_, err = f.service.CancelManualFile(t.Context(), request.RequestUUID, pending.Revision-1)
	require.ErrorIs(t, err, models.ErrArchiveJobConflict)
	cancelled, err := f.service.CancelManualFile(t.Context(), request.RequestUUID, pending.Revision)
	require.NoError(t, err)
	require.Equal(t, "cancelled", cancelled.State)
	require.True(t, cancelled.RegistrationCommitted)
	require.False(t, cancelled.MediaIngested)
	require.Equal(t, pending.Publication, cancelled.Publication)
	replayed, err := f.service.SubmitManualFile(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, cancelled, replayed)
	processed, err := worker.ProcessNext(t.Context())
	require.NoError(t, err)
	require.False(t, processed)
}

func TestManualFilePreviewRejectsPartialEscapingAndOutsideCollectionPaths(t *testing.T) {
	f := newManualFileFixture(t, models.ArchiveImage)
	for _, relative := range []string{"../outside.png", "/absolute.png", "download.PART", ".", "missing.png"} {
		input := f.input
		input.RelativePath = relative
		_, err := f.service.PreviewManualFile(t.Context(), input)
		require.Error(t, err, relative)
	}
	outside := filepath.Join(t.TempDir(), "outside.png")
	require.NoError(t, os.WriteFile(outside, intakePNG(t), 0600))
	require.NoError(t, os.Symlink(outside, filepath.Join(f.root.Binding.Path, "escape.png")))
	input := f.input
	input.RelativePath = "escape.png"
	_, err := f.service.PreviewManualFile(t.Context(), input)
	require.Error(t, err)
	require.NoError(t, f.service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		definition := f.collection.SourceCollectionDefinition
		definition.PathPrefix = "another-folder"
		_, err := f.service.Repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision, SourceCollectionDefinition: definition, Origin: "review"})
		return err
	}))
	_, err = f.service.PreviewManualFile(t.Context(), f.input)
	require.ErrorIs(t, err, ingest.ErrDefinition)
}

type changingManualAdmission struct {
	models.ArchiveJobReaderWriter
	mutate func()
}

func (s changingManualAdmission) Submit(ctx context.Context, input models.ArchiveJobSubmission, now time.Time, limit int) (*models.ArchiveJob, error) {
	result, err := s.ArchiveJobReaderWriter.Submit(ctx, input, now, limit)
	if err == nil {
		s.mutate()
	}
	return result, err
}

func TestManualFileAdmissionRechecksFileBeforeCommit(t *testing.T) {
	f := newManualFileFixture(t, models.ArchiveImage)
	request := f.request(t)
	f.service.Repo.ArchiveJob = changingManualAdmission{ArchiveJobReaderWriter: f.service.Repo.ArchiveJob, mutate: func() {
		require.NoError(t, os.Rename(f.path, f.path+".original"))
		require.NoError(t, os.WriteFile(f.path, intakePNG(t), 0600))
	}}
	_, err := f.service.SubmitManualFile(t.Context(), request)
	require.ErrorIs(t, err, ingest.ErrManualFileChanged)
	_, err = f.service.ManualFileStatus(t.Context(), request.RequestUUID)
	require.ErrorIs(t, err, ingest.ErrNotFound, "failed pre-commit check must roll back the job and receipt")
}

func TestManualFilePortableRestoreKeepsMediaPolicyAndRequestRecovery(t *testing.T) {
	f := newManualFileFixture(t, models.ArchiveImage)
	f.policy(t, 0, map[string]models.MetadataMapping{})
	request := f.request(t)
	_, err := f.service.SubmitManualFile(t.Context(), request)
	require.NoError(t, err)
	worker := f.worker(t, func(ctx context.Context, _ ingest.FileWork, _ ingest.IntakePublicationResult, guard ingest.FileEffectGuard) error {
		return guard(ctx)
	})
	_, err = worker.ProcessNext(t.Context())
	require.NoError(t, err)
	completed := f.status(t, request.RequestUUID)
	require.True(t, completed.MediaIngested)
	queuedRequest := f.request(t)
	_, err = f.service.SubmitManualFile(t.Context(), queuedRequest)
	require.NoError(t, err)
	failedWorker := f.worker(t, func(context.Context, ingest.FileWork, ingest.IntakePublicationResult, ingest.FileEffectGuard) error {
		return ingest.ErrInvalid
	})
	_, err = failedWorker.ProcessNext(t.Context())
	require.NoError(t, err)
	queued := f.status(t, queuedRequest.RequestUUID)
	require.True(t, queued.RegistrationCommitted)
	require.Equal(t, "failed", queued.State)
	retryID := uuid.NewString()
	retry, err := f.service.RetryManualFile(t.Context(), queuedRequest.RequestUUID, queued.Revision, retryID)
	require.NoError(t, err)
	directory := t.TempDir()
	python := os.Getenv("PRODUCER_PYTHON")
	if python == "" {
		python, err = exec.LookPath("python3")
		require.NoError(t, err)
	}
	packagePath, err := filepath.Abs(filepath.Join("..", "..", "integrations", "archive", "src"))
	require.NoError(t, err)
	entries, err := os.ReadDir(filepath.Join(packagePath, "stash_archive"))
	require.NoError(t, err)
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".py" {
			_, err := os.ReadFile(filepath.Join(packagePath, "stash_archive", entry.Name()))
			require.NoError(t, err) // Interpreter sources participate in Go's cache.
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, python, "-c", manualFileRestoreScript, f.db.DatabasePath(), directory)
	command.Env = append(os.Environ(), "PYTHONPATH="+packagePath, "PYTHONDONTWRITEBYTECODE=1")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	restored := sqlite.NewDatabase()
	require.NoError(t, restored.Open(filepath.Join(directory, "restored", "library.sqlite")))
	t.Cleanup(func() { require.NoError(t, restored.Close()) })
	service := ingest.New(restored.Repository())
	for _, pair := range []struct {
		request ingest.ManualFileRequest
		status  *ingest.ManualFileStatus
	}{{request, completed}, {queuedRequest, queued}} {
		recovered, err := service.SubmitManualFile(t.Context(), pair.request)
		require.NoError(t, err)
		require.Equal(t, pair.status, recovered)
	}
	recoveredRetry, err := service.RetryManualFile(t.Context(), queuedRequest.RequestUUID, queued.Revision, retryID)
	require.NoError(t, err)
	require.Equal(t, retry, recoveredRetry)
	require.True(t, recoveredRetry.RegistrationCommitted)
	require.Equal(t, "filename", intakeField(t, service.Repo, completed.Publication.MediaUUID, "title").Origin)
}

const manualFileRestoreScript = `
from contextlib import closing
from pathlib import Path
import sys
from stash_archive.bundle import connect_readonly, export_archive, import_archive
from stash_archive.cli import list_records
database, destination = map(Path, sys.argv[1:3])
export_archive(database, destination / 'archive', reserve=0)
import_archive(destination / 'archive', destination / 'restored', reserve=0)
with closing(connect_readonly(database)) as original, closing(connect_readonly(destination / 'restored' / 'library.sqlite')) as restored:
    for table in ('archive_jobs', 'archive_job_submissions', 'archive_job_attempts',
                  'archive_entities', 'source_collections', 'source_collection_media_intake',
                  'metadata_field_decisions', 'metadata_field_heads', 'images', 'files'):
        assert original.execute('SELECT * FROM ' + table + ' ORDER BY 1').fetchall() == restored.execute('SELECT * FROM ' + table + ' ORDER BY 1').fetchall(), table
    for table in ('ingest_producers', 'ingest_receipts', 'source_accounts', 'source_posts', 'source_captures'):
        assert restored.execute('SELECT count(*) FROM ' + table).fetchone() == (0,), table
    assert restored.execute('PRAGMA foreign_key_check').fetchall() == []
    assert restored.execute('PRAGMA integrity_check').fetchone() == ('ok',)
    assert restored.execute('SELECT title FROM images').fetchall() == [('Purchased image',)]
assert len(list_records(destination / 'restored', 'archive_jobs', limit=10)['rows']) == 3
`
