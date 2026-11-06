package sqlite_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/dedup"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/models/paths"
	"github.com/stashapp/stash/pkg/scene"
	"github.com/stretchr/testify/require"
)

type dedupSceneConfig struct{}

func (dedupSceneConfig) GetVideoFileNamingAlgorithm() models.HashAlgorithm {
	return models.HashAlgorithmMd5
}

func separateDedupOwners(t *testing.T, isImage bool) (deduplicationFixture, *scene.Service, int, int) {
	t.Helper()
	f := newDeduplicationFixture(t, isImage)
	table, relation, column, first, second := "scenes", "scenes_files", "scene_id", 31, 32
	if isImage {
		table, relation, column, first, second = "images", "images_files", "image_id", 41, 42
		attachmentSQL(t, f.db, "INSERT INTO images(id,title,details,created_at,updated_at) VALUES(42,'Later title','Later details',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)")
	}
	attachmentSQL(t, f.db, "UPDATE "+relation+" SET "+column+"="+strconv.Itoa(second)+" WHERE file_id="+f.duplicate.Base().ID.String())
	attachmentSQL(t, f.db, "UPDATE "+relation+" SET \"primary\"=1 WHERE file_id="+f.file.Base().ID.String())
	attachmentSQL(t, f.db, "UPDATE "+table+" SET created_at='2020-01-01 00:00:00' WHERE id="+strconv.Itoa(first))
	if !isImage {
		attachmentSQL(t, f.db, "UPDATE scenes SET details='Later details' WHERE id=32")
	}
	p := paths.NewPaths(t.TempDir(), t.TempDir())
	s := &scene.Service{File: f.repo.File, Repository: f.repo.Scene, MarkerRepository: f.repo.SceneMarker, Config: dedupSceneConfig{}, Paths: &p}
	f.service = dedup.New(f.repo, "").WithScenes(s)
	f.input.MediaPolicy = models.FileDeduplicationEarliest
	return f, s, first, second
}

func TestFileDeduplicationReviewedMediaPolicy(t *testing.T) {
	for _, isImage := range []bool{false, true} {
		for _, scenario := range []string{"fill", "explicit_clear", "oldest_is_removed_file", "stale_source", "different_bytes", "rollback"} {
			t.Run(strconv.FormatBool(isImage)+"/"+scenario, func(t *testing.T) {
				f, scenes, first, second := separateDedupOwners(t, isImage)
				kind, table, title := models.ArchiveScene, "scenes", "Kept title"
				if isImage {
					kind, table, title = models.ArchiveImage, "images", "Kept image"
				}
				if scenario == "oldest_is_removed_file" {
					attachmentSQL(t, f.db, "UPDATE "+table+" SET created_at='2010-01-01 00:00:00' WHERE id="+strconv.Itoa(second))
					first, second = second, first
					if isImage {
						title = "Later title"
					}
				}
				destination := archiveFind(t, f.repo, kind, first)
				source := archiveFind(t, f.repo, kind, second)
				if scenario == "explicit_clear" {
					require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
						_, err := f.repo.MetadataField.Decide(ctx, models.MetadataFieldDecisionInput{EntityUUID: destination.UUID, ExpectedEntityRevision: destination.Revision, Field: "details", Mode: "clear", Origin: "review", Reason: "User deliberately cleared details"})
						return err
					}))
				}
				var artwork string
				if !isImage {
					artwork = scenes.Paths.Scene.GetSpriteImageFilePath("fixture-md5")
					require.NoError(t, os.MkdirAll(filepath.Dir(artwork), 0700))
					require.NoError(t, os.WriteFile(artwork, []byte("survivor artwork"), 0600))
				}
				request := f.request(t)
				switch scenario {
				case "stale_source":
					attachmentSQL(t, f.db, "UPDATE "+table+" SET title='Changed during hashing' WHERE id="+strconv.Itoa(second))
				case "different_bytes":
					require.NoError(t, os.WriteFile(f.duplicate.Base().Path, []byte("different data"), 0600))
				case "rollback":
					repo := f.repo
					repo.File = failDeduplicationDestroy{FileReaderWriter: repo.File, after: func(context.Context) error { return errors.New("injected failure after staging") }}
					f.service = dedup.New(repo, "").WithScenes(scenes)
				}
				result, err := f.service.Apply(t.Context(), request)
				if scenario == "stale_source" || scenario == "different_bytes" || scenario == "rollback" {
					require.Error(t, err)
					if scenario == "different_bytes" {
						require.True(t, errors.Is(err, archive.ErrMediaFileDigest) || errors.Is(err, models.ErrFileDeduplicationConflict))
					}
					f.requireIntact(t)
					require.Equal(t, models.ArchiveEntityActive, archiveFind(t, f.repo, kind, second).State)
					return
				}
				require.NoError(t, err)
				require.Equal(t, destination.UUID, result.MediaUUID)
				require.Equal(t, models.FileDeduplicationEarliest, result.MediaPolicy)
				require.FileExists(t, f.file.Base().Path)
				require.NoFileExists(t, f.duplicate.Base().Path)
				if artwork != "" {
					require.FileExists(t, artwork)
				}
				require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
					src, err := f.repo.ArchiveEntity.Find(ctx, source.UUID)
					require.NoError(t, err)
					require.Equal(t, models.ArchiveEntityRedirected, src.State)
					d, err := f.repo.MetadataField.State(ctx, destination.UUID, "details")
					require.NoError(t, err)
					if scenario == "explicit_clear" {
						require.JSONEq(t, `""`, string(d.Value))
						require.Equal(t, "clear", d.Mode)
					} else {
						require.JSONEq(t, `"Later details"`, string(d.Value))
					}
					titleField, err := f.repo.MetadataField.State(ctx, destination.UUID, "title")
					require.NoError(t, err)
					require.JSONEq(t, strconv.Quote(title), string(titleField.Value))
					return nil
				}))
				// Lost responses and later restarts must not repeat logical merges.
				require.NoError(t, f.db.Close())
				require.NoError(t, f.db.Open(f.db.DatabasePath()))
				replayed, err := dedup.New(f.db.Repository(), "").Apply(t.Context(), request)
				require.NoError(t, err)
				require.Equal(t, result, replayed)
			})
		}
	}
}

