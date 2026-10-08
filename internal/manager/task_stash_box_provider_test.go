package manager

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func providerBatchHistory(t *testing.T, mgr *Manager, kind models.ArchiveEntityKind, id int) []models.ProviderMetadataImport {
	t.Helper()
	var ret []models.ProviderMetadataImport
	require.NoError(t, mgr.Repository.WithReadTxn(t.Context(), func(ctx context.Context) error {
		entity, err := mgr.Repository.ArchiveEntity.FindByLocalID(ctx, kind, id)
		if err != nil {
			return err
		}
		require.NotNil(t, entity)
		ret, err = mgr.Repository.ProviderMetadata.History(ctx, entity.UUID, 0, 100)
		return err
	}))
	return ret
}

func TestStashBoxStudioBatchAttributesParentCreationAndSelectedRefresh(t *testing.T) {
	mgr, box := stashBoxPerformerTestManager(t, nil)
	id, parentID, details := "studio-id", "parent-id", "Provider details"
	source := &models.ScrapedStudio{Name: "Studio name", Details: &details, RemoteSiteID: &id,
		Parent: &models.ScrapedStudio{Name: "Parent name", RemoteSiteID: &parentID}}
	task := &stashBoxBatchStudioTagTask{box: box, createParent: true}
	task.processMatchedStudio(t.Context(), source, map[string]bool{"details": true})
	var local *models.Studio
	require.NoError(t, mgr.Repository.WithReadTxn(t.Context(), func(ctx context.Context) error {
		matches, err := mgr.Repository.Studio.FindByStashID(ctx, models.StashID{Endpoint: box.Endpoint, StashID: id})
		require.NoError(t, err)
		require.Len(t, matches, 1)
		local = matches[0]
		require.NotNil(t, local.ParentID)
		return nil
	}))
	history := providerBatchHistory(t, mgr, models.ArchiveStudio, local.ID)
	require.Len(t, history, 1)
	require.NotContains(t, string(history[0].Values), "details")
	require.Contains(t, string(history[0].Values), "parent")
	parentHistory := providerBatchHistory(t, mgr, models.ArchiveStudio, *local.ParentID)
	require.Len(t, parentHistory, 1)
	require.Equal(t, parentID, parentHistory[0].RemoteID)
	// An explicit local selection works even if the remote matcher supplied no
	// StoredID. The previous code dereferenced that optional remote field.
	task.studio = local
	source.Name = "Excluded replacement"
	task.processMatchedStudio(t.Context(), source, map[string]bool{"name": true})
	history = providerBatchHistory(t, mgr, models.ArchiveStudio, local.ID)
	require.Len(t, history, 2)
	var values map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(history[1].Values, &values))
	require.NotContains(t, values, "name")
	require.Equal(t, json.RawMessage(`"Provider details"`), values["details"])
	require.NoError(t, mgr.Repository.WithReadTxn(t.Context(), func(ctx context.Context) error {
		current, err := mgr.Repository.Studio.Find(ctx, local.ID)
		require.NoError(t, err)
		require.Equal(t, "Studio name", current.Name)
		return nil
	}))
}

