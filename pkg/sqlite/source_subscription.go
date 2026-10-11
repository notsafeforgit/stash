package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
)

type SourceSubscriptionStore struct{}

func subscribable(c *models.SourceCollection) bool {
	if c == nil || c.CanonicalUUID != "" && c.CanonicalUUID != c.UUID || c.TargetURL == "" || c.RootUUID == nil {
		return false
	}
	switch c.Kind {
	case "account", "feed", "subreddit", "search", "collection":
		return true
	}
	return false
}

func (s *SourceSubscriptionStore) Find(ctx context.Context, id string) (*models.SourceSubscription, error) {
	c, err := (&SourceCollectionStore{}).Find(ctx, id)
	if err != nil || c == nil {
		return nil, err
	}
	var row struct {
		Revision int  `db:"revision"`
		Enabled  bool `db:"enabled"`
	}
	if err := dbWrapper.Get(ctx, &row, "SELECT revision,enabled FROM source_subscriptions WHERE collection_uuid=?", id); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return &models.SourceSubscription{Collection: c, Revision: row.Revision, Enabled: row.Enabled}, nil
}

func sourceSubscriptionQuery(ctx context.Context, f models.SourceSubscriptionFilter) (string, []any, error) {
	if f.Limit < 1 || f.Limit > 100 || f.Offset < 0 || f.Offset > 1000000 || f.After != "" && f.Offset != 0 || f.Namespace != "" && !archive.ValidAccountNamespace(f.Namespace) {
		return "", nil, models.ErrSourceDefinitionInvalid
	}
	if f.NamespacePrefix != "" && (f.Namespace != "" || !strings.HasSuffix(f.NamespacePrefix, ":") || !archive.ValidAccountNamespace(f.NamespacePrefix+"example")) {
		return "", nil, models.ErrSourceDefinitionInvalid
	}
	for _, id := range []string{f.CollectionUUID, f.AccountUUID, f.PerformerUUID, f.RootUUID, f.After} {
		if id != "" && !validSourceRunUUID(id) {
			return "", nil, models.ErrSourceDefinitionInvalid
		}
	}
	query := ` FROM source_collections b JOIN source_collection_revisions r ON r.collection_uuid=b.uuid AND r.revision=b.revision
LEFT JOIN source_subscriptions s ON s.collection_uuid=b.uuid
WHERE r.kind IN ('account','feed','subreddit','search','collection') AND r.target_url!='' AND r.root_uuid IS NOT NULL
AND NOT EXISTS(SELECT 1 FROM source_collection_aliases a WHERE a.alias_uuid=b.uuid)`
	args := []any{}
	if f.NamespacePrefix != "" {
		query += " AND r.namespace>=? AND r.namespace<?"
		args = append(args, f.NamespacePrefix, f.NamespacePrefix+"~")
	}
	for _, pair := range [][2]string{{"b.uuid", f.CollectionUUID}, {"r.root_uuid", f.RootUUID}, {"r.namespace", f.Namespace}} {
		if pair[1] != "" {
			query += " AND " + pair[0] + "=?"
			args = append(args, pair[1])
		}
	}
	if f.EnabledOnly {
		query += ` AND s.enabled=1 AND r.state='active'
AND EXISTS(SELECT 1 FROM media_roots m JOIN media_root_revisions mr ON mr.root_uuid=m.uuid AND mr.revision=m.revision WHERE m.uuid=r.root_uuid AND mr.state='active')`
	}
	if f.AccountUUID != "" {
		query += ` AND r.account_uuid IN (SELECT uuid FROM source_accounts WHERE canonical_uuid=(SELECT canonical_uuid FROM source_accounts WHERE uuid=?))`
		args = append(args, f.AccountUUID)
	}
	if f.PerformerUUID != "" {
		performer, ids, err := performerSourceScope(ctx, f.PerformerUUID, "", 100)
		if err != nil {
			return "", nil, err
		}
		if performer == nil {
			query += " AND 0"
		} else {
			query += ` AND r.account_uuid IN (SELECT a.uuid FROM account_performer_decisions d
JOIN account_performer_links h ON h.account_uuid=d.account_uuid AND h.decision_uuid=d.uuid
JOIN source_accounts a ON a.canonical_uuid=h.account_uuid
WHERE d.state='linked' AND d.performer_uuid IN ` + getInBinding(len(ids)) + `)`
			for _, id := range ids {
				args = append(args, id)
			}
		}
	}
	if f.AllowedCollections != nil {
		if len(f.AllowedCollections) == 0 {
			query += " AND 0"
		} else {
			query += " AND b.uuid IN " + getInBinding(len(f.AllowedCollections))
			for _, id := range f.AllowedCollections {
				args = append(args, id)
			}
		}
	}
	return query, args, nil
}

func (s *SourceSubscriptionStore) List(ctx context.Context, f models.SourceSubscriptionFilter) (*models.SourceSubscriptionPage, error) {
	query, args, err := sourceSubscriptionQuery(ctx, f)
	if err != nil {
		return nil, err
	}
	ret := &models.SourceSubscriptionPage{Items: []models.SourceSubscription{}}
	if err := dbWrapper.Get(ctx, &ret.Total, "SELECT count(*)"+query, args...); err != nil {
		return nil, err
	}
	if f.After != "" {
		query += " AND b.uuid>?"
		args = append(args, f.After)
	}
	args = append(args, f.Limit, f.Offset)
	var ids []string
	if err := dbWrapper.Select(ctx, &ids, "SELECT b.uuid"+query+" ORDER BY b.uuid LIMIT ? OFFSET ?", args...); err != nil {
		return nil, err
	}
	for _, id := range ids {
		item, err := s.Find(ctx, id)
		if err != nil {
			return nil, err
		}
		ret.Items = append(ret.Items, *item)
	}
	return ret, nil
}

