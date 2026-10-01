package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

const archiveMetadataFixture = `
INSERT INTO tags(id, name, created_at, updated_at) VALUES
 (81, 'Identity tag A', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP), (82, 'Identity tag B', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
INSERT INTO studios(id, name, created_at, updated_at) VALUES
 (91, 'Identity studio A', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP), (92, 'Identity studio B', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
INSERT INTO groups(id, name, created_at, updated_at) VALUES
 (101, 'Identity group A', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP), (102, 'Identity group B', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
`

func TestArchiveMetadataIdentityLifecycle(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	attachmentSQL(t, db, archiveMetadataFixture)
	for _, tc := range []struct {
		kind  models.ArchiveEntityKind
		table string
		id    int
	}{{models.ArchiveTag, "tags", 81}, {models.ArchiveStudio, "studios", 91}, {models.ArchiveGroup, "groups", 101}} {
		t.Run(string(tc.kind), func(t *testing.T) {
			original := archiveFind(t, repo, tc.kind, tc.id)
			require.Equal(t, 1, original.Revision)
			attachmentSQL(t, db, "UPDATE "+tc.table+" SET name='Renamed definition' WHERE id=?", tc.id)
			edited := archiveFind(t, repo, tc.kind, tc.id)
			require.Equal(t, original.UUID, edited.UUID)
			require.Greater(t, edited.Revision, original.Revision)
			adopted := uuid.NewString()
			require.ErrorIs(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
				_, err := repo.ArchiveEntity.AdoptUUID(ctx, original.UUID, adopted, original.Revision)
				return err
			}), models.ErrArchiveIdentityConflict)
			require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
				_, err := repo.ArchiveEntity.AdoptUUID(ctx, edited.UUID, adopted, edited.Revision)
				return err
			}))
			attachmentSQL(t, db, "DELETE FROM "+tc.table+" WHERE id=?", tc.id)
			attachmentSQL(t, db, "INSERT INTO "+tc.table+"(id, name, created_at, updated_at) VALUES (?, 'Different definition', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)", tc.id)
			replacement := archiveFind(t, repo, tc.kind, tc.id)
			require.NotEqual(t, adopted, replacement.UUID)
			require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
				retired, err := repo.ArchiveEntity.Resolve(ctx, original.UUID)
				require.NoError(t, err)
				require.Equal(t, adopted, retired.UUID)
				require.Equal(t, models.ArchiveEntityDeleted, retired.State)
				require.Equal(t, tc.id, *retired.OriginalID)
				require.Nil(t, retired.LocalID)
				return nil
			}))
		})
	}
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	for _, column := range []string{"tag_id", "studio_id", "group_id"} {
		var unused1, unused2, unused3 int
		var plan string
		require.NoError(t, raw.QueryRow("EXPLAIN QUERY PLAN SELECT * FROM archive_entities WHERE "+column+"=1").Scan(&unused1, &unused2, &unused3, &plan))
		require.Contains(t, plan, "USING INDEX archive_entities_")
	}
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestArchiveMetadataRelationshipRevisions(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	attachmentSQL(t, db, archiveMetadataFixture)
	for _, tc := range []struct {
		kind  models.ArchiveEntityKind
		id    int
		query string
	}{
		{models.ArchiveTag, 81, "INSERT INTO tag_aliases(tag_id, alias) VALUES (81, 'Former tag name')"},
		{models.ArchiveTag, 81, "INSERT INTO tag_stash_ids(tag_id, endpoint, stash_id) VALUES (81, 'https://example.test/graphql', 'remote-tag')"},
		{models.ArchiveTag, 81, "INSERT INTO tag_custom_fields(tag_id, field, value) VALUES (81, 'note', 'text')"},
		{models.ArchiveTag, 81, "INSERT INTO tags_relations(parent_id, child_id) VALUES (81, 82)"},
		{models.ArchiveTag, 82, "DELETE FROM tags_relations WHERE child_id=82"},
		{models.ArchiveStudio, 91, "INSERT INTO studio_aliases(studio_id, alias) VALUES (91, 'Former studio name')"},
		{models.ArchiveStudio, 91, "INSERT INTO studio_urls(studio_id, position, url) VALUES (91, 0, 'https://example.test/studio')"},
		{models.ArchiveStudio, 91, "INSERT INTO studio_stash_ids(studio_id, endpoint, stash_id) VALUES (91, 'https://example.test/graphql', 'remote-studio')"},
		{models.ArchiveStudio, 91, "INSERT INTO studio_custom_fields(studio_id, field, value) VALUES (91, 'note', 'text')"},
		{models.ArchiveStudio, 91, "INSERT INTO studios_tags(studio_id, tag_id) VALUES (91, 81)"},
		{models.ArchiveGroup, 101, "INSERT INTO group_urls(group_id, position, url) VALUES (101, 0, 'https://example.test/group')"},
		{models.ArchiveGroup, 101, "INSERT INTO group_custom_fields(group_id, field, value) VALUES (101, 'note', 'text')"},
		{models.ArchiveGroup, 101, "INSERT INTO groups_tags(group_id, tag_id) VALUES (101, 81)"},
		{models.ArchiveGroup, 101, "INSERT INTO groups_relations(containing_id, sub_id, order_index) VALUES (101, 102, 0)"},
		{models.ArchiveGroup, 102, "UPDATE groups_relations SET description='Edited relation' WHERE sub_id=102"},
	} {
		before := archiveFind(t, repo, tc.kind, tc.id)
		attachmentSQL(t, db, tc.query)
		after := archiveFind(t, repo, tc.kind, tc.id)
		require.Equal(t, before.UUID, after.UUID)
		require.Greater(t, after.Revision, before.Revision, tc.query)
	}
	// Moving a relation invalidates both the former and the new owner.
	before := archiveFind(t, repo, models.ArchiveStudio, 91)
	other := archiveFind(t, repo, models.ArchiveStudio, 92)
	attachmentSQL(t, db, "UPDATE studio_urls SET studio_id=92 WHERE studio_id=91")
	require.Greater(t, archiveFind(t, repo, models.ArchiveStudio, 91).Revision, before.Revision)
	require.Greater(t, archiveFind(t, repo, models.ArchiveStudio, 92).Revision, other.Revision)
}

