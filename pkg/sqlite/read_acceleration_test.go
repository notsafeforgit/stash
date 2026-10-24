//go:build sqlite_fts5

package sqlite

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

const searchTestSchema = `
CREATE TABLE scenes (id INTEGER PRIMARY KEY, title TEXT, details TEXT, rating INTEGER);
CREATE TABLE images (id INTEGER PRIMARY KEY, title TEXT, details TEXT, rating INTEGER);
CREATE TABLE folders (id INTEGER PRIMARY KEY, path TEXT);
CREATE TABLE files (id INTEGER PRIMARY KEY, parent_folder_id INTEGER REFERENCES folders(id), basename TEXT, size INTEGER);
CREATE TABLE scenes_files (scene_id INTEGER REFERENCES scenes(id) ON DELETE CASCADE, file_id INTEGER REFERENCES files(id) ON DELETE CASCADE);
CREATE TABLE images_files (image_id INTEGER REFERENCES images(id) ON DELETE CASCADE, file_id INTEGER REFERENCES files(id) ON DELETE CASCADE);
CREATE TABLE files_fingerprints (file_id INTEGER REFERENCES files(id) ON DELETE CASCADE, fingerprint TEXT);
CREATE TABLE scene_markers (scene_id INTEGER REFERENCES scenes(id) ON DELETE CASCADE, title TEXT);
CREATE TABLE worker_bookkeeping (id INTEGER PRIMARY KEY, state TEXT);
INSERT INTO scenes VALUES (1, 'alpha beta', NULL, 0), (2, '', 'detail', 0), (3, NULL, NULL, 0), (4, '中文 café ABC', NULL, 0), (5, 'alpha', NULL, 0);
INSERT INTO images SELECT * FROM scenes;
INSERT INTO folders VALUES (1, '/archive/2024'), (2, '/other');
INSERT INTO files VALUES (1, 1, 'alpha.mp4', 10), (2, 2, 'beta.mp4', 20), (3, 2, 'gamma.mp4', 30), (4, 2, 'alpha beta.mp4', 40);
INSERT INTO scenes_files VALUES (2, 1), (2, 2), (3, 4), (5, 3);
INSERT INTO images_files SELECT * FROM scenes_files;
INSERT INTO files_fingerprints VALUES (1, 'hash-alpha'), (1, 'hash-other'), (2, 'hash-beta'), (3, 'hash-gamma');
INSERT INTO scene_markers VALUES (2, 'marker-alpha'), (2, 'marker-beta'), (5, 'marker-only');
`

func acceleratedTestDatabase(t *testing.T) *Database {
	t.Helper()
	db := NewDatabase()
	db.dbPath = filepath.Join(t.TempDir(), "library.sqlite")
	require.NoError(t, db.openWriteDB())
	_, err := db.writeDB.Exec(searchTestSchema)
	require.NoError(t, err)
	_, err = db.writeDB.Exec("CREATE TABLE native_migration_history(version INTEGER, name TEXT, details TEXT)")
	require.NoError(t, err)
	script, err := migrationsBox.ReadFile("migrations/1000131_search_revision.up.sql")
	require.NoError(t, err)
	_, err = db.writeDB.Exec(string(script))
	require.NoError(t, err)
	require.NoError(t, db.openReadDB())
	db.reads, err = newReadAcceleration(db.readDB, db.dbPath)
	require.NoError(t, err)
	require.NotNil(t, db.reads.index)
	// Drive refreshes explicitly to exercise the stale-index window reliably.
	db.reads.cancel()
	<-db.reads.done
	require.NoError(t, db.reads.refreshSearchIndex(context.Background()))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

func acceleratedRead(t *testing.T, db *Database, fn func(context.Context)) {
	t.Helper()
	ctx, err := db.Begin(context.Background(), false)
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Rollback(ctx)) }()
	fn(ctx)
}

