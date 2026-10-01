package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func TestProducerCollectionLookupPreservesTargetAndScope(t *testing.T) {
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "collections.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	service := ingest.New(db.Repository())
	const target = "https://www.reddit.com/user/example/submitted/?sort=new"
	const topTarget = "https://www.reddit.com/user/example/submitted/?sort=top&t=all"
	var root, otherRoot *models.MediaRoot
	var producer *models.IngestProducer
	var top, unbound, moved, retired *models.SourceCollection
	var scopes []models.IngestScope
	require.NoError(t, service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		var err error
		root, err = service.Repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Local", State: "active"}})
		require.NoError(t, err)
		otherRoot, err = service.Repo.MediaRoot.Put(ctx, models.MediaRootInput{Origin: "review", MediaRootDefinition: models.MediaRootDefinition{Label: "Other", State: "active"}})
		require.NoError(t, err)
		producer, err = service.Repo.Ingest.CreateProducer(ctx, "Caller")
		require.NoError(t, err)
		put := func(url, state string, rootID *string) *models.SourceCollection {
			prefix := ""
			if rootID != nil {
				prefix = "Private directory"
			}
			value, err := service.Repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
				Label: "Private collection label", Kind: "feed", Namespace: "native:reddit", State: state, TargetURL: url, RootUUID: rootID, PathPrefix: prefix}})
			require.NoError(t, err)
			return value
		}
		// More than one page of unrelated matches cannot hide an authorized
		// candidate. All authorized duplicates must remain visible as ambiguous.
		for range 55 {
			put(target, "active", &root.UUID)
			value := put(target, "active", &root.UUID)
			scopes = append(scopes, models.IngestScope{CollectionUUID: value.UUID, RootUUID: &root.UUID})
		}
		top = put(topTarget, "active", &root.UUID)
		unbound = put(target, "active", nil)
		moved = put(target+"&moved=1", "active", &root.UUID)
		retired = put(target+"&old=1", "active", &root.UUID)
		for _, value := range []*models.SourceCollection{top, unbound, moved, retired} {
			scopes = append(scopes, models.IngestScope{CollectionUUID: value.UUID, RootUUID: value.RootUUID})
		}
		return nil
	}))
	credential, token, err := service.IssueCredential(t.Context(), producer.UUID, scopes, nil)
	require.NoError(t, err)
	require.NoError(t, service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		definition := unbound.SourceCollectionDefinition
		definition.State = "disabled"
		unbound, err = service.Repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: unbound.UUID, ExpectedRevision: unbound.Revision, SourceCollectionDefinition: definition, Origin: "review"})
		require.NoError(t, err)
		definition = retired.SourceCollectionDefinition
		definition.State = "retired"
		retired, err = service.Repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: retired.UUID, ExpectedRevision: retired.Revision, SourceCollectionDefinition: definition, Origin: "review"})
		return err
	}))
	handler := (&ingestRoutes{service: service}).router()
	request := func(auth string, rootID *string, targets []string) *httptest.ResponseRecorder {
		body, err := json.Marshal(map[string]any{"root_uuid": rootID, "targets": targets})
		require.NoError(t, err)
		r := httptest.NewRequest(http.MethodPost, ingestPath+"/collections/lookup", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+auth)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	lookup := func(rootID *string, targets ...string) []ingestCollectionMatches {
		w := request(token, rootID, targets)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		require.NotContains(t, w.Body.String(), "Private")
		var response struct {
			RootUUID *string                   `json:"root_uuid"`
			Targets  []ingestCollectionMatches `json:"targets"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		require.Equal(t, rootID, response.RootUUID)
		return response.Targets
	}
	result := lookup(&root.UUID, target, topTarget, target+"&missing=1", retired.TargetURL)
	require.Len(t, result[0].Candidates, 55)
	require.Equal(t, []ingestCollectionBinding{{top.UUID, 1, "active"}}, result[1].Candidates)
	require.Empty(t, result[2].Candidates)
	require.Equal(t, []ingestCollectionBinding{{retired.UUID, 2, "retired"}}, result[3].Candidates)
	require.Equal(t, []ingestCollectionBinding{{unbound.UUID, 2, "disabled"}}, lookup(nil, target)[0].Candidates)
	require.Equal(t, http.StatusUnauthorized, request("", &root.UUID, []string{target}).Code)
	require.Equal(t, http.StatusForbidden, request(token, &otherRoot.UUID, []string{target}).Code)
	for _, targets := range [][]string{nil, {target, target}, {"not a URL"}, {"https://user:password@example.invalid/"}, {target + "\n"}, strings.Fields(strings.Repeat(target+" ", 51))} {
		require.Equal(t, http.StatusBadRequest, request(token, &root.UUID, targets).Code)
	}
	require.NoError(t, service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		definition := top.SourceCollectionDefinition
		definition.TargetURL += "&changed=1"
		_, err := service.Repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: top.UUID, ExpectedRevision: top.Revision, SourceCollectionDefinition: definition, Origin: "review"})
		require.NoError(t, err)
		definition = moved.SourceCollectionDefinition
		definition.RootUUID = &otherRoot.UUID
		_, err = service.Repo.SourceCollection.Put(ctx, models.SourceCollectionInput{UUID: moved.UUID, ExpectedRevision: moved.Revision, SourceCollectionDefinition: definition, Origin: "review"})
		return err
	}))
	require.Empty(t, lookup(&root.UUID, topTarget)[0].Candidates, "do not substitute historical URLs")
	require.Equal(t, []ingestCollectionBinding{{top.UUID, 2, "active"}}, lookup(&root.UUID, topTarget+"&changed=1")[0].Candidates)
	require.Empty(t, lookup(&root.UUID, moved.TargetURL)[0].Candidates, "a prior root grant cannot see a moved definition")
	rootCredential, rootToken, err := service.IssueCredential(t.Context(), producer.UUID, nil, nil, root.UUID)
	require.NoError(t, err)
	require.Empty(t, rootCredential.Scopes)
	require.Equal(t, []string{root.UUID}, rootCredential.RootUUIDs)
	// Collections registered after issuance are included without minting another
	// token. Candidate limits must still reveal ambiguity instead of truncating
	// to an apparently unique source definition.
	require.NoError(t, service.Repo.WithTxn(t.Context(), func(ctx context.Context) error {
		for range 30 {
			_, err := service.Repo.SourceCollection.Put(ctx, models.SourceCollectionInput{Origin: "review", SourceCollectionDefinition: models.SourceCollectionDefinition{
				Label: "Later source", Kind: "feed", State: "active", TargetURL: target, RootUUID: &root.UUID, PathPrefix: "Later"}})
			if err != nil {
				return err
			}
		}
		return nil
	}))
	w := request(rootToken, &root.UUID, []string{target})
	require.Equal(t, http.StatusOK, w.Code)
	var bounded struct {
		Targets []ingestCollectionMatches `json:"targets"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &bounded))
	require.Len(t, bounded.Targets[0].Candidates, 128)
	require.True(t, bounded.Targets[0].HasMore)
	require.False(t, lookup(&root.UUID, target)[0].HasMore, "individual grants were not expanded")
	require.Equal(t, http.StatusForbidden, request(rootToken, nil, []string{target}).Code)
	require.Equal(t, http.StatusForbidden, request(rootToken, &otherRoot.UUID, []string{target}).Code)
	require.NoError(t, service.Repo.WithTxn(t.Context(), func(ctx context.Context) error { return service.Repo.Ingest.RevokeCredential(ctx, rootCredential.UUID) }))
	require.Equal(t, http.StatusUnauthorized, request(rootToken, &root.UUID, []string{target}).Code)
	require.NoError(t, service.Repo.WithTxn(t.Context(), func(ctx context.Context) error { return service.Repo.Ingest.RevokeCredential(ctx, credential.UUID) }))
	require.Equal(t, http.StatusUnauthorized, request(token, &root.UUID, []string{target}).Code)
}
