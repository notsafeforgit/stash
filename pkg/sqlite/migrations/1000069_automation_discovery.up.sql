CREATE INDEX automation_discovery_input ON automation_snapshot_records(snapshot_uuid,ordinal)
 WHERE source_table IN ('discovery_accounts','discovery_targets','discovery_candidates','maintenance');
CREATE TABLE automation_discovery_imports (
 snapshot_uuid TEXT PRIMARY KEY REFERENCES automation_snapshots(uuid),
 manifest_sha256 TEXT NOT NULL CHECK(length(manifest_sha256)=64 AND manifest_sha256 NOT GLOB '*[^0-9a-f]*'),
 policy TEXT NOT NULL CHECK(policy='automation-discovery-v1'),
 state TEXT NOT NULL CHECK(state IN ('running','mapped','review')),
 last_ordinal INTEGER NOT NULL DEFAULT 0 CHECK(last_ordinal>=0),
 source_records INTEGER NOT NULL CHECK(source_records>=0),
 processed_records INTEGER NOT NULL DEFAULT 0 CHECK(processed_records BETWEEN 0 AND source_records),
 mapped_records INTEGER NOT NULL DEFAULT 0 CHECK(mapped_records>=0),
 review_records INTEGER NOT NULL DEFAULT 0 CHECK(review_records>=0),
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 CHECK(mapped_records+review_records=processed_records),
 CHECK(state='running' OR processed_records=source_records),
 CHECK(state!='mapped' OR review_records=0),
 CHECK(state!='review' OR review_records>0)
);
CREATE TRIGGER automation_discovery_import_guard BEFORE UPDATE ON automation_discovery_imports
WHEN NEW.snapshot_uuid!=OLD.snapshot_uuid OR NEW.manifest_sha256!=OLD.manifest_sha256 OR NEW.policy!=OLD.policy
 OR NEW.source_records!=OLD.source_records OR NEW.created_at!=OLD.created_at OR OLD.state!='running'
 OR NEW.last_ordinal<OLD.last_ordinal OR NEW.processed_records<OLD.processed_records
 OR NEW.mapped_records<OLD.mapped_records OR NEW.review_records<OLD.review_records