func (s *SourceSubscriptionStore) Put(ctx context.Context, id string, input models.SourceSubscriptionInput) (*models.SourceSubscription, error) {
	if input.ExpectedRevision < 0 {
		return nil, models.ErrSourceDefinitionInvalid
	}
	current, err := s.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if current == nil || !subscribable(current.Collection) {
		return nil, models.ErrSourceDefinitionInvalid
	}
	if current.Revision != input.ExpectedRevision {
		return nil, models.ErrSourceDefinitionConflict
	}
	if input.Enabled && current.Collection.State != "active" {
		return nil, models.ErrSourceDefinitionConflict
	}
	if current.Enabled == input.Enabled {
		return current, nil
	}
	_, err = dbWrapper.Exec(ctx, `INSERT INTO source_subscriptions(collection_uuid,revision,enabled) VALUES(?,?,?)
ON CONFLICT(collection_uuid) DO UPDATE SET revision=excluded.revision,enabled=excluded.enabled`, id, current.Revision+1, input.Enabled)
	if err != nil {
		return nil, err
	}
	if !input.Enabled {
		// Keep the current attempt and its intake lease intact. Queued work and
		// subsequent attempts are cancelled; already captured data can still arrive.
		if err := cancelSubscribedRuns(ctx, id, time.Now()); err != nil {
			return nil, err
		}
	}
	return s.Find(ctx, id)
}

func (s *SourceSubscriptionStore) StopPerformer(ctx context.Context, id string) (int, error) {
	// Exact current ownership, including performer/account merges. A name or a
	// depiction in media never authorizes stopping somebody else's subscription.
	query, args, err := sourceSubscriptionQuery(ctx, models.SourceSubscriptionFilter{PerformerUUID: id, Limit: 100})
	if err != nil {
		return 0, err
	}
	if strings.TrimSpace(id) == "" {
		return 0, models.ErrSourceDefinitionInvalid
	}
	var ids []string
	if err := dbWrapper.Select(ctx, &ids, "SELECT b.uuid"+query+" AND s.enabled=1 LIMIT 10001", args...); err != nil {
		return 0, err
	}
	if len(ids) > 10000 {
		return 0, models.ErrSourceDefinitionInvalid
	}
	for _, collection := range ids {
		current, err := s.Find(ctx, collection)
		if err != nil {
			return 0, err
		}
		if _, err := s.Put(ctx, collection, models.SourceSubscriptionInput{ExpectedRevision: current.Revision, Enabled: false}); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

func cancelSubscribedRuns(ctx context.Context, collection string, now time.Time) error {
	// Indexed by collection; unrelated library data and manual runs are untouched.
	_, err := dbWrapper.Exec(ctx, `UPDATE source_runs SET state='cancelled',error_code='monitoring_stopped',revision=revision+1,updated_at_ms=?
WHERE uuid IN (SELECT run_uuid FROM source_run_subscriptions WHERE collection_uuid=?) AND state IN ('queued','deferred')`, now.UnixMilli(), collection)
	return err
}

func sourceRunSubscriptionActive(ctx context.Context, id string) (bool, error) {
	var active bool
	err := dbWrapper.Get(ctx, &active, `SELECT NOT EXISTS(SELECT 1 FROM source_run_subscriptions b
LEFT JOIN source_subscriptions s ON s.collection_uuid=b.collection_uuid
WHERE b.run_uuid=? AND (s.enabled IS NOT 1 OR s.revision!=b.revision))`, id)
	return active, err
}

func (s *SourceSubscriptionStore) BindRun(ctx context.Context, id string, input models.SourceRunSubscriptionInput) error {
	if !validSourceRunUUID(id) || input.ExpectedRunRevision < 1 || input.SubscriptionRevision < 1 {
		return models.ErrSourceDefinitionInvalid
	}
	run, err := (&SourceRunStore{}).Find(ctx, id)
	if err != nil {
		return err
	}
	if run == nil {
		return models.ErrSourceDefinitionInvalid
	}
	var bound int
	err = dbWrapper.Get(ctx, &bound, "SELECT revision FROM source_run_subscriptions WHERE run_uuid=?", id)
	if err == nil {
		if bound != input.SubscriptionRevision {
			return models.ErrSourceDefinitionConflict
		}
		return nil // The immutable association is the receipt for a lost response.
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if run.Revision != input.ExpectedRunRevision || run.State != "queued" && run.State != "deferred" {
		return models.ErrSourceDefinitionConflict
	}
	subscription, err := s.Find(ctx, run.CollectionUUID)
	if err != nil {
		return err
	}
	if subscription == nil || !subscription.Enabled || subscription.Revision != input.SubscriptionRevision {
		return models.ErrSourceDefinitionConflict
	}
	_, err = dbWrapper.Exec(ctx, "INSERT INTO source_run_subscriptions(run_uuid,collection_uuid,revision) VALUES(?,?,?)", id, run.CollectionUUID, input.SubscriptionRevision)
	return err
}
