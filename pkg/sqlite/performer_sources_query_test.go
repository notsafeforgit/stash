package sqlite

import (
	"fmt"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"
)

func TestPerformerSourcesQueriesStartFromSelectedIdentities(t *testing.T) {
	db, err := sqlx.Open(sqlite3Driver, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = db.Exec(`CREATE TABLE archive_entities(uuid TEXT PRIMARY KEY,redirect_to TEXT);
CREATE INDEX archive_entities_redirect ON archive_entities(redirect_to) WHERE redirect_to IS NOT NULL;
CREATE TABLE source_accounts(uuid TEXT PRIMARY KEY,canonical_uuid TEXT);
CREATE TABLE account_performer_decisions(uuid TEXT PRIMARY KEY,account_uuid TEXT,performer_uuid TEXT,state TEXT);
CREATE INDEX account_performer_decisions_performer ON account_performer_decisions(performer_uuid) WHERE performer_uuid IS NOT NULL;
CREATE TABLE account_performer_links(account_uuid TEXT PRIMARY KEY,decision_uuid TEXT);
INSERT INTO archive_entities VALUES('owner',NULL),('former','owner'),('unrelated',NULL);
INSERT INTO source_accounts VALUES('a','a'),('b','b'),('c','c'),('old-account','a'),('unrelated','unrelated');
INSERT INTO account_performer_decisions VALUES
('d1','a','owner','linked'),('d2','b','former','linked'),('d3','c','owner','linked'),
('d4','c',NULL,'unlinked'),('d5','old-account','former','linked'),('d6','unrelated','unrelated','linked');
INSERT INTO account_performer_links VALUES('a','d1'),('b','d2'),('c','d4'),('old-account','d5'),('unrelated','d6');`)
	require.NoError(t, err)
	var identities []string
	require.NoError(t, db.Select(&identities, performerSourceAliasesQuery, "owner"))
	require.Equal(t, []string{"former", "owner"}, identities)
	query, args := performerSourceAccountsQuery(identities, "", 25)
	var accounts []string
	require.NoError(t, db.Select(&accounts, query, args...))
	require.Equal(t, []string{"a", "b"}, accounts)
	query, args = performerSourceAccountsQuery(identities, "a", 1)
	require.NoError(t, db.Select(&accounts, query, args...))
	require.Equal(t, []string{"b"}, accounts)
	var plans []struct {
		ID, Parent, Notused int
		Detail              string
	}
	require.NoError(t, db.Select(&plans, "EXPLAIN QUERY PLAN "+query, args...))
	plan := fmt.Sprint(plans)
	require.Contains(t, plan, "SEARCH d USING INDEX account_performer_decisions_performer (performer_uuid=?)")
	require.Contains(t, plan, "SEARCH h USING INDEX sqlite_autoindex_account_performer_links_1 (account_uuid=?)")
	require.Contains(t, plan, "SEARCH a USING INDEX sqlite_autoindex_source_accounts_1 (uuid=?)")
	require.NoError(t, db.Select(&plans, "EXPLAIN QUERY PLAN "+performerSourceAliasesQuery, "owner"))
	require.Contains(t, fmt.Sprint(plans), "SEARCH e USING INDEX archive_entities_redirect (redirect_to=?)")
}
