-- Exact case-insensitive relationship lookups must not scan whole entity or
-- alias tables. Canonical/alias uniqueness remains an explicit review concern.
CREATE INDEX metadata_studio_names ON studios(name COLLATE NOCASE,id);
CREATE INDEX metadata_studio_aliases ON studio_aliases(alias COLLATE NOCASE,studio_id);
CREATE INDEX metadata_tag_names ON tags(name COLLATE NOCASE,id);
CREATE INDEX metadata_tag_aliases ON tag_aliases(alias COLLATE NOCASE,tag_id);
CREATE INDEX metadata_group_names ON groups(name COLLATE NOCASE,id);

INSERT INTO native_migration_history(version,name,details)
VALUES(1000079,'Indexed native relationship name matching','{}');
