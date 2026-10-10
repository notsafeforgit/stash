package sqlite_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

type sourceFileFixture struct {
	db          *sqlite.Database
	repo        models.Repository
	root        *models.MediaRoot
	collection  *models.SourceCollection
	claim       models.SourceContentClaim
	observation models.SourceFileObservation
}

func newSourceFileFixture(t *testing.T) sourceFileFixture {
	t.Helper()
	db, repo := archiveTestDatabase(t)
	root := putMediaRoot(t, repo, models.MediaRootInput{Origin: "migration", MediaRootDefinition: models.MediaRootDefinition{Label: "Offline historical root", State: "disabled"}})
	collection := putSourceCollection(t, repo, models.SourceCollectionInput{Origin: "migration", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Historical catalog", Kind: "legacy_catalog", State: "disabled"}})
	size := int64(1000)
	stamp := time.Date(2026, 9, 29, 1, 2, 3, 123456789, time.FixedZone("captured", -7*3600))
	claim := models.SourceContentClaim{UUID: uuid.NewString(), CollectionUUID: collection.UUID, CollectionRevision: collection.Revision,
		ReferenceNamespace: "legacy:catalog:fixture", ReferenceValue: "path:not-a-checksum", Size: &size, SourceCreatedAt: "2025-01-02T03:04:05-07:00",
		Origin: "migration", ObservedAt: stamp, Details: json.RawMessage(`{"source_id":98765432109876543210987}`)}
	observation := models.SourceFileObservation{UUID: uuid.NewString(), ContentClaimUUID: &claim.UUID, CollectionUUID: collection.UUID, CollectionRevision: collection.Revision,
		RootUUID: root.UUID, RootRevision: root.Revision, RelativePath: "video.mp4", State: "present", Role: "local", Size: &size,
		SourceFirstObserved: "2025-01-02T03:04:06-07:00", Origin: "migration", ObservedAt: stamp}
	return sourceFileFixture{db, repo, root, collection, claim, observation}
}

func recordContentClaim(t *testing.T, repo models.Repository, input models.SourceContentClaim) *models.SourceContentClaim {
	t.Helper()
	var ret *models.SourceContentClaim
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceFile.RecordContentClaim(ctx, input)
		return err
	}))
	return ret
}

func recordFileObservation(t *testing.T, repo models.Repository, input models.SourceFileObservation) *models.SourceFileObservation {
	t.Helper()
	var ret *models.SourceFileObservation
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceFile.RecordObservation(ctx, input)
		return err
	}))
	return ret
}

func recordFileMatch(t *testing.T, repo models.Repository, input models.SourceFileMatch) *models.SourceFileMatch {
	t.Helper()
	var ret *models.SourceFileMatch
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		ret, err = repo.SourceFile.RecordMatch(ctx, input)
		return err
	}))
	return ret
}

func TestSourceFileObservationsPreserveSharedClaimsStatesAndOfflineLocations(t *testing.T) {
	f := newSourceFileFixture(t)
	claim := recordContentClaim(t, f.repo, f.claim)
	require.Equal(t, claim, recordContentClaim(t, f.repo, f.claim))
	require.JSONEq(t, string(f.claim.Details), string(claim.Details))
	require.True(t, claim.ObservedAt.Equal(f.claim.ObservedAt))
	for _, state := range []string{"present", "missing", "pending", "deduplicated"} {
		observation := f.observation
		observation.UUID, observation.State = uuid.NewString(), state
		observation.RelativePath = state + ".mp4"
		if state == "deduplicated" {
			survivor := "present.mp4"
			observation.SurvivorPath = &survivor
		}
		stored := recordFileObservation(t, f.repo, observation)
		require.Equal(t, stored, recordFileObservation(t, f.repo, observation))
		require.Equal(t, state, stored.State)
		require.Equal(t, claim.UUID, *stored.ContentClaimUUID)
	}
	// No source asset or filesystem location creates a playable file or hash.
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM source_content_claims"))
	require.Equal(t, uint(4), queryUint(t, raw, "SELECT count(*) FROM source_file_observations"))
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM files"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM media_contents"))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM source_file_matches"))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	f.repo = f.db.Repository()
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		page, err := f.repo.SourceFile.ClaimObservations(ctx, claim.UUID, "", 2)
		require.NoError(t, err)
		require.Len(t, page, 2)
		next, err := f.repo.SourceFile.ClaimObservations(ctx, claim.UUID, page[1].UUID, 2)
		require.NoError(t, err)
		require.Len(t, next, 2)
		page, err = f.repo.SourceFile.LocationObservations(ctx, f.root.UUID, nil, "missing.mp4", "", 100)
		require.NoError(t, err)
		require.Len(t, page, 1)
		require.Equal(t, "missing", page[0].State)
		_, err = f.repo.SourceFile.LocationObservations(ctx, f.root.UUID, nil, "../outside", "", 100)
		require.Error(t, err)
		_, err = f.repo.SourceFile.ClaimObservations(ctx, claim.UUID, "", 101)
		require.Error(t, err)
		return nil
	}))
	require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM pragma_foreign_key_check"))
}

