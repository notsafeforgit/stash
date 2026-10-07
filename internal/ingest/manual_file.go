package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/txn"
)

var ErrManualFileChanged = errors.New("local file import preview changed")

// ManualFileInput refers to an existing bound collection, including a folder or
// manual batch with a shared performer policy. It needs no producer or source.
type ManualFileInput struct {
	CollectionUUID string                   `json:"collection_uuid"`
	RelativePath   string                   `json:"relative_path"`
	MediaKind      models.ArchiveEntityKind `json:"media_kind"`
}

type ManualFileRequest struct {
	ManualFileInput
	RequestUUID string `json:"request_uuid"`
	Signature   string `json:"signature"`
}

type ManualFilePreview struct {
	ManualFileInput
	CollectionRevision int       `json:"collection_revision"`
	PolicyRevision     int       `json:"policy_revision"`
	RootUUID           string    `json:"root_uuid"`
	RootRevision       int       `json:"root_revision"`
	Filename           string    `json:"filename"`
	Size               int64     `json:"size"`
	ModifiedAt         time.Time `json:"modified_at"`
	ExistingFileUUID   string    `json:"existing_file_uuid,omitempty"`
	FileSignature      string    `json:"file_signature,omitempty"`
	Signature          string    `json:"signature"`
}

// ManualFileWork is server-created admission evidence within the immutable job
// arguments. The archive job submission is its durable application receipt.
// Producer file events cannot supply this variant of work.
type ManualFileWork struct {
	Resume       *ManualFileResume           `json:"resume,omitempty"`
	Request      ManualFileRequest           `json:"request"`
	RootRevision int                         `json:"root_revision"`
	Inspection   archive.MediaFileInspection `json:"inspection"`
}

type ManualFileStatus struct {
	ResumeFromJobUUID  string `json:"resume_from_job_uuid,omitempty"`
	ResumeFromRevision int64  `json:"resume_from_revision,omitempty"`
	RequestUUID        string `json:"request_uuid"`
	JobUUID            string `json:"job_uuid"`
	ManualFileInput
	Signature             string                   `json:"signature"`
	State                 string                   `json:"state"`
	Revision              int64                    `json:"revision"`
	Attempts              int64                    `json:"attempts"`
	MaxAttempts           int                      `json:"max_attempts"`
	AvailableAt           time.Time                `json:"available_at"`
	RegistrationCommitted bool                     `json:"registration_committed"`
	MediaIngested         bool                     `json:"media_ingested"`
	Publication           *IntakePublicationResult `json:"publication,omitempty"`
	ErrorCode             string                   `json:"error_code,omitempty"`
	CreatedAt             time.Time                `json:"created_at"`
	UpdatedAt             time.Time                `json:"updated_at"`
}

type manualFilePlan struct {
	Preview    ManualFilePreview
	Target     FileTarget
	Inspection archive.MediaFileInspection
}

func validManualFileInput(input ManualFileInput) bool {
	return ValidUUID(input.CollectionUUID) && archive.ValidRootRelativePath(input.RelativePath, false) &&
		!strings.HasSuffix(strings.ToLower(input.RelativePath), ".part") && (input.MediaKind == models.ArchiveScene || input.MediaKind == models.ArchiveImage)
}

