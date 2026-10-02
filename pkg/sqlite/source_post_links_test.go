package sqlite_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func postLinkEvidence(post string) models.SourcePostEvidence {
	return models.SourcePostEvidence{UUID: uuid.NewString(), PostUUID: post, Origin: "migration", Basis: "catalog-row",
		ObservedAt: time.Date(2026, 9, 30, 1, 2, 3, 456789123, time.FixedZone("source", -7*3600)), Details: json.RawMessage(`{"table":"post_urls"}`)}
}

func postLinkRevision(t *testing.T, repo models.Repository, post string) int {
	t.Helper()
	var revision int
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		current, err := repo.SourceEvidence.FindPost(ctx, post)
		require.NoError(t, err)
		require.NotNil(t, current)
		revision = current.Revision
		return nil
	}))
	return revision
}

func observePostURL(repo models.Repository, input models.SourcePostURLInput) (*models.SourcePostURLObservation, error) {
	var ret *models.SourcePostURLObservation
	err := repo.WithTxn(context.Background(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourcePostLinks.ObserveURL(ctx, input)
		return err
	})
	return ret, err
}

func TestSourcePostLinksDeduplicateURLsRetainEvidenceAndReplay(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "one"}, "")
	other := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "two"}, "")
	input := models.SourcePostURLInput{SourcePostEvidence: postLinkEvidence(post.UUID), URL: "https://www.reddit.com/comments/one/?b=2&a=1#original"}
	first, err := observePostURL(repo, input)
	require.NoError(t, err)
	require.Equal(t, input.URL, first.URL, "retain the original URL without rewriting or fetching it")
	require.Equal(t, input.ObservedAt.UTC(), first.ObservedAt)
	replay, err := observePostURL(repo, input)
	require.NoError(t, err)
	require.Equal(t, first, replay)
	require.Equal(t, post.Revision+1, postLinkRevision(t, repo, post.UUID))
	changed := input
	changed.Details = json.RawMessage(`{"table":"another"}`)
	_, err = observePostURL(repo, changed)
	require.ErrorIs(t, err, models.ErrSourcePostEvidenceReplay)
	later := input
	later.UUID = uuid.NewString()
	later.ObservedAt = later.ObservedAt.Add(time.Hour)
	second, err := observePostURL(repo, later)
	require.NoError(t, err)
	require.Equal(t, first.URLUUID, second.URLUUID, "observing a URL again does not copy the URL row")
	anotherPost := input
	anotherPost.UUID, anotherPost.PostUUID = uuid.NewString(), other.UUID
	separate, err := observePostURL(repo, anotherPost)
	require.NoError(t, err)
	require.NotEqual(t, first.URLUUID, separate.URLUUID, "a shared URL does not merge posts")
	anotherURL := input
	anotherURL.UUID, anotherURL.URL = uuid.NewString(), "https://redd.it/one"
	_, err = observePostURL(repo, anotherURL)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		page, err := repo.SourcePostLinks.URLs(ctx, post.UUID, "", 1)
		require.NoError(t, err)
		require.Len(t, page, 1)
		next, err := repo.SourcePostLinks.URLs(ctx, post.UUID, page[0].UUID, 1)
		require.NoError(t, err)
		require.Len(t, next, 1)
		require.NotEqual(t, page[0].URL, next[0].URL)
		end, err := repo.SourcePostLinks.URLs(ctx, post.UUID, next[0].UUID, 1)
		require.NoError(t, err)
		require.Empty(t, end)
		evidence, err := repo.SourcePostLinks.URLEvidence(ctx, first.URLUUID, "", 1)
		require.NoError(t, err)
		require.Len(t, evidence, 1)
		remaining, err := repo.SourcePostLinks.URLEvidence(ctx, first.URLUUID, evidence[0].UUID, 100)
		require.NoError(t, err)
		require.Len(t, remaining, 1)
		require.NotEqual(t, evidence[0].UUID, remaining[0].UUID)
		return nil
	}))
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	_, err = raw.Exec("UPDATE source_posts SET state='forgotten' WHERE uuid=?", post.UUID)
	require.NoError(t, err)
	replayed, err := observePostURL(repo, input)
	require.NoError(t, err)
	require.Equal(t, first, replayed)
	later.UUID = uuid.NewString()
	_, err = observePostURL(repo, later)
	require.ErrorIs(t, err, models.ErrSourcePostForgotten)
	require.Equal(t, uint(3), queryUint(t, raw, "SELECT count(*) FROM source_post_urls"))
	require.Equal(t, uint(4), queryUint(t, raw, "SELECT count(*) FROM source_post_url_evidence"))
	for _, table := range []string{"source_post_urls", "source_post_url_evidence"} {
		_, err := raw.Exec("UPDATE " + table + " SET uuid=uuid")
		require.ErrorContains(t, err, "immutable")
	}
	_, err = raw.Exec(`INSERT INTO source_post_url_evidence(uuid,url_uuid,origin,basis,observed_at,details,request_digest)
SELECT ?,url_uuid,origin,basis,observed_at,details,request_digest FROM source_post_url_evidence WHERE uuid=?`, uuid.NewString(), input.UUID)
	require.ErrorContains(t, err, "forgotten")
}