func TestSourceFileObservationsShareClaimAcrossCollectionRevisions(t *testing.T) {
	f := newSourceFileFixture(t)
	claim := recordContentClaim(t, f.repo, f.claim)
	recordFileObservation(t, f.repo, f.observation)
	definition := f.collection.SourceCollectionDefinition
	definition.Label = "Renamed historical catalog"
	revised := putSourceCollection(t, f.repo, models.SourceCollectionInput{
		UUID: f.collection.UUID, ExpectedRevision: f.collection.Revision,
		SourceCollectionDefinition: definition, Origin: "review",
	})
	next := f.observation
	next.UUID, next.CollectionRevision = uuid.NewString(), revised.Revision
	next.RelativePath = "another.mp4"
	stored := recordFileObservation(t, f.repo, next)
	require.Equal(t, claim.UUID, *stored.ContentClaimUUID)
	require.NotEqual(t, claim.CollectionRevision, stored.CollectionRevision)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

func TestSourceFileEvidenceRejectsChangedReplayAndForeignScope(t *testing.T) {
	f := newSourceFileFixture(t)
	recordContentClaim(t, f.repo, f.claim)
	recordFileObservation(t, f.repo, f.observation)
	claim := f.claim
	claim.ReferenceValue = "different"
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.SourceFile.RecordContentClaim(ctx, claim)
		return err
	})
	require.ErrorIs(t, err, models.ErrSourceFileEvidenceReplay)
	observation := f.observation
	observation.State = "missing"
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.SourceFile.RecordObservation(ctx, observation)
		return err
	})
	require.ErrorIs(t, err, models.ErrSourceFileEvidenceReplay)
	foreign := putSourceCollection(t, f.repo, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Other", Kind: "manual_batch", State: "disabled"}})
	observation.UUID, observation.CollectionUUID = uuid.NewString(), foreign.UUID
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.SourceFile.RecordObservation(ctx, observation)
		return err
	})
	require.ErrorContains(t, err, "FOREIGN KEY")
	for _, path := range []string{"../escape", "/absolute", "a/../b", "a//b", "a\\b", "a\x00b"} {
		observation = f.observation
		observation.UUID, observation.RelativePath = uuid.NewString(), path
		err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := f.repo.SourceFile.RecordObservation(ctx, observation)
			return err
		})
		require.ErrorIs(t, err, models.ErrSourceFileEvidenceInvalid)
	}
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err = raw.Exec("UPDATE source_file_observations SET state='missing'")
	require.ErrorContains(t, err, "immutable")
	_, err = raw.Exec("UPDATE source_content_claims SET size=size+1")
	require.ErrorContains(t, err, "immutable")
	require.Equal(t, uint(1), queryUint(t, raw, "SELECT count(*) FROM source_file_observations"))
}

