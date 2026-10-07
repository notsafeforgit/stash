package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"slices"

	"github.com/stashapp/stash/pkg/models"
)

type postConsolidationReviewRow struct {
	RequestUUID         string         `db:"request_uuid"`
	RequestJSON         string         `db:"request_json"`
	ResultJSON          string         `db:"result_json"`
	Signature           string         `db:"signature"`
	SelectionUUID       sql.NullString `db:"selection_uuid"`
	GalleryDecisionUUID sql.NullString `db:"gallery_decision_uuid"`
	NotificationJobUUID sql.NullString `db:"notification_job_uuid"`
}

func postConsolidationReceiptSignature(review *models.PostConsolidationReview) (string, error) {
	return sourceSignature("stash-post-merge-review-receipt-v1", review)
}

func storePostConsolidationReview(ctx context.Context, review *models.PostConsolidationReview, request []byte) error {
	result, err := json.Marshal(review.Result)
	if err != nil || len(result) > models.MaxPostConsolidationReviewBytes {
		return models.ErrPostConsolidationReviewInvalid
	}
	signature, err := postConsolidationReceiptSignature(review)
	if err != nil {
		return err
	}
	nullable := func(value string) sql.NullString { return sql.NullString{String: value, Valid: value != ""} }
	id := review.Request.RequestUUID
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO post_consolidation_reviews
(request_uuid,request_json,result_json,signature,selection_uuid,gallery_decision_uuid,notification_job_uuid) VALUES(?,?,?,?,?,?,?)`,
		id, string(request), string(result), signature, nullable(review.Result.SelectionUUID), nullable(review.Result.GalleryDecisionUUID), nullable(review.Result.NotificationJobUUID)); err != nil {
		return err
	}
	for _, member := range review.Result.Members {
		if _, err := dbWrapper.Exec(ctx, `INSERT INTO post_consolidation_review_members(request_uuid,post_uuid,previous_canonical_uuid,previous_revision)
VALUES(?,?,?,?)`, id, member.PostUUID, member.PreviousCanonicalUUID, member.PreviousRevision); err != nil {
			return err
		}
	}
	for table, ids := range map[string][]string{"post_consolidation_review_media": review.Result.MediaDecisionUUIDs, "post_consolidation_review_attachments": review.Result.AttachmentDecisionUUIDs} {
		for ordinal, decision := range ids {
			if _, err := dbWrapper.Exec(ctx, "INSERT INTO "+table+"(request_uuid,ordinal,decision_uuid) VALUES(?,?,?)", id, ordinal, decision); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *SourceEvidenceStore) ConsolidationReview(ctx context.Context, id string) (*models.PostConsolidationReview, error) {
	return readPostConsolidationReview(func(out any, query string, args ...any) error { return dbWrapper.Get(ctx, out, query, args...) },
		func(out any, query string, args ...any) error { return dbWrapper.Select(ctx, out, query, args...) }, id)
}

func (s *SourceEvidenceStore) CheckConsolidationReview(ctx context.Context, input models.PostConsolidationReviewApplyInput) (*models.PostConsolidationReview, error) {
	encoded, err := postConsolidationReviewRequest(input)
	if err != nil {
		return nil, err
	}
	prior, err := s.ConsolidationReview(ctx, input.RequestUUID)
	if err != nil || prior == nil {
		return prior, err
	}
	original, err := postConsolidationReviewRequest(prior.Request)
	if err != nil {
		return nil, err
	}
	if string(original) != string(encoded) {
		return nil, models.ErrSourcePostConsolidationReplay
	}
	return prior, nil
}

func readPostConsolidationReview(get enrichmentGet, selectRows enrichmentSelect, id string) (*models.PostConsolidationReview, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrPostConsolidationReviewInvalid
	}
	var row postConsolidationReviewRow
	if err := get(&row, "SELECT * FROM post_consolidation_reviews WHERE request_uuid=?", id); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	ret := &models.PostConsolidationReview{}
	if len(row.RequestJSON) > models.MaxPostConsolidationReviewBytes || len(row.ResultJSON) > models.MaxPostConsolidationReviewBytes ||
		json.Unmarshal([]byte(row.RequestJSON), &ret.Request) != nil || json.Unmarshal([]byte(row.ResultJSON), &ret.Result) != nil {
		return nil, models.ErrSourcePayloadCorrupt
	}
	request, err := postConsolidationReviewRequest(ret.Request)
	result, resultErr := json.Marshal(ret.Result)
	signature, signatureErr := postConsolidationReceiptSignature(ret)
	if err != nil || resultErr != nil || signatureErr != nil || string(request) != row.RequestJSON || string(result) != row.ResultJSON || signature != row.Signature ||
		ret.Request.RequestUUID != id || ret.Result.SelectionUUID != row.SelectionUUID.String || ret.Result.GalleryDecisionUUID != row.GalleryDecisionUUID.String ||
		ret.Result.NotificationJobUUID != row.NotificationJobUUID.String {
		return nil, models.ErrSourcePayloadCorrupt
	}
	var merge postConsolidationRow
	if err := get(&merge, "SELECT * FROM source_post_consolidations WHERE uuid=?", id); err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(merge.record(), ret.Result.Consolidation) || merge.SourceUUID != ret.Request.SourceUUID || merge.DestinationUUID != ret.Request.DestinationUUID ||
		merge.ReviewSignature != ret.Request.Digest || merge.Origin != "review" || merge.Reason != ret.Request.Reason {
		return nil, models.ErrSourcePayloadCorrupt
	}
	if err := readPostConsolidationReviewScope(get, selectRows, ret); err != nil {
		return nil, err
	}
	if ret.Result.Gallery.Changed() != row.NotificationJobUUID.Valid {
		return nil, models.ErrSourcePayloadCorrupt
	}
	if row.NotificationJobUUID.Valid {
		var job archiveJobRow
		if err := get(&job, "SELECT * FROM archive_jobs WHERE uuid=?", row.NotificationJobUUID.String); err != nil {
			return nil, err
		}
		if err := validatePostMergeNotificationJob(job.resolve(), id, ret.Result.Gallery.GalleryUUID); err != nil {
			return nil, err
		}
	}
	return ret, nil
}

func readPostConsolidationReviewScope(get enrichmentGet, selectRows enrichmentSelect, review *models.PostConsolidationReview) error {
	id, input, result := review.Request.RequestUUID, review.Request, review.Result
	if err := validatePostConsolidationGalleryResult(get, result); err != nil {
		return err
	}
	var members []struct {
		PostUUID              string `db:"post_uuid"`
		PreviousCanonicalUUID string `db:"previous_canonical_uuid"`
		PreviousRevision      int    `db:"previous_revision"`
	}
	if err := selectRows(&members, `SELECT post_uuid,previous_canonical_uuid,previous_revision
FROM post_consolidation_review_members WHERE request_uuid=? ORDER BY post_uuid LIMIT ?`, id, maxPostIdentityMembers+1); err != nil {
		return err
	}
	if len(members) != result.Consolidation.MemberCount || len(members) != len(result.Members) || len(members) > maxPostIdentityMembers {
		return models.ErrSourcePayloadCorrupt
	}
	owners := map[string]bool{}
	for i, member := range members {
		if member.PostUUID != result.Members[i].PostUUID || member.PreviousCanonicalUUID != result.Members[i].PreviousCanonicalUUID ||
			member.PreviousRevision != result.Members[i].PreviousRevision || member.PreviousRevision < 1 ||
			(member.PreviousCanonicalUUID != input.SourceUUID && member.PreviousCanonicalUUID != input.DestinationUUID) {
			return models.ErrSourcePayloadCorrupt
		}
		owners[member.PostUUID] = true
		if (member.PostUUID == input.SourceUUID && (member.PreviousCanonicalUUID != input.SourceUUID || member.PreviousRevision != result.Consolidation.SourceRevision)) ||
			(member.PostUUID == input.DestinationUUID && (member.PreviousCanonicalUUID != input.DestinationUUID || member.PreviousRevision != result.Consolidation.DestinationRevision)) {
			return models.ErrSourcePayloadCorrupt
		}
	}
	if !owners[input.SourceUUID] || !owners[input.DestinationUUID] {
		return models.ErrSourcePayloadCorrupt
	}
	if result.SelectionUUID != "" {
		var selection attachmentSelectionRow
		if err := get(&selection, "SELECT * FROM post_attachment_decisions WHERE uuid=?", result.SelectionUUID); err != nil {
			return err
		}
		if selection.PostUUID != input.DestinationUUID || selection.Origin != "review" || selection.Reason != input.Reason {
			return models.ErrSourcePayloadCorrupt
		}
	}
	if result.GalleryDecisionUUID != "" {
		var gallery sourceGalleryDecisionRow
		if err := get(&gallery, "SELECT * FROM post_gallery_decisions WHERE uuid=?", result.GalleryDecisionUUID); err != nil {
			return err
		}
		if gallery.PostUUID != input.DestinationUUID || (gallery.Origin != "review" && gallery.Origin != "source") ||
			(gallery.Origin == "review" && gallery.Reason != input.Reason) ||
			(gallery.Origin == "source" && (!result.Gallery.Created || gallery.SelectionUUID.String != result.SelectionUUID)) ||
			gallery.GalleryUUID.String != result.Gallery.GalleryUUID {
			return models.ErrSourcePayloadCorrupt
		}
	}
	for _, attachment := range []bool{false, true} {
		table, join, expected := "post_consolidation_review_media", "JOIN post_media_decisions d ON d.uuid=r.decision_uuid", result.MediaDecisionUUIDs
		post := "d.post_uuid"
		if attachment {
			table, expected = "post_consolidation_review_attachments", result.AttachmentDecisionUUIDs
			join, post = "JOIN attachment_media_decisions d ON d.uuid=r.decision_uuid JOIN source_attachments a ON a.uuid=d.attachment_uuid", "a.post_uuid"
		}
		var rows []struct {
			Ordinal      int    `db:"ordinal"`
			DecisionUUID string `db:"decision_uuid"`
			PostUUID     string `db:"post_uuid"`
			Origin       string `db:"origin"`
			Reason       string `db:"reason"`
		}
		if err := selectRows(&rows, "SELECT r.ordinal,r.decision_uuid,"+post+" AS post_uuid,d.origin,d.reason FROM "+table+" r "+join+
			" WHERE r.request_uuid=? ORDER BY r.ordinal LIMIT ?", id, maxPostComparisonChoices+1); err != nil {
			return err
		}
		if len(rows) != len(expected) || len(rows) > maxPostComparisonChoices {
			return models.ErrSourcePayloadCorrupt
		}
		seen := []string{}
		for i, row := range rows {
			if row.Ordinal != i || row.DecisionUUID != expected[i] || row.Origin != "review" || row.Reason != input.Reason ||
				(!attachment && row.PostUUID != input.DestinationUUID) || (attachment && !owners[row.PostUUID]) {
				return models.ErrSourcePayloadCorrupt
			}
			seen = append(seen, row.DecisionUUID)
		}
		slices.Sort(seen)
		if len(slices.Compact(seen)) != len(expected) {
			return models.ErrSourcePayloadCorrupt
		}
	}
	return nil
}

func validatePostConsolidationGalleryResult(get enrichmentGet, result models.PostConsolidationReviewResult) error {
	g := result.Gallery
	if (g.Action != "create" && g.Action != "sync" && g.Action != "disabled" && g.Action != "ineligible") ||
		g.Created != (g.Action == "create") || (g.Changed() && (g.Action != "create" && g.Action != "sync")) ||
		((g.Action == "create" || g.Action == "sync") && (g.GalleryUUID == "" || result.SelectionUUID == "")) ||
		(g.GalleryUUID != "" && result.GalleryDecisionUUID == "") || len(g.Added) > maxSourceGalleryMembers || len(g.Removed) > maxSourceGalleryMembers ||
		(g.Created && len(g.Removed) != 0) {
		return models.ErrSourcePayloadCorrupt
	}
	if g.GalleryUUID != "" {
		var kind string
		if err := get(&kind, "SELECT kind FROM archive_entities WHERE uuid=?", g.GalleryUUID); err != nil {
			return err
		}
		if kind != string(models.ArchiveGallery) {
			return models.ErrSourcePayloadCorrupt
		}
	}
	seen := map[string]bool{}
	ids := make([]string, 0, len(g.Added)+len(g.Removed))
	for _, group := range [][]string{g.Added, g.Removed} {
		for _, id := range group {
			if !validSourceRunUUID(id) || seen[id] {
				return models.ErrSourcePayloadCorrupt
			}
			seen[id] = true
			ids = append(ids, id)
		}
	}
	if len(ids) != 0 {
		encoded, err := json.Marshal(ids)
		if err != nil {
			return err
		}
		var count int
		if err := get(&count, `SELECT count(*) FROM archive_entities WHERE kind IN ('scene','image')
AND uuid IN (SELECT value FROM json_each(?))`, string(encoded)); err != nil {
			return err
		}
		if count != len(ids) {
			return models.ErrSourcePayloadCorrupt
		}
	}
	return nil
}
