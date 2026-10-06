package metadata_test

import (
	"encoding/json"
	"testing"

	"github.com/stashapp/stash/pkg/metadata"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestMetadataMappingFallbackRequiresExpressionAndPreservesExplicitNull(t *testing.T) {
	for _, body := range []string{`{"jq":"empty"}`, `{"jq":"empty","fallback":null}`, `{"jq":"empty","fallback":false}`,
		`{"jq":"empty","fallback":0}`, `{"jq":"empty","fallback":""}`, `{"jq":"empty","fallback":[]}`,
		`{"jq":"empty","fallback":{"note":"value"}}`, `{"value":"unchanged"}`} {
		var mapping models.MetadataMapping
		require.NoError(t, json.Unmarshal([]byte(body), &mapping))
		definition := models.MetadataPolicyDefinition{Rules: map[models.ArchiveEntityKind]models.MetadataPolicyRule{
			models.ArchiveScene: {Mappings: map[string]models.MetadataMapping{"title": mapping}},
		}}
		require.NoError(t, metadata.ValidateDefinition(definition), "destination type validation occurs in the repository")
		stored, err := json.Marshal(mapping)
		require.NoError(t, err)
		require.Equal(t, body, string(stored), "old definitions stay byte stable; null and empty defaults stay present")
	}
	for _, mapping := range []models.MetadataMapping{
		{Value: json.RawMessage(`"constant"`), Fallback: json.RawMessage(`null`)},
		{JQ: "empty", Fallback: json.RawMessage(`not-json`)},
		{JQ: "invalid(", Fallback: json.RawMessage(`null`)},
	} {
		require.Error(t, metadata.ValidateDefinition(models.MetadataPolicyDefinition{Rules: map[models.ArchiveEntityKind]models.MetadataPolicyRule{
			models.ArchiveScene: {Mappings: map[string]models.MetadataMapping{"title": mapping}},
		}}))
	}
}
