package metadata

import (
	"encoding/json"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestOrganizedCompletenessUsesEffectiveValues(t *testing.T) {
	state := func(value string, protected bool) *models.MetadataFieldState {
		return &models.MetadataFieldState{Value: json.RawMessage(value), Protected: protected}
	}
	states := map[string]*models.MetadataFieldState{
		"title":      state(`"Kept title"`, true),
		"details":    state(`"Old details"`, false),
		"performers": state(`[]`, false),
		"tags":       state(`[]`, true),
		"studio":     state(`null`, false),
		"rating100":  state(`0`, true),
	}
	changes := []Change{
		{Field: "title", Value: json.RawMessage(`""`), Status: "protected"},
		{Field: "details", Value: json.RawMessage(`""`), Status: "ready"},
		{Field: "performers", Value: json.RawMessage(`["chosen-performer"]`), Status: "ready"},
		{Field: "tags", Value: json.RawMessage(`["ignored-candidate"]`), Status: "protected"},
		{Field: "studio", Value: json.RawMessage(`"unreviewed-studio"`), Status: "review"},
	}
	fields := []string{"title", "details", "performers", "tags", "studio", "rating100"}
	require.Equal(t, []string{"details", "tags", "studio"}, missingOrganizedFields(fields, states, changes))
	changes[1].Value = json.RawMessage(`"New details"`)
	changes[2].Status = "older_capture"
	require.Equal(t, []string{"performers", "tags", "studio"}, missingOrganizedFields(fields, states, changes))
	require.Empty(t, missingOrganizedFields(nil, states, changes))
	require.Equal(t, []string{"missing"}, missingOrganizedFields([]string{"missing"}, states, changes))
}

func TestOrganizedCompletenessPresence(t *testing.T) {
	for _, raw := range []string{`null`, `""`, `" \n\t "`, `[]`, `{}`, `invalid`} {
		require.False(t, metadataPresent(json.RawMessage(raw)), raw)
	}
	for _, raw := range []string{`"Title"`, `0`, `100`, `["performer"]`, `{"vendor":"purchase"}`} {
		require.True(t, metadataPresent(json.RawMessage(raw)), raw)
	}
}

func TestOrganizedRequirementsAreSchemaFields(t *testing.T) {
	definition := func(kind models.ArchiveEntityKind, required []string) models.MetadataPolicyDefinition {
		return models.MetadataPolicyDefinition{Rules: map[models.ArchiveEntityKind]models.MetadataPolicyRule{
			kind: {MarkOrganized: true, OrganizedRequires: required},
		}}
	}
	require.NoError(t, ValidateDefinition(definition(models.ArchiveScene, []string{"title", "performers", "groups"})))
	require.NoError(t, ValidateDefinition(definition(models.ArchiveImage, []string{"title", "photographer"})))
	for _, fields := range [][]string{{"organized"}, {"performers", "performers"}, {"invented"}, {"groups"}} {
		require.Error(t, ValidateDefinition(definition(models.ArchiveImage, fields)))
	}
}