func TestFileDeduplicationDoesNotMergeUnverifiedMediaVariants(t *testing.T) {
	for _, image := range []bool{false, true} {
		for _, late := range []bool{false, true} {
			t.Run(strconv.FormatBool(image)+"/late="+strconv.FormatBool(late), func(t *testing.T) {
				f, _, _, second := separateDedupOwners(t, image)
				var request dedup.Request
				if late {
					request = f.request(t)
				}
				extra := f.addFile(t, "another-version.mp4", []byte("unrelated rendition"))
				relation, column := "scenes_files", "scene_id"
				if image {
					relation, column = "images_files", "image_id"
				}
				attachmentSQL(t, f.db, "INSERT INTO "+relation+"("+column+",file_id,\"primary\") VALUES("+strconv.Itoa(second)+","+extra.Base().ID.String()+",0)")
				if late {
					_, err := f.service.Apply(t.Context(), request)
					require.ErrorIs(t, err, models.ErrFileDeduplicationConflict)
				} else {
					preview, err := f.service.Preview(t.Context(), f.input)
					require.NoError(t, err)
					require.False(t, preview.Eligible)
					require.Equal(t, "media_variants_require_review", preview.BlockedReason)
				}
				f.requireIntact(t)
				require.FileExists(t, extra.Base().Path)
			})
		}
	}
}

func TestFileDeduplicationPreservesGalleryMembershipsAndURLs(t *testing.T) {
	for _, isImage := range []bool{false, true} {
		t.Run(strconv.FormatBool(isImage), func(t *testing.T) {
			f, _, first, second := separateDedupOwners(t, isImage)
			kind := models.ArchiveScene
			if isImage {
				kind = models.ArchiveImage
			}
			galleries := []int{createArchiveGallery(t, f.repo, "First post").ID, createArchiveGallery(t, f.repo, "Later post").ID}
			for i, id := range []int{first, second} {
				require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
					if isImage {
						return f.repo.Gallery.AddImages(ctx, galleries[i], id)
					}
					return f.repo.Gallery.AddSceneIDs(ctx, galleries[i], []int{id})
				}))
			}
			destination, source := archiveFind(t, f.repo, kind, first), archiveFind(t, f.repo, kind, second)
			for i, entity := range []*models.ArchiveEntity{destination, source} {
				require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
					state, err := f.repo.MetadataField.State(ctx, entity.UUID, "urls")
					if err != nil {
						return err
					}
					_, err = f.repo.MetadataField.Decide(ctx, models.MetadataFieldDecisionInput{EntityUUID: entity.UUID,
						ExpectedEntityRevision: state.Entity.Revision, Field: "urls", Mode: "set", Origin: "review",
						Value: []byte(`["https://example.com/post/` + strconv.Itoa(i) + `"]`)})
					return err
				}))
			}
			_, err := f.service.Apply(t.Context(), f.request(t))
			require.NoError(t, err)
			require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				var actual []int
				var err error
				if isImage {
					actual, err = f.repo.Image.GetGalleryIDs(ctx, first)
				} else {
					actual, err = f.repo.Scene.GetGalleryIDs(ctx, first)
				}
				require.NoError(t, err)
				require.ElementsMatch(t, galleries, actual)
				urls, err := f.repo.MetadataField.State(ctx, destination.UUID, "urls")
				require.NoError(t, err)
				require.JSONEq(t, `["https://example.com/post/0","https://example.com/post/1"]`, string(urls.Value))
				return nil
			}))
		})
	}
}
