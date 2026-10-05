package config

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/stretchr/testify/require"
)

func TestBackupSnapshotPreservesSeparateSettingsAndOverrides(t *testing.T) {
	c := InitializeEmpty()
	c.SetConfigFile(filepath.Join(t.TempDir(), "config.yml"))
	c.SetInt(Port, 8009)
	c.SetString(ApiKey, "private-test-value")
	require.NoError(t, c.overrides.Set(Port, "8010")) // Overrides retain their original representation.
	c.certFile, c.keyFile = "server.crt", "server.key"
	snapshot, err := c.CaptureBackupSnapshot()
	require.NoError(t, err)
	main, err := yaml.Parser().Unmarshal(snapshot.MainYAML)
	require.NoError(t, err)
	overrides, err := yaml.Parser().Unmarshal(snapshot.OverridesYAML)
	require.NoError(t, err)
	require.Equal(t, 8009, main[Port])
	require.Equal(t, "8010", overrides[Port])
	require.Equal(t, "private-test-value", main[ApiKey])
	require.NotContains(t, overrides, ApiKey)
	require.Equal(t, c.GetConfigFile(), snapshot.ConfigPath)
	require.Equal(t, "server.key", snapshot.TLSKeyPath)
	c.SetInt(Port, 9000)
	require.Equal(t, 8009, main[Port])
	snapshot.MainYAML[0] = '!'
	_, err = c.CaptureBackupSnapshot()
	require.NoError(t, err)
}

func TestBackupSnapshotExcludesSettingsChangesThroughAssetCapture(t *testing.T) {
	c := InitializeEmpty()
	c.SetInt(Port, 8009)
	updated := make(chan struct{})
	captureError := errors.New("asset capture failed")
	err := c.WithBackupSnapshot(func(snapshot *BackupSnapshot) error {
		// A writer cannot acquire the configuration lock while the referenced
		// assets are being captured, even after the YAML has been marshalled.
		acquired := c.TryLock()
		if acquired {
			c.Unlock()
		}
		require.False(t, acquired)
		go func() {
			c.SetInt(Port, 9000)
			close(updated)
		}()
		main, err := yaml.Parser().Unmarshal(snapshot.MainYAML)
		require.NoError(t, err)
		require.Equal(t, 8009, main[Port])
		return captureError
	})
	require.ErrorIs(t, err, captureError)
	select {
	case <-updated:
		require.Equal(t, 9000, c.GetPort())
	case <-time.After(5 * time.Second):
		t.Fatal("failed capture retained the configuration lock")
	}
}
