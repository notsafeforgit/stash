package sqlite

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/models"
)

func validAssociationReviewUUID(value string) bool {
	id, err := archiveUUID(value)
	return err == nil && id == value
}

type sourceAssociationReviewRow struct {
	RequestUUID    string    `db:"request_uuid"`
	PostUUID       string    `db:"post_uuid"`
	AttachmentUUID string    `db:"attachment_uuid"`
	DecisionUUID   string    `db:"decision_uuid"`
	RequestJSON    string    `db:"request_json"`
	Signature      string    `db:"signature"`
	CreatedAt      Timestamp `db:"created_at"`
}

func readSourceAssociationReviewRow(get enrichmentGet, table, id string) (*sourceAssociationReviewRow, error) {
	if !validAssociationReviewUUID(id) {
		return nil, models.ErrSourceAssociationReviewInvalid
	}
	var row sourceAssociationReviewRow
	if err := get(&row, "SELECT * FROM "+table+" WHERE request_uuid=?", id); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &row, nil
}

func sourceAssociationReceiptSignature(kind string, input any, decision string) (string, error) {
	return sourceSignature("stash-"+kind+"-receipt-v1", struct {
		Request      any    `json:"request"`
		DecisionUUID string `json:"decision_uuid"`
	}{input, decision})
}

// Imported UUID adoption may cascade into retained decisions. Preserve the
// original request bytes and compare retained identities through redirects.
// Ordinary later merges/deletion do not invalidate an already committed receipt.
func sameReviewArchiveIdentity(get enrichmentGet, requested, recorded string) (bool, error) {
	if requested == recorded {
		return true, nil
	}
	resolve := func(id string) (string, error) {
		for range 128 {
			var row struct {
				UUID     string         `db:"uuid"`
				State    string         `db:"state"`
				Redirect sql.NullString `db:"redirect_to"`
			}
			if err := get(&row, "SELECT uuid,state,redirect_to FROM archive_entities WHERE uuid=?", id); err != nil {
				return "", err
			}
			if row.State != "redirected" {
				return row.UUID, nil
			}
			if !row.Redirect.Valid {
				return "", models.ErrSourcePayloadCorrupt
			}
			id = row.Redirect.String
		}
		return "", models.ErrSourcePayloadCorrupt
	}
	one, err := resolve(requested)
	if err != nil {
		return false, err
	}
	two, err := resolve(recorded)
	return one == two, err
}

func validateSourceAssociationReviewSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"gallery_association_reviews", "gallery_association_reviews_post", "gallery_association_review_immutable", "gallery_association_review_scope",
		"attachment_media_reviews", "attachment_media_reviews_attachment", "attachment_media_reviews_post", "attachment_media_review_immutable", "attachment_media_review_scope"} {
		var exists bool
		if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	if !auditData {
		return nil
	}
	for _, table := range []string{"gallery_association_reviews", "attachment_media_reviews"} {
		for after := ""; ; {
			var id string
			if err := conn.Get(&id, "SELECT coalesce(min(request_uuid),'') FROM "+table+" WHERE request_uuid>?", after); err != nil {
				return err
			}
			if id == "" {
				break
			}
			var err error
			if table == "gallery_association_reviews" {
				_, err = readGalleryAssociationReview(conn.Get, id)
			} else {
				_, err = readAttachmentMediaReview(conn.Get, id)
			}
			if err != nil {
				return fmt.Errorf("source association review %s: %w", id, err)
			}
			after = id
		}
	}
	return nil
}
