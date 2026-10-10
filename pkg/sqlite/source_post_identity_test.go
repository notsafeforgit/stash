package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func postIdentityFixture(t *testing.T) (*Database, models.Repository) {
	t.Helper()
	config.InitializeEmpty()
	db := NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "post-identities.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.GreaterOrEqual(t, GetRequiredSchemaVersion(), NativeSchemaBaseline+88)
	return db, db.Repository()
}

func identityPost(t *testing.T, repo models.Repository, namespace, key string) string {
	t.Helper()
	id := ""
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		post, err := repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: namespace, Value: key}, "")
		if err == nil {
			id = post.UUID
		}
		return err
	}))
	return id
}

func identityRequest(t *testing.T, repo models.Repository, source, destination string) postIdentityConsolidationInput {
	t.Helper()
	ret := postIdentityConsolidationInput{UUID: uuid.NewString(), SourceUUID: source, DestinationUUID: destination, ReviewSignature: strings.Repeat("a", 64), Origin: "review", Reason: "Reviewed duplicate source record"}
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		preview, err := inspectPostConsolidationIdentity(ctx, source, destination)
		if err == nil {
			ret.IdentitySignature = preview.Signature
		}
		return err
	}))
	return ret
}

func publishPostIdentity(t *testing.T, repo models.Repository, input postIdentityConsolidationInput) *models.SourcePostConsolidation {
	t.Helper()
	var ret *models.SourcePostConsolidation
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = publishPostConsolidationIdentity(ctx, input)
		return err
	}))
	return ret
}

func identityRead(t *testing.T, repo models.Repository, id string) *models.SourcePostIdentity {
	t.Helper()
	var ret *models.SourcePostIdentity
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceEvidence.PostIdentity(ctx, id)
		return err
	}))
	return ret
}

func identityRows(t *testing.T, repo models.Repository, tables ...string) map[string][]string {
	t.Helper()
	ret := map[string][]string{}
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		tx, err := getTx(ctx)
		if err != nil {
			return err
		}
		for _, table := range tables {
			func() {
				rows, err := tx.Queryx("SELECT * FROM " + table)
				require.NoError(t, err)
				defer rows.Close()
				items := []string{}
				for rows.Next() {
					item := map[string]any{}
					require.NoError(t, rows.MapScan(item))
					encoded, err := json.Marshal(item)
					require.NoError(t, err)
					items = append(items, string(encoded))
				}
				require.NoError(t, rows.Err())
				slices.Sort(items)
				ret[table] = items
			}()
		}
		return nil
	}))
	return ret
}

func identityAlbum(t *testing.T, repo models.Repository, post string) models.SourceCaptureInput {
	t.Helper()
	raw, err := archive.RetainSourcePayload([]byte(`{"category":"reddit","id":"one-post","title":"Original source text"}`))
	require.NoError(t, err)
	payload, err := archive.PrepareRetainedCapture("gallery-dl", "reddit", raw)
	require.NoError(t, err)
	title := "Original source text"
	input := models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: post, Origin: "gallery-dl", Platform: "reddit", CapturedAt: time.Now().UTC(), RetentionPolicy: archive.SourceRetentionVersion, Metadata: models.SourcePostMetadata{Title: &title}, Payload: *payload}
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		capture, err := repo.SourceEvidence.RecordCapture(ctx, input)
		if err != nil {
			return err
		}
		_, err = repo.SourceAttachment.RecordManifest(ctx, models.SourceAttachmentManifestInput{CaptureUUID: capture.UUID, Complete: true, DeclaredAlbum: true,
			Entries: []models.SourceAttachmentEntry{{Position: 0, Reference: models.SourcePostIdentifier{Namespace: "native:reddit", Value: "first"}, MediaKind: "image"}, {Position: 1, Reference: models.SourcePostIdentifier{Namespace: "native:reddit", Value: "second"}, MediaKind: "video"}}})
		if err != nil {
			return err
		}
		p, err := repo.SourceEvidence.FindPost(ctx, post)
		if err != nil {
			return err
		}
		_, err = repo.SourceAttachment.DecideSelection(ctx, models.AttachmentSelectionInput{PostUUID: post, ExpectedPostRevision: p.Revision, CaptureUUID: capture.UUID, Mode: "pinned", Origin: "review"})
		if err != nil {
			return err
		}
		preview, err := repo.SourceGallery.Preview(ctx, post)
		if err != nil {
			return err
		}
		_, err = repo.SourceGallery.Sync(ctx, post, preview.Signature)
		return err
	}))
	return input
}

