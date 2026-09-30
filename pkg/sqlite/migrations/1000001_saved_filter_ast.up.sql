CREATE TABLE native_migration_history (
  version INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  details TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(details))
);
INSERT INTO native_migration_history(version, name, applied_at)
SELECT 1000000, 'Native schema lineage and sidecar promotion', promoted_at FROM native_schema;
INSERT INTO native_migration_history(version, name, details)
SELECT 1000001, 'Canonical saved-filter ASTs',
  json_object('filters', count(*), 'conflicts_retained', count(conflict_json))
FROM native_saved_filter_input;

CREATE TABLE saved_filter_import_conflicts (
  id INTEGER PRIMARY KEY,
  saved_filter_id INTEGER UNIQUE REFERENCES saved_filters(id) ON DELETE SET NULL,
  migration_version INTEGER NOT NULL REFERENCES native_migration_history(version),
  evidence TEXT NOT NULL CHECK (json_valid(evidence)),
  selection TEXT NOT NULL DEFAULT 'preserved-canonical',
  review_required BOOLEAN NOT NULL DEFAULT 1,
  reviewed_at DATETIME
);
INSERT INTO saved_filter_import_conflicts(id, saved_filter_id, migration_version, evidence)
SELECT saved_filter_id, saved_filter_id, 1000001, conflict_json
FROM native_saved_filter_input WHERE conflict_json IS NOT NULL;

ALTER TABLE saved_filters ADD COLUMN filter_ast BLOB NOT NULL DEFAULT ''
  CHECK (filter_ast = '' OR json_valid(filter_ast));
UPDATE saved_filters SET filter_ast = (
  SELECT filter_ast FROM native_saved_filter_input WHERE saved_filter_id = saved_filters.id
);
ALTER TABLE saved_filters DROP COLUMN object_filter;
DROP TABLE saved_filter_state;
DROP TABLE native_saved_filter_input;