func TestSearchCancellationKeepsCleanupSynchronous(t *testing.T) {
	db := acceleratedTestDatabase(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	tx, finish, err := beginSearchTransaction(ctx, db.readDB)
	require.NoError(t, err)
	t.Cleanup(finish)
	var before, pinned int
	require.NoError(t, tx.GetContext(ctx, &before, "SELECT count(*) FROM scenes"))
	cancel()
	require.ErrorIs(t, tx.GetContext(ctx, &pinned, "SELECT count(*) FROM scenes"), context.Canceled)
	acceleratedWrite(t, db, "INSERT INTO scenes VALUES (99, 'later', NULL, 0)", true)
	// Cancellation interrupts work but does not race explicit cleanup. The
	// pinned snapshot remains owned by this scope until finish releases it.
	require.NoError(t, tx.GetContext(t.Context(), &pinned, "SELECT count(*) FROM scenes"))
	require.Equal(t, before, pinned)
	finish()
	require.Equal(t, 1, db.readDB.Stats().InUse, "only the data-version observer remains checked out")
}

func acceleratedWrite(t *testing.T, db *Database, sql string, commit bool) {
	t.Helper()
	ctx, err := db.Begin(context.Background(), true)
	require.NoError(t, err)
	tx, err := getTx(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(sql)
	require.NoError(t, err)
	if commit {
		require.NoError(t, db.Commit(ctx))
	} else {
		require.NoError(t, db.Rollback(ctx))
	}
}

func assertSearchMatchesScan(t *testing.T, db *Database, table, search string, indexed bool) {
	t.Helper()
	acceleratedRead(t, db, func(ctx context.Context) {
		_, used := indexedMediaCandidates(ctx, table, search)
		require.Equal(t, indexed, used, "index eligibility: %q", search)
		uncached := context.WithValue(ctx, readAccelerationKey, (*readSnapshot)(nil))
		find := &models.FindFilterType{Q: &search, Sort: ptr("id"), PerPage: ptr(-1)}
		var fast, scan *queryBuilder
		var err error
		if table == sceneTable {
			fast, err = db.Scene.makeQuery(ctx, nil, find)
			require.NoError(t, err)
			scan, err = db.Scene.makeQuery(uncached, nil, find)
		} else {
			fast, err = db.Image.makeQuery(ctx, nil, find)
			require.NoError(t, err)
			scan, err = db.Image.makeQuery(uncached, nil, find)
		}
		require.NoError(t, err)
		wantIDs, wantCount, err := scan.executeFind(uncached)
		require.NoError(t, err)
		gotIDs, gotCount, err := fast.executeFind(ctx)
		require.NoError(t, err)
		require.Equal(t, wantIDs, gotIDs, "%s %q", table, search)
		require.Equal(t, wantCount, gotCount, "%s %q", table, search)
	})
}

func ptr[T any](value T) *T { return &value }

func TestSearchIndexPreservesSubstringAndJoinedRowSemantics(t *testing.T) {
	db := acceleratedTestDatabase(t)
	for _, table := range []string{sceneTable, imageTable} {
		for _, search := range []string{"alpha", "2024", "detail", "hash-beta", "marker-only", "alpha beta", `"alpha beta"`, "alpha OR beta", "alpha -beta", "hash-alpha hash-other", "marker-alpha marker-beta", "2024 beta", "café", "abc", "CAFÉ", "a%pha", "al_pha", "alpha OR 2024 -gamma", "unmatched", "a alpha", "alpha OR a beta", "alpha OR beta gamma OR a"} {
			t.Run(table+"/"+search, func(t *testing.T) { assertSearchMatchesScan(t, db, table, search, true) })
		}
		for _, search := range []string{"-alpha", "a", "ab", "α", "中文", "%", "_", "alpha OR a"} {
			t.Run(table+"/"+search, func(t *testing.T) { assertSearchMatchesScan(t, db, table, search, false) })
		}
	}
	acceleratedWrite(t, db, fmt.Sprintf(`WITH RECURSIVE n(id) AS (SELECT 100 UNION ALL SELECT id+1 FROM n WHERE id < %d) INSERT INTO scenes SELECT id, 'common', '', 0 FROM n`, mediaSearchCandidateLimit+200), true)
	require.NoError(t, db.reads.refreshSearchIndex(context.Background()))
	assertSearchMatchesScan(t, db, sceneTable, "common", true)
}

func TestSearchIndexTracksTextRelationshipsAndRollback(t *testing.T) {
	db := acceleratedTestDatabase(t)
	var before []string
	require.NoError(t, db.readDB.Select(&before, "SELECT sql FROM sqlite_schema WHERE sql IS NOT NULL ORDER BY name"))
	for _, update := range []string{
		"UPDATE scenes SET title = 'changed-title' WHERE id = 1",
		"UPDATE images SET details = 'changed-details' WHERE id = 4",
		"UPDATE files SET basename = 'changed-basename' WHERE id = 1",
		"UPDATE folders SET path = '/changed-folder' WHERE id = 2",
		"UPDATE files_fingerprints SET fingerprint = 'changed-fingerprint' WHERE file_id = 2",
		"INSERT INTO files_fingerprints VALUES (3, 'changed-insert')",
		"DELETE FROM files_fingerprints WHERE file_id = 3",
		"UPDATE scene_markers SET title = 'changed-marker', scene_id = 1 WHERE scene_id = 2",
		"INSERT INTO scene_markers VALUES (4, 'changed-insert')",
		"DELETE FROM scene_markers WHERE scene_id = 4",
		"UPDATE scenes_files SET scene_id = 4 WHERE scene_id = 2",
		"DELETE FROM images_files WHERE image_id = 2",
		"INSERT INTO images_files VALUES (1, 1)",
		"UPDATE files SET parent_folder_id = 1 WHERE id = 3",
		"DELETE FROM files WHERE id = 1",
		"DELETE FROM scenes WHERE id = 1",
		"DELETE FROM images WHERE id = 4",
	} {
		t.Run(update, func(t *testing.T) {
			acceleratedWrite(t, db, update, true)
			require.False(t, db.reads.fullRebuild, "ordinary edits should be incremental")
			for _, table := range []string{sceneTable, imageTable} {
				assertSearchMatchesScan(t, db, table, "changed", false)
			}
			require.NoError(t, db.reads.refreshSearchIndex(context.Background()))
			for _, table := range []string{sceneTable, imageTable} {
				for _, term := range []string{"changed", "alpha", "beta", "gamma", "2024", "marker", "detail"} {
					assertSearchMatchesScan(t, db, table, term, true)
				}
			}
		})
	}
	version := db.reads.searchRevision
	acceleratedWrite(t, db, "UPDATE scenes SET title = 'rolled-back'", false)
	require.Equal(t, version, db.reads.searchRevision)
	assertSearchMatchesScan(t, db, sceneTable, "rolled-back", true)
	acceleratedWrite(t, db, "UPDATE scenes SET rating = 100", true)
	require.Equal(t, version, db.reads.searchRevision, "non-text writes must retain the index")
	var after []string
	require.NoError(t, db.readDB.Select(&after, "SELECT sql FROM sqlite_schema WHERE sql IS NOT NULL ORDER BY name"))
	require.Equal(t, before, after, "TEMP tracking must not change the main schema")
}

func TestReadCachesRespectSnapshotsAndExternalWriters(t *testing.T) {
	db := acceleratedTestDatabase(t)
	query := "SELECT count(*) AS count FROM scenes WHERE title LIKE ?"
	args := []interface{}{"%alpha%"}
	key, _ := countCacheKey(query, args)
	checkCount := func(want int) {
		acceleratedRead(t, db, func(ctx context.Context) {
			got, err := sceneRepository.runCountQuery(ctx, query, args)
			require.NoError(t, err)
			require.Equal(t, want, got)
			cached, ok := snapshotFromContext(ctx).count(key)
			require.True(t, ok)
			require.Equal(t, got, cached)
		})
	}
	checkCount(2)
	old, err := db.Begin(context.Background(), false)
	require.NoError(t, err)
	acceleratedWrite(t, db, "UPDATE scenes SET title = 'new-title' WHERE id = 1", true)
	// Old reads remain valid while new reads cannot use the old cache.
	ids, used := indexedMediaCandidates(old, sceneTable, "alpha")
	require.True(t, used)
	require.Contains(t, ids, 1)
	oldCount, err := sceneRepository.runCountQuery(old, query, args)
	require.NoError(t, err)
	require.Equal(t, 2, oldCount)
	checkCount(1)
	require.NoError(t, db.reads.refreshSearchIndex(context.Background()))
	_, used = indexedMediaCandidates(old, sceneTable, "alpha")
	require.False(t, used, "new index cannot narrow an older library snapshot")
	require.NoError(t, db.Rollback(old))

	external, err := sqlx.Open("sqlite3", "file:"+db.dbPath+"?_journal=WAL&_fk=true")
	require.NoError(t, err)
	defer external.Close()
	_, err = external.Exec("UPDATE scenes SET title = 'external alpha' WHERE id = 3")
	require.NoError(t, err)
	checkCount(2)
	assertSearchMatchesScan(t, db, sceneTable, "external", false)
	require.True(t, db.reads.fullRebuild)
	require.NoError(t, db.reads.refreshSearchIndex(context.Background()))
	assertSearchMatchesScan(t, db, sceneTable, "external", true)

	// Page/sort parameters are not part of count SQL; identical filters reuse
	// the entry. Distinct binding types and time-dependent SQL must not collide.
	integer, _ := countCacheKey(query, []interface{}{1})
	decimal, _ := countCacheKey(query, []interface{}{1.0})
	require.NotEqual(t, integer, decimal)
	_, cacheable := countCacheKey("SELECT count(*) FROM scenes WHERE date('now') > title", nil)
	require.False(t, cacheable)
	acceleratedRead(t, db, func(ctx context.Context) {
		snapshot := snapshotFromContext(ctx)
		for i := 0; i < maxCachedCounts*2; i++ {
			key, ok := countCacheKey(query, []interface{}{i})
			require.True(t, ok)
			snapshot.rememberCount(key, i, query)
		}
		require.Len(t, db.reads.counts, maxCachedCounts)
	})
}

func TestCountCacheInvalidatesOnlyReadDependencies(t *testing.T) {
	db := acceleratedTestDatabase(t)
	query := "SELECT count(*) AS count FROM scenes WHERE EXISTS (SELECT 1 FROM scenes_files WHERE scene_id = scenes.id)"
	key, _ := countCacheKey(query, nil)
	deps, dependencyErr := db.reads.countDependencies(query)
	require.NoError(t, dependencyErr)
	require.ElementsMatch(t, []string{"scenes", "scenes_files"}, deps)
	acceleratedRead(t, db, func(ctx context.Context) {
		_, err := sceneRepository.runCountQuery(ctx, query, nil)
		require.NoError(t, err)
		_, ok := snapshotFromContext(ctx).count(key)
		require.True(t, ok)
	})
	check := func(cached bool) {
		acceleratedRead(t, db, func(ctx context.Context) {
			_, ok := snapshotFromContext(ctx).count(key)
			require.Equal(t, cached, ok)
		})
	}
	acceleratedWrite(t, db, "INSERT INTO worker_bookkeeping VALUES (1, 'running')", true)
	check(true)
	acceleratedWrite(t, db, "UPDATE images SET rating = 4", true)
	check(true)
	acceleratedWrite(t, db, "DELETE FROM scenes_files", false)
	check(true)
	acceleratedWrite(t, db, "DELETE FROM scenes_files", true)
	check(false)
	acceleratedRead(t, db, func(ctx context.Context) {
		got, err := sceneRepository.runCountQuery(ctx, query, nil)
		require.NoError(t, err)
		require.Zero(t, got)
	})
}

func TestSearchIndexMissingDamagedOrFromPreviousProcessFallsBack(t *testing.T) {
	db := acceleratedTestDatabase(t)
	_, err := db.reads.index.Exec("UPDATE snapshot SET owner = 'another-process'")
	require.NoError(t, err)
	assertSearchMatchesScan(t, db, sceneTable, "alpha", false)
	_, err = db.reads.index.Exec("DROP TABLE snapshot")
	require.NoError(t, err)
	assertSearchMatchesScan(t, db, sceneTable, "alpha", false)
	// Search documents stay in the disposable cache, not the library schema.
	var schema []string
	require.NoError(t, db.readDB.Select(&schema, "SELECT sql FROM sqlite_schema WHERE sql IS NOT NULL"))
	require.NotContains(t, strings.ToLower(strings.Join(schema, "\n")), "fts5")
}

func TestSearchIndexReuseRequiresMatchingPersistentRevision(t *testing.T) {
	db := acceleratedTestDatabase(t)
	reopen := func() {
		db.reads.close()
		var err error
		db.reads, err = newReadAcceleration(db.readDB, db.dbPath)
		require.NoError(t, err)
		db.reads.cancel()
		<-db.reads.done
	}
	owner := db.reads.owner
	reopen()
	require.NotEqual(t, owner, db.reads.owner)
	require.False(t, db.reads.fullRebuild)
	assertSearchMatchesScan(t, db, sceneTable, "alpha", true)
	acceleratedWrite(t, db, "UPDATE scenes SET title='rolled-back' WHERE id=1", false)
	reopen()
	require.False(t, db.reads.fullRebuild)
	acceleratedWrite(t, db, "INSERT INTO scenes(id,title) VALUES(1,'upsert alpha') ON CONFLICT(id) DO UPDATE SET title=excluded.title", true)
	require.NoError(t, db.reads.refreshSearchIndex(context.Background()))
	assertSearchMatchesScan(t, db, sceneTable, "upsert", true)
	// A commit whose index update was interrupted must rebuild, not relabel
	// the previous index. The original search remains correct via SQL meanwhile.
	acceleratedWrite(t, db, "UPDATE scenes SET title='interrupted alpha' WHERE id=1", true)
	reopen()
	require.True(t, db.reads.fullRebuild)
	assertSearchMatchesScan(t, db, sceneTable, "interrupted", false)
	require.NoError(t, db.reads.refreshSearchIndex(context.Background()))
	reopen()
	require.False(t, db.reads.fullRebuild)
	assertSearchMatchesScan(t, db, sceneTable, "interrupted", true)
	// Missing FTS tables cannot inherit the still-matching snapshot marker.
	_, err := db.reads.index.Exec("DROP TABLE scenes")
	require.NoError(t, err)
	reopen()
	require.True(t, db.reads.fullRebuild)
	assertSearchMatchesScan(t, db, sceneTable, "interrupted", false)
	require.NoError(t, db.reads.refreshSearchIndex(context.Background()))
	// An external connection is tracked by permanent native triggers, even
	// while the application's connection-local journal is absent.
	external, err := sqlx.Open("sqlite3", "file:"+db.dbPath+"?_journal=WAL&_fk=true")
	require.NoError(t, err)
	db.reads.close()
	_, err = external.Exec("UPDATE scenes SET title='external restart' WHERE id=1")
	require.NoError(t, err)
	require.NoError(t, external.Close())
	db.reads, err = newReadAcceleration(db.readDB, db.dbPath)
	require.NoError(t, err)
	db.reads.cancel()
	<-db.reads.done
	require.True(t, db.reads.fullRebuild)
	assertSearchMatchesScan(t, db, sceneTable, "external", false)
}

func TestSearchRevisionCoversIncrementalTriggerDependencies(t *testing.T) {
	db := acceleratedTestDatabase(t)
	acceleratedWrite(t, db, "UPDATE scenes SET rating=5", true)
	var permanent, temporary []string
	require.NoError(t, db.writeDB.Select(&permanent, "SELECT tbl_name || substr(sql, instr(sql, 'AFTER'), instr(sql, ' ON ') - instr(sql, 'AFTER')) FROM sqlite_schema WHERE name GLOB 'library_search_*' AND name != 'library_search_state_retained' AND type='trigger' ORDER BY 1"))
	// Normalize BEFORE DELETE to AFTER DELETE: both mark the same dependency.
	require.NoError(t, db.writeDB.Select(&temporary, "SELECT tbl_name || replace(substr(sql, CASE WHEN instr(sql, 'BEFORE')>0 THEN instr(sql, 'BEFORE') ELSE instr(sql, 'AFTER') END, instr(sql, ' ON ') - CASE WHEN instr(sql, 'BEFORE')>0 THEN instr(sql, 'BEFORE') ELSE instr(sql, 'AFTER') END), 'BEFORE', 'AFTER') FROM sqlite_temp_schema WHERE name GLOB 'stash_search_*' AND type='trigger' ORDER BY 1"))
	require.Equal(t, temporary, permanent, "new search dependencies must update both journals")
}

func TestReadCachesConcurrentCommits(t *testing.T) {
	db := acceleratedTestDatabase(t)
	external, err := sqlx.Open("sqlite3", "file:"+db.dbPath+"?_journal=WAL&_busy_timeout=5000")
	require.NoError(t, err)
	defer external.Close()
	var group errgroup.Group
	for i := 0; i < 6; i++ {
		group.Go(func() error {
			for j := 0; j < 60; j++ {
				if err := func() error {
					ctx, err := db.Begin(context.Background(), false)
					if err != nil {
						return err
					}
					defer func() { _ = db.Rollback(ctx) }()
					uncached := context.WithValue(ctx, readAccelerationKey, (*readSnapshot)(nil))
					find := &models.FindFilterType{Q: ptr("alpha"), Sort: ptr("id")}
					fast, err := db.Scene.makeQuery(ctx, nil, find)
					if err != nil {
						return err
					}
					scan, err := db.Scene.makeQuery(uncached, nil, find)
					if err != nil {
						return err
					}
					ids, count, err := fast.executeFind(ctx)
					if err != nil {
						return err
					}
					wantIDs, wantCount, err := scan.executeFind(uncached)
					if err != nil {
						return err
					}
					if !reflect.DeepEqual(ids, wantIDs) || count != wantCount {
						return fmt.Errorf("cached read differed from its snapshot: %v/%d vs %v/%d", ids, count, wantIDs, wantCount)
					}
					return nil
				}(); err != nil {
					return err
				}
			}
			return nil
		})
	}
	group.Go(func() error {
		for i := 0; i < 60; i++ {
			ctx, err := db.Begin(context.Background(), true)
			if err != nil {
				return err
			}
			tx, err := getTx(ctx)
			if err != nil {
				return err
			}
			_, err = tx.Exec("UPDATE scenes SET title = ? WHERE id = 1", []string{"alpha", "changed"}[i%2])
			if err != nil {
				_ = db.Rollback(ctx)
				return err
			}
			if err := db.Commit(ctx); err != nil {
				return err
			}
		}
		return nil
	})
	group.Go(func() error {
		for i := 0; i < 60; i++ {
			if _, err := external.Exec("UPDATE scenes SET title = ? WHERE id = 3", []string{"alpha", "external"}[i%2]); err != nil {
				return err
			}
		}
		return nil
	})
	group.Go(func() error {
		for i := 0; i < 60; i++ {
			if err := db.reads.refreshSearchIndex(context.Background()); err != nil && !strings.Contains(err.Error(), "while pinning") {
				return err
			}
		}
		return nil
	})
	require.NoError(t, group.Wait())
	require.NoError(t, db.reads.refreshSearchIndex(context.Background()))
	assertSearchMatchesScan(t, db, sceneTable, "alpha", true)
}

func TestSearchTrackingSurvivesTableRebuild(t *testing.T) {
	db := acceleratedTestDatabase(t)
	acceleratedWrite(t, db, "UPDATE scenes SET rating = 10 WHERE id = 1", true)
	external, err := sqlx.Open("sqlite3", "file:"+db.dbPath+"?_journal=WAL&_fk=false")
	require.NoError(t, err)
	defer external.Close()
	_, err = external.Exec(`BEGIN;
CREATE TABLE scenes_new (id INTEGER PRIMARY KEY, title TEXT, details TEXT, rating INTEGER);
INSERT INTO scenes_new SELECT * FROM scenes;
DROP TABLE scenes;
ALTER TABLE scenes_new RENAME TO scenes;
COMMIT;`)
	require.NoError(t, err)
	require.NoError(t, db.reads.refreshSearchIndex(context.Background()))
	assertSearchMatchesScan(t, db, sceneTable, "alpha", true)
	acceleratedWrite(t, db, "UPDATE scenes SET title = 'after-rebuild' WHERE id = 1", true)
	assertSearchMatchesScan(t, db, sceneTable, "after-rebuild", false)
	require.NoError(t, db.reads.refreshSearchIndex(context.Background()))
	assertSearchMatchesScan(t, db, sceneTable, "after-rebuild", true)
}
