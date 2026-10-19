package api

import (
	"context"
	"sync"

	"github.com/stashapp/stash/internal/manager/config"
)

// settingsRuntime owns the services affected by saved configuration. The server
// supplies these dependencies; settings resolvers do not look up a process-wide
// manager. Refreshes run only after durable publication.
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
// rather than run them. An unsuccessful save never refreshes runtime services.
func (s *settingsService) update(ctx context.Context, prepare func(*config.Config) (func(), error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var apply func()
	if err := s.config.Update(ctx, func(candidate *config.Config) error {
		var err error
		apply, err = prepare(candidate)
		return err
	}); err != nil {
		return err
	}
	if apply != nil {
		apply()
	}
	return nil
}
