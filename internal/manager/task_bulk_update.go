package manager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/bulkupdate"
	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin/common"
	"github.com/stashapp/stash/pkg/plugin/hook"
)

type BulkUpdateOperation = bulkupdate.Operation

type bulkReference struct {
	bulkupdate.Reference
	UUID string `json:"uuid"`
}

type bulkUpdateJob struct {
	repo         models.Repository
	services     bulkupdate.Services
	hookExecutor HookExecutor
	command      bulkupdate.Command
	id           string
	generation   int
	total        int
	kind         models.ArchiveEntityKind
	operation    bulkupdate.Operation
	references   []bulkReference
	notice       *common.HookContext

	// These exist only until admission. Execution uses the saved command and
	// targets, never the HTTP input, filter or caller's mutable operation.
	selector    bulkupdate.Selector
	hookType    hook.TriggerEnum
	input       interface{}
	inputFields []string
}

func (s *Manager) bulkServices() bulkupdate.Services {
	return bulkupdate.Services{Repository: s.Repository, Galleries: s.GalleryService, Groups: s.GroupService}
}

func (s *Manager) BulkUpdate(ctx context.Context, description string, selectTargets bulkupdate.Selector, operation BulkUpdateOperation,
	hookType hook.TriggerEnum, input interface{}, inputFields []string) (string, int, error) {
	command, err := bulkupdate.Encode(operation)
	if err != nil {
		return "", 0, err
	}
	j := &bulkUpdateJob{
		repo: s.Repository, services: s.bulkServices(), hookExecutor: s.HookExecutor,
		command: command, selector: selectTargets, hookType: hookType, input: input, inputFields: inputFields,
	}
	id, err := s.JobManager.Add(ctx, description, j)
	return id, j.total, err
}

func bulkHookType(kind models.ArchiveEntityKind) hook.TriggerEnum {
	switch kind {
	case models.ArchiveScene:
		return hook.SceneUpdatePost
	case models.ArchiveImage:
		return hook.ImageUpdatePost
	case models.ArchiveGallery:
		return hook.GalleryUpdatePost
	case models.ArchiveGroup:
		return hook.GroupUpdatePost
	case models.ArchivePerformer:
		return hook.PerformerUpdatePost
	case models.ArchiveStudio:
		return hook.StudioUpdatePost
	case models.ArchiveTag:
		return hook.TagUpdatePost
	case models.ArchiveSceneMarker:
		return hook.SceneMarkerUpdatePost
	default:
		return ""
	}
}

func (j *bulkUpdateJob) Admit(ctx context.Context, admitted job.Job) error {
	if j.selector == nil {
		return errors.New("bulk selection is required")
	}
	operation, err := bulkupdate.Decode(j.command, j.services)
	if err != nil {
		return err
	}
	kind, err := j.command.EntityKind()
	if err != nil {
		return err
	}
	if j.hookType != bulkHookType(kind) {
		return errors.New("bulk notification does not match command")
	}
	refs, err := bulkupdate.References(operation)
	if err != nil {
		return err
	}
	err = j.repo.WithTxn(ctx, func(ctx context.Context) error {
		ids, err := j.selector(ctx)
		if err != nil {
			return err
		}
		if len(ids) > bulkupdate.MaxTargets {
			return fmt.Errorf("bulk selection exceeds %d items", bulkupdate.MaxTargets)
		}
		targets := make([]models.LibraryBulkTarget, 0, len(ids))
		for _, id := range ids {
			entity, err := j.repo.ArchiveEntity.FindByLocalID(ctx, kind, id)
			if err != nil {
				return err
			}
			if entity == nil || entity.State != models.ArchiveEntityActive {
				return fmt.Errorf("selected %s %d no longer exists", kind, id)
			}
			targets = append(targets, models.LibraryBulkTarget{EntityID: id, EntityUUID: entity.UUID})
		}
		j.references = make([]bulkReference, 0, len(refs))
		for _, ref := range refs {
			entity, err := j.repo.ArchiveEntity.FindByLocalID(ctx, ref.Kind, ref.ID)
			if err != nil {
				return err
			}
			if entity == nil || entity.State != models.ArchiveEntityActive {
				return fmt.Errorf("referenced %s %d no longer exists", ref.Kind, ref.ID)
			}
			j.references = append(j.references, bulkReference{Reference: ref, UUID: entity.UUID})
		}
		referencesJSON, err := json.Marshal(j.references)
		if err != nil {
			return err
		}
		var noticeJSON []byte
		if config.GetBulkUpdateHooks() && j.hookExecutor != nil {
			notice, err := j.hookExecutor.PrepareDurablePostHook(ctx, j.hookType, j.input, j.inputFields)
			if err != nil {
				return fmt.Errorf("preparing bulk notification: %w", err)
			}
			noticeJSON, err = json.Marshal(notice)
			if err != nil {
				return err
			}
		}
		saved := models.LibraryBulkAdmission{
			ID: admitted.ID, Description: admitted.Description, AddedAt: admitted.AddTime, Generation: admitted.Generation,
			CommandVersion: j.command.Version, CommandKind: j.command.Kind, CommandJSON: j.command.Payload,
			NoticeJSON: noticeJSON, ReferencesJSON: referencesJSON, Targets: targets,
		}
		// Decode exactly what will be saved, so fresh and restored executions agree.
		if err := j.load(models.LibraryBulkJob{
			ID: saved.ID, Generation: saved.Generation, CommandVersion: saved.CommandVersion, CommandKind: saved.CommandKind,
			CommandJSON: saved.CommandJSON, NoticeJSON: saved.NoticeJSON, ReferencesJSON: saved.ReferencesJSON, Total: len(targets),
		}); err != nil {
			return err
		}
		return j.repo.LibraryBulkJob.Admit(ctx, saved)
	})
	j.selector, j.input, j.inputFields = nil, nil, nil
	return err
}

