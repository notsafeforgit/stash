CREATE TABLE source_run_attachment_retries (
 run_uuid TEXT PRIMARY KEY NOT NULL REFERENCES source_runs(uuid),
 producer_uuid TEXT NOT NULL,
 event_uuid TEXT NOT NULL,
 FOREIGN KEY(producer_uuid,event_uuid) REFERENCES source_attachment_downloads(producer_uuid,event_uuid)
);
CREATE TRIGGER source_run_attachment_retry_valid BEFORE INSERT ON source_run_attachment_retries
WHEN NOT EXISTS(SELECT 1 FROM source_attachment_downloads d JOIN source_runs parent ON parent.uuid=d.run_uuid
 JOIN source_runs retry ON retry.uuid=NEW.run_uuid
 WHERE d.producer_uuid=NEW.producer_uuid AND d.event_uuid=NEW.event_uuid AND d.state='failed'
 AND parent.collection_uuid=retry.collection_uuid AND parent.collection_revision=retry.collection_revision
 AND parent.root_uuid=retry.root_uuid AND parent.root_revision=retry.root_revision AND retry.operation='download')
BEGIN SELECT RAISE(ABORT,'invalid attachment retry source'); END;
CREATE TRIGGER source_run_attachment_retry_immutable BEFORE UPDATE ON source_run_attachment_retries
BEGIN SELECT RAISE(ABORT,'attachment retry source is immutable'); END;
CREATE TRIGGER source_run_attachment_retry_retained BEFORE DELETE ON source_run_attachment_retries
BEGIN SELECT RAISE(ABORT,'attachment retry source is retained'); END;
INSERT INTO native_migration_history(version,name,details)
VALUES(1000136,'Independent source runs for failed Reddit attachment downloads','{}');