func TestPostIdentityPreservesOriginalSourcesChoicesAndReplayAcrossChainedMerges(t *testing.T) {
	db, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "first")
	b := identityPost(t, repo, "legacy:catalog:fixture", "second")
	c := identityPost(t, repo, "native:reddit", "one-post")
	original := identityAlbum(t, repo, a)
	identityAlbum(t, repo, b)
	tables := []string{"source_post_identifiers", "source_payloads", "source_post_revisions", "source_captures", "source_attachments", "source_attachment_manifests", "source_attachment_entries", "post_attachment_decisions", "post_attachment_selections", "post_gallery_decisions", "post_gallery_links", "galleries", "metadata_field_decisions"}
	before := identityRows(t, repo, tables...)
	first := identityRequest(t, repo, a, b)
	receipt := publishPostIdentity(t, repo, first)
	require.Equal(t, 2, receipt.MemberCount)
	require.Equal(t, b, identityRead(t, repo, a).CanonicalUUID)
	require.Equal(t, b, *identityRead(t, repo, a).RedirectTo)
	second := identityRequest(t, repo, b, c)
	later := publishPostIdentity(t, repo, second)
	require.Equal(t, 3, later.MemberCount)
	for _, id := range []string{a, b, c} {
		require.Equal(t, c, identityRead(t, repo, id).CanonicalUUID)
	}
	require.Equal(t, b, *identityRead(t, repo, a).RedirectTo, "direct event remains history while canonical lookup is flattened")
	require.Equal(t, before, identityRows(t, repo, tables...), "identity consolidation cannot rewrite original evidence or choose an album")
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	repo = db.Repository()
	require.Equal(t, receipt, publishPostIdentity(t, repo, first), "lost response recovers its original receipt after later merges and restart")
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		capture, err := repo.SourceEvidence.RecordCapture(ctx, original)
		require.NoError(t, err)
		require.Equal(t, a, capture.PostUUID)
		return err
	}))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var actual []string
		after := ""
		for {
			page, err := repo.SourceEvidence.PostIdentityMembers(ctx, a, after, 1)
			require.NoError(t, err)
			if len(page) == 0 {
				break
			}
			require.Equal(t, c, page[0].CanonicalUUID)
			actual = append(actual, page[0].UUID)
			after = page[0].UUID
		}
		require.ElementsMatch(t, []string{a, b, c}, actual)
		history, err := repo.SourceEvidence.PostConsolidationHistory(ctx, b, 0, 100)
		require.NoError(t, err)
		require.Equal(t, []models.SourcePostConsolidation{*receipt, *later}, history)
		changed := first
		changed.Reason = "Different reason"
		_, err = postConsolidationReceipt(ctx, changed)
		require.ErrorIs(t, err, models.ErrSourcePostConsolidationReplay)
		return nil
	}))
}

func TestPostIdentityRejectsStaleReviewAndConflictingNativeIdentifiers(t *testing.T) {
	_, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "a")
	b := identityPost(t, repo, "native:reddit", "one")
	other := identityPost(t, repo, "native:reddit", "two")
	request := identityRequest(t, repo, a, b)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		post, err := repo.SourceEvidence.FindPost(ctx, a)
		if err != nil {
			return err
		}
		return repo.SourceEvidence.AddPostIdentifier(ctx, a, models.SourcePostIdentifier{Namespace: "legacy:catalog:fixture", Value: "added"}, post.Revision)
	}))
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error { _, err := publishPostConsolidationIdentity(ctx, request); return err })
	require.ErrorIs(t, err, models.ErrSourcePostIdentityConflict)
	require.Equal(t, a, identityRead(t, repo, a).CanonicalUUID)
	for _, pair := range [][2]string{{b, other}, {other, b}} {
		require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			_, err := inspectPostConsolidationIdentity(ctx, pair[0], pair[1])
			require.ErrorIs(t, err, models.ErrSourcePostIdentifierConflict)
			return nil
		}))
	}
	publishPostIdentity(t, repo, identityRequest(t, repo, a, b))
	err = repo.WithTxn(t.Context(), func(ctx context.Context) error {
		post, err := repo.SourceEvidence.FindPost(ctx, a)
		if err != nil {
			return err
		}
		return repo.SourceEvidence.AddPostIdentifier(ctx, a, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "different"}, post.Revision)
	})
	require.ErrorContains(t, err, "contradictory upstream identity")
}

func TestPostIdentitySwallowedFailureCannotCommitPartialMerge(t *testing.T) {
	_, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "a")
	b := identityPost(t, repo, "native:reddit", "one")
	request := identityRequest(t, repo, a, b)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := dbWrapper.Exec(ctx, `CREATE TRIGGER reject_identity_publish BEFORE UPDATE OF canonical_uuid ON source_post_identities
WHEN OLD.canonical_uuid IS NOT NEW.canonical_uuid BEGIN SELECT RAISE(ABORT,'injected identity publication failure'); END;`)
		return err
	}))
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := publishPostConsolidationIdentity(ctx, request)
		require.ErrorContains(t, err, "injected identity publication failure")
		return nil
	})
	require.ErrorContains(t, err, "unfinished source post consolidation")
	for _, id := range []string{a, b} {
		identity := identityRead(t, repo, id)
		require.Equal(t, id, identity.CanonicalUUID)
		require.Equal(t, 1, identity.Revision)
	}
	require.Empty(t, identityRows(t, repo, "source_post_consolidations")["source_post_consolidations"])
}