func (s *Service) manualFilePlan(ctx context.Context, input ManualFileInput) (*manualFilePlan, error) {
	if !validManualFileInput(input) {
		return nil, ErrInvalid
	}
	collection, err := s.Repo.SourceCollection.Find(ctx, input.CollectionUUID)
	if err != nil {
		return nil, err
	}
	if collection == nil {
		return nil, ErrNotFound
	}
	if collection.RootUUID == nil || (collection.Kind != "directory" && collection.Kind != "manual_batch") {
		return nil, ErrDefinition
	}
	target := FileTarget{RootUUID: *collection.RootUUID, RelativePath: input.RelativePath}
	if !collectionIncludesFile(collection, target) {
		return nil, ErrDefinition
	}
	root, err := s.Repo.MediaRoot.Find(ctx, target.RootUUID)
	if err != nil {
		return nil, err
	}
	if root == nil || root.State != "active" || root.Binding == nil {
		return nil, ErrDefinition
	}
	inspection, err := archive.InspectMediaFile(ctx, *root, input.RelativePath)
	if err != nil {
		return nil, err
	}
	if inspection.Snapshot.Size < 1 {
		return nil, ErrInvalid
	}
	sensitive, err := fsutil.IsFsPathCaseSensitive(filepath.Join(root.Binding.Path, filepath.FromSlash(input.RelativePath)))
	if err != nil {
		return nil, err
	}
	pinned, err := CaptureFileTarget(ctx, s.Repo, *root, input.RelativePath, sensitive)
	if err != nil {
		return nil, err
	}
	// A reviewed import cannot resurrect a path removed from the archive. An
	// explicit restoration must establish a new file lifetime first.
	if pinned.FileUUID == "" && pinned.PathFence.Revision != 0 {
		return nil, models.ErrFilePathChanged
	}
	policy, err := s.Repo.MetadataPolicy.Find(ctx, input.CollectionUUID)
	if err != nil {
		return nil, err
	}
	policyRevision := 0
	if policy != nil {
		if policy.CollectionRevision != collection.Revision {
			return nil, models.ErrMetadataPolicyConflict
		}
		policyRevision = policy.Revision
	}
	plan := &manualFilePlan{Target: *pinned, Inspection: *inspection, Preview: ManualFilePreview{
		ManualFileInput: input, CollectionRevision: collection.Revision, PolicyRevision: policyRevision,
		RootUUID: root.UUID, RootRevision: root.Revision, Filename: path.Base(input.RelativePath),
		Size: inspection.Snapshot.Size, ModifiedAt: inspection.Snapshot.ModifiedAt, ExistingFileUUID: pinned.FileUUID,
	}}
	// Folder discovery needs a filesystem version that remains stable when
	// registration creates or updates the database's file identity. Hash the
	// confined inspection, including ctime/inode (or full digest fallback),
	// without exposing those server-local details. Apply still requires the
	// complete preview signature and all its database/policy guards.
	fileVersion, err := json.Marshal(struct {
		RootUUID     string
		RootRevision int
		Path         string
		Inspection   archive.MediaFileInspection
	}{root.UUID, root.Revision, input.RelativePath, *inspection})
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	plan.Preview.Signature = Digest(raw)
	// This output-only hint must not change the original admission signature:
	// already saved native requests still need to validate after an upgrade.
	plan.Preview.FileSignature = Digest(fileVersion)
	return plan, nil
}

// PreviewManualFile checks the scoped path and file identity without creating a
// file, media item, source record or job. Content matching and probing happen in
// the worker; an empty ExistingFileUUID does not promise a new library item.
func (s *Service) PreviewManualFile(ctx context.Context, input ManualFileInput) (*ManualFilePreview, error) {
	var plan *manualFilePlan
	err := s.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		var err error
		plan, err = s.manualFilePlan(ctx, input)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &plan.Preview, nil
}

func validFileWork(work FileWork) bool {
	if work.Size <= 0 {
		return false
	}
	if work.Version == 1 {
		return work.Manual == nil && ValidUUID(work.ProducerUUID) && ValidUUID(work.EventUUID) && archive.ValidSHA256(work.SHA256)
	}
	m := work.Manual
	if work.Version != 2 || m == nil || work.ProducerUUID != "" || work.EventUUID != "" || !work.ObservedAt.IsZero() ||
		!validManualFileInput(m.Request.ManualFileInput) || !ValidUUID(m.Request.RequestUUID) || !archive.ValidSHA256(m.Request.Signature) ||
		m.RootRevision < 1 || m.Inspection.Snapshot.Identity == "" || m.Inspection.Snapshot.Size != work.Size ||
		(m.Inspection.Snapshot.ChangeToken == "" && !archive.ValidSHA256(m.Inspection.SHA256)) ||
		(m.Inspection.SHA256 != "" && !archive.ValidSHA256(m.Inspection.SHA256)) || work.SHA256 != m.Inspection.SHA256 {
		return false
	}
	if !validManualFileResume(m.Resume) {
		return false
	}
	p := work.Publication
	return p.Source == nil && p.UUID == manualIntakeUUID(m.Request.RequestUUID) && p.Kind == m.Request.MediaKind &&
		p.CollectionUUID == m.Request.CollectionUUID && p.Target.RelativePath == m.Request.RelativePath
}

