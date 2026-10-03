-- Review authorization is separate from worker admission. Original held
-- targets, captures and staging remain intact until a native job consumes it.
CREATE TABLE checkpoint_handoffs (
 uuid TEXT PRIMARY KEY NOT NULL CHECK(length(uuid)=36 AND uuid=lower(uuid)
   AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
   AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
   AND uuid!='00000000-0000-0000-0000-000000000000'),
 evidence_uuid TEXT NOT NULL REFERENCES checkpoint_evidence_acceptances(uuid),
 target_uuid TEXT NOT NULL,
 target_revision INTEGER NOT NULL CHECK(target_revision>0),
 collection_uuid TEXT NOT NULL,
 collection_revision INTEGER NOT NULL CHECK(collection_revision>0),
 input_sha256 TEXT NOT NULL CHECK(length(input_sha256)=64 AND input_sha256 NOT GLOB '*[^0-9a-f]*'),
 plan_sha256 TEXT NOT NULL CHECK(length(plan_sha256)=64 AND plan_sha256 NOT GLOB '*[^0-9a-f]*'),
 seed_sha256 TEXT NOT NULL CHECK(length(seed_sha256)=64 AND seed_sha256 NOT GLOB '*[^0-9a-f]*'),
 plan TEXT NOT NULL CHECK(json_valid(plan) AND json_type(plan)='object' AND length(CAST(plan AS BLOB))<=1048576),
 created_at DATETIME NOT NULL,
 FOREIGN KEY(target_uuid,target_revision) REFERENCES enrichment_target_history(target_uuid,revision),
 FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision)
);
CREATE INDEX checkpoint_handoffs_evidence ON checkpoint_handoffs(evidence_uuid,uuid);
CREATE TRIGGER checkpoint_handoff_immutable BEFORE UPDATE ON checkpoint_handoffs
BEGIN SELECT RAISE(ABORT,'checkpoint handoff review is immutable'); END;
CREATE TRIGGER checkpoint_handoff_scope BEFORE INSERT ON checkpoint_handoffs
WHEN NOT EXISTS(SELECT 1 FROM checkpoint_evidence_acceptances a
 JOIN enrichment_targets t ON t.uuid=a.target_uuid
 JOIN source_posts p ON p.uuid=t.post_uuid
 JOIN source_collections c ON c.uuid=t.collection_uuid
 JOIN source_collection_revisions d ON d.collection_uuid=c.uuid AND d.revision=c.revision
 WHERE a.uuid=NEW.evidence_uuid AND t.uuid=NEW.target_uuid AND a.target_revision=NEW.target_revision
 AND t.revision=NEW.target_revision AND t.state='review' AND t.reason='legacy_checkpoint_conversion'
 AND p.state='active' AND c.uuid=NEW.collection_uuid AND c.revision=NEW.collection_revision AND d.state='active')
BEGIN SELECT RAISE(ABORT,'checkpoint handoff requires original review and active destination'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000064,'Review exact retained checkpoint seeds for native execution','{}');
