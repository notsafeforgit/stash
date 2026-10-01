ALTER TABLE galleries ADD COLUMN origin TEXT NOT NULL DEFAULT 'manual'
  CHECK (origin IN ('manual', 'filesystem', 'source'));
DROP TRIGGER archive_gallery_changed;
UPDATE galleries SET origin = 'filesystem'
WHERE folder_id IS NOT NULL OR EXISTS (SELECT 1 FROM galleries_files WHERE gallery_id = galleries.id);
CREATE TRIGGER archive_gallery_changed AFTER UPDATE ON galleries
BEGIN UPDATE archive_entities SET revision = revision + 1 WHERE gallery_id = NEW.id; END;
CREATE TRIGGER gallery_origin_immutable BEFORE UPDATE OF origin ON galleries
WHEN NEW.origin != OLD.origin
BEGIN SELECT RAISE(ABORT, 'gallery creation origin is immutable'); END;
CREATE TRIGGER source_gallery_folder_insert BEFORE INSERT ON galleries
WHEN NEW.origin = 'source' AND NEW.folder_id IS NOT NULL
BEGIN SELECT RAISE(ABORT, 'source albums cannot be filesystem galleries'); END;
CREATE TRIGGER source_gallery_folder_update BEFORE UPDATE OF folder_id ON galleries
WHEN NEW.origin = 'source' AND NEW.folder_id IS NOT NULL
BEGIN SELECT RAISE(ABORT, 'source albums cannot be filesystem galleries'); END;
CREATE TRIGGER source_gallery_file_insert BEFORE INSERT ON galleries_files
WHEN EXISTS (SELECT 1 FROM galleries WHERE id = NEW.gallery_id AND origin = 'source')
BEGIN SELECT RAISE(ABORT, 'source albums cannot be filesystem galleries'); END;
CREATE TRIGGER source_gallery_file_update BEFORE UPDATE OF gallery_id ON galleries_files
WHEN EXISTS (SELECT 1 FROM galleries WHERE id = NEW.gallery_id AND origin = 'source')
BEGIN SELECT RAISE(ABORT, 'source albums cannot be filesystem galleries'); END;
CREATE TABLE post_gallery_decisions (
  uuid TEXT NOT NULL PRIMARY KEY
    CHECK (length(uuid) = 36 AND uuid = lower(uuid)
      AND substr(uuid, 9, 1) = '-' AND substr(uuid, 14, 1) = '-'
      AND substr(uuid, 19, 1) = '-' AND substr(uuid, 24, 1) = '-'
      AND length(replace(uuid, '-', '')) = 32
      AND replace(uuid, '-', '') NOT GLOB '*[^0-9a-f]*'
      AND uuid != '00000000-0000-0000-0000-000000000000'),
  post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
  revision INTEGER NOT NULL CHECK (revision > 0),
  state TEXT NOT NULL CHECK (state IN ('linked', 'disabled')),
  gallery_uuid TEXT REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
  selection_uuid TEXT,
  origin TEXT NOT NULL CHECK (origin IN ('source', 'review', 'migration')),
  reason TEXT NOT NULL DEFAULT '' CHECK (length(reason) <= 4096),
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE (post_uuid, revision),
  UNIQUE (post_uuid, uuid),
  UNIQUE (post_uuid, uuid, gallery_uuid),
  FOREIGN KEY (post_uuid, selection_uuid) REFERENCES post_attachment_decisions(post_uuid, uuid),
  CHECK (origin != 'source' OR (state = 'linked' AND selection_uuid IS NOT NULL)),
  CHECK ((state = 'linked' AND gallery_uuid IS NOT NULL) OR (state = 'disabled' AND gallery_uuid IS NULL))
);
CREATE INDEX post_gallery_decisions_gallery ON post_gallery_decisions(gallery_uuid) WHERE gallery_uuid IS NOT NULL;
CREATE TABLE post_gallery_links (
  post_uuid TEXT NOT NULL PRIMARY KEY REFERENCES source_posts(uuid),
  decision_uuid TEXT NOT NULL,
  gallery_uuid TEXT,
  UNIQUE (post_uuid, gallery_uuid),
  FOREIGN KEY (post_uuid, decision_uuid) REFERENCES post_gallery_decisions(post_uuid, uuid),
  FOREIGN KEY (post_uuid, decision_uuid, gallery_uuid) REFERENCES post_gallery_decisions(post_uuid, uuid, gallery_uuid) ON UPDATE CASCADE
) WITHOUT ROWID;
CREATE UNIQUE INDEX post_gallery_links_gallery ON post_gallery_links(gallery_uuid) WHERE gallery_uuid IS NOT NULL;
CREATE TABLE source_gallery_write_context (
  gallery_uuid TEXT NOT NULL PRIMARY KEY,
  post_uuid TEXT NOT NULL,
  selection_uuid TEXT NOT NULL,
  FOREIGN KEY (post_uuid, selection_uuid) REFERENCES post_attachment_decisions(post_uuid, uuid),
  FOREIGN KEY (post_uuid, gallery_uuid) REFERENCES post_gallery_links(post_uuid, gallery_uuid) ON UPDATE CASCADE
) WITHOUT ROWID;
CREATE TABLE gallery_membership_events (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  uuid TEXT NOT NULL UNIQUE
    CHECK (length(uuid) = 36 AND uuid = lower(uuid)
      AND substr(uuid, 9, 1) = '-' AND substr(uuid, 14, 1) = '-'
      AND substr(uuid, 19, 1) = '-' AND substr(uuid, 24, 1) = '-'
      AND length(replace(uuid, '-', '')) = 32
      AND replace(uuid, '-', '') NOT GLOB '*[^0-9a-f]*'
      AND uuid != '00000000-0000-0000-0000-000000000000'),
  gallery_uuid TEXT NOT NULL REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
  media_uuid TEXT NOT NULL REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
  state TEXT NOT NULL CHECK (state IN ('included', 'excluded')),
  origin TEXT NOT NULL CHECK (origin IN ('source', 'library', 'migration')),
  post_uuid TEXT REFERENCES source_posts(uuid),
  selection_uuid TEXT,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE (gallery_uuid, media_uuid, uuid),
  FOREIGN KEY (post_uuid, selection_uuid) REFERENCES post_attachment_decisions(post_uuid, uuid),
  CHECK ((origin = 'source' AND post_uuid IS NOT NULL AND selection_uuid IS NOT NULL) OR (origin != 'source' AND post_uuid IS NULL AND selection_uuid IS NULL))
);
CREATE INDEX gallery_membership_events_gallery ON gallery_membership_events(gallery_uuid, sequence);
CREATE INDEX gallery_membership_events_media ON gallery_membership_events(media_uuid);
CREATE INDEX gallery_membership_events_post ON gallery_membership_events(post_uuid) WHERE post_uuid IS NOT NULL;
CREATE TABLE gallery_membership_heads (
  gallery_uuid TEXT NOT NULL,
  media_uuid TEXT NOT NULL,
  event_uuid TEXT NOT NULL,
  PRIMARY KEY (gallery_uuid, media_uuid),
  FOREIGN KEY (gallery_uuid, media_uuid, event_uuid) REFERENCES gallery_membership_events(gallery_uuid, media_uuid, uuid) ON UPDATE CASCADE
) WITHOUT ROWID;
CREATE TRIGGER post_gallery_decision_kind_insert BEFORE INSERT ON post_gallery_decisions
WHEN EXISTS (SELECT 1 FROM archive_entities WHERE uuid = NEW.gallery_uuid AND kind != 'gallery')
BEGIN SELECT RAISE(ABORT, 'source album requires a gallery identity'); END;
CREATE TRIGGER post_gallery_link_scope_insert BEFORE INSERT ON post_gallery_links
WHEN NEW.gallery_uuid IS NOT (SELECT gallery_uuid FROM post_gallery_decisions WHERE uuid = NEW.decision_uuid AND post_uuid = NEW.post_uuid)
BEGIN SELECT RAISE(ABORT, 'source album head does not match its decision'); END;
CREATE TRIGGER gallery_membership_kind_insert BEFORE INSERT ON gallery_membership_events
BEGIN
  SELECT RAISE(ABORT, 'membership requires a gallery identity') WHERE EXISTS (SELECT 1 FROM archive_entities WHERE uuid = NEW.gallery_uuid AND kind != 'gallery');
  SELECT RAISE(ABORT, 'membership requires a scene or image identity') WHERE EXISTS (SELECT 1 FROM archive_entities WHERE uuid = NEW.media_uuid AND kind NOT IN ('scene', 'image'));
