package sqlite_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestSavedTwitterDatesFillBlanksRespectClearsAndRetireFailedWork(t *testing.T) {
	for _, scenario := range []string{"blank", "clear", "existing", "missing_file", "automatic"} {
		t.Run(scenario, func(t *testing.T) {
			f := newEnrichmentExecutionFixtureForPost(t, models.SourcePostIdentifier{Namespace: "legacy:catalog:fixture", Value: "saved-tweet"},
				"https://x.com/i/web/status/1555857150810865666", models.SourceCollectionDefinition{
					Label: "Saved tweets", Kind: "feed", Namespace: "native:twitter", State: "active", TargetURL: "https://x.com/example"})
			input := contentTestCapture(t, f.target.PostUUID, 0, "Saved caption")
			input.Origin, input.Platform = "legacy-nfo", "twitter"
			capture := recordSourceTestCapture(t, f.repo, input)
			job := f.admit(t)
			running := f.claim(t, job.UUID, 0)
			if scenario != "automatic" {
				_, err := f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), "not_found")
				require.NoError(t, err)
				require.Equal(t, "pending", readEnrichmentTarget(t, f.repo, f.target.UUID).State)
			}
			binding, err := archive.ProbeMediaRoot(t.TempDir())
			require.NoError(t, err)
			root := putMediaRoot(t, f.repo, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Saved media", State: "active", Binding: binding}})
			files := fileContentFixture{db: f.db, repo: f.repo, root: root}
			file := files.addFile(t, "saved.jpg", []byte("retained media"))
			require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				return f.repo.File.Update(ctx, &models.ImageFile{BaseFile: file.Base(), Width: 3, Height: 4, Format: "jpeg"})
			}))
			attachmentSQL(t, f.db, "INSERT INTO images_files(image_id,file_id,\"primary\") VALUES(41,"+file.Base().ID.String()+",1)")
			media := archiveFind(t, f.repo, models.ArchiveImage, 41)
			require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				post, err := f.repo.SourceEvidence.FindPost(ctx, f.target.PostUUID)
				if err != nil {
					return err
				}
				_, err = f.repo.SourcePostMedia.Decide(ctx, models.SourcePostMediaInput{UUID: uuid.NewString(), PostUUID: post.UUID,
					MediaUUID: media.UUID, ExpectedPostRevision: post.Revision, ExpectedMediaRevision: media.Revision, State: "linked", Origin: "review"})
				return err
			}))
			state := metadataState(t, f.repo, media.UUID, "date")
			if scenario == "clear" || scenario == "existing" {
				mode, value := "clear", ""
				if scenario == "existing" {
					mode, value = "set", `"2022-08-07"`
				}
				_, err := applyMetadata(f.repo, metadataInput(state, mode, "review", value), false)
				require.NoError(t, err)
				state = metadataState(t, f.repo, media.UUID, "date")
			}
			if scenario == "missing_file" {
				require.NoError(t, os.Remove(file.Base().Path))
			}
			var result *ingest.SavedEnrichmentResolution
			if scenario == "automatic" {
				_, err = f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), "not_found")
			} else {
				err = f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
					result, err = f.worker.AcceptSaved(ctx, f.target.UUID, f.target.Revision)
					return err
				})
			}
			if scenario == "missing_file" {
				require.ErrorIs(t, err, models.ErrEnrichmentConflict)
				require.Equal(t, "pending", readEnrichmentTarget(t, f.repo, f.target.UUID).State)
				require.Equal(t, state.Value, metadataState(t, f.repo, media.UUID, "date").Value)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "excluded", readEnrichmentTarget(t, f.repo, f.target.UUID).State)
			after := metadataState(t, f.repo, media.UUID, "date")
			if scenario == "clear" || scenario == "existing" {
				require.Equal(t, state.Value, after.Value)
				require.Equal(t, state.Decision, after.Decision)
				require.Empty(t, result.DatesFilled)
			} else {
				require.JSONEq(t, `"2022-08-06"`, string(after.Value))
				require.Equal(t, "inherit", after.Mode)
				require.Contains(t, after.Decision.Reason, "X Snowflake ID")
			}
			require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				again, err := f.worker.AcceptSaved(ctx, f.target.UUID, f.target.Revision)
				require.NoError(t, err)
				require.Empty(t, again.DatesFilled)
				stored, err := f.repo.SourceEvidence.FindCapture(ctx, capture.UUID)
				require.NoError(t, err)
				require.Equal(t, capture.Metadata, stored.Metadata)
				return nil
			}))
			require.Equal(t, after.Decision, metadataState(t, f.repo, media.UUID, "date").Decision)
		})
	}
}

