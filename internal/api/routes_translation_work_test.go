package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestTranslationWorkRoutesInspectSharedCacheAndBoundedTargets(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "translation-work.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	repo := db.Repository()
	var request *models.TranslationRequest
	var held, completed *models.TranslationTarget
	var post *models.SourcePost
	var cache *models.TranslationCache
	now := time.Now().UTC()
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		request, err = repo.TranslationWork.RetainRequest(ctx, models.TranslationRequestInput{OriginalText: "Original source text", TargetLanguage: "en", Policy: models.TranslationBingTextV1})
		if err != nil {
			return err
		}
		post, err = repo.SourceEvidence.EnsurePost(ctx, models.SourcePostIdentifier{Namespace: "native:reddit", Value: "translation-routes"}, "")
		if err != nil {
			return err
		}
		held, err = repo.TranslationWork.RetainTarget(ctx, models.TranslationTargetInput{RequestUUID: request.UUID, PostUUID: post.UUID, Field: "title", Origin: "migration"}, models.TranslationTargetSchedule{State: "held", Priority: 25}, now)
		if err != nil {
			return err
		}
		completed, err = repo.TranslationWork.RetainTarget(ctx, models.TranslationTargetInput{RequestUUID: request.UUID, PostUUID: post.UUID, Field: "caption", Origin: "capture"}, models.TranslationTargetSchedule{State: "pending", Priority: 100}, now)
		return err
	}))
	router := (&nativeArchiveRoutes{repo: repo}).router()
	read := func(path string, status int, target any) {
		t.Helper()
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		require.Equal(t, status, response.Code, "%s: %s", path, response.Body.String())
		if target != nil {
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), target))
		}
	}
	var absent any
	read("/translation-requests/"+request.UUID+"/cache", 200, &absent)
	require.Nil(t, absent, "uncached work is distinct from a nonexistent request")
	require.NoError(t, repo.WithTxn(t.Context(), func(ctx context.Context) error {
		output := "Shared translated text"
		var err error
		cache, err = repo.TranslationWork.RetainCache(ctx, models.TranslationCacheInput{RequestUUID: request.UUID, Status: "translated", TranslatedText: &output, Origin: "worker"})
		if err != nil {
			return err
		}
		completed, err = repo.TranslationWork.PublishTarget(ctx, completed.UUID, completed.Revision, now)
		return err
	}))
	for _, item := range []struct {
		path   string
		status int
	}{
		{"/translation-requests/invalid", 400},
		{"/translation-requests/" + uuid.NewString(), 404},
		{"/translation-requests/invalid/cache", 400},
		{"/translation-requests/" + uuid.NewString() + "/cache", 404},
		{"/translation-requests/invalid/targets", 400},
		{"/translation-requests/" + uuid.NewString() + "/targets", 404},
		{"/translation-requests/" + request.UUID + "/targets?state=unknown", 400},
		{"/translation-requests/" + request.UUID + "/targets?limit=101", 400},
		{"/translation-requests/" + request.UUID + "/targets?after=invalid", 400},
		{"/posts/invalid/translation-targets", 400},
		{"/posts/" + uuid.NewString() + "/translation-targets", 404},
		{"/translation-targets/invalid", 400},
		{"/translation-targets/" + uuid.NewString(), 404},
		{"/translation-targets/invalid/history", 400},
		{"/translation-targets/" + uuid.NewString() + "/history", 404},
		{"/translation-targets/" + held.UUID + "/history?limit=0", 400},
		{"/translation-targets/" + held.UUID + "/history?after=-1", 400},
		{"/translation-targets/" + held.UUID + "/history?after=invalid", 400},
	} {
		read(item.path, item.status, nil)
	}
	var gotRequest models.TranslationRequest
	read("/translation-requests/"+request.UUID, 200, &gotRequest)
	require.Equal(t, request, &gotRequest)
	var gotCache models.TranslationCache
	read("/translation-requests/"+request.UUID+"/cache", 200, &gotCache)
	require.Equal(t, cache, &gotCache)
	var result models.SourceTranslation
	read("/translations/"+*cache.TranslationUUID, 200, &result)
	require.Equal(t, "Shared translated text", result.TranslatedText)
	var gotTarget models.TranslationTarget
	read("/translation-targets/"+held.UUID, 200, &gotTarget)
	require.Equal(t, held, &gotTarget)
	var first, next, byPost []models.TranslationTarget
	read("/translation-requests/"+request.UUID+"/targets?limit=1", 200, &first)
	require.Len(t, first, 1)
	read("/translation-requests/"+request.UUID+"/targets?limit=1&after="+first[0].UUID, 200, &next)
	require.Len(t, next, 1)
	require.NotEqual(t, first[0].UUID, next[0].UUID)
	read("/posts/"+post.UUID+"/translation-targets?state=held", 200, &byPost)
	require.Equal(t, []models.TranslationTarget{*held}, byPost)
	var history, tail []models.TranslationTargetHistory
	read("/translation-targets/"+completed.UUID+"/history?limit=1", 200, &history)
	require.Len(t, history, 1)
	require.Equal(t, "pending", history[0].State)
	read("/translation-targets/"+completed.UUID+"/history?after=1&limit=1", 200, &tail)
	require.Len(t, tail, 1)
	require.Equal(t, "completed", tail[0].State)
	var summary map[string]any
	read("/translation-targets/"+completed.UUID, 200, &summary)
	require.NotContains(t, summary, "original_text")
	require.NotContains(t, summary, "translated_text")
}
