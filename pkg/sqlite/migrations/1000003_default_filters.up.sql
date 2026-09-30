CREATE TABLE default_filters (
  view TEXT PRIMARY KEY,
  revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
  enabled BOOLEAN NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
  mode TEXT NOT NULL,
  find_filter TEXT NOT NULL DEFAULT '' CHECK (find_filter = '' OR json_valid(find_filter)),
  filter_ast TEXT NOT NULL DEFAULT '' CHECK (filter_ast = '' OR json_valid(filter_ast)),
  ui_options TEXT NOT NULL DEFAULT '' CHECK (ui_options = '' OR json_valid(ui_options)),
  CHECK (enabled = 0 OR mode IN ('SCENES', 'IMAGES', 'PERFORMERS', 'STUDIOS', 'GALLERIES', 'SCENE_MARKERS', 'MOVIES', 'GROUPS', 'TAGS'))
);
CREATE TABLE configuration_migrations (
  name TEXT PRIMARY KEY,
  source_json TEXT NOT NULL CHECK (json_valid(source_json)),
  target_json TEXT NOT NULL CHECK (json_valid(target_json)),
  state TEXT NOT NULL DEFAULT 'prepared' CHECK (state IN ('prepared', 'published')),
  prepared_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  published_at DATETIME
);
CREATE TABLE default_filter_import_conflicts (
  view TEXT PRIMARY KEY REFERENCES default_filters(view),
  migration_name TEXT NOT NULL REFERENCES configuration_migrations(name),
  evidence TEXT NOT NULL CHECK (json_valid(evidence)),
  alternative_ast TEXT NOT NULL DEFAULT '' CHECK (alternative_ast = '' OR json_valid(alternative_ast)),
  import_error TEXT NOT NULL DEFAULT '',
  selection TEXT NOT NULL DEFAULT 'pending'
    CHECK (selection IN ('pending', 'kept-current', 'used-imported', 'replaced', 'cleared')),
  resolved_at DATETIME
);
INSERT INTO native_migration_history(version, name)
VALUES (1000003, 'Native default filters and durable configuration import');
