package manager

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/pkg/fsutil"
)

const nativeCheckpointReleaseFormat = "org.notsafeforgit.stash.server-checkpoint-release"

// NativeCheckpointReleaseInput binds an authorized publisher's release to the
// exact checkpoint and enclosing archive. The publisher must verify durable
// publication before calling: the application does not contact remote storage.
type NativeCheckpointReleaseInput struct {
	CheckpointSHA256      string `json:"checkpoint_sha256"`
	ArchiveUUID           string `json:"archive_uuid"`
	ArchiveManifestSHA256 string `json:"archive_manifest_sha256"`
}

type NativeCheckpointRelease struct {
	Format        string                       `json:"format"`
	Version       int                          `json:"version"`
	UUID          string                       `json:"uuid"`
	RequestSHA256 string                       `json:"request_sha256"`
	Archive       NativeCheckpointReleaseInput `json:"archive"`
	ReleasedAt    time.Time                    `json:"released_at"`
}

func checkpointDigest(manifest *NativeBackupCheckpoint) (string, error) {
	body, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:]), nil
}

func validCheckpointRelease(input NativeCheckpointReleaseInput) bool {
	id, err := uuid.Parse(input.ArchiveUUID)
	return err == nil && id.String() == input.ArchiveUUID &&
		checkpointSHA(input.CheckpointSHA256) && checkpointSHA(input.ArchiveManifestSHA256)
}

func publishCheckpointRelease(directory string, body []byte) error {
	return publishCheckpointRecord(directory, "release.json", body)
}

func publishCheckpointRecord(directory, name string, body []byte) error {
	f, err := os.CreateTemp(directory, ".record-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.Write(body); err != nil {
		_ = f.Close()
		return err
	}
	if err := errors.Join(f.Sync(), f.Close()); err != nil {
		return err
	}
	// An atomic hard link publishes without replacing another process's
	// record. Both names are private files on the same checkpoint volume.
	return os.Link(f.Name(), filepath.Join(directory, name))
}

func readNativeCheckpointRelease(directory string, manifest *NativeBackupCheckpoint) (*NativeCheckpointRelease, error) {
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Lstat("release.json")
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 4096 {
		return nil, ErrNativeCheckpointInvalid
	}
	f, err := root.Open("release.json")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil {
		return nil, err
	}
	var result NativeCheckpointRelease
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, ErrNativeCheckpointInvalid
	}
	canonical, err := json.Marshal(result)
	if err != nil || len(body) > 4096 || !bytes.Equal(body, canonical) ||
		result.Format != nativeCheckpointReleaseFormat || result.Version != 1 || result.UUID != manifest.UUID ||
		result.RequestSHA256 != manifest.RequestSHA256 || result.ReleasedAt.IsZero() || !validCheckpointRelease(result.Archive) {
		return nil, ErrNativeCheckpointInvalid
	}
	digest, err := checkpointDigest(manifest)
	if err != nil || digest != result.Archive.CheckpointSHA256 {
		return nil, ErrNativeCheckpointInvalid
	}
	return &result, nil
}

// ReadNativeCheckpointRelease returns the permanent archive binding. It does
// not assert that every temporary component has already been removed; POSTing
// the same release safely completes interrupted cleanup.
func (s *Manager) ReadNativeCheckpointRelease(id string) (*NativeCheckpointRelease, error) {
	directory, err := s.nativeCheckpointDirectory(id)
	if err != nil {
		return nil, err
	}
	manifest, err := readNativeCheckpointManifest(directory, id)
	if err != nil {
		return nil, err
	}
	return readNativeCheckpointRelease(directory, manifest)
}

// ReleaseNativeCheckpoint persists the archive binding before removing any
// temporary bytes. Preserve the directory, checkpoint manifest and release
// record permanently: deleting those would let a retry capture newer state.
func (s *Manager) ReleaseNativeCheckpoint(ctx context.Context, id string, input NativeCheckpointReleaseInput) (*NativeCheckpointRelease, error) {
	if !validCheckpointRelease(input) {
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
	manifest, err := readNativeCheckpointManifest(directory, id)
	if err != nil {
		return nil, err
	}
	digest, err := checkpointDigest(manifest)
	if err != nil || digest != input.CheckpointSHA256 {
		return nil, ErrNativeCheckpointInvalid
	}
	result, err := readNativeCheckpointRelease(directory, manifest)
	switch {
	case err == nil:
		if result.Archive != input {
			return nil, ErrNativeCheckpointInvalid
		}
	case errors.Is(err, os.ErrNotExist):
		result = &NativeCheckpointRelease{Format: nativeCheckpointReleaseFormat, Version: 1, UUID: id,
			RequestSHA256: manifest.RequestSHA256, Archive: input, ReleasedAt: time.Now().UTC()}
		body, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		if err := publishCheckpointRelease(directory, body); errors.Is(err, os.ErrExist) {
			result, err = readNativeCheckpointRelease(directory, manifest)
			if err != nil {
				return nil, err
			}
			if result.Archive != input {
				return nil, ErrNativeCheckpointInvalid
			}
		} else if err != nil {
			return nil, err
		}
	default:
		return nil, err
	}
	// Re-sync on retries too: a prior call may have published the record but
	// failed to make the directory entry durable before stopping.
	if err := fsutil.SyncDir(directory); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	for _, component := range manifest.Components {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		info, err := root.Lstat(component.Name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, ErrNativeCheckpointInvalid
		}
		if err := root.Remove(component.Name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	if err := fsutil.SyncDir(directory); err != nil {
		return nil, err
	}
	return result, nil
}