func TestStashBoxTagBatchAttributesCategoryAndAcceptedFields(t *testing.T) {
	mgr, box := stashBoxPerformerTestManager(t, nil)
	id, parentID, description := "tag-id", "category-id", "Provider description"
	source := &models.ScrapedTag{Name: "Tag name", Description: &description, RemoteSiteID: &id,
		Parent: &models.ScrapedTag{Name: "Category name", RemoteSiteID: &parentID}}
	task := &stashBoxBatchTagTagTask{box: box, createParent: true}
	task.processMatchedTag(t.Context(), source, map[string]bool{"description": true})
	var local *models.Tag
	var parentLocalID int
	require.NoError(t, mgr.Repository.WithReadTxn(t.Context(), func(ctx context.Context) error {
		matches, err := mgr.Repository.Tag.FindByStashID(ctx, models.StashID{Endpoint: box.Endpoint, StashID: id})
		require.NoError(t, err)
		require.Len(t, matches, 1)
		local = matches[0]
		parents, err := mgr.Repository.Tag.GetParentIDs(ctx, local.ID)
		require.NoError(t, err)
		require.Len(t, parents, 1)
		parentLocalID = parents[0]
		return nil
	}))
	history := providerBatchHistory(t, mgr, models.ArchiveTag, local.ID)
	require.Len(t, history, 1)
	require.Contains(t, string(history[0].Values), "parents")
	require.NotContains(t, string(history[0].Values), "description")
	require.Len(t, providerBatchHistory(t, mgr, models.ArchiveTag, parentLocalID), 1)
	task.tag = local
	source.Name = "Excluded tag replacement"
	task.processMatchedTag(t.Context(), source, map[string]bool{"name": true})
	history = providerBatchHistory(t, mgr, models.ArchiveTag, local.ID)
	require.Len(t, history, 2)
	var values map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(history[1].Values, &values))
	require.NotContains(t, values, "name")
	require.Equal(t, json.RawMessage(`"Provider description"`), values["description"])
}

func TestStashBoxParentReceiptFailureDoesNotLeaveStaleLocalID(t *testing.T) {
	for _, kind := range []models.ArchiveEntityKind{models.ArchiveStudio, models.ArchiveTag} {
		t.Run(string(kind), func(t *testing.T) {
			mgr, box := stashBoxPerformerTestManager(t, nil)
			box.Endpoint += "?secret=must-not-be-recorded"
			remote := "parent-id"
			if kind == models.ArchiveStudio {
				parent := &models.ScrapedStudio{Name: "Parent studio", RemoteSiteID: &remote}
				err := (&stashBoxBatchStudioTagTask{box: box}).processParentStudio(t.Context(), parent, nil)
				require.Error(t, err)
				require.Nil(t, parent.StoredID)
			} else {
				parent := &models.ScrapedTag{Name: "Parent tag", RemoteSiteID: &remote}
				err := (&stashBoxBatchTagTagTask{box: box}).processParentTag(t.Context(), parent, nil)
				require.Error(t, err)
				require.Nil(t, parent.StoredID)
			}
			require.NoError(t, mgr.Repository.WithReadTxn(t.Context(), func(ctx context.Context) error {
				entity, err := mgr.Repository.ArchiveEntity.FindByLocalID(ctx, kind, 1)
				require.NoError(t, err)
				require.Nil(t, entity)
				return nil
			}))
		})
	}
}

func TestStashBoxStudioBatchRefusesConflictingOwnership(t *testing.T) {
	for _, conflict := range []string{"matched another local studio", "linked to another local studio", "changed selected link", "already matched on create"} {
		t.Run(conflict, func(t *testing.T) {
			mgr, box := stashBoxPerformerTestManager(t, nil)
			local, other := models.NewCreateStudioInput(), models.NewCreateStudioInput()
			local.Name, other.Name = "Selected studio", "Other studio"
			remote := "remote-studio"
			if conflict == "linked to another local studio" {
				other.StashIDs = models.NewRelatedStashIDs([]models.StashID{{Endpoint: box.Endpoint, StashID: remote}})
			}
			if conflict == "changed selected link" {
				local.StashIDs = models.NewRelatedStashIDs([]models.StashID{{Endpoint: box.Endpoint, StashID: "changed-remote"}})
			}
			require.NoError(t, mgr.Repository.WithTxn(t.Context(), func(ctx context.Context) error {
				if err := mgr.Repository.Studio.Create(ctx, &local); err != nil {
					return err
				}
				return mgr.Repository.Studio.Create(ctx, &other)
			}))
			task := &stashBoxBatchStudioTagTask{box: box, studio: local.Studio}
			source := &models.ScrapedStudio{Name: "Must not be written", RemoteSiteID: &remote}
			if conflict == "matched another local studio" || conflict == "already matched on create" {
				id := strconv.Itoa(other.ID)
				source.StoredID = &id
			}
			if conflict == "already matched on create" {
				task.studio = nil
			}
			task.processMatchedStudio(t.Context(), source, nil)
			require.Empty(t, providerBatchHistory(t, mgr, models.ArchiveStudio, local.ID))
			require.Empty(t, providerBatchHistory(t, mgr, models.ArchiveStudio, other.ID))
			require.NoError(t, mgr.Repository.WithReadTxn(t.Context(), func(ctx context.Context) error {
				current, err := mgr.Repository.Studio.Find(ctx, local.ID)
				require.NoError(t, err)
				require.Equal(t, local.Name, current.Name)
				return nil
			}))
		})
	}
}

