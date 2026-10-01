package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"reflect"
	"strings"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

type SourceCollectionStore struct{}

type sourceCollectionRow struct {
	UUID        string    `db:"uuid"`
	Revision    int       `db:"revision"`
	Label       string    `db:"label"`
	State       string    `db:"state"`
	Kind        string    `db:"kind"`
	Namespace   string    `db:"namespace"`
	TargetURL   string    `db:"target_url"`
	AccountUUID *string   `db:"account_uuid"`
	RootUUID    *string   `db:"root_uuid"`
	PathPrefix  string    `db:"path_prefix"`
	Origin      string    `db:"origin"`
	Reason      string    `db:"reason"`
	CreatedAt   Timestamp `db:"created_at"`
	RecordedAt  Timestamp `db:"recorded_at"`
}

const sourceCollectionSelect = `SELECT b.uuid,b.created_at,r.revision,r.label,r.state,r.kind,r.namespace,r.target_url,r.account_uuid,r.root_uuid,r.path_prefix,r.origin,r.reason,r.created_at AS recorded_at
FROM source_collections b JOIN source_collection_revisions r ON r.collection_uuid=b.uuid`

func (r sourceCollectionRow) resolve() *models.SourceCollection {
	return &models.SourceCollection{UUID: r.UUID, Revision: r.Revision, CreatedAt: r.CreatedAt.Timestamp,
		SourceCollectionDefinition: models.SourceCollectionDefinition{Label: r.Label, Kind: r.Kind, Namespace: r.Namespace, State: r.State,
			TargetURL: r.TargetURL, AccountUUID: r.AccountUUID, RootUUID: r.RootUUID, PathPrefix: r.PathPrefix}}
}

func validCollectionURL(value string) bool {
	if !validAccountText(value, 8192, false) || strings.TrimSpace(value) != value {
		return false
	}
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Hostname() != "" && parsed.User == nil
}

func normalizeCollectionDefinition(input *models.SourceCollectionInput) error {
	if !sourceDefinitionText(input.Label, input.State, input.Reason) ||
		(input.Origin != "review" && input.Origin != "migration" && input.Origin != "ingest") ||
		(input.Namespace != "" && !archive.ValidAccountNamespace(input.Namespace)) ||
		(input.TargetURL != "" && !validCollectionURL(input.TargetURL)) {
		return errors.New("invalid source collection definition")
	}
	switch input.Kind {
	case "account", "feed", "subreddit", "search", "manual_batch", "directory", "legacy_catalog", "collection":
	default:
		return errors.New("invalid source collection kind")
	}
	if input.AccountUUID != nil {
		value, err := archiveUUID(*input.AccountUUID)
		if err != nil {
			return err
		}
		input.AccountUUID = &value
		if input.Namespace == "" {
			return errors.New("account collection requires a qualified namespace")
		}
	}
	if input.RootUUID == nil {
		if input.PathPrefix != "" {
			return errors.New("collection path prefix requires a media root")
		}
	} else {
		value, err := archiveUUID(*input.RootUUID)
		if err != nil {
			return err
		}
		input.RootUUID = &value
		if !archive.ValidRootRelativePath(input.PathPrefix, true) {
			return errors.New("invalid collection path prefix")
		}
	}
	return nil
}

