package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/archive"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestAttachmentSelectionReviewHTTPUniqueListsPreviewRecoveryAndHistory(t *testing.T) {
	db, repo, get := sourcePostBrowserHTTPFixture(t)
	var post *models.SourcePost
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		post, err = repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "native:twitter", Value: "album-review"}, "")
		if err != nil {
			return err
		}
		payload, err := archive.PrepareRetainedCapture("gallery-dl", "twitter", []byte(`{"category":"twitter","tweet_id":"album-review"}`))
		if err != nil {
			return err
		}
		for i := range 5 {
			capture, err := repo.SourceEvidence.RecordCapture(ctx, models.SourceCaptureInput{UUID: uuid.NewString(), PostUUID: post.UUID,
				Origin: "gallery-dl", Platform: "twitter", CapturedAt: time.Now().UTC(), RetentionPolicy: archive.SourceRetentionVersion, Payload: *payload})
			if err != nil {
				return err
			}
			_, err = repo.SourceAttachment.RecordManifest(ctx, models.SourceAttachmentManifestInput{CaptureUUID: capture.UUID, DeclaredAlbum: true,
				Entries: []models.SourceAttachmentEntry{{Position: 0, MediaKind: "image", Reference: models.SourcePostIdentifier{Namespace: "native:twitter", Value: strconv.Itoa(i % 2)}},
					{Position: 5, MediaKind: "video", Reference: models.SourcePostIdentifier{Namespace: "native:twitter", Value: "video"}}}})
			if err != nil {
				return err
			}
		}
		post, err = repo.SourceEvidence.FindPost(ctx, post.UUID)
		return err
	}))
	base := "/posts/" + post.UUID
	var first, second []models.AttachmentSelectionReviewManifest
	require.NoError(t, json.Unmarshal(get(base+"/attachment-manifests?limit=1", 200).Body.Bytes(), &first))
	require.Len(t, first, 1)
	require.NoError(t, json.Unmarshal(get(base+"/attachment-manifests?limit=1&after="+first[0].UUID, 200).Body.Bytes(), &second))
	require.Len(t, second, 1)
	require.Less(t, first[0].UUID, second[0].UUID)
	require.JSONEq(t, "[]", get(base+"/attachment-manifests?after="+second[0].UUID, 200).Body.String())
	require.Equal(t, 2, first[0].EntryCount)
	require.False(t, first[0].Complete)
	require.True(t, first[0].DeclaredAlbum)
	require.JSONEq(t, "[]", get(base+"/attachment-selection-history", 200).Body.String())
	get(base+"/attachment-manifests?limit=101", 400)
	get(base+"/attachment-manifests?after=invalid", 400)
	get(base+"/attachment-selection-history?after=-1", 400)
	get("/posts/"+uuid.NewString()+"/attachment-manifests", 404)
	get("/attachment-selection/requests/"+uuid.NewString(), 404)
	get("/attachment-selection/requests/invalid", 400)

	handler := (&nativeArchiveRoutes{repo: repo}).router()
	send := func(path string, body any, status int) *httptest.ResponseRecorder {
		t.Helper()
		encoded, err := json.Marshal(body)
		require.NoError(t, err)
		r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(encoded))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		require.Equal(t, status, w.Code, w.Body.String())
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		return w
	}
	input := models.AttachmentSelectionReviewInput{PostUUID: post.UUID, PostRevision: post.Revision,
		Mode: "pinned", CaptureUUID: first[0].CaptureUUID, Reason: "Choose this source order"}
	var preview models.AttachmentSelectionReviewPreview
	w := send("/attachment-selection/preview", input, 200)
	require.NotContains(t, w.Body.String(), `"Namespace"`, "nested public fields use the native JSON contract")
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &preview))
	require.Nil(t, preview.Current)
	require.Len(t, preview.Proposed.Entries, 2)
	require.Equal(t, "native:twitter", preview.Proposed.Entries[0].Reference.Namespace)
	require.JSONEq(t, "[]", get(base+"/attachment-selection-history", 200).Body.String())
	request := models.AttachmentSelectionReviewApplyInput{AttachmentSelectionReviewInput: input, RequestUUID: uuid.NewString(), Digest: preview.Digest}
	var original models.AttachmentSelectionReview
	for attempt := range 2 {
		var result struct {
			Review   models.AttachmentSelectionReview `json:"review"`
			Replayed bool                             `json:"replayed"`
		}
		require.NoError(t, json.Unmarshal(send("/attachment-selection/apply", request, 200).Body.Bytes(), &result))
		require.Equal(t, attempt == 1, result.Replayed)
		if attempt == 0 {
			original = result.Review
		} else {
			require.Equal(t, original, result.Review)
		}
	}
	var history []attachmentSelectionReviewDecision
	require.NoError(t, json.Unmarshal(get(base+"/attachment-selection-history?limit=1", 200).Body.Bytes(), &history))
	require.Len(t, history, 1)
	require.Equal(t, original.DecisionUUID, history[0].UUID)
	require.Equal(t, "pinned", history[0].Mode)
	require.JSONEq(t, "[]", get(base+"/attachment-selection-history?after="+strconv.Itoa(history[0].Revision), 200).Body.String())
	stale := request
	stale.RequestUUID = uuid.NewString()
	require.Contains(t, send("/attachment-selection/apply", stale, 409).Body.String(), "preview_changed")
	reused := request
	reused.Reason = "Different intent"
	require.Contains(t, send("/attachment-selection/apply", reused, 409).Body.String(), "request_conflict")
	invalid := input
	invalid.Mode = "disabled"
	send("/attachment-selection/preview", invalid, 400)
	send("/attachment-selection/preview", map[string]any{"post_uuid": input.PostUUID, "post_revision": input.PostRevision, "mode": "disabled", "origin": "ingest"}, 400)
	for _, path := range []string{"/attachment-selection/preview", "/attachment-selection/apply"} {
		r := httptest.NewRequest(http.MethodPost, path, nil)
		r.Header.Set("Origin", "https://other.example")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		require.Equal(t, http.StatusForbidden, w.Code)
	}
	require.NoError(t, db.Close())
	require.NoError(t, db.Open(db.DatabasePath()))
	var recovered models.AttachmentSelectionReview
	require.NoError(t, json.Unmarshal(get("/attachment-selection/requests/"+request.RequestUUID, 200).Body.Bytes(), &recovered))
	require.Equal(t, original, recovered)
	require.NoError(t, db.Close())
	restored := restoreAttachmentSelectionReview(t, db.DatabasePath())
	proof, err := sqlite.VerifyNativeSnapshot(t.Context(), restored)
	require.NoError(t, err)
	require.True(t, proof.DatabaseVerified)
	require.NoError(t, db.Open(restored))
	restoredRepo := db.Repository()
	require.NoError(t, restoredRepo.WithReadTxn(t.Context(), func(ctx context.Context) error {
		receipt, err := restoredRepo.SourceAttachment.SelectionReview(ctx, request.RequestUUID)
		require.NoError(t, err)
		require.Equal(t, original, *receipt)
		return nil
	}))
}

func restoreAttachmentSelectionReview(t *testing.T, database string) string {
	t.Helper()
	return restoreSourceReview(t, database, "attachment_selection_review_check.py")
}

func restoreSourceReview(t *testing.T, database, scriptName string) string {
	t.Helper()
	python, producer := nativeProducerRuntime(t)
	archive := filepath.Join(filepath.Dir(producer), "archive")
	entries, err := os.ReadDir(filepath.Join(archive, "src", "stash_archive"))
	require.NoError(t, err)
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".py") {
			_, err := os.ReadFile(filepath.Join(archive, "src", "stash_archive", entry.Name()))
			require.NoError(t, err)
		}
	}
	script := filepath.Join(archive, "tests", scriptName)
	_, err = os.ReadFile(script)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, python, script, database, t.TempDir())
	command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+filepath.Join(archive, "src"))
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	var result struct {
		ArchiveUUID string `json:"archive_uuid"`
		Restored    string `json:"restored"`
	}
	require.NoError(t, json.Unmarshal(output, &result))
	require.NotEmpty(t, result.ArchiveUUID)
	require.FileExists(t, result.Restored)
	return result.Restored
}
