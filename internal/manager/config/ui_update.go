package config

// UpdateUIConfiguration serializes UI read/modify/write operations, including
// persistence. The callback owns a detached snapshot and must not call Config
// methods while the lock is held. A failed update leaves memory unchanged.
func (i *Config) UpdateUIConfiguration(update func(map[string]interface{}) (map[string]interface{}, error)) (map[string]interface{}, error) {
	i.Lock()
	defer i.Unlock()

	current := i.forKey(UI).Cut(UI).Raw()
	if current == nil {
		current = make(map[string]interface{})
	}
	next, err := update(current)
	if err != nil {
		return nil, err
	}
	previous := i.main.Copy()
	i.set(UI, next)
	if err := i.write(); err != nil {
		i.main = previous
		return nil, err
	}
	return i.forKey(UI).Cut(UI).Raw(), nil
}
