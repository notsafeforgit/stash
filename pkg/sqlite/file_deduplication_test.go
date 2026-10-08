package sqlite_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/dedup"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func removeFileDeduplicationsSchema(t *testing.T, raw *sql.DB) {
	t.Helper()
	removeProviderMetadataSchema(t, raw)
	var exists bool
	require.NoError(t, raw.QueryRow("SELECT EXISTS(SELECT 1 FROM native_migration_history WHERE version=1000095)").Scan(&exists))
	if exists {
		_, err := raw.Exec("DROP TABLE file_deduplications; DELETE FROM native_migration_history WHERE version=1000095")
		require.NoError(t, err)
	}
}

type deduplicationFixture struct {
	fileContentFixture
	duplicate models.File
	input     models.FileDeduplicationInput
	service   *dedup.Service
	image     bool
}

func newDeduplicationFixture(t *testing.T, image bool) deduplicationFixture {
	t.Helper()
	f := newFileContentFixture(t)
	duplicate := f.addFile(t, "duplicate.mp4", []byte("original media"))
	for _, value := range []models.File{f.file, duplicate} {
		require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			if image {
				if err := f.repo.File.Update(ctx, &models.ImageFile{BaseFile: value.Base(), Width: 3, Height: 4, Format: "jpeg"}); err != nil {
					return err
				}
				_, _, err := f.db.ExecSQL(ctx, `INSERT INTO images_files(image_id,file_id,"primary") VALUES(41,?,?)`, []any{value.Base().ID, value == duplicate})
				return err
			}
			if err := f.repo.File.Update(ctx, &models.VideoFile{BaseFile: value.Base(), Width: 3, Height: 4, VideoCodec: "h264"}); err != nil {
				return err
			}
			_, _, err := f.db.ExecSQL(ctx, `INSERT INTO scenes_files(scene_id,file_id,"primary") VALUES(31,?,?)`, []any{value.Base().ID, value == duplicate})
			return err
		}))
	}
	return deduplicationFixture{fileContentFixture: f, duplicate: duplicate, image: image,
		input: models.FileDeduplicationInput{RootUUID: f.root.UUID, KeepPath: "first.mp4", RemovePath: "duplicate.mp4"}, service: dedup.New(f.repo, "")}
}

func (f deduplicationFixture) request(t *testing.T) dedup.Request {
	t.Helper()
	preview, err := f.service.Preview(t.Context(), f.input)
	require.NoError(t, err)
	require.True(t, preview.Eligible, preview.BlockedReason)
	return dedup.Request{FileDeduplicationInput: f.input, RequestUUID: uuid.NewString(), Signature: preview.Signature}
}

func (f deduplicationFixture) requireIntact(t *testing.T) {
	t.Helper()
	require.FileExists(t, f.file.Base().Path)
	require.FileExists(t, f.duplicate.Base().Path)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM file_deduplications"))
	require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM files WHERE id="+f.duplicate.Base().ID.String()))
	require.NoDirExists(t, f.db.FileDeletionJournalPath())
	require.Equal(t, models.ArchiveEntityActive, archiveFind(t, f.repo, models.ArchiveFile, int(f.duplicate.Base().ID)).State)
}

