package manager

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/stashapp/stash/pkg/fsutil"
)

const nativeCheckpointAttemptFormat = "org.notsafeforgit.stash.server-checkpoint-attempt"
const nativeCheckpointAbandonFormat = "org.notsafeforgit.stash.server-checkpoint-abandonment"
const nativeCheckpointStatusFormat = "org.notsafeforgit.stash.server-checkpoint-status"

var ErrNativeCheckpointAbandoned = errors.New("the native checkpoint attempt was abandoned without publication")
var ErrNativeCheckpointSealed = errors.New("the native checkpoint is sealed; resume publication and release instead of abandoning it")

// Attempt records are private admission evidence, not backup certificates.
type nativeCheckpointAttempt struct {
	Format        string    `json:"format"`
	Version       int       `json:"version"`
	UUID          string    `json:"uuid"`
	RequestSHA256 string    `json:"request_sha256"`
	StartedAt     time.Time `json:"started_at"`
}

type NativeCheckpointAbandonInput struct {
	RequestSHA256 string `json:"request_sha256"`
}

// NativeCheckpointAbandonment fences late capture requests and permits cleanup
// of this unsealed attempt only. It never asserts publication or recoverability.
type NativeCheckpointAbandonment struct {
	Format        string    `json:"format"`
	Version       int       `json:"version"`
	UUID          string    `json:"uuid"`
	RequestSHA256 string    `json:"request_sha256"`
	AbandonedAt   time.Time `json:"abandoned_at"`
}

type NativeCheckpointStatus struct {
	Format           string `json:"format"`
	Version          int    `json:"version"`
	UUID             string `json:"uuid"`
	State            string `json:"state"`
	RequestSHA256    string `json:"request_sha256,omitempty"`
	CheckpointSHA256 string `json:"checkpoint_sha256,omitempty"`
}

