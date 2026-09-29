package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stashapp/stash/pkg/session"
	"github.com/stretchr/testify/require"
)

type previewServerConfig struct {
	ServerConfig
	disabled []string
}

func (c previewServerConfig) GetDisabledPlugins() []string { return c.disabled }
func (previewServerConfig) GetHost() string                { return "localhost" }
func (previewServerConfig) GetPort() int                   { return 9999 }
func (previewServerConfig) GetConfigPathAbs() string       { return "" }
func (previewServerConfig) HasTLSConfig() bool             { return false }

type previewSessionConfig struct{ session.SessionConfig }

func (previewSessionConfig) GetSessionStoreKey() []byte { return []byte("preview-test-session-key") }
func (previewSessionConfig) GetMaxSessionAge() int      { return 60 }

func TestSettingPreviewExecution(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "fixture.yml")
	require.NoError(t, os.WriteFile(manifest, []byte(`apiVersion: 3
name: Preview fixture
interface: js
exec: [preview.js]
settings:
  mappings:
    type: JSON
    editor: JQ_MAP
    preview: {entity: SCENE}
`), 0600))
	cfg, err := loadPluginFromYAMLFile(manifest)
	require.NoError(t, err)
	c := NewCache(previewServerConfig{})
	c.plugins = []Config{*cfg}
	c.RegisterSessionStore(session.NewStore(previewSessionConfig{}))
	script := filepath.Join(dir, "preview.js")
	require.NoError(t, os.WriteFile(script, []byte(`({Output: input.Args})`), 0600))
	result, err := c.SettingPreviewV3(context.Background(), "fixture", "mappings", "42")
	require.NoError(t, err)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.JSONEq(t, `{"mode":"preview","setting":"mappings","entity_type":"scene","entity_id":"42"}`, string(encoded))

	_, err = c.SettingPreviewV3(context.Background(), "missing", "mappings", "42")
	require.ErrorContains(t, err, "not found")
	c.config = previewServerConfig{disabled: []string{"fixture"}}
	_, err = c.SettingPreviewV3(context.Background(), "fixture", "mappings", "42")
	require.ErrorContains(t, err, "disabled")
	c.config = previewServerConfig{}

	require.NoError(t, os.WriteFile(script, []byte(`({Error: "Catalog unavailable"})`), 0600))
	_, err = c.SettingPreviewV3(context.Background(), "fixture", "mappings", "42")
	require.ErrorContains(t, err, "Catalog unavailable")
	require.NoError(t, os.WriteFile(script, []byte(`({Output: "x".repeat(1048577)})`), 0600))
	_, err = c.SettingPreviewV3(context.Background(), "fixture", "mappings", "42")
	require.ErrorContains(t, err, "at most")

	require.NoError(t, os.WriteFile(script, []byte(`while (true) {}`), 0600))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = c.SettingPreviewV3(ctx, "fixture", "mappings", "42")
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestSettingPreviewContract(t *testing.T) {
	config, err := loadPluginFromYAML(strings.NewReader(`apiVersion: 3
name: Context provider
settings:
  import:
    type: JSON
    editor: JQ_MAP
    preview: {entity: SCENE, description: Import context}
  export:
    editor: JQ
    preview: {entity: IMAGE}
  plain: {type: BOOLEAN}
`))
	require.NoError(t, err)
	p := config.toPlugin()
	args, err := p.settingPreviewArgs("import", "42")
	require.NoError(t, err)
	require.Equal(t, map[string]interface{}{
		"mode": "preview", "setting": "import", "entity_type": "scene", "entity_id": "42",
	}, args)
	args, err = p.settingPreviewArgs("export", "17")
	require.NoError(t, err)
	require.Equal(t, "image", args["entity_type"])
	for _, setting := range []string{"plain", "missing"} {
		_, err = p.settingPreviewArgs(setting, "42")
		require.Error(t, err)
	}
	_, err = p.settingPreviewArgs("import", " ")
	require.Error(t, err)
	p.APIVersion = 2
	_, err = p.settingPreviewArgs("import", "42")
	require.Error(t, err)

	for _, setting := range []string{
		"{editor: JQ, preview: {entity: PERFORMER}}",
		"{editor: JQ, preview: {}}",
		"{type: BOOLEAN, preview: {entity: SCENE}}",
		"{editor: JQ, preview: {entity: SCENE, mode: normal}}",
	} {
		_, err := loadPluginFromYAML(strings.NewReader("apiVersion: 3\nname: Invalid\nsettings:\n  test: " + setting))
		require.Error(t, err, setting)
	}
}
