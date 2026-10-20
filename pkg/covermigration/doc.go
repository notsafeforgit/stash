// Package covermigration reads original v1 path-keyed cover manifests.
//
// LEGACY-UPGRADE-ONLY: these manifests formerly held authored timestamps that
// were not saved in SQLite. The schema 1000125 startup upgrade captures bounded
// claims before job admission and resolves them against attached source files.
// Normal cover queries/generation use SQLite and never import cache metadata.
// Retiring this reader requires retiring uncaptured upgrade cohorts; unresolved
// claims already captured in SQLite must be preserved independently of it.
package covermigration
