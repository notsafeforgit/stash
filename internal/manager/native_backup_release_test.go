package manager

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func nativeReleaseFixture(t *testing.T) (*Manager, NativeCheckpointInput, *NativeBackupCheckpoint, NativeCheckpointReleaseInput, string) {
	t.Helper()
	m := nativeBackupTestManager(t)
	zero := int64(0)
	request := NativeCheckpointInput{UUID: uuid.NewString(), ReserveBytes: &zero}
	manifest, err := m.CaptureNativeCheckpoint(t.Context(), request)
	require.NoError(t, err)
	digest, err := checkpointDigest(manifest)
	require.NoError(t, err)
	directory, err := m.nativeCheckpointDirectory(request.UUID)
	require.NoError(t, err)
	return m, request, manifest, NativeCheckpointReleaseInput{CheckpointSHA256: digest,
		ArchiveUUID: uuid.NewString(), ArchiveManifestSHA256: strings.Repeat("a", 64)}, directory
}

func TestNativeBackupCheckpointReleaseRetainsIdentity(t *testing.T) {
	m, request, manifest, input, directory := nativeReleaseFixture(t)
	wrong := input
	wrong.CheckpointSHA256 = strings.Repeat("b", 64)
	_, err := m.ReleaseNativeCheckpoint(t.Context(), request.UUID, wrong)
	require.ErrorIs(t, err, ErrNativeCheckpointInvalid)
	require.NoFileExists(t, filepath.Join(directory, "release.json"))
	require.FileExists(t, filepath.Join(directory, "library.sqlite"))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = m.ReleaseNativeCheckpoint(ctx, request.UUID, input)
	require.ErrorIs(t, err, context.Canceled)
	require.NoFileExists(t, filepath.Join(directory, "release.json"))
	m.nativeBackupMu.Lock()
	_, err = m.ReleaseNativeCheckpoint(t.Context(), request.UUID, input)
	m.nativeBackupMu.Unlock()
	require.ErrorIs(t, err, ErrNativeCheckpointBusy)
	require.NoFileExists(t, filepath.Join(directory, "release.json"))

	// An unrelated file is never part of automatic component cleanup.
	require.NoError(t, os.WriteFile(filepath.Join(directory, "keep"), []byte("operator evidence"), 0600))
	receipt, err := m.ReleaseNativeCheckpoint(t.Context(), request.UUID, input)
	require.NoError(t, err)
	require.Equal(t, input, receipt.Archive)
	require.Equal(t, manifest.RequestSHA256, receipt.RequestSHA256)
	for _, component := range manifest.Components {
		require.NoFileExists(t, filepath.Join(directory, component.Name))
	}
	require.FileExists(t, filepath.Join(directory, "checkpoint.json"))
	require.FileExists(t, filepath.Join(directory, "keep"))
	info, err := os.Stat(filepath.Join(directory, "release.json"))
	require.NoError(t, err)
	require.Zero(t, info.Mode().Perm()&0077)
	_, err = m.CaptureNativeCheckpoint(t.Context(), request)
	require.ErrorIs(t, err, ErrNativeCheckpointReleased)
	_, err = m.ReadNativeCheckpoint(request.UUID)
	require.ErrorIs(t, err, ErrNativeCheckpointReleased)
	_, _, err = m.OpenNativeCheckpointComponent(request.UUID, "config.yml")
	require.ErrorIs(t, err, ErrNativeCheckpointReleased)

	// A new manager has no in-memory knowledge of the earlier release.
	restarted := &Manager{Config: m.Config, Database: m.Database}
	again, err := restarted.ReleaseNativeCheckpoint(t.Context(), request.UUID, input)
	require.NoError(t, err)
	require.Equal(t, receipt, again)
	read, err := restarted.ReadNativeCheckpointRelease(request.UUID)
	require.NoError(t, err)
	require.Equal(t, receipt, read)
	wrong = input
	wrong.ArchiveUUID = uuid.NewString()
	_, err = restarted.ReleaseNativeCheckpoint(t.Context(), request.UUID, wrong)
	require.ErrorIs(t, err, ErrNativeCheckpointInvalid)
}

func TestNativeBackupCheckpointReleaseResumesInterruptedCleanup(t *testing.T) {
	m, request, _, input, directory := nativeReleaseFixture(t)
	library := filepath.Join(directory, "library.sqlite")
	saved := filepath.Join(t.TempDir(), "library.sqlite")
	require.NoError(t, os.Rename(library, saved))
	require.NoError(t, os.Mkdir(library, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(library, "keep"), []byte("unexpected replacement"), 0600))
	_, err := m.ReleaseNativeCheckpoint(t.Context(), request.UUID, input)
	require.ErrorIs(t, err, ErrNativeCheckpointInvalid)
	receipt, err := m.ReadNativeCheckpointRelease(request.UUID)
	require.NoError(t, err)
	require.Equal(t, input, receipt.Archive)
	// Some components have gone, but permanent identity was recorded first.
	require.NoFileExists(t, filepath.Join(directory, "config.yml"))
	require.FileExists(t, filepath.Join(library, "keep"))
	_, err = m.CaptureNativeCheckpoint(t.Context(), request)
	require.ErrorIs(t, err, ErrNativeCheckpointReleased)
	require.NoError(t, os.Remove(filepath.Join(library, "keep")))
	require.NoError(t, os.Remove(library))
	require.NoError(t, os.Rename(saved, library))
	restarted := &Manager{Config: m.Config, Database: m.Database}
	again, err := restarted.ReleaseNativeCheckpoint(t.Context(), request.UUID, input)
	require.NoError(t, err)
	require.Equal(t, receipt, again)
	require.NoFileExists(t, library)
	// A damaged permanent record is never overwritten or used for recapture.
	require.NoError(t, os.WriteFile(filepath.Join(directory, "release.json"), []byte("{}"), 0600))
	_, err = restarted.ReleaseNativeCheckpoint(t.Context(), request.UUID, input)
	require.ErrorIs(t, err, ErrNativeCheckpointInvalid)
	_, err = restarted.CaptureNativeCheckpoint(t.Context(), request)
	require.ErrorIs(t, err, ErrNativeCheckpointInvalid)
}

func TestNativeBackupCheckpointReleaseNeverReplacesConcurrentBinding(t *testing.T) {
	m, request, _, input, _ := nativeReleaseFixture(t)
	inputs := []NativeCheckpointReleaseInput{input, input}
	inputs[1].ArchiveUUID = uuid.NewString()
	receipts := make([]*NativeCheckpointRelease, 2)
	errors := make([]error, 2)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for i := range inputs {
		workers.Go(func() {
			separate := &Manager{Config: m.Config, Database: m.Database}
			<-start
			receipts[i], errors[i] = separate.ReleaseNativeCheckpoint(t.Context(), request.UUID, inputs[i])
		})
	}
	close(start)
	workers.Wait()
	receipt, err := m.ReadNativeCheckpointRelease(request.UUID)
	require.NoError(t, err)
	winners := 0
	for i := range inputs {
		if errors[i] == nil {
			winners++
			require.Equal(t, receipt, receipts[i])
			require.Equal(t, inputs[i], receipt.Archive)
		} else {
			require.ErrorIs(t, errors[i], ErrNativeCheckpointInvalid)
		}
	}
	require.Equal(t, 1, winners)
}
