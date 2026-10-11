package config

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSourceTrackingIsOptIn(t *testing.T) {
	cfg := InitializeEmpty()
	require.False(t, cfg.GetSourceTrackingEnabled())
	cfg.SetConfigFile(filepath.Join(t.TempDir(), "config.yml"))
	cfg.setExistingSystemDefaults()
	require.False(t, cfg.GetSourceTrackingEnabled())
	cfg.SetBool(SourceTrackingEnabled, true)
	require.NoError(t, cfg.Write())
	reloaded := InitializeEmpty()
	require.NoError(t, reloaded.load(cfg.GetConfigFile()))
	require.True(t, reloaded.GetSourceTrackingEnabled())
	reloaded.SetBool(SourceTrackingEnabled, false)
	require.NoError(t, reloaded.Write())
	require.False(t, reloaded.GetSourceTrackingEnabled())
}

func TestSourceTrackingEnvironmentOptIn(t *testing.T) {
	for _, value := range []string{"true", "false"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("STASH_SOURCE_TRACKING_ENABLED", value)
			cfg := InitializeEmpty()
			cfg.loadFromEnv()
			require.True(t, cfg.HasOverride(SourceTrackingEnabled))
			require.Equal(t, value == "true", cfg.GetSourceTrackingEnabled())
		})
	}
}