BEGIN SELECT RAISE(ABORT,'automation discovery input and completed progress are immutable'); END;
CREATE TABLE automation_discovery_records (
 snapshot_uuid TEXT NOT NULL REFERENCES automation_discovery_imports(snapshot_uuid),
 ordinal INTEGER NOT NULL CHECK(ordinal>0),
 account_uuid TEXT REFERENCES source_accounts(uuid),
 account_ordinal INTEGER,
 target_ordinal INTEGER,
 enrichment_ordinal INTEGER,
 post_uuid TEXT REFERENCES source_posts(uuid),
 post_reference TEXT NOT NULL DEFAULT '' CHECK(length(post_reference) IN (0,69)),
 catalog_snapshot_uuid TEXT REFERENCES catalog_snapshots(uuid),
 collection_uuid TEXT,
 collection_revision INTEGER CHECK(collection_revision IS NULL OR collection_revision=1),
 phase TEXT NOT NULL DEFAULT '' CHECK(phase IN ('','listing','matching','complete')),
 service_scope TEXT NOT NULL DEFAULT '',
 profile_url TEXT NOT NULL DEFAULT '',
 not_before DATETIME,
 historical_attempts INTEGER CHECK(historical_attempts IS NULL OR (typeof(historical_attempts)='integer' AND historical_attempts>=0)),
 historical_pages INTEGER CHECK(historical_pages IS NULL OR (typeof(historical_pages)='integer' AND historical_pages>=0)),
 cursor_sha256 TEXT NOT NULL DEFAULT '' CHECK(cursor_sha256='' OR (length(cursor_sha256)=64 AND cursor_sha256 NOT GLOB '*[^0-9a-f]*')),
 staged_sha256 TEXT NOT NULL DEFAULT '' CHECK(staged_sha256='' OR (length(staged_sha256)=64 AND staged_sha256 NOT GLOB '*[^0-9a-f]*')),
 staged_kind TEXT NOT NULL DEFAULT '' CHECK(staged_kind IN ('','page','detail')),
 evidence_sha256 TEXT NOT NULL DEFAULT '' CHECK(evidence_sha256='' OR (length(evidence_sha256)=64 AND evidence_sha256 NOT GLOB '*[^0-9a-f]*')),
 candidate_url TEXT NOT NULL DEFAULT '',
 candidate_basis TEXT NOT NULL DEFAULT '',
 payload_sha256 TEXT NOT NULL DEFAULT '' CHECK(payload_sha256='' OR (length(payload_sha256)=64 AND payload_sha256 NOT GLOB '*[^0-9a-f]*')),
 maintenance_kind TEXT NOT NULL DEFAULT '' CHECK(maintenance_kind IN ('','inventory_watermark','translation_seed','seed_summary','source_exclusions','pruning_summary')),
 maintenance_time DATETIME,
 watermark_ns TEXT NOT NULL DEFAULT '',
 disposition TEXT NOT NULL CHECK(disposition IN ('held','historical_listing','lookup','historical_completion','source_present','coalesced','candidate','maintenance_history','review')),
 outcome TEXT NOT NULL CHECK(outcome IN ('mapped','review')),
 reason TEXT NOT NULL,
 PRIMARY KEY(snapshot_uuid,ordinal),
 FOREIGN KEY(snapshot_uuid,ordinal) REFERENCES automation_snapshot_records(snapshot_uuid,ordinal),
 FOREIGN KEY(snapshot_uuid,account_ordinal) REFERENCES automation_snapshot_records(snapshot_uuid,ordinal),
 FOREIGN KEY(snapshot_uuid,target_ordinal) REFERENCES automation_snapshot_records(snapshot_uuid,ordinal),
 FOREIGN KEY(snapshot_uuid,enrichment_ordinal) REFERENCES automation_enrichment_records(snapshot_uuid,ordinal),
 FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision),
 CHECK((collection_uuid IS NULL)=(collection_revision IS NULL)),
 CHECK((outcome='review')=(reason!='') AND (outcome='review')=(disposition='review')),
 CHECK(post_uuid IS NULL OR post_reference!=''),
 CHECK((staged_sha256='')=(staged_kind='')),
 CHECK(disposition NOT IN ('lookup','historical_completion','source_present','coalesced') OR (post_uuid IS NOT NULL AND enrichment_ordinal IS NOT NULL)),
 CHECK(disposition!='candidate' OR (post_uuid IS NOT NULL AND target_ordinal IS NOT NULL AND candidate_url!='' AND candidate_basis!='' AND payload_sha256!='')),
 CHECK(disposition!='maintenance_history' OR (maintenance_kind!='' AND payload_sha256!='')),
 CHECK(disposition!='historical_listing' OR (account_uuid IS NOT NULL AND phase='complete')),
 CHECK(disposition!='held' OR account_uuid IS NOT NULL)
);
CREATE INDEX automation_discovery_review ON automation_discovery_records(snapshot_uuid,outcome,ordinal);
CREATE INDEX automation_discovery_accounts ON automation_discovery_records(account_uuid,snapshot_uuid,ordinal) WHERE account_uuid IS NOT NULL;
CREATE INDEX automation_discovery_posts ON automation_discovery_records(post_uuid,snapshot_uuid,ordinal) WHERE post_uuid IS NOT NULL;
CREATE TRIGGER automation_discovery_record_immutable BEFORE UPDATE ON automation_discovery_records
BEGIN SELECT RAISE(ABORT,'automation discovery records are immutable'); END;
CREATE TRIGGER automation_discovery_record_scope BEFORE INSERT ON automation_discovery_records
WHEN NOT EXISTS(SELECT 1 FROM automation_snapshot_records e JOIN automation_discovery_imports i ON i.snapshot_uuid=e.snapshot_uuid
 WHERE e.snapshot_uuid=NEW.snapshot_uuid AND e.ordinal=NEW.ordinal AND i.state='running'
 AND e.source_table IN ('discovery_accounts','discovery_targets','discovery_candidates','maintenance'))
 OR (NEW.account_ordinal IS NOT NULL AND NOT EXISTS(SELECT 1 FROM automation_snapshot_records e
  WHERE e.snapshot_uuid=NEW.snapshot_uuid AND e.ordinal=NEW.account_ordinal AND e.source_table='discovery_accounts'))
 OR (NEW.target_ordinal IS NOT NULL AND NOT EXISTS(SELECT 1 FROM automation_snapshot_records e
  WHERE e.snapshot_uuid=NEW.snapshot_uuid AND e.ordinal=NEW.target_ordinal AND e.source_table='discovery_targets'))
BEGIN SELECT RAISE(ABORT,'automation discovery references must retain their original family'); END;
INSERT INTO native_migration_history(version,name,details)
VALUES(1000069,'Frozen source discovery continuations and maintenance history','{}');