func TestSourceFileMatchesCheckLiteralPathsGenerationsAndSurvivors(t *testing.T) {
	f := newSourceFileFixture(t)
	recordContentClaim(t, f.repo, f.claim)
	observation := recordFileObservation(t, f.repo, f.observation)
	file := archiveFind(t, f.repo, models.ArchiveFile, 21)
	input := models.SourceFileMatch{UUID: uuid.NewString(), ObservationUUID: observation.UUID, FileUUID: file.UUID, Generation: 1,
		LibraryRootPath: "/identity-fixture", Basis: "exact-path", Origin: "migration"}
	match := recordFileMatch(t, f.repo, input)
	require.Equal(t, match, recordFileMatch(t, f.repo, input))
	var storedFiles []models.File
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		storedFiles, err = f.repo.File.Find(ctx, 21)
		return err
	}))
	fractional := f.observation
	reportedTime := storedFiles[0].Base().ModTime.UnixNano() + 123456789
	fractional.UUID, fractional.ModifiedAtNS = uuid.NewString(), &reportedTime
	recordFileObservation(t, f.repo, fractional)
	precisionMatch := input
	precisionMatch.UUID, precisionMatch.ObservationUUID = uuid.NewString(), fractional.UUID
	recordFileMatch(t, f.repo, precisionMatch)
	apply := func(value models.SourceFileMatch) error {
		return f.repo.WithTxn(t.Context(), func(ctx context.Context) error { _, err := f.repo.SourceFile.RecordMatch(ctx, value); return err })
	}
	changed := input
	changed.LibraryRootPath = "/another-root"
	require.ErrorIs(t, apply(changed), models.ErrSourceFileEvidenceReplay)
	changed.UUID = uuid.NewString()
	require.ErrorIs(t, apply(changed), models.ErrSourceFileEvidenceInvalid)
	changed = input
	changed.UUID, changed.Generation = uuid.NewString(), 2
	require.ErrorIs(t, apply(changed), models.ErrFileGenerationConflict)
	wild := f.observation
	wild.UUID, wild.RelativePath = uuid.NewString(), "*.mp4"
	recordFileObservation(t, f.repo, wild)
	changed = input
	changed.UUID, changed.ObservationUUID = uuid.NewString(), wild.UUID
	require.ErrorIs(t, apply(changed), models.ErrSourceFileEvidenceInvalid, "a literal filename cannot become a wildcard lookup")
	changedTime := f.observation
	modifiedAt := int64(1)
	changedTime.UUID, changedTime.ModifiedAtNS = uuid.NewString(), &modifiedAt
	recordFileObservation(t, f.repo, changedTime)
	changed.UUID, changed.ObservationUUID = uuid.NewString(), changedTime.UUID
	require.ErrorIs(t, apply(changed), models.ErrSourceFileEvidenceInvalid, "path and size agreement must not conceal a reported modification-time conflict")
	survivor := "video.mp4"
	old := f.observation
	old.UUID, old.RelativePath, old.State, old.Role = uuid.NewString(), "old.gif", "missing", "converted-source"
	old.Size, old.SurvivorPath = nil, &survivor
	recordFileObservation(t, f.repo, old)
	converted := input
	converted.UUID, converted.ObservationUUID, converted.Basis = uuid.NewString(), old.UUID, "survivor-path"
	recordFileMatch(t, f.repo, converted)
	require.Equal(t, "missing", recordFileObservation(t, f.repo, old).State)
	attachmentSQL(t, f.db, "UPDATE files SET size=size+1 WHERE id=21")
	require.Equal(t, match, recordFileMatch(t, f.repo, input), "historical replay does not require the old generation to be active")
	changed = input
	changed.UUID = uuid.NewString()
	require.ErrorIs(t, apply(changed), models.ErrFileGenerationConflict)
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	_, err := raw.Exec(`INSERT INTO source_file_matches(uuid,observation_uuid,file_uuid,generation,library_root_path,basis,origin,details) VALUES(?,?,?,1,'/identity-fixture','exact-path','migration','{}')`, uuid.NewString(), observation.UUID, file.UUID)
	require.ErrorContains(t, err, "active file generation")
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		matches, err := f.repo.SourceFile.Matches(ctx, observation.UUID, "", 1)
		require.NoError(t, err)
		require.Equal(t, []models.SourceFileMatch{*match}, matches)
		matches, err = f.repo.SourceFile.Matches(ctx, observation.UUID, match.UUID, 1)
		require.NoError(t, err)
		require.Empty(t, matches)
		return nil
	}))
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