func TestSourcePostLinksIdentifierEvidenceDoesNotReassignPosts(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:instagram", Value: "one"}, "")
	other := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:instagram", Value: "two"}, "")
	input := models.SourcePostIdentifierInput{SourcePostEvidence: postLinkEvidence(post.UUID),
		Identifier: models.SourcePostIdentifier{Namespace: "legacy:catalog:test", Value: "original-local-key"}, ExpectedPostRevision: post.Revision}
	var first *models.SourcePostIdentifierObservation
	write := func(value models.SourcePostIdentifierInput) error {
		return repo.WithTxn(context.Background(), func(ctx context.Context) error {
			var err error
			first, err = repo.SourcePostLinks.ObserveIdentifier(ctx, value)
			return err
		})
	}
	require.NoError(t, write(input))
	written := first
	require.NoError(t, write(input))
	require.Equal(t, written, first)
	stale := input
	stale.UUID = uuid.NewString()
	require.ErrorIs(t, write(stale), models.ErrSourcePostConflict)
	conflict := input
	conflict.UUID, conflict.PostUUID, conflict.ExpectedPostRevision = uuid.NewString(), other.UUID, other.Revision
	require.ErrorIs(t, write(conflict), models.ErrSourcePostConflict)
	stale.ExpectedPostRevision = postLinkRevision(t, repo, post.UUID)
	require.NoError(t, write(stale), "another observation of the same identifier retains new evidence")
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		owner, err := repo.SourceEvidence.FindPostByIdentifier(ctx, input.Identifier)
		require.NoError(t, err)
		require.Equal(t, post.UUID, owner.UUID)
		page, err := repo.SourcePostLinks.IdentifierEvidence(ctx, post.UUID, "", 1)
		require.NoError(t, err)
		require.Len(t, page, 1)
		next, err := repo.SourcePostLinks.IdentifierEvidence(ctx, post.UUID, page[0].UUID, 1)
		require.NoError(t, err)
		require.Len(t, next, 1)
		return nil
	}))
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec(`INSERT INTO source_post_identifier_evidence(uuid,post_uuid,namespace,value,origin,basis,observed_at,details,request_digest)
SELECT ?,?,namespace,value,origin,basis,observed_at,details,request_digest FROM source_post_identifier_evidence WHERE uuid=?`, uuid.NewString(), other.UUID, input.UUID)
	require.ErrorContains(t, err, "different post")
	_, err = raw.Exec("UPDATE source_post_identifier_evidence SET basis='changed'")
	require.ErrorContains(t, err, "immutable")
	_, err = raw.Exec("UPDATE source_posts SET state='forgotten' WHERE uuid=?", post.UUID)
	require.NoError(t, err)
	require.NoError(t, write(input))
	stale.UUID = uuid.NewString()
	require.ErrorIs(t, write(stale), models.ErrSourcePostForgotten)
}

