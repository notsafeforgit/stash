CREATE TABLE discovery_activations (
 uuid TEXT PRIMARY KEY NOT NULL CHECK(length(uuid)=36),
 input_sha256 TEXT NOT NULL CHECK(length(input_sha256)=64 AND input_sha256 NOT GLOB '*[^0-9a-f]*'),
 plan_sha256 TEXT NOT NULL CHECK(length(plan_sha256)=64 AND plan_sha256 NOT GLOB '*[^0-9a-f]*'),
 snapshot_uuid TEXT NOT NULL REFERENCES automation_discovery_imports(snapshot_uuid),
 manifest_sha256 TEXT NOT NULL CHECK(length(manifest_sha256)=64 AND manifest_sha256 NOT GLOB '*[^0-9a-f]*'),
 listing_uuid TEXT NOT NULL REFERENCES discovery_listings(uuid),
 plan TEXT NOT NULL CHECK(json_valid(plan) AND json_type(plan)='object' AND length(CAST(plan AS BLOB))<=4194304),
 created_at DATETIME NOT NULL
);
CREATE INDEX discovery_activations_listing ON discovery_activations(listing_uuid,uuid);
CREATE TRIGGER discovery_activation_immutable BEFORE UPDATE ON discovery_activations
BEGIN SELECT RAISE(ABORT,'discovery activation receipts are immutable'); END;
CREATE TRIGGER discovery_activation_source BEFORE INSERT ON discovery_activations
WHEN NOT EXISTS(SELECT 1 FROM automation_discovery_imports i JOIN discovery_listing_legacy l ON l.snapshot_uuid=i.snapshot_uuid
 JOIN discovery_listings d ON d.uuid=l.listing_uuid
 JOIN automation_snapshot_records a ON a.snapshot_uuid=l.snapshot_uuid AND a.ordinal=l.account_ordinal
 WHERE i.snapshot_uuid=NEW.snapshot_uuid AND i.state!='running' AND i.manifest_sha256=NEW.manifest_sha256
 AND d.uuid=NEW.listing_uuid AND d.digest=json_extract(NEW.plan,'$.listing_sha256')
 AND a.data_sha256=json_extract(NEW.plan,'$.account_sha256'))
BEGIN SELECT RAISE(ABORT,'discovery activation requires its reviewed source'); END;

CREATE TABLE discovery_activation_targets (
 activation_uuid TEXT NOT NULL REFERENCES discovery_activations(uuid),
 target_uuid TEXT NOT NULL REFERENCES discovery_match_targets(uuid),
 PRIMARY KEY(activation_uuid,target_uuid)
);
CREATE INDEX discovery_activation_targets_target ON discovery_activation_targets(target_uuid,activation_uuid);
CREATE TRIGGER discovery_activation_target_immutable BEFORE UPDATE ON discovery_activation_targets
BEGIN SELECT RAISE(ABORT,'discovery activation target receipts are immutable'); END;
CREATE TRIGGER discovery_activation_target_scope BEFORE INSERT ON discovery_activation_targets
WHEN NOT EXISTS(SELECT 1 FROM discovery_activations a JOIN discovery_match_targets t ON t.listing_uuid=a.listing_uuid
 JOIN json_each(a.plan,'$.entries') e ON json_extract(e.value,'$.target_uuid')=t.uuid
 WHERE a.uuid=NEW.activation_uuid AND t.uuid=NEW.target_uuid
 AND t.source_ordinal=json_extract(e.value,'$.source_ordinal') AND t.source_sha256=json_extract(e.value,'$.source_sha256')
 AND t.post_uuid=json_extract(e.value,'$.post_uuid') AND t.post_revision=json_extract(e.value,'$.post_revision'))
BEGIN SELECT RAISE(ABORT,'discovery activation target differs from its review'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000073,'Reviewed discovery activation and replayable target bindings','{}');