func TestSourceFileMatchesPreserveAdoptionAndDeletion(t *testing.T) {
	f := newSourceFileFixture(t)
	recordContentClaim(t, f.repo, f.claim)
	recordFileObservation(t, f.repo, f.observation)
	file := archiveFind(t, f.repo, models.ArchiveFile, 21)
	input := models.SourceFileMatch{UUID: uuid.NewString(), ObservationUUID: f.observation.UUID, FileUUID: file.UUID, Generation: 1, Basis: "review", Origin: "review"}
	recordFileMatch(t, f.repo, input)
	adopted := uuid.NewString()
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := f.repo.ArchiveEntity.AdoptUUID(ctx, file.UUID, adopted, file.Revision)
		return err
	}))
	require.Equal(t, adopted, recordFileMatch(t, f.repo, input).FileUUID)
	newMatch := input
	newMatch.UUID = uuid.NewString()
	require.Equal(t, adopted, recordFileMatch(t, f.repo, newMatch).FileUUID, "an old UUID alias can identify a new match")
	require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error { return f.repo.File.Destroy(ctx, 21) }))
	require.Equal(t, adopted, recordFileMatch(t, f.repo, input).FileUUID)
	newMatch.UUID = uuid.NewString()
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error { _, err := f.repo.SourceFile.RecordMatch(ctx, newMatch); return err })
	require.ErrorIs(t, err, models.ErrFileGenerationConflict)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

func TestSourceFileMatchRequiresBothZipArchiveAndMemberIdentity(t *testing.T) {
	f := newSourceFileFixture(t)
	attachmentSQL(t, f.db, `INSERT INTO files(id,parent_folder_id,basename,size,mod_time,created_at,updated_at) VALUES(22,1,'album.zip',9999,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
 INSERT INTO folders(id,path,basename,zip_file_id,mod_time,created_at,updated_at) VALUES(2,'/identity-fixture/album.zip','album.zip',22,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);
 INSERT INTO files(id,parent_folder_id,basename,zip_file_id,size,mod_time,created_at,updated_at) VALUES(23,2,'member.jpg',22,1000,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP);`)
	recordContentClaim(t, f.repo, f.claim)
	zipPath := "album.zip"
	f.observation.ArchivePath, f.observation.RelativePath = &zipPath, "member.jpg"
	recordFileObservation(t, f.repo, f.observation)
	member := archiveFind(t, f.repo, models.ArchiveFile, 23)
	zipFile := archiveFind(t, f.repo, models.ArchiveFile, 22)
	input := models.SourceFileMatch{UUID: uuid.NewString(), ObservationUUID: f.observation.UUID, FileUUID: member.UUID, Generation: 1,
		LibraryRootPath: "/identity-fixture", Basis: "exact-path", Origin: "migration"}
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error { _, err := f.repo.SourceFile.RecordMatch(ctx, input); return err })
	require.ErrorIs(t, err, models.ErrSourceFileEvidenceInvalid)
	generation := int64(1)
	input.ArchiveFileUUID, input.ArchiveGeneration = &zipFile.UUID, &generation
	stored := recordFileMatch(t, f.repo, input)
	attachmentSQL(t, f.db, "UPDATE files SET size=size+1 WHERE id=22")
	require.Equal(t, stored, recordFileMatch(t, f.repo, input))
	input.UUID = uuid.NewString()
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error { _, err := f.repo.SourceFile.RecordMatch(ctx, input); return err })
	require.ErrorIs(t, err, models.ErrFileGenerationConflict, "unchanged member generation cannot conceal changed archive bytes")
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
}

func TestSourceFileDeclaredDigestCannotInventVerifiedContent(t *testing.T) {
	f := newFileContentFixture(t)
	collection := putSourceCollection(t, f.repo, models.SourceCollectionInput{Origin: "migration", SourceCollectionDefinition: models.SourceCollectionDefinition{Label: "Content claims", Kind: "legacy_catalog", State: "disabled"}})
	verified := f.input(t, f.file)
	algorithm, digest := "sha256", verified.SHA256
	claim := models.SourceContentClaim{UUID: uuid.NewString(), CollectionUUID: collection.UUID, CollectionRevision: collection.Revision,
		ReferenceNamespace: "fixture:asset", ReferenceValue: "asset", DigestAlgorithm: &algorithm, Digest: &digest, Origin: "migration", ObservedAt: time.Now()}
	recordContentClaim(t, f.repo, claim)
	observation := models.SourceFileObservation{UUID: uuid.NewString(), ContentClaimUUID: &claim.UUID, CollectionUUID: collection.UUID, CollectionRevision: collection.Revision,
		RootUUID: f.root.UUID, RootRevision: f.root.Revision, RelativePath: "old-name.mp4", State: "missing", Role: "local", Origin: "migration", ObservedAt: time.Now()}
	recordFileObservation(t, f.repo, observation)
	input := models.SourceFileMatch{UUID: uuid.NewString(), ObservationUUID: observation.UUID, FileUUID: f.identity.UUID, Generation: f.file.Base().Generation, Basis: "verified-content", Origin: "migration"}
	err := f.repo.WithTxn(t.Context(), func(ctx context.Context) error { _, err := f.repo.SourceFile.RecordMatch(ctx, input); return err })
	require.ErrorIs(t, err, models.ErrFileContentConflict)
	f.record(t, verified)
	recordFileMatch(t, f.repo, input)
	claim.UUID, observation.UUID, input.UUID = uuid.NewString(), uuid.NewString(), uuid.NewString()
	digest = strings.Repeat("0", 64)
	claim.Digest = &digest
	recordContentClaim(t, f.repo, claim)
	observation.ContentClaimUUID, input.ObservationUUID = &claim.UUID, observation.UUID
	recordFileObservation(t, f.repo, observation)
	err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error { _, err := f.repo.SourceFile.RecordMatch(ctx, input); return err })
	require.ErrorIs(t, err, models.ErrFileContentConflict)
}

