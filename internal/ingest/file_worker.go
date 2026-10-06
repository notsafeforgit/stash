package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/file"
	"github.com/stashapp/stash/pkg/file/video"
	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

// FileWorker keeps hashing outside SQLite transactions, renews ownership while
// inspecting, and checkpoints publication before restartable post-commit work.
// Effects must be idempotent and must respect ctx cancellation; retries can
// repeat them. The stable job/event identities are available in work.
type FileWorker struct {
	Service       *Service
	Durable       *job.Durable
	Scanner       func(models.ArchiveEntityKind) *file.Scanner
	Effects       func(context.Context, FileWork, IntakePublicationResult, FileEffectGuard) error
	OwnerUUID     string
	LeaseDuration time.Duration
	PollInterval  time.Duration
	RetryDelay    time.Duration
}

// FileEffectGuard checks the accepted scope, file lifetime and worker lease.
// Effects call it before publishing artifacts or notifying plugins. It works
// inside a managed transaction or opens a read transaction when called outside.
type FileEffectGuard func(context.Context) error

func NewFileWorker(service *Service, scanner func(models.ArchiveEntityKind) *file.Scanner, effects func(context.Context, FileWork, IntakePublicationResult, FileEffectGuard) error) *FileWorker {
	return &FileWorker{Service: service, Durable: job.NewDurable(service.Repo), Scanner: scanner, Effects: effects,
		OwnerUUID: uuid.NewString(), LeaseDuration: time.Minute, PollInterval: time.Second, RetryDelay: 30 * time.Second}
}

type fileProgress struct {
	Version     int                      `json:"version"`
	Publication *IntakePublicationResult `json:"publication,omitempty"`
}

type fileCompletion struct {
	RegistrationCommitted bool                     `json:"registration_committed"`
	MediaIngested         bool                     `json:"media_ingested"`
	Publication           *IntakePublicationResult `json:"publication,omitempty"`
}

// Run does not start implicitly on submission. The application owns this
// context and waits for shutdown before closing its database.
func (w *FileWorker) Run(ctx context.Context) error {
	if w.PollInterval <= 0 {
		return ErrInvalid
	}
	for {
		processed, err := w.ProcessNext(ctx)
		if err != nil {
			return err
		}
		if processed {
			continue
		}
		timer := time.NewTimer(w.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (w *FileWorker) ProcessNext(ctx context.Context) (bool, error) {
	if w.Service == nil || w.Durable == nil || w.Scanner == nil || w.Effects == nil || !ValidUUID(w.OwnerUUID) || w.LeaseDuration < 5*time.Second || w.LeaseDuration > 15*time.Minute || w.RetryDelay <= 0 {
		return false, ErrInvalid
	}
	claimed, err := w.Durable.Claim(ctx, models.ArchiveJobVerifyMedia, w.OwnerUUID, w.LeaseDuration)
	if err != nil || claimed == nil {
		return false, err
	}
	return true, w.process(ctx, claimed)
}

func (w *FileWorker) process(ctx context.Context, claimed *models.ArchiveJob) error {
	var work FileWork
	if err := StrictJSON(claimed.Arguments, 262144, &work); err != nil || !validFileWork(work) {
		return w.fail(ctx, claimed, ErrInvalid)
	}
	var progress fileProgress
	if err := StrictJSON(claimed.Progress, 16384, &progress); err != nil || (progress.Version != 0 && progress.Version != 1) || (progress.Version == 0 && progress.Publication != nil) {
		return w.fail(ctx, claimed, ErrInvalid)
	}
	root, err := w.authorize(ctx, claimed, work)
	if err != nil {
		return w.fail(ctx, claimed, err)
	}
	if work.Manual != nil && progress.Publication == nil {
		if err := w.Service.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
			return w.Service.validateManualFilePublication(ctx, work)
		}); err != nil {
			return w.fail(ctx, claimed, err)
		}
	}
	workCtx, cancelWork := context.WithCancelCause(ctx)
	defer cancelWork(nil)
	heartbeatCtx, stopHeartbeat := context.WithCancel(workCtx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(w.LeaseDuration / 3)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				if _, err := w.Durable.Renew(heartbeatCtx, claimed.Lease(), w.LeaseDuration); err != nil {
					if heartbeatCtx.Err() == nil {
						cancelWork(err)
					}
					return
				}
			}
		}
	}()
	defer func() { stopHeartbeat(); <-done }()
	prepared, err := PrepareMedia(workCtx, *root, work.Publication.Target.RelativePath, &work.Size, work.SHA256, w.Scanner(work.Publication.Kind))
	if err != nil {
		if workCtx.Err() != nil {
			return context.Cause(workCtx)
		}
		return w.fail(ctx, claimed, err)
	}
	defer prepared.Close()
	if work.Manual != nil && !work.Manual.Inspection.Matches(prepared.Snapshot(), prepared.SHA256()) {
		return w.fail(ctx, claimed, archive.ErrMediaFileChanged)
	}
	if progress.Publication == nil {
		_, err := w.Durable.Checkpoint(workCtx, claimed.Lease(), func(ctx context.Context, _ *models.ArchiveJob) (json.RawMessage, error) {
			if _, err := w.authorizeInTxn(ctx, claimed, work); err != nil {
				return nil, err
			}
			if err := w.Service.validateManualFilePublication(ctx, work); err != nil {
				return nil, err
			}
			if work.Manual != nil {
				txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
					return w.Service.validateManualFilePublication(ctx, work)
				})
			}
			published, err := prepared.PublishIntake(ctx, w.Service.Repo, work.Publication)
			if err != nil {
				return nil, err
			}
			progress = fileProgress{Version: 1, Publication: &published.Result}
			return json.Marshal(progress)
		})
		if err != nil {
			return w.fail(ctx, claimed, err)
		}
	}
	if err := w.validatePublished(workCtx, claimed, work, prepared, *progress.Publication); err != nil {
		return w.fail(ctx, claimed, err)
	}
	guard := func(ctx context.Context) error {
		check := func(ctx context.Context) error {
			if _, err := w.Service.Repo.ArchiveJob.CheckLease(ctx, claimed.Lease(), w.Durable.Now()); err != nil {
				return err
			}
			return w.validatePublishedInTxn(ctx, claimed, work, prepared, *progress.Publication)
		}
		if txn.HasHooks(ctx) {
			return check(ctx)
		}
		return w.Service.Repo.WithReadTxn(ctx, check)
	}
	if err := guard(workCtx); err != nil {
		return w.fail(ctx, claimed, err)
	}
	if err := w.Effects(workCtx, work, *progress.Publication, guard); err != nil {
		if workCtx.Err() != nil {
			return context.Cause(workCtx)
		}
		return w.fail(ctx, claimed, err)
	}
	stopHeartbeat()
	<-done
	if workCtx.Err() != nil {
		return context.Cause(workCtx)
	}
	if _, err := w.Durable.Renew(ctx, claimed.Lease(), w.LeaseDuration); err != nil {
		return err
	}
	_, err = w.Durable.Publish(ctx, claimed.Lease(), func(ctx context.Context, _ *models.ArchiveJob) (models.ArchiveJobOutcome, error) {
		if err := w.validatePublishedInTxn(ctx, claimed, work, prepared, *progress.Publication); err != nil {
			return models.ArchiveJobOutcome{}, err
		}
		txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
			return w.validatePublishedInTxn(ctx, claimed, work, prepared, *progress.Publication)
		})
		result, err := json.Marshal(fileCompletion{RegistrationCommitted: true, MediaIngested: true, Publication: progress.Publication})
		return models.ArchiveJobOutcome{State: "succeeded", Result: result}, err
	})
	if err != nil {
		return w.fail(ctx, claimed, err)
	}
	return nil
}

