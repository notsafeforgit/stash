package manager

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stashBoxPerformerTestManager(t *testing.T, remote map[string]any) (*Manager, *models.StashBox) {
	t.Helper()
	cfg := config.InitializeEmpty()
	db := sqlite.NewDatabase()
	require.NoError(t, db.Open(filepath.Join(t.TempDir(), "stash-box.sqlite")))
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	mgr := &Manager{Config: cfg, Database: db, Repository: db.Repository()}
	previous := instance
	instance = mgr
	t.Cleanup(func() { instance = previous })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "fixture-key", r.Header.Get("ApiKey"))
		var request struct {
			Operation string `json:"operationName"`
			Variables struct {
				ID string `json:"id"`
			} `json:"variables"`
		}
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&request)) {
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		assert.Equal(t, "FindPerformerByID", request.Operation)
		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"findPerformer": remote[request.Variables.ID]}}))
	}))
	t.Cleanup(server.Close)
	return mgr, &models.StashBox{Endpoint: server.URL, APIKey: "fixture-key", MaxRequestsPerMinute: 60000}
}

func stashBoxTestPerformer(t *testing.T, mgr *Manager, name, endpoint, remoteID string) *models.Performer {
	t.Helper()
	p := models.NewPerformer()
	p.Name = name
	p.StashIDs = models.NewRelatedStashIDs([]models.StashID{{Endpoint: endpoint, StashID: remoteID}, {Endpoint: "https://other.invalid/graphql", StashID: "other-" + name}})
	require.NoError(t, mgr.Repository.WithTxn(t.Context(), func(ctx context.Context) error {
		return mgr.Repository.Performer.Create(ctx, &models.CreatePerformerInput{Performer: &p})
	}))
	return &p
}

func remotePerformerFixture(id, name, merged string, deleted bool) map[string]any {
	ret := map[string]any{"id": id, "name": name, "deleted": deleted, "aliases": []string{}, "images": []any{}}
	if merged != "" {
		ret["merged_into_id"] = merged
	}
	return ret
}

func TestStashBoxRemoteMergeRetainsSelectedLocalIdentity(t *testing.T) {
	mgr, box := stashBoxPerformerTestManager(t, map[string]any{
		"old":    remotePerformerFixture("old", "Old remote name", "middle", true),
		"middle": remotePerformerFixture("middle", "Intermediate remote name", "final", true),
		"final":  remotePerformerFixture("final", "New remote name", "", false),
	})
	local := stashBoxTestPerformer(t, mgr, "Original local name", box.Endpoint, "old")
	var before *models.ArchiveEntity
	require.NoError(t, mgr.Repository.WithReadTxn(t.Context(), func(ctx context.Context) error {
		var err error
		before, err = mgr.Repository.ArchiveEntity.FindByLocalID(ctx, models.ArchivePerformer, local.ID)
		return err
	}))
	task := &stashBoxBatchPerformerTagTask{performer: local, box: box}
	result, err := task.findStashBoxPerformer(t.Context())
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.StoredID)
	assert.Equal(t, strconv.Itoa(local.ID), *result.StoredID)
	assert.Equal(t, "final", *result.RemoteSiteID)
	task.processMatchedPerformer(t.Context(), result, nil, nil)
	require.NoError(t, mgr.Repository.WithReadTxn(t.Context(), func(ctx context.Context) error {
		current, err := mgr.Repository.Performer.Find(ctx, local.ID)
		require.NoError(t, err)
		assert.Equal(t, "New remote name", current.Name)
		ids, err := mgr.Repository.Performer.GetStashIDs(ctx, local.ID)
		require.NoError(t, err)
		assert.Len(t, ids, 2)
		for _, id := range ids {
			if id.Endpoint == box.Endpoint {
				assert.Equal(t, "final", id.StashID)
			} else {
				assert.Equal(t, "other-Original local name", id.StashID)
			}
		}
		after, err := mgr.Repository.ArchiveEntity.FindByLocalID(ctx, models.ArchivePerformer, local.ID)
		require.NoError(t, err)
		assert.Equal(t, before.UUID, after.UUID)
		imports, err := mgr.Repository.ProviderMetadata.History(ctx, after.UUID, 0, 100)
		require.NoError(t, err)
		require.Len(t, imports, 1)
		assert.Equal(t, box.Endpoint, imports[0].Endpoint)
		assert.Equal(t, "final", imports[0].RemoteID)
		assert.Equal(t, "batch", imports[0].Operation)
		var accepted map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(imports[0].Values, &accepted))
		assert.JSONEq(t, `"New remote name"`, string(accepted["name"]))
		return nil
	}))
}

