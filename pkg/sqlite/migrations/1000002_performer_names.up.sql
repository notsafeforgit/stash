CREATE TABLE performer_names (
  performer_id INTEGER NOT NULL REFERENCES performers(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  position INTEGER NOT NULL CHECK (position >= 0),
  is_primary BOOLEAN GENERATED ALWAYS AS (position = 0) STORED,
  ignore_auto_tag BOOLEAN NOT NULL DEFAULT 0 CHECK (ignore_auto_tag IN (0, 1)),
  PRIMARY KEY (performer_id, name),
  UNIQUE (performer_id, position)
);
CREATE INDEX performer_names_lookup ON performer_names(name COLLATE NOCASE, performer_id);

INSERT INTO performer_names(performer_id, name, position, ignore_auto_tag)
SELECT id, COALESCE(name, ''), 0,
  EXISTS (SELECT 1 FROM performer_autotag_ignored_names n
          WHERE n.performer_id = performers.id AND n.name = performers.name)
FROM performers;

INSERT INTO performer_names(performer_id, name, position, ignore_auto_tag)
SELECT a.performer_id, a.alias,
  ROW_NUMBER() OVER (PARTITION BY a.performer_id ORDER BY a.alias),
  EXISTS (SELECT 1 FROM performer_autotag_ignored_names n
          WHERE n.performer_id = a.performer_id AND n.name = a.alias)
FROM performer_aliases a JOIN performers p ON p.id = a.performer_id
WHERE a.alias != COALESCE(p.name, '');

INSERT INTO native_migration_history(version, name, details)
SELECT 1000002, 'Unified performer names and auto-tag policies', json_object(
  'performers', (SELECT count(*) FROM performers),
  'alias_rows', (SELECT count(*) FROM performer_aliases),
  'names', (SELECT count(*) FROM performer_names),
  'duplicate_primary_aliases', json((SELECT json_group_array(json_object('performer_id', a.performer_id, 'name', a.alias))
    FROM performer_aliases a JOIN performers p ON p.id = a.performer_id WHERE a.alias = COALESCE(p.name, ''))),
  'unmatched_name_policies', json((SELECT json_group_array(json_object('performer_id', n.performer_id, 'name', n.name))
    FROM performer_autotag_ignored_names n
    WHERE NOT EXISTS (SELECT 1 FROM performer_names p WHERE p.performer_id = n.performer_id AND p.name = n.name COLLATE NOCASE)))
);

DROP TABLE performer_aliases;
DROP TABLE performer_autotag_ignored_names;
DROP INDEX performers_name_disambiguation_unique;
DROP INDEX performers_name_unique;
ALTER TABLE performers DROP COLUMN name;