func (w *FileWorker) authorize(ctx context.Context, claimed *models.ArchiveJob, work FileWork) (*models.MediaRoot, error) {
	var root *models.MediaRoot
	err := w.Service.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		var err error
		root, err = w.authorizeInTxn(ctx, claimed, work)
		return err
	})
	return root, err
}

func (w *FileWorker) authorizeInTxn(ctx context.Context, claimed *models.ArchiveJob, work FileWork) (*models.MediaRoot, error) {
	repo := w.Service.Repo
	if work.Manual != nil {
		accepted, err := repo.ArchiveJob.FindSubmission(ctx, work.Manual.Request.RequestUUID)
		if err != nil {
			return nil, err
		}
		if accepted == nil || accepted.UUID != claimed.UUID {
			return nil, ErrForbidden
		}
	} else if err := w.authorizeProducerFile(ctx, claimed, work); err != nil {
		return nil, err
	}
	if err := validatePublicationCollection(ctx, repo, work.Publication); err != nil {
		return nil, err
	}
	root, err := repo.MediaRoot.Find(ctx, work.Publication.Target.RootUUID)
	if err != nil {
		return nil, err
	}
	if root == nil || root.State != "active" || root.Binding == nil || filepath.Join(root.Binding.Path, filepath.FromSlash(work.Publication.Target.RelativePath)) != work.Publication.Target.PathFence.Path {
		return nil, ErrDefinition
	}
	return root, nil
}

func (w *FileWorker) authorizeProducerFile(ctx context.Context, claimed *models.ArchiveJob, work FileWork) error {
	repo := w.Service.Repo
	receipt, err := repo.Ingest.FindReceipt(ctx, work.ProducerUUID, work.EventUUID)
	if err != nil {
		return err
	}
	if receipt == nil || receipt.JobUUID != claimed.UUID || receipt.Kind != "file.completed" || receipt.CollectionUUID != work.Publication.CollectionUUID ||
		receipt.CollectionRevision != work.Publication.CollectionRevision || receipt.RootUUID == nil || *receipt.RootUUID != work.Publication.Target.RootUUID {
		return ErrForbidden
	}
	if (work.Publication.Source == nil && receipt.CaptureUUID != "") || (work.Publication.Source != nil && work.Publication.Source.CaptureUUID != receipt.CaptureUUID) {
		return ErrForbidden
	}
	if receipt.PostUUID != "" {
		post, err := repo.SourceEvidence.FindPost(ctx, receipt.PostUUID)
		if err != nil {
			return err
		}
		if post == nil || post.State == "forgotten" {
			return models.ErrSourcePostForgotten
		}
	}
	return nil
}

