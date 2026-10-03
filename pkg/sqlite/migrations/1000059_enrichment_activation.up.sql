CREATE TABLE enrichment_activations (
 uuid TEXT PRIMARY KEY NOT NULL CHECK(length(uuid)=36),
 input_sha256 TEXT NOT NULL CHECK(length(input_sha256)=64),
 plan_sha256 TEXT NOT NULL CHECK(length(plan_sha256)=64),
 snapshot_uuid TEXT REFERENCES automation_enrichment_imports(snapshot_uuid),
 manifest_sha256 TEXT CHECK(manifest_sha256 IS NULL OR length(manifest_sha256)=64),
 plan TEXT NOT NULL CHECK(json_valid(plan) AND json_type(plan)='object' AND length(CAST(plan AS BLOB))<=8388608),
 created_at DATETIME NOT NULL,
 CHECK((snapshot_uuid IS NULL)=(manifest_sha256 IS NULL))
);
CREATE TRIGGER enrichment_activation_immutable BEFORE UPDATE ON enrichment_activations
BEGIN SELECT RAISE(ABORT,'enrichment activation receipts are immutable'); END;
CREATE TABLE enrichment_activation_targets (
 activation_uuid TEXT NOT NULL REFERENCES enrichment_activations(uuid),
 target_uuid TEXT NOT NULL REFERENCES enrichment_targets(uuid),
 previous_revision INTEGER NOT NULL CHECK(previous_revision>0),
 consumed_revision INTEGER NOT NULL CHECK(consumed_revision=previous_revision+1),
 released_target_uuid TEXT NOT NULL REFERENCES enrichment_targets(uuid),
 released_revision INTEGER NOT NULL CHECK(released_revision>0),
 CHECK((released_target_uuid=target_uuid AND released_revision=consumed_revision)
  OR (released_target_uuid!=target_uuid AND released_revision=1)),
 PRIMARY KEY(activation_uuid,target_uuid),
 UNIQUE(target_uuid,previous_revision),
 UNIQUE(released_target_uuid,released_revision),
 FOREIGN KEY(target_uuid,previous_revision) REFERENCES enrichment_target_history(target_uuid,revision),
 FOREIGN KEY(target_uuid,consumed_revision) REFERENCES enrichment_target_history(target_uuid,revision),
 FOREIGN KEY(released_target_uuid,released_revision) REFERENCES enrichment_target_history(target_uuid,revision)
);
CREATE TRIGGER enrichment_activation_target_immutable BEFORE UPDATE ON enrichment_activation_targets
BEGIN SELECT RAISE(ABORT,'enrichment activation target receipts are immutable'); END;
CREATE INDEX automation_enrichment_held ON automation_enrichment_records(snapshot_uuid,ordinal) WHERE disposition='held';

CREATE INDEX automation_enrichment_held_targets ON automation_enrichment_records(snapshot_uuid,target_uuid,ordinal) WHERE disposition='held';

INSERT INTO native_migration_history(version,name,details)
VALUES(1000059,'Activate exact held enrichment revisions with replayable receipts','{}');
