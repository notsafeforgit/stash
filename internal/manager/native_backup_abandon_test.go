package manager

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestNativeBackupCheckpointAbandonFencesMissingAndPartial(t *testing.T) {
	m := nativeBackupTestManager(t)
	for _, partial := range []bool{false, true} {
		name := "missing"
		if partial {
			name = "partial"
		}
		t.Run(name, func(t *testing.T) {
			huge := int64(math.MaxInt64)
			request := NativeCheckpointInput{UUID: uuid.NewString(), ReserveBytes: &huge}
			hash, _, err := nativeCheckpointRequest(request)
			require.NoError(t, err)
			directory, err := m.nativeCheckpointDirectory(request.UUID)
			require.NoError(t, err)
			if partial {
				_, err = m.CaptureNativeCheckpoint(t.Context(), request)
				require.ErrorContains(t, err, "disk reserve")
				require.NoError(t, os.WriteFile(filepath.Join(directory, "config.yml"), []byte("partial bytes"), 0600))
				require.NoError(t, os.WriteFile(filepath.Join(directory, "keep"), []byte("unknown"), 0600))
			}
			state, err := m.NativeCheckpointState(t.Context(), request.UUID)
			require.NoError(t, err)
			require.Equal(t, name, state.State)
			input := NativeCheckpointAbandonInput{RequestSHA256: hash}
			if partial {
				_, err := m.AbandonNativeCheckpoint(t.Context(), request.UUID, NativeCheckpointAbandonInput{RequestSHA256: strings.Repeat("a", 64)})
				require.ErrorIs(t, err, ErrNativeCheckpointInvalid)
				require.NoFileExists(t, filepath.Join(directory, "abandoned.json"))
			}
			receipt, err := m.AbandonNativeCheckpoint(t.Context(), request.UUID, input)
			require.NoError(t, err)
			require.Equal(t, hash, receipt.RequestSHA256)
			require.NoFileExists(t, filepath.Join(directory, "config.yml"))
			if partial {
				require.FileExists(t, filepath.Join(directory, "attempt.json"))
				require.FileExists(t, filepath.Join(directory, "keep"))
			}
			restarted := &Manager{Config: m.Config, Database: m.Database}
			again, err := restarted.AbandonNativeCheckpoint(t.Context(), request.UUID, input)
			require.NoError(t, err)
			require.Equal(t, receipt, again)
			state, err = restarted.NativeCheckpointState(t.Context(), request.UUID)
			require.NoError(t, err)
			require.Equal(t, "abandoned", state.State)
			require.Equal(t, hash, state.RequestSHA256)
			_, err = restarted.CaptureNativeCheckpoint(t.Context(), request)
			require.ErrorIs(t, err, ErrNativeCheckpointAbandoned)
			_, err = restarted.ReadNativeCheckpoint(request.UUID)
			require.ErrorIs(t, err, ErrNativeCheckpointAbandoned)
			_, _, err = restarted.OpenNativeCheckpointComponent(request.UUID, "config.yml")
			require.ErrorIs(t, err, ErrNativeCheckpointAbandoned)
			_, err = restarted.ReadNativeCheckpointRelease(request.UUID)
			require.ErrorIs(t, err, os.ErrNotExist)
			_, err = restarted.AbandonNativeCheckpoint(t.Context(), request.UUID, NativeCheckpointAbandonInput{RequestSHA256: strings.Repeat("b", 64)})
			require.ErrorIs(t, err, ErrNativeCheckpointInvalid)
		})
	}
}

