package stashbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type protocolRequest struct {
	Operation string          `json:"operationName"`
	Query     string          `json:"query"`
	Variables json.RawMessage `json:"variables"`
}

// Exercise the generated HTTP client and model conversions together. All keys,
// IDs, fingerprints and response documents here are synthetic; no external
// service receives requests, including from submission tests.
func protocolClient(t *testing.T, response func(protocolRequest) any, options ...ClientOption) (*Client, <-chan protocolRequest) {
	t.Helper()
	requests := make(chan protocolRequest, 100)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/graphql/fixture", r.URL.Path)
		assert.Equal(t, "synthetic-api-key", r.Header.Get("ApiKey"))
		assert.True(t, strings.HasPrefix(r.Header.Get("User-Agent"), "stash/"))
		var request protocolRequest
		if !assert.NoError(t, json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&request)) {
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		requests <- request
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"data": response(request)}))
	}))
	t.Cleanup(server.Close)
	options = append([]ClientOption{MaxRequestsPerMinute(60000)}, options...)
	return NewClient(models.StashBox{Endpoint: server.URL + "/graphql/fixture", APIKey: "synthetic-api-key"}, options...), requests
}

func TestProtocolPerformerBatchKeepsInputOrder(t *testing.T) {
	client, requests := protocolClient(t, func(request protocolRequest) any {
		assert.Equal(t, "SearchPerformer", request.Operation)
		var variables struct {
			Term string `json:"term"`
		}
		assert.NoError(t, json.Unmarshal(request.Variables, &variables))
		return map[string]any{"searchPerformer": []any{map[string]any{
			"id": "remote-" + variables.Term, "name": variables.Term, "aliases": []string{},
		}}}
	})
	result, err := client.QueryPerformers(context.Background(), []string{"", "Example Alpha", "", "Example Beta"})
	require.NoError(t, err)
	require.Len(t, result, 4)
	assert.Empty(t, result[0])
	assert.Empty(t, result[2])
	for index, name := range map[int]string{1: "Example Alpha", 3: "Example Beta"} {
		require.Len(t, result[index], 1)
		assert.Equal(t, name, *result[index][0].Name)
		assert.Equal(t, "remote-"+name, *result[index][0].RemoteSiteID)
	}
	require.Len(t, requests, 2)
	for _, name := range []string{"Example Alpha", "Example Beta"} {
		assert.JSONEq(t, fmt.Sprintf(`{"term":%q}`, name), string((<-requests).Variables))
	}
}

func TestProtocolRemotePerformerStateAndMissingRecord(t *testing.T) {
	client, _ := protocolClient(t, func(request protocolRequest) any {
		assert.Equal(t, "FindPerformerByID", request.Operation)
		var variables struct {
			ID string `json:"id"`
		}
		assert.NoError(t, json.Unmarshal(request.Variables, &variables))
		if variables.ID == "missing" {
			return map[string]any{"findPerformer": nil}
		}
		return map[string]any{"findPerformer": map[string]any{
			"id": "remote-before-merge", "name": "Example Performer", "deleted": true,
			"merged_into_id": "remote-survivor", "merged_ids": []string{"older-id"},
			"aliases": []string{"EXAMPLE PERFORMER", "Other Name", "other name"},
			"urls":    []any{map[string]any{"url": "https://example.invalid/profile", "type": "HOME"}},
			"images":  []any{},
		}}
	})
	performer, err := client.FindPerformerByID(context.Background(), "remote-before-merge")
	require.NoError(t, err)
	require.NotNil(t, performer)
	assert.Equal(t, "remote-before-merge", *performer.RemoteSiteID)
	assert.True(t, performer.RemoteDeleted)
	assert.Equal(t, "remote-survivor", *performer.RemoteMergedIntoId)
	assert.Equal(t, "Other Name", *performer.Aliases)
	assert.Equal(t, []string{"https://example.invalid/profile"}, performer.URLs)
	missing, err := client.FindPerformerByID(context.Background(), "missing")
	require.NoError(t, err)
	assert.Nil(t, missing)
}

