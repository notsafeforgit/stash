// Package metadata selects inherited library fields from explicit collection
// policies. It never infers depicted performers from a publisher account.
package metadata

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/stashapp/stash/pkg/jq"
	"github.com/stashapp/stash/pkg/models"
)

func ValidateDefinition(def models.MetadataPolicyDefinition) error {
	data, err := json.Marshal(def)
	if err != nil || len(data) > 131072 || len(def.Rules) > 2 {
		return errors.New("metadata policy exceeds its definition limits")
	}
	for kind, rule := range def.Rules {
		if kind != models.ArchiveScene && kind != models.ArchiveImage {
			return errors.New("intake policies support scenes and images")
		}
		fields := make(map[string]models.MetadataFieldDefinition)
		for _, field := range models.MetadataFields(kind) {
			fields[field.Name] = field
		}
		if len(rule.OrganizedRequires) > len(fields)-1 {
			return errors.New("too many organized requirements")
		}
		required := make(map[string]bool)
		for _, field := range rule.OrganizedRequires {
			if _, ok := fields[field]; !ok || field == "organized" || required[field] {
				return fmt.Errorf("invalid or repeated %s organized requirement %q", kind, field)
			}
			required[field] = true
		}
		for field, mapping := range rule.Mappings {
			if _, ok := fields[field]; !ok {
				return fmt.Errorf("unsupported %s policy field %q", kind, field)
			}
			if (mapping.JQ != "") == (len(mapping.Value) != 0) {
				return fmt.Errorf("mapping %q requires exactly one jq expression or value", field)
			}
			if mapping.JQ != "" {
				if _, err := jq.Compile(mapping.JQ); err != nil {
					return fmt.Errorf("mapping %q: %w", field, err)
				}
			} else if !json.Valid(mapping.Value) {
				return fmt.Errorf("mapping %q requires valid JSON", field)
			}
			if mapping.PerformerNames && (field != "performers" || mapping.ReferenceNames) {
				return errors.New("performer_names is only for performers and cannot be combined with reference_names")
			}
			if mapping.UsesNames() {
				if fields[field].ReferenceKind == "" {
					return errors.New("name matching requires a relationship field")
				}
				if len(mapping.Value) != 0 {
					if _, err := referenceNames(fields[field], mapping.Value); err != nil {
						return err
					}
				}
			}
		}
		if _, exists := rule.Mappings["organized"]; exists && rule.MarkOrganized {
			return errors.New("choose an organized mapping or mark_organized, not both")
		}
	}
	return nil
}
