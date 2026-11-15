package plugin

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stashapp/stash/pkg/plugin/hook"
	"github.com/stretchr/testify/require"
)

// setTestPlugins publishes plugins as a registry snapshot, as ReloadPlugins does.
func (c *Cache) setTestPlugins(plugins ...Config) {
	c.registry.Store(&pluginRegistry{plugins: plugins})
}

// TestReloadPluginsConcurrentReaders reloads the registry while readers use
// the public accessors. Run with -race: a reader must never observe a partially
// published registry, and the stable plugin must be present in every snapshot.
func TestReloadPluginsConcurrentReaders(t *testing.T) {
	dir := t.TempDir()
	const (
		stable  = "apiVersion: 3\nname: Stable\nhooks: [{name: Stable hook, triggeredBy: [Scene.Update.Post]}]\n"
		valid   = "apiVersion: 3\nname: Toggled\nhooks: [{name: Toggled hook, triggeredBy: [Scene.Update.Post]}]\n"
		invalid = "apiVersion: 4\nname: Toggled\n"
	)
	write := func(name, body string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0600))
	}
	write("stable.yml", stable)
	write("toggled.yml", invalid)

	cache := NewCache(manifestServerConfig{directory: dir})
	cache.ReloadPlugins()

	done := make(chan struct{})
	var readers sync.WaitGroup
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				if cache.GetPlugin("stable") == nil {
					t.Error("stable plugin missing from a registry snapshot")
				}
				_ = cache.ListPlugins()
				_ = cache.ListPluginTasks()
				_ = cache.LoadErrors()
				_ = cache.HasHooks(hook.SceneUpdatePost)
			}
		}()
	}

	const reloads = 40
	// A second reloader contends with the writer on reloadMu.
	var reloaders sync.WaitGroup
	reloaders.Add(1)
	go func() {
		defer reloaders.Done()
		for range reloads {
			cache.ReloadPlugins()
		}
	}()
	for i := range reloads {
		if i%2 == 1 {
			write("toggled.yml", valid)
		} else {
			write("toggled.yml", invalid)
		}
		cache.ReloadPlugins()
	}
	reloaders.Wait()
	close(done)
	readers.Wait()

	// Settle on the valid manifest once all concurrent writers have finished.
	write("toggled.yml", valid)
	cache.ReloadPlugins()
	require.NotNil(t, cache.GetPlugin("toggled"))
	require.Len(t, cache.ListPlugins(), 2)
	require.Empty(t, cache.LoadErrors())
}