func TestFileDeduplicationKeepsMetadataPrimaryAndSourceHistory(t *testing.T) {
	for _, image := range []bool{false, true} {
		t.Run(map[bool]string{true: "image", false: "scene"}[image], func(t *testing.T) {
			f := newDeduplicationFixture(t, image)
			collection := putSourceCollection(t, f.repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Retained source", Kind: "legacy_catalog", State: "disabled"}})
			observation := recordFileObservation(t, f.repo, models.SourceFileObservation{UUID: uuid.NewString(), CollectionUUID: collection.UUID, CollectionRevision: collection.Revision,
				RootUUID: f.root.UUID, RootRevision: f.root.Revision, RelativePath: f.input.RemovePath, State: "present", Role: "local", Origin: "migration", ObservedAt: time.Now()})
			removedIdentity := archiveFind(t, f.repo, models.ArchiveFile, int(f.duplicate.Base().ID))
			originalMatch := recordFileMatch(t, f.repo, models.SourceFileMatch{UUID: uuid.NewString(), ObservationUUID: observation.UUID, FileUUID: removedIdentity.UUID,
				Generation: f.duplicate.Base().Generation, Basis: "exact-path", LibraryRootPath: f.root.Binding.Path, Origin: "migration"})
			raw := openRawDB(t, f.db.DatabasePath())
			defer raw.Close()
			performers := albumJobRows(t, raw, "performers_scenes")
			request := f.request(t)
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM file_content_versions"), "preview must not create verification evidence")
			result, err := f.service.Apply(t.Context(), request)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, request.Signature, result.Signature)
			require.FileExists(t, f.file.Base().Path)
			require.NoFileExists(t, f.duplicate.Base().Path)
			require.NoDirExists(t, f.db.FileDeletionJournalPath())
			require.EqualValues(t, 1, queryUint(t, raw, "SELECT count(*) FROM file_deduplications"))
			require.EqualValues(t, 2, queryUint(t, raw, "SELECT count(*) FROM file_content_versions"))
			require.Equal(t, performers, albumJobRows(t, raw, "performers_scenes"))
			require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
				matches, err := f.repo.SourceFile.Matches(ctx, observation.UUID, "", 100)
				if err != nil {
					return err
				}
				require.Len(t, matches, 2)
				require.Contains(t, matches, *originalMatch)
				for _, match := range matches {
					if match.UUID != originalMatch.UUID {
						require.Equal(t, result.KeptUUID, match.FileUUID)
						require.Contains(t, string(match.Details), result.UUID)
					}
				}
				if image {
					item, err := f.repo.Image.Find(ctx, 41)
					require.NoError(t, err)
					require.Equal(t, "Kept image", item.Title)
					require.Equal(t, f.file.Base().ID, *item.PrimaryFileID)
				} else {
					item, err := f.repo.Scene.Find(ctx, 31)
					require.NoError(t, err)
					require.Equal(t, "Kept title", item.Title)
					require.Equal(t, f.file.Base().ID, *item.PrimaryFileID)
				}
				resolved, err := f.repo.ArchiveEntity.Resolve(ctx, removedIdentity.UUID)
				require.NoError(t, err)
				require.Equal(t, result.KeptUUID, resolved.UUID)
				return nil
			}))
			_, err = raw.Exec("UPDATE file_deduplications SET signature=printf('%064d',0)")
			require.ErrorContains(t, err, "immutable")
			require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
			require.NoError(t, f.db.Close())
			require.NoError(t, f.db.Open(f.db.DatabasePath()))
			replayed, err := dedup.New(f.db.Repository(), "").Apply(t.Context(), request)
			require.NoError(t, err)
			require.Equal(t, result, replayed)
			request.RemovePath = "another.mp4"
			_, err = f.service.Apply(t.Context(), request)
			require.ErrorIs(t, err, models.ErrFileDeduplicationReplay)
		})
	}
}

func TestFileDeduplicationRejectsChangedBytesAndStaleState(t *testing.T) {
	for _, scenario := range []string{"different_bytes", "same_size_rewrite", "changed_metadata", "changed_owner", "changed_root", "new_source_match"} {
		t.Run(scenario, func(t *testing.T) {
			f := newDeduplicationFixture(t, false)
			if scenario == "different_bytes" {
				require.NoError(t, os.WriteFile(f.duplicate.Base().Path, []byte("different data"), 0600))
				require.NoError(t, os.Chtimes(f.duplicate.Base().Path, f.duplicate.Base().ModTime, f.duplicate.Base().ModTime))
			}
			request := f.request(t)
			switch scenario {
			case "same_size_rewrite":
				info, err := os.Stat(f.duplicate.Base().Path)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(f.duplicate.Base().Path, []byte("different data"), 0600))
				require.NoError(t, os.Chtimes(f.duplicate.Base().Path, info.ModTime(), info.ModTime()))
			case "changed_metadata":
				attachmentSQL(t, f.db, "UPDATE scenes SET title='Later title' WHERE id=31")
			case "changed_owner":
				attachmentSQL(t, f.db, "UPDATE scenes_files SET scene_id=32 WHERE file_id="+f.duplicate.Base().ID.String())
			case "changed_root":
				definition := f.root.MediaRootDefinition
				definition.Label = "Later root review"
				putMediaRoot(t, f.repo, models.MediaRootInput{UUID: f.root.UUID, ExpectedRevision: f.root.Revision, Origin: "review", MediaRootDefinition: definition})
			case "new_source_match":
				collection := putSourceCollection(t, f.repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Source", Kind: "legacy_catalog", State: "disabled"}})
				observation := recordFileObservation(t, f.repo, models.SourceFileObservation{UUID: uuid.NewString(), CollectionUUID: collection.UUID, CollectionRevision: collection.Revision, RootUUID: f.root.UUID, RootRevision: f.root.Revision, RelativePath: f.input.RemovePath, State: "present", Role: "local", Origin: "review", ObservedAt: time.Now()})
				recordFileMatch(t, f.repo, models.SourceFileMatch{UUID: uuid.NewString(), ObservationUUID: observation.UUID, FileUUID: archiveFind(t, f.repo, models.ArchiveFile, int(f.duplicate.Base().ID)).UUID, Generation: f.duplicate.Base().Generation, Basis: "review", Origin: "review"})
			}
			_, err := f.service.Apply(t.Context(), request)
			if scenario == "different_bytes" {
				require.ErrorIs(t, err, archive.ErrMediaFileDigest)
			} else {
				require.ErrorIs(t, err, models.ErrFileDeduplicationConflict)
			}
			f.requireIntact(t)
		})
	}
}

