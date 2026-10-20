package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/plugin/common"
	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stashapp/stash/pkg/session"
	"github.com/stretchr/testify/require"
)

type durableHookConfig struct{ previewServerConfig }

func (durableHookConfig) GetPluginHookOrder() map[string][]string { return nil }

func TestDurableHooksKeepIdentityReportFailureAndStop(t *testing.T) {
	_, repo, _, imageID := pluginLibraryFixture(t)
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
	c.RegisterRepository(repo)
	c.RegisterSessionStore(session.NewStore(previewSessionConfig{}))
	script := filepath.Join(dir, "hook.js")
	event := uuid.NewString()
	require.NoError(t, os.WriteFile(script, []byte(`({Error: input.Args.hookContext.eventId})`), 0600))
	for range 2 {
		err := c.ExecuteDurablePostHooks(t.Context(), event, 7, hook.ImageCreatePost, nil, nil)
		require.ErrorContains(t, err, event)
	}
	require.NoError(t, os.WriteFile(script, []byte(`({Error: input.Args.hookContext.id === "`+imageID+`" ? null : "wrong public ID"})`), 0600))
	require.NoError(t, c.ExecuteDurablePostHooks(t.Context(), event, 7, hook.ImageCreatePost, nil, nil))
	require.NoError(t, os.WriteFile(script, []byte(`while (true) {}`), 0600))
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, c.ExecuteDurablePostHooks(ctx, event, 7, hook.ImageCreatePost, nil, nil), context.DeadlineExceeded)
	require.Error(t, c.ExecuteDurablePostHooks(t.Context(), "", 7, hook.ImageCreatePost, nil, nil))
}

func TestPreparedDurableHooksKeepPublicReferencesAndCycleDetection(t *testing.T) {
	store, repo, sceneID, imageID := pluginLibraryFixture(t)
	dir := t.TempDir()
	manifest := filepath.Join(dir, "fixture.yml")
	require.NoError(t, os.WriteFile(manifest, []byte(`apiVersion: 3
name: Prepared hook fixture
interface: js
exec: [hook.js]
hooks:
  - name: Updated
    triggeredBy: [Image.Update.Post]
`), 0600))
	cfg, err := loadPluginFromYAMLFile(manifest)
	require.NoError(t, err)
	c := NewCache(durableHookConfig{})
	c.plugins = []Config{*cfg}
	c.RegisterRepository(repo)
	c.RegisterSessionStore(session.NewStore(previewSessionConfig{}))
	ctx := session.AddVisitedPluginHook(t.Context(), cfg.id, hook.ImageUpdatePost)
	notice, err := c.PrepareDurablePostHook(ctx, hook.ImageUpdatePost, map[string]any{"scene_ids": []string{"42"}}, []string{"scene_ids"})
	require.NoError(t, err)
	require.Equal(t, []common.HookSource{{PluginID: cfg.id, Type: string(hook.ImageUpdatePost)}}, notice.ParentHooks)
	data, err := json.Marshal(notice)
	require.NoError(t, err)
	var restored common.HookContext
	require.NoError(t, json.Unmarshal(data, &restored))
	restored.UUID, restored.EventID = imageID, uuid.NewString()
	store.rows[sceneID].State = models.ArchiveEntityDeleted
	store.rows[sceneID].LocalID = nil
	script := filepath.Join(dir, "hook.js")
	require.NoError(t, os.WriteFile(script, []byte(`({Error: input.Args.hookContext.input.scene_ids[0] === "`+sceneID+`" && input.Args.hookContext.parentHooks.length === 1 ? null : "changed prepared event"})`), 0600))
	require.NoError(t, c.DeliverPreparedPostHook(t.Context(), restored), "deleting a related entity cannot remap the accepted notice")
	restored.ParentHooks = nil
	for range maxCyclicLoopDepth {
		restored.ParentHooks = append(restored.ParentHooks, common.HookSource{PluginID: cfg.id, Type: string(hook.ImageUpdatePost)})
	}
	require.NoError(t, os.WriteFile(script, []byte(`({Error:"cycle should have been suppressed"})`), 0600))
	require.NoError(t, c.DeliverPreparedPostHook(t.Context(), restored), "restart must not reset the parent hook chain")
}
