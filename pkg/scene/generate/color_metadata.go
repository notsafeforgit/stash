package generate

import (
	"strings"

	"github.com/stashapp/stash/pkg/ffmpeg"
	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/logger"
)

func (g Generator) retryWithColorMetadataFixedInput(lockCtx *fsutil.LockContext, input string, originalErr error, retry func(input string) error) error {
	if !ffmpeg.IsInvalidColorSpaceError(originalErr) {
		return originalErr
	}

	fixedInput, cleanup, err := g.Encoder.CreateColorMetadataFixedInput(lockCtx, input, "")
	if err != nil {
		logger.Debugf("[generator] color metadata rewrite fallback unavailable for %s: %s", input, compactError(err))
		return originalErr
	}
	defer cleanup()

	logger.Warnf("[generator] screenshot failed for %s due to invalid color metadata, retrying with rewritten stream metadata", input)
	return retry(fixedInput)
}

func compactError(err error) string {
	if err == nil {
		return ""
	}

	return strings.ReplaceAll(err.Error(), "\n", "; ")
}
