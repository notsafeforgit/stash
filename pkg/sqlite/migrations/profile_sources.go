package migrations

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/scrape"
	"github.com/stashapp/stash/pkg/sqlite"
)

func init() { sqlite.RegisterPostMigration(1000106, consolidateRedditProfiles) }

type profileSourceRow struct {
	UUID            string         `db:"uuid"`
	Target          string         `db:"target_url"`
	State           string         `db:"state"`
	Root            sql.NullString `db:"root_uuid"`
	Account         sql.NullString `db:"account_uuid"`
	Prefix          string         `db:"path_prefix"`
	Metadata        sql.NullString `db:"metadata"`
	Translation     sql.NullString `db:"translation"`
	CurrentPolicies bool           `db:"current_policies"`
}

// Only equivalent subscriptions are consolidated. Conflicting scope, account,
// state or policies remain visible for review instead of being silently chosen.
func consolidateRedditProfiles(ctx context.Context, db *sqlx.DB) error {
	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var rows []profileSourceRow
	err = tx.SelectContext(ctx, &rows, `SELECT c.uuid,r.target_url,r.state,r.root_uuid,r.account_uuid,r.path_prefix,
m.definition AS metadata,t.definition AS translation,
(m.collection_uuid IS NULL OR m.collection_revision=c.revision) AND (t.collection_uuid IS NULL OR t.collection_revision=c.revision) AS current_policies
FROM source_collections c JOIN source_collection_revisions r ON r.collection_uuid=c.uuid AND r.revision=c.revision
LEFT JOIN metadata_policies mp ON mp.collection_uuid=c.uuid
LEFT JOIN metadata_policy_revisions m ON m.collection_uuid=mp.collection_uuid AND m.revision=mp.revision
LEFT JOIN translation_policies tp ON tp.collection_uuid=c.uuid
LEFT JOIN translation_policy_revisions t ON t.collection_uuid=tp.collection_uuid AND t.revision=tp.revision
WHERE r.namespace='native:reddit' AND r.target_url!=''
AND NOT EXISTS(SELECT 1 FROM source_collection_aliases a WHERE a.alias_uuid=c.uuid) ORDER BY c.uuid`)
	if err != nil {
		return err
	}
	groups := map[string][]profileSourceRow{}
	for _, row := range rows {
		if profile := scrape.RedditProfileURL(row.Target); profile != "" {
			key, err := json.Marshal([]string{profile, row.Root.String, row.Prefix})
			if err != nil {
				return err
			}
			groups[string(key)] = append(groups[string(key)], row)
		}
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	profiles, aliases, conflicts := 0, 0, 0
	for _, key := range keys {
		group := groups[key]
		first := group[0]
		profile := scrape.RedditProfileURL(first.Target)
		if len(group) == 1 && first.Target == profile {
			continue
		}
		valid := first.CurrentPolicies
		for _, row := range group {
			if !row.CurrentPolicies || row.State != first.State || row.Account != first.Account || row.Metadata != first.Metadata || row.Translation != first.Translation {
				valid = false
			}
		}
		if !valid || first.State == "retired" || len(group) > 128 {
			conflicts++
			continue
		}
		id := uuid.NewSHA1(uuid.NameSpaceURL, []byte("stash:reddit-profile:"+key)).String()
		if _, err = tx.ExecContext(ctx, "INSERT INTO source_collections(uuid) VALUES(?)", id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO source_collection_revisions(collection_uuid,revision,label,kind,namespace,state,target_url,account_uuid,root_uuid,path_prefix,origin,reason)
VALUES(?,1,?,'account','native:reddit',?,?,?,?,?,'migration','Consolidated standard Reddit profile retrieval passes')`, id, "reddit: "+profile, first.State, profile, first.Account, first.Root, first.Prefix)
		if err != nil {
			return err
		}
		for _, policy := range []struct {
			name  string
			value sql.NullString
		}{{"metadata", first.Metadata}, {"translation", first.Translation}} {
			if !policy.value.Valid {
				continue
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO "+policy.name+"_policies(collection_uuid) VALUES(?)", id); err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s_policy_revisions(collection_uuid,revision,collection_revision,definition,origin,reason)
VALUES(?,1,1,?,'migration','Preserved equivalent profile retrieval policy')`, policy.name), id, policy.value.String)
			if err != nil {
				return err
			}
		}
		for _, row := range group {
			if _, err = tx.ExecContext(ctx, "INSERT INTO source_collection_aliases(alias_uuid,source_uuid) VALUES(?,?)", row.UUID, id); err != nil {
				return err
			}
			aliases++
		}
		profiles++
	}
	details, err := json.Marshal(map[string]int{"profiles": profiles, "historical_aliases": aliases, "conflicting_groups": conflicts})
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE native_migration_history SET details=? WHERE version=1000106", string(details)); err != nil {
		return err
	}
	return tx.Commit()
}
