package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"maps"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

type discoveryPageRow struct {
	models.DiscoveryPageReceipt
	Body  string `db:"body"`
	Bytes int64  `db:"byte_count"`
}

func (row discoveryPageRow) resolve() (*models.DiscoveryPage, error) {
	parsed, err := archive.ParseDiscoveryPage([]byte(row.Body))
	if err != nil || string(parsed.Body()) != row.Body || sourceDigest([]byte(row.Body)) != row.Digest || int64(len(row.Body)) != row.Bytes ||
		row.RecordCount != len(parsed.Records) || row.Complete != parsed.Complete || !validJobTime(row.CreatedAt) {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return &models.DiscoveryPage{DiscoveryPageReceipt: row.DiscoveryPageReceipt, Body: []byte(row.Body)}, nil
}

func readDiscoveryPage(ctx context.Context, query string, args ...any) (*models.DiscoveryPage, error) {
	var row discoveryPageRow
	err := dbWrapper.Get(ctx, &row, query, args...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return row.resolve()
}

func (s *DiscoveryJobStore) Page(ctx context.Context, id string, ordinal int) (*models.DiscoveryPage, error) {
	if !validSourceRunUUID(id) || ordinal < 1 || ordinal > archive.MaxDiscoveryPages {
		return nil, models.ErrDiscoveryInvalid
	}
	return readDiscoveryPage(ctx, "SELECT * FROM discovery_pages WHERE listing_uuid=? AND ordinal=?", id, ordinal)
}

func (s *DiscoveryJobStore) PageHead(ctx context.Context, id string) (*models.DiscoveryPage, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrDiscoveryInvalid
	}
	return readDiscoveryPage(ctx, "SELECT * FROM discovery_pages WHERE listing_uuid=? ORDER BY ordinal DESC LIMIT 1", id)
}

func (s *DiscoveryJobStore) Pages(ctx context.Context, id string, after, limit int) ([]models.DiscoveryPageReceipt, error) {
	if !validSourceRunUUID(id) || after < 0 {
		return nil, models.ErrDiscoveryInvalid
	}
	limit, err := sourcePageLimit(limit)
	if err != nil {
		return nil, err
	}
	rows := []models.DiscoveryPageReceipt{}
	err = dbWrapper.Select(ctx, &rows, `SELECT listing_uuid,ordinal,job_uuid,fence,producer_uuid,digest,record_count,complete,created_at
 FROM discovery_pages WHERE listing_uuid=? AND ordinal>? ORDER BY ordinal LIMIT ?`, id, after, limit)
	return rows, err
}

func (s *DiscoveryJobStore) AppendPage(ctx context.Context, lease models.DiscoveryJobLease, ordinal int, raw json.RawMessage, now time.Time) (*models.DiscoveryPageReceipt, error) {
	if err := managedArchiveJobWrite(ctx); err != nil {
		return nil, err
	}
	if !validJobTime(now) || ordinal < 1 || ordinal > archive.MaxDiscoveryPages {
		return nil, models.ErrDiscoveryInvalid
	}
	if err := s.ownedAttempt(ctx, lease); err != nil {
		return nil, err
	}
	page, err := archive.ParseDiscoveryPage(raw)
	if err != nil {
		return nil, err
	}
	body := page.Body()
	digest := sourceDigest(body)
	job, err := (&ArchiveJobStore{}).Find(ctx, lease.JobUUID)
	if err != nil {
		return nil, err
	}
	work, err := archive.DecodeDiscoveryJob(job)
	if err != nil {
		return nil, err
	}
	if work.PageOrdinal != ordinal {
		return nil, models.ErrDiscoveryConflict
	}
	prior, err := s.Page(ctx, work.ListingUUID, ordinal)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		// Lost acknowledgement replay proves only this original immutable page;
		// it does not revive a cancelled or expired source reservation.
		if prior.Digest != digest || prior.JobUUID != lease.JobUUID || prior.Fence != lease.Fence || prior.ProducerUUID != lease.ProducerUUID {
			return nil, models.ErrDiscoveryConflict
		}
		return &prior.DiscoveryPageReceipt, nil
	}
	job, err = s.CheckLease(ctx, lease, now)
	if err != nil {
		return nil, err
	}
	listing, err := s.Listing(ctx, work.ListingUUID)
	if err != nil {
		return nil, err
	}
	if page.URL != listing.ProfileURL || page.ExtractorVersion != listing.ExtractorVersion {
		return nil, models.ErrDiscoveryConflict
	}
	cursor := listing.InitialCursor
	expected := 1
	head, err := s.PageHead(ctx, listing.UUID)
	if err != nil {
		return nil, err
	}
	if head != nil {
		if head.Complete || now.Before(head.CreatedAt) {
			return nil, models.ErrDiscoveryConflict
		}
		previous, err := archive.ParseDiscoveryPage(head.Body)
		if err != nil {
			return nil, err
		}
		cursor, expected = previous.NextCursor, head.Ordinal+1
	}
	if ordinal != expected || !maps.Equal(page.Cursor, cursor) {
		return nil, models.ErrDiscoveryConflict
	}
	if !page.Complete {
		next, err := archive.EncodeSourceJSON(page.NextCursor)
		if err != nil {
			return nil, err
		}
		var repeated bool
		if err := dbWrapper.Get(ctx, &repeated, "SELECT EXISTS(SELECT 1 FROM discovery_pages WHERE listing_uuid=? AND json_extract(body,'$.cursor')=?)", listing.UUID, string(next)); err != nil {
			return nil, err
		}
		if repeated {
			return nil, models.ErrDiscoveryConflict
		}
	}
	for _, record := range page.Records {
		observed, err := time.Parse(time.RFC3339Nano, record.ObservedAt)
		if err != nil || observed.After(now) {
			return nil, models.ErrDiscoveryInvalid
		}
	}
	var used int64
	if err := dbWrapper.Get(ctx, &used, "SELECT coalesce(sum(byte_count),0) FROM discovery_pages"); err != nil {
		return nil, err
	}
	if int64(len(body)) > archive.MaxDiscoveryStoredBytes-used {
		return nil, models.ErrArchiveJobCapacity
	}
	complete := discoveryAtomic(ctx)
	receipt := models.DiscoveryPageReceipt{ListingUUID: listing.UUID, Ordinal: ordinal, JobUUID: job.UUID, Fence: job.Fence, ProducerUUID: lease.ProducerUUID,
		Digest: digest, RecordCount: len(page.Records), Complete: page.Complete, CreatedAt: now.UTC()}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO discovery_pages(listing_uuid,ordinal,job_uuid,fence,producer_uuid,digest,byte_count,record_count,complete,body,created_at)
 VALUES(?,?,?,?,?,?,?,?,?,?,?)`, receipt.ListingUUID, ordinal, receipt.JobUUID, receipt.Fence, receipt.ProducerUUID, digest, len(body), receipt.RecordCount, receipt.Complete, string(body), receipt.CreatedAt)
	if err != nil {
		return nil, err
	}
	progress, err := archive.EncodeSourceJSON(map[string]any{"listing_uuid": listing.UUID, "page_ordinal": ordinal, "page_sha256": digest})
	if err != nil {
		return nil, err
	}
	// Release the service after each page so downloads retain their preference.
	_, err = (&ArchiveJobStore{}).Finish(ctx, lease.ArchiveJobLease, now, models.ArchiveJobOutcome{State: "succeeded", Result: progress})
	if err != nil {
		return nil, err
	}
	*complete = true
	return &receipt, nil
}
