package metadata

import (
	"encoding/json"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestReferenceNameMappingsUseTheirFieldShape(t *testing.T) {
	for _, tc := range []struct {
		field, raw string
		valid      bool
	}{
		{"studio", `"Studio name"`, true}, {"studio", `null`, true}, {"studio", `["Studio name"]`, false},
		{"tags", `["Tag name"]`, true}, {"tags", `[]`, true}, {"tags", `null`, false},
		{"performers", `["Known alias"]`, true}, {"tags", `[" spaced "]`, false},
		{"groups", `[{"name":"An album","scene_index":3}]`, true}, {"groups", `[]`, true},
		{"groups", `["An album"]`, false}, {"groups", `[{"uuid":"native-id"}]`, false},
		{"groups", `[{"name":"Album","scene_index":1.5}]`, false},
		{"groups", `[{"name":"Album","unexpected":true}]`, false},
		{"title", `"A title"`, false},
	} {
		definition := models.MetadataPolicyDefinition{Rules: map[models.ArchiveEntityKind]models.MetadataPolicyRule{
			models.ArchiveScene: {Mappings: map[string]models.MetadataMapping{tc.field: {Value: json.RawMessage(tc.raw), ReferenceNames: true}}},
		}}
		err := ValidateDefinition(definition)
		if tc.valid {
			require.NoError(t, err, "%s %s", tc.field, tc.raw)
		} else {
			require.Error(t, err, "%s %s", tc.field, tc.raw)
		}
	}
}