func TestStashBoxTagBatchRefusesConflictingOwnership(t *testing.T) {
	for _, conflict := range []string{"matched another tag", "linked to another tag", "changed selected link"} {
		t.Run(conflict, func(t *testing.T) {
			mgr, box := stashBoxPerformerTestManager(t, nil)
			local, other := models.NewTag(), models.NewTag()
			local.Name, other.Name = "Selected tag", "Other tag"
			remote := "remote-tag"
			if conflict == "linked to another tag" {
				other.StashIDs = models.NewRelatedStashIDs([]models.StashID{{Endpoint: box.Endpoint, StashID: remote}})
			}
			if conflict == "changed selected link" {
				local.StashIDs = models.NewRelatedStashIDs([]models.StashID{{Endpoint: box.Endpoint, StashID: "changed-remote"}})
			}
			require.NoError(t, mgr.Repository.WithTxn(t.Context(), func(ctx context.Context) error {
				if err := mgr.Repository.Tag.Create(ctx, &models.CreateTagInput{Tag: &local}); err != nil {
					return err
				}
				return mgr.Repository.Tag.Create(ctx, &models.CreateTagInput{Tag: &other})
			}))
			source := &models.ScrapedTag{Name: "Must not be written", RemoteSiteID: &remote}
			if conflict == "matched another tag" {
				id := strconv.Itoa(other.ID)
				source.StoredID = &id
			}
			(&stashBoxBatchTagTagTask{box: box, tag: &local}).processMatchedTag(t.Context(), source, nil)
			require.Empty(t, providerBatchHistory(t, mgr, models.ArchiveTag, local.ID))
			require.Empty(t, providerBatchHistory(t, mgr, models.ArchiveTag, other.ID))
			require.NoError(t, mgr.Repository.WithReadTxn(t.Context(), func(ctx context.Context) error {
				current, err := mgr.Repository.Tag.Find(ctx, local.ID)
				require.NoError(t, err)
				require.Equal(t, local.Name, current.Name)
				return nil
			}))
		})
	}
}

func TestStashBoxTagNameAmbiguityRequiresExplicitRemoteID(t *testing.T) {
	_, box := stashBoxPerformerTestManager(t, nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"queryTags":{"count":2,"tags":[{"id":"first","name":"Shared name","aliases":[]},{"id":"second","name":"SHARED NAME","aliases":[]}]}}}`))
	}))
	defer server.Close()
	box.Endpoint = server.URL
	name := "Shared name"
	result, err := (&stashBoxBatchTagTagTask{box: box, name: &name}).findStashBoxTag(t.Context())
	require.ErrorContains(t, err, "multiple provider tags")
	require.Nil(t, result)
}

func TestStashBoxPerformerCreationDoesNotAttributeRejectedValues(t *testing.T) {
	mgr, box := stashBoxPerformerTestManager(t, nil)
	remote, name, invalid := "performer-id", "Provider Performer", "invalid"
	source := &models.ScrapedPerformer{Name: &name, RemoteSiteID: &remote, Gender: &invalid, Birthdate: &invalid, Height: &invalid, Circumcised: &invalid}
	(&stashBoxBatchPerformerTagTask{box: box}).processMatchedPerformer(t.Context(), source, nil, nil)
	history := providerBatchHistory(t, mgr, models.ArchivePerformer, 1)
	require.Len(t, history, 1)
	require.JSONEq(t, `{"name":"Provider Performer"}`, string(history[0].Values))
}
