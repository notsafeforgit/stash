package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/txn"
)

// LibraryJobHistory is a core store, available with source tracking disabled.
// Only terminal history is bounded; outstanding work is never pruned.
type LibraryJobHistory struct{ database *Database }

func NewLibraryJobHistory(db *Database) *LibraryJobHistory { return &LibraryJobHistory{database: db} }

type libraryJobRow struct {
	ID          string     `db:"id"`
	Status      job.Status `db:"status"`
	Description string     `db:"description"`
	Progress    float64    `db:"progress"`
	AddedAt     int64      `db:"added_at"`
	StartedAt   *int64     `db:"started_at"`
	EndedAt     *int64     `db:"ended_at"`
	Error       *string    `db:"error"`
	Generation  int        `db:"generation"`
}

func jobTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.UnixNano()
}

func (r libraryJobRow) job() job.Job {
	j := job.Job{ID: r.ID, Status: r.Status, Description: r.Description, Progress: r.Progress, AddTime: time.Unix(0, r.AddedAt), Error: r.Error, Generation: r.Generation}
	if r.StartedAt != nil {
		value := time.Unix(0, *r.StartedAt)
		j.StartTime = &value
	}
	if r.EndedAt != nil {
		value := time.Unix(0, *r.EndedAt)
		j.EndTime = &value
	}
	return j
}

func (s *LibraryJobHistory) Create(ctx context.Context, j job.Job) error {
	return txn.WithTxn(ctx, s.database, func(ctx context.Context) error {
		_, err := dbWrapper.Exec(ctx, `INSERT INTO library_jobs(id,status,description,progress,added_at,generation) VALUES(?,?,?,?,?,?)`,
			j.ID, j.Status, boundedJobText(j.Description, 1024), j.Progress, j.AddTime.UnixNano(), max(j.Generation, 1))
		return err
	})
}

func (s *LibraryJobHistory) Save(ctx context.Context, j job.Job) error {
	if j.Error != nil {
		message := boundedJobText(*j.Error, 16384)
		j.Error = &message
	}
	return txn.WithTxn(ctx, s.database, func(ctx context.Context) error {
		result, err := dbWrapper.Exec(ctx, `UPDATE library_jobs SET status=?,progress=?,started_at=?,ended_at=?,error=? WHERE id=? AND generation=? AND (ended_at IS NULL OR (status=? AND progress=? AND started_at IS ? AND ended_at IS ? AND error IS ?))`,
			j.Status, j.Progress, jobTime(j.StartTime), jobTime(j.EndTime), j.Error, j.ID, max(j.Generation, 1), j.Status, j.Progress, jobTime(j.StartTime), jobTime(j.EndTime), j.Error)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("job %s is missing, already terminal or superseded", j.ID)
		}
		if j.EndTime != nil {
			return pruneLibraryJobHistory(ctx)
		}
		return nil
	})
}

func pruneLibraryJobHistory(ctx context.Context) error {
	// Large commands/targets are recovery state, not history. Terminal outcome
	// and reclamation commit together; the compact summary retains 1,000 jobs.
	if _, err := dbWrapper.Exec(ctx, `DELETE FROM library_bulk_jobs WHERE job_id IN (SELECT id FROM library_jobs WHERE ended_at IS NOT NULL)`); err != nil {
		return err
	}
	_, err := dbWrapper.Exec(ctx, `DELETE FROM library_jobs WHERE id IN (SELECT id FROM library_jobs WHERE ended_at IS NOT NULL ORDER BY added_at DESC,id LIMIT -1 OFFSET 1000)`)
	return err
}

func (s *LibraryJobHistory) Interrupt(ctx context.Context) error {
	return txn.WithTxn(ctx, s.database, func(ctx context.Context) error {
		if _, err := dbWrapper.Exec(ctx, `UPDATE library_jobs SET status='CANCELLED',ended_at=? WHERE status='STOPPING' AND ended_at IS NULL`, time.Now().UnixNano()); err != nil {
			return err
		}
		if _, err := dbWrapper.Exec(ctx, `UPDATE library_jobs SET status='READY',generation=generation+1,error=NULL
WHERE ended_at IS NULL AND EXISTS(SELECT 1 FROM library_bulk_jobs WHERE job_id=library_jobs.id)`); err != nil {
			return err
		}
		_, err := dbWrapper.Exec(ctx, `UPDATE library_jobs SET status='INTERRUPTED',ended_at=?,error=?
WHERE ended_at IS NULL AND NOT EXISTS(SELECT 1 FROM library_bulk_jobs WHERE job_id=library_jobs.id)`, time.Now().UnixNano(), "Interrupted by a server restart. This job was not resumed; review partial results before starting it again.")
		if err != nil {
			return err
		}
		return pruneLibraryJobHistory(ctx)
	})
}

func (s *LibraryJobHistory) Cancel(ctx context.Context, id string, generation int) error {
	return txn.WithTxn(ctx, s.database, func(ctx context.Context) error {
		result, err := dbWrapper.Exec(ctx, "UPDATE library_jobs SET status='STOPPING' WHERE id=? AND generation=? AND ended_at IS NULL", id, max(generation, 1))
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if changed != 1 {
			return fmt.Errorf("job %s is missing, already terminal or superseded", id)
		}
		return nil
	})
}
func (s *LibraryJobHistory) Find(ctx context.Context, id string) (*job.Job, error) {
	var result *job.Job
	err := txn.WithReadTxn(ctx, s.database, func(ctx context.Context) error {
		var row libraryJobRow
		if err := dbWrapper.Get(ctx, &row, `SELECT * FROM library_jobs WHERE id=?`, id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		value := row.job()
		result = &value
		return nil
	})
	return result, err
}

func (s *LibraryJobHistory) List(ctx context.Context, limit, offset int) ([]job.Job, error) {
	if limit < 1 || limit > 100 || offset < 0 {
		return nil, errors.New("invalid job history pagination")
	}
	ret := []job.Job{}
	err := txn.WithReadTxn(ctx, s.database, func(ctx context.Context) error {
		var rows []libraryJobRow
		if err := dbWrapper.Select(ctx, &rows, `SELECT * FROM library_jobs ORDER BY added_at DESC,id LIMIT ? OFFSET ?`, limit, offset); err != nil {
			return err
		}
		for _, row := range rows {
			ret = append(ret, row.job())
		}
		return nil
	})
	return ret, err
}
