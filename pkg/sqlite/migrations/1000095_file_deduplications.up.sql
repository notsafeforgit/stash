-- A completed physical deduplication preserves both verified file lifetimes.
-- It does not merge scenes/images, select metadata, or rewrite source history.
CREATE TABLE file_deduplications (
 uuid TEXT PRIMARY KEY NOT NULL CHECK(length(uuid)=36 AND uuid=lower(uuid)
  AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
  AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
  AND uuid!='00000000-0000-0000-0000-000000000000'),
 signature TEXT NOT NULL CHECK(length(signature)=64 AND signature NOT GLOB '*[^0-9a-f]*'),
 root_uuid TEXT NOT NULL REFERENCES media_roots(uuid),
 keep_path TEXT NOT NULL CHECK(length(keep_path) BETWEEN 1 AND 4096),
 remove_path TEXT NOT NULL CHECK(length(remove_path) BETWEEN 1 AND 4096),
 kept_file_uuid TEXT NOT NULL,
 kept_generation INTEGER NOT NULL,
 removed_file_uuid TEXT NOT NULL,
 removed_generation INTEGER NOT NULL,
 media_uuid TEXT NOT NULL REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
 sha256 TEXT NOT NULL REFERENCES media_contents(sha256),
 proof TEXT NOT NULL CHECK(json_valid(proof) AND json_type(proof)='object' AND length(CAST(proof AS BLOB))<=2097152),
 committed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 FOREIGN KEY(kept_file_uuid,kept_generation) REFERENCES file_content_versions(file_uuid,generation) ON UPDATE CASCADE,
 FOREIGN KEY(removed_file_uuid,removed_generation) REFERENCES file_content_versions(file_uuid,generation) ON UPDATE CASCADE,
 UNIQUE(removed_file_uuid,removed_generation),
 CHECK(kept_file_uuid!=removed_file_uuid AND keep_path!=remove_path)
);
CREATE INDEX file_deduplications_kept ON file_deduplications(kept_file_uuid,kept_generation);
CREATE INDEX file_deduplications_media ON file_deduplications(media_uuid,uuid);
CREATE TRIGGER file_deduplication_verified BEFORE INSERT ON file_deduplications
BEGIN
 SELECT RAISE(ABORT,'deduplication requires equal verified content') WHERE NOT EXISTS (
  SELECT 1 FROM file_content_versions k JOIN file_content_versions d ON d.content_uuid=k.content_uuid
  JOIN media_contents c ON c.uuid=k.content_uuid AND c.sha256=NEW.sha256
  WHERE k.file_uuid=NEW.kept_file_uuid AND k.generation=NEW.kept_generation
   AND d.file_uuid=NEW.removed_file_uuid AND d.generation=NEW.removed_generation);
END;
CREATE TRIGGER file_deduplication_immutable BEFORE UPDATE ON file_deduplications
WHEN NEW.uuid!=OLD.uuid OR NEW.signature!=OLD.signature OR NEW.root_uuid!=OLD.root_uuid
 OR NEW.keep_path!=OLD.keep_path OR NEW.remove_path!=OLD.remove_path
 OR NEW.kept_generation!=OLD.kept_generation OR NEW.removed_generation!=OLD.removed_generation
 OR NEW.sha256!=OLD.sha256 OR NEW.proof!=OLD.proof OR NEW.committed_at!=OLD.committed_at
 OR (NEW.kept_file_uuid!=OLD.kept_file_uuid AND EXISTS(SELECT 1 FROM archive_entities WHERE uuid=OLD.kept_file_uuid))
 OR (NEW.removed_file_uuid!=OLD.removed_file_uuid AND EXISTS(SELECT 1 FROM archive_entities WHERE uuid=OLD.removed_file_uuid))
 OR (NEW.media_uuid!=OLD.media_uuid AND EXISTS(SELECT 1 FROM archive_entities WHERE uuid=OLD.media_uuid))
BEGIN SELECT RAISE(ABORT,'file deduplication receipts are immutable'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000095,'Verified physical deduplication receipts and source provenance','{}');