func TestNativeBackupCheckpointAbandonRejectsActiveSealedAndUnknown(t *testing.T) {
	m := nativeBackupTestManager(t)
	zero := int64(0)
	request := NativeCheckpointInput{UUID: uuid.NewString(), ReserveBytes: &zero,
		ExternalBoundary: &NativeCheckpointBoundaryInput{TimeoutSeconds: 5}}
	hash, _, err := nativeCheckpointRequest(request)
	require.NoError(t, err)
	input := NativeCheckpointAbandonInput{RequestSHA256: hash}
	_, err = m.CaptureNativeCheckpointWithBoundary(t.Context(), request, func(ready NativeCheckpointBoundaryReady) error {
		_, err := m.AbandonNativeCheckpoint(t.Context(), request.UUID, input)
		require.ErrorIs(t, err, ErrNativeCheckpointBusy)
		_, err = m.NativeCheckpointState(t.Context(), request.UUID)
		require.ErrorIs(t, err, ErrNativeCheckpointBusy)
		_, err = m.ConfirmNativeCheckpointBoundary(t.Context(), request.UUID,
			NativeCheckpointBoundaryConfirmation{Token: ready.Token, Details: []byte(`{"view":"retained"}`)})
		return err
	})
	require.NoError(t, err)
	state, err := m.NativeCheckpointState(t.Context(), request.UUID)
	require.NoError(t, err)
	require.Equal(t, "sealed", state.State)
	require.True(t, checkpointSHA(state.CheckpointSHA256))
	_, err = m.AbandonNativeCheckpoint(t.Context(), request.UUID, input)
	require.ErrorIs(t, err, ErrNativeCheckpointSealed)
	_, err = m.ReadNativeCheckpoint(request.UUID)
	require.NoError(t, err)
	_, err = m.ReleaseNativeCheckpoint(t.Context(), request.UUID, NativeCheckpointReleaseInput{
		CheckpointSHA256: state.CheckpointSHA256, ArchiveUUID: uuid.NewString(), ArchiveManifestSHA256: strings.Repeat("c", 64)})
	require.NoError(t, err)
	state, err = m.NativeCheckpointState(t.Context(), request.UUID)
	require.NoError(t, err)
	require.Equal(t, "released", state.State)
	_, err = m.AbandonNativeCheckpoint(t.Context(), request.UUID, input)
	require.ErrorIs(t, err, ErrNativeCheckpointSealed)

	id := uuid.NewString()
	directory, err := m.nativeCheckpointDirectory(id)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = m.AbandonNativeCheckpoint(ctx, id, input)
	require.ErrorIs(t, err, context.Canceled)
	require.NoDirExists(t, directory)
	require.NoError(t, os.Mkdir(directory, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "config.yml"), []byte("unowned"), 0600))
	_, err = m.AbandonNativeCheckpoint(t.Context(), id, input)
	require.ErrorIs(t, err, ErrNativeCheckpointIncomplete)
	require.NoFileExists(t, filepath.Join(directory, "abandoned.json"))
	require.FileExists(t, filepath.Join(directory, "config.yml"))
}

func TestNativeBackupCheckpointAbandonResumesAfterUnsafeComponent(t *testing.T) {
	m := nativeBackupTestManager(t)
	huge := int64(math.MaxInt64)
	request := NativeCheckpointInput{UUID: uuid.NewString(), ReserveBytes: &huge}
	_, err := m.CaptureNativeCheckpoint(t.Context(), request)
	require.Error(t, err)
	directory, err := m.nativeCheckpointDirectory(request.UUID)
	require.NoError(t, err)
	foreign := filepath.Join(t.TempDir(), "foreign")
	require.NoError(t, os.WriteFile(foreign, []byte("preserve"), 0600))
	require.NoError(t, os.Symlink(foreign, filepath.Join(directory, "config.yml")))
	hash, _, err := nativeCheckpointRequest(request)
	require.NoError(t, err)
	input := NativeCheckpointAbandonInput{RequestSHA256: hash}
	_, err = m.AbandonNativeCheckpoint(t.Context(), request.UUID, input)
	require.ErrorIs(t, err, ErrNativeCheckpointInvalid)
	receipt, err := readNativeCheckpointAbandonment(directory, request.UUID)
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(directory, "config.yml")))
	restarted := &Manager{Config: m.Config, Database: m.Database}
	again, err := restarted.AbandonNativeCheckpoint(t.Context(), request.UUID, input)
	require.NoError(t, err)
	require.Equal(t, receipt, again)
	body, err := os.ReadFile(foreign)
	require.NoError(t, err)
	require.Equal(t, "preserve", string(body))
}
