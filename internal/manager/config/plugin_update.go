package config

import "github.com/stashapp/stash/pkg/fsutil"

// UpdatePluginConfiguration performs a patch and persistence under one lock.
// Pre-rename failures restore memory; callers never replace unrelated settings.
func (i *Config) UpdatePluginConfiguration(pluginID string, input map[string]interface{}, reset []string) (map[string]interface{}, error) {
	i.Lock()
	defer i.Unlock()

	key := PluginsSettingPrefix + pluginID
	next := i.forKey(key).Cut(key).Raw()
	if next == nil {
		next = make(map[string]interface{})
	}
	for key, value := range input {
		next[key] = value
	}
	for _, key := range reset {
		delete(next, key)
	}
	previous := i.main.Copy()
	i.set(key, next)
	if err := i.write(); err != nil {
		if !fsutil.WasPublished(err) {
			i.main = previous
		}
		return nil, err
	}
	return i.forKey(key).Cut(key).Raw(), nil
}
