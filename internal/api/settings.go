package api

import (
	"context"
	"sync"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/fsutil"
)

// settingsRuntime owns the services affected by saved configuration. The server
// supplies these dependencies; settings resolvers do not look up a process-wide
// manager. Refreshes run only after file publication.
type settingsRuntime interface {
	RefreshConfig()
	RefreshScraperCache()
	RefreshPluginCache()
	RefreshFFMpeg(context.Context)
	RefreshStreamManager()
	SetBlobStoreOptions()
	RefreshScraperSourceManager()
	RefreshPluginSourceManager()
	RefreshDLNA()
	UIConfiguration(context.Context) (map[string]interface{}, error)
}

type settingsService struct {
	config      *config.Config
	runtime     settingsRuntime
	setLogLevel func(string)
	mu          sync.Mutex
}

// update keeps publication and refreshes ordered across simultaneous saves.
// prepare can be retried against a newer snapshot and must return its effects
// rather than run them. Pre-rename failures never refresh runtime services; published writes do, even
// when a directory-sync error leaves durability uncertain.
func (s *settingsService) update(ctx context.Context, prepare func(*config.Config) (func(), error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var apply func()
	err := s.config.Update(ctx, func(candidate *config.Config) error {
		var err error
		apply, err = prepare(candidate)
		return err
	})
	if err != nil && !fsutil.WasPublished(err) {
		return err
	}
	if apply != nil {
		apply()
	}
	return err
}
