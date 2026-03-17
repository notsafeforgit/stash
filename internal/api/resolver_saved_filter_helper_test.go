package api

import (
	"context"
	"github.com/stashapp/stash/pkg/models"
	"testing"
)

func TestMergeFindFilter(t *testing.T) {
	// Ensure that findFilter overlays correctly
	resolver := &queryResolver{}

	// Create an empty findFilter pointer to ensure no panics
	filter := &models.FindFilterType{}

	savedFilterId := "invalid"

	_, err := resolver.resolveSavedFilter(context.Background(), savedFilterId, models.FilterModeScenes, filter, nil)
	if err == nil {
		t.Errorf("Expected error from invalid ID")
	}
}
