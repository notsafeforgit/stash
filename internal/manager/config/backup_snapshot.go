package config

import (
	"github.com/knadh/koanf/parsers/yaml"
)

// BackupSnapshot preserves persistent settings and runtime overrides separately.
// It is private backup material and can contain credentials. It must never be
// included in logs, public reports, plugin mapping inputs or ordinary API JSON.
type BackupSnapshot struct {
	MainYAML      []byte
	OverridesYAML []byte
	ConfigPath    string
	TLSCertPath   string
	TLSKeyPath    string
}

// CaptureBackupSnapshot takes one detached configuration view. The coordinator
// acquires the database writer guard FIRST, then calls this method. It must not
// hold a Config lock while waiting for a database transaction. Existing UI/plugin
// update callbacks only edit maps; default-filter publication runs after commit.
func (i *Config) CaptureBackupSnapshot() (*BackupSnapshot, error) {
	i.RLock()
	defer i.RUnlock()
	return i.backupSnapshot()
}

// WithBackupSnapshot keeps settings fixed while the coordinator captures their
// small external assets. The callback must not call Config methods or write to
// the guarded database. Release this lock before large database/media copies.
func (i *Config) WithBackupSnapshot(capture func(*BackupSnapshot) error) error {
	i.RLock()
	defer i.RUnlock()
	snapshot, err := i.backupSnapshot()
	if err != nil {
		return err
	}
	return capture(snapshot)
}

func (i *Config) backupSnapshot() (*BackupSnapshot, error) {
	main, err := i.main.Marshal(yaml.Parser())
	if err != nil {
		return nil, err
	}
	overrides, err := i.overrides.Marshal(yaml.Parser())
	if err != nil {
		return nil, err
	}
	return &BackupSnapshot{MainYAML: main, OverridesYAML: overrides, ConfigPath: i.filePath,
		TLSCertPath: i.certFile, TLSKeyPath: i.keyFile}, nil
}
