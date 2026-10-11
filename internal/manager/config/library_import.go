package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

const libraryFolderImportMarker = "native_configuration_migrations.library_folders"

func (i *Config) LibraryFolderImportSource() (string, error) {
	i.RLock()
	defer i.RUnlock()
	return i.libraryFolderImportSource()
}

func (i *Config) libraryFolderImportSource() (string, error) {
	data, err := json.Marshal(i.legacyStashPaths())
	return string(data), err
}

func (i *Config) PublishLibraryFolderImport(source string) error {
	i.Lock()
	defer i.Unlock()
	sum := sha256.Sum256([]byte(source))
	digest := hex.EncodeToString(sum[:])
	if i.main.String(libraryFolderImportMarker) == digest && !i.main.Exists(Stash) {
		return nil
	}
	current, err := i.libraryFolderImportSource()
	if err != nil {
		return err
	}
	if current != source {
		return errors.New("library folders changed after staging; retained native records require reconciliation")
	}
	previous := i.main.Copy()
	i.main.Delete(Stash)
	i.set(libraryFolderImportMarker, digest)
	if err := i.write(); err != nil {
		i.main = previous
		return err
	}
	return nil
}
