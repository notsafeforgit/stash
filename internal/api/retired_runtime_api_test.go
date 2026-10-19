package api

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRetiredRuntimeAPIsStayAbsent(t *testing.T) {
	schema := newLibrarySchema(Config{Resolvers: &Resolver{}}).Schema()
	for parent, fields := range map[string][]string{
		"Query":        {"allScenes", "allSceneMarkers", "allImages", "allGalleries", "allPerformers", "allTags", "allStudios"},
		"Mutation":     {"sceneIncrementO", "sceneDecrementO", "sceneIncrementPlayCount"},
		"Subscription": {"jobsSubscribe"},
	} {
		for _, field := range fields {
			require.Nil(t, schema.Types[parent].Fields.ForName(field), parent+"."+field)
		}
	}
	for parent, fields := range map[string][]string{
		"Query":        {"findScenes", "findSceneMarkers", "findImages", "findGalleries", "findPerformers", "findTags", "findStudios", "findJob", "jobHistory"},
		"Mutation":     {"sceneAddO", "sceneDeleteO", "sceneAddPlay"},
		"Subscription": {"jobsLifecycleSubscribe", "jobsProgressSubscribe"},
	} {
		for _, field := range fields {
			require.NotNil(t, schema.Types[parent].Fields.ForName(field), parent+"."+field)
		}
	}
}