func manualIntakeUUID(request string) string {
	return uuid.NewSHA1(uuid.MustParse(request), []byte("manual-file-intake")).String()
}

func decodeManualFileWork(current *models.ArchiveJob) (*FileWork, error) {
	if current == nil || current.Kind != models.ArchiveJobVerifyMedia {
		return nil, ErrNotFound
	}
	var work FileWork
	if err := StrictJSON(current.Arguments, 262144, &work); err != nil || !validFileWork(work) || work.Manual == nil {
		return nil, ErrNotFound
	}
	return &work, nil
}

func manualFileStatus(current *models.ArchiveJob, request string) (*ManualFileStatus, error) {
	work, err := decodeManualFileWork(current)
	if err != nil {
		return nil, err
	}
	progress, err := fileProgressForWork(current, *work)
	if err != nil {
		return nil, err
	}
	var completion fileCompletion
	if err := StrictJSON(current.Result, 16384, &completion); err != nil || completion.MediaIngested != (current.State == "succeeded") ||
		(completion.MediaIngested && (!completion.RegistrationCommitted || progress.Publication == nil || !reflect.DeepEqual(completion.Publication, progress.Publication))) {
		return nil, ErrInvalid
	}
	result := &ManualFileStatus{RequestUUID: request, JobUUID: current.UUID, ManualFileInput: work.Manual.Request.ManualFileInput,
		Signature: work.Manual.Request.Signature, State: current.State, Revision: current.Revision, Attempts: current.Fence,
		MaxAttempts: current.MaxAttempts, AvailableAt: current.AvailableAt, RegistrationCommitted: progress.Publication != nil,
		MediaIngested: completion.MediaIngested, Publication: progress.Publication, ErrorCode: current.ErrorCode,
		CreatedAt: current.CreatedAt, UpdatedAt: current.UpdatedAt}
	if work.Manual.Resume != nil {
		result.ResumeFromJobUUID, result.ResumeFromRevision = work.Manual.Resume.FromJobUUID, work.Manual.Resume.FromRevision
	}
	return result, nil
}

