package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func sourceStudioCollection(t *testing.T, repo models.Repository) *models.SourceCollection {
	return putSourceCollection(t, repo, models.SourceCollectionInput{UUID: uuid.NewString(), Origin: "review",
		SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Subreddit", Kind: "subreddit", Namespace: "native:reddit", State: "active", TargetURL: "https://www.reddit.com/r/example/"}})
}

func sourceStudioMember(t *testing.T, repo models.Repository, collection *models.SourceCollection, post string) {
	t.Helper()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourceCollection.RecordPostMembership(ctx, models.CollectionPostMembership{
			SourcePostEvidence: models.SourcePostEvidence{UUID: uuid.NewString(), PostUUID: post, Origin: "migration", Basis: "catalog-membership", ObservedAt: time.Now().UTC(), Details: []byte(`{}`)},
			CollectionUUID:     collection.UUID, CollectionRevision: collection.Revision,
		})
		return err
	}))
}

func assignSourceStudio(t *testing.T, repo models.Repository, c string, revision int, studio *string) *models.SourceStudioAssignment {
	t.Helper()
	var ret *models.SourceStudioAssignment
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceStudio.Assign(ctx, c, models.SourceStudioInput{ExpectedRevision: revision, StudioUUID: studio})
		return err
	}))
	return ret
}

func TestSourceStudiosUnifyPostsAndMixedAlbumsWithoutChoosingAnArbitraryStudio(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	attachmentSQL(t, db, archiveMetadataFixture)
	studio := archiveFind(t, repo, models.ArchiveStudio, 91)
	attachmentSQL(t, db, "INSERT INTO studios(id,name,created_at,updated_at) VALUES(191,'Another subreddit',datetime('now'),datetime('now'))")
	other := archiveFind(t, repo, models.ArchiveStudio, 191)
	c := sourceStudioCollection(t, repo)
	d := sourceStudioCollection(t, repo)
	p := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "album"}, "")
	sourceStudioMember(t, repo, c, p.UUID)
	sourceStudioMember(t, repo, c, p.UUID) // repeated captures/membership must not duplicate studios
	video := sourceAttachmentEntry(1, "second")
	video.MediaKind = "video"
	selection := selectAlbum(t, repo, p.UUID, models.SourceAttachmentManifestInput{Complete: true, Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, "first"), video}})
	chooseAlbumMedia(t, repo, selection.Entries[0].Attachment.UUID, models.ArchiveImage, 41)
	chooseAlbumMedia(t, repo, selection.Entries[1].Attachment.UUID, models.ArchiveScene, 31)
	g := syncSourceGallery(t, repo, p.UUID)
	assignSourceStudio(t, repo, c.UUID, 0, &studio.UUID)
	require.JSONEq(t, `"`+studio.UUID+`"`, string(metadataState(t, repo, g.GalleryUUID, "studio").Value))
	require.False(t, metadataState(t, repo, g.GalleryUUID, "studio").Protected)
	require.Len(t, readGalleryPosts(t, repo, g.GalleryUUID, "", 25).Studios, 1)
	// Source configuration revisions remain valid for in-flight workers.
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		current, err := repo.SourceCollection.Find(ctx, c.UUID)
		require.NoError(t, err)
		require.Equal(t, c.Revision, current.Revision)
		studios, err := repo.SourceStudio.ForPost(ctx, p.UUID)
		require.Len(t, studios, 1)
		return err
	}))
	assignSourceStudio(t, repo, d.UUID, 0, &other.UUID)
	sourceStudioMember(t, repo, d, p.UUID)
	require.JSONEq(t, `null`, string(metadataState(t, repo, g.GalleryUUID, "studio").Value))
	require.Len(t, readGalleryPosts(t, repo, g.GalleryUUID, "", 25).Studios, 2)
	assignSourceStudio(t, repo, d.UUID, 1, nil)
	require.JSONEq(t, `"`+studio.UUID+`"`, string(metadataState(t, repo, g.GalleryUUID, "studio").Value))
	// Explicit gallery clears are protected even if the source changes later.
	_, err := applyMetadata(repo, metadataInput(metadataState(t, repo, g.GalleryUUID, "studio"), "clear", "review", ""), false)
	require.NoError(t, err)
	assignSourceStudio(t, repo, c.UUID, 1, &other.UUID)
	require.JSONEq(t, `null`, string(metadataState(t, repo, g.GalleryUUID, "studio").Value))
	require.True(t, metadataState(t, repo, g.GalleryUUID, "studio").Protected)
	_, err = applyMetadata(repo, metadataInput(metadataState(t, repo, g.GalleryUUID, "studio"), "inherit", "review", "null"), false)
	require.NoError(t, err)
	require.JSONEq(t, `"`+other.UUID+`"`, string(metadataState(t, repo, g.GalleryUUID, "studio").Value))
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Studio.Destroy(ctx, 191) }))
	require.False(t, metadataState(t, repo, g.GalleryUUID, "studio").Protected)
	require.Empty(t, readGalleryPosts(t, repo, g.GalleryUUID, "", 25).Studios)
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_gallery_studio_pending"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestSourceStudioAssignmentRetriesConflictsAndRollback(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	attachmentSQL(t, db, archiveMetadataFixture)
	c := sourceStudioCollection(t, repo)
	studio := archiveFind(t, repo, models.ArchiveStudio, 91)
	first := assignSourceStudio(t, repo, c.UUID, 0, &studio.UUID)
	require.Equal(t, first, assignSourceStudio(t, repo, c.UUID, 0, &studio.UUID))
	assignSourceStudio(t, repo, c.UUID, 1, nil)
	err := repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourceStudio.Assign(ctx, c.UUID, models.SourceStudioInput{StudioUUID: &studio.UUID})
		return err
	})
	require.ErrorIs(t, err, models.ErrSourceDefinitionConflict)
	err = repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.SourceStudio.Assign(ctx, c.UUID, models.SourceStudioInput{ExpectedRevision: 2, StudioUUID: &studio.UUID})
		if err != nil {
			return err
		}
		return errors.New("rollback")
	})
	require.ErrorContains(t, err, "rollback")
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		current, err := repo.SourceStudio.Assignment(ctx, c.UUID)
		require.Equal(t, 2, current.Revision)
		require.Nil(t, current.Studio)
		return err
	}))
}

