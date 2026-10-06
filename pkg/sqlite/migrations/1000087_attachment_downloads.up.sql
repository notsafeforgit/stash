-- Keep original ingestion receipts byte-for-byte while admitting transfer reports.
DROP TRIGGER ingest_scope_receipts;
DROP TRIGGER ingest_root_receipts;
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
 kind TEXT NOT NULL CHECK(kind IN ('source.capture','file.completed','attachment.download')),
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
       (kind='file.completed' AND job_uuid IS NOT NULL AND root_uuid IS NOT NULL) OR
       (kind='attachment.download' AND capture_uuid IS NOT NULL AND job_uuid IS NULL AND root_uuid IS NOT NULL))
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

-- At most one start and one terminal report belong to a transfer. Transfers
-- are ordered within an owned run by a durable producer sequence, not by clocks
-- or delivery order. A terminal report may arrive before its start report.
CREATE TABLE source_attachment_downloads (
 id INTEGER PRIMARY KEY,
 producer_uuid TEXT NOT NULL,
 event_uuid TEXT NOT NULL,
 run_uuid TEXT NOT NULL,
 fence INTEGER NOT NULL CHECK(fence BETWEEN 1 AND 9007199254740991),
 owner_uuid TEXT NOT NULL,
 transfer_sequence INTEGER NOT NULL CHECK(transfer_sequence BETWEEN 1 AND 9007199254740991),
 capture_event_uuid TEXT NOT NULL,
 attachment_uuid TEXT NOT NULL REFERENCES source_attachments(uuid),
 state TEXT NOT NULL CHECK(state IN ('started','downloaded','failed','excluded','skipped')),
 phase INTEGER NOT NULL CHECK(phase=CASE WHEN state='started' THEN 0 ELSE 1 END),
 file_event_uuid TEXT,
 reason_code TEXT NOT NULL DEFAULT '',
 observed_at DATETIME NOT NULL CHECK(length(observed_at) BETWEEN 20 AND 35 AND julianday(observed_at) IS NOT NULL),
 recorded_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 UNIQUE(producer_uuid,event_uuid),
 FOREIGN KEY(producer_uuid,event_uuid) REFERENCES ingest_receipts(producer_uuid,event_uuid) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(producer_uuid,capture_event_uuid) REFERENCES ingest_receipts(producer_uuid,event_uuid),
 FOREIGN KEY(producer_uuid,file_event_uuid) REFERENCES ingest_receipts(producer_uuid,event_uuid),
 FOREIGN KEY(run_uuid,fence) REFERENCES source_run_attempts(run_uuid,fence),
 CHECK((state='started' AND file_event_uuid IS NULL AND reason_code='') OR
       (state='downloaded' AND file_event_uuid IS NOT NULL AND reason_code='') OR
       (state='failed' AND file_event_uuid IS NULL AND reason_code IN ('download_failed','postprocess_failed','source_failure')) OR
       (state='excluded' AND file_event_uuid IS NULL AND reason_code IN ('unsupported_media','filter')) OR
       (state='skipped' AND file_event_uuid IS NULL AND reason_code IN ('archive_entry_without_file','existing_without_file')))
);
CREATE UNIQUE INDEX attachment_download_transfer_phase ON source_attachment_downloads(producer_uuid,capture_event_uuid,attachment_uuid,phase);
CREATE UNIQUE INDEX attachment_download_sequence_phase ON source_attachment_downloads(producer_uuid,run_uuid,fence,transfer_sequence,phase);
CREATE INDEX attachment_download_history ON source_attachment_downloads(attachment_uuid,id);
CREATE TRIGGER attachment_download_immutable BEFORE UPDATE ON source_attachment_downloads
BEGIN SELECT RAISE(ABORT,'attachment download reports are immutable'); END;
CREATE TRIGGER attachment_download_scope BEFORE INSERT ON source_attachment_downloads
BEGIN
 SELECT RAISE(ABORT,'attachment download requires its original capture and owned run attempt') WHERE NOT EXISTS(
  SELECT 1 FROM ingest_receipts c JOIN source_runs r ON r.uuid=NEW.run_uuid AND r.operation='download'
  JOIN source_run_attempts a ON a.run_uuid=r.uuid AND a.fence=NEW.fence
  JOIN source_capture_attachment_manifests m ON m.capture_uuid=c.capture_uuid
  JOIN source_attachment_entries e ON e.manifest_uuid=m.manifest_uuid AND e.attachment_uuid=NEW.attachment_uuid
  WHERE c.producer_uuid=NEW.producer_uuid AND c.event_uuid=NEW.capture_event_uuid AND c.kind='source.capture'
  AND c.run_uuid=r.uuid AND c.collection_uuid=r.collection_uuid AND c.collection_revision=r.collection_revision
  AND c.root_uuid=r.root_uuid AND a.producer_uuid=NEW.producer_uuid AND a.owner_uuid=NEW.owner_uuid);
 SELECT RAISE(ABORT,'attachment download transfer identity is immutable') WHERE EXISTS(
  SELECT 1 FROM source_attachment_downloads d WHERE d.producer_uuid=NEW.producer_uuid
  AND d.capture_event_uuid=NEW.capture_event_uuid AND d.attachment_uuid=NEW.attachment_uuid
  AND (d.run_uuid!=NEW.run_uuid OR d.fence!=NEW.fence OR d.owner_uuid!=NEW.owner_uuid OR d.transfer_sequence!=NEW.transfer_sequence));
 SELECT RAISE(ABORT,'attachment download sequence belongs to another transfer') WHERE EXISTS(
  SELECT 1 FROM source_attachment_downloads d WHERE d.producer_uuid=NEW.producer_uuid
  AND d.run_uuid=NEW.run_uuid AND d.fence=NEW.fence AND d.transfer_sequence=NEW.transfer_sequence
  AND (d.capture_event_uuid!=NEW.capture_event_uuid OR d.attachment_uuid!=NEW.attachment_uuid));
 SELECT RAISE(ABORT,'attachment download event belongs to another receipt kind') WHERE EXISTS(
  SELECT 1 FROM ingest_receipts r WHERE r.producer_uuid=NEW.producer_uuid AND r.event_uuid=NEW.event_uuid AND r.kind!='attachment.download');
 SELECT RAISE(ABORT,'downloaded report requires the exact file verification receipt') WHERE NEW.file_event_uuid IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM ingest_receipts f JOIN ingest_receipts c ON c.producer_uuid=NEW.producer_uuid AND c.event_uuid=NEW.capture_event_uuid
  JOIN archive_jobs j ON j.uuid=f.job_uuid AND j.kind='media.verify'
  WHERE f.producer_uuid=NEW.producer_uuid AND f.event_uuid=NEW.file_event_uuid AND f.kind='file.completed'
  AND f.run_uuid=NEW.run_uuid AND f.collection_uuid=c.collection_uuid AND f.collection_revision=c.collection_revision
  AND f.root_uuid=c.root_uuid AND f.capture_uuid=c.capture_uuid AND f.post_uuid=c.post_uuid
  AND json_extract(j.arguments,'$.publication.source.capture_uuid')=c.capture_uuid
  AND json_extract(j.arguments,'$.publication.source.attachment_uuid')=NEW.attachment_uuid);
END;
CREATE TRIGGER attachment_download_receipt BEFORE INSERT ON ingest_receipts
WHEN NEW.kind='attachment.download'
BEGIN
 SELECT RAISE(ABORT,'attachment download receipt requires its atomic report') WHERE NOT EXISTS(
  SELECT 1 FROM source_attachment_downloads d JOIN ingest_receipts c ON c.producer_uuid=d.producer_uuid AND c.event_uuid=d.capture_event_uuid
  WHERE d.producer_uuid=NEW.producer_uuid AND d.event_uuid=NEW.event_uuid AND d.run_uuid=NEW.run_uuid
  AND c.collection_uuid=NEW.collection_uuid AND c.collection_revision=NEW.collection_revision
  AND c.root_uuid=NEW.root_uuid AND c.post_uuid=NEW.post_uuid AND c.capture_uuid=NEW.capture_uuid);
END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000087,'Retained attachment download reports bound to source run attempts','{}');
