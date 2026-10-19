package config

import (
	"context"
	"github.com/stashapp/stash/pkg/fsutil"
)

// Snapshot returns a detached, internally consistent configuration. It does not
// copy locks. Environment/flag overrides and startup-only values are preserved.
func (i *Config) Snapshot() *Config {
	i.RLock()
	defer i.RUnlock()
	return &Config{
		main: i.main.Copy(), overrides: i.overrides.Copy(),
		filePath: i.filePath, isNewSystem: i.isNewSystem,
		certFile: i.certFile, keyFile: i.keyFile,
		publicIPWhitelist: i.publicIPWhitelist, revision: i.revision,
	}
}

// Update validates a detached candidate, persists it, then publishes it to
// readers. Failures before rename change neither live values nor the existing
// file. A post-rename directory-sync failure publishes the new live values and
// returns PublishedWriteError so callers can report uncertain durability. Callbacks may use the candidate's getters/setters but must
// not persist or mutate the candidate after returning. Any preparation outside
// configuration (such as ensuring directories exist) must be safe to repeat.
//
// Updates are serialized. Startup/legacy writers still use the ordinary config
// lock; if one changes the snapshot while validation is running, validation is
// retried against its new values. No config lock is held during validation, which
// may query a repository whose other callers themselves read configuration.
func (i *Config) Update(ctx context.Context, update func(*Config) error) error {
	return i.update(ctx, update, func(candidate *Config) error { return candidate.write() })
}

func (i *Config) update(ctx context.Context, update func(*Config) error, persist func(*Config) error) error {
	i.updateMu.Lock()
	defer i.updateMu.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		next := i.Snapshot()
		revision := next.revision
		if err := update(next); err != nil {
			return err
		}
		i.Lock()
		if i.revision != revision {
			i.Unlock()
			continue
		}
		err := ctx.Err()
		if err == nil {
			err = persist(next)
		}
		if err == nil || fsutil.WasPublished(err) {
			i.main = next.main
			i.revision++
		}
		i.Unlock()
		return err
	}
}
