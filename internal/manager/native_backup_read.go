package manager

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var nativeCheckpointRoles = map[string]string{
	"library.sqlite": "library", "deletions.zip": "file_journal",
	"config.yml": "config", "runtime-overrides.yml": "config", "tls.crt": "config", "tls.key": "config",
}

func readNativeCheckpoint(directory, id string) (*NativeBackupCheckpoint, error) {
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, ErrNativeCheckpointInvalid
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err = root.Lstat("checkpoint.json")
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, ErrNativeCheckpointInvalid
	}
	f, err := root.Open("checkpoint.json")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		return nil, ErrNativeCheckpointInvalid
	}
	var result NativeBackupCheckpoint
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, ErrNativeCheckpointInvalid
	}
	canonical, err := json.Marshal(result)
	if err != nil || !bytes.Equal(body, canonical) || result.Format != nativeCheckpointFormat || result.Version != 1 || result.UUID != id ||
		result.Coverage != nativeCheckpointCoverage || result.CreatedAt.IsZero() || !checkpointSHA(result.RequestSHA256) {
		return nil, ErrNativeCheckpointInvalid
	}
	names := make(map[string]bool)
	for _, component := range result.Components {
		if nativeCheckpointRoles[component.Name] != component.Role || component.Role == "" || names[component.Name] || component.Bytes < 0 || !checkpointSHA(component.SHA256) {
			return nil, ErrNativeCheckpointInvalid
		}
		names[component.Name] = true
		info, err := root.Lstat(component.Name)
		if err != nil || !info.Mode().IsRegular() || info.Size() != component.Bytes {
			return nil, ErrNativeCheckpointInvalid
		}
	}
	for _, name := range []string{"library.sqlite", "deletions.zip", "config.yml", "runtime-overrides.yml"} {
		if !names[name] {
			return nil, ErrNativeCheckpointInvalid
		}
	}
	return &result, nil
}

func checkpointSHA(value string) bool {
	body, err := hex.DecodeString(value)
	return err == nil && len(body) == 32 && strings.ToLower(value) == value
}

func (s *Manager) ReadNativeCheckpoint(id string) (*NativeBackupCheckpoint, error) {
	directory, err := s.nativeCheckpointDirectory(id)
	if err != nil {
		return nil, err
	}
	return readNativeCheckpoint(directory, id)
}

// OpenNativeCheckpointComponent serves only a sealed manifest's fixed inventory.
// Callers must retain normal application authentication and verify SHA-256 after
// transport; paths or source configuration values are never accepted as names.
func (s *Manager) OpenNativeCheckpointComponent(id, name string) (*os.File, *NativeCheckpointComponent, error) {
	if nativeCheckpointRoles[name] == "" {
		return nil, nil, ErrNativeCheckpointInvalid
	}
	directory, err := s.nativeCheckpointDirectory(id)
	if err != nil {
		return nil, nil, err
	}
	manifest, err := readNativeCheckpoint(directory, id)
	if err != nil {
		return nil, nil, err
	}
	var selected *NativeCheckpointComponent
	for i := range manifest.Components {
		if manifest.Components[i].Name == name {
			selected = &manifest.Components[i]
			break
		}
	}
	if selected == nil {
		return nil, nil, os.ErrNotExist
	}
	path := filepath.Join(directory, name)
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() {
		return nil, nil, errors.Join(err, ErrNativeCheckpointInvalid)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) || after.Size() != selected.Bytes {
		f.Close()
		return nil, nil, ErrNativeCheckpointInvalid
	}
	return f, selected, nil
}