func readCheckpointRecord(directory, name string, result interface{}) error {
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return ErrNativeCheckpointInvalid
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	info, err = root.Lstat(name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 4096 {
		return ErrNativeCheckpointInvalid
	}
	f, err := root.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	after, err := f.Stat()
	if err != nil || !os.SameFile(info, after) {
		return errors.Join(err, ErrNativeCheckpointInvalid)
	}
	body, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil {
		return err
	}
	if len(body) > 4096 || json.Unmarshal(body, result) != nil {
		return ErrNativeCheckpointInvalid
	}
	canonical, err := json.Marshal(result)
	if err != nil || !bytes.Equal(body, canonical) {
		return ErrNativeCheckpointInvalid
	}
	return nil
}

func readNativeCheckpointAttempt(directory, id string) (*nativeCheckpointAttempt, error) {
	var result nativeCheckpointAttempt
	if err := readCheckpointRecord(directory, "attempt.json", &result); err != nil {
		return nil, err
	}
	if result.Format != nativeCheckpointAttemptFormat || result.Version != 1 || result.UUID != id ||
		!checkpointSHA(result.RequestSHA256) || result.StartedAt.IsZero() {
		return nil, ErrNativeCheckpointInvalid
	}
	return &result, nil
}

func readNativeCheckpointAbandonment(directory, id string) (*NativeCheckpointAbandonment, error) {
	var result NativeCheckpointAbandonment
	if err := readCheckpointRecord(directory, "abandoned.json", &result); err != nil {
		return nil, err
	}
	if result.Format != nativeCheckpointAbandonFormat || result.Version != 1 || result.UUID != id ||
		!checkpointSHA(result.RequestSHA256) || result.AbandonedAt.IsZero() {
		return nil, ErrNativeCheckpointInvalid
	}
	return &result, nil
}

func syncCheckpointIdentity(directory string) error {
	for _, path := range []string{directory, filepath.Dir(directory), filepath.Dir(filepath.Dir(directory))} {
		if err := fsutil.SyncDir(path); err != nil {
			return err
		}
	}
	return nil
}

func startNativeCheckpointAttempt(directory, id, requestHash string) error {
	body, err := json.Marshal(nativeCheckpointAttempt{Format: nativeCheckpointAttemptFormat, Version: 1,
		UUID: id, RequestSHA256: requestHash, StartedAt: time.Now().UTC()})
	if err != nil {
		return err
	}
	if err := publishCheckpointRecord(directory, "attempt.json", body); err != nil {
		return err
	}
	// No database, filesystem boundary or component effects precede this sync.
	return syncCheckpointIdentity(directory)
}

// NativeCheckpointState serializes with capture/cleanup so an unsealed result
// cannot be mistaken for a stopped capture. Missing is an explicit state: the
// caller still needs an abandonment receipt to fence a delayed capture POST.
func (s *Manager) NativeCheckpointState(ctx context.Context, id string) (*NativeCheckpointStatus, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !s.nativeBackupMu.TryLock() {
		return nil, ErrNativeCheckpointBusy
	}
	defer s.nativeBackupMu.Unlock()
	directory, err := s.nativeCheckpointDirectory(id)
	if err != nil {
		return nil, err
	}
	result := &NativeCheckpointStatus{Format: nativeCheckpointStatusFormat, Version: 1, UUID: id, State: "missing"}
	if _, err := os.Lstat(directory); errors.Is(err, os.ErrNotExist) {
		return result, nil
	} else if err != nil {
		return nil, err
	}
	if record, err := readNativeCheckpointAbandonment(directory, id); err == nil {
		result.State, result.RequestSHA256 = "abandoned", record.RequestSHA256
		return result, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	manifest, err := readNativeCheckpointManifest(directory, id)
	if err == nil {
		result.State, result.RequestSHA256 = "sealed", manifest.RequestSHA256
		result.CheckpointSHA256, err = checkpointDigest(manifest)
		if err != nil {
			return nil, err
		}
		if _, err := readNativeCheckpointRelease(directory, manifest); err == nil {
			result.State = "released"
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		return result, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	attempt, err := readNativeCheckpointAttempt(directory, id)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNativeCheckpointIncomplete
	} else if err != nil {
		return nil, err
	}
	result.State, result.RequestSHA256 = "partial", attempt.RequestSHA256
	return result, nil
}

// AbandonNativeCheckpoint retires an unsealed or never-admitted attempt under
// the same exclusion as capture. Sealed checkpoints must follow archive-bound
// release. Retain the receipt permanently, even if cleanup later fails.
func (s *Manager) AbandonNativeCheckpoint(ctx context.Context, id string, input NativeCheckpointAbandonInput) (*NativeCheckpointAbandonment, error) {
	if !checkpointSHA(input.RequestSHA256) {
		return nil, ErrNativeCheckpointInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !s.nativeBackupMu.TryLock() {
		return nil, ErrNativeCheckpointBusy
	}
	defer s.nativeBackupMu.Unlock()
	directory, err := s.nativeCheckpointDirectory(id)
	if err != nil {
		return nil, err
	}
	if err := os.Mkdir(filepath.Dir(directory), 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	if err := os.Mkdir(directory, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	if _, err := readNativeCheckpointManifest(directory, id); err == nil {
		return nil, ErrNativeCheckpointSealed
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	attempt, err := readNativeCheckpointAttempt(directory, id)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if attempt != nil && attempt.RequestSHA256 != input.RequestSHA256 {
		return nil, ErrNativeCheckpointInvalid
	}
	record, err := readNativeCheckpointAbandonment(directory, id)
	if err == nil && record.RequestSHA256 != input.RequestSHA256 {
		return nil, ErrNativeCheckpointInvalid
	}
	if errors.Is(err, os.ErrNotExist) {
		if attempt == nil {
			entries, err := os.ReadDir(directory)
			if err != nil {
				return nil, err
			}
			if len(entries) != 0 {
				return nil, ErrNativeCheckpointIncomplete
			}
		}
		record = &NativeCheckpointAbandonment{Format: nativeCheckpointAbandonFormat, Version: 1,
			UUID: id, RequestSHA256: input.RequestSHA256, AbandonedAt: time.Now().UTC()}
		body, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		if err := publishCheckpointRecord(directory, "abandoned.json", body); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if err := syncCheckpointIdentity(directory); err != nil {
		return nil, err
	}
	if attempt == nil {
		return record, nil
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	// Fixed component names are the only files owned by an admitted attempt.
	// Unknown entries and temporary files remain for inspection; never recurse.
	for name := range nativeCheckpointRoles {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		info, err := root.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, ErrNativeCheckpointInvalid
		}
		if err := root.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	if err := fsutil.SyncDir(directory); err != nil {
		return nil, err
	}
	return record, nil
}
