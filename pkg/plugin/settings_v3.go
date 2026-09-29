package plugin

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
)

// SettingOptions extends the original three setting types without changing
// their representation. In particular, JSON editors persist JSON text.
type SettingOptions struct {
	DefaultValue interface{}           `yaml:"default" json:"default_value"`
	Editor       *PluginSettingEditor  `yaml:"editor" json:"editor"`
	Options      []PluginSettingOption `yaml:"options" json:"options"`
}

type PluginSettingEditor string

func (e PluginSettingEditor) IsValid() bool {
	switch e {
	case "TEXT", "TEXTAREA", "SELECT", "JSON", "JQ", "JQ_MAP":
		return true
	}
	return false
}

func (e PluginSettingEditor) MarshalGQL(w io.Writer) {
	fmt.Fprint(w, strconv.Quote(string(e)))
}

func (e *PluginSettingEditor) UnmarshalGQL(value interface{}) error {
	text, ok := value.(string)
	if !ok {
		return fmt.Errorf("editor must be a string")
	}
	*e = PluginSettingEditor(text)
	if !e.IsValid() {
		return fmt.Errorf("invalid editor %q", text)
	}
	return nil
}

type PluginSettingOption struct {
	Value string `yaml:"value" json:"value"`
	Label string `yaml:"label" json:"label"`
}

func (s SettingConfig) settingType() PluginSettingTypeEnum {
	if s.Type == "" {
		return PluginSettingTypeEnumString
	}
	return s.Type
}

func (s SettingConfig) validateDefinition() error {
	if s.Editor != nil {
		if s.settingType() != PluginSettingTypeEnumString {
			return fmt.Errorf("editors require a STRING setting")
		}
		switch *s.Editor {
		case "TEXT", "TEXTAREA", "JSON", "JQ", "JQ_MAP":
		case "SELECT":
			if len(s.Options) == 0 {
				return fmt.Errorf("SELECT requires options")
			}
		default:
			return fmt.Errorf("unknown editor %q", *s.Editor)
		}
	}
	seen := make(map[string]bool)
	for _, option := range s.Options {
		if seen[option.Value] || option.Label == "" {
			return fmt.Errorf("options require distinct values and nonempty labels")
		}
		seen[option.Value] = true
	}
	if s.DefaultValue != nil {
		return s.validateValue(s.DefaultValue)
	}
	return nil
}

func (s SettingConfig) validateValue(value interface{}) error {
	switch s.settingType() {
	case PluginSettingTypeEnumBoolean:
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected a boolean")
		}
	case PluginSettingTypeEnumNumber:
		encoded, err := json.Marshal(value)
		if err != nil || value == nil {
			return fmt.Errorf("expected a finite number")
		}
		var number float64
		if err := json.Unmarshal(encoded, &number); err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return fmt.Errorf("expected a finite number")
		}
	case PluginSettingTypeEnumString:
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("expected a string")
		}
		if len(text) > jqMaxBytes {
			return fmt.Errorf("setting exceeds %d bytes", jqMaxBytes)
		}
		if s.Editor == nil {
			return nil
		}
		switch *s.Editor {
		case "JSON":
			if !json.Valid([]byte(text)) {
				return fmt.Errorf("expected valid JSON")
			}
		case "JQ":
			_, err := compileJQ(text)
			return err
		case "JQ_MAP":
			var mappings map[string]interface{}
			if err := json.Unmarshal([]byte(text), &mappings); err != nil {
				return fmt.Errorf("expected a JSON object of field names and jq expressions: %w", err)
			}
			return validateJQMappings(mappings)
		case "SELECT":
			for _, option := range s.Options {
				if option.Value == text {
					return nil
				}
			}
			return fmt.Errorf("value is not one of the declared options")
		}
	default:
		return fmt.Errorf("unknown setting type %q", s.Type)
	}
	return nil
}

// SettingsValues merges saved values over defaults without changing either map.
func (p Plugin) SettingsValues(saved map[string]interface{}) map[string]interface{} {
	ret := make(map[string]interface{})
	for _, setting := range p.Settings {
		if setting.DefaultValue != nil {
			ret[setting.Name] = setting.DefaultValue
		}
	}
	for key, value := range saved {
		ret[key] = value
	}
	return ret
}

func (p Plugin) ValidateSettings(input map[string]interface{}, reset []string) error {
	definitions := make(map[string]SettingConfig)
	for _, setting := range p.Settings {
		definitions[setting.Name] = SettingConfig{Type: setting.Type, SettingOptions: setting.SettingOptions}
	}
	for key, value := range input {
		definition, ok := definitions[key]
		if !ok {
			return fmt.Errorf("unknown setting %q", key)
		}
		if err := definition.validateValue(value); err != nil {
			return fmt.Errorf("setting %q: %w", key, err)
		}
	}
	for _, key := range reset {
		if _, ok := definitions[key]; !ok {
			return fmt.Errorf("unknown setting %q", key)
		}
		if _, ok := input[key]; ok {
			return fmt.Errorf("setting %q cannot be updated and reset together", key)
		}
	}
	return nil
}
