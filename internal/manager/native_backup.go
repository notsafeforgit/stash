package manager

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/file"
	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/sqlite"
)

const nativeCheckpointFormat = "org.notsafeforgit.stash.server-checkpoint"
const nativeCheckpointCoverage = "database-configuration-deletion-recovery"
const NativeCheckpointDefaultReserve int64 = 50 << 30

var ErrNativeCheckpointBusy = errors.New("a native checkpoint is already running")
var ErrNativeCheckpointInvalid = errors.New("invalid native checkpoint request")
var ErrNativeCheckpointIncomplete = errors.New("an incomplete native checkpoint already exists; abandon this attempt before using a new UUID")
var ErrNativeCheckpointReleased = errors.New("the native checkpoint has been released to its enclosing archive")

type NativeCheckpointRoot struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type NativeCheckpointInput struct {
	UUID             string                         `json:"uuid"`
	RecoveryRoots    []NativeCheckpointRoot         `json:"recovery_roots"`
	ReserveBytes     *int64                         `json:"reserve_bytes,omitempty"`
	ExternalBoundary *NativeCheckpointBoundaryInput `json:"external_boundary,omitempty"`
}

type NativeCheckpointComponent struct {
	Role       string `json:"role"`
	Name       string `json:"name"`
	Bytes      int64  `json:"bytes"`
	SHA256     string `json:"sha256"`
	SourcePath []byte `json:"source_path,omitempty"`
}

// NativeBackupCheckpoint describes sealed server components. It deliberately
// excludes configuration values, API keys and other private file contents.
// Original artwork/media and all producer state must still be coordinated by
// the enclosing archive publisher; this is not a complete archive certificate.
type NativeBackupCheckpoint struct {
	Format                 string                      `json:"format"`
	Version                int                         `json:"version"`
	UUID                   string                      `json:"uuid"`
	Coverage               string                      `json:"coverage"`
	CreatedAt              time.Time                   `json:"created_at"`
	RequestSHA256          string                      `json:"request_sha256"`
	SourceDatabasePath     []byte                      `json:"source_database_path"`
	SourceConfigPath       []byte                      `json:"source_config_path"`
	SourceWorkingDirectory []byte                      `json:"source_working_directory"`
	CommittedDeletionIDs   []string                    `json:"committed_deletion_ids"`
	Components             []NativeCheckpointComponent `json:"components"`
}

func nativeCheckpointRequest(input NativeCheckpointInput) (string, int64, error) {
	id, err := uuid.Parse(input.UUID)
	if err != nil || id.String() != input.UUID || len(input.RecoveryRoots) > 1024 {
		return "", 0, ErrNativeCheckpointInvalid
	}
	reserve := NativeCheckpointDefaultReserve
	if input.ReserveBytes != nil {
		reserve = *input.ReserveBytes
	}
	if reserve < 0 {
		return "", 0, ErrNativeCheckpointInvalid
	}
	if input.ExternalBoundary != nil && (input.ExternalBoundary.TimeoutSeconds < 1 || input.ExternalBoundary.TimeoutSeconds > 120) {
		return "", 0, ErrNativeCheckpointInvalid
	}
	// Semantically identical omitted/default reserve values have one identity.
	input.ReserveBytes = &reserve
	body, err := json.Marshal(input)
	if err != nil {
		return "", 0, err
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:]), reserve, nil
}

func (s *Manager) nativeCheckpointDirectory(id string) (string, error) {
	parsed, err := uuid.Parse(id)
	if err != nil || parsed.String() != id {
		return "", ErrNativeCheckpointInvalid
	}
	base, err := filepath.Abs(s.Config.GetBackupDirectoryPathOrDefault())
	if err != nil {
		return "", err
	}
	base, err = filepath.EvalSymlinks(base)
	if err != nil {
		return "", err
	}
	parent := filepath.Join(base, "native-checkpoints")
	info, err := os.Lstat(parent)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err == nil && (!info.IsDir() || info.Mode().Perm()&0077 != 0) {
		return "", ErrNativeCheckpointInvalid
	}
	return filepath.Join(parent, id), nil
}

// CaptureNativeCheckpoint is the application-side checkpoint coordinator. It
// acquires database exclusion before reading configuration, captures recovery
// trees, then copies the fixed WAL view with ordinary writers released. The
// manifest is published last. An attempt record permanently reserves the UUID,
// including after failure; explicit abandonment retires its temporary output.
func (s *Manager) CaptureNativeCheckpoint(ctx context.Context, input NativeCheckpointInput) (_ *NativeBackupCheckpoint, retErr error) {
	return s.captureNativeCheckpoint(ctx, input, nil)
}

