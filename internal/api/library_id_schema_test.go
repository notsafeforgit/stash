package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vektah/gqlparser/v2/ast"
)

// Every GraphQL ID must declare its identity domain. A new unmarked library
// reference would otherwise silently bypass UUID validation and translation.
func TestLibraryIDSchemaCoverage(t *testing.T) {
	opaque := map[string][]string{
		"job": {
			"Mutation.migrate", "Mutation.downloadFFMpeg", "Mutation.sceneGenerateScreenshot",
			"Mutation.importObjects", "Mutation.metadataImport", "Mutation.metadataExport",
			"Mutation.metadataScan", "Mutation.metadataGenerate", "Mutation.metadataAutoTag",
			"Mutation.metadataClean", "Mutation.metadataCleanGenerated", "Mutation.metadataIdentify",
			"Mutation.migrateHashNaming", "Mutation.migrateSceneScreenshots", "Mutation.migrateBlobs",
			"Mutation.optimiseDatabase", "Mutation.runPluginTask", "Mutation.installPackages",
			"Mutation.updatePackages", "Mutation.uninstallPackages", "Mutation.stopJob(job_id)",
			"Mutation.sceneRegenerateCover", "BulkUpdateResult.job_id", "Job.id", "FindJobInput.id",
		},
		"plugin": {
			"Query.pluginSettingsV3(plugin_id)", "Query.pluginSettingPreviewV3(plugin_id)", "Query.pluginQueryV3(plugin_id)",
			"Mutation.configurePlugin(plugin_id)", "Mutation.runPluginTask(plugin_id)", "Mutation.runPluginOperation(plugin_id)",
			"Mutation.updatePluginSettingsV3(plugin_id)", "Mutation.pluginMutationV3(plugin_id)",
			"ConfigResult.plugins(include)", "PluginV3.id", "PluginV3.requires",
		},
		"share": {
			"Query.mediaShare(id)", "Mutation.mediaShareRevoke(id)", "Mutation.mediaShareDelete(id)",
			"Mutation.mediaShareRotate(id)", "Mutation.mediaShareRetry(id)", "MediaShareUpdateInput.id", "MediaShare.id",
		},
		"remote provider": {"Mutation.submitStashBoxSceneDraft", "Mutation.submitStashBoxPerformerDraft"},
		"scraper":         {"Scraper.id", "ScraperSourceInput.scraper_id", "ScraperSource.scraper_id"},
		"source post":     {"GalleryMediaItem.source_post_uuid"},
	}
	domains := map[string]string{}
	for domain, paths := range opaque {
		for _, path := range paths {
			domains[path] = domain
		}
	}
	seen := map[string]bool{}
	knownKinds := map[string]bool{
		"scene": true, "image": true, "gallery": true, "performer": true,
		"studio": true, "tag": true, "group": true, "file": true,
		"folder": true, "scene_marker": true, "gallery_chapter": true,
		"saved_filter": true, "scope": true, "any": true,
	}
	check := func(path string, typ *ast.Type, directives ast.DirectiveList) {
		library := directives.ForName("libraryID")
		if library != nil {
			assert.Equal(t, "ID", typ.Name(), "%s: library references must use the ID boundary", path)
			kind := library.Arguments.ForName("kind")
			if assert.NotNil(t, kind, path) {
				assert.True(t, knownKinds[kind.Value.Raw], "%s: unknown library kind %q", path, kind.Value.Raw)
			}
			assert.Empty(t, domains[path], "%s: remove obsolete opaque-ID exception", path)
		} else if typ.Name() == "ID" {
			assert.NotEmpty(t, domains[path], "%s: declare @libraryID or document this separate identity domain", path)
			seen[path] = true
		}
	}
	for name, typ := range NewExecutableSchema(Config{}).Schema().Types {
		if len(name) >= 2 && name[:2] == "__" {
			continue
		}
		for _, field := range typ.Fields {
			check(name+"."+field.Name, field.Type, field.Directives)
			for _, arg := range field.Arguments {
				check(name+"."+field.Name+"("+arg.Name+")", arg.Type, arg.Directives)
			}
		}
	}
	for path := range domains {
		assert.True(t, seen[path], "%s: stale opaque-ID exception", path)
	}
}