func TestUnavailableEnrichmentRetiresOnlyCompleteSavedPosts(t *testing.T) {
	for _, scenario := range []string{"access_denied", "not_found", "authentication", "missing_date", "missing_file", "unlinked", "checkpoint", "reviewed_incomplete", "reviewed_missing_file", "reviewed_authentication"} {
		t.Run(scenario, func(t *testing.T) {
			f := newEnrichmentExecutionFixture(t)
			reviewed := scenario == "reviewed_incomplete" || scenario == "reviewed_missing_file" || scenario == "reviewed_authentication"
			if reviewed {
				schedule := f.target.EnrichmentSchedule
				schedule.Reason = "reviewed_single_retry_saved_metadata"
				var err error
				f.target, err = scheduleEnrichment(f.repo, f.target, schedule, f.now)
				require.NoError(t, err)
			}
			job := f.admit(t)
			running := f.claim(t, job.UUID, 0)
			raw := `{"category":"reddit","id":"abc123","title":"Saved title","selftext":"Saved description","date":"2026-10-01T00:00:00Z"}`
			if scenario == "missing_date" {
				raw = `{"category":"reddit","id":"abc123","title":"Saved title","selftext":"Saved description"}`
			}
			if reviewed {
				raw = `{"category":"reddit","id":"abc123","title":"Saved title"}`
			}
			capturedGapFixture(t, f, []byte(raw))
			binding, err := archive.ProbeMediaRoot(t.TempDir())
			require.NoError(t, err)
			root := putMediaRoot(t, f.repo, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Saved media", State: "active", Binding: binding}})
			files := fileContentFixture{db: f.db, repo: f.repo, root: root}
			file := files.addFile(t, "saved.jpg", []byte("retained original media"))
			require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				return f.repo.File.Update(ctx, &models.ImageFile{BaseFile: file.Base(), Width: 3, Height: 4, Format: "jpeg"})
			}))
			attachmentSQL(t, f.db, "INSERT INTO images_files(image_id,file_id,\"primary\") VALUES(41,"+file.Base().ID.String()+",1)")
			media := archiveFind(t, f.repo, models.ArchiveImage, 41)
			require.NoError(t, f.repo.WithTxn(t.Context(), func(ctx context.Context) error {
				post, err := f.repo.SourceEvidence.FindPost(ctx, f.target.PostUUID)
				if err != nil {
					return err
				}
				state := "linked"
				if scenario == "unlinked" {
					state = "unlinked"
				}
				_, err = f.repo.SourcePostMedia.Decide(ctx, models.SourcePostMediaInput{UUID: uuid.NewString(), PostUUID: post.UUID,
					MediaUUID: media.UUID, ExpectedPostRevision: post.Revision, ExpectedMediaRevision: media.Revision, State: state, Origin: "review"})
				return err
			}))
			if scenario == "missing_file" || scenario == "reviewed_missing_file" {
				require.NoError(t, os.Remove(file.Base().Path))
			}
			if scenario == "checkpoint" {
				_, err := f.worker.Checkpoint(t.Context(), f.tokens[0], running.Lease(), 0, f.initial)
				require.NoError(t, err)
			}
			code := "access_denied"
			if scenario == "not_found" || scenario == "authentication" {
				code = scenario
			}
			if scenario == "reviewed_authentication" {
				code = "authentication"
			}
			receipt, err := f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), code)
			require.NoError(t, err)
			require.Equal(t, "failed", receipt.Outcome)
			require.Equal(t, code, receipt.ErrorCode)
			target := readEnrichmentTarget(t, f.repo, f.target.UUID)
			if scenario == "access_denied" || scenario == "not_found" || scenario == "reviewed_incomplete" {
				require.Equal(t, "excluded", target.State)
				reason := "saved_metadata_sufficient"
				if reviewed {
					reason = "reviewed_saved_metadata_accepted"
				}
				require.Equal(t, reason, target.Reason)
				require.Nil(t, target.CompletionUUID)
			} else {
				require.Equal(t, "pending", target.State, scenario)
			}
			again, err := f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), code)
			require.NoError(t, err)
			require.Equal(t, receipt, again)
			require.Equal(t, target, readEnrichmentTarget(t, f.repo, f.target.UUID), "failure replay cannot repeat retirement")
			state, err := f.worker.Find(t.Context(), f.tokens[0], job.UUID)
			require.NoError(t, err)
			require.Equal(t, "failed", state.State)
		})
	}
}
