-- Generation is a location fence, not a content hash or media-entity identity.
-- Existing files remain unverified until their bytes are inspected. Adding the
-- default does not hash media or reinterpret existing MD5/oshash fingerprints.
ALTER TABLE files ADD COLUMN generation INTEGER NOT NULL DEFAULT 1 CHECK (typeof(generation)='integer' AND generation > 0);

CREATE TRIGGER file_generation_forward BEFORE UPDATE OF generation ON files
WHEN NEW.generation != OLD.generation + 1
BEGIN SELECT RAISE(ABORT, 'file generation must advance'); END;

CREATE TRIGGER file_generation_location AFTER UPDATE OF basename,parent_folder_id,zip_file_id,size,mod_time ON files
WHEN NEW.basename IS NOT OLD.basename OR NEW.parent_folder_id IS NOT OLD.parent_folder_id
 OR NEW.zip_file_id IS NOT OLD.zip_file_id OR NEW.size IS NOT OLD.size OR NEW.mod_time IS NOT OLD.mod_time
BEGIN UPDATE files SET generation = generation + 1 WHERE id=NEW.id; END;

CREATE TRIGGER file_generation_folder AFTER UPDATE OF path ON folders
WHEN NEW.path IS NOT OLD.path
BEGIN UPDATE files SET generation = generation + 1 WHERE parent_folder_id=NEW.id; END;

CREATE TRIGGER file_generation_fingerprint_insert AFTER INSERT ON files_fingerprints
WHEN NEW.type IN ('md5','oshash')
BEGIN UPDATE files SET generation = generation + 1 WHERE id=NEW.file_id; END;
CREATE TRIGGER file_generation_fingerprint_delete AFTER DELETE ON files_fingerprints
WHEN OLD.type IN ('md5','oshash')
BEGIN UPDATE files SET generation = generation + 1 WHERE id=OLD.file_id; END;
CREATE TRIGGER file_generation_fingerprint_update AFTER UPDATE ON files_fingerprints
WHEN (OLD.type IN ('md5','oshash') OR NEW.type IN ('md5','oshash'))
 AND (NEW.file_id IS NOT OLD.file_id OR NEW.type IS NOT OLD.type OR NEW.fingerprint IS NOT OLD.fingerprint)
BEGIN UPDATE files SET generation = generation + 1 WHERE id IN (OLD.file_id,NEW.file_id); END;

CREATE TABLE media_contents (
 uuid TEXT NOT NULL PRIMARY KEY
 CHECK(length(uuid)=36 AND uuid=lower(uuid) AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-'
 AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-' AND length(replace(uuid,'-',''))=32
 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*' AND uuid!='00000000-0000-0000-0000-000000000000'),
 sha256 TEXT NOT NULL UNIQUE CHECK(length(sha256)=64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
 size INTEGER NOT NULL CHECK(typeof(size)='integer' AND size>=0),
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TRIGGER media_content_immutable BEFORE UPDATE ON media_contents
BEGIN SELECT RAISE(ABORT, 'verified content is immutable'); END;

CREATE TABLE file_content_versions (
 file_uuid TEXT NOT NULL REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
 generation INTEGER NOT NULL CHECK(typeof(generation)='integer' AND generation>0),
 content_uuid TEXT NOT NULL REFERENCES media_contents(uuid),
 root_uuid TEXT NOT NULL REFERENCES media_roots(uuid),
 root_revision INTEGER NOT NULL CHECK(typeof(root_revision)='integer' AND root_revision>0),
 relative_path TEXT NOT NULL CHECK(length(relative_path)>0 AND length(relative_path)<=4096),
 filesystem_identity TEXT NOT NULL CHECK(length(filesystem_identity)>0 AND length(filesystem_identity)<=256),
 mod_time_text TEXT NOT NULL CHECK(length(mod_time_text)>0 AND length(mod_time_text)<=64),
 change_token TEXT NOT NULL CHECK(length(change_token)<=256),
 verified_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY(file_uuid,generation),
 FOREIGN KEY(root_uuid,root_revision) REFERENCES media_root_revisions(root_uuid,revision)
);
CREATE INDEX file_content_versions_content ON file_content_versions(content_uuid,file_uuid,generation);
CREATE INDEX file_content_versions_root ON file_content_versions(root_uuid);

CREATE TRIGGER file_content_version_valid BEFORE INSERT ON file_content_versions
BEGIN
 SELECT RAISE(ABORT, 'file generation is not active') WHERE NOT EXISTS (
   SELECT 1 FROM archive_entities e JOIN files f ON f.id=e.file_id
   JOIN media_contents c ON c.uuid=NEW.content_uuid
   WHERE e.uuid=NEW.file_uuid AND e.kind='file' AND e.state='active'
     AND f.generation=NEW.generation AND f.size=c.size AND f.zip_file_id IS NULL
 );
 SELECT RAISE(ABORT, 'verified file root is inactive') WHERE NOT EXISTS (
   SELECT 1 FROM media_roots r JOIN media_root_revisions d ON d.root_uuid=r.uuid AND d.revision=r.revision
   WHERE r.uuid=NEW.root_uuid AND r.revision=NEW.root_revision AND d.state='active' AND d.server_path IS NOT NULL
 );
END;
CREATE TRIGGER file_content_version_immutable BEFORE UPDATE OF generation,content_uuid,root_uuid,root_revision,relative_path,filesystem_identity,mod_time_text,change_token,verified_at ON file_content_versions
BEGIN SELECT RAISE(ABORT, 'file content verification is immutable'); END;
-- Canonical UUID adoption cascades only after the old primary key is gone.
CREATE TRIGGER file_content_version_identity BEFORE UPDATE OF file_uuid ON file_content_versions
WHEN NEW.file_uuid!=OLD.file_uuid AND EXISTS(SELECT 1 FROM archive_entities WHERE uuid=OLD.file_uuid)
BEGIN SELECT RAISE(ABORT, 'file content identity is immutable'); END;

INSERT INTO native_migration_history(version,name,details)
SELECT 1000018,'Verified content identities and file generations',json_object('files',count(*)) FROM files;
