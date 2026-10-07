package sqlite

import (
	"context"

	"github.com/stashapp/stash/pkg/models"
)

// Read normalized immutable bindings, not worker JSON. File and album work have
// owning-service readers for their versioned admission/publication formats.
func (s *ArchiveActivityStore) JobReferences(ctx context.Context, id string) ([]models.ArchiveActivityReference, error) {
	current, err := s.Job(ctx, id)
	if err != nil {
		return nil, err
	}
	ret := []models.ArchiveActivityReference{}
	if current == nil {
		return ret, nil
	}
	var bound string
	switch current.Kind {
	case models.ArchiveJobTranslateText:
		bound = `SELECT t.post_uuid,t.collection_uuid,t.collection_revision FROM translation_job_targets b
JOIN translation_targets t ON t.uuid=b.target_uuid WHERE b.job_uuid=?`
	case models.ArchiveJobEnrichPost:
		bound = `SELECT t.post_uuid,t.collection_uuid,t.collection_revision FROM enrichment_job_targets b
JOIN enrichment_targets t ON t.uuid=b.target_uuid WHERE b.job_uuid=?`
	case models.ArchiveJobListAccount:
		bound = `SELECT NULL AS post_uuid,d.collection_uuid,d.collection_revision FROM discovery_listing_jobs b
JOIN discovery_listings d ON d.uuid=b.listing_uuid WHERE b.job_uuid=?`
	case models.ArchiveJobVerifyCandidate:
		bound = `SELECT t.post_uuid,d.collection_uuid,d.collection_revision FROM discovery_detail_jobs b
JOIN discovery_match_targets t ON t.uuid=b.target_uuid JOIN discovery_listings d ON d.uuid=t.listing_uuid WHERE b.job_uuid=?`
	default:
		return ret, nil
	}
	query := `WITH bound AS (` + bound + `) SELECT 'post' AS kind,post_uuid AS uuid,0 AS revision FROM bound WHERE post_uuid IS NOT NULL
UNION SELECT 'collection',collection_uuid,collection_revision FROM bound WHERE collection_uuid IS NOT NULL ORDER BY kind,uuid,revision LIMIT 101`
	if err := dbWrapper.Select(ctx, &ret, query, id); err != nil {
		return nil, err
	}
	if len(ret) > 100 {
		return nil, models.ErrSourcePayloadCorrupt
	}
	return ret, nil
}
