package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

type PluginPreviewEntityV3 string

func (e PluginPreviewEntityV3) IsValid() bool { return e == "SCENE" || e == "IMAGE" }

func (e PluginPreviewEntityV3) MarshalGQL(w io.Writer) { fmt.Fprint(w, strconv.Quote(string(e))) }

func (e *PluginPreviewEntityV3) UnmarshalGQL(value interface{}) error {
	text, ok := value.(string)
	if !ok || !PluginPreviewEntityV3(text).IsValid() {
		return fmt.Errorf("invalid preview entity %v", value)
	}
	*e = PluginPreviewEntityV3(text)
	return nil
}

// Providers return expression input only. Their preview handler must be read-only;
// like other executable plugin operations, it is trusted code, not a sandbox.
type PluginSettingPreviewV3 struct {
	Entity      PluginPreviewEntityV3 `yaml:"entity" json:"entity"`
	Description string                `yaml:"description" json:"description"`
}

func (p Plugin) settingPreviewArgs(settingName, entityID string) (map[string]interface{}, error) {
	if p.APIVersion != 3 {
		return nil, fmt.Errorf("setting previews require a v3 plugin")
	}
	if strings.TrimSpace(entityID) == "" {
		return nil, fmt.Errorf("an entity ID is required")
	}
	for _, setting := range p.SettingsV3 {
		if setting.Name == settingName && setting.Preview != nil {
			return map[string]interface{}{
				"mode": "preview", "setting": settingName,
				"entity_type": strings.ToLower(string(setting.Preview.Entity)), "entity_id": entityID,
			}, nil
		}
	}
	return nil, fmt.Errorf("setting %q has no entity preview", settingName)
}

// SettingPreviewV3 invokes the declared context provider, never a hook or task.
// The browser evaluates the unsaved expression separately using the pure jq API.
func (c *Cache) SettingPreviewV3(ctx context.Context, pluginID, settingName, entityID string) (interface{}, error) {
	p := c.GetPlugin(pluginID)
	if p == nil {
		return nil, fmt.Errorf("plugin %q not found", pluginID)
	}
	if !p.Enabled {
		return nil, fmt.Errorf("plugin %q is disabled", pluginID)
	}
	args, err := p.settingPreviewArgs(settingName, entityID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	result, err := c.RunPlugin(ctx, pluginID, args)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > jqMaxBytes {
		return nil, fmt.Errorf("preview input must be JSON of at most %d bytes", jqMaxBytes)
	}
	return result, nil
}
