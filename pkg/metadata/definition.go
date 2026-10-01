// Package metadata selects inherited library fields from explicit collection
// policies. It never infers depicted performers from a publisher account.
package metadata

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode"

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
			if mapping.PerformerNames {
				if field != "performers" {
					return errors.New("name matching is supported only for performers")
				}
				if len(mapping.Value) != 0 {
					if _, err := PerformerNames(mapping.Value); err != nil {
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

func PerformerNames(raw json.RawMessage) ([]string, error) {
	var names []string
	if err := json.Unmarshal(raw, &names); err != nil || names == nil || len(names) > 128 {
		return nil, errors.New("performer name matching requires at most 128 names")
	}
	for _, name := range names {
		if name == "" || strings.TrimSpace(name) != name || len(name) > 1024 || strings.ContainsFunc(name, unicode.IsControl) {
			return nil, errors.New("performer name must be nonempty text within 1024 bytes")
		}
	}
	return names, nil
}
