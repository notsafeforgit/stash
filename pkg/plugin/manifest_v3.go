package plugin

import (
	"bytes"
	"fmt"
	"io"
	"sort"

	"github.com/stashapp/stash/pkg/utils"
	"gopkg.in/yaml.v2"
)

// ManifestV3 is a separate contract, not an extension of the v2.5 manifest.
// Shared execution details are adapted to Config only after strict decoding.
type ManifestV3 struct {
	APIVersion        int                          `yaml:"apiVersion"`
	Name              string                       `yaml:"name"`
	Description       *string                      `yaml:"description"`
	URL               *string                      `yaml:"url"`
	Version           *string                      `yaml:"version"`
	Interface         interfaceEnum                `yaml:"interface"`
	Exec              []string                     `yaml:"exec"`
	PluginErrLogLevel string                       `yaml:"errLog"`
	Tasks             []*OperationConfig           `yaml:"tasks"`
	Hooks             []*HookConfig                `yaml:"hooks"`
	UI                ManifestUIV3                 `yaml:"ui"`
	Settings          map[string]SettingConfigV3   `yaml:"settings"`
	Operations        map[string]OperationConfigV3 `yaml:"operations"`
}

type ManifestUIV3 struct {
	Entry    string       `yaml:"entry"`
	Requires []string     `yaml:"requires"`
	Assets   utils.URLMap `yaml:"assets"`
	CSP      PluginCSP    `yaml:"csp"`
}

func decodePluginManifest(reader io.Reader) (*Config, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	var header map[string]interface{}
	if err := yaml.Unmarshal(data, &header); err != nil {
		return nil, err
	}
	value, versioned := header["apiVersion"]
	if !versioned {
		return loadLegacyPluginFromYAML(bytes.NewReader(data))
	}
	version, ok := value.(int)
	if !ok || version != 3 {
		return nil, fmt.Errorf("unsupported plugin apiVersion %v; supported: 3 (or omit for legacy plugins)", value)
	}
	var manifest ManifestV3
	if err := yaml.UnmarshalStrict(data, &manifest); err != nil {
		return nil, fmt.Errorf("v3 plugin manifest: %w", err)
	}
	for _, task := range manifest.Tasks {
		if task == nil || task.Name == "" {
			return nil, fmt.Errorf("v3 tasks require a name")
		}
	}
	for _, hook := range manifest.Hooks {
		if hook == nil || hook.Name == "" || len(hook.TriggeredBy) == 0 {
			return nil, fmt.Errorf("v3 hooks require a name and triggeredBy events")
		}
		for _, event := range hook.TriggeredBy {
			if !event.IsValid() {
				return nil, fmt.Errorf("v3 hook %s: unsupported event %q", hook.Name, event)
			}
		}
	}
	for key, setting := range manifest.Settings {
		if key == "" {
			return nil, fmt.Errorf("v3 setting name cannot be empty")
		}
		if err := setting.normalizeAndValidate(); err != nil {
			return nil, fmt.Errorf("v3 setting %s: %w", key, err)
		}
		manifest.Settings[key] = setting
	}
	for name, operation := range manifest.Operations {
		if !operationNameV3.MatchString(name) || !operation.Kind.IsValid() {
			return nil, fmt.Errorf("v3 operation %q requires a name containing letters, digits or underscores and kind QUERY or MUTATION", name)
		}
	}
	ret := &Config{
		v3: &manifest, Name: manifest.Name, Description: manifest.Description,
		URL: manifest.URL, Version: manifest.Version, Interface: manifest.Interface,
		Exec: manifest.Exec, PluginErrLogLevel: manifest.PluginErrLogLevel,
		Tasks: manifest.Tasks, Hooks: manifest.Hooks,
		UI: UIConfig{Entry: manifest.UI.Entry, Requires: manifest.UI.Requires, Assets: manifest.UI.Assets, CSP: manifest.UI.CSP},
	}
	if ret.Interface == "" {
		ret.Interface = InterfaceEnumRaw
	}
	if err := ret.valid(); err != nil {
		return nil, err
	}
	return ret, nil
}

func (c Config) apiVersion() int {
	if c.v3 != nil {
		return 3
	}
	return 2
}

func (c Config) settingsV3() []PluginSettingV3 {
	if c.v3 == nil {
		// Legacy plugins are adapted into the new API; v3 authors never need to
		// provide a legacy representation of their settings.
		return adaptLegacySettings(c.getPluginSettings())
	}
	keys := make([]string, 0, len(c.v3.Settings))
	for key := range c.v3.Settings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ret := make([]PluginSettingV3, 0, len(keys))
	for _, key := range keys {
		ret = append(ret, PluginSettingV3{Name: key, SettingConfigV3: c.v3.Settings[key]})
	}
	return ret
}

// LegacyPlugins and LegacyPluginTasks keep v3-only metadata out of v2.5 clients.
// Backend hooks still run for successful writes from every client.
func (c Cache) LegacyPlugins() []*Plugin {
	var ret []*Plugin
	for _, p := range c.ListPlugins() {
		if p.APIVersion != 3 {
			ret = append(ret, p)
		}
	}
	return ret
}

func (c Cache) LegacyPluginTasks() []*PluginTask {
	var ret []*PluginTask
	for _, task := range c.ListPluginTasks() {
		if task.Plugin.APIVersion != 3 {
			ret = append(ret, task)
		}
	}
	return ret
}
