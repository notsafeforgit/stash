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
}

func jobTime(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.UnixNano()
}

func (r libraryJobRow) job() job.Job {
	j := job.Job{ID: r.ID, Status: r.Status, Description: r.Description, Progress: r.Progress, AddTime: time.Unix(0, r.AddedAt), Error: r.Error}
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
		_, err := dbWrapper.Exec(ctx, `INSERT INTO library_jobs(id,status,description,progress,added_at) VALUES(?,?,?,?,?)`, j.ID, j.Status, j.Description, j.Progress, j.AddTime.UnixNano())
		return err
	})
}

func (s *LibraryJobHistory) Save(ctx context.Context, j job.Job) error {
	return txn.WithTxn(ctx, s.database, func(ctx context.Context) error {
		result, err := dbWrapper.Exec(ctx, `UPDATE library_jobs SET status=?,progress=?,started_at=?,ended_at=?,error=? WHERE id=? AND (ended_at IS NULL OR (status=? AND progress=? AND started_at IS ? AND ended_at IS ? AND error IS ?))`, j.Status, j.Progress, jobTime(j.StartTime), jobTime(j.EndTime), j.Error, j.ID, j.Status, j.Progress, jobTime(j.StartTime), jobTime(j.EndTime), j.Error)
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("job %s is missing or already terminal", j.ID)
		}
		if j.EndTime != nil {
			return pruneLibraryJobHistory(ctx)
		}
		return nil
	})
}

func pruneLibraryJobHistory(ctx context.Context) error {
	_, err := dbWrapper.Exec(ctx, `DELETE FROM library_jobs WHERE id IN (SELECT id FROM library_jobs WHERE ended_at IS NOT NULL ORDER BY added_at DESC,id LIMIT -1 OFFSET 1000)`)
	return err
}

func (s *LibraryJobHistory) Interrupt(ctx context.Context) error {
	return txn.WithTxn(ctx, s.database, func(ctx context.Context) error {
		_, err := dbWrapper.Exec(ctx, `UPDATE library_jobs SET status='INTERRUPTED',ended_at=?,error=? WHERE ended_at IS NULL`, time.Now().UnixNano(), "Interrupted by a server restart. This job was not resumed; review partial results before starting it again.")
		if err != nil {
			return err
		}
		return pruneLibraryJobHistory(ctx)
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
