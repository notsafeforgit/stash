package sqlite_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func archivePageInfo(t *testing.T, repo models.Repository, filter models.ArchivePageFilter) *models.ArchivePageInfo {
	t.Helper()
	var info *models.ArchivePageInfo
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		info, err = repo.ArchivePagination.PageInfo(ctx, filter)
		return err
	}))
	return info
}

func TestArchivePaginationEmptyResourcesAndValidation(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	for _, resource := range []string{"media-roots", "collections", "accounts", "posts", "jobs", "runs", "root-history", "collection-history", "policy-history", "provider-history", "job-attempts", "run-attempts", "review-accounts", "review-media", "review-metadata", "shares"} {
		t.Run(resource, func(t *testing.T) {
			info := archivePageInfo(t, repo, models.ArchivePageFilter{Resource: resource, Subject: uuid.NewString()})
			require.EqualValues(t, 0, info.Total)
			require.Zero(t, info.Offset)
		})
	}
	for _, kind := range []string{"catalog", "automation"} {
		require.Zero(t, archivePageInfo(t, repo, models.ArchivePageFilter{Resource: "imports", Kind: kind}).Total)
	}
	for _, filter := range []models.ArchivePageFilter{{Resource: "arbitrary_table"}, {Resource: "media-roots", State: "invalid"}, {Resource: "jobs", Cursor: "-1"}, {Resource: "posts", Cursor: "bad"}, {Resource: "root-history"}, {Resource: "accounts", Scope: "bad"}} {
		err := repo.WithReadTxn(t.Context(), func(ctx context.Context) error { _, err := repo.ArchivePagination.PageInfo(ctx, filter); return err })
		require.ErrorIs(t, err, models.ErrArchivePageInvalid)
	}
}

func TestArchivePaginationFilteredRootsAndRevisionGaps(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	var roots []*models.MediaRoot
	for i := range 26 {
		roots = append(roots, putMediaRoot(t, repo, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: fmt.Sprintf("Library %02d", i), State: "disabled"}}))
	}
	var rows []*models.MediaRoot
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		rows, err = repo.MediaRoot.Search(ctx, models.SourceDefinitionFilter{Limit: 25, State: "disabled"})
		return err
	}))
	info := archivePageInfo(t, repo, models.ArchivePageFilter{Resource: "media-roots", State: "disabled", Cursor: rows[24].UUID})
	require.EqualValues(t, 26, info.Total)
	require.EqualValues(t, 25, info.Offset)
	require.EqualValues(t, 0, archivePageInfo(t, repo, models.ArchivePageFilter{Resource: "media-roots", Query: "Library %"}).Total, "LIKE wildcards must remain literal")
	require.EqualValues(t, 1, archivePageInfo(t, repo, models.ArchivePageFilter{Resource: "media-roots", Query: "Library 00"}).Total)
	require.Zero(t, archivePageInfo(t, repo, models.ArchivePageFilter{Resource: "media-roots", State: "active"}).Total)
	root := roots[0]
	putMediaRoot(t, repo, models.MediaRootInput{UUID: root.UUID, ExpectedRevision: 1, Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Renamed", State: "disabled"}})
	info = archivePageInfo(t, repo, models.ArchivePageFilter{Resource: "root-history", Subject: root.UUID, Cursor: "1"})
	require.EqualValues(t, 2, info.Total)
	require.EqualValues(t, 1, info.Offset)
}

func TestArchivePaginationAccountsAndPostSelectors(t *testing.T) {
	_, repo := archiveTestDatabase(t)
	account := createSourceAccount(t, repo, "native:reddit")
	putSourceCollection(t, repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Subscribed", Kind: "account", Namespace: account.Namespace, State: "active", AccountUUID: &account.UUID}})
	createSourceAccount(t, repo, "native:twitter")
	require.EqualValues(t, 1, archivePageInfo(t, repo, models.ArchivePageFilter{Resource: "accounts"}).Total)
	require.EqualValues(t, 2, archivePageInfo(t, repo, models.ArchivePageFilter{Resource: "accounts", Scope: "all"}).Total)
	require.EqualValues(t, 1, archivePageInfo(t, repo, models.ArchivePageFilter{Resource: "accounts", Scope: "all", Namespace: "native:twitter"}).Total)
	require.Zero(t, archivePageInfo(t, repo, models.ArchivePageFilter{Resource: "accounts", Ownership: "linked"}).Total)
	post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "page-count"}, "")
	sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "other"}, "")
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourcePostLinks.ObserveURL(ctx, models.SourcePostURLInput{SourcePostEvidence: models.SourcePostEvidence{UUID: uuid.NewString(), PostUUID: post.UUID, Origin: "migration", Basis: "retained", ObservedAt: time.Now(), Details: []byte(`{}`)}, URL: "https://example.com/post"})
		return err
	}))
	for _, f := range []models.ArchivePageFilter{
		{Resource: "posts", PostUUID: post.UUID}, {Resource: "posts", URL: "https://example.com/post"}, {Resource: "posts", Namespace: "native:reddit", Value: "page-count"},
	} {
		require.EqualValues(t, 1, archivePageInfo(t, repo, f).Total)
	}
	require.EqualValues(t, 2, archivePageInfo(t, repo, models.ArchivePageFilter{Resource: "posts"}).Total)
}
