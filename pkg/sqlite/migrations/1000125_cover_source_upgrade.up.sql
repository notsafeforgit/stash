-- Freeze the upgrade cohort once. Native writes never append new work.
-- Absence after processing is the durable completion checkpoint; migration
-- history prevents cache files from being reimported over later user choices.
CREATE TABLE scene_cover_upgrade (
  scene_id INTEGER PRIMARY KEY REFERENCES scenes(id) ON DELETE CASCADE,
  cover_checksum TEXT NOT NULL,
  captured INTEGER NOT NULL DEFAULT 0 CHECK(captured IN (0,1))
);
CREATE TABLE scene_cover_claims (
  scene_id INTEGER NOT NULL REFERENCES scene_cover_upgrade(scene_id) ON DELETE CASCADE,
  source_key TEXT NOT NULL CHECK(length(source_key)=64 AND source_key NOT GLOB '*[^0-9a-f]*'),
  at REAL NOT NULL CHECK(at >= 0 AND at <= 1.7976931348623157e308),
  PRIMARY KEY(scene_id,source_key)
);
CREATE INDEX scene_cover_upgrade_uncaptured ON scene_cover_upgrade(scene_id) WHERE captured=0;
INSERT INTO scene_cover_upgrade(scene_id,cover_checksum)
SELECT scenes.id,scenes.cover_blob FROM scenes
WHERE scenes.cover_blob IS NOT NULL AND scenes.cover_blob <> ''
  AND NOT EXISTS (SELECT 1 FROM scene_cover_sources AS source
                  WHERE source.scene_id=scenes.id AND source.cover_checksum=scenes.cover_blob);

CREATE TRIGGER scene_cover_upgrade_artwork AFTER UPDATE OF cover_blob ON scenes
WHEN NEW.cover_blob IS NOT OLD.cover_blob
BEGIN
  DELETE FROM scene_cover_upgrade WHERE scene_id=NEW.id;
END;
CREATE TRIGGER scene_cover_upgrade_selected AFTER INSERT ON scene_cover_sources
BEGIN
  DELETE FROM scene_cover_upgrade WHERE scene_id=NEW.scene_id;
END;
CREATE TRIGGER scene_cover_upgrade_reselected AFTER UPDATE ON scene_cover_sources
BEGIN
  DELETE FROM scene_cover_upgrade WHERE scene_id=NEW.scene_id;
END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000125,'Preserve cover selections before discarding preview caches','{}');