func TestStashBoxPerformerImportRecordsOnlyAcceptedFields(t *testing.T) {
	mgr, box := stashBoxPerformerTestManager(t, nil)
	remoteID, name, details, aliases := "source-performer", "Provider name", "Provider details", "Provider alias"
	p := &models.ScrapedPerformer{RemoteSiteID: &remoteID, Name: &name, Details: &details, Aliases: &aliases}
	task := &stashBoxBatchPerformerTagTask{box: box}
	task.processMatchedPerformer(t.Context(), p, map[string]bool{"name": true, "details": true}, nil)
	require.NoError(t, mgr.Repository.WithReadTxn(t.Context(), func(ctx context.Context) error {
		matches, err := mgr.Repository.Performer.FindByStashID(ctx, models.StashID{Endpoint: box.Endpoint, StashID: remoteID})
		require.NoError(t, err)
		require.Len(t, matches, 1)
		entity, err := mgr.Repository.ArchiveEntity.FindByLocalID(ctx, models.ArchivePerformer, matches[0].ID)
		require.NoError(t, err)
		imports, err := mgr.Repository.ProviderMetadata.History(ctx, entity.UUID, 0, 100)
		require.NoError(t, err)
		require.Len(t, imports, 1)
		var values map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(imports[0].Values, &values))
		require.Contains(t, values, "name") // mandatory on creation
		require.Contains(t, values, "aliases")
		require.NotContains(t, values, "details")
		require.NotContains(t, values, "stash_ids")
		return nil
	}))
}

func TestStashBoxUnsafeRemoteRefreshLeavesLocalMetadata(t *testing.T) {
	for _, name := range []string{"deleted", "missing target", "cycle", "excessive chain", "target linked elsewhere", "ambiguous original link"} {
		t.Run(name, func(t *testing.T) {
			remote := map[string]any{
				"old":  remotePerformerFixture("old", "Changed remote name", "next", true),
				"next": remotePerformerFixture("next", "Next remote name", "", false),
			}
			switch name {
			case "deleted":
				remote["old"] = remotePerformerFixture("old", "Deleted name", "", true)
			case "missing target":
				delete(remote, "next")
			case "cycle":
				remote["next"] = remotePerformerFixture("next", "Next remote name", "old", true)
			case "excessive chain":
				remote["next"] = remotePerformerFixture("next", "Next remote name", "hop-0", true)
				for i := 0; i < 20; i++ {
					id := fmt.Sprintf("hop-%d", i)
					remote[id] = remotePerformerFixture(id, id, fmt.Sprintf("hop-%d", i+1), true)
				}
			}
			mgr, box := stashBoxPerformerTestManager(t, remote)
			local := stashBoxTestPerformer(t, mgr, "Original local name", box.Endpoint, "old")
			switch name {
			case "target linked elsewhere":
				stashBoxTestPerformer(t, mgr, "Separate local performer", box.Endpoint, "next")
			case "ambiguous original link":
				stashBoxTestPerformer(t, mgr, "Separate local performer", box.Endpoint, "old")
			}
			task := &stashBoxBatchPerformerTagTask{performer: local, box: box}
			var result *models.ScrapedPerformer
			var err error
			require.NotPanics(t, func() { result, err = task.findStashBoxPerformer(t.Context()) })
			require.Error(t, err)
			assert.Nil(t, result)
			task.Start(t.Context())
			require.NoError(t, mgr.Repository.WithReadTxn(t.Context(), func(ctx context.Context) error {
				current, err := mgr.Repository.Performer.Find(ctx, local.ID)
				require.NoError(t, err)
				assert.Equal(t, local.Name, current.Name)
				ids, err := mgr.Repository.Performer.GetStashIDs(ctx, local.ID)
				require.NoError(t, err)
				assert.Equal(t, local.StashIDs.List(), ids)
				return nil
			}))
		})
	}
}

