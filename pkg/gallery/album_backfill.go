package gallery

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/models"
)

var (
	ErrAlbumWorkInvalid  = errors.New("invalid source album work")
	ErrAlbumWorkNotFound = errors.New("source album work not found")
)

type AlbumBackfillRequest struct {
	RequestUUID string `json:"request_uuid"`
	PostUUID    string `json:"post_uuid"`
	Policy      string `json:"policy"`
	Signature   string `json:"signature"`
}

// Publication is the durable result of one committed domain transaction. It
// remains true if later hook delivery fails or the owner cancels pending work.
// EventUUID belongs to the original publication, including on explicit retry.
type AlbumPublication struct {
	EventUUID   string `json:"event_uuid"`
	PostUUID    string `json:"post_uuid"`
	GalleryUUID string `json:"gallery_uuid,omitempty"`
	Action      string `json:"action"`
	Created     bool   `json:"created"`
	Selected    int    `json:"selected"`
	Review      int    `json:"review"`
	Unavailable int    `json:"unavailable"`
	Added       int    `json:"added"`
	Removed     int    `json:"removed"`
}

func (p AlbumPublication) ChangedGallery() bool { return p.Created || p.Added > 0 || p.Removed > 0 }

type albumWork struct {
	Version           int               `json:"version"`
	PostUUID          string            `json:"post_uuid"`
	Policy            string            `json:"policy"`
	Signature         string            `json:"signature"`
	ResumeFromJobUUID string            `json:"resume_from_job_uuid,omitempty"`
	ResumePublication *AlbumPublication `json:"resume_publication,omitempty"`
}

type albumProgress struct {
	Version     int               `json:"version"`
	Publication *AlbumPublication `json:"publication,omitempty"`
}

type albumOutcome struct {
	HooksFinished bool `json:"hooks_finished"`
}

type AlbumBackfillStatus struct {
	JobUUID              string            `json:"job_uuid"`
	Sequence             int64             `json:"sequence"`
	PostUUID             string            `json:"post_uuid"`
	Policy               string            `json:"policy"`
	Signature            string            `json:"signature"`
	State                string            `json:"state"`
	Revision             int64             `json:"revision"`
	Attempts             int64             `json:"attempts"`
	MaxAttempts          int               `json:"max_attempts"`
	AvailableAt          time.Time         `json:"available_at"`
	PublicationCommitted bool              `json:"publication_committed"`
	Publication          *AlbumPublication `json:"publication,omitempty"`
	HooksFinished        bool              `json:"hooks_finished"`
	ErrorCode            string            `json:"error_code,omitempty"`
	ResumeFromJobUUID    string            `json:"resume_from_job_uuid,omitempty"`
	CreatedAt            time.Time         `json:"created_at"`
	UpdatedAt            time.Time         `json:"updated_at"`
}

// AlbumBackfill admits application-authorized work. Producer tokens cannot
// request historical matching, change its policy or resume library operations.
type AlbumBackfill struct{ Durable *job.Durable }

func NewAlbumBackfill(repo models.Repository) *AlbumBackfill {
	return &AlbumBackfill{Durable: job.NewDurable(repo)}
}

func albumUUID(value string) bool {
	id, err := uuid.Parse(value)
	return err == nil && id != uuid.Nil && id.String() == value
}

func albumDigest(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}
func albumResource(post string) string { return albumDigest([]byte("source-post-album\x00" + post)) }

func decodeAlbumJSON(raw json.RawMessage, limit int, target any) error {
	if _, err := archive.DecodeJSONObject(raw, limit); err != nil {
		return ErrAlbumWorkInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return ErrAlbumWorkInvalid
	}
	return nil
}

func validAlbumPublication(p *AlbumPublication, post string) bool {
	if p == nil || !albumUUID(p.EventUUID) || p.PostUUID != post || (p.GalleryUUID != "" && !albumUUID(p.GalleryUUID)) ||
		p.Selected < 0 || p.Review < 0 || p.Unavailable < 0 || p.Selected+p.Review+p.Unavailable > 8192 || p.Added < 0 || p.Removed < 0 || p.Added > 8192 || p.Removed > 8192 {
		return false
	}
	if p.Action != "create" && p.Action != "sync" && p.Action != "disabled" && p.Action != "ineligible" {
		return false
	}
	if p.Created != (p.Action == "create") || (p.ChangedGallery() && p.GalleryUUID == "") {
		return false
	}
	return (p.Action == "create" || p.Action == "sync") || (!p.ChangedGallery() && p.Selected == 0 && p.Review == 0 && p.Unavailable == 0)
}

