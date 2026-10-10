package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/scrape"
)

func validateDiscoveryMatchSchema(conn *sqlx.DB, auditData bool) error {
	for _, name := range []string{"discovery_match_targets", "discovery_match_targets_source", "discovery_match_targets_pending", "discovery_match_target_scope", "discovery_match_target_transition",
		"discovery_match_pages", "discovery_match_page_immutable", "discovery_match_page_scope", "discovery_match_candidates", "discovery_match_candidates_page",
		"discovery_match_candidate_transition", "discovery_match_evidence", "discovery_match_evidence_candidate", "discovery_match_evidence_immutable"} {
		var found bool
		if err := conn.Get(&found, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	if !auditData {
		return nil
	}
	for _, table := range []string{"discovery_match_targets", "discovery_match_pages", "discovery_match_candidates", "discovery_match_evidence"} {
		var child, parent string
		var row, key int64
		err := conn.QueryRow("PRAGMA foreign_key_check("+table+")").Scan(&child, &row, &parent, &key)
		if err == nil {
			return models.ErrSourcePayloadCorrupt
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	var oversized bool
	if err := conn.Get(&oversized, "SELECT EXISTS(SELECT 1 FROM discovery_match_targets GROUP BY listing_uuid HAVING count(*)>?)", maxDiscoveryMatchTargets); err != nil {
		return err
	}
	if oversized {
		return models.ErrSourcePayloadCorrupt
	}
	for after := ""; ; {
		var target models.DiscoveryMatchTarget
		err := conn.Get(&target, "SELECT * FROM discovery_match_targets WHERE uuid>? ORDER BY uuid LIMIT 1", after)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := validateDiscoveryMatchTarget(conn, target); err != nil {
			return err
		}
		after = target.UUID
	}
}

func validateDiscoveryMatchTarget(conn *sqlx.DB, target models.DiscoveryMatchTarget) error {
	if !validSourceRunUUID(target.ListingUUID) || !validSourceRunUUID(target.SnapshotUUID) || target.SourceOrdinal < 1 || target.UUID != discoveryTargetUUID(target.ListingUUID, target.SourceOrdinal) ||
		target.Policy != scrape.DiscoveryListingMatchPolicy || target.PostRevision < 1 || target.Revision != target.LastPage+1 || target.LastPage < 0 || target.LastPage > archive.MaxDiscoveryPages ||
		!validJobTime(target.CreatedAt) || !validJobTime(target.UpdatedAt) || target.UpdatedAt.Before(target.CreatedAt) {
		return models.ErrSourcePayloadCorrupt
	}
	var row discoveryListingRow
	if err := conn.Get(&row, "SELECT * FROM discovery_listings WHERE uuid=?", target.ListingUUID); err != nil {
		return err
	}
	listing, err := resolveDiscoveryListing(conn.Get, row)
	if err != nil {
		return err
	}
	if listing.Legacy == nil || listing.Legacy.SnapshotUUID != target.SnapshotUUID || target.CreatedAt.Before(listing.CreatedAt) {
		return models.ErrSourcePayloadCorrupt
	}
	source, values, err := discoveryMatchSource(conn.Get, listing, target.SourceOrdinal)
	if err != nil {
		return err
	}
	if source.SHA256 != target.SourceSHA256 || source.PostUUID != target.PostUUID {
		return models.ErrSourcePayloadCorrupt
	}
	var revision int
	if err := conn.Get(&revision, "SELECT revision FROM source_posts WHERE uuid=?", target.PostUUID); err != nil {
		return err
	}
	if target.PostRevision > revision {
		return models.ErrSourcePayloadCorrupt
	}
	var count int
	if err := conn.Get(&count, "SELECT count(*) FROM discovery_match_pages WHERE target_uuid=?", target.UUID); err != nil {
		return err
	}
	if count != target.LastPage {
		return models.ErrSourcePayloadCorrupt
	}
	previous, complete := target.CreatedAt, false
	aggregates := map[models.SourcePostIdentifier]models.DiscoveryMatchCandidate{}
	for ordinal := 1; ordinal <= target.LastPage; ordinal++ {
		var pageRow discoveryPageRow
		if err := conn.Get(&pageRow, "SELECT * FROM discovery_pages WHERE listing_uuid=? AND ordinal=?", target.ListingUUID, ordinal); err != nil {
			return err
		}
		page, err := pageRow.resolve()
		if err != nil {
			return err
		}
		matches, err := scrape.MatchDiscoveryPage(values, page.Body)
		if err != nil {
			return err
		}
		digest, err := discoveryMatchesDigest(matches)
		if err != nil {
			return err
		}
		var receipt models.DiscoveryMatchReceipt
		if err := conn.Get(&receipt, "SELECT * FROM discovery_match_pages WHERE target_uuid=? AND page_ordinal=?", target.UUID, ordinal); err != nil {
			return err
		}
		if receipt.ListingUUID != target.ListingUUID || receipt.PageSHA256 != page.Digest || receipt.MatchesSHA256 != digest || receipt.CandidateCount != len(matches) || receipt.Complete != page.Complete ||
			!validJobTime(receipt.CreatedAt) || receipt.CreatedAt.Before(previous) || receipt.CreatedAt.Before(page.CreatedAt) || complete {
			return models.ErrSourcePayloadCorrupt
		}
		var evidence []discoveryMatchEvidenceRow
		if err := conn.Select(&evidence, `SELECT * FROM discovery_match_evidence WHERE target_uuid=? AND page_ordinal=? ORDER BY json_extract(record_ordinals,'$[0]')`, target.UUID, ordinal); err != nil {
			return err
		}
		if len(evidence) != len(matches) {
			return models.ErrSourcePayloadCorrupt
		}
		for i, match := range matches {
			actual, err := evidence[i].resolve()
			if err != nil {
				return err
			}
			expected := models.DiscoveryMatchEvidence{TargetUUID: target.UUID, PageOrdinal: ordinal, Namespace: match.Post.Namespace, Value: match.Post.Value,
				URL: match.URL, Basis: match.Basis, NeedsDetail: match.NeedsDetail, RecordOrdinals: match.RecordOrdinals}
			if !reflect.DeepEqual(expected, *actual) {
				return models.ErrSourcePayloadCorrupt
			}
			candidate, found := aggregates[match.Post]
			if !found {
				candidate = models.DiscoveryMatchCandidate{TargetUUID: target.UUID, Namespace: match.Post.Namespace, Value: match.Post.Value, FirstPage: ordinal, BestPage: ordinal,
					URL: match.URL, Basis: match.Basis, NeedsDetail: match.NeedsDetail}
			} else if candidate.NeedsDetail && !match.NeedsDetail {
				candidate.BestPage, candidate.URL, candidate.Basis, candidate.NeedsDetail = ordinal, match.URL, match.Basis, match.NeedsDetail
			}
			candidate.LastPage, candidate.PageCount = ordinal, candidate.PageCount+1
			aggregates[match.Post] = candidate
		}
		if len(aggregates) > archive.MaxDiscoveryRecords {
			return models.ErrSourcePayloadCorrupt
		}
		previous, complete = receipt.CreatedAt, receipt.Complete
	}
	if !target.UpdatedAt.Equal(previous) || target.EnumerationComplete != complete {
		return models.ErrSourcePayloadCorrupt
	}
	var candidates []models.DiscoveryMatchCandidate
	if err := conn.Select(&candidates, discoveryCandidateSelect+" WHERE c.target_uuid=? LIMIT ?", target.UUID, archive.MaxDiscoveryRecords+1); err != nil {
		return err
	}
	if len(candidates) != len(aggregates) {
		return models.ErrSourcePayloadCorrupt
	}
	for _, actual := range candidates {
		expected, found := aggregates[models.SourcePostIdentifier{Namespace: actual.Namespace, Value: actual.Value}]
		if !found || actual.Sequence < 1 {
			return models.ErrSourcePayloadCorrupt
		}
		expected.Sequence = actual.Sequence
		if !reflect.DeepEqual(expected, actual) {
			return models.ErrSourcePayloadCorrupt
		}
	}
	return nil
}
