package sqlite

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/models"
)

type ArchivePaginationStore struct{}

// Reuse the list's validated predicate, removing only its final ORDER/LIMIT.
// The URL post lookup has an inner limit as well; -1 leaves it unbounded.
func unboundedArchivePage(query string, args []any) (string, []any) {
	if strings.HasSuffix(query, " LIMIT ?") {
		query = query[:strings.LastIndex(query, " ORDER BY ")]
		args = args[:len(args)-1]
	} else if at := strings.LastIndex(query, " ORDER BY "); at >= 0 {
		query = query[:at]
		args[len(args)-1] = -1
	}
	return query, args
}

func (s *ArchivePaginationStore) PageInfo(ctx context.Context, f models.ArchivePageFilter) (*models.ArchivePageInfo, error) {
	var query string
	var args []any
	var err error
	column, descending, numeric := "uuid", false, false
	if f.Subject != "" && !validSourceRunUUID(f.Subject) {
		return nil, models.ErrArchivePageInvalid
	}
	switch f.Resource {
	case "media-roots", "collections":
		where, a, e := sourceDefinitionSearch(models.SourceDefinitionFilter{Limit: 1, Query: f.Query, State: f.State, Kind: f.Kind}, f.Resource == "collections")
		query, args, err = mediaRootSelect+where, a, e
		if f.Resource == "collections" {
			query = sourceCollectionSelect + where
		}
		if err == nil {
			query, args = unboundedArchivePage(query, args)
		}
	case "accounts", "review-accounts":
		ownership := models.AccountOwnershipState(f.Ownership)
		if f.Resource == "review-accounts" {
			ownership = models.AccountOwnershipUndecided
		}
		query, args, err = accountReviewQuery(models.AccountReviewFilter{Limit: 1, Query: f.Query, Namespace: f.Namespace, Ownership: ownership, Scope: f.Scope})
		if err == nil {
			query, args = unboundedArchivePage(query, args)
		}
	case "posts":
		filter := models.SourcePostFilter{Limit: 1, PostUUID: f.PostUUID, URL: f.URL}
		if f.Namespace != "" || f.Value != "" {
			filter.Identifier = &models.SourcePostIdentifier{Namespace: f.Namespace, Value: f.Value}
		}
		err = validateSourcePostFilter(filter)
		if err == nil {
			query, args = sourcePostBrowserQuery(filter)
			query, args = unboundedArchivePage(query, args)
		}
	case "jobs":
		query, args, err = jobActivityQuery(models.ArchiveJobActivityFilter{ArchiveActivityPage: models.ArchiveActivityPage{Limit: 1}, Kind: f.Kind, State: f.State})
		if err == nil {
			query, args = unboundedArchivePage(query, args)
		}
		column, descending, numeric = "id", true, true
	case "runs":
		// Canonical collection activity includes the retained retrieval aliases.
		filter := models.SourceRunActivityFilter{ArchiveActivityPage: models.ArchiveActivityPage{Limit: 1}, State: f.State}
		if f.Collection != "" && !validSourceRunUUID(f.Collection) {
			return nil, models.ErrArchivePageInvalid
		}
		query, args, err = runActivityQuery(filter)
		if err == nil {
			query, args = unboundedArchivePage(query, args)
			if f.Collection != "" {
				if f.State == "" {
					query += " WHERE "
				} else {
					query += " AND "
				}
				query += "r.collection_uuid IN (SELECT ? UNION SELECT alias_uuid FROM source_collection_aliases WHERE source_uuid=?)"
				args = append(args, f.Collection, f.Collection)
			}
		}
		column, descending, numeric = "id", true, true
	case "imports":
		query, err = importSnapshotSelect(f.Kind)
	case "shares":
		var active *bool
		if f.State != "" {
			if f.State != "ACTIVE" && f.State != "INACTIVE" {
				return nil, models.ErrArchivePageInvalid
			}
			value := f.State == "ACTIVE"
			active = &value
		}
		query, args = shareListPredicate(models.ShareListOptions{Active: active, Now: time.Now().Unix()})
		result := &models.ArchivePageInfo{}
		if f.Cursor != "" {
			result.Offset, err = strconv.ParseInt(f.Cursor, 10, 64)
		}
		if err != nil || result.Offset < 0 {
			return nil, models.ErrArchivePageInvalid
		}
		err = dbWrapper.Get(ctx, &result.Total, "SELECT count(*) FROM ("+query+")", args...)
		result.Offset = min(result.Offset, result.Total)
		return result, err

	case "root-history", "collection-history", "policy-history", "provider-history", "job-attempts", "run-attempts":
		if f.Subject == "" {
			return nil, models.ErrArchivePageInvalid
		}
		numeric = true
		table, key := "media_root_revisions", "root_uuid"
		column = "revision"
		switch f.Resource {
		case "collection-history":
			table, key = "source_collection_revisions", "collection_uuid"
		case "policy-history":
			table, key = "metadata_policy_revisions", "collection_uuid"
		case "provider-history":
			table, key, column = "provider_metadata_imports", "entity_uuid", "sequence"
		case "job-attempts":
			table, key, column, descending = "archive_job_attempts", "job_uuid", "fence", true
		case "run-attempts":
			table, key, column, descending = "source_run_attempts", "run_uuid", "fence", true
		}
		query, args = "SELECT "+column+" FROM "+table+" WHERE "+key+"=?", []any{f.Subject}
	case "review-metadata":
		query, args = unboundedArchivePage(archiveReviewMetadataQuery, []any{"", "", 1})
	case "review-media":
		query, args = archiveReviewMediaItemsQuery("", -1)
		column = "post_uuid"
	default:
		return nil, models.ErrArchivePageInvalid
	}
	if err != nil {
		return nil, models.ErrArchivePageInvalid
	}
	var cursor any = f.Cursor
	if numeric {
		n := int64(0)
		if f.Cursor != "" {
			n, err = strconv.ParseInt(f.Cursor, 10, 64)
		}
		if err != nil || n < 0 {
			return nil, models.ErrArchivePageInvalid
		}
		cursor = n
	} else if f.Cursor != "" && !validSourceRunUUID(f.Cursor) {
		return nil, models.ErrArchivePageInvalid
	}
	operator := "<="
	if descending {
		operator = ">="
	}
	// A zero/empty cursor denotes the first page in both directions.
	position := "0"
	if f.Cursor != "" && f.Cursor != "0" {
		position = "coalesce(sum(" + column + operator + "?),0)"
		args = append([]any{cursor}, args...)
	}
	result := &models.ArchivePageInfo{}
	err = dbWrapper.Get(ctx, result, "SELECT count(*) AS total,"+position+" AS offset FROM ("+query+")", args...)
	return result, err
}
