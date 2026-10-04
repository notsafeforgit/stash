CREATE INDEX automation_discovery_lookup ON automation_discovery_records(post_uuid,collection_uuid,candidate_url,snapshot_uuid,ordinal)
 WHERE disposition='lookup' AND outcome='mapped';
CREATE TABLE enrichment_discovery_resolutions (
 job_uuid TEXT PRIMARY KEY NOT NULL REFERENCES enrichment_job_targets(job_uuid),
 snapshot_uuid TEXT NOT NULL,
 source_ordinal INTEGER NOT NULL CHECK(source_ordinal>0),
 checkpoint_revision INTEGER NOT NULL CHECK(checkpoint_revision>0),
 record_ordinal INTEGER NOT NULL CHECK(record_ordinal>=0),
 post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
 post_revision INTEGER NOT NULL CHECK(post_revision>0),
 namespace TEXT NOT NULL CHECK(namespace IN ('native:reddit','native:twitter')),
 value TEXT NOT NULL CHECK(length(value)>0),
 policy TEXT NOT NULL CHECK(policy='retained-discovery-identity-v1'),
 basis TEXT NOT NULL CHECK(basis IN ('strict-filename-id','captured-account-and-post-id','original-text-date-and-post-id','title-date-and-post-id')),
 evidence_uuid TEXT NOT NULL UNIQUE REFERENCES source_post_identifier_evidence(uuid),
 created_at DATETIME NOT NULL,
 FOREIGN KEY(snapshot_uuid,source_ordinal) REFERENCES automation_discovery_records(snapshot_uuid,ordinal),
 FOREIGN KEY(job_uuid,checkpoint_revision) REFERENCES enrichment_checkpoint_receipts(job_uuid,revision),
 FOREIGN KEY(job_uuid,record_ordinal) REFERENCES enrichment_checkpoint_records(job_uuid,ordinal),
 FOREIGN KEY(job_uuid) REFERENCES enrichment_publications(job_uuid) DEFERRABLE INITIALLY DEFERRED
);
CREATE TRIGGER enrichment_discovery_resolution_immutable BEFORE UPDATE ON enrichment_discovery_resolutions
BEGIN SELECT RAISE(ABORT,'discovery identity resolutions are immutable'); END;
INSERT INTO native_migration_history(version,name,details)
VALUES(1000070,'Verified legacy lookup identities in native enrichment publication','{}');
