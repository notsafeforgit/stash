CREATE TABLE enrichment_rebindings (
 uuid TEXT PRIMARY KEY NOT NULL CHECK(length(uuid)=36),
 input_sha256 TEXT NOT NULL CHECK(length(input_sha256)=64),
 plan_sha256 TEXT NOT NULL CHECK(length(plan_sha256)=64),
 activation_uuid TEXT NOT NULL UNIQUE REFERENCES enrichment_activations(uuid),
 plan TEXT NOT NULL CHECK(json_valid(plan) AND json_type(plan)='object' AND length(CAST(plan AS BLOB))<=8388608),
 created_at DATETIME NOT NULL,
 UNIQUE(uuid,activation_uuid)
);
CREATE TRIGGER enrichment_rebinding_immutable BEFORE UPDATE ON enrichment_rebindings
BEGIN SELECT RAISE(ABORT,'enrichment collection review receipts are immutable'); END;

CREATE TABLE enrichment_rebinding_targets (
 rebinding_uuid TEXT NOT NULL,
 activation_uuid TEXT NOT NULL,
 target_uuid TEXT NOT NULL,
 previous_revision INTEGER NOT NULL CHECK(previous_revision>0),
 held_revision INTEGER NOT NULL CHECK(held_revision=previous_revision+1),
 PRIMARY KEY(rebinding_uuid,target_uuid),
 UNIQUE(target_uuid,previous_revision),
 FOREIGN KEY(rebinding_uuid,activation_uuid) REFERENCES enrichment_rebindings(uuid,activation_uuid),
 FOREIGN KEY(activation_uuid,target_uuid) REFERENCES enrichment_activation_targets(activation_uuid,target_uuid),
 FOREIGN KEY(target_uuid,previous_revision) REFERENCES enrichment_target_history(target_uuid,revision),
 FOREIGN KEY(target_uuid,held_revision) REFERENCES enrichment_target_history(target_uuid,revision)
);
CREATE TRIGGER enrichment_rebinding_target_immutable BEFORE UPDATE ON enrichment_rebinding_targets
BEGIN SELECT RAISE(ABORT,'enrichment collection review targets are immutable'); END;
CREATE TRIGGER enrichment_rebinding_target_scope BEFORE INSERT ON enrichment_rebinding_targets
WHEN NOT EXISTS (
 SELECT 1 FROM enrichment_rebindings r
 JOIN enrichment_activation_targets a ON a.activation_uuid=r.activation_uuid AND a.target_uuid=NEW.target_uuid
 JOIN enrichment_target_history old ON old.target_uuid=NEW.target_uuid AND old.revision=NEW.previous_revision
 JOIN enrichment_target_history held ON held.target_uuid=NEW.target_uuid AND held.revision=NEW.held_revision
 WHERE r.uuid=NEW.rebinding_uuid AND r.activation_uuid=NEW.activation_uuid
 AND a.previous_revision=NEW.held_revision AND a.released_target_uuid!=a.target_uuid
 AND old.state='pending' AND old.completion_uuid IS NULL AND held.state='held' AND held.completion_uuid IS NULL
 AND held.reason='collection_rebind' AND held.priority=old.priority AND held.not_before=old.not_before
 AND held.recorded_at=r.created_at
 AND NOT EXISTS(SELECT 1 FROM enrichment_job_targets j WHERE j.target_uuid=NEW.target_uuid AND j.target_revision<=NEW.previous_revision)
)
BEGIN SELECT RAISE(ABORT,'invalid enrichment collection review history'); END;

CREATE INDEX enrichment_targets_pending_scope ON enrichment_targets(collection_uuid,collection_revision,uuid) WHERE state='pending';

INSERT INTO native_migration_history(version,name,details)
VALUES(1000083,'Reviewed collection transitions for unstarted enrichment work','{}');