END;
CREATE TRIGGER post_gallery_decision_kind_update BEFORE UPDATE ON post_gallery_decisions
WHEN EXISTS (SELECT 1 FROM archive_entities WHERE uuid = NEW.gallery_uuid AND kind != 'gallery')
BEGIN SELECT RAISE(ABORT, 'source album requires a gallery identity'); END;
CREATE TRIGGER post_gallery_link_scope_update BEFORE UPDATE ON post_gallery_links
WHEN NEW.gallery_uuid IS NOT (SELECT gallery_uuid FROM post_gallery_decisions WHERE uuid = NEW.decision_uuid AND post_uuid = NEW.post_uuid)
BEGIN SELECT RAISE(ABORT, 'source album head does not match its decision'); END;
CREATE TRIGGER gallery_membership_kind_update BEFORE UPDATE ON gallery_membership_events
BEGIN
  SELECT RAISE(ABORT, 'membership requires a gallery identity') WHERE EXISTS (SELECT 1 FROM archive_entities WHERE uuid = NEW.gallery_uuid AND kind != 'gallery');
  SELECT RAISE(ABORT, 'membership requires a scene or image identity') WHERE EXISTS (SELECT 1 FROM archive_entities WHERE uuid = NEW.media_uuid AND kind NOT IN ('scene', 'image'));