func TestProtocolFingerprintBatchesPreserveAlgorithmsAndPositions(t *testing.T) {
	client, requests := protocolClient(t, func(request protocolRequest) any {
		assert.Equal(t, "FindScenesBySceneFingerprints", request.Operation)
		var variables struct {
			Fingerprints [][]struct {
				Algorithm string `json:"algorithm"`
				Hash      string `json:"hash"`
			} `json:"fingerprints"`
		}
		assert.NoError(t, json.Unmarshal(request.Variables, &variables))
		assert.LessOrEqual(t, len(variables.Fingerprints), 40)
		var results []any
		for _, fingerprints := range variables.Fingerprints {
			if !assert.NotEmpty(t, fingerprints) {
				continue
			}
			results = append(results, []any{map[string]any{
				"id": "remote-" + fingerprints[0].Hash, "title": "Fixture scene", "images": []any{},
				"fingerprints": []any{map[string]any{"algorithm": "PHASH", "hash": "8000000000000000", "duration": 120, "submissions": 2}},
				"studio":       map[string]any{"id": "remote-studio", "name": "Fixture Studio", "aliases": []string{"Studio Alias"}},
				"tags":         []any{map[string]any{"id": "tag-keep", "name": "Keep"}, map[string]any{"id": "tag-filter", "name": "Internal"}},
			}})
		}
		return map[string]any{"findScenesBySceneFingerprints": results}
	}, ExcludeTagPatterns([]string{"^Internal$"}))
	inputs := []models.Fingerprints{
		{
			{Type: models.FingerprintTypeMD5, Fingerprint: "00000000000000000000000000000001"},
			{Type: models.FingerprintTypeOshash, Fingerprint: "0123456789abcdef"},
			{Type: models.FingerprintTypePhash, Fingerprint: int64(-9223372036854775808)},
			{Type: "sha256", Fingerprint: "not-a-stash-box-matching-algorithm"},
		},
		nil,
		{{Type: "sha256", Fingerprint: "not-a-stash-box-matching-algorithm"}},
	}
	for i := 2; i <= 41; i++ {
		inputs = append(inputs, models.Fingerprints{{Type: models.FingerprintTypeMD5, Fingerprint: fmt.Sprintf("%032x", i)}})
	}
	results, err := client.FindScenesByFingerprints(context.Background(), inputs)
	require.NoError(t, err)
	require.Len(t, results, len(inputs))
	assert.Empty(t, results[1])
	assert.Empty(t, results[2])
	for index, fingerprints := range inputs {
		if index == 1 || index == 2 {
			continue
		}
		require.Len(t, results[index], 1)
		scene := results[index][0]
		assert.Equal(t, "remote-"+fingerprints[0].String(), *scene.RemoteSiteID)
		require.NotNil(t, scene.Studio)
		assert.Equal(t, "remote-studio", *scene.Studio.RemoteSiteID)
		require.Len(t, scene.Tags, 1)
		assert.Equal(t, "Keep", scene.Tags[0].Name)
		require.Len(t, scene.Fingerprints, 1)
		assert.Equal(t, "8000000000000000", scene.Fingerprints[0].Hash)
	}
	require.Len(t, requests, 2)
	first := <-requests
	var variables map[string][]json.RawMessage
	require.NoError(t, json.Unmarshal(first.Variables, &variables))
	require.Len(t, variables["fingerprints"], 40)
	assert.JSONEq(t, `[{"algorithm":"MD5","hash":"00000000000000000000000000000001"},{"algorithm":"OSHASH","hash":"0123456789abcdef"},{"algorithm":"PHASH","hash":"8000000000000000"}]`, string(variables["fingerprints"][0]))
	require.NoError(t, json.Unmarshal((<-requests).Variables, &variables))
	assert.Len(t, variables["fingerprints"], 1)
}

func TestProtocolFingerprintInvalidBatchFails(t *testing.T) {
	for name, response := range map[string]any{
		"missing result": []any{},
		"extra result":   []any{[]any{}, []any{}},
		"null scene":     []any{[]any{nil}},
	} {
		t.Run(name, func(t *testing.T) {
			client, _ := protocolClient(t, func(request protocolRequest) any {
				return map[string]any{"findScenesBySceneFingerprints": response}
			})
			var err error
			require.NotPanics(t, func() {
				_, err = client.FindSceneByFingerprints(context.Background(), models.Fingerprints{{Type: models.FingerprintTypeMD5, Fingerprint: "00000000000000000000000000000001"}})
			})
			require.Error(t, err)
		})
	}
}