func TestArchiveTagMergeKeepsAliasesAndPortableIdentity(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	attachmentSQL(t, db, archiveMetadataFixture)
	attachmentSQL(t, db, "INSERT INTO scenes_tags(scene_id, tag_id) VALUES (31, 81), (31, 82)")
	attachmentSQL(t, db, "INSERT INTO tag_stash_ids(tag_id, endpoint, stash_id) VALUES (81, 'https://example.test/graphql', 'remote-tag')")
	source := archiveFind(t, repo, models.ArchiveTag, 81)
	target := archiveFind(t, repo, models.ArchiveTag, 82)
	require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error { return repo.Tag.Merge(ctx, []int{81}, 82) }))
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		resolved, err := repo.ArchiveEntity.Resolve(ctx, source.UUID)
		require.NoError(t, err)
		require.Equal(t, target.UUID, resolved.UUID)
		require.Equal(t, 82, *resolved.LocalID)
		aliases, err := repo.Tag.GetAliases(ctx, 82)
		require.NoError(t, err)
		require.Contains(t, aliases, "Identity tag A")
		remote, err := repo.Tag.GetStashIDs(ctx, 82)
		require.NoError(t, err)
		require.Len(t, remote, 1)
		require.Equal(t, "remote-tag", remote[0].StashID)
		tags, err := repo.Scene.GetTagIDs(ctx, 31)
		require.NoError(t, err)
		require.Equal(t, []int{82}, tags)
		return nil
	}))
	raw := openRawDB(t, db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec("UPDATE archive_entities SET studio_id=91 WHERE tag_id=82")
	require.ErrorContains(t, err, "CHECK constraint")
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestArchiveMetadataRedirectMustFinishInOneTransaction(t *testing.T) {
	db, repo := archiveTestDatabase(t)
	attachmentSQL(t, db, archiveMetadataFixture)
	for _, tc := range []struct {
		kind     models.ArchiveEntityKind
		table    string
		from, to int
	}{{models.ArchiveTag, "tags", 81, 82}, {models.ArchiveStudio, "studios", 91, 92}, {models.ArchiveGroup, "groups", 101, 102}} {
		source := archiveFind(t, repo, tc.kind, tc.from)
		target := archiveFind(t, repo, tc.kind, tc.to)
		require.ErrorContains(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
			return repo.ArchiveEntity.Redirect(ctx, source.UUID, target.UUID, source.Revision)
		}), "source record without a current identity")
		require.Equal(t, source, archiveFind(t, repo, tc.kind, tc.from))
		require.NoError(t, repo.WithTxn(context.Background(), func(ctx context.Context) error {
			if err := repo.ArchiveEntity.Redirect(ctx, source.UUID, target.UUID, source.Revision); err != nil {
				return err
			}
			_, _, err := db.ExecSQL(ctx, "DELETE FROM "+tc.table+" WHERE id=?", []interface{}{tc.from})
			return err
		}))
		require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
			resolved, err := repo.ArchiveEntity.Resolve(ctx, source.UUID)
			require.NoError(t, err)
			require.Equal(t, target.UUID, resolved.UUID)
			return nil
		}))
	}
}