func TestSourceStudiosConsolidateDifferentInheritedStudios(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	attachmentSQL(t, db, archiveMetadataFixture)
	attachmentSQL(t, db, `INSERT INTO images(id,title,created_at,updated_at) VALUES(42,'Second image',datetime('now'),datetime('now'));
INSERT INTO studios(id,name,created_at,updated_at) VALUES(191,'Second studio',datetime('now'),datetime('now'))`)
	var galleries []*models.SourceGallerySyncResult
	for i, studioID := range []int{91, 191} {
		c := sourceStudioCollection(t, repo)
		studio := archiveFind(t, repo, models.ArchiveStudio, studioID)
		assignSourceStudio(t, repo, c.UUID, 0, &studio.UUID)
		post := sourceTestPost(t, repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: uuid.NewString()}, "")
		sourceStudioMember(t, repo, c, post.UUID)
		video := sourceAttachmentEntry(1, uuid.NewString())
		video.MediaKind = "video"
		selection := selectAlbum(t, repo, post.UUID, models.SourceAttachmentManifestInput{Complete: true, Entries: []models.SourceAttachmentEntry{sourceAttachmentEntry(0, uuid.NewString()), video}})
		chooseAlbumMedia(t, repo, selection.Entries[0].Attachment.UUID, models.ArchiveImage, 41+i)
		chooseAlbumMedia(t, repo, selection.Entries[1].Attachment.UUID, models.ArchiveScene, 31+i)
		galleries = append(galleries, syncSourceGallery(t, repo, post.UUID))
	}
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error { return repo.Gallery.AddImages(ctx, *galleries[1].GalleryID, 41) }))
	p := consolidationPreview(t, repo, galleries[0].GalleryUUID)
	require.Empty(t, p.Conflicts)
	merged := consolidationApply(t, repo, galleries[0].GalleryUUID, p)
	require.Len(t, readGalleryPosts(t, repo, merged.GalleryUUID, "", 25).Studios, 2)
	require.JSONEq(t, `null`, string(metadataState(t, repo, merged.GalleryUUID, "studio").Value))
	require.False(t, metadataState(t, repo, merged.GalleryUUID, "studio").Protected)
}

func removeSourceStudioSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	if queryUint(t, db, "SELECT count(*) FROM sqlite_schema WHERE name='source_collection_studios'") == 0 {
		return
	}
	rows, err := db.Query("SELECT name FROM sqlite_schema WHERE type='trigger' AND (name LIKE 'source_gallery_studio_%' OR name IN ('source_collection_studio_removed','source_collection_post_evidence_studio','source_collection_captures_studio'))")
	require.NoError(t, err)
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		names = append(names, name)
	}
	require.NoError(t, rows.Err())
	for _, name := range names {
		_, err = db.Exec("DROP TRIGGER " + name)
		require.NoError(t, err)
	}
	_, err = db.Exec("DROP VIEW source_post_studio_collections; DROP TABLE source_gallery_studio_pending; DROP TABLE source_collection_studios; DELETE FROM native_migration_history WHERE version=1000119")
	require.NoError(t, err)
}

func TestSourceStudioMigrationPreservesLibraryMetadata(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	g := consolidationGallery(t, repo, "Reviewed", []int{41}, nil)
	before := metadataState(t, repo, g.UUID, "studio")
	path := db.DatabasePath()
	require.NoError(t, db.Close())
	raw := openRawDB(t, path)
	defer raw.Close()
	removeSourceStudioSchema(t, raw)
	_, err := raw.Exec("UPDATE schema_migrations SET version=1000118")
	require.NoError(t, err)
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	require.Equal(t, before, metadataState(t, db.Repository(), g.UUID, "studio"))
}