type failDeduplicationDestroy struct {
	models.FileReaderWriter
	after func(context.Context) error
}

func (f failDeduplicationDestroy) Destroy(ctx context.Context, id models.FileID) error {
	if err := f.FileReaderWriter.Destroy(ctx, id); err != nil {
		return err
	}
	return f.after(ctx)
}

func TestFileDeduplicationRollsBackStagedFilesAndReferences(t *testing.T) {
	for _, scenario := range []string{"late_failure", "keeper_changes", "keeper_generation_changes"} {
		t.Run(scenario, func(t *testing.T) {
			f := newDeduplicationFixture(t, false)
			request := f.request(t)
			repo := f.repo
			repo.File = failDeduplicationDestroy{FileReaderWriter: repo.File, after: func(ctx context.Context) error {
				if scenario == "keeper_generation_changes" {
					_, err := repo.FileContent.Advance(ctx, f.identity.UUID, f.file.Base().Generation)
					return err
				}
				if scenario == "keeper_changes" {
					return os.WriteFile(f.file.Base().Path, []byte("different data"), 0600)
				}
				return errors.New("injected failure after staging")
			}}
			_, err := dedup.New(repo, "").Apply(t.Context(), request)
			switch scenario {
			case "keeper_changes":
				require.ErrorIs(t, err, archive.ErrMediaFileChanged)
			case "keeper_generation_changes":
				require.ErrorIs(t, err, models.ErrFileGenerationConflict)
			default:
				require.ErrorContains(t, err, "injected failure after staging")
			}
			f.requireIntact(t)
			body, err := os.ReadFile(f.duplicate.Base().Path)
			require.NoError(t, err)
			require.Equal(t, "original media", string(body))
		})
	}
}

func TestFileDeduplicationDoesNotMergeDistinctOrAmbiguousMedia(t *testing.T) {
	for _, scenario := range []string{"distinct", "ambiguous", "unindexed", "captions", "hardlink"} {
		t.Run(scenario, func(t *testing.T) {
			f := newDeduplicationFixture(t, false)
			var reason string
			switch scenario {
			case "distinct":
				attachmentSQL(t, f.db, "UPDATE scenes_files SET scene_id=32 WHERE file_id="+f.duplicate.Base().ID.String())
				reason = "different_media_owners"
			case "ambiguous":
				attachmentSQL(t, f.db, `INSERT INTO scenes_files(scene_id,file_id,"primary") VALUES(32,`+f.duplicate.Base().ID.String()+`,1)`)
				reason = "ambiguous_or_missing_media_owner"
			case "unindexed":
				f.input.RemovePath = "unindexed.mp4"
				reason = "file_requires_intake"
			case "captions":
				require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
					return f.repo.File.UpdateCaptions(ctx, f.duplicate.Base().ID, []*models.VideoCaption{{LanguageCode: "en", CaptionType: "srt"}})
				}))
				reason = "duplicate_has_captions"
			case "hardlink":
				require.NoError(t, os.Remove(f.duplicate.Base().Path))
				require.NoError(t, os.Link(f.file.Base().Path, f.duplicate.Base().Path))
				reason = "same_filesystem_entry"
			}
			preview, err := f.service.Preview(t.Context(), f.input)
			require.NoError(t, err)
			require.False(t, preview.Eligible)
			require.Equal(t, reason, preview.BlockedReason)
			_, err = f.service.Apply(t.Context(), dedup.Request{FileDeduplicationInput: f.input, RequestUUID: uuid.NewString(), Signature: preview.Signature})
			require.ErrorIs(t, err, models.ErrFileDeduplicationConflict)
			f.requireIntact(t)
		})
	}
}