// SubmitManualFile is for application-authenticated callers. The exact saved
// request is recovered before looking at today's file or policy, including after
// the original response was lost and the path has subsequently disappeared.
func (s *Service) SubmitManualFile(ctx context.Context, input ManualFileRequest) (*ManualFileStatus, error) {
	if !validManualFileInput(input.ManualFileInput) || !ValidUUID(input.RequestUUID) || !archive.ValidSHA256(input.Signature) {
		return nil, ErrInvalid
	}
	var current *models.ArchiveJob
	err := s.Repo.WithTxn(ctx, func(ctx context.Context) error {
		var err error
		current, err = s.Repo.ArchiveJob.FindSubmission(ctx, input.RequestUUID)
		if err != nil {
			return err
		}
		if current != nil {
			work, err := decodeManualFileWork(current)
			if err != nil || work.Manual.Request != input {
				return models.ErrArchiveJobConflict
			}
			return nil
		}
		plan, err := s.manualFilePlan(ctx, input.ManualFileInput)
		if err != nil {
			return err
		}
		if plan.Preview.Signature != input.Signature {
			return ErrManualFileChanged
		}
		work := FileWork{Version: 2, Size: plan.Inspection.Snapshot.Size, SHA256: plan.Inspection.SHA256,
			Manual: &ManualFileWork{Request: input, RootRevision: plan.Preview.RootRevision, Inspection: plan.Inspection},
			Publication: IntakePublication{UUID: manualIntakeUUID(input.RequestUUID), CollectionUUID: input.CollectionUUID,
				CollectionRevision: plan.Preview.CollectionRevision, PolicyRevision: plan.Preview.PolicyRevision, Target: plan.Target, Kind: input.MediaKind}}
		args, err := json.Marshal(work)
		if err != nil {
			return err
		}
		resource := plan.Target.PathFence.Path
		if !plan.Target.PathFence.CaseSensitive {
			resource = strings.ToLower(resource)
		}
		current, err = s.Repo.ArchiveJob.Submit(ctx, models.ArchiveJobSubmission{RequestUUID: input.RequestUUID,
			Kind: models.ArchiveJobVerifyMedia, WorkKey: Digest(args), ResourceKey: Digest([]byte(resource)), Arguments: args, MaxAttempts: 8}, time.Now(), 10000)
		if err != nil {
			return err
		}
		txn.AddPreCommitHook(ctx, func(ctx context.Context) error {
			latest, err := s.manualFilePlan(ctx, input.ManualFileInput)
			if err != nil {
				return err
			}
			if latest.Preview.Signature != input.Signature {
				return ErrManualFileChanged
			}
			return nil
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return manualFileStatus(current, input.RequestUUID)
}

func (s *Service) ManualFileStatus(ctx context.Context, request string) (*ManualFileStatus, error) {
	if !ValidUUID(request) {
		return nil, ErrInvalid
	}
	var current *models.ArchiveJob
	err := s.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		var err error
		current, err = s.Repo.ArchiveJob.FindSubmission(ctx, request)
		return err
	})
	if err != nil {
		return nil, err
	}
	return manualFileStatus(current, request)
}

// CancelManualFile stops uncommitted work and remaining effects. A committed
// media registration is retained in status and is never undone by cancellation.
func (s *Service) CancelManualFile(ctx context.Context, request string, revision int64) (*ManualFileStatus, error) {
	if !ValidUUID(request) || revision < 1 {
		return nil, ErrInvalid
	}
	var current *models.ArchiveJob
	err := s.Repo.WithTxn(ctx, func(ctx context.Context) error {
		var err error
		current, err = s.Repo.ArchiveJob.FindSubmission(ctx, request)
		if err != nil {
			return err
		}
		if _, err := decodeManualFileWork(current); err != nil {
			return err
		}
		current, err = s.Repo.ArchiveJob.Cancel(ctx, current.UUID, revision, time.Now())
		return err
	})
	if err != nil {
		return nil, err
	}
	return manualFileStatus(current, request)
}

// Before first publication, require precisely the reviewed collection, root and
// metadata policy revisions. Once registration commits, later metadata edits do
// not erase that result or prevent delivery of its remaining notifications.
func (s *Service) validateManualFilePublication(ctx context.Context, work FileWork) error {
	if work.Manual == nil {
		return nil
	}
	collection, err := s.Repo.SourceCollection.Find(ctx, work.Publication.CollectionUUID)
	if err != nil {
		return err
	}
	root, err := s.Repo.MediaRoot.Find(ctx, work.Publication.Target.RootUUID)
	if err != nil {
		return err
	}
	policy, err := s.Repo.MetadataPolicy.Find(ctx, work.Publication.CollectionUUID)
	if err != nil {
		return err
	}
	policyRevision := 0
	if policy != nil {
		policyRevision = policy.Revision
	}
	if collection == nil || root == nil || collection.Revision != work.Publication.CollectionRevision ||
		root.Revision != work.Manual.RootRevision || policyRevision != work.Publication.PolicyRevision {
		return ErrManualFileChanged
	}
	return nil
}
