package task

import (
	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/previewimage"
)

func (j *CleanGeneratedJob) compactPreviewImages(store previewimage.Store, id int, kind, key string) {
	bytes, err := store.Compact(id, kind, key, j.Options.DryRun)
	if err != nil {
		logger.Warnf("[preview image] keeping JPEG fallback for %s %d: %v", kind, id, err)
		return
	}
	if bytes > 0 {
		logger.Debugf("%s[preview image] redundant JPEGs for %s %d: %d bytes", j.dryRunPrefix, kind, id, bytes)
	}
}
