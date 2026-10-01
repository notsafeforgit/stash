package plugin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stashapp/stash/pkg/session"
	"github.com/stretchr/testify/require"
)

type durableHookConfig struct{ previewServerConfig }

func (durableHookConfig) GetPluginHookOrder() map[string][]string { return nil }

func TestDurableHooksKeepIdentityReportFailureAndStop(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "fixture.yml")
	require.NoError(t, os.WriteFile(manifest, []byte(`apiVersion: 3
name: Durable hook fixture
interface: js
exec: [hook.js]
hooks:
  - name: Created
    triggeredBy: [Image.Create.Post]
`), 0600))
	cfg, err := loadPluginFromYAMLFile(manifest)
	require.NoError(t, err)
	c := NewCache(durableHookConfig{})
	c.plugins = []Config{*cfg}
	c.RegisterSessionStore(session.NewStore(previewSessionConfig{}))
	script := filepath.Join(dir, "hook.js")
	event := uuid.NewString()
	require.NoError(t, os.WriteFile(script, []byte(`({Error: input.Args.hookContext.eventId})`), 0600))
	for range 2 {
		err := c.ExecuteDurablePostHooks(t.Context(), event, 7, hook.ImageCreatePost, nil, nil)
		require.ErrorContains(t, err, event)
	}
	require.NoError(t, os.WriteFile(script, []byte(`({Output: "handled"})`), 0600))
	require.NoError(t, c.ExecuteDurablePostHooks(t.Context(), event, 7, hook.ImageCreatePost, nil, nil))
	require.NoError(t, os.WriteFile(script, []byte(`while (true) {}`), 0600))
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, c.ExecuteDurablePostHooks(ctx, event, 7, hook.ImageCreatePost, nil, nil), context.DeadlineExceeded)
	require.Error(t, c.ExecuteDurablePostHooks(t.Context(), "", 7, hook.ImageCreatePost, nil, nil))
}