func TestStashBoxRefreshRechecksLinksBeforeWriting(t *testing.T) {
	for _, moved := range []bool{false, true} {
		t.Run(fmt.Sprintf("selected_link_changed_%t", moved), func(t *testing.T) {
			mgr, box := stashBoxPerformerTestManager(t, map[string]any{"old": remotePerformerFixture("old", "Changed remote name", "", false)})
			local := stashBoxTestPerformer(t, mgr, "Original local name", box.Endpoint, "old")
			task := &stashBoxBatchPerformerTagTask{performer: local, box: box}
			result, err := task.findStashBoxPerformer(t.Context())
			require.NoError(t, err)
			if moved {
				require.NoError(t, mgr.Repository.WithTxn(t.Context(), func(ctx context.Context) error {
					partial := models.NewPerformerPartial()
					partial.StashIDs = &models.UpdateStashIDs{Mode: models.RelationshipUpdateModeSet, StashIDs: []models.StashID{{Endpoint: box.Endpoint, StashID: "user-changed-link"}}}
					_, err := mgr.Repository.Performer.UpdatePartial(ctx, local.ID, partial)
					return err
				}))
			} else {
				stashBoxTestPerformer(t, mgr, "Newly linked performer", box.Endpoint, "old")
			}
			task.processMatchedPerformer(t.Context(), result, nil, nil)
			require.NoError(t, mgr.Repository.WithReadTxn(t.Context(), func(ctx context.Context) error {
				current, err := mgr.Repository.Performer.Find(ctx, local.ID)
				require.NoError(t, err)
				assert.Equal(t, local.Name, current.Name)
				ids, err := mgr.Repository.Performer.GetStashIDs(ctx, local.ID)
				require.NoError(t, err)
				if moved {
					require.Len(t, ids, 1)
					assert.Equal(t, "user-changed-link", ids[0].StashID)
				} else {
					assert.Equal(t, local.StashIDs.List(), ids)
				}
				return nil
			}))
		})
	}
}

func TestStashBoxAddExistingRemoteIDDoesNotDuplicatePerformer(t *testing.T) {
	mgr, box := stashBoxPerformerTestManager(t, map[string]any{"old": remotePerformerFixture("old", "Changed remote name", "", false)})
	local := stashBoxTestPerformer(t, mgr, "Original local name", box.Endpoint, "old")
	id := "old"
	task := &stashBoxBatchPerformerTagTask{stashID: &id, box: box}
	task.Start(t.Context())
	require.NoError(t, mgr.Repository.WithReadTxn(t.Context(), func(ctx context.Context) error {
		matches, err := mgr.Repository.Performer.FindByStashID(ctx, models.StashID{Endpoint: box.Endpoint, StashID: id})
		require.NoError(t, err)
		require.Len(t, matches, 1)
		assert.Equal(t, local.ID, matches[0].ID)
		assert.Equal(t, local.Name, matches[0].Name)
		return nil
	}))
}

func TestStashBoxAddRechecksRemoteLinkBeforeCreating(t *testing.T) {
	mgr, box := stashBoxPerformerTestManager(t, map[string]any{"new": remotePerformerFixture("new", "New remote name", "", false)})
	id := "new"
	task := &stashBoxBatchPerformerTagTask{stashID: &id, box: box}
	result, err := task.findStashBoxPerformer(t.Context())
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Nil(t, result.StoredID)
	local := stashBoxTestPerformer(t, mgr, "Concurrently linked performer", box.Endpoint, id)
	task.processMatchedPerformer(t.Context(), result, nil, nil)
	require.NoError(t, mgr.Repository.WithReadTxn(t.Context(), func(ctx context.Context) error {
		matches, err := mgr.Repository.Performer.FindByStashID(ctx, models.StashID{Endpoint: box.Endpoint, StashID: id})
		require.NoError(t, err)
		require.Len(t, matches, 1)
		assert.Equal(t, local.ID, matches[0].ID)
		assert.Equal(t, local.Name, matches[0].Name)
		return nil
	}))
}
