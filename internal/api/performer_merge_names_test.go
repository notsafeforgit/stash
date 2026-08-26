package api

import (
	"context"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func performerWithAliases(id int, name string, ignorePrimary bool, aliases ...models.PerformerAlias) *models.Performer {
	if aliases == nil {
		aliases = []models.PerformerAlias{}
	}
	return &models.Performer{
		ID:                       id,
		Name:                     name,
		IgnorePrimaryNameAutoTag: ignorePrimary,
		Aliases:                  models.NewRelatedPerformerAliases(aliases),
	}
}

func TestPreservePerformerMergeNames(t *testing.T) {
	t.Run("direct API retains canonical names and policies", func(t *testing.T) {
		destination := performerWithAliases(3, "Destination", false,
			models.PerformerAlias{Alias: "Existing Alias", IgnoreAutoTag: true},
		)
		sources := []*models.Performer{
			performerWithAliases(1, "Source Name", false,
				models.PerformerAlias{Alias: "Unselected Source Alias", IgnoreAutoTag: true},
			),
			performerWithAliases(2, "source name", true),
		}
		values := models.NewPerformerPartial()

		err := preservePerformerMergeNames(context.Background(), nil, destination, sources, &values, false)
		require.NoError(t, err)
		require.NotNil(t, values.Aliases)
		assert.Equal(t, models.RelationshipUpdateModeSet, values.Aliases.Mode)
		assert.Equal(t, []models.PerformerAlias{
			{Alias: "Existing Alias", IgnoreAutoTag: true},
			{Alias: "Source Name", IgnoreAutoTag: true},
		}, values.Aliases.Values)
		assert.True(t, values.IgnorePrimaryNameAutoTag.Set)
		assert.False(t, values.IgnorePrimaryNameAutoTag.Value)
	})

	t.Run("selected source name becomes canonical", func(t *testing.T) {
		destination := performerWithAliases(2, "Destination", true)
		source := performerWithAliases(1, "Selected Source", false,
			models.PerformerAlias{Alias: "Policy Alias", IgnoreAutoTag: true},
		)
		values := models.NewPerformerPartial()
		values.Name = models.NewOptionalString("Selected Source")
		values.Aliases = &models.UpdatePerformerAliases{
			Values: []models.PerformerAlias{{Alias: "Policy Alias"}},
			Mode:   models.RelationshipUpdateModeSet,
		}

		err := preservePerformerMergeNames(context.Background(), nil, destination, []*models.Performer{source}, &values, false)
		require.NoError(t, err)
		assert.Equal(t, []models.PerformerAlias{
			{Alias: "Policy Alias", IgnoreAutoTag: true},
			{Alias: "Destination", IgnoreAutoTag: true},
		}, values.Aliases.Values)
		assert.True(t, values.IgnorePrimaryNameAutoTag.Set)
		assert.False(t, values.IgnorePrimaryNameAutoTag.Value)
	})

	t.Run("selected alias becomes canonical with its policy", func(t *testing.T) {
		destination := performerWithAliases(2, "Destination", false,
			models.PerformerAlias{Alias: "Selected Alias", IgnoreAutoTag: true},
		)
		source := performerWithAliases(1, "Source Name", false)
		values := models.NewPerformerPartial()
		values.Name = models.NewOptionalString("Selected Alias")
		values.Aliases = &models.UpdatePerformerAliases{
			Values: []models.PerformerAlias{},
			Mode:   models.RelationshipUpdateModeSet,
		}

		err := preservePerformerMergeNames(context.Background(), nil, destination, []*models.Performer{source}, &values, false)
		require.NoError(t, err)
		assert.Equal(t, []models.PerformerAlias{
			{Alias: "Destination"},
			{Alias: "Source Name"},
		}, values.Aliases.Values)
		assert.True(t, values.IgnorePrimaryNameAutoTag.Set)
		assert.True(t, values.IgnorePrimaryNameAutoTag.Value)
	})

	t.Run("explicit canonical policy wins", func(t *testing.T) {
		destination := performerWithAliases(2, "Destination", false)
		source := performerWithAliases(1, "Selected Source", false)
		values := models.NewPerformerPartial()
		values.Name = models.NewOptionalString("Selected Source")
		values.IgnorePrimaryNameAutoTag = models.NewOptionalBool(true)

		err := preservePerformerMergeNames(context.Background(), nil, destination, []*models.Performer{source}, &values, false)
		require.NoError(t, err)
		assert.True(t, values.IgnorePrimaryNameAutoTag.Value)
	})

	t.Run("v2.5 alias list keeps stored policies", func(t *testing.T) {
		destination := performerWithAliases(2, "Destination", false,
			models.PerformerAlias{Alias: "Existing Alias"},
		)
		source := performerWithAliases(1, "Source Name", false,
			models.PerformerAlias{Alias: "Source Alias"},
		)
		values := models.NewPerformerPartial()
		values.Aliases = &models.UpdatePerformerAliases{
			Values: []models.PerformerAlias{
				{Alias: "Existing Alias", IgnoreAutoTag: true},
				{Alias: "Source Name", IgnoreAutoTag: true},
				{Alias: "Source Alias", IgnoreAutoTag: true},
				{Alias: "New Legacy Alias", IgnoreAutoTag: true},
			},
			Mode: models.RelationshipUpdateModeSet,
		}

		err := preservePerformerMergeNames(context.Background(), nil, destination, []*models.Performer{source}, &values, true)
		require.NoError(t, err)
		assert.Equal(t, []models.PerformerAlias{
			{Alias: "Existing Alias"},
			{Alias: "Source Name"},
			{Alias: "Source Alias"},
			{Alias: "New Legacy Alias", IgnoreAutoTag: true},
		}, values.Aliases.Values)
	})
}
