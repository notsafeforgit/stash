CREATE TABLE checkpoint_evidence_acceptances (
 uuid TEXT PRIMARY KEY NOT NULL CHECK(length(uuid)=36 AND uuid=lower(uuid)
   AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
   AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
   AND uuid!='00000000-0000-0000-0000-000000000000'),
 snapshot_uuid TEXT NOT NULL,
 ordinal INTEGER NOT NULL CHECK(ordinal>0),
 target_uuid TEXT NOT NULL,
 target_revision INTEGER NOT NULL CHECK(target_revision>0),
 input_sha256 TEXT NOT NULL CHECK(length(input_sha256)=64 AND input_sha256 NOT GLOB '*[^0-9a-f]*'),
 plan_sha256 TEXT NOT NULL CHECK(length(plan_sha256)=64 AND plan_sha256 NOT GLOB '*[^0-9a-f]*'),
 plan TEXT NOT NULL CHECK(json_valid(plan) AND json_type(plan)='object' AND length(CAST(plan AS BLOB))<=1048576),
 created_at DATETIME NOT NULL,
 UNIQUE(snapshot_uuid,ordinal),
 FOREIGN KEY(snapshot_uuid,ordinal) REFERENCES automation_checkpoint_records(snapshot_uuid,ordinal),
 FOREIGN KEY(target_uuid,target_revision) REFERENCES enrichment_target_history(target_uuid,revision)
);
CREATE INDEX checkpoint_evidence_target ON checkpoint_evidence_acceptances(target_uuid,uuid);
CREATE TRIGGER checkpoint_evidence_acceptance_immutable BEFORE UPDATE ON checkpoint_evidence_acceptances
BEGIN SELECT RAISE(ABORT,'accepted legacy evidence is immutable'); END;
CREATE TRIGGER checkpoint_evidence_acceptance_scope BEFORE INSERT ON checkpoint_evidence_acceptances
WHEN NOT EXISTS(SELECT 1 FROM automation_enrichment_records r JOIN automation_checkpoint_records c
 ON c.snapshot_uuid=r.snapshot_uuid AND c.ordinal=r.ordinal
 JOIN enrichment_targets t ON t.uuid=r.target_uuid
 WHERE r.snapshot_uuid=NEW.snapshot_uuid AND r.ordinal=NEW.ordinal
 AND r.target_uuid=NEW.target_uuid AND r.target_revision=NEW.target_revision
 AND r.disposition='staged_review' AND c.outcome='mapped'
 AND t.revision=NEW.target_revision AND t.state='review' AND t.reason='legacy_checkpoint_conversion')
BEGIN SELECT RAISE(ABORT,'legacy evidence requires its original staged review target'); END;

CREATE TABLE checkpoint_evidence_captures (
 acceptance_uuid TEXT NOT NULL REFERENCES checkpoint_evidence_acceptances(uuid),
 body_index INTEGER NOT NULL CHECK(body_index BETWEEN 0 AND 4095),
 capture_uuid TEXT NOT NULL REFERENCES source_captures(uuid),
 payload_sha256 TEXT NOT NULL CHECK(length(payload_sha256)=64 AND payload_sha256 NOT GLOB '*[^0-9a-f]*'),
 PRIMARY KEY(acceptance_uuid,body_index)
);
CREATE INDEX checkpoint_evidence_capture ON checkpoint_evidence_captures(capture_uuid,acceptance_uuid);
CREATE TRIGGER checkpoint_evidence_capture_immutable BEFORE UPDATE ON checkpoint_evidence_captures
BEGIN SELECT RAISE(ABORT,'legacy checkpoint capture bindings are immutable'); END;
CREATE TRIGGER checkpoint_evidence_capture_scope BEFORE INSERT ON checkpoint_evidence_captures
WHEN NOT EXISTS(SELECT 1 FROM checkpoint_evidence_acceptances a
 JOIN enrichment_targets t ON t.uuid=a.target_uuid
 JOIN source_captures c ON c.post_uuid=t.post_uuid AND c.uuid=NEW.capture_uuid
 WHERE a.uuid=NEW.acceptance_uuid AND c.captured_at IS NULL
 AND c.origin='legacy-enrichment' AND c.retention_policy='legacy-retained-v1')
BEGIN SELECT RAISE(ABORT,'legacy checkpoint capture scope does not match'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000062,'Accept reviewed checkpoint evidence as undated native captures','{}');