func TestProtocolEmptyFingerprintInputMakesNoRequest(t *testing.T) {
	client, requests := protocolClient(t, func(request protocolRequest) any {
		t.Error("empty fingerprints must not query a remote service")
		return nil
	})
	results, err := client.FindScenesByFingerprints(context.Background(), []models.Fingerprints{nil, {{Type: "sha256", Fingerprint: "unused"}}})
	require.NoError(t, err)
	assert.Equal(t, [][]*models.ScrapedScene{nil, nil}, results)
	assert.Empty(t, requests)
}

func TestProtocolFingerprintSubmissionUsesSelectedEndpoint(t *testing.T) {
	client, requests := protocolClient(t, func(request protocolRequest) any {
		assert.Equal(t, "SubmitFingerprint", request.Operation)
		return map[string]any{"submitFingerprint": true}
	})
	fingerprints := models.Fingerprints{
		{Type: models.FingerprintTypeMD5, Fingerprint: "00000000000000000000000000000001"},
		{Type: models.FingerprintTypeOshash, Fingerprint: "0123456789abcdef"},
		{Type: models.FingerprintTypePhash, Fingerprint: int64(-1)},
		{Type: "sha256", Fingerprint: "local-content-proof"},
	}
	files := models.NewRelatedVideoFiles([]*models.VideoFile{
		{BaseFile: &models.BaseFile{Fingerprints: fingerprints}, Duration: 123.9},
		{BaseFile: &models.BaseFile{Fingerprints: fingerprints}, Duration: 0},
	})
	scenes := []*models.Scene{
		{Files: files, StashIDs: models.NewRelatedStashIDs([]models.StashID{
			{Endpoint: "https://other.invalid/graphql", StashID: "other-server-id"},
			{Endpoint: client.box.Endpoint, StashID: "selected-server-id"},
		})},
		{Files: files, StashIDs: models.NewRelatedStashIDs([]models.StashID{{Endpoint: "https://other.invalid/graphql", StashID: "unrelated-scene"}})},
	}
	ok, err := client.SubmitFingerprints(context.Background(), scenes)
	require.NoError(t, err)
	assert.True(t, ok)
	require.Len(t, requests, 3)
	for _, fingerprint := range []string{
		`{"algorithm":"MD5","hash":"00000000000000000000000000000001","duration":123}`,
		`{"algorithm":"OSHASH","hash":"0123456789abcdef","duration":123}`,
		`{"algorithm":"PHASH","hash":"ffffffffffffffff","duration":123}`,
	} {
		assert.JSONEq(t, `{"input":{"scene_id":"selected-server-id","fingerprint":`+fingerprint+`}}`, string((<-requests).Variables))
	}
}

func TestProtocolRateLimitHonorsCancellation(t *testing.T) {
	client, requests := protocolClient(t, func(request protocolRequest) any {
		assert.Equal(t, "Me", request.Operation)
		return map[string]any{"me": map[string]any{"name": "Synthetic User"}}
	}, MaxRequestsPerMinute(1))
	user, err := client.GetUser(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "Synthetic User", user.Me.Name)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = client.GetUser(ctx)
	require.Error(t, err)
	assert.Len(t, requests, 1, "waiting for the rate limit must not send another request")
}

func TestProtocolStudioAndTagMetadata(t *testing.T) {
	const remoteID = "b4f6cfee-1b6b-43a4-ae81-1e1dc2ab19e9"
	client, requests := protocolClient(t, func(request protocolRequest) any {
		switch request.Operation {
		case "FindStudio":
			return map[string]any{"findStudio": map[string]any{
				"id": remoteID, "name": "Fixture Studio", "aliases": []string{"Another Name"},
				"urls":   []any{map[string]any{"url": "https://example.invalid/studio", "type": "HOME"}},
				"parent": map[string]any{"id": "parent-id", "name": "Parent Studio"},
			}}
		case "FindTag":
			return map[string]any{"findTag": map[string]any{
				"id": remoteID, "name": "Fixture Tag", "aliases": []string{"Tag Alias"},
				"description": "Fixture description", "category": map[string]any{"id": "category-id", "name": "Fixture Category"},
			}}
		default:
			t.Errorf("unexpected operation %q", request.Operation)
			return nil
		}
	})
	for _, search := range []string{remoteID, "Fixture Studio"} {
		studio, err := client.FindStudio(context.Background(), search)
		require.NoError(t, err)
		require.NotNil(t, studio)
		assert.Equal(t, remoteID, *studio.RemoteSiteID)
		assert.Equal(t, "Another Name", *studio.Aliases)
		assert.Equal(t, []string{"https://example.invalid/studio"}, studio.URLs)
		require.NotNil(t, studio.Parent)
		assert.Equal(t, "parent-id", *studio.Parent.RemoteSiteID)
	}
	tags, err := client.QueryTag(context.Background(), remoteID)
	require.NoError(t, err)
	require.Len(t, tags, 1)
	assert.Equal(t, remoteID, *tags[0].RemoteSiteID)
	assert.Equal(t, []string{"Tag Alias"}, tags[0].AliasList)
	require.NotNil(t, tags[0].Parent)
	assert.Equal(t, "Fixture Category", tags[0].Parent.Name)
	require.Len(t, requests, 3)
	assert.JSONEq(t, `{"id":"`+remoteID+`","name":null}`, string((<-requests).Variables))
	assert.JSONEq(t, `{"id":null,"name":"Fixture Studio"}`, string((<-requests).Variables))
	assert.JSONEq(t, `{"id":"`+remoteID+`","name":null}`, string((<-requests).Variables))
}

