package metadata

import (
	"context"
	"fmt"

	"github.com/stashapp/stash/pkg/models"
)

// DraftPreview deliberately has no Apply digest. Testing a new definition or
// simulating creation never creates a saved policy, intake record or decision.
type DraftPreview struct {
	Input   Input                  `json:"context"`
	State   string                 `json:"state"`
	Data    map[string]interface{} `json:"data,omitempty"`
	Changes []Change               `json:"changes"`
}

// PreviewDraft runs in a read transaction. The input pins the current saved
// policy revision (zero when absent) and collection revision. Created may be
// simulated here; the ordinary preview/apply HTTP paths derive it server-side.
func (s Service) PreviewDraft(ctx context.Context, input Input, definition models.MetadataPolicyDefinition) (*DraftPreview, error) {
	if err := ValidateDefinition(definition); err != nil {
		return nil, fmt.Errorf("%w: %v", models.ErrMetadataPolicyInvalid, err)
	}
	// Validate constants for both kinds, including the kind not selected as the
	// sample. Name collisions are review results, not malformed definitions.
	for kind, rule := range definition.Rules {
		for field, mapping := range rule.Mappings {
			value := mapping.TypedConstant()
			if len(value) == 0 {
				continue
			}
			if _, _, _, err := s.normalize(ctx, kind, field, value, false); err != nil {
				return nil, fmt.Errorf("%w: %s: %v", models.ErrMetadataPolicyInvalid, field, err)
			}
		}
	}
	preview, err := s.preview(ctx, input, true, &definition)
	if err != nil {
		return nil, err
	}
	return &DraftPreview{Input: preview.Input, State: preview.State, Data: preview.Data, Changes: preview.Changes}, nil
}