func decodeAlbumWork(current *models.ArchiveJob) (*albumWork, *albumProgress, error) {
	if current == nil || current.Kind != models.ArchiveJobBackfillAlbum {
		return nil, nil, ErrAlbumWorkNotFound
	}
	var work albumWork
	if err := decodeAlbumJSON(current.Arguments, 16384, &work); err != nil || work.Version != 1 || !albumUUID(work.PostUUID) ||
		!models.ValidSourceAlbumPolicy(work.Policy) || !archive.ValidSHA256(work.Signature) || (work.ResumeFromJobUUID != "" && !albumUUID(work.ResumeFromJobUUID)) ||
		(work.ResumePublication != nil && (work.ResumeFromJobUUID == "" || !validAlbumPublication(work.ResumePublication, work.PostUUID))) {
		return nil, nil, ErrAlbumWorkInvalid
	}
	var progress albumProgress
	if err := decodeAlbumJSON(current.Progress, 16384, &progress); err != nil || (progress.Version != 0 && progress.Version != 1) ||
		(progress.Version == 0 && progress.Publication != nil) || (progress.Version == 1 && progress.Publication == nil) {
		return nil, nil, ErrAlbumWorkInvalid
	}
	if progress.Publication != nil {
		if !validAlbumPublication(progress.Publication, work.PostUUID) || (work.ResumePublication == nil && progress.Publication.EventUUID != current.UUID) ||
			(work.ResumePublication != nil && !reflect.DeepEqual(progress.Publication, work.ResumePublication)) {
			return nil, nil, ErrAlbumWorkInvalid
		}
	} else if work.ResumePublication != nil {
		// An explicitly resumed publication already committed in the earlier
		// job, even while this new attempt is still queued or cancelled.
		progress.Publication = work.ResumePublication
	}
	return &work, &progress, nil
}

func albumStatus(current *models.ArchiveJob) (*AlbumBackfillStatus, error) {
	work, progress, err := decodeAlbumWork(current)
	if err != nil {
		return nil, err
	}
	var outcome albumOutcome
	if err := decodeAlbumJSON(current.Result, 16384, &outcome); err != nil {
		return nil, err
	}
	if outcome.HooksFinished != (current.State == "succeeded") || (outcome.HooksFinished && progress.Publication == nil) {
		return nil, ErrAlbumWorkInvalid
	}
	return &AlbumBackfillStatus{JobUUID: current.UUID, Sequence: current.Sequence, PostUUID: work.PostUUID, Policy: work.Policy, Signature: work.Signature, State: current.State, Revision: current.Revision,
		Attempts: current.Fence, MaxAttempts: current.MaxAttempts, AvailableAt: current.AvailableAt, PublicationCommitted: progress.Publication != nil, Publication: progress.Publication,
		HooksFinished: outcome.HooksFinished, ErrorCode: current.ErrorCode, ResumeFromJobUUID: work.ResumeFromJobUUID, CreatedAt: current.CreatedAt, UpdatedAt: current.UpdatedAt}, nil
}

func albumSubmission(request string, work albumWork) (models.ArchiveJobSubmission, error) {
	if !albumUUID(request) || !albumUUID(work.PostUUID) || !models.ValidSourceAlbumPolicy(work.Policy) || !archive.ValidSHA256(work.Signature) {
		return models.ArchiveJobSubmission{}, ErrAlbumWorkInvalid
	}
	args, err := json.Marshal(work)
	return models.ArchiveJobSubmission{RequestUUID: request, Kind: models.ArchiveJobBackfillAlbum, WorkKey: albumDigest(append([]byte("album-backfill-v1\x00"), args...)),
		ResourceKey: albumResource(work.PostUUID), Arguments: args, Priority: 10, MaxAttempts: 10}, err
}

func (s *AlbumBackfill) submit(ctx context.Context, request string, work albumWork) (*models.ArchiveJob, error) {
	input, err := albumSubmission(request, work)
	if err != nil {
		return nil, err
	}
	repo := s.Durable.Repo
	prior, err := repo.ArchiveJob.FindSubmission(ctx, request)
	if err != nil {
		return nil, err
	}
	if prior == nil && work.ResumePublication == nil {
		parent, err := repo.SourceEvidence.FindPost(ctx, work.PostUUID)
		if err != nil {
			return nil, err
		}
		if parent == nil {
			return nil, ErrAlbumWorkNotFound
		}
		preview, err := repo.SourceGallery.PreviewBackfill(ctx, work.PostUUID, work.Policy)
		if err != nil {
			return nil, err
		}
		if preview.Signature != work.Signature || preview.Gallery.Action == "review" {
			return nil, models.ErrSourceGalleryConflict
		}
	}
	// Submit checks the original receipt before queue capacity or current
	// domain revisions. A lost response cannot create another operation.
	return repo.ArchiveJob.Submit(ctx, input, s.Durable.Now(), s.Durable.MaxActive)
}