func TestSourcePostAccountClaimsDoNotSelectPublishersOrPerformers(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	capture := publisherCapture(t, repo, "native:reddit", "reddit", `{"category":"reddit","author_fullname":"t2_publisher","author":"Publisher"}`)
	original, survivor := createSourceAccount(t, repo, "native:reddit"), createSourceAccount(t, repo, "native:reddit")
	wrong := createSourceAccount(t, repo, "mirror:coomer:onlyfans")
	input := models.SourcePostAccountClaimInput{SourcePostEvidence: postLinkEvidence(capture.PostUUID), AccountUUID: original.UUID}
	var first *models.SourcePostAccountClaim
	write := func(value models.SourcePostAccountClaimInput) error {
		return repo.WithTxn(context.Background(), func(ctx context.Context) error {
			var err error
			first, err = repo.SourcePostLinks.ClaimAccount(ctx, value)
			return err
		})
	}
	require.NoError(t, write(input))
	require.Equal(t, original.UUID, first.CanonicalAccountUUID)
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		ownership, err := repo.SourceAccount.Ownership(ctx, original.UUID)
		require.NoError(t, err)
		require.Nil(t, ownership, "a publisher claim does not create an account ownership decision")
		return nil
	}))
	bad := input
	bad.UUID, bad.AccountUUID = uuid.NewString(), wrong.UUID
	require.ErrorIs(t, write(bad), models.ErrSourcePostConflict)
	_, err := consolidateAccount(repo, consolidationInput(accountPreview(t, repo, original.UUID, survivor.UUID)))
	require.NoError(t, err)
	require.NoError(t, write(input), "replay retains the original account and resolves its current canonical UUID")
	require.Equal(t, original.UUID, first.AccountUUID)
	require.Equal(t, survivor.UUID, first.CanonicalAccountUUID)
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		claims, err := repo.SourcePostLinks.AccountClaims(ctx, capture.PostUUID, "", 100)
		require.NoError(t, err)
		require.Equal(t, []models.SourcePostAccountClaim{*first}, claims)
		end, err := repo.SourcePostLinks.AccountClaims(ctx, capture.PostUUID, first.UUID, 100)
		require.NoError(t, err)
		require.Empty(t, end)
		selected, err := repo.CapturePublisher.Current(ctx, capture.UUID)
		require.NoError(t, err)
		require.Nil(t, selected)
		publishers, err := repo.CapturePublisher.PostAccounts(ctx, capture.PostUUID, "", 100)
		require.NoError(t, err)
		require.Empty(t, publishers)
		return nil
	}))
	unlinked, err := applyPublisher(repo, publisherInput(publisherPreview(t, repo, capture.UUID, ""), "unlink"))
	require.NoError(t, err)
	later := input
	later.UUID = uuid.NewString()
	require.NoError(t, write(later))
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		current, err := repo.CapturePublisher.Current(ctx, capture.UUID)
		require.NoError(t, err)
		require.Equal(t, unlinked, current, "new legacy evidence cannot override an explicit publisher unlink")
		return nil
	}))
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM account_performer_decisions WHERE state='linked'"))
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM performers_scenes"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM performers_images"))
	_, err = raw.Exec("UPDATE source_post_account_claims SET account_uuid=?", survivor.UUID)
	require.ErrorContains(t, err, "immutable")
	_, err = raw.Exec("UPDATE source_posts SET state='forgotten' WHERE uuid=?", capture.PostUUID)
	require.NoError(t, err)
	require.NoError(t, write(input))
	input.UUID = uuid.NewString()
	require.ErrorIs(t, write(input), models.ErrSourcePostForgotten)
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
	require.NoError(t, raw.Close())
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
}

