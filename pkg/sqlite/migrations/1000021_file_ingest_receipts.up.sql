-- Keep event identity shared across kinds. An accepted file receipt points at
-- durable work; it never claims that verification or media import has finished.
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
 FOREIGN KEY(credential_uuid,collection_uuid) REFERENCES ingest_credential_scopes(credential_uuid,collection_uuid),
 FOREIGN KEY(post_uuid,capture_uuid) REFERENCES source_captures(post_uuid,uuid),
 CHECK((post_uuid IS NULL)=(capture_uuid IS NULL)),
 CHECK((kind='source.capture' AND capture_uuid IS NOT NULL AND job_uuid IS NULL) OR
       (kind='file.completed' AND job_uuid IS NOT NULL AND root_uuid IS NOT NULL))
);
INSERT INTO ingest_receipts_next(producer_uuid,event_uuid,digest,credential_uuid,collection_uuid,collection_revision,root_uuid,run_uuid,kind,post_uuid,capture_uuid,result,committed_at)
SELECT producer_uuid,event_uuid,digest,credential_uuid,collection_uuid,collection_revision,root_uuid,run_uuid,kind,post_uuid,capture_uuid,result,committed_at FROM ingest_receipts;
DROP TABLE ingest_receipts;
ALTER TABLE ingest_receipts_next RENAME TO ingest_receipts;
CREATE INDEX ingest_receipts_capture ON ingest_receipts(capture_uuid);
CREATE UNIQUE INDEX ingest_receipts_job ON ingest_receipts(job_uuid) WHERE job_uuid IS NOT NULL;
CREATE TRIGGER ingest_receipt_scope BEFORE INSERT ON ingest_receipts
BEGIN
 SELECT RAISE(ABORT,'ingestion receipt root is outside its credential scope') WHERE NEW.root_uuid IS NOT
  (SELECT root_uuid FROM ingest_credential_scopes WHERE credential_uuid=NEW.credential_uuid AND collection_uuid=NEW.collection_uuid);
 SELECT RAISE(ABORT,'ingestion receipt root differs from its collection revision') WHERE NEW.root_uuid IS NOT
  (SELECT root_uuid FROM source_collection_revisions WHERE collection_uuid=NEW.collection_uuid AND revision=NEW.collection_revision);
 SELECT RAISE(ABORT,'ingestion receipt requires capture provenance') WHERE NEW.capture_uuid IS NOT NULL AND NOT EXISTS
  (SELECT 1 FROM source_collection_captures WHERE capture_uuid=NEW.capture_uuid AND collection_uuid=NEW.collection_uuid AND collection_revision=NEW.collection_revision);
 SELECT RAISE(ABORT,'file ingestion receipt requires media verification work') WHERE NEW.job_uuid IS NOT NULL AND NOT EXISTS
  (SELECT 1 FROM archive_jobs WHERE uuid=NEW.job_uuid AND kind='media.verify');
END;
CREATE TRIGGER ingest_receipt_immutable BEFORE UPDATE ON ingest_receipts
BEGIN SELECT RAISE(ABORT,'ingestion receipts are immutable'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000021,'Durable file intake acknowledgements','{}');