func (s *AlbumBackfill) Submit(ctx context.Context, input AlbumBackfillRequest) (*AlbumBackfillStatus, error) {
	var current *models.ArchiveJob
	err := s.Durable.Repo.WithTxn(ctx, func(ctx context.Context) error {
		var err error
		current, err = s.submit(ctx, input.RequestUUID, albumWork{Version: 1, PostUUID: input.PostUUID, Policy: input.Policy, Signature: input.Signature})
		return err
	})
	if err != nil {
		return nil, err
	}
	return albumStatus(current)
}

func (s *AlbumBackfill) find(ctx context.Context, id string, request bool) (*models.ArchiveJob, error) {
	if !albumUUID(id) {
		return nil, ErrAlbumWorkInvalid
	}
	var current *models.ArchiveJob
	var err error
	if request {
		current, err = s.Durable.Repo.ArchiveJob.FindSubmission(ctx, id)
	} else {
		current, err = s.Durable.Repo.ArchiveJob.Find(ctx, id)
	}
	if err != nil {
		return nil, err
	}
	if current == nil || current.Kind != models.ArchiveJobBackfillAlbum {
		return nil, ErrAlbumWorkNotFound
	}
	return current, nil
}

func (s *AlbumBackfill) Status(ctx context.Context, id string, byRequest bool) (*AlbumBackfillStatus, error) {
	var current *models.ArchiveJob
	err := s.Durable.Repo.WithReadTxn(ctx, func(ctx context.Context) error { var err error; current, err = s.find(ctx, id, byRequest); return err })
	if err != nil {
		return nil, err
	}
	return albumStatus(current)
}

func (s *AlbumBackfill) History(ctx context.Context, post string, after int64, limit int) ([]AlbumBackfillStatus, error) {
	if !albumUUID(post) || after < 0 || limit < 1 || limit > 100 {
		return nil, ErrAlbumWorkInvalid
	}
	ret := []AlbumBackfillStatus{}
	err := s.Durable.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		parent, err := s.Durable.Repo.SourceEvidence.FindPost(ctx, post)
		if err != nil {
			return err
		}
		if parent == nil {
			return ErrAlbumWorkNotFound
		}
		rows, err := s.Durable.Repo.ArchiveJob.ResourceHistory(ctx, models.ArchiveJobBackfillAlbum, albumResource(post), after, limit)
		if err != nil {
			return err
		}
		for i := range rows {
			status, err := albumStatus(&rows[i])
			if err != nil {
				return err
			}
			ret = append(ret, *status)
		}
		return nil
	})
	return ret, err
}

func (s *AlbumBackfill) Attempts(ctx context.Context, id string, after int64, limit int) ([]models.ArchiveJobAttempt, error) {
	if after < 0 || limit < 1 || limit > 100 {
		return nil, ErrAlbumWorkInvalid
	}
	var ret []models.ArchiveJobAttempt
	err := s.Durable.Repo.WithReadTxn(ctx, func(ctx context.Context) error {
		if _, err := s.find(ctx, id, false); err != nil {
			return err
		}
		var err error
		ret, err = s.Durable.Repo.ArchiveJob.Attempts(ctx, id, after, limit)
		return err
	})
	return ret, err
}

func (s *AlbumBackfill) Cancel(ctx context.Context, id string, revision int64) (*AlbumBackfillStatus, error) {
	if revision < 1 {
		return nil, ErrAlbumWorkInvalid
	}
	var current *models.ArchiveJob
	err := s.Durable.Repo.WithTxn(ctx, func(ctx context.Context) error {
		if _, err := s.find(ctx, id, false); err != nil {
			return err
		}
		var err error
		current, err = s.Durable.Repo.ArchiveJob.Cancel(ctx, id, revision, s.Durable.Now())
		return err
	})
	if err != nil {
		return nil, err
	}
	return albumStatus(current)
}

// Retry creates a new submission without rewriting terminal job history. A
// committed publication carries its original event identity and never repeats
// the domain mutation. Unpublished work must still pass its original preview.
func (s *AlbumBackfill) Retry(ctx context.Context, id string, revision int64, request string) (*AlbumBackfillStatus, error) {
	if !albumUUID(request) || revision < 1 {
		return nil, ErrAlbumWorkInvalid
	}
	var current *models.ArchiveJob
	err := s.Durable.Repo.WithTxn(ctx, func(ctx context.Context) error {
		parent, err := s.find(ctx, id, false)
		if err != nil {
			return err
		}
		if parent.Revision != revision || (parent.State != "failed" && parent.State != "cancelled") {
			return models.ErrArchiveJobConflict
		}
		work, progress, err := decodeAlbumWork(parent)
		if err != nil {
			return err
		}
		work.ResumeFromJobUUID, work.ResumePublication = parent.UUID, progress.Publication
		current, err = s.submit(ctx, request, *work)
		return err
	})
	if err != nil {
		return nil, err
	}
	return albumStatus(current)
}
