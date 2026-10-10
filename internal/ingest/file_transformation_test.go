package ingest_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/file"
	"github.com/stashapp/stash/pkg/file/video"
	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func convertedFileEvent(t *testing.T, f intakePublicationFixture) ingest.FileEvent {
	t.Helper()
	event := f.fileEvent(t)
	event.RelativePath, event.MediaKind = "animation.mkv", models.ArchiveScene
	event.Transformation = &ingest.FileTransformation{Kind: "gif-to-video", OriginalRelativePath: "animation.gif"}
	// Admission opens the final file but deliberately leaves byte/probe
	// verification to the worker; these are opaque bytes for admission tests.
	require.NoError(t, os.WriteFile(filepath.Join(f.root.Binding.Path, event.RelativePath), []byte("converted file"), 0600))
	return event
}

func TestFileTransformationAdmissionFreezesOriginalLifetimeAndReplaysAfterRestart(t *testing.T) {
	f := newIntakePublicationFixture(t, true)
	repo := f.service.Repo
	original := f.createScannedFile(t)
	original.Base().Path = filepath.Join(f.root.Binding.Path, "animation.gif")
	original.Base().Basename = "animation.gif"
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		return repo.File.Update(ctx, original)
	}))
	originalTarget := f.captureTarget(t, "animation.gif", true)
	require.NotEmpty(t, originalTarget.FileUUID)
	event := convertedFileEvent(t, f)
	accepted, err := f.submitFile(t, event)
	require.NoError(t, err)

	// A later file lifetime must not retroactively change the accepted claim.
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		_, err := repo.FileContent.Advance(ctx, originalTarget.FileUUID, originalTarget.Generation)
		return err
	}))
	databasePath := f.db.DatabasePath()
	require.NoError(t, f.db.Close())
	require.NoError(t, f.db.Open(databasePath))
	f.service = ingest.New(f.db.Repository())
	replayed, err := f.submitFile(t, event)
	require.NoError(t, err)
	require.Equal(t, accepted, replayed)
	claimed, err := job.NewDurable(f.service.Repo).Claim(t.Context(), models.ArchiveJobVerifyMedia, uuid.NewString(), time.Minute)
	require.NoError(t, err)
	require.Equal(t, accepted.JobUUID, claimed.UUID)
	var work ingest.FileWork
	require.NoError(t, json.Unmarshal(claimed.Arguments, &work))
	require.NotNil(t, work.Publication.Transformation)
	require.Equal(t, *originalTarget, work.Publication.Transformation.Original)
	require.Equal(t, f.receipt.CaptureUUID, work.Publication.Source.CaptureUUID)
	_, err = ingest.DescribeFileJob(claimed)
	require.NoError(t, err, "accepted transformation work survives strict job decoding")
}

func TestFileTransformationRejectsUnrelatedPathsKindsAndProducerIdentityOverrides(t *testing.T) {
	f := newIntakePublicationFixture(t, true)
	event := convertedFileEvent(t, f)
	for _, original := range []string{"../animation.gif", "/animation.gif", "other/animation.gif", "other.gif", "animation.jpg", "animation.gif.part"} {
		t.Run(original, func(t *testing.T) {
			changed := event
			changed.EventUUID = uuid.NewString()
			changed.Transformation = &ingest.FileTransformation{Kind: "gif-to-video", OriginalRelativePath: original}
			_, err := f.submitFile(t, changed)
			require.ErrorIs(t, err, ingest.ErrInvalid)
		})
	}
	for _, change := range []func(*ingest.FileEvent){
		func(e *ingest.FileEvent) { e.Source = nil },
		func(e *ingest.FileEvent) { e.MediaKind = models.ArchiveImage },
		func(e *ingest.FileEvent) { e.RelativePath = "animation.mp4" },
		func(e *ingest.FileEvent) {
			e.Transformation = &ingest.FileTransformation{Kind: "rename", OriginalRelativePath: "animation.gif"}
		},
	} {
		changed := event
		changed.EventUUID = uuid.NewString()
		change(&changed)
		_, err := f.submitFile(t, changed)
		require.ErrorIs(t, err, ingest.ErrInvalid)
	}
	raw, err := json.Marshal(event)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(raw, &body))
	body["transformation"].(map[string]any)["image_uuid"] = uuid.NewString()
	raw, err = json.Marshal(body)
	require.NoError(t, err)
	_, err = f.service.FileCompleted(t.Context(), f.token, raw, ingest.Digest(raw))
	require.Error(t, err, "producers cannot choose the replaced image identity")
}

func TestFileTransformationRetainsPortableSourceEvidenceAfterVideoPublication(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg unavailable")
	}
	f := newIntakePublicationFixture(t, true)
	event := convertedFileEvent(t, f)
	path := filepath.Join(f.root.Binding.Path, event.RelativePath)
	output, err := exec.CommandContext(t.Context(), ffmpeg, "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=32x32:r=5", "-t", "0.4", "-c:v", "ffv1", "-y", path).CombinedOutput()
	require.NoError(t, err, string(output))
	scanner := &file.Scanner{FingerprintCalculator: intakeFingerprinter{}, FileDecorators: []file.Decorator{&video.Decorator{FFProbe: intakeProbe(t)}}}
	prepared, err := ingest.PrepareMedia(t.Context(), *f.root, event.RelativePath, nil, "", scanner)
	require.NoError(t, err)
	defer prepared.Close()
	event.Size, event.SHA256 = prepared.Snapshot().Size, prepared.SHA256()
	_, err = f.submitFile(t, event)
	require.NoError(t, err)
	repo := f.service.Repo
	durable := job.NewDurable(repo)
	claimed, err := durable.Claim(t.Context(), models.ArchiveJobVerifyMedia, uuid.NewString(), time.Minute)
	require.NoError(t, err)
	var work ingest.FileWork
	require.NoError(t, json.Unmarshal(claimed.Arguments, &work))
	_, err = durable.Publish(t.Context(), claimed.Lease(), func(ctx context.Context, _ *models.ArchiveJob) (models.ArchiveJobOutcome, error) {
		published, err := prepared.PublishIntake(ctx, repo, work.Publication)
		if err != nil {
			return models.ArchiveJobOutcome{}, err
		}
		require.Equal(t, models.ArchiveScene, published.Result.MediaKind)
		require.Equal(t, "linked", published.Result.SourceMedia)
		body, err := json.Marshal(published.Result)
		return models.ArchiveJobOutcome{State: "succeeded", Result: body}, err
	})
	require.NoError(t, err)
	require.NoError(t, repo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		evidence, err := repo.SourceAttachment.MediaEvidence(ctx, work.Publication.Source.AttachmentUUID, "", 10)
		require.NoError(t, err)
		require.Len(t, evidence, 1)
		var details map[string]any
		require.NoError(t, json.Unmarshal(evidence[0].Details, &details))
		require.Equal(t, map[string]any{"kind": "gif-to-video", "original_relative_path": "animation.gif"}, details["transformation"])
		require.NotContains(t, string(evidence[0].Details), f.root.Binding.Path)
		require.NotContains(t, string(evidence[0].Details), "path_fence")
		return nil
	}))
}
