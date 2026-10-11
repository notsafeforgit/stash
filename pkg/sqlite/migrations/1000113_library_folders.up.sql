-- Library scanning and ingestion share the existing root identity. An offline
-- folder can retain its configured path without claiming a verified binding.
ALTER TABLE media_root_revisions ADD COLUMN unbound_path TEXT NOT NULL DEFAULT ''
  CHECK(length(unbound_path)<=4096 AND (unbound_path='' OR server_path IS NULL));
ALTER TABLE media_roots ADD COLUMN scan_enabled INTEGER NOT NULL DEFAULT 0 CHECK(scan_enabled IN (0,1));
ALTER TABLE media_roots ADD COLUMN exclude_video INTEGER NOT NULL DEFAULT 0 CHECK(exclude_video IN (0,1));
ALTER TABLE media_roots ADD COLUMN exclude_image INTEGER NOT NULL DEFAULT 0 CHECK(exclude_image IN (0,1));

-- Scan options have their own optimistic revision: changing them cannot revoke
-- the storage revision pinned by a running download or verified file receipt.
ALTER TABLE media_roots ADD COLUMN scan_revision INTEGER NOT NULL DEFAULT 0 CHECK(scan_revision>=0);
CREATE TRIGGER media_root_scan_revision BEFORE UPDATE OF scan_revision,scan_enabled,exclude_video,exclude_image ON media_roots
WHEN NEW.scan_revision!=OLD.scan_revision+1
BEGIN SELECT RAISE(ABORT,'library scan settings require the next revision'); END;

INSERT INTO native_migration_history(version,name,details)
 VALUES(1000113,'Unified library folders and independent scan options','{}');