func TestPostIdentityForgettingOneAliasTombstonesTheGroupAndRetainsReceipt(t *testing.T) {
	db, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "a")
	b := identityPost(t, repo, "native:reddit", "one")
	request := identityRequest(t, repo, a, b)
	receipt := publishPostIdentity(t, repo, request)
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := dbWrapper.Exec(ctx, "UPDATE source_posts SET state='forgotten' WHERE uuid=?", a)
		return err
	}))
	for _, id := range []string{a, b} {
		require.Equal(t, "forgotten", identityRead(t, repo, id).State)
	}
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := dbWrapper.Exec(ctx, "UPDATE source_posts SET state='active' WHERE uuid=?", b)
		return err
	})
	require.ErrorContains(t, err, "cannot be resurrected")
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	repo = db.Repository()
	require.Equal(t, receipt, publishPostIdentity(t, repo, request))
	c := identityPost(t, repo, "legacy:catalog:fixture", "c")
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		_, err := inspectPostConsolidationIdentity(ctx, b, c)
		require.ErrorIs(t, err, models.ErrSourcePostForgotten)
		return nil
	}))
}

func TestPostIdentityRequiresManagedWriteAndRejectsAdHocRoots(t *testing.T) {
	_, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "a")
	b := identityPost(t, repo, "native:reddit", "one")
	request := identityRequest(t, repo, a, b)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		_, err := publishPostConsolidationIdentity(ctx, request)
		require.Error(t, err)
		return nil
	}))
	for _, query := range []string{"UPDATE source_post_identities SET canonical_uuid=? WHERE post_uuid=?", "UPDATE source_posts SET uuid=? WHERE uuid=?"} {
		err := repo.WithTxn(t.Context(), func(ctx context.Context) error { _, err := dbWrapper.Exec(ctx, query, b, a); return err })
		require.Error(t, err)
	}
	for _, bad := range []string{"bad", uuid.Nil.String(), "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA"} {
		require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
			_, err := repo.SourceEvidence.PostIdentity(ctx, bad)
			require.ErrorIs(t, err, models.ErrSourcePostIdentityInvalid)
			return nil
		}))
	}
}

func TestPostIdentityAnonymiseRemovesConsolidationAndPrivateReasons(t *testing.T) {
	db, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "a")
	b := identityPost(t, repo, "native:reddit", "one")
	request := identityRequest(t, repo, a, b)
	publishPostIdentity(t, repo, request)
	output := filepath.Join(t.TempDir(), "anonymous.sqlite")
	require.NoError(t, db.Anonymise(output))
	other := NewDatabase()
	require.NoError(t, other.Open(output))
	defer other.Close()
	rows := identityRows(t, other.Repository(), "source_posts", "source_post_identities", "source_post_consolidations", "source_post_consolidation_context")
	for _, values := range rows {
		require.Empty(t, values)
	}
}

func TestPostIdentityOpenRejectsUnfinishedPublication(t *testing.T) {
	db, repo := postIdentityFixture(t)
	a := identityPost(t, repo, "legacy:catalog:fixture", "a")
	b := identityPost(t, repo, "native:reddit", "one")
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := dbWrapper.Exec(ctx, "INSERT INTO source_post_consolidation_context VALUES(?,?,?)", uuid.NewString(), a, b)
		return err
	}))
	require.NoError(t, db.Close())
	err := db.Open(db.DatabasePath())
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "unfinished post consolidation") || errors.Is(err, models.ErrSourcePostIdentityConflict), err.Error())
}

