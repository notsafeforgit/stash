package sqlite

import (
	"fmt"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestSourcePostBrowserQueriesStartFromIdentityAndURLIndexes(t *testing.T) {
	db, err := sqlx.Open(sqlite3Driver, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = db.Exec(`CREATE TABLE source_posts(uuid TEXT PRIMARY KEY,state TEXT,revision INTEGER,created_at TEXT);
CREATE TABLE source_post_identities(post_uuid TEXT PRIMARY KEY,canonical_uuid TEXT);
CREATE INDEX source_post_identities_canonical ON source_post_identities(canonical_uuid,post_uuid);
CREATE TABLE source_post_urls(uuid TEXT PRIMARY KEY,post_uuid TEXT,url TEXT,UNIQUE(post_uuid,url));
CREATE INDEX source_post_urls_lookup ON source_post_urls(url,post_uuid);
CREATE TABLE source_post_identifiers(namespace TEXT,value TEXT,post_uuid TEXT,PRIMARY KEY(namespace,value)) WITHOUT ROWID;`)
	require.NoError(t, err)
	for _, tc := range []struct {
		filter models.SourcePostFilter
		index  string
	}{
		{models.SourcePostFilter{After: "cursor", Limit: 25}, "SEARCH i USING COVERING INDEX source_post_identities_canonical (canonical_uuid>?)"},
		{models.SourcePostFilter{PostUUID: "post", Limit: 25}, "SEARCH i USING INDEX sqlite_autoindex_source_post_identities_1 (post_uuid=?)"},
		{models.SourcePostFilter{URL: "https://example.test/post", After: "cursor", Limit: 25}, "SEARCH u USING COVERING INDEX source_post_urls_lookup (url=?)"},
		{models.SourcePostFilter{Identifier: &models.SourcePostIdentifier{Namespace: "native:twitter", Value: "id"}, Limit: 25}, "SEARCH i USING PRIMARY KEY (namespace=? AND value=?)"},
	} {
		query, args := sourcePostBrowserQuery(tc.filter)
		var plans []struct {
			ID, Parent, Notused int
			Detail              string
		}
		require.NoError(t, db.Select(&plans, "EXPLAIN QUERY PLAN "+query, args...))
		plan := fmt.Sprint(plans)
		require.Contains(t, plan, tc.index)
		require.NotContains(t, plan, "SCAN source_posts")
		require.NotContains(t, plan, "SCAN source_post_urls")
	}
}

func TestSourcePostBrowserMediaQueryStartsFromSelectedPost(t *testing.T) {
	db, err := sqlx.Open(sqlite3Driver, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = db.Exec(`CREATE TABLE source_post_identities(post_uuid TEXT PRIMARY KEY,canonical_uuid TEXT);
CREATE INDEX source_post_identities_canonical ON source_post_identities(canonical_uuid,post_uuid);
INSERT INTO source_post_identities VALUES('post','post'),('other','other');
CREATE TABLE source_media_evidence(uuid TEXT PRIMARY KEY,post_uuid TEXT,media_uuid TEXT);
CREATE INDEX source_media_evidence_post ON source_media_evidence(post_uuid,uuid);
CREATE TABLE post_media_links(post_uuid TEXT,media_uuid TEXT,decision_uuid TEXT,PRIMARY KEY(post_uuid,media_uuid));
CREATE TABLE source_attachments(uuid TEXT PRIMARY KEY,post_uuid TEXT,UNIQUE(post_uuid,uuid));
CREATE TABLE attachment_media_links(attachment_uuid TEXT PRIMARY KEY,decision_uuid TEXT);
CREATE TABLE attachment_media_decisions(uuid TEXT PRIMARY KEY,attachment_uuid TEXT,media_uuid TEXT);
INSERT INTO source_media_evidence VALUES('e1','post','candidate'),('e2','post','candidate'),('e3','other','unrelated');
INSERT INTO post_media_links VALUES('post','chosen','d1'),('post','candidate','d2');
INSERT INTO source_attachments VALUES('a','post'),('b','other');
INSERT INTO attachment_media_links VALUES('a','a1'),('b','b1');
INSERT INTO attachment_media_decisions VALUES('a1','a','attached'),('b1','b','unrelated');`)
	require.NoError(t, err)
	args := []interface{}{"post", 10, "post", 10, "post", 10, 10}
	var rows []string
	require.NoError(t, db.Select(&rows, sourcePostBrowserMediaQuery, args...))
	require.ElementsMatch(t, []string{"candidate", "chosen", "attached"}, rows)
	var plans []struct {
		ID, Parent, Notused int
		Detail              string
	}
	require.NoError(t, db.Select(&plans, "EXPLAIN QUERY PLAN "+sourcePostBrowserMediaQuery, args...))
	plan := fmt.Sprint(plans)
	for _, indexed := range []string{
		"SEARCH e USING INDEX source_media_evidence_post (post_uuid=?)",
		"SEARCH l USING COVERING INDEX sqlite_autoindex_post_media_links_1 (post_uuid=?)",
		"SEARCH a USING COVERING INDEX sqlite_autoindex_source_attachments_2 (post_uuid=?)",
	} {
		require.Contains(t, plan, indexed)
	}
}
