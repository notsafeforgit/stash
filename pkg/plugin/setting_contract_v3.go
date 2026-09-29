package plugin

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
)

type PluginSettingTypeV3 string

func (t PluginSettingTypeV3) IsValid() bool {
	return t == "STRING" || t == "NUMBER" || t == "BOOLEAN" || t == "JSON"
}

func (t PluginSettingTypeV3) MarshalGQL(w io.Writer) { fmt.Fprint(w, strconv.Quote(string(t))) }

func (t *PluginSettingTypeV3) UnmarshalGQL(value interface{}) error {
	text, ok := value.(string)
	if !ok || !PluginSettingTypeV3(text).IsValid() {
		return fmt.Errorf("invalid v3 setting type %v", value)
	}
	*t = PluginSettingTypeV3(text)
	return nil
}

type PluginSettingEditorV3 string

func (e PluginSettingEditorV3) IsValid() bool {
	switch e {
	case "TEXT", "TEXTAREA", "SELECT", "JSON", "JQ", "JQ_MAP":
		return true
	}
	return false
}

func (e PluginSettingEditorV3) MarshalGQL(w io.Writer) { fmt.Fprint(w, strconv.Quote(string(e))) }

func (e *PluginSettingEditorV3) UnmarshalGQL(value interface{}) error {
	text, ok := value.(string)
	if !ok || !PluginSettingEditorV3(text).IsValid() {
		return fmt.Errorf("invalid v3 setting editor %v", value)
	}
	*e = PluginSettingEditorV3(text)
	return nil
}

type PluginSettingOptionV3 struct {
	Value string `yaml:"value" json:"value"`
	Label string `yaml:"label" json:"label"`
}

type SettingConfigV3 struct {
	Type         PluginSettingTypeV3     `yaml:"type" json:"type"`
	DisplayName  string                  `yaml:"displayName" json:"display_name"`
	Description  string                  `yaml:"description" json:"description"`
	DefaultValue interface{}             `yaml:"default" json:"default_value"`
	Editor       *PluginSettingEditorV3  `yaml:"editor" json:"editor"`
	Options      []PluginSettingOptionV3 `yaml:"options" json:"options"`
	Preview      *PluginSettingPreviewV3 `yaml:"preview" json:"preview"`
}

type PluginSettingV3 struct {
	Name string `json:"name"`
	SettingConfigV3
}

func (s *SettingConfigV3) normalizeAndValidate() error {
	if s.Type == "" {
		s.Type = "STRING"
	}
	if !s.Type.IsValid() {
		return fmt.Errorf("unknown type %q", s.Type)
	}
	if s.Type == "JSON" && s.Editor == nil {
		editor := PluginSettingEditorV3("JSON")
		s.Editor = &editor
	}
	if s.Editor != nil {
		if !s.Editor.IsValid() {
			return fmt.Errorf("unknown editor %q", *s.Editor)
		}
		jsonEditor := *s.Editor == "JSON" || *s.Editor == "JQ_MAP"
		if jsonEditor && s.Type != "JSON" && s.Type != "STRING" {
			return fmt.Errorf("%s editor requires type JSON or STRING", *s.Editor)
		}
		if !jsonEditor && s.Type != "STRING" {
			return fmt.Errorf("%s editor requires type STRING", *s.Editor)
		}
		if *s.Editor == "SELECT" && len(s.Options) == 0 {
			return fmt.Errorf("SELECT requires options")
		}
	}
	seen := make(map[string]bool)
	for _, option := range s.Options {
		if seen[option.Value] || option.Label == "" {
			return fmt.Errorf("options require distinct values and nonempty labels")
		}
		seen[option.Value] = true
	}
	if s.Options == nil {
		s.Options = []PluginSettingOptionV3{}
	}
	if s.Preview != nil {
		if s.Editor == nil || (*s.Editor != "JQ" && *s.Editor != "JQ_MAP") {
			return fmt.Errorf("preview requires a JQ or JQ_MAP editor")
		}
		if !s.Preview.Entity.IsValid() {
			return fmt.Errorf("preview entity must be SCENE or IMAGE")
		}
	}
	if s.DefaultValue != nil {
		value, err := manifestJSON(s.DefaultValue)
		if err != nil {
			return err
		}
		s.DefaultValue = value
		return s.validateValue(value)
	}
	return nil
}

