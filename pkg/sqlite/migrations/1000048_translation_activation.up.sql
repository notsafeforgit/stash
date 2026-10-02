CREATE TABLE translation_activations (
 uuid TEXT PRIMARY KEY NOT NULL CHECK(length(uuid)=36),
 input_sha256 TEXT NOT NULL CHECK(length(input_sha256)=64),
 plan_sha256 TEXT NOT NULL CHECK(length(plan_sha256)=64),
 snapshot_uuid TEXT REFERENCES automation_translation_imports(snapshot_uuid),
 manifest_sha256 TEXT CHECK(manifest_sha256 IS NULL OR length(manifest_sha256)=64),
 plan TEXT NOT NULL CHECK(json_valid(plan) AND json_type(plan)='object' AND length(CAST(plan AS BLOB))<=131072),
 created_at DATETIME NOT NULL,
 CHECK((snapshot_uuid IS NULL)=(manifest_sha256 IS NULL))
);
CREATE TRIGGER translation_activation_immutable BEFORE UPDATE ON translation_activations
BEGIN SELECT RAISE(ABORT,'translation activation receipts are immutable'); END;
CREATE TABLE translation_activation_targets (
 activation_uuid TEXT NOT NULL REFERENCES translation_activations(uuid),
 target_uuid TEXT NOT NULL REFERENCES translation_targets(uuid),
 previous_revision INTEGER NOT NULL CHECK(previous_revision>0),
 activated_revision INTEGER NOT NULL CHECK(activated_revision=previous_revision+1),
 PRIMARY KEY(activation_uuid,target_uuid),
 UNIQUE(target_uuid,previous_revision),
 FOREIGN KEY(target_uuid,previous_revision) REFERENCES translation_target_history(target_uuid,revision),
 FOREIGN KEY(target_uuid,activated_revision) REFERENCES translation_target_history(target_uuid,revision)
);
CREATE TRIGGER translation_activation_target_immutable BEFORE UPDATE ON translation_activation_targets
BEGIN SELECT RAISE(ABORT,'translation activation target receipts are immutable'); END;
CREATE INDEX automation_translation_held ON automation_translation_records(snapshot_uuid,ordinal) WHERE disposition='held';

INSERT INTO native_migration_history(version,name,details)
VALUES(1000048,'Activate exact held translation revisions with replayable receipts','{}');
