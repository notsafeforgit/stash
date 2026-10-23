package api

import (
	"net/http"

	"github.com/stashapp/stash/internal/ingest"
	"github.com/stashapp/stash/pkg/gallery"
	"github.com/stashapp/stash/pkg/translation"
)

// Capture the installation setting once, alongside the routes and GraphQL
// capability. Saving a new setting requires a restart; it cannot leave workers
// running behind disabled routes or present enabled UI over an inactive API.
func (s *Server) configureSourceTracking(enabled bool) (*ingest.Service, *nativeArchiveRoutes) {
	mgr := s.manager
	repo := mgr.Repository
	routes := &nativeArchiveRoutes{repo: repo, notifyMetadata: mgr.RegisterMetadataPolicyHooks}
	if !enabled {
		return nil, routes
	}

	service := ingest.New(repo)
	s.enrichmentMaintenance = &backgroundWorkerRuntime{worker: ingest.NewEnrichmentMaintenance(service)}
	s.discoveryMaintenance = &backgroundWorkerRuntime{worker: ingest.NewDiscoveryMaintenance(service)}
	s.discoveryComparison = &backgroundWorkerRuntime{worker: ingest.NewDiscoveryComparisonWorker(service)}
	s.discoveryPublication = &backgroundWorkerRuntime{worker: ingest.NewDiscoveryPublicationWorker(service)}
	if worker := mgr.NewIngestFileWorker(service); worker != nil {
		s.ingestWorker = &backgroundWorkerRuntime{worker: worker}
	}
	routes.fileIngestion = s.ingestWorker != nil
	routes.albums = gallery.NewAlbumBackfill(repo)
	s.albumWorker = &backgroundWorkerRuntime{worker: mgr.NewAlbumBackfillWorker(routes.albums)}
	s.postMergeNotifications = &backgroundWorkerRuntime{worker: mgr.NewPostMergeNotificationWorker(gallery.NewPostMergeNotifications(repo))}
	routes.translations = translation.New(repo)
	s.translationWorker = newTranslationWorkerRuntime(mgr.Config, routes.translations)
	return service, routes
}

// A disabled installation never acknowledges an intake event or consumes work.
// Producers retain their outboxes and may deliver them after re-enabling.
func sourceTrackingDisabled(w http.ResponseWriter, _ *http.Request) {
	ingestJSON(w, http.StatusServiceUnavailable, map[string]string{
		"error":   "source_tracking_disabled",
		"message": "Enable source tracking in Settings > Services and restart Stash.",
	})
}
