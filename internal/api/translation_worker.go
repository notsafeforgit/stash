package api

import (
	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/translation"
)

func newTranslationWorkerRuntime(conf *config.Config, service *translation.Service) *archiveWorkerRuntime {
	if !conf.GetTranslationWorkerEnabled() {
		return nil
	}
	provider, err := translation.NewBingTranslateShell(conf.GetTranslationShellPath())
	if err != nil {
		logger.Warn("Native translation worker could not start: configured translate-shell provider unavailable")
		return nil
	}
	return &archiveWorkerRuntime{worker: translation.NewWorker(service, provider)}
}
