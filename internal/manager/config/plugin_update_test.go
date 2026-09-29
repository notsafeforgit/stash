package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPluginSettingsAtomicPatch(t *testing.T) {
	c := InitializeEmpty()
	t.Cleanup(func() { InitializeEmpty() })
	path := filepath.Join(t.TempDir(), "config.yml")
	c.SetConfigFile(path)
	c.SetPluginConfiguration("fixture", map[string]interface{}{"keep": false, "reset": 1})
	var wg sync.WaitGroup
	errs := make(chan error, 10)
	for n := range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.UpdatePluginConfiguration("fixture", map[string]interface{}{fmt.Sprintf("key%d", n): n}, nil)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	values, err := c.UpdatePluginConfiguration("fixture", nil, []string{"reset"})
	require.NoError(t, err)
	require.Len(t, values, 11)
	require.Equal(t, false, values["keep"])
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(contents), "key9: 9")
	c.SetConfigFile(filepath.Join(t.TempDir(), "missing", "config.yml"))
	_, err = c.UpdatePluginConfiguration("fixture", map[string]interface{}{"keep": true}, nil)
	require.Error(t, err)
	require.Equal(t, values, c.GetPluginConfiguration("fixture"))
}
