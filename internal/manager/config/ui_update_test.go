package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpdateUIConfigurationSerializesAndPersists(t *testing.T) {
	c := InitializeEmpty()
	path := filepath.Join(t.TempDir(), "config.yml")
	c.SetConfigFile(path)
	c.SetUIConfiguration(map[string]interface{}{"theme": "dark"})
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for n := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.UpdateUIConfiguration(func(ui map[string]interface{}) (map[string]interface{}, error) {
				ui[fmt.Sprintf("view%d", n)] = "saved"
				return ui, nil
			})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Len(t, c.GetUIConfiguration(), 21)
	require.Equal(t, "dark", c.GetUIConfiguration()["theme"])
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	for n := range 20 {
		require.Contains(t, string(contents), fmt.Sprintf("view%d: saved", n))
	}
}

func TestUpdateUIConfigurationRollsBackOnError(t *testing.T) {
	c := InitializeEmpty()
	c.SetUIConfiguration(map[string]interface{}{"nested": map[string]interface{}{"value": "original"}})
	before := c.GetUIConfiguration()
	_, err := c.UpdateUIConfiguration(func(ui map[string]interface{}) (map[string]interface{}, error) {
		ui["nested"].(map[string]interface{})["value"] = "changed"
		return ui, errors.New("invalid update")
	})
	require.ErrorContains(t, err, "invalid update")
	require.Equal(t, before, c.GetUIConfiguration())
	c.SetConfigFile(filepath.Join(t.TempDir(), "missing", "config.yml"))
	_, err = c.UpdateUIConfiguration(func(ui map[string]interface{}) (map[string]interface{}, error) {
		ui["nested"] = "changed"
		return ui, nil
	})
	require.Error(t, err)
	require.Equal(t, before, c.GetUIConfiguration())
}
