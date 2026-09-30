package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

const defaultFilterImportMarker = "native_configuration_migrations.default_filters"

// DefaultFilterImportSource extracts only the historical filter state. Other
// UI settings and server credentials never become migration evidence.
func DefaultFilterImportSource(ui map[string]interface{}) (string, error) {
	input := make(map[string]interface{})
	for _, key := range []string{"defaultFilters", "forkDefaultFilterState"} {
		if value, exists := ui[key]; exists {
			input[key] = value
		}
	}
	data, err := json.Marshal(input)
	return string(data), err
}

// PublishDefaultFilterImport removes the old writable representation only
// after native records and their source checkpoint have committed. The marker
// and removals share one atomic file publication and one configuration lock.
func (i *Config) PublishDefaultFilterImport(source string) error {
	i.Lock()
	defer i.Unlock()
	ui := i.forKey(UI).Cut(UI).Raw()
	current, err := DefaultFilterImportSource(ui)
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(source))
	digest := hex.EncodeToString(sum[:])
	if current == "{}" && i.main.String(defaultFilterImportMarker) == digest {
		return nil
	}
	if current != source {
		return errors.New("default-filter configuration changed after staging; native input is retained and publication requires reconciliation")
	}
	previous := i.main.Copy()
	delete(ui, "defaultFilters")
	delete(ui, "forkDefaultFilterState")
	i.set(UI, ui)
	i.set(defaultFilterImportMarker, digest)
	if err := i.write(); err != nil {
		i.main = previous
		return err
	}
	return nil
}
