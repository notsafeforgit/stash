package metadata

import (
	"encoding/json"
	"strings"

	"github.com/stashapp/stash/pkg/models"
)

// Completeness considers the values that will remain after this preview is
// applied. An explicit protected field still counts, while a rejected candidate
// must not make an empty field appear populated. Clearing a field removes it
// from the completed set even when its previously selected value was nonempty.
func missingOrganizedFields(required []string, states map[string]*models.MetadataFieldState, changes []Change) []string {
	missing := []string{}
	for _, field := range required {
		state := states[field]
		if state == nil {
			missing = append(missing, field)
			continue
		}
		value := state.Value
		if !state.Protected {
			for _, change := range changes {
				if change.Field == field && (change.Status == "ready" || change.Status == "unchanged") {
					value = change.Value
				}
			}
		}
		if !metadataPresent(value) {
			missing = append(missing, field)
		}
	}
	return missing
}

func metadataPresent(raw json.RawMessage) bool {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return false
	}
	switch value := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(value) != ""
	case []any:
		return len(value) > 0
	case map[string]any:
		return len(value) > 0
	default:
		// Zero is a real rating; only null means no rating was selected.
		return true
	}
}