END;
CREATE TRIGGER post_gallery_decision_immutable BEFORE UPDATE ON post_gallery_decisions
WHEN NEW.uuid != OLD.uuid OR NEW.post_uuid != OLD.post_uuid OR NEW.revision != OLD.revision
  OR NEW.selection_uuid IS NOT OLD.selection_uuid OR NEW.state != OLD.state OR NEW.origin != OLD.origin OR NEW.reason != OLD.reason OR NEW.created_at != OLD.created_at
  OR (NEW.gallery_uuid IS NOT OLD.gallery_uuid AND (NEW.gallery_uuid IS NULL OR OLD.gallery_uuid IS NULL OR EXISTS (SELECT 1 FROM archive_entities WHERE uuid = OLD.gallery_uuid)))
BEGIN SELECT RAISE(ABORT, 'source gallery decisions are immutable'); END;
CREATE TRIGGER post_gallery_head_forward BEFORE UPDATE OF decision_uuid ON post_gallery_links
WHEN NEW.decision_uuid != OLD.decision_uuid
BEGIN
  SELECT RAISE(ABORT, 'source gallery head cannot move backwards')
    WHERE (SELECT revision FROM post_gallery_decisions WHERE uuid = NEW.decision_uuid)
       <= (SELECT revision FROM post_gallery_decisions WHERE uuid = OLD.decision_uuid);
END;
CREATE TRIGGER gallery_membership_event_immutable BEFORE UPDATE ON gallery_membership_events
WHEN NEW.uuid != OLD.uuid OR NEW.sequence != OLD.sequence OR NEW.state != OLD.state OR NEW.origin != OLD.origin
  OR NEW.post_uuid IS NOT OLD.post_uuid OR NEW.selection_uuid IS NOT OLD.selection_uuid OR NEW.created_at != OLD.created_at
  OR (NEW.gallery_uuid != OLD.gallery_uuid AND EXISTS (SELECT 1 FROM archive_entities WHERE uuid = OLD.gallery_uuid))
  OR (NEW.media_uuid != OLD.media_uuid AND EXISTS (SELECT 1 FROM archive_entities WHERE uuid = OLD.media_uuid))
BEGIN SELECT RAISE(ABORT, 'gallery membership events are immutable'); END;
CREATE TRIGGER gallery_membership_head_forward BEFORE UPDATE OF event_uuid ON gallery_membership_heads
WHEN NEW.event_uuid != OLD.event_uuid
BEGIN
  SELECT RAISE(ABORT, 'gallery membership head cannot move backwards')
    WHERE (SELECT sequence FROM gallery_membership_events WHERE uuid = NEW.event_uuid)
       <= (SELECT sequence FROM gallery_membership_events WHERE uuid = OLD.event_uuid);
END;
CREATE TRIGGER gallery_membership_current AFTER INSERT ON gallery_membership_events
BEGIN
  INSERT INTO gallery_membership_heads(gallery_uuid, media_uuid, event_uuid) VALUES (NEW.gallery_uuid, NEW.media_uuid, NEW.uuid)
  ON CONFLICT(gallery_uuid, media_uuid) DO UPDATE SET event_uuid = excluded.event_uuid;
