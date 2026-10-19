package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpdatePublishesOnlyPersistedCandidate(t *testing.T) {
	t.Parallel()
	c := NewEmpty()
	c.SetConfigFile(filepath.Join(t.TempDir(), "config.yml"))
	c.SetString(Username, "before")
	require.NoError(t, c.Write())
	before, err := os.ReadFile(c.GetConfigFile())
	require.NoError(t, err)
	err = c.Update(t.Context(), func(next *Config) error {
		next.SetString(Username, "rejected")
		return errors.New("invalid setting")
	})
	require.ErrorContains(t, err, "invalid setting")
	require.Equal(t, "before", c.GetUsername())
	after, err := os.ReadFile(c.GetConfigFile())
	require.NoError(t, err)
	require.Equal(t, before, after)

	staged, commit := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- c.Update(t.Context(), func(next *Config) error {
			next.SetString(Username, "after")
			next.SetBool(SourceTrackingEnabled, true)
			close(staged)
			<-commit
			return nil
		})
	}()
	<-staged
	// A slow validator neither blocks readers nor exposes its partial candidate.
	snapshot := c.Snapshot()
	require.Equal(t, "before", snapshot.GetUsername())
	require.False(t, snapshot.GetSourceTrackingEnabled())
	close(commit)
	require.NoError(t, <-done)
	require.Equal(t, "after", c.GetUsername())
	require.True(t, c.GetSourceTrackingEnabled())
	reloaded := NewEmpty()
	require.NoError(t, reloaded.load(c.GetConfigFile()))
	require.Equal(t, "after", reloaded.GetUsername())
	require.True(t, reloaded.GetSourceTrackingEnabled())
	require.Equal(t, "before", snapshot.GetUsername())
}

func TestUpdateWriteFailureAndCancellationRetainMemory(t *testing.T) {
	t.Parallel()
	c := NewEmpty()
	c.SetConfigFile(filepath.Join(t.TempDir(), "missing", "config.yml"))
	c.SetString(Username, "before")
	err := c.Update(t.Context(), func(next *Config) error {
		next.SetString(Username, "not saved")
		return nil
	})
	require.Error(t, err)
	require.Equal(t, "before", c.GetUsername())
	ctx, cancel := context.WithCancel(t.Context())
	err = c.Update(ctx, func(next *Config) error {
		next.SetString(Username, "cancelled")
		cancel()
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, "before", c.GetUsername())
}

func TestUpdateConcurrentPatchesAndStartupWriter(t *testing.T) {
	t.Parallel()
	c := NewEmpty()
	c.SetConfigFile(filepath.Join(t.TempDir(), "config.yml"))
	require.NoError(t, c.overrides.Set(Port, 12345))
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for n := range 20 {
		wg.Go(func() {
			errs <- c.Update(t.Context(), func(next *Config) error {
				next.SetString(fmt.Sprintf("setting%d", n), "saved")
				return nil
			})
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	calls := 0
	require.NoError(t, c.Update(t.Context(), func(next *Config) error {
		calls++
		if calls == 1 {
			c.SetString(Username, "concurrent startup writer")
		}
		next.SetString(ApiKey, next.GetUsername())
		return nil
	}))
	require.Equal(t, 2, calls)
	require.Equal(t, "concurrent startup writer", c.GetAPIKey())
	require.Equal(t, 12345, c.GetPort())
	reloaded := NewEmpty()
	require.NoError(t, reloaded.load(c.GetConfigFile()))
	for n := range 20 {
		require.Equal(t, "saved", reloaded.getString(fmt.Sprintf("setting%d", n)))
	}
	require.False(t, reloaded.HasOverride(Port))
	require.False(t, reloaded.main.Exists(Port), "environment overrides must not leak into YAML")
}

func TestCustomizationPublicationAndStandaloneFileFallback(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		get  func(*Config) string
		set  func(*Config, string)
	}{
		{"custom.css", (*Config).GetCSS, (*Config).SetCSS},
		{"custom.js", (*Config).GetJavascript, (*Config).SetJavascript},
		{"custom-locales.json", (*Config).GetCustomLocales, (*Config).SetCustomLocales},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := NewEmpty()
			dir := t.TempDir()
			c.SetConfigFile(filepath.Join(dir, "config.yml"))
			path := filepath.Join(dir, tt.name)
			require.NoError(t, os.WriteFile(path, []byte("standalone"), 0600))
			require.Equal(t, "standalone", tt.get(c))
			require.Error(t, c.Update(t.Context(), func(next *Config) error {
				tt.set(next, "rejected")
				return errors.New("rejected")
			}))
			require.Equal(t, "standalone", tt.get(c))
			for _, value := range []string{"saved", ""} {
				require.NoError(t, c.Update(t.Context(), func(next *Config) error {
					tt.set(next, value)
					return nil
				}))
				reloaded := NewEmpty()
				require.NoError(t, reloaded.load(c.GetConfigFile()))
				require.Equal(t, value, tt.get(reloaded))
			}
			standalone, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, "standalone", string(standalone))
		})
	}
}
