package plugin

import (
	"fmt"
	"strings"
)

// PluginMappingTargetV3 declares a destination field, not a path into jq input.
// Type describes the destination's value format; the destination API remains
// responsible for validating evaluated values.
type PluginMappingTargetV3 struct {
	Name        string `yaml:"name" json:"name"`
	Label       string `yaml:"label" json:"label"`
	Type        string `yaml:"type" json:"type"`
	Description string `yaml:"description" json:"description"`
}

func (s SettingConfigV3) validateMappingTargets() error {
	if s.MappingTargets == nil {
		return nil
	}
	if s.Editor == nil || *s.Editor != "JQ_MAP" {
		return fmt.Errorf("mappingTargets requires a JQ_MAP editor")
	}
	if len(s.MappingTargets) == 0 {
		return fmt.Errorf("mappingTargets must declare at least one target")
	}
	seen := make(map[string]bool, len(s.MappingTargets))
	for _, target := range s.MappingTargets {
		if strings.TrimSpace(target.Name) == "" || target.Name != strings.TrimSpace(target.Name) || seen[target.Name] {
			return fmt.Errorf("mappingTargets require distinct, nonempty names without surrounding whitespace")
		}
		if strings.TrimSpace(target.Label) == "" || strings.TrimSpace(target.Type) == "" {
			return fmt.Errorf("mapping target %q requires a label and value type", target.Name)
		}
		seen[target.Name] = true
	}
	return nil
}

func (s SettingConfigV3) validateMappings(mappings map[string]interface{}) error {
	if s.MappingTargets != nil {
		allowed := make(map[string]bool, len(s.MappingTargets))
		for _, target := range s.MappingTargets {
			allowed[target.Name] = true
		}
		for name := range mappings {
			if !allowed[name] {
				return fmt.Errorf("unsupported mapping target %q; choose a declared target field", name)
			}
		}
	}
	return validateJQMappings(mappings)
}