func (s *SourceCollectionStore) Put(ctx context.Context, input models.SourceCollectionInput) (*models.SourceCollection, error) {
	id, err := sourceDefinitionID(input.UUID, input.ExpectedRevision)
	if err != nil {
		return nil, err
	}
	if err := normalizeCollectionDefinition(&input); err != nil {
		return nil, err
	}
	current, err := s.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if (current == nil && input.ExpectedRevision != 0) || (current != nil && (current.Revision != input.ExpectedRevision || current.State == "retired")) {
		return nil, models.ErrSourceDefinitionConflict
	}
	if current != nil && reflect.DeepEqual(current.SourceCollectionDefinition, input.SourceCollectionDefinition) {
		return current, nil
	}
	if current == nil {
		if _, err := dbWrapper.Exec(ctx, "INSERT INTO source_collections(uuid) VALUES(?)", id); err != nil {
			return nil, err
		}
	}
	if _, err := dbWrapper.Exec(ctx, `INSERT INTO source_collection_revisions(collection_uuid,revision,label,kind,namespace,state,target_url,account_uuid,root_uuid,path_prefix,origin,reason)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, id, input.ExpectedRevision+1, input.Label, input.Kind, input.Namespace, input.State, input.TargetURL, input.AccountUUID, input.RootUUID, input.PathPrefix, input.Origin, input.Reason); err != nil {
		return nil, err
	}
	return s.Find(ctx, id)
}

func (s *SourceCollectionStore) Find(ctx context.Context, value string) (*models.SourceCollection, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	var row sourceCollectionRow
	if err := dbWrapper.Get(ctx, &row, sourceCollectionSelect+" WHERE b.uuid=? AND r.revision=b.revision", id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return row.resolve(), nil
}

func resolveCollections(rows []sourceCollectionRow) []*models.SourceCollection {
	ret := make([]*models.SourceCollection, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, row.resolve())
	}
	return ret
}

func (s *SourceCollectionStore) List(ctx context.Context, after string, limit int) ([]*models.SourceCollection, error) {
	after, limit, err := sourceDefinitionPage(after, limit)
	if err != nil {
		return nil, err
	}
	var rows []sourceCollectionRow
	if err := dbWrapper.Select(ctx, &rows, sourceCollectionSelect+" WHERE b.uuid>? AND r.revision=b.revision ORDER BY b.uuid LIMIT ?", after, limit); err != nil {
		return nil, err
	}
	return resolveCollections(rows), nil
}

// LookupTarget returns current definitions that have used this exact URL at any
// revision. URLs are evidence, not unique identities or authority to merge.
func (s *SourceCollectionStore) LookupTarget(ctx context.Context, target, after string, limit int) ([]*models.SourceCollection, error) {
	if !validCollectionURL(target) {
		return nil, errors.New("invalid collection target URL")
	}
	after, limit, err := sourceDefinitionPage(after, limit)
	if err != nil {
		return nil, err
	}
	var rows []sourceCollectionRow
	if err := dbWrapper.Select(ctx, &rows, sourceCollectionSelect+` WHERE r.revision=b.revision AND b.uuid IN (
 SELECT collection_uuid FROM source_collection_revisions WHERE target_url=? AND target_url!='' AND collection_uuid>?
 GROUP BY collection_uuid ORDER BY collection_uuid LIMIT ?) ORDER BY b.uuid`, target, after, limit); err != nil {
		return nil, err
	}
	return resolveCollections(rows), nil
}

// LookupCurrentTargets resolves only the current exact URL and root within an
// explicit collection or root grant. Historical URLs are review evidence, not authority
// to redirect a scheduled scrape to a different target or scan order.
func (s *SourceCollectionStore) LookupCurrentTargets(ctx context.Context, targets, collections []string, root *string, allCollections bool) ([]*models.SourceCollection, error) {
	if len(targets) < 1 || len(targets) > 50 || (len(collections) < 1 && !allCollections) || len(collections) > 128 || (allCollections && root == nil) {
		return nil, errors.New("invalid bounded collection lookup")
	}
	for _, target := range targets {
		if !validCollectionURL(target) {
			return nil, errors.New("invalid collection target URL")
		}
	}
	for _, collection := range collections {
		if _, err := archiveUUID(collection); err != nil {
			return nil, err
		}
	}
	if root != nil {
		if _, err := archiveUUID(*root); err != nil {
			return nil, err
		}
	}
	collectionJSON, err := json.Marshal(collections)
	if err != nil {
		return nil, err
	}
	ret := make([]*models.SourceCollection, 0)
	for _, target := range targets {
		query := sourceCollectionSelect + " WHERE r.revision=b.revision AND r.target_url!='' AND r.target_url=? AND r.root_uuid IS ?"
		args := []any{target, root}
		if !allCollections {
			query += " AND b.uuid IN (SELECT value FROM json_each(?))"
			args = append(args, collectionJSON)
		}
		// One extra candidate proves truncation. Never let a partial match set
		// look unique, and never page unrelated targets before applying scope.
		query += " ORDER BY b.uuid LIMIT 129"
		var rows []sourceCollectionRow
		if err := dbWrapper.Select(ctx, &rows, query, args...); err != nil {
			return nil, err
		}
		ret = append(ret, resolveCollections(rows)...)
	}
	return ret, nil
}

func (s *SourceCollectionStore) History(ctx context.Context, value string, after, limit int) ([]models.SourceCollectionRevision, error) {
	id, err := archiveUUID(value)
	if err != nil {
		return nil, err
	}
	if after < 0 {
		return nil, errors.New("invalid collection revision cursor")
	}
	limit, err = sourcePageLimit(limit)
	if err != nil {
		return nil, err
	}
	var rows []sourceCollectionRow
	if err := dbWrapper.Select(ctx, &rows, sourceCollectionSelect+" WHERE b.uuid=? AND r.revision>? ORDER BY r.revision LIMIT ?", id, after, limit); err != nil {
		return nil, err
	}
	ret := make([]models.SourceCollectionRevision, 0, len(rows))
	for _, row := range rows {
		ret = append(ret, models.SourceCollectionRevision{SourceCollection: *row.resolve(), Origin: row.Origin, Reason: row.Reason, RecordedAt: row.RecordedAt.Timestamp})
	}
	return ret, nil
}