type deduplicationCrashTxn struct {
	*sqlite.Database
	commits int
}

func (m *deduplicationCrashTxn) Commit(ctx context.Context) error {
	if err := m.Database.Commit(ctx); err != nil {
		return err
	}
	m.commits++
	if m.commits == 2 {
		os.Exit(deletionCrashExit)
	}
	return nil
}

func TestFileDeduplicationCrashHelper(t *testing.T) {
	phase := os.Getenv("STASH_TEST_DEDUP_CRASH")
	if phase == "" {
		return
	}
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(os.Getenv("STASH_TEST_DEDUP_DATABASE")))
	var request dedup.Request
	require.NoError(t, json.Unmarshal([]byte(os.Getenv("STASH_TEST_DEDUP_REQUEST")), &request))
	repo := db.Repository()
	if phase == "before_commit" {
		repo.File = failDeduplicationDestroy{FileReaderWriter: repo.File, after: func(context.Context) error { os.Exit(deletionCrashExit); return nil }}
	} else {
		repo.TxnManager = &deduplicationCrashTxn{Database: db}
	}
	_, err := dedup.New(repo, "").Apply(t.Context(), request)
	require.NoError(t, err)
	t.Fatal("crash helper did not exit")
}

func TestFileDeduplicationRecoversCommitOutcomeAfterProcessDeath(t *testing.T) {
	for _, phase := range []string{"before_commit", "after_commit"} {
		t.Run(phase, func(t *testing.T) {
			f := newDeduplicationFixture(t, false)
			request := f.request(t)
			encoded, err := json.Marshal(request)
			require.NoError(t, err)
			require.NoError(t, f.db.Close())
			command := exec.Command(os.Args[0], "-test.run=^TestFileDeduplicationCrashHelper$", "-test.v")
			command.Env = append(os.Environ(), "STASH_TEST_DEDUP_CRASH="+phase, "STASH_TEST_DEDUP_DATABASE="+f.db.DatabasePath(), "STASH_TEST_DEDUP_REQUEST="+string(encoded))
			output, err := command.CombinedOutput()
			var exit *exec.ExitError
			require.ErrorAs(t, err, &exit, string(output))
			require.Equal(t, deletionCrashExit, exit.ExitCode(), string(output))
			require.NoError(t, f.db.Open(f.db.DatabasePath()))
			if phase == "before_commit" {
				f.requireIntact(t)
			} else {
				replayed, err := dedup.New(f.db.Repository(), "").Apply(t.Context(), request)
				require.NoError(t, err)
				require.Equal(t, request.Signature, replayed.Signature)
				require.NoFileExists(t, f.duplicate.Base().Path)
			}
			require.FileExists(t, f.file.Base().Path)
			require.NoDirExists(t, f.db.FileDeletionJournalPath())
		})
	}
}