END;
CREATE TRIGGER post_gallery_active_post BEFORE INSERT ON post_gallery_decisions
WHEN EXISTS (SELECT 1 FROM source_posts WHERE uuid = NEW.post_uuid AND state != 'active')
BEGIN SELECT RAISE(ABORT, 'forgotten post cannot select a gallery'); END;
CREATE TRIGGER source_gallery_galleries_images_insert BEFORE INSERT ON galleries_images
BEGIN
  INSERT INTO gallery_membership_events(uuid, gallery_uuid, media_uuid, state, origin, post_uuid, selection_uuid)
  SELECT (lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' || substr('89ab', (random() & 3) + 1, 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6)))), g.uuid, m.uuid, 'included',
    CASE WHEN w.post_uuid IS NULL THEN 'library' ELSE 'source' END, w.post_uuid, w.selection_uuid
  FROM archive_entities g JOIN galleries ga ON ga.id = g.gallery_id
  JOIN archive_entities m ON m.image_id = NEW.image_id
  LEFT JOIN source_gallery_write_context w ON w.gallery_uuid = g.uuid
  WHERE g.gallery_id = NEW.gallery_id AND
    (ga.origin = 'source' OR EXISTS (SELECT 1 FROM post_gallery_decisions WHERE gallery_uuid = g.uuid));
END;
CREATE TRIGGER source_gallery_galleries_images_delete BEFORE DELETE ON galleries_images
BEGIN
  INSERT INTO gallery_membership_events(uuid, gallery_uuid, media_uuid, state, origin, post_uuid, selection_uuid)
  SELECT (lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' || substr('89ab', (random() & 3) + 1, 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6)))), g.uuid, m.uuid, 'excluded',
    CASE WHEN w.post_uuid IS NULL THEN 'library' ELSE 'source' END, w.post_uuid, w.selection_uuid
  FROM archive_entities g JOIN galleries ga ON ga.id = g.gallery_id
  JOIN archive_entities m ON m.image_id = OLD.image_id
  LEFT JOIN source_gallery_write_context w ON w.gallery_uuid = g.uuid
  WHERE g.gallery_id = OLD.gallery_id AND
    (ga.origin = 'source' OR EXISTS (SELECT 1 FROM post_gallery_decisions WHERE gallery_uuid = g.uuid));
END;
CREATE TRIGGER source_gallery_galleries_images_update BEFORE UPDATE OF gallery_id, image_id ON galleries_images
WHEN OLD.gallery_id != NEW.gallery_id OR OLD.image_id != NEW.image_id
BEGIN
  INSERT INTO gallery_membership_events(uuid, gallery_uuid, media_uuid, state, origin, post_uuid, selection_uuid)
  SELECT (lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' || substr('89ab', (random() & 3) + 1, 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6)))), g.uuid, m.uuid, 'excluded',
    CASE WHEN w.post_uuid IS NULL THEN 'library' ELSE 'source' END, w.post_uuid, w.selection_uuid
  FROM archive_entities g JOIN galleries ga ON ga.id = g.gallery_id
  JOIN archive_entities m ON m.image_id = OLD.image_id
  LEFT JOIN source_gallery_write_context w ON w.gallery_uuid = g.uuid
  WHERE g.gallery_id = OLD.gallery_id AND
    (ga.origin = 'source' OR EXISTS (SELECT 1 FROM post_gallery_decisions WHERE gallery_uuid = g.uuid));
  INSERT INTO gallery_membership_events(uuid, gallery_uuid, media_uuid, state, origin, post_uuid, selection_uuid)
  SELECT (lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' || substr('89ab', (random() & 3) + 1, 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6)))), g.uuid, m.uuid, 'included',
    CASE WHEN w.post_uuid IS NULL THEN 'library' ELSE 'source' END, w.post_uuid, w.selection_uuid
  FROM archive_entities g JOIN galleries ga ON ga.id = g.gallery_id
  JOIN archive_entities m ON m.image_id = NEW.image_id
  LEFT JOIN source_gallery_write_context w ON w.gallery_uuid = g.uuid
  WHERE g.gallery_id = NEW.gallery_id AND
    (ga.origin = 'source' OR EXISTS (SELECT 1 FROM post_gallery_decisions WHERE gallery_uuid = g.uuid));
END;
CREATE TRIGGER source_gallery_scenes_galleries_insert BEFORE INSERT ON scenes_galleries
BEGIN
  INSERT INTO gallery_membership_events(uuid, gallery_uuid, media_uuid, state, origin, post_uuid, selection_uuid)
  SELECT (lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' || substr('89ab', (random() & 3) + 1, 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6)))), g.uuid, m.uuid, 'included',
    CASE WHEN w.post_uuid IS NULL THEN 'library' ELSE 'source' END, w.post_uuid, w.selection_uuid
  FROM archive_entities g JOIN galleries ga ON ga.id = g.gallery_id
  JOIN archive_entities m ON m.scene_id = NEW.scene_id
  LEFT JOIN source_gallery_write_context w ON w.gallery_uuid = g.uuid
  WHERE g.gallery_id = NEW.gallery_id AND
    (ga.origin = 'source' OR EXISTS (SELECT 1 FROM post_gallery_decisions WHERE gallery_uuid = g.uuid));
END;
CREATE TRIGGER source_gallery_scenes_galleries_delete BEFORE DELETE ON scenes_galleries
BEGIN
  INSERT INTO gallery_membership_events(uuid, gallery_uuid, media_uuid, state, origin, post_uuid, selection_uuid)
  SELECT (lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' || substr('89ab', (random() & 3) + 1, 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6)))), g.uuid, m.uuid, 'excluded',
    CASE WHEN w.post_uuid IS NULL THEN 'library' ELSE 'source' END, w.post_uuid, w.selection_uuid
  FROM archive_entities g JOIN galleries ga ON ga.id = g.gallery_id
  JOIN archive_entities m ON m.scene_id = OLD.scene_id
  LEFT JOIN source_gallery_write_context w ON w.gallery_uuid = g.uuid
  WHERE g.gallery_id = OLD.gallery_id AND
    (ga.origin = 'source' OR EXISTS (SELECT 1 FROM post_gallery_decisions WHERE gallery_uuid = g.uuid));
END;
CREATE TRIGGER source_gallery_scenes_galleries_update BEFORE UPDATE OF gallery_id, scene_id ON scenes_galleries
WHEN OLD.gallery_id != NEW.gallery_id OR OLD.scene_id != NEW.scene_id
BEGIN
  INSERT INTO gallery_membership_events(uuid, gallery_uuid, media_uuid, state, origin, post_uuid, selection_uuid)
  SELECT (lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' || substr('89ab', (random() & 3) + 1, 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6)))), g.uuid, m.uuid, 'excluded',
    CASE WHEN w.post_uuid IS NULL THEN 'library' ELSE 'source' END, w.post_uuid, w.selection_uuid
  FROM archive_entities g JOIN galleries ga ON ga.id = g.gallery_id
  JOIN archive_entities m ON m.scene_id = OLD.scene_id
  LEFT JOIN source_gallery_write_context w ON w.gallery_uuid = g.uuid
  WHERE g.gallery_id = OLD.gallery_id AND
    (ga.origin = 'source' OR EXISTS (SELECT 1 FROM post_gallery_decisions WHERE gallery_uuid = g.uuid));
  INSERT INTO gallery_membership_events(uuid, gallery_uuid, media_uuid, state, origin, post_uuid, selection_uuid)
  SELECT (lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)), 2) || '-' || substr('89ab', (random() & 3) + 1, 1) || substr(hex(randomblob(2)), 2) || '-' || hex(randomblob(6)))), g.uuid, m.uuid, 'included',
    CASE WHEN w.post_uuid IS NULL THEN 'library' ELSE 'source' END, w.post_uuid, w.selection_uuid
  FROM archive_entities g JOIN galleries ga ON ga.id = g.gallery_id
  JOIN archive_entities m ON m.scene_id = NEW.scene_id
  LEFT JOIN source_gallery_write_context w ON w.gallery_uuid = g.uuid
  WHERE g.gallery_id = NEW.gallery_id AND
    (ga.origin = 'source' OR EXISTS (SELECT 1 FROM post_gallery_decisions WHERE gallery_uuid = g.uuid));
END;
INSERT INTO native_migration_history(version, name, details) VALUES (1000010, 'Source album galleries and persistent membership intent', '{}');
