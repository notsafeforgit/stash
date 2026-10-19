// Package filtermigration converts historical filter formats during upgrades.
//
// LEGACY-UPGRADE-ONLY: these codecs read supported upstream and pre-native fork
// data. They are not compatibility APIs, native import/export codecs, or the
// current UI's URL codec. Their input semantics and index tables are historical
// contracts; do not change them to follow new native filter features.
//
// Production callers are limited to the saved-filter database promotion and the
// checkpointed default-filter configuration promotion. boundary_test.go enforces
// that restriction. Conversion fixtures cover flat upstream criteria and the
// old fork's embedded AST, exclusions, and reconciliation projections.
//
// Changes require an explicit historical-input, recovery, or security fix and
// regression evidence preserving existing inputs. See PROJECT.md's "Legacy upgrade
// code" policy and pkg/sqlite/migrations/README.md before removing this package;
// native bootstrap bypasses these readers, but supported historical upgrades
// and unfinished configuration publication still require them.
package filtermigration