// YAML objects may have non-string keys; settings must be ordinary JSON.
func manifestJSON(value interface{}) (interface{}, error) {
	switch value := value.(type) {
	case map[interface{}]interface{}:
		ret := make(map[string]interface{}, len(value))
		for key, child := range value {
			name, ok := key.(string)
			if !ok {
				return nil, fmt.Errorf("JSON setting object keys must be strings")
			}
			converted, err := manifestJSON(child)
			if err != nil {
				return nil, err
			}
			ret[name] = converted
		}
		return ret, nil
	case []interface{}:
		ret := make([]interface{}, len(value))
		for i, child := range value {
			converted, err := manifestJSON(child)
			if err != nil {
				return nil, err
			}
			ret[i] = converted
		}
		return ret, nil
	default:
		return value, nil
	}
}

func (s SettingConfigV3) validateValue(value interface{}) error {
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > jqMaxBytes {
		return fmt.Errorf("setting must be JSON of at most %d bytes", jqMaxBytes)
	}
	switch s.Type {
	case "BOOLEAN":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected a boolean")
		}
	case "NUMBER":
		var number float64
		if value == nil || json.Unmarshal(encoded, &number) != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return fmt.Errorf("expected a finite number")
		}
	case "STRING":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("expected a string")
		}
		if s.Editor != nil {
			switch *s.Editor {
			case "JSON":
				if !json.Valid([]byte(text)) {
					return fmt.Errorf("expected valid JSON")
				}
			case "JQ_MAP":
				var mappings map[string]interface{}
				if err := json.Unmarshal([]byte(text), &mappings); err != nil {
					return fmt.Errorf("expected a JSON object of field names and jq expressions: %w", err)
				}
				return validateJQMappings(mappings)
			case "JQ":
				_, err := compileJQ(text)
				return err
			case "SELECT":
				for _, option := range s.Options {
					if option.Value == text {
						return nil
					}
				}
				return fmt.Errorf("value is not one of the declared options")
			}
		}
	case "JSON":
		if s.Editor != nil && *s.Editor == "JQ_MAP" {
			mappings, ok := value.(map[string]interface{})
			if !ok {
				return fmt.Errorf("expected an object of field names and jq expressions")
			}
			return validateJQMappings(mappings)
		}
	default:
		return fmt.Errorf("unknown setting type %q", s.Type)
	}
	return nil
}

func adaptLegacySettings(settings []PluginSetting) []PluginSettingV3 {
	ret := make([]PluginSettingV3, 0, len(settings))
	for _, setting := range settings {
		var editor *PluginSettingEditorV3
		if setting.Editor != nil {
			value := PluginSettingEditorV3(*setting.Editor)
			editor = &value
		}
		options := make([]PluginSettingOptionV3, 0, len(setting.Options))
		for _, option := range setting.Options {
			options = append(options, PluginSettingOptionV3(option))
		}
		ret = append(ret, PluginSettingV3{Name: setting.Name, SettingConfigV3: SettingConfigV3{
			Type: PluginSettingTypeV3(setting.Type), DisplayName: setting.DisplayName, Description: setting.Description,
			DefaultValue: setting.DefaultValue, Editor: editor, Options: options,
		}})
	}
	return ret
}

func (p Plugin) SettingsValuesV3(saved map[string]interface{}) map[string]interface{} {
	if p.APIVersion != 3 {
		return p.SettingsValues(saved)
	}
	ret := make(map[string]interface{})
	for _, setting := range p.SettingsV3 {
		if setting.DefaultValue != nil {
			ret[setting.Name] = setting.DefaultValue
		}
	}
	for key, value := range saved {
		ret[key] = value
	}
	// Earlier mapping editors saved JSON text. A native JQ_MAP has an
	// unambiguous object shape, so old overrides can be read without losing
	// them. New writes are validated as objects by the v3 contract.
	for _, setting := range p.SettingsV3 {
		if setting.Type != "JSON" || setting.Editor == nil || *setting.Editor != "JQ_MAP" {
			continue
		}
		if text, ok := ret[setting.Name].(string); ok {
			var mappings map[string]interface{}
			if json.Unmarshal([]byte(text), &mappings) == nil && setting.validateValue(mappings) == nil {
				ret[setting.Name] = mappings
			}
		}
	}
	return ret
}

func (p Plugin) ValidateSettingsV3(input map[string]interface{}, reset []string) error {
	if p.APIVersion != 3 {
		return p.ValidateSettings(input, reset)
	}
	definitions := make(map[string]SettingConfigV3, len(p.SettingsV3))
	for _, setting := range p.SettingsV3 {
		definitions[setting.Name] = setting.SettingConfigV3
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
