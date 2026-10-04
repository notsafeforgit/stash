package sqlite

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

func verifyDiscoveryPublication(get enrichmentGet, selectRows enrichmentSelect, row models.DiscoveryMatchPublication) error {
	if !validSourceRunUUID(row.TargetUUID) || !validJobTime(row.CreatedAt) {
		return models.ErrSourcePayloadCorrupt
	}
	p, err := prepareDiscoveryPublication(get, selectRows, models.DiscoveryPublicationInput{TargetUUID: row.TargetUUID, ExpectedTargetRevision: row.TargetRevision, DetailJobUUID: publicationDetailUUID(&row)})
	if err != nil {
		return fmt.Errorf("%w: discovery publication source: %v", models.ErrSourcePayloadCorrupt, err)
	}
	expected := p.row
	expected.CreatedAt, row.CreatedAt = row.CreatedAt.UTC(), row.CreatedAt.UTC()
	if !reflect.DeepEqual(expected, row) || row.CreatedAt.Before(p.target.UpdatedAt) || row.CreatedAt.Before(p.page.CreatedAt) ||
		(p.detail != nil && row.CreatedAt.Before(p.detail.result.CreatedAt)) {
		return models.ErrSourcePayloadCorrupt
	}
	var revision int
	if err := get(&revision, "SELECT revision FROM source_posts WHERE uuid=?", row.PostUUID); err != nil {
		return err
	}
	if revision <= row.PostRevision {
		return models.ErrSourcePayloadCorrupt
	}
	var records []models.DiscoveryPublishedRecord
	if err := selectRows(&records, "SELECT * FROM discovery_published_records WHERE target_uuid=? ORDER BY ordinal LIMIT ?", row.TargetUUID, archive.MaxDiscoveryRecords+1); err != nil {
		return err
	}
	if len(records) != len(p.records) {
		return models.ErrSourcePayloadCorrupt
	}
	seen := make(map[string]bool)
	for i, original := range p.records {
		if records[i].TargetUUID != row.TargetUUID || records[i].Ordinal != original.Ordinal || records[i].CaptureUUID != original.Input.UUID || row.CreatedAt.Before(original.Input.CapturedAt) {
			return models.ErrSourcePayloadCorrupt
		}
		if seen[original.Input.UUID] {
			continue
		}
		if err := verifyEnrichmentCapture(get, selectRows, original.Input, models.SourcePostIdentifier{Namespace: row.Namespace, Value: row.Value}, p.listing.CollectionUUID, p.listing.CollectionRevision); err != nil {
			return fmt.Errorf("%w: discovery published capture: %v", models.ErrSourcePayloadCorrupt, err)
		}
		seen[original.Input.UUID] = true
	}
	identity, url, err := discoveryPublicationLinks(p)
	if err != nil {
		return err
	}
	if err := normalizePostLink(&identity.SourcePostEvidence); err != nil {
		return err
	}
	if err := normalizePostLink(&url.SourcePostEvidence); err != nil {
		return err
	}
	identifierDigest, err := sourceSignature("stash-post-identifier-evidence-v1", identity)
	if err != nil {
		return err
	}
	urlDigest, err := sourceSignature("stash-post-url-evidence-v1", url)
	if err != nil {
		return err
	}
	for _, check := range []struct {
		input  models.SourcePostEvidence
		query  string
		digest string
	}{
		{identity.SourcePostEvidence, "SELECT * FROM source_post_identifier_evidence WHERE uuid=?", identifierDigest},
		{url.SourcePostEvidence, postURLObservationSelect + " WHERE e.uuid=?", urlDigest},
	} {
		var actual postLinkRow
		if err := get(&actual, check.query, check.input.UUID); err != nil {
			return err
		}
		if actual.UUID != check.input.UUID || actual.PostUUID != check.input.PostUUID || actual.Origin != check.input.Origin || actual.Basis != check.input.Basis ||
			!actual.ObservedAt.Timestamp.Equal(check.input.ObservedAt) || actual.Details != string(check.input.Details) || actual.Digest != check.digest {
			return models.ErrSourcePayloadCorrupt
		}
		if (actual.UUID == identity.UUID && (actual.Namespace != row.Namespace || actual.Value != row.Value)) || (actual.UUID == url.UUID && actual.URL != url.URL) {
			return models.ErrSourcePayloadCorrupt
		}
	}
	return nil
}

func validateDiscoveryPublicationSchema(conn *sqlx.DB, details bool) error {
	names := []string{"discovery_match_publications", "discovery_match_publication_immutable", "discovery_match_publication_scope",
		"discovery_published_records", "discovery_published_records_capture", "discovery_published_record_immutable", "discovery_published_record_scope"}
	if details {
		names = append(names, "discovery_detail_candidate_history", "discovery_publication_detail")
		var columns int
		if err := conn.Get(&columns, "SELECT count(*) FROM pragma_table_info('discovery_match_publications') WHERE name='detail_job_uuid'"); err != nil {
			return err
		}
		if columns != 1 {
			return models.ErrSourcePayloadCorrupt
		}
	}
	for _, name := range names {
		var exists bool
		if err := conn.Get(&exists, "SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name=?)", name); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("native database schema is incomplete: missing %s", name)
		}
	}
	for _, table := range []string{"discovery_match_publications", "discovery_published_records"} {
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
	for after := ""; ; {
		var row models.DiscoveryMatchPublication
		err := conn.Get(&row, "SELECT * FROM discovery_match_publications WHERE target_uuid>? ORDER BY target_uuid LIMIT 1", after)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := verifyDiscoveryPublication(conn.Get, conn.Select, row); err != nil {
			return err
		}
		after = row.TargetUUID
	}
}