func TestPostIdentityRejectsOversizedReviewWithoutPartialMutation(t *testing.T) {
	_, repo := postIdentityFixture(t)
	var ids []string
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		for i := 0; i <= maxPostIdentityMembers; i++ {
			post, err := repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "legacy:catalog:fixture", Value: fmt.Sprint(i)}, "")
			if err != nil {
				return err
			}
			ids = append(ids, post.UUID)
		}
		return nil
	}))
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		// Balanced merges exercise both already-consolidated source and destination
		// groups, reaching the real limit through the managed write primitive.
		roots := slices.Clone(ids[:maxPostIdentityMembers])
		for len(roots) > 1 {
			next := []string{}
			for i := 0; i < len(roots); i += 2 {
				preview, err := inspectPostConsolidationIdentity(ctx, roots[i], roots[i+1])
				if err != nil {
					return err
				}
				_, err = publishPostConsolidationIdentity(ctx, postIdentityConsolidationInput{UUID: uuid.NewString(), SourceUUID: roots[i], DestinationUUID: roots[i+1],
					IdentitySignature: preview.Signature, ReviewSignature: strings.Repeat("a", 64), Origin: "review"})
				if err != nil {
					return err
				}
				next = append(next, roots[i+1])
			}
			roots = next
		}
		return nil
	}))
	before := identityRows(t, repo, "source_posts", "source_post_identities", "source_post_consolidations")
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		_, err := inspectPostConsolidationIdentity(ctx, ids[maxPostIdentityMembers-1], ids[maxPostIdentityMembers])
		require.ErrorIs(t, err, models.ErrSourcePostIdentityLimit)
		return nil
	}))
	require.Equal(t, before, identityRows(t, repo, "source_posts", "source_post_identities", "source_post_consolidations"))
	a := identityPost(t, repo, "legacy:catalog:fixture", "large-identifiers")
	b := ids[maxPostIdentityMembers]
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := dbWrapper.Exec(ctx, `WITH RECURSIVE ids(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM ids WHERE n<?)
INSERT INTO source_post_identifiers(namespace,value,post_uuid) SELECT 'legacy:catalog:extra',CAST(n AS TEXT),? FROM ids`, maxPostIdentityIdentifiers, a)
		return err
	}))
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		_, err := inspectPostConsolidationIdentity(ctx, a, b)
		require.ErrorIs(t, err, models.ErrSourcePostIdentityLimit)
		return nil
	}))
}

func TestPostIdentityAuditRejectsCorruptRootBeforeWriting(t *testing.T) {
	for _, corrupt := range []string{"missing identity", "redirect without receipt", "outdated revision"} {
		t.Run(corrupt, func(t *testing.T) {
			db, repo := postIdentityFixture(t)
			a := identityPost(t, repo, "legacy:catalog:fixture", "a")
			b := identityPost(t, repo, "native:reddit", "one")
			if corrupt == "outdated revision" {
				publishPostIdentity(t, repo, identityRequest(t, repo, a, b))
			}
			require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
				switch corrupt {
				case "missing identity":
					_, err := dbWrapper.Exec(ctx, "DELETE FROM source_post_identities WHERE post_uuid=?", a)
					return err
				case "redirect without receipt":
					var guard string
					if err := dbWrapper.Get(ctx, &guard, "SELECT sql FROM sqlite_schema WHERE name='source_post_root_update'"); err != nil {
						return err
					}
					if _, err := dbWrapper.Exec(ctx, "DROP TRIGGER source_post_root_update"); err != nil {
						return err
					}
					if _, err := dbWrapper.Exec(ctx, "UPDATE source_post_identities SET canonical_uuid=? WHERE post_uuid=?", b, a); err != nil {
						return err
					}
					_, err := dbWrapper.Exec(ctx, guard)
					return err
				default:
					_, err := dbWrapper.Exec(ctx, "UPDATE source_posts SET revision=1 WHERE uuid=?", a)
					return err
				}
			}))
			require.NoError(t, db.Close())
			before, err := os.ReadFile(db.DatabasePath())
			require.NoError(t, err)
			require.ErrorContains(t, db.AuditForTesting(db.DatabasePath()), "post identity differs")
			after, err := os.ReadFile(db.DatabasePath())
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}

func TestPostIdentityQueriesUseCanonicalAndHistoryIndexes(t *testing.T) {
	_, repo := postIdentityFixture(t)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		for _, tc := range []struct {
			query   string
			args    []any
			indexes []string
		}{
			{postIdentityMembersQuery, []any{"root", "after", 101}, []string{"SEARCH i USING COVERING INDEX source_post_identities_canonical"}},
			{postIdentityIdentifiersQuery, []any{"source", "destination", 8193}, []string{"SEARCH p USING COVERING INDEX source_post_identities_canonical", "SEARCH i USING COVERING INDEX source_post_identifiers_post"}},
			{postIdentityHistoryQuery, []any{"source", 0, 100, "destination", 0, 100, 100}, []string{"SEARCH source_post_consolidations USING INDEX sqlite_autoindex_source_post_consolidations_2", "SEARCH source_post_consolidations USING INDEX source_post_consolidations_destination"}},
		} {
			var rows []struct {
				ID, Parent, Notused int
				Detail              string
			}
			require.NoError(t, dbWrapper.Select(ctx, &rows, "EXPLAIN QUERY PLAN "+tc.query, tc.args...))
			plan := fmt.Sprint(rows)
			for _, index := range tc.indexes {
				require.Contains(t, plan, index)
			}
			require.NotContains(t, plan, "SCAN source_post")
			if tc.query != postIdentityHistoryQuery {
				require.NotContains(t, plan, "TEMP B-TREE")
			}
		}
		return nil
	}))
}
