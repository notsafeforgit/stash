package api

import (
	"context"
	"fmt"

	"github.com/stashapp/stash/internal/api/urlbuilders"
	"github.com/stashapp/stash/internal/manager"
	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/session"
	"github.com/stashapp/stash/pkg/signedurl"
)

// SceneStreamsV3 exposes the segmented-stream catalog used by the v3 player.
// The existing Scene.sceneStreams field intentionally retains the upstream
// catalog for v2.5 and external client compatibility.
func (r *sceneResolver) SceneStreamsV3(ctx context.Context, obj *models.Scene) ([]*manager.SceneStreamEndpoint, error) {
	_, err := r.getPrimaryFile(ctx, obj)
	if err != nil {
		return nil, err
	}

	config := manager.GetInstance().Config
	baseURL, _ := ctx.Value(BaseURLCtxKey).(string)
	builder := urlbuilders.NewSceneURLBuilder(baseURL, obj)

	streamURL := builder.GetStreamURL("")
	if config.HasCredentials() {
		userID := session.GetCurrentUserID(ctx)
		if userID == nil {
			return nil, fmt.Errorf("user ID not found")
		}
		streamURL.RawQuery = signedParams(config, *userID, signedurl.DerivePrefix(streamURL.Path)).Encode()
	} else if apiKey := config.GetAPIKey(); apiKey != "" {
		v := streamURL.Query()
		v.Set("apikey", apiKey)
		streamURL.RawQuery = v.Encode()
	}

	return manager.GetV3SceneStreamPaths(obj, streamURL, config.GetMaxStreamingTranscodeSize())
}
