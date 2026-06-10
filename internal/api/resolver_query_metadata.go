package api

import (
	"context"

	"github.com/stashapp/stash/internal/manager"
)

func (r *queryResolver) SystemStatus(ctx context.Context) (*manager.SystemStatus, error) {
	return manager.GetInstance().GetSystemStatus(), nil
}

func (r *queryResolver) ServerCapabilities(ctx context.Context) (*manager.ServerCapabilities, error) {
	return manager.GetInstance().GetServerCapabilities(), nil
}