func TestSourcePostFileEvidenceRetainsUnavailableMediaAndRetirement(t *testing.T) {
	f := newSourceFileFixture(t)
	f.observation.State, f.observation.RelativePath = "pending", "unfinished.mp4"
	recordContentClaim(t, f.repo, f.claim)
	recordFileObservation(t, f.repo, f.observation)
	post := sourceTestPost(t, f.repo, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "post-with-missing-files"}, "")
	input := models.SourcePostFileEvidence{SourcePostEvidence: postLinkEvidence(post.UUID), ObservationUUID: f.observation.UUID}
	apply := func(value models.SourcePostFileEvidence) error {
		return f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
			_, err := f.repo.SourceFile.RecordPostEvidence(ctx, value)
			return err
		})
	}
	require.NoError(t, apply(input))
	require.NoError(t, apply(input))
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	require.Equal(t, uint(post.Revision+1), queryUint(t, raw, "SELECT revision FROM source_posts"))
	for _, table := range []string{"source_captures", "source_attachments", "source_media_evidence", "galleries"} {
		require.Zero(t, queryUint(t, raw, "SELECT count(*) FROM "+table))
	}
	_, err := raw.Exec("UPDATE source_posts SET state='forgotten'")
	require.NoError(t, err)
	require.NoError(t, apply(input))
	input.UUID = uuid.NewString()
	require.ErrorIs(t, apply(input), models.ErrSourcePostForgotten)
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(f.db.DatabasePath()))
	f.repo = f.db.Repository()
	require.NoError(t, f.repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		page, err := f.repo.SourceFile.PostEvidence(ctx, post.UUID, "", 1)
		require.NoError(t, err)
		require.Len(t, page, 1)
		require.Equal(t, f.observation.UUID, page[0].ObservationUUID)
		page, err = f.repo.SourceFile.PostEvidence(ctx, post.UUID, page[0].UUID, 1)
		require.NoError(t, err)
		require.Empty(t, page)
		return nil
	}))
}

func TestSourceFileAuditRejectsInvalidLocationsBeforeWriting(t *testing.T) {
	f := newSourceFileFixture(t)
	recordContentClaim(t, f.repo, f.claim)
	recordFileObservation(t, f.repo, f.observation)
	require.NoError(t, f.db.Close())
	raw := openRawDB(t, f.db.DatabasePath())
	defer raw.Close()
	var guard string
	require.NoError(t, raw.QueryRow("SELECT sql FROM sqlite_schema WHERE name='source_file_observation_immutable'").Scan(&guard))
	_, err := raw.Exec("DROP TRIGGER source_file_observation_immutable; UPDATE source_file_observations SET relative_path='../escape'")
	require.NoError(t, err)
	_, err = raw.Exec(guard)
	require.NoError(t, err)
	before, err := os.ReadFile(f.db.DatabasePath())
	require.NoError(t, err)
	require.ErrorContains(t, f.db.AuditForTesting(f.db.DatabasePath()), "invalid source file observation paths")
	after, err := os.ReadFile(f.db.DatabasePath())
	require.NoError(t, err)
	require.Equal(t, before, after)
}
