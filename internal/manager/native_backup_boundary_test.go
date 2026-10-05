package manager

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func nativeBoundaryWriter(t *testing.T, m *Manager) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(m.Database.DatabasePath())+"?mode=rw&_busy_timeout=50")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	return db
}

func TestNativeBackupCheckpointBoundaryBoundsCanonicalEvidence(t *testing.T) {
	input := NativeCheckpointBoundaryConfirmation{Token: uuid.NewString(),
		Details: json.RawMessage(`{"value":"` + strings.Repeat("&", 12000) + `"}`)}
	require.Less(t, len(input.Details), nativeBoundaryLimit)
	_, err := boundaryDetails(input)
	require.ErrorIs(t, err, ErrNativeCheckpointInvalid)
}

func TestNativeBackupCheckpointExternalBoundaryExcludesWritesAndReplays(t *testing.T) {
	m := nativeBackupTestManager(t)
	writer := nativeBoundaryWriter(t, m)
	zero := int64(0)
	request := NativeCheckpointInput{UUID: uuid.NewString(), ReserveBytes: &zero,
		ExternalBoundary: &NativeCheckpointBoundaryInput{TimeoutSeconds: 5}}
	var confirmation NativeCheckpointBoundaryConfirmation
	var accepted *NativeCheckpointBoundaryRecord
	manifest, err := m.CaptureNativeCheckpointWithBoundary(t.Context(), request, func(ready NativeCheckpointBoundaryReady) error {
		require.Equal(t, request.UUID, ready.UUID)
		require.NotEmpty(t, ready.Token)
		_, err := writer.Exec("BEGIN IMMEDIATE")
		require.ErrorContains(t, err, "locked")
		confirmation = NativeCheckpointBoundaryConfirmation{Token: ready.Token,
			Details: json.RawMessage(`{"snapshot":{"guid":18446744073709551615,"name":"isolated-test-view"}}`)}
		wrong := confirmation
		wrong.Token = uuid.NewString()
		_, err = m.ConfirmNativeCheckpointBoundary(t.Context(), request.UUID, wrong)
		require.ErrorIs(t, err, ErrNativeCheckpointInvalid)
		accepted, err = m.ConfirmNativeCheckpointBoundary(t.Context(), request.UUID, confirmation)
		require.NoError(t, err)
		require.Contains(t, string(accepted.Details), "18446744073709551615")
		require.Equal(t, ready.RequestSHA256, accepted.RequestSHA256)
		again, err := m.ConfirmNativeCheckpointBoundary(t.Context(), request.UUID, confirmation)
		require.NoError(t, err)
		require.Equal(t, accepted, again)
		// A caller cannot mutate the pending receipt through the returned slice.
		again.Details[0] = '!'
		wrong = confirmation
		wrong.Details = json.RawMessage(`{"snapshot":"changed"}`)
		_, err = m.ConfirmNativeCheckpointBoundary(t.Context(), request.UUID, wrong)
		require.ErrorIs(t, err, ErrNativeCheckpointInvalid)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, manifest.Components, 5)
	f, _, err := m.OpenNativeCheckpointComponent(request.UUID, "filesystem-boundary.json")
	require.NoError(t, err)
	body, err := io.ReadAll(f)
	require.NoError(t, err)
	require.NoError(t, f.Close())
	var stored NativeCheckpointBoundaryRecord
	require.NoError(t, json.Unmarshal(body, &stored))
	require.Equal(t, accepted, &stored)
	// Sealed retries recover their original receipt without requesting another
	// filesystem view, including after a fresh manager has lost pending state.
	restarted := &Manager{Config: m.Config, Database: m.Database}
	again, err := restarted.ConfirmNativeCheckpointBoundary(t.Context(), request.UUID, confirmation)
	require.NoError(t, err)
	require.Equal(t, accepted, again)
	replayed, err := restarted.CaptureNativeCheckpointWithBoundary(t.Context(), request, func(NativeCheckpointBoundaryReady) error {
		t.Fatal("sealed retry attempted another filesystem capture")
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, manifest, replayed)
	_, err = writer.Exec("BEGIN IMMEDIATE; ROLLBACK")
	require.NoError(t, err)
	directory, err := m.nativeCheckpointDirectory(request.UUID)
	require.NoError(t, err)
	changed := strings.ReplaceAll(string(body), "18446744073709551615", "18446744073709551614")
	require.NoError(t, os.WriteFile(filepath.Join(directory, "filesystem-boundary.json"), []byte(changed), 0600))
	confirmation.Details = json.RawMessage(strings.ReplaceAll(string(confirmation.Details), "18446744073709551615", "18446744073709551614"))
	_, err = restarted.ConfirmNativeCheckpointBoundary(t.Context(), request.UUID, confirmation)
	require.ErrorIs(t, err, ErrNativeCheckpointInvalid)
}

func TestNativeBackupCheckpointExternalBoundaryFailureReleasesGuard(t *testing.T) {
	m := nativeBackupTestManager(t)
	writer := nativeBoundaryWriter(t, m)
	zero := int64(0)
	for _, mode := range []string{"timeout", "cancel", "notification"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			request := NativeCheckpointInput{UUID: uuid.NewString(), ReserveBytes: &zero,
				ExternalBoundary: &NativeCheckpointBoundaryInput{TimeoutSeconds: 1}}
			var token string
			_, err := m.CaptureNativeCheckpointWithBoundary(ctx, request, func(ready NativeCheckpointBoundaryReady) error {
				token = ready.Token
				if mode == "cancel" {
					cancel()
				}
				if mode == "notification" {
					return errors.New("client connection failed")
				}
				return nil
			})
			require.Error(t, err)
			if mode == "timeout" {
				require.ErrorIs(t, err, context.DeadlineExceeded)
			}
			directory, err := m.nativeCheckpointDirectory(request.UUID)
			require.NoError(t, err)
			require.FileExists(t, filepath.Join(directory, "attempt.json"))
			_, err = writer.Exec("BEGIN IMMEDIATE; ROLLBACK")
			require.NoError(t, err)
			// A failed request permanently reserves its UUID. A new capture must
			// use a fresh identity, and cannot acknowledge the older challenge.
			_, err = m.CaptureNativeCheckpointWithBoundary(t.Context(), request, func(NativeCheckpointBoundaryReady) error {
				t.Fatal("failed attempt tried to capture newer state")
				return nil
			})
			require.ErrorIs(t, err, ErrNativeCheckpointIncomplete)
			request.UUID = uuid.NewString()
			_, err = m.CaptureNativeCheckpointWithBoundary(t.Context(), request, func(ready NativeCheckpointBoundaryReady) error {
				require.NotEqual(t, token, ready.Token)
				confirmation := NativeCheckpointBoundaryConfirmation{Token: token, Details: json.RawMessage(`{"snapshot":"old"}`)}
				_, err := m.ConfirmNativeCheckpointBoundary(t.Context(), request.UUID, confirmation)
				require.ErrorIs(t, err, ErrNativeCheckpointInvalid)
				confirmation.Token = ready.Token
				confirmation.Details = json.RawMessage(`{"snapshot":"new"}`)
				_, err = m.ConfirmNativeCheckpointBoundary(t.Context(), request.UUID, confirmation)
				return err
			})
			require.NoError(t, err)
		})
	}
}
