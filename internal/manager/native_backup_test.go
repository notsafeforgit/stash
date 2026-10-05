package manager

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/file"
	"github.com/stashapp/stash/pkg/sqlite"
	"github.com/stretchr/testify/require"
)

func nativeBackupTestManager(t *testing.T) *Manager {
	t.Helper()
	m := defaultFilterTestManager(t, "")
	m.Config.SetString(config.Database, m.Database.DatabasePath())
	m.Config.SetString(config.BackupDirectoryPath, t.TempDir())
	m.Config.SetString(config.ApiKey, "private-backup-fixture")
	return m
}

func TestNativeBackupCheckpointCapturesAndReplays(t *testing.T) {
	m := nativeBackupTestManager(t)
	zero := int64(0)
	request := NativeCheckpointInput{UUID: uuid.NewString(), ReserveBytes: &zero}
	result, err := m.CaptureNativeCheckpoint(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, nativeCheckpointCoverage, result.Coverage)
	require.Len(t, result.Components, 4)
	body, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(body), "private-backup-fixture")
	directory, err := m.nativeCheckpointDirectory(request.UUID)
	require.NoError(t, err)
	info, err := os.Stat(directory)
	require.NoError(t, err)
	require.Zero(t, info.Mode().Perm()&0077)
	for _, component := range result.Components {
		f, descriptor, err := m.OpenNativeCheckpointComponent(request.UUID, component.Name)
		require.NoError(t, err)
		require.Equal(t, component, *descriptor)
		if component.Name == "config.yml" {
			data, err := io.ReadAll(f)
			require.NoError(t, err)
			require.Contains(t, string(data), "private-backup-fixture")
		}
		require.NoError(t, f.Close())
		info, err := os.Stat(filepath.Join(directory, component.Name))
		require.NoError(t, err)
		require.Zero(t, info.Mode().Perm()&0077)
	}
	verified, err := sqlite.VerifyNativeSnapshot(t.Context(), filepath.Join(directory, "library.sqlite"))
	require.NoError(t, err)
	require.Zero(t, verified.PendingFileDeletions)
	_, err = file.RestoreDeletionSnapshot(t.Context(), filepath.Join(directory, "deletions.zip"), filepath.Join(t.TempDir(), "recovery"), result.CommittedDeletionIDs, nil)
	require.NoError(t, err)
	// A retry reuses the sealed source even if the live configuration changes.
	m.Config.SetString(config.ApiKey, "new-live-value")
	again, err := m.CaptureNativeCheckpoint(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, result, again)
	request.RecoveryRoots = []NativeCheckpointRoot{{Name: "changed", Path: t.TempDir()}}
	_, err = m.CaptureNativeCheckpoint(t.Context(), request)
	require.ErrorIs(t, err, ErrNativeCheckpointInvalid)
	request.RecoveryRoots = nil
	configPath := filepath.Join(directory, "config.yml")
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	data[0] ^= 1
	require.NoError(t, os.WriteFile(configPath, data, 0600))
	_, err = m.CaptureNativeCheckpoint(t.Context(), request)
	require.ErrorContains(t, err, "digest")
	_, _, err = m.OpenNativeCheckpointComponent(request.UUID, "../config.yml")
	require.ErrorIs(t, err, ErrNativeCheckpointInvalid)
}

func TestNativeBackupCheckpointFailureAndAdmission(t *testing.T) {
	m := nativeBackupTestManager(t)
	zero, huge := int64(0), int64(math.MaxInt64)
	for _, mode := range []string{"reserve", "cancel", "busy", "incomplete", "invalid-root"} {
		t.Run(mode, func(t *testing.T) {
			request := NativeCheckpointInput{UUID: uuid.NewString(), ReserveBytes: &zero}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			directory, err := m.nativeCheckpointDirectory(request.UUID)
			require.NoError(t, err)
			switch mode {
			case "reserve":
				request.ReserveBytes = &huge
			case "cancel":
				cancel()
			case "busy":
				m.nativeBackupMu.Lock()
				defer m.nativeBackupMu.Unlock()
			case "incomplete":
				require.NoError(t, os.MkdirAll(directory, 0700))
				require.NoError(t, os.WriteFile(filepath.Join(directory, "keep"), []byte("partial evidence"), 0600))
			case "invalid-root":
				request.RecoveryRoots = []NativeCheckpointRoot{{Name: "root", Path: string(filepath.Separator)}}
			}
			_, err = m.CaptureNativeCheckpoint(ctx, request)
			require.Error(t, err)
			if mode == "busy" {
				require.ErrorIs(t, err, ErrNativeCheckpointBusy)
			}
			switch mode {
			case "incomplete":
				require.ErrorIs(t, err, ErrNativeCheckpointIncomplete)
				require.FileExists(t, filepath.Join(directory, "keep"))
			case "cancel", "busy":
				require.NoDirExists(t, directory)
			default:
				require.FileExists(t, filepath.Join(directory, "attempt.json"))
				_, err = m.CaptureNativeCheckpoint(t.Context(), request)
				require.ErrorIs(t, err, ErrNativeCheckpointIncomplete)
			}
		})
	}
}

func TestNativeBackupCheckpointRejectsRedirectedCache(t *testing.T) {
	m := nativeBackupTestManager(t)
	zero := int64(0)
	request := NativeCheckpointInput{UUID: uuid.NewString(), ReserveBytes: &zero}
	_, err := m.CaptureNativeCheckpoint(t.Context(), request)
	require.NoError(t, err)
	directory, err := m.nativeCheckpointDirectory(request.UUID)
	require.NoError(t, err)
	parent := filepath.Dir(directory)
	moved := filepath.Join(t.TempDir(), "moved-cache")
	require.NoError(t, os.Rename(parent, moved))
	require.NoError(t, os.Symlink(moved, parent))
	_, err = m.ReadNativeCheckpoint(request.UUID)
	require.ErrorIs(t, err, ErrNativeCheckpointInvalid)
	_, _, err = m.OpenNativeCheckpointComponent(request.UUID, "config.yml")
	require.ErrorIs(t, err, ErrNativeCheckpointInvalid)
	_, err = m.CaptureNativeCheckpoint(t.Context(), request)
	require.ErrorIs(t, err, ErrNativeCheckpointInvalid)
	require.FileExists(t, filepath.Join(moved, request.UUID, "config.yml"))
}
