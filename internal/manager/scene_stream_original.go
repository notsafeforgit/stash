package manager

import (
	"net/url"

	"github.com/stashapp/stash/pkg/models"
)

// GetOriginalSceneStreamPaths uses the normal browser-compatible stream
// catalog without substituting a generated transcode for the pinned source.
// Share downloads and direct playback always refer to that original file.
func GetOriginalSceneStreamPaths(scene *models.Scene, streamURL *url.URL, maxResolution models.StreamingResolutionEnum) ([]*SceneStreamEndpoint, error) {
	return getSceneStreamPaths(scene, streamURL, maxResolution, false)
}
