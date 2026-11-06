package sqlite_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestUnavailableEnrichmentRetiresOnlyCompleteSavedPosts(t *testing.T) {
	for _, scenario := range []string{"access_denied", "not_found", "authentication", "missing_date", "missing_file", "unlinked", "checkpoint"} {
		t.Run(scenario, func(t *testing.T) {
			f := newEnrichmentExecutionFixture(t)
			job := f.admit(t)
			running := f.claim(t, job.UUID, 0)
			raw := `{"category":"reddit","id":"abc123","title":"Saved title","selftext":"Saved description","date":"2026-10-01T00:00:00Z"}`
			if scenario == "missing_date" {
				raw = `{"category":"reddit","id":"abc123","title":"Saved title","selftext":"Saved description"}`
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
			if scenario == "missing_file" {
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
			receipt, err := f.worker.Fail(t.Context(), f.tokens[0], running.Lease(), code)
			require.NoError(t, err)
			require.Equal(t, "failed", receipt.Outcome)
			require.Equal(t, code, receipt.ErrorCode)
			target := readEnrichmentTarget(t, f.repo, f.target.UUID)
			if scenario == "access_denied" || scenario == "not_found" {
				require.Equal(t, "excluded", target.State)
				require.Equal(t, "saved_metadata_sufficient", target.Reason)
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