func TestSourcePostLinkFailuresCannotCommitPartialWrites(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "one"}, "")
	account := createSourceAccount(t, repo, "native:reddit")
	for _, table := range []string{"source_post_url_evidence", "source_post_identifier_evidence", "source_post_account_claims"} {
		raw := openRawDB(t, db.DatabasePath())
		_, err := raw.Exec("CREATE TRIGGER fail_post_link AFTER INSERT ON " + table + " BEGIN SELECT RAISE(FAIL,'injected failure'); END")
		require.NoError(t, err)
		require.NoError(t, raw.Close())
		err = repo.WithTxn(context.Background(), func(ctx context.Context) error {
			var err error
			switch table {
			case "source_post_url_evidence":
				_, err = repo.SourcePostLinks.ObserveURL(ctx, models.SourcePostURLInput{SourcePostEvidence: postLinkEvidence(post.UUID), URL: "https://example.test/post"})
			case "source_post_identifier_evidence":
				_, err = repo.SourcePostLinks.ObserveIdentifier(ctx, models.SourcePostIdentifierInput{SourcePostEvidence: postLinkEvidence(post.UUID), Identifier: models.SourcePostIdentifier{Namespace: "legacy:catalog:test", Value: "alias"}, ExpectedPostRevision: post.Revision})
			case "source_post_account_claims":
				_, err = repo.SourcePostLinks.ClaimAccount(ctx, models.SourcePostAccountClaimInput{SourcePostEvidence: postLinkEvidence(post.UUID), AccountUUID: account.UUID})
			}
			require.ErrorContains(t, err, "injected failure")
			return nil // Even a caller that swallows the error cannot publish a partial write.
		})
		require.ErrorContains(t, err, "did not finish atomically")
		raw = openRawDB(t, db.DatabasePath())
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_post_urls"))
		require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM source_post_identifiers"))
		_, err = raw.Exec("DROP TRIGGER fail_post_link")
		require.NoError(t, err)
		require.NoError(t, raw.Close())
		require.Equal(t, post.Revision, postLinkRevision(t, repo, post.UUID))
	}
}

func TestSourcePostLinksRejectInvalidInputsAndReadOnlyWrites(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "one"}, "")
	input := models.SourcePostURLInput{SourcePostEvidence: postLinkEvidence(post.UUID), URL: "https://example.test/post"}
	for _, change := range []func(*models.SourcePostURLInput){
		func(v *models.SourcePostURLInput) { v.UUID = "invalid" },
		func(v *models.SourcePostURLInput) { v.PostUUID = uuid.Nil.String() },
		func(v *models.SourcePostURLInput) { v.Origin = "unknown" },
		func(v *models.SourcePostURLInput) { v.Basis = " " },
		func(v *models.SourcePostURLInput) { v.ObservedAt = time.Time{} },
		func(v *models.SourcePostURLInput) { v.ObservedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) },
		func(v *models.SourcePostURLInput) { v.Details = json.RawMessage(`{"same":1,"same":2}`) },
		func(v *models.SourcePostURLInput) { v.Details = json.RawMessage(`[]`) },
		func(v *models.SourcePostURLInput) {
			v.Details = json.RawMessage(`{"data":"` + strings.Repeat("x", 65536) + `"}`)
		},
		func(v *models.SourcePostURLInput) { v.URL = "file:///tmp/private" },
		func(v *models.SourcePostURLInput) { v.URL = "https://name:secret@example.test/post" },
		func(v *models.SourcePostURLInput) { v.URL = " https://example.test/post" },
		func(v *models.SourcePostURLInput) { v.URL = "https://example.test/" + strings.Repeat("x", 8192) },
	} {
		invalid := input
		change(&invalid)
		_, err := observePostURL(repo, invalid)
		require.ErrorIs(t, err, models.ErrSourcePostEvidenceInvalid)
	}
	require.Error(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.SourcePostLinks.ObserveURL(ctx, input)
		return err
	}))
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		_, err := repo.SourcePostLinks.URLs(ctx, post.UUID, "invalid", 10)
		require.Error(t, err)
		_, err = repo.SourcePostLinks.URLs(ctx, post.UUID, "", 101)
		require.Error(t, err)
		return nil
	}))
	require.Equal(t, post.Revision, postLinkRevision(t, repo, post.UUID))
}

func TestSourcePostLinksStartupRejectsIncompleteEvidence(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "one"}, "")
	_, err := observePostURL(repo, models.SourcePostURLInput{SourcePostEvidence: postLinkEvidence(post.UUID), URL: "https://example.test/post"})
	require.NoError(t, err)
	require.NoError(t, db.Close())
	raw := openRawDB(t, db.DatabasePath())
	_, err = raw.Exec("DELETE FROM source_post_url_evidence")
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	require.ErrorContains(t, db.Open(db.DatabasePath()), "incomplete source post link evidence")
}
