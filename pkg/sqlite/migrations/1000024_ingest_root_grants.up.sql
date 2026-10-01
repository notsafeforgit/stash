-- Existing credentials acquire no new authority. Broad access requires an
-- explicitly issued grant for a particular registered logical media root.
CREATE TABLE ingest_credential_roots (
 credential_uuid TEXT NOT NULL REFERENCES ingest_credentials(uuid),
 root_uuid TEXT NOT NULL REFERENCES media_roots(uuid),
 PRIMARY KEY(credential_uuid,root_uuid)
);
CREATE TRIGGER ingest_root_immutable BEFORE UPDATE ON ingest_credential_roots
BEGIN SELECT RAISE(ABORT,'ingestion root grants are immutable; issue a new credential'); END;
CREATE INDEX source_collection_target_root ON source_collection_revisions(target_url,root_uuid,collection_uuid,revision)
 WHERE target_url!='';

CREATE TABLE ingest_receipts_next (
 producer_uuid TEXT NOT NULL REFERENCES ingest_producers(uuid),
 event_uuid TEXT NOT NULL CHECK(length(event_uuid)=36 AND event_uuid=lower(event_uuid)
  AND substr(event_uuid,9,1)='-' AND substr(event_uuid,14,1)='-' AND substr(event_uuid,19,1)='-' AND substr(event_uuid,24,1)='-'
  AND length(replace(event_uuid,'-',''))=32 AND replace(event_uuid,'-','') NOT GLOB '*[^0-9a-f]*'
  AND event_uuid!='00000000-0000-0000-0000-000000000000'),
 digest TEXT NOT NULL CHECK(length(digest)=64 AND digest NOT GLOB '*[^0-9a-f]*'),
 credential_uuid TEXT NOT NULL,
 collection_uuid TEXT NOT NULL,
 collection_revision INTEGER NOT NULL,
 root_uuid TEXT REFERENCES media_roots(uuid),
 run_uuid TEXT NOT NULL CHECK(length(run_uuid)=36 AND run_uuid=lower(run_uuid)
  AND substr(run_uuid,9,1)='-' AND substr(run_uuid,14,1)='-' AND substr(run_uuid,19,1)='-' AND substr(run_uuid,24,1)='-'
  AND length(replace(run_uuid,'-',''))=32 AND replace(run_uuid,'-','') NOT GLOB '*[^0-9a-f]*'
  AND run_uuid!='00000000-0000-0000-0000-000000000000'),
 kind TEXT NOT NULL CHECK(kind IN ('source.capture','file.completed')),
 post_uuid TEXT REFERENCES source_posts(uuid),
 capture_uuid TEXT REFERENCES source_captures(uuid),
 job_uuid TEXT REFERENCES archive_jobs(uuid),
 result TEXT NOT NULL CHECK(json_valid(result) AND json_type(result)='object' AND length(result)<=16384),
 committed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY(producer_uuid,event_uuid),
 FOREIGN KEY(producer_uuid,credential_uuid) REFERENCES ingest_credentials(producer_uuid,uuid),
 FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision),
 FOREIGN KEY(post_uuid,capture_uuid) REFERENCES source_captures(post_uuid,uuid),
 CHECK((post_uuid IS NULL)=(capture_uuid IS NULL)),
 CHECK((kind='source.capture' AND capture_uuid IS NOT NULL AND job_uuid IS NULL) OR
       (kind='file.completed' AND job_uuid IS NOT NULL AND root_uuid IS NOT NULL))
);
INSERT INTO ingest_receipts_next(producer_uuid,event_uuid,digest,credential_uuid,collection_uuid,collection_revision,root_uuid,run_uuid,kind,post_uuid,capture_uuid,job_uuid,result,committed_at)
SELECT producer_uuid,event_uuid,digest,credential_uuid,collection_uuid,collection_revision,root_uuid,run_uuid,kind,post_uuid,capture_uuid,job_uuid,result,committed_at FROM ingest_receipts;
DROP TABLE ingest_receipts;
ALTER TABLE ingest_receipts_next RENAME TO ingest_receipts;
CREATE INDEX ingest_receipts_capture ON ingest_receipts(capture_uuid);
CREATE UNIQUE INDEX ingest_receipts_job ON ingest_receipts(job_uuid) WHERE job_uuid IS NOT NULL;
CREATE TRIGGER ingest_receipt_scope BEFORE INSERT ON ingest_receipts
BEGIN
 SELECT RAISE(ABORT,'ingestion receipt root is outside its credential scope') WHERE NOT EXISTS
  (SELECT 1 FROM ingest_credential_scopes WHERE credential_uuid=NEW.credential_uuid
   AND collection_uuid=NEW.collection_uuid AND root_uuid IS NEW.root_uuid)
  AND NOT EXISTS(SELECT 1 FROM ingest_credential_roots WHERE credential_uuid=NEW.credential_uuid AND root_uuid=NEW.root_uuid);
 SELECT RAISE(ABORT,'ingestion receipt root differs from its collection revision') WHERE NEW.root_uuid IS NOT
  (SELECT root_uuid FROM source_collection_revisions WHERE collection_uuid=NEW.collection_uuid AND revision=NEW.collection_revision);
 SELECT RAISE(ABORT,'ingestion receipt requires capture provenance') WHERE NEW.capture_uuid IS NOT NULL AND NOT EXISTS
  (SELECT 1 FROM source_collection_captures WHERE capture_uuid=NEW.capture_uuid AND collection_uuid=NEW.collection_uuid AND collection_revision=NEW.collection_revision);
 SELECT RAISE(ABORT,'file ingestion receipt requires media verification work') WHERE NEW.job_uuid IS NOT NULL AND NOT EXISTS
  (SELECT 1 FROM archive_jobs WHERE uuid=NEW.job_uuid AND kind='media.verify');
END;
CREATE TRIGGER ingest_receipt_immutable BEFORE UPDATE ON ingest_receipts
BEGIN SELECT RAISE(ABORT,'ingestion receipts are immutable'); END;

CREATE TRIGGER ingest_scope_receipts BEFORE DELETE ON ingest_credential_scopes
WHEN EXISTS(SELECT 1 FROM ingest_receipts WHERE credential_uuid=OLD.credential_uuid
 AND collection_uuid=OLD.collection_uuid AND root_uuid IS OLD.root_uuid)
BEGIN SELECT RAISE(ABORT,'ingestion scope has retained receipts'); END;
CREATE TRIGGER ingest_root_receipts BEFORE DELETE ON ingest_credential_roots
WHEN EXISTS(SELECT 1 FROM ingest_receipts WHERE credential_uuid=OLD.credential_uuid AND root_uuid=OLD.root_uuid)
BEGIN SELECT RAISE(ABORT,'ingestion root grant has retained receipts'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000024,'Explicit producer access to registered media roots','{}');
