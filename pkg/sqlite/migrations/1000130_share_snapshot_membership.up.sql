-- Snapshot membership is captured once. Preparation only fills file proofs;
-- recipients cannot use the grant until every media row has been prepared.
ALTER TABLE shares ADD COLUMN preparation_state TEXT NOT NULL DEFAULT 'READY'
 CHECK(preparation_state IN ('PREPARING','READY','FAILED'));
ALTER TABLE shares ADD COLUMN preparation_revision INTEGER NOT NULL DEFAULT 1 CHECK(preparation_revision>=0);
ALTER TABLE shares ADD COLUMN prepared_count INTEGER NOT NULL DEFAULT 0 CHECK(prepared_count>=0);
ALTER TABLE shares ADD COLUMN media_count INTEGER NOT NULL DEFAULT 0 CHECK(media_count>=0);
ALTER TABLE shares ADD COLUMN selection_count INTEGER NOT NULL DEFAULT 0 CHECK(selection_count>=0);
ALTER TABLE shares ADD COLUMN preparation_error TEXT NOT NULL DEFAULT '';
CREATE INDEX shares_preparing ON shares(created_at,id) WHERE preparation_state='PREPARING' AND revoked_at=0;

CREATE TABLE share_snapshot_entries (
 share_id TEXT NOT NULL REFERENCES shares(id) ON DELETE CASCADE,
 key TEXT NOT NULL,
 position INTEGER NOT NULL CHECK(position>0),
 kind TEXT NOT NULL CHECK(kind IN ('SCENE','IMAGE','GALLERY')),
 entity_id INTEGER NOT NULL,
 entity_uuid TEXT NOT NULL,
 title TEXT NOT NULL,
 media_count INTEGER NOT NULL DEFAULT 0 CHECK(media_count>=0),
 PRIMARY KEY(share_id,key), UNIQUE(share_id,position)
);
CREATE INDEX share_snapshot_entries_kind ON share_snapshot_entries(share_id,kind,position);
CREATE TABLE share_snapshot_media (
 share_id TEXT NOT NULL REFERENCES shares(id) ON DELETE CASCADE,
 key TEXT NOT NULL,
 position INTEGER NOT NULL CHECK(position>0),
 media BLOB NOT NULL CHECK(json_valid(media)),
 file_uuid TEXT NOT NULL DEFAULT '',
 file_generation INTEGER NOT NULL DEFAULT 0,
 expected_size INTEGER NOT NULL DEFAULT 0,
 expected_mtime INTEGER NOT NULL DEFAULT 0,
 prepared INTEGER NOT NULL DEFAULT 0 CHECK(prepared IN (0,1)),
 PRIMARY KEY(share_id,key), UNIQUE(share_id,position)
);
CREATE INDEX share_snapshot_media_preparation ON share_snapshot_media(share_id,prepared,position);
CREATE TABLE share_snapshot_members (
 share_id TEXT NOT NULL,
 entry_key TEXT NOT NULL,
 media_key TEXT NOT NULL,
 position INTEGER NOT NULL CHECK(position>0),
 PRIMARY KEY(share_id,entry_key,media_key), UNIQUE(share_id,entry_key,position),
 FOREIGN KEY(share_id,entry_key) REFERENCES share_snapshot_entries(share_id,key) ON DELETE CASCADE,
 FOREIGN KEY(share_id,media_key) REFERENCES share_snapshot_media(share_id,key) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX share_snapshot_members_media ON share_snapshot_members(share_id,media_key,entry_key);

INSERT INTO share_snapshot_entries(share_id,key,position,kind,entity_id,entity_uuid,title,media_count)
SELECT s.id,coalesce(nullif(json_extract(e.value,'$.key'),''),'gallery-'||(CAST(e.key AS INTEGER)+1)),
 CAST(e.key AS INTEGER)+1,json_extract(e.value,'$.kind'),json_extract(e.value,'$.id'),
 coalesce(json_extract(e.value,'$.uuid'),''),coalesce(json_extract(e.value,'$.title'),''),
 coalesce(json_array_length(e.value,'$.media_keys'),0)
FROM shares s,json_each(s.snapshot,'$.entries') e;
INSERT INTO share_snapshot_media(share_id,key,position,media,prepared)
SELECT s.id,json_extract(m.value,'$.key'),CAST(m.key AS INTEGER)+1,m.value,1
FROM shares s,json_each(s.snapshot,'$.media') m;
INSERT INTO share_snapshot_members(share_id,entry_key,media_key,position)
SELECT s.id,coalesce(nullif(json_extract(e.value,'$.key'),''),'gallery-'||(CAST(e.key AS INTEGER)+1)),
 m.value,CAST(m.key AS INTEGER)+1
FROM shares s,json_each(s.snapshot,'$.entries') e,json_each(e.value,'$.media_keys') m
WHERE EXISTS(SELECT 1 FROM share_snapshot_media sm WHERE sm.share_id=s.id AND sm.key=m.value);
UPDATE share_snapshot_entries SET media_count=(SELECT count(*) FROM share_snapshot_members m WHERE m.share_id=share_snapshot_entries.share_id AND m.entry_key=share_snapshot_entries.key);
UPDATE shares SET media_count=(SELECT count(*) FROM share_snapshot_media m WHERE m.share_id=shares.id),
 prepared_count=(SELECT count(*) FROM share_snapshot_media m WHERE m.share_id=shares.id),
 selection_count=(SELECT count(*) FROM share_snapshot_entries e WHERE e.share_id=shares.id);
ALTER TABLE shares DROP COLUMN snapshot;

CREATE TRIGGER share_snapshot_entry_insert BEFORE INSERT ON share_snapshot_entries
WHEN NOT EXISTS(SELECT 1 FROM shares WHERE id=NEW.share_id AND preparation_state='PREPARING' AND preparation_revision=0)
BEGIN SELECT RAISE(ABORT,'snapshot admission is closed'); END;
CREATE TRIGGER share_snapshot_member_insert BEFORE INSERT ON share_snapshot_members
WHEN NOT EXISTS(SELECT 1 FROM shares WHERE id=NEW.share_id AND preparation_state='PREPARING' AND preparation_revision=0)
BEGIN SELECT RAISE(ABORT,'snapshot admission is closed'); END;
CREATE TRIGGER share_snapshot_media_insert BEFORE INSERT ON share_snapshot_media
WHEN NOT EXISTS(SELECT 1 FROM shares WHERE id=NEW.share_id AND preparation_state='PREPARING' AND preparation_revision=0)
BEGIN SELECT RAISE(ABORT,'snapshot admission is closed'); END;
CREATE TRIGGER share_snapshot_entry_delete BEFORE DELETE ON share_snapshot_entries
WHEN EXISTS(SELECT 1 FROM shares WHERE id=OLD.share_id)
BEGIN SELECT RAISE(ABORT,'snapshot entries are immutable'); END;
CREATE TRIGGER share_snapshot_member_delete BEFORE DELETE ON share_snapshot_members
WHEN EXISTS(SELECT 1 FROM shares WHERE id=OLD.share_id)
BEGIN SELECT RAISE(ABORT,'snapshot membership is immutable'); END;
CREATE TRIGGER share_snapshot_media_delete BEFORE DELETE ON share_snapshot_media
WHEN EXISTS(SELECT 1 FROM shares WHERE id=OLD.share_id)
BEGIN SELECT RAISE(ABORT,'snapshot media is immutable'); END;
CREATE TRIGGER share_snapshot_admission_closed BEFORE UPDATE OF preparation_revision ON shares
WHEN OLD.preparation_revision>0 AND NEW.preparation_revision<=OLD.preparation_revision
BEGIN SELECT RAISE(ABORT,'snapshot admission is closed'); END;
CREATE TRIGGER share_snapshot_entry_immutable BEFORE UPDATE ON share_snapshot_entries
BEGIN SELECT RAISE(ABORT,'snapshot entries are immutable'); END;
CREATE TRIGGER share_snapshot_member_immutable BEFORE UPDATE ON share_snapshot_members
BEGIN SELECT RAISE(ABORT,'snapshot membership is immutable'); END;
CREATE TRIGGER share_snapshot_media_immutable BEFORE UPDATE ON share_snapshot_media
WHEN OLD.prepared=1 OR NEW.share_id<>OLD.share_id OR NEW.key<>OLD.key OR NEW.position<>OLD.position
 OR NEW.file_uuid<>OLD.file_uuid OR NEW.file_generation<>OLD.file_generation
 OR NEW.expected_size<>OLD.expected_size OR NEW.expected_mtime<>OLD.expected_mtime
 OR NEW.prepared<>1 OR json_remove(NEW.media,'$.fingerprint')<>json_remove(OLD.media,'$.fingerprint')
BEGIN SELECT RAISE(ABORT,'snapshot media is immutable'); END;
CREATE TRIGGER share_snapshot_counts_immutable BEFORE UPDATE OF media_count,selection_count ON shares
WHEN OLD.preparation_revision>0 AND (NEW.media_count<>OLD.media_count OR NEW.selection_count<>OLD.selection_count)
BEGIN SELECT RAISE(ABORT,'snapshot counts are immutable'); END;
CREATE TRIGGER share_snapshot_checkpoint BEFORE UPDATE OF prepared_count ON shares
WHEN NEW.prepared_count<OLD.prepared_count OR NEW.prepared_count>NEW.media_count
 OR EXISTS(SELECT 1 FROM share_snapshot_media m WHERE m.share_id=NEW.id AND m.prepared=0 AND m.position<=NEW.prepared_count)
 OR EXISTS(SELECT 1 FROM share_snapshot_media m WHERE m.share_id=NEW.id AND m.prepared=1 AND m.position>NEW.prepared_count)
BEGIN SELECT RAISE(ABORT,'invalid snapshot checkpoint'); END;
CREATE TRIGGER share_snapshot_ready BEFORE UPDATE OF preparation_state ON shares
WHEN NEW.preparation_state='READY' AND (NEW.prepared_count<>NEW.media_count OR
 EXISTS(SELECT 1 FROM share_snapshot_media m WHERE m.share_id=NEW.id AND m.prepared=0))
BEGIN SELECT RAISE(ABORT,'snapshot preparation is incomplete'); END;
CREATE TRIGGER share_snapshot_sealed BEFORE UPDATE OF preparation_state ON shares
WHEN OLD.preparation_state='READY' AND NEW.preparation_state<>'READY'
BEGIN SELECT RAISE(ABORT,'published snapshots are immutable'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000130,'Indexed snapshot membership and recoverable preparation','{}');