func (w *FileWorker) validatePublished(ctx context.Context, claimed *models.ArchiveJob, work FileWork, prepared *PreparedMedia, published IntakePublicationResult) error {
	return w.Service.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		return w.validatePublishedInTxn(ctx, claimed, work, prepared, published)
	})
}

func (w *FileWorker) validatePublishedInTxn(ctx context.Context, claimed *models.ArchiveJob, work FileWork, prepared *PreparedMedia, published IntakePublicationResult) error {
	repo := w.Service.Repo
	root, err := w.authorizeInTxn(ctx, claimed, work)
	if err != nil {
		return err
	}
	if err := prepared.Revalidate(ctx, *root); err != nil {
		return err
	}
	if err := repo.FilePath.Check(ctx, work.Publication.Target.PathFence); err != nil {
		return err
	}
	file, err := repo.ArchiveEntity.Resolve(ctx, published.FileUUID)
	if err != nil {
		return err
	}
	media, err := repo.ArchiveEntity.Resolve(ctx, published.MediaUUID)
	if err != nil {
		return err
	}
	if file == nil || file.Kind != models.ArchiveFile || file.State != models.ArchiveEntityActive || media == nil || media.State != models.ArchiveEntityActive || media.Kind != published.MediaKind || media.Kind != work.Publication.Kind {
		return models.ErrFileGenerationConflict
	}
	proof, err := repo.FileContent.Current(ctx, file.UUID)
	if err != nil {
		return err
	}
	snapshot := prepared.Snapshot()
	if proof == nil || proof.RootUUID != root.UUID || proof.Generation != published.Generation || proof.Content.UUID != published.ContentUUID || proof.Content.SHA256 != prepared.SHA256() ||
		proof.Content.Size != work.Size || proof.Snapshot.Identity != snapshot.Identity || proof.Snapshot.ChangeToken != snapshot.ChangeToken || !proof.Snapshot.ModifiedAt.Equal(snapshot.ModifiedAt) {
		return models.ErrFileGenerationConflict
	}
	if err := prepared.verified.RevalidateAlias(ctx, *root, proof.RelativePath); err != nil {
		return err
	}
	linked, err := repo.FileContent.HasOwner(ctx, file.UUID, media.UUID)
	if err != nil {
		return err
	}
	if !linked {
		return models.ErrArchiveIdentityConflict
	}
	return nil
}

func (w *FileWorker) fail(ctx context.Context, claimed *models.ArchiveJob, cause error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(cause, models.ErrArchiveJobLease) || errors.Is(cause, models.ErrArchiveJobConflict) {
		return cause
	}
	code, permanent := fileFailure(cause)
	outcome := models.ArchiveJobOutcome{State: "failed", ErrorCode: code, Result: json.RawMessage(`{"media_ingested":false}`)}
	if !permanent {
		outcome.State, outcome.RetryAt = "retry", w.Durable.Now().Add(w.RetryDelay)
	}
	_, err := w.Durable.Publish(ctx, claimed.Lease(), func(_ context.Context, current *models.ArchiveJob) (models.ArchiveJobOutcome, error) {
		var progress fileProgress
		if err := StrictJSON(current.Progress, 16384, &progress); err != nil {
			return models.ArchiveJobOutcome{}, err
		}
		var err error
		outcome.Result, err = json.Marshal(fileCompletion{RegistrationCommitted: progress.Publication != nil, Publication: progress.Publication})
		return outcome, err
	})
	return err
}

func fileFailure(err error) (string, bool) {
	switch {
	case errors.Is(err, ErrInvalid), errors.Is(err, ErrUnsupported), errors.Is(err, video.ErrNoVideoStream):
		return "invalid_file_work", true
	case errors.Is(err, ErrForbidden), errors.Is(err, ErrDefinition), errors.Is(err, ErrManualFileChanged):
		return "file_scope_changed", true
	case errors.Is(err, archive.ErrMediaFileDigest):
		return "file_digest_mismatch", true
	case errors.Is(err, archive.ErrMediaFileChanged), errors.Is(err, models.ErrFileGenerationConflict), errors.Is(err, models.ErrFilePathChanged), errors.Is(err, models.ErrFileContentConflict):
		return "file_changed", true
	case errors.Is(err, ErrAmbiguousMedia), errors.Is(err, ErrMediaKindConflict), errors.Is(err, models.ErrSourceAttachmentConflict), errors.Is(err, models.ErrSourcePostMediaConflict), errors.Is(err, models.ErrArchiveIdentityConflict), errors.Is(err, models.ErrSourcePostForgotten):
		return "file_association_review", true
	default:
		return "file_processing_unavailable", false
	}
}