func decodeBulkRecord(data []byte, value any) error {
	if len(data) > models.MaxLibraryBulkBytes {
		return errors.New("saved bulk record exceeds 64 KiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("saved bulk record has trailing data")
	}
	return nil
}

func (j *bulkUpdateJob) load(saved models.LibraryBulkJob) error {
	j.id, j.generation, j.total = saved.ID, saved.Generation, saved.Total
	j.command = bulkupdate.Command{Version: saved.CommandVersion, Kind: saved.CommandKind, Payload: saved.CommandJSON}
	var err error
	j.operation, err = bulkupdate.Decode(j.command, j.services)
	if err != nil {
		return err
	}
	j.kind, err = j.command.EntityKind()
	if err != nil {
		return err
	}
	if err := decodeBulkRecord(saved.ReferencesJSON, &j.references); err != nil {
		return err
	}
	refs, err := bulkupdate.References(j.operation)
	if err != nil {
		return err
	}
	if len(refs) != len(j.references) {
		return errors.New("saved bulk relationship identities do not match the command")
	}
	for i, ref := range refs {
		id, err := uuid.Parse(j.references[i].UUID)
		if err != nil || id == uuid.Nil || id.String() != j.references[i].UUID || ref != j.references[i].Reference {
			return errors.New("invalid saved bulk relationship identity")
		}
	}
	j.notice = nil
	if saved.NoticeJSON != nil {
		var notice common.HookContext
		if err := decodeBulkRecord(saved.NoticeJSON, &notice); err != nil {
			return err
		}
		if notice.Type != string(bulkHookType(j.kind)) || notice.UUID != "" || notice.EventID != "" {
			return errors.New("invalid saved bulk notification")
		}
		j.notice = &notice
	}
	return nil
}

func (j *bulkUpdateJob) validateIdentity(ctx context.Context, kind models.ArchiveEntityKind, id int, publicID string) error {
	entity, err := j.repo.ArchiveEntity.Find(ctx, publicID)
	if err != nil {
		return err
	}
	if entity == nil || entity.State != models.ArchiveEntityActive || entity.Kind != kind || entity.LocalID == nil || *entity.LocalID != id {
		return fmt.Errorf("%s %s was removed or merged after this bulk edit was accepted", kind, publicID)
	}
	return nil
}

func pendingBulk(err error) error { return fmt.Errorf("%w: %v", job.ErrResumePending, err) }

func (j *bulkUpdateJob) Execute(ctx context.Context, progress *job.Progress) error {
	progress.SetTotal(j.total)
	var state *models.LibraryBulkJob
	if err := j.repo.WithReadTxn(ctx, func(ctx context.Context) error {
		var err error
		state, err = j.repo.LibraryBulkJob.Find(ctx, j.id)
		return err
	}); err != nil {
		return pendingBulk(err)
	}
	if state == nil || state.Generation != j.generation {
		return pendingBulk(models.ErrLibraryBulkInterrupted)
	}
	progress.SetProcessed(state.Processed)
	if j.total == 0 {
		progress.SetPercent(1)
	}
	for {
		if err := ctx.Err(); err != nil {
			return pendingBulk(err)
		}
		var target *models.LibraryBulkTarget
		var updateErr error
		processed := 0
		err := j.repo.WithTxn(ctx, func(ctx context.Context) error {
			var err error
			target, err = j.repo.LibraryBulkJob.Next(ctx, j.id, j.generation)
			if err != nil || target == nil || target.State == "NOTIFY" {
				return err
			}
			updateErr = j.validateIdentity(ctx, j.kind, target.EntityID, target.EntityUUID)
			for _, ref := range j.references {
				if updateErr != nil {
					break
				}
				updateErr = j.validateIdentity(ctx, ref.Kind, ref.ID, ref.UUID)
			}
			if updateErr == nil {
				updateErr = j.operation.Update(ctx, target.EntityID)
			}
			if updateErr != nil {
				return updateErr
			} // Roll back every part of this item's edit.
			processed, err = j.repo.LibraryBulkJob.Applied(ctx, j.id, j.generation, *target)
			return err
		})
		if ctx.Err() != nil {
			return pendingBulk(ctx.Err())
		}
		if err != nil {
			if updateErr == nil {
				return pendingBulk(err)
			} // Do not discard work on checkpoint failure.
			err = j.repo.WithTxn(ctx, func(ctx context.Context) error {
				var err error
				processed, err = j.repo.LibraryBulkJob.Failed(ctx, j.id, j.generation, *target, updateErr.Error())
				return err
			})
			if err != nil {
				return pendingBulk(err)
			}
		}
		if target == nil {
			return j.outcome(ctx)
		}
		if target.State == "NOTIFY" {
			if err := j.deliver(ctx, *target); err != nil {
				return pendingBulk(err)
			}
		} else {
			progress.SetProcessed(processed)
		}
	}
}

func (j *bulkUpdateJob) deliver(ctx context.Context, target models.LibraryBulkTarget) error {
	var failure *string
	if target.HookAttempts < models.MaxLibraryHookTries {
		if err := j.repo.WithTxn(ctx, func(ctx context.Context) error {
			return j.repo.LibraryBulkJob.StartNotification(ctx, j.id, j.generation, target)
		}); err != nil {
			return err
		}
		target.HookAttempts++
		err := errors.New("bulk notification executor is unavailable")
		if j.notice != nil && j.hookExecutor != nil {
			notice := *j.notice
			notice.ID, notice.UUID = target.EntityID, target.EntityUUID
			notice.EventID = uuid.NewSHA1(uuid.MustParse(j.id), []byte(fmt.Sprintf("update:%d", target.Ordinal))).String()
			err = j.hookExecutor.DeliverPreparedPostHook(ctx, notice)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			value := err.Error()
			failure = &value
		}
	} else {
		value := "notification retry limit reached after interrupted delivery"
		failure = &value
	}
	return j.repo.WithTxn(ctx, func(ctx context.Context) error {
		return j.repo.LibraryBulkJob.FinishNotification(ctx, j.id, j.generation, target, failure)
	})
}

func (j *bulkUpdateJob) outcome(ctx context.Context) error {
	var saved *models.LibraryBulkJob
	if err := j.repo.WithReadTxn(ctx, func(ctx context.Context) error {
		var err error
		saved, err = j.repo.LibraryBulkJob.Find(ctx, j.id)
		return err
	}); err != nil {
		return pendingBulk(err)
	}
	if saved == nil || saved.Generation != j.generation || saved.Processed != saved.Total {
		return pendingBulk(errors.New("bulk work is incomplete or superseded"))
	}
	if saved.Failed == 0 && saved.NotificationFailed == 0 {
		return nil
	}
	var samples []models.LibraryBulkError
	if err := json.Unmarshal(saved.ErrorsJSON, &samples); err != nil {
		return pendingBulk(err)
	}
	messages := make([]string, 0, len(samples))
	for _, sample := range samples {
		messages = append(messages, fmt.Sprintf("%s (%s): %s", sample.EntityUUID, sample.Stage, sample.Message))
	}
	return fmt.Errorf("%d of %d items failed; %d notification failures: %s", saved.Failed, saved.Total, saved.NotificationFailed, strings.Join(messages, "; "))
}

func (s *Manager) restoreBulkUpdates(ctx context.Context, history job.History) ([]job.Restoration, error) {
	var saved []models.LibraryBulkJob
	if err := s.Repository.WithReadTxn(ctx, func(ctx context.Context) error {
		var err error
		saved, err = s.Repository.LibraryBulkJob.Pending(ctx)
		return err
	}); err != nil {
		return nil, err
	}
	ret := make([]job.Restoration, 0, len(saved))
	for _, pending := range saved {
		status, err := history.Find(ctx, pending.ID)
		if err != nil {
			return nil, err
		}
		if status == nil || status.Generation != pending.Generation || status.Status != job.StatusReady {
			return nil, errors.New("pending bulk job has inconsistent history")
		}
		executor := &bulkUpdateJob{repo: s.Repository, services: s.bulkServices(), hookExecutor: s.HookExecutor}
		if err := executor.load(pending); err != nil {
			// An unrecognized command never executes or blocks the rest of the queue.
			// Retain its explicit failure in bounded ordinary job history.
			now := time.Now()
			message := fmt.Sprintf("Cannot resume saved bulk edit: %v", err)
			status.Status, status.EndTime, status.Error = job.StatusFailed, &now, &message
			if err := history.Save(ctx, *status); err != nil {
				return nil, err
			}
			continue
		}
		ret = append(ret, job.Restoration{Job: *status, Executor: executor})
	}
	return ret, nil
}
