package sqlite

import (
	"context"
	"net/url"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

func validateSourcePostFilter(filter models.SourcePostFilter) error {
	if filter.Limit < 1 || filter.Limit > 100 || (filter.After != "" && !validSourceRunUUID(filter.After)) {
		return models.ErrSourcePostBrowseInvalid
	}
	selectors := 0
	if filter.PostUUID != "" {
		if !validSourceRunUUID(filter.PostUUID) {
			return models.ErrSourcePostBrowseInvalid
		}
		selectors++
	}
	if filter.URL != "" {
		parsed, err := url.Parse(filter.URL)
		if !validAccountText(filter.URL, 8192, false) || strings.TrimSpace(filter.URL) != filter.URL || err != nil ||
			(parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil {
			return models.ErrSourcePostBrowseInvalid
		}
		selectors++
	}
	if filter.Identifier != nil {
		if validatePostIdentifier(*filter.Identifier) != nil {
			return models.ErrSourcePostBrowseInvalid
		}
		selectors++
	}
	if selectors > 1 {
		return models.ErrSourcePostBrowseInvalid
	}
	return nil
}

// A cursor always advances through post UUIDs. Exact URL lookup starts at the
// URL-leading index and preserves multiple posts with the same retained URL;
// it must never collapse them into an invented identity match.
func sourcePostBrowserQuery(filter models.SourcePostFilter) (string, []interface{}) {
	if filter.PostUUID != "" {
		return `SELECT * FROM source_posts WHERE uuid=? AND uuid>? ORDER BY uuid LIMIT ?`,
			[]interface{}{filter.PostUUID, filter.After, filter.Limit}
	}
	if filter.URL != "" {
		return `SELECT p.* FROM (
SELECT post_uuid FROM source_post_urls INDEXED BY source_post_urls_lookup
WHERE url=? AND post_uuid>? ORDER BY post_uuid LIMIT ?
) matches JOIN source_posts p ON p.uuid=matches.post_uuid ORDER BY p.uuid`,
			[]interface{}{filter.URL, filter.After, filter.Limit}
	}
	if filter.Identifier != nil {
		return `SELECT p.* FROM source_post_identifiers i JOIN source_posts p ON p.uuid=i.post_uuid
WHERE i.namespace=? AND i.value=? AND i.post_uuid>? ORDER BY p.uuid LIMIT ?`,
			[]interface{}{filter.Identifier.Namespace, filter.Identifier.Value, filter.After, filter.Limit}
	}
	return `SELECT * FROM source_posts WHERE uuid>? ORDER BY uuid LIMIT ?`, []interface{}{filter.After, filter.Limit}
}

func (s *SourceEvidenceStore) BrowsePosts(ctx context.Context, filter models.SourcePostFilter) ([]models.SourcePostSummary, error) {
	if err := validateSourcePostFilter(filter); err != nil {
		return nil, err
	}
	query, args := sourcePostBrowserQuery(filter)
	var rows []sourcePostRow
	if err := dbWrapper.Select(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	result := make([]models.SourcePostSummary, 0, len(rows))
	for _, row := range rows {
		summary, err := s.postSummary(ctx, row.resolve())
		if err != nil {
			return nil, err
		}
		result = append(result, *summary)
	}
	return result, nil
}

func (s *SourceEvidenceStore) PostSummary(ctx context.Context, id string) (*models.SourcePostSummary, error) {
	if !validSourceRunUUID(id) {
		return nil, models.ErrSourcePostBrowseInvalid
	}
	post, err := s.FindPost(ctx, id)
	if err != nil || post == nil {
		return nil, err
	}
	return s.postSummary(ctx, post)
}

func (s *SourceEvidenceStore) postSummary(ctx context.Context, post *models.SourcePost) (*models.SourcePostSummary, error) {
	identifiers, err := s.PostIdentifiers(ctx, post.UUID, nil, 4)
	if err != nil {
		return nil, err
	}
	result := &models.SourcePostSummary{UUID: post.UUID, State: post.State, Revision: post.Revision,
		CreatedAt: post.CreatedAt, Identifiers: []models.SourcePostIdentifierSummary{}}
	if len(identifiers) > 3 {
		identifiers, result.MoreIdentifiers = identifiers[:3], true
	}
	for _, identifier := range identifiers {
		result.Identifiers = append(result.Identifiers, models.SourcePostIdentifierSummary(identifier))
	}
	result.URLs, err = (&SourcePostLinksStore{}).URLs(ctx, post.UUID, "", 4)
	if err != nil {
		return nil, err
	}
	if len(result.URLs) > 3 {
		result.URLs, result.MoreURLs = result.URLs[:3], true
	}
	result.LatestCapture, err = sourceReviewLatestCapture(ctx, post.UUID)
	if err != nil {
		return nil, err
	}
	return result, nil
}