func TestArchiveMetadataMigrationPreservesSourceGalleryReferences(t *testing.T) {
	config.InitializeEmpty()
	path := filepath.Join(t.TempDir(), "before-metadata-identities.sqlite")
	buildLegacyDatabase(t, path, 86, true)
	db := sqlite.NewDatabase()
	var needed *sqlite.MigrationNeededError
	require.True(t, errors.As(db.Open(path), &needed))
	m, err := sqlite.NewMigrator(db)
	require.NoError(t, err)
	for version := m.CurrentSchemaVersion(); version < sqlite.NativeSchemaBaseline+10; version = m.CurrentSchemaVersion() {
		require.NoError(t, m.RunMigration(context.Background(), m.GetNextMigrationVersion(version)))
	}
	m.Close()
	raw := openRawDB(t, path)
	_, err = raw.Exec(archiveIdentityFixture + archiveMetadataFixture)
	require.NoError(t, err)
	_, err = raw.Exec(`INSERT INTO galleries(id, origin, title, created_at, updated_at) VALUES (111, 'source', 'Kept album', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP);
INSERT INTO galleries_images(gallery_id, image_id, cover) VALUES (111, 41, 1);
INSERT INTO galleries_tags(gallery_id, tag_id) VALUES (111, 81);
UPDATE scenes SET studio_id=91 WHERE id=31;
INSERT INTO groups_scenes(group_id, scene_id) VALUES (101, 31);`)
	require.NoError(t, err)
	post, association := uuid.NewString(), uuid.NewString()
	_, err = raw.Exec("INSERT INTO source_posts(uuid) VALUES (?)", post)
	require.NoError(t, err)
	var galleryUUID, membershipUUID string
	var galleryRevision int
	require.NoError(t, raw.QueryRow("SELECT uuid, revision FROM archive_entities WHERE gallery_id=111").Scan(&galleryUUID, &galleryRevision))
	require.NoError(t, raw.QueryRow("SELECT uuid FROM gallery_membership_events WHERE gallery_uuid=?", galleryUUID).Scan(&membershipUUID))
	_, err = raw.Exec(`INSERT INTO post_gallery_decisions(uuid, post_uuid, revision, state, gallery_uuid, origin) VALUES (?, ?, 1, 'linked', ?, 'review')`, association, post, galleryUUID)
	require.NoError(t, err)
	_, err = raw.Exec("INSERT INTO post_gallery_links(post_uuid, decision_uuid, gallery_uuid) VALUES (?, ?, ?)", post, association, galleryUUID)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	require.NoError(t, db.RunAllMigrations())
	require.NoError(t, db.ReInitialise())
	defer db.Close()
	repo := db.Repository()
	gallery := archiveFind(t, repo, models.ArchiveGallery, 111)
	require.Equal(t, galleryUUID, gallery.UUID)
	require.Equal(t, galleryRevision, gallery.Revision)
	require.Equal(t, membershipUUID, sourceGalleryHistory(t, repo, galleryUUID)[0].UUID)
	require.NoError(t, repo.WithReadTxn(context.Background(), func(ctx context.Context) error {
		link, err := repo.SourceGallery.Association(ctx, post)
		require.NoError(t, err)
		require.Equal(t, association, link.UUID)
		require.Equal(t, galleryUUID, *link.GalleryUUID)
		return nil
	}))
	for kind, ids := range map[models.ArchiveEntityKind][]int{models.ArchiveTag: {81, 82}, models.ArchiveStudio: {91, 92}, models.ArchiveGroup: {101, 102}} {
		for _, id := range ids {
			identity := archiveFind(t, repo, kind, id)
			require.Equal(t, 1, identity.Revision)
			require.Equal(t, id, *identity.LocalID)
		}
	}
	raw = openRawDB(t, path)
	defer raw.Close()
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM galleries_tags WHERE gallery_id=111 AND tag_id=81"))
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM scenes WHERE id=31 AND studio_id=91"))
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM groups_scenes WHERE group_id=101 AND scene_id=31"))
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM galleries_images WHERE gallery_id=111 AND image_id=41 AND cover=1"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM sqlite_schema WHERE name='native_metadata_identity_rows'"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}