func TestFileDeduplicationMigrationPreservesExistingEvidence(t *testing.T) {
	f := newDeduplicationFixture(t, false)
	f.record(t, f.inputForKept(t))
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	before := albumJobRows(t, raw, "file_content_versions")
	removeFileDeduplicationsSchema(t, raw)
	_, err := raw.Exec("UPDATE schema_migrations SET version=1000094,dirty=0")
	require.NoError(t, err)
	var needed *sqlite.MigrationNeededError
	require.ErrorAs(t, f.db.Open(f.db.DatabasePath()), &needed)
	require.NoError(t, f.db.RunAllMigrations())
	require.Equal(t, before, albumJobRows(t, raw, "file_content_versions"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM file_deduplications"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func (f deduplicationFixture) inputForKept(t *testing.T) models.FileContentInput {
	return f.fileContentFixture.input(t, f.file)
}

func TestFileDeduplicationRejectsInvalidPaths(t *testing.T) {
	f := newDeduplicationFixture(t, false)
	for _, path := range []string{"../escape.mp4", "/absolute.mp4", "pending.MP4.part", strings.Repeat("x", 4097)} {
		input := f.input
		input.RemovePath = path
		_, err := f.service.Preview(t.Context(), input)
		require.ErrorIs(t, err, models.ErrFileDeduplicationInvalid)
	}
	require.FileExists(t, filepath.Join(f.root.Binding.Path, f.input.RemovePath))
}

func TestFileDeduplicationStartupRejectsChangedReceiptWithoutWrites(t *testing.T) {
	f := newDeduplicationFixture(t, false)
	_, err := f.service.Apply(t.Context(), f.request(t))
	require.NoError(t, err)
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, f.db.DatabasePath())
	var guard string
	require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='file_deduplication_immutable'").Scan(&guard))
	_, err = raw.Exec("DROP TRIGGER file_deduplication_immutable; UPDATE file_deduplications SET signature=printf('%064d',0);" + guard)
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	before, err := os.ReadFile(f.db.DatabasePath())
	require.NoError(t, err)
	require.ErrorIs(t, f.db.Open(f.db.DatabasePath()), models.ErrSourcePayloadCorrupt)
	after, err := os.ReadFile(f.db.DatabasePath())
	require.NoError(t, err)
	require.Equal(t, sha256.Sum256(before), sha256.Sum256(after))
}

func TestFileDeduplicationMigrationRejectsForeignTableWithoutWrites(t *testing.T) {
	f := newDeduplicationFixture(t, false)
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, f.db.DatabasePath())
	removeFileDeduplicationsSchema(t, raw)
	_, err := raw.Exec("UPDATE schema_migrations SET version=1000094,dirty=0; CREATE TABLE file_deduplications(private_data TEXT); INSERT INTO file_deduplications VALUES('retained')")
	require.NoError(t, err)
	require.NoError(t, raw.Close())
	before, err := os.ReadFile(f.db.DatabasePath())
	require.NoError(t, err)
	var needed *sqlite.MigrationNeededError
	require.ErrorAs(t, f.db.Open(f.db.DatabasePath()), &needed)
	require.ErrorContains(t, f.db.RunAllMigrations(), "destination file_deduplications already exists")
	after, err := os.ReadFile(f.db.DatabasePath())
	require.NoError(t, err)
	require.Equal(t, sha256.Sum256(before), sha256.Sum256(after))
	raw = openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.EqualValues(t, 1000094, queryUint(t, raw, "SELECT version FROM schema_migrations"))
	require.Zero(t, queryUint(t, raw, "SELECT dirty FROM schema_migrations"))
}

func TestFileDeduplicationAnonymisationRemovesPrivateReceipt(t *testing.T) {
	f := newDeduplicationFixture(t, false)
	result, err := f.service.Apply(t.Context(), f.request(t))
	require.NoError(t, err)
	destination := filepath.Join(t.TempDir(), "anonymous.sqlite")
	anonymiser, err := sqlite.NewAnonymiser(f.db, destination)
	require.NoError(t, err)
	require.NoError(t, anonymiser.Anonymise(t.Context()))
	body, err := os.ReadFile(destination)
	require.NoError(t, err)
	require.NotContains(t, string(body), result.UUID)
	require.NotContains(t, string(body), f.root.Binding.Path)
	raw := openRawDB(t, destination)
	defer raw.Close()
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM file_deduplications"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestFileDeduplicationReceiptSurvivesAdoptionAndLaterDeletion(t *testing.T) {
	f := newDeduplicationFixture(t, false)
	request := f.request(t)
	result, err := f.service.Apply(t.Context(), request)
	require.NoError(t, err)
	kept, media := uuid.NewString(), uuid.NewString()
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		for _, pair := range [][2]string{{result.KeptUUID, kept}, {result.MediaUUID, media}} {
			entity, err := f.repo.ArchiveEntity.Find(ctx, pair[0])
			if err != nil {
				return err
			}
			if _, err := f.repo.ArchiveEntity.AdoptUUID(ctx, pair[0], pair[1], entity.Revision); err != nil {
				return err
			}
		}
		return nil
	}))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error { return f.repo.File.Destroy(ctx, f.file.Base().ID) }))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	replayed, err := f.service.Apply(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, result.Signature, replayed.Signature)
	require.Equal(t, kept, replayed.KeptUUID)
	require.Equal(t, media, replayed.MediaUUID)
	require.Equal(t, result.CommittedAt, replayed.CommittedAt)
}