func TestProtocolSceneDraftRetainsEndpointQualifiedIDs(t *testing.T) {
	requests := make(chan protocolRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "synthetic-api-key", r.Header.Get("ApiKey"))
		if !assert.NoError(t, r.ParseMultipartForm(1<<20)) {
			http.Error(w, "bad multipart fixture", http.StatusBadRequest)
			return
		}
		defer func() { assert.NoError(t, r.MultipartForm.RemoveAll()) }()
		var request protocolRequest
		assert.NoError(t, json.Unmarshal([]byte(r.FormValue("operations")), &request))
		assert.Contains(t, request.Query, "mutation SubmitSceneDraft")
		assert.JSONEq(t, `{"0":["variables.input.image"]}`, r.FormValue("map"))
		image, _, err := r.FormFile("0")
		if assert.NoError(t, err) {
			body, err := io.ReadAll(image)
			assert.NoError(t, err)
			assert.NoError(t, image.Close())
			assert.Equal(t, []byte("synthetic-image-bytes"), body)
		}
		requests <- request
		w.Header().Set("Content-Type", "application/json")
		_, err = io.WriteString(w, `{"data":{"submitSceneDraft":{"id":"new-draft"}}}`)
		assert.NoError(t, err)
	}))
	defer server.Close()
	client := NewClient(models.StashBox{Endpoint: server.URL, APIKey: "synthetic-api-key"})
	ids := func(id string) models.RelatedStashIDs {
		return models.NewRelatedStashIDs([]models.StashID{
			{Endpoint: "https://other.invalid/graphql", StashID: "wrong-" + id},
			{Endpoint: server.URL, StashID: id},
		})
	}
	draft := SceneDraft{
		Scene:  &models.Scene{Title: "Native scene", StashIDs: ids("scene-id"), URLs: models.NewRelatedStrings([]string{"https://example.invalid/scene"}), Files: models.NewRelatedVideoFiles([]*models.VideoFile{})},
		Studio: &models.Studio{Name: "Native studio", StashIDs: ids("studio-id")},
		Performers: []*models.Performer{
			{Name: "Canonical name", StashIDs: ids("performer-id")},
			{Name: "Unlinked name", StashIDs: models.NewRelatedStashIDs([]models.StashID{{Endpoint: "https://other.invalid/graphql", StashID: "wrong-unlinked"}})},
		},
		Tags:  []*models.Tag{{Name: "Native tag", StashIDs: ids("tag-id")}},
		Cover: []byte("synthetic-image-bytes"),
	}
	id, err := client.SubmitSceneDraft(context.Background(), draft)
	require.NoError(t, err)
	require.NotNil(t, id)
	assert.Equal(t, "new-draft", *id)
	require.Len(t, requests, 1)
	assert.JSONEq(t, `{"input":{"id":"scene-id","title":"Native scene","urls":["https://example.invalid/scene"],"studio":{"id":"studio-id","name":"Native studio"},"performers":[{"id":"performer-id","name":"Canonical name"},{"name":"Unlinked name"}],"tags":[{"id":"tag-id","name":"Native tag"}],"fingerprints":[]}}`, string((<-requests).Variables))
}
