package metadata

import (
	"encoding/json"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

// CanFillBlank implements the reviewed duplicate-repair policy. Imported empty
// values may be filled, but explicit decisions (including clears) remain intact.
func CanFillBlank(state *models.MetadataFieldState) bool {
	if state.Pending || (state.Decision != nil && state.Decision.Mode != "inherit") {
		return false
	}
	if state.Protected && (state.Decision != nil || state.Mode != "preserved" || state.Origin != "legacy") {
		return false
	}
	var value *string
	return json.Unmarshal(state.Value, &value) == nil && (value == nil || strings.TrimSpace(*value) == "")
}