// CaptureNativeCheckpointWithBoundary lets the authorized coordinator capture
// an external filesystem view while native writes are excluded. The callback
// publishes a bounded, one-use challenge, never an arbitrary server command.
func (s *Manager) CaptureNativeCheckpointWithBoundary(ctx context.Context, input NativeCheckpointInput, ready func(NativeCheckpointBoundaryReady) error) (*NativeBackupCheckpoint, error) {
	return s.captureNativeCheckpoint(ctx, input, ready)
}

func (s *Manager) captureNativeCheckpoint(ctx context.Context, input NativeCheckpointInput, ready func(NativeCheckpointBoundaryReady) error) (_ *NativeBackupCheckpoint, retErr error) {
	if input.ExternalBoundary != nil && ready == nil {
		return nil, ErrNativeCheckpointInvalid
	}
	requestHash, reserve, err := nativeCheckpointRequest(input)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !s.nativeBackupMu.TryLock() {
		return nil, ErrNativeCheckpointBusy
	}
	defer s.nativeBackupMu.Unlock()
	defer s.clearNativeCheckpointBoundary(input.UUID)
	directory, err := s.nativeCheckpointDirectory(input.UUID)
	if err != nil {
		return nil, err
	}
	if existing, err := readNativeCheckpoint(directory, input.UUID); err == nil {
		if existing.RequestSHA256 != requestHash {
			return nil, ErrNativeCheckpointInvalid
		}
		for _, component := range existing.Components {
			verified, err := nativeCheckpointFile(ctx, filepath.Join(directory, component.Name), component.Role, component.Name)
			if err != nil || verified.SHA256 != component.SHA256 || verified.Bytes != component.Bytes {
				return nil, errors.Join(err, errors.New("sealed checkpoint component no longer matches its digest"))
			}
		}
		return existing, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	parent := filepath.Dir(directory)
	if err := os.Mkdir(parent, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	info, err := os.Lstat(parent)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.Join(err, errors.New("native checkpoint directory must be private and cannot be a symlink"))
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, ErrNativeCheckpointIncomplete
		}
		return nil, err
	}
	if err := startNativeCheckpointAttempt(directory, input.UUID, requestHash); err != nil {
		return nil, err
	}
	checkSpace := func(remaining int64) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		available, err := fsutil.AvailableBytes(directory)
		if err != nil {
			return err
		}
		if available < reserve || remaining > available-reserve {
			return errors.New("native checkpoint would cross the configured disk reserve")
		}
		// A pinned reader retains newer WAL pages until the database copy ends.
		// The live database can be on a different volume from checkpoint output.
		available, err = fsutil.AvailableBytes(filepath.Dir(s.Database.DatabasePath()))
		if err != nil {
			return err
		}
		if available < reserve {
			return errors.New("native checkpoint source volume crossed the configured disk reserve")
		}
		return nil
	}
	if err := checkSpace(0); err != nil {
		return nil, err
	}
	result := &NativeBackupCheckpoint{Format: nativeCheckpointFormat, Version: 1, UUID: input.UUID,
		Coverage: nativeCheckpointCoverage, RequestSHA256: requestHash, SourceDatabasePath: []byte(s.Database.DatabasePath())}
	working, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	result.SourceWorkingDirectory = []byte(working)
	roots := make([]file.DeletionSnapshotRoot, 0, len(input.RecoveryRoots))
	for _, root := range input.RecoveryRoots {
		roots = append(roots, file.DeletionSnapshotRoot{Name: root.Name, Path: root.Path})
	}
	add := func(role, name string) error {
		component, err := nativeCheckpointFile(ctx, filepath.Join(directory, name), role, name)
		if err == nil {
			result.Components = append(result.Components, *component)
		}
		return err
	}
	err = sqlite.CaptureNativeSnapshot(ctx, s.Database.DatabasePath(), filepath.Join(directory, "library.sqlite"), checkSpace, func(c *sqlite.NativeCheckpoint) error {
		// Database exclusion precedes the Config read lock. Keep settings fixed
		// through small asset capture, then release Config before recovery/DB IO.
		err := s.Config.WithBackupSnapshot(func(configuration *config.BackupSnapshot) error {
			result.CreatedAt = time.Now().UTC()
			result.SourceConfigPath = []byte(configuration.ConfigPath)
			result.CommittedDeletionIDs = c.CommittedDeletionIDs()
			for _, component := range []struct {
				name string
				data []byte
			}{{"config.yml", configuration.MainYAML}, {"runtime-overrides.yml", configuration.OverridesYAML}} {
				if err := checkSpace(int64(len(component.data))); err != nil {
					return err
				}
				if err := fsutil.WriteFileAtomic(filepath.Join(directory, component.name), component.data, 0600); err != nil {
					return err
				}
				if err := add("config", component.name); err != nil {
					return err
				}
			}
			for _, asset := range []struct{ name, path string }{{"tls.crt", configuration.TLSCertPath}, {"tls.key", configuration.TLSKeyPath}} {
				if asset.path == "" {
					continue
				}
				if err := copyNativeCheckpointAsset(ctx, asset.path, filepath.Join(directory, asset.name), checkSpace); err != nil {
					return err
				}
				if err := add("config", asset.name); err != nil {
					return err
				}
				result.Components[len(result.Components)-1].SourcePath = []byte(asset.path)
			}
			for _, component := range result.Components {
				if len(component.SourcePath) == 0 {
					continue
				}
				resolved, err := filepath.EvalSymlinks(string(component.SourcePath))
				if err != nil {
					return err
				}
				verified, err := nativeCheckpointFile(ctx, resolved, component.Role, component.Name)
				if err != nil || verified.SHA256 != component.SHA256 || verified.Bytes != component.Bytes {
					return errors.Join(err, errors.New("configuration asset changed during checkpoint capture"))
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		if err := c.CopyDeletionSnapshot(filepath.Join(directory, "deletions.zip"), roots, checkSpace); err != nil {
			return err
		}
		if input.ExternalBoundary != nil {
			record, err := s.awaitNativeCheckpointBoundary(ctx, input, requestHash, ready)
			if err != nil {
				return err
			}
			body, err := json.Marshal(record)
			if err != nil {
				return err
			}
			if err := checkSpace(int64(len(body))); err != nil {
				return err
			}
			if err := fsutil.WriteFileAtomic(filepath.Join(directory, "filesystem-boundary.json"), body, 0600); err != nil {
				return err
			}
			if err := add("operating_state", "filesystem-boundary.json"); err != nil {
				return err
			}
		}
		// Hash the component after writer release; this file is already frozen.
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := add("library", "library.sqlite"); err != nil {
		return nil, err
	}
	if err := add("file_journal", "deletions.zip"); err != nil {
		return nil, err
	}
	body, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if len(body) > 1<<20 {
		return nil, errors.New("native checkpoint manifest exceeds its size limit")
	}
	if err := checkSpace(int64(len(body))); err != nil {
		return nil, err
	}
	if err := fsutil.WriteFileAtomic(filepath.Join(directory, "checkpoint.json"), body, 0600); err != nil {
		return nil, err
	}
	if err := errors.Join(fsutil.SyncDir(directory), fsutil.SyncDir(parent), fsutil.SyncDir(filepath.Dir(parent))); err != nil {
		return nil, err
	}
	return result, nil
}

func nativeCheckpointFile(ctx context.Context, path, role, name string) (*NativeCheckpointComponent, error) {
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() {
		return nil, errors.Join(err, errors.New("checkpoint component must be a regular file"))
	}
	input, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer input.Close()
	opened, err := input.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return nil, errors.New("checkpoint component was replaced")
	}
	digest := sha256.New()
	buffer := make([]byte, 1<<20)
	var n int64
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		count, err := input.Read(buffer)
		_, _ = digest.Write(buffer[:count])
		n += int64(count)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, after) || n != before.Size() || after.Size() != n || !after.ModTime().Equal(before.ModTime()) {
		return nil, errors.New("checkpoint component changed while hashing")
	}
	return &NativeCheckpointComponent{Role: role, Name: name, Bytes: n, SHA256: hex.EncodeToString(digest.Sum(nil))}, nil
}

func copyNativeCheckpointAsset(ctx context.Context, source, destination string, checkSpace func(int64) error) error {
	// TLS files are bounded configuration assets, not arbitrary media streams.
	before, err := os.Stat(source)
	if err != nil || !before.Mode().IsRegular() || before.Size() > 8<<20 {
		return errors.Join(err, errors.New("checkpoint configuration asset must be a regular file no larger than 8 MiB"))
	}
	if err := checkSpace(before.Size()); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	opened, err := input.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return errors.New("checkpoint configuration asset changed before opening")
	}
	body, err := io.ReadAll(io.LimitReader(input, (8<<20)+1))
	if err != nil || len(body) > 8<<20 {
		return errors.Join(err, errors.New("checkpoint configuration asset exceeds its size limit"))
	}
	after, err := os.Stat(source)
	if err != nil || !os.SameFile(before, after) || int64(len(body)) != before.Size() || !before.ModTime().Equal(after.ModTime()) {
		return fmt.Errorf("checkpoint configuration asset changed: %q", source)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(destination, body, 0600)
}
