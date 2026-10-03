-- Preserve native capture completions and distinguish imported historical proof.
DROP TRIGGER enrichment_completion_immutable;
DROP TRIGGER enrichment_completion_scope;
DROP TRIGGER enrichment_completion_capture_scope;
DROP TRIGGER enrichment_target_scope;
-- SQLite validates dependent triggers during the table replacement. Preserve
-- the publication guards unchanged and restore them before leaving migration.
DROP TRIGGER enrichment_publication_scope;
DROP TRIGGER enrichment_published_record_scope;
DROP TRIGGER enrichment_job_success;
CREATE TABLE enrichment_completions_next (
 uuid TEXT PRIMARY KEY NOT NULL CHECK(length(uuid)=36 AND uuid=lower(uuid)
  AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
  AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
  AND uuid!='00000000-0000-0000-0000-000000000000'),
 target_uuid TEXT NOT NULL UNIQUE REFERENCES enrichment_targets(uuid),
 expected_revision INTEGER NOT NULL CHECK(typeof(expected_revision)='integer' AND expected_revision>0),
 request_digest TEXT NOT NULL CHECK(length(request_digest)=64 AND request_digest NOT GLOB '*[^0-9a-f]*'),
 capture_count INTEGER NOT NULL CHECK(typeof(capture_count)='integer' AND capture_count BETWEEN 0 AND 1024),
 legacy_receipt_uuid TEXT REFERENCES source_enrichment_receipts(uuid),
 legacy_capture_uuid TEXT REFERENCES source_captures(uuid),
 created_at DATETIME NOT NULL,
 CHECK((legacy_receipt_uuid IS NULL AND legacy_capture_uuid IS NULL AND capture_count>=1)
  OR (legacy_receipt_uuid IS NOT NULL AND legacy_capture_uuid IS NULL AND capture_count=0)
  OR (legacy_receipt_uuid IS NULL AND legacy_capture_uuid IS NOT NULL AND capture_count=0)),
 FOREIGN KEY(target_uuid,uuid) REFERENCES enrichment_targets(uuid,completion_uuid) DEFERRABLE INITIALLY DEFERRED
);

INSERT INTO enrichment_completions_next(uuid,target_uuid,expected_revision,request_digest,capture_count,created_at)
 SELECT uuid,target_uuid,expected_revision,request_digest,capture_count,created_at FROM enrichment_completions;
DROP TABLE enrichment_completions;
ALTER TABLE enrichment_completions_next RENAME TO enrichment_completions;
CREATE TRIGGER enrichment_publication_scope BEFORE INSERT ON enrichment_publications
WHEN NOT EXISTS(SELECT 1 FROM archive_jobs j
 JOIN enrichment_job_targets b ON b.job_uuid=j.uuid
 JOIN enrichment_checkpoints h ON h.job_uuid=j.uuid AND h.revision=NEW.checkpoint_revision
 JOIN enrichment_checkpoint_receipts r ON r.job_uuid=h.job_uuid AND r.revision=h.revision AND r.pending_count=0
 JOIN enrichment_completions e ON e.uuid=NEW.completion_uuid AND e.target_uuid=b.target_uuid AND e.expected_revision=b.target_revision
 JOIN enrichment_targets t ON t.uuid=e.target_uuid AND t.state='completed' AND t.completion_uuid=e.uuid
 WHERE j.uuid=NEW.job_uuid AND j.kind='post.enrich' AND j.state='running' AND j.fence=NEW.fence
 AND r.fence<=NEW.fence AND e.created_at=NEW.created_at AND r.created_at<=NEW.created_at)
BEGIN SELECT RAISE(ABORT,'enrichment publication requires the owned checkpoint and exact target completion'); END;
CREATE TRIGGER enrichment_published_record_scope BEFORE INSERT ON enrichment_published_records
WHEN NOT EXISTS(SELECT 1 FROM enrichment_publications p
 JOIN archive_jobs j ON j.uuid=p.job_uuid AND j.state='running' AND j.fence=p.fence
 JOIN enrichment_completions e ON e.uuid=p.completion_uuid
 JOIN enrichment_completion_captures c ON c.completion_uuid=e.uuid AND c.capture_uuid=NEW.capture_uuid
 WHERE p.job_uuid=NEW.job_uuid)
BEGIN SELECT RAISE(ABORT,'enrichment published record must belong to its completion'); END;
CREATE TRIGGER enrichment_job_success BEFORE UPDATE ON archive_jobs
WHEN NEW.kind='post.enrich' AND NEW.state='succeeded' AND NOT EXISTS(
 SELECT 1 FROM enrichment_publications p
 JOIN enrichment_checkpoint_receipts r ON r.job_uuid=p.job_uuid AND r.revision=p.checkpoint_revision
 JOIN enrichment_completions e ON e.uuid=p.completion_uuid
 WHERE p.job_uuid=NEW.uuid AND p.fence=NEW.fence
 AND r.record_count=(SELECT count(*) FROM enrichment_published_records c WHERE c.job_uuid=p.job_uuid)
 AND e.capture_count=(SELECT count(DISTINCT capture_uuid) FROM enrichment_published_records c WHERE c.job_uuid=p.job_uuid))
BEGIN SELECT RAISE(ABORT,'enrichment success requires complete native capture publication'); END;
CREATE INDEX enrichment_completion_legacy_receipt ON enrichment_completions(legacy_receipt_uuid) WHERE legacy_receipt_uuid IS NOT NULL;
CREATE INDEX enrichment_completion_legacy_capture ON enrichment_completions(legacy_capture_uuid) WHERE legacy_capture_uuid IS NOT NULL;
CREATE TRIGGER enrichment_completion_immutable BEFORE UPDATE ON enrichment_completions
BEGIN SELECT RAISE(ABORT,'enrichment completion evidence is immutable'); END;
CREATE TRIGGER enrichment_completion_scope BEFORE INSERT ON enrichment_completions
WHEN NOT EXISTS(SELECT 1 FROM enrichment_targets t WHERE t.uuid=NEW.target_uuid AND t.revision=NEW.expected_revision AND t.updated_at<=NEW.created_at
 AND ((NEW.legacy_receipt_uuid IS NULL AND NEW.legacy_capture_uuid IS NULL AND t.state='pending' AND t.not_before<=NEW.created_at)
  OR ((NEW.legacy_receipt_uuid IS NOT NULL OR NEW.legacy_capture_uuid IS NOT NULL)
   AND t.origin='migration' AND t.state='held' AND t.collection_revision=1
   AND EXISTS(SELECT 1 FROM source_collection_revisions c WHERE c.collection_uuid=t.collection_uuid AND c.revision=1 AND c.origin='migration')
   AND ((NEW.legacy_receipt_uuid IS NOT NULL AND EXISTS(SELECT 1 FROM source_enrichment_receipts r WHERE r.uuid=NEW.legacy_receipt_uuid
     AND r.post_uuid=t.post_uuid AND r.collection_uuid=t.collection_uuid AND r.collection_revision=t.collection_revision))
    OR (NEW.legacy_capture_uuid IS NOT NULL AND EXISTS(SELECT 1 FROM source_captures c JOIN source_collection_captures b ON b.capture_uuid=c.uuid
     WHERE c.uuid=NEW.legacy_capture_uuid AND c.post_uuid=t.post_uuid AND c.origin='gallery-dl'
     AND b.collection_uuid=t.collection_uuid AND b.collection_revision=t.collection_revision))))))
BEGIN SELECT RAISE(ABORT,'enrichment completion requires its exact target revision and proof scope'); END;
CREATE TRIGGER enrichment_completion_capture_scope BEFORE INSERT ON enrichment_completion_captures
WHEN NOT EXISTS(SELECT 1 FROM enrichment_completions e JOIN enrichment_targets t ON t.uuid=e.target_uuid
 JOIN source_captures c ON c.uuid=NEW.capture_uuid AND c.post_uuid=t.post_uuid
 JOIN source_collection_captures b ON b.capture_uuid=c.uuid AND b.collection_uuid=t.collection_uuid AND b.collection_revision=t.collection_revision
 WHERE e.uuid=NEW.completion_uuid AND c.origin IN ('gallery-dl','gallery-dl-enrichment')
 AND e.legacy_receipt_uuid IS NULL AND e.legacy_capture_uuid IS NULL AND t.state='pending' AND t.revision=e.expected_revision
 AND e.capture_count>(SELECT count(*) FROM enrichment_completion_captures WHERE completion_uuid=e.uuid))
BEGIN SELECT RAISE(ABORT,'enrichment completion capture is outside its post or collection scope'); END;
CREATE TRIGGER enrichment_target_scope BEFORE UPDATE ON enrichment_targets
WHEN (NEW.state IN ('pending','completed') AND NOT EXISTS(SELECT 1 FROM source_posts WHERE uuid=NEW.post_uuid AND state='active'))
 OR ((NEW.state='pending' OR (NEW.state='completed' AND EXISTS(SELECT 1 FROM enrichment_completions e WHERE e.uuid=NEW.completion_uuid
   AND e.legacy_receipt_uuid IS NULL AND e.legacy_capture_uuid IS NULL)))
  AND NOT EXISTS(SELECT 1 FROM source_collections c JOIN source_collection_revisions r ON r.collection_uuid=c.uuid AND r.revision=c.revision
   WHERE c.uuid=NEW.collection_uuid AND c.revision=NEW.collection_revision AND r.state='active'))
 OR (NEW.state='completed' AND NOT EXISTS(SELECT 1 FROM enrichment_completions e
  WHERE e.uuid=NEW.completion_uuid AND e.target_uuid=NEW.uuid AND e.expected_revision=OLD.revision AND e.created_at=NEW.updated_at
  AND e.capture_count=(SELECT count(*) FROM enrichment_completion_captures WHERE completion_uuid=e.uuid)
  AND ((e.legacy_receipt_uuid IS NULL AND e.legacy_capture_uuid IS NULL AND OLD.state='pending' AND NEW.not_before<=NEW.updated_at)
   OR ((e.legacy_receipt_uuid IS NOT NULL OR e.legacy_capture_uuid IS NOT NULL) AND OLD.state='held' AND NEW.origin='migration'))))
BEGIN SELECT RAISE(ABORT,'enrichment completion or active source scope does not match'); END;

CREATE INDEX automation_enrichment_input ON automation_snapshot_records(snapshot_uuid,ordinal)
 WHERE source_table IN ('enrichment_cooldowns','enrichment_jobs','enrichment_seed_progress','enrichment_source_progress');
CREATE TABLE automation_enrichment_imports (
 snapshot_uuid TEXT PRIMARY KEY REFERENCES automation_snapshots(uuid),
 manifest_sha256 TEXT NOT NULL CHECK(length(manifest_sha256)=64 AND manifest_sha256 NOT GLOB '*[^0-9a-f]*'),
 policy TEXT NOT NULL CHECK(policy='automation-enrichment-v1'),
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
CREATE TRIGGER automation_enrichment_import_guard BEFORE UPDATE ON automation_enrichment_imports
WHEN NEW.snapshot_uuid!=OLD.snapshot_uuid OR NEW.manifest_sha256!=OLD.manifest_sha256 OR NEW.policy!=OLD.policy
 OR NEW.source_records!=OLD.source_records OR NEW.created_at!=OLD.created_at OR OLD.state!='running'
 OR NEW.last_ordinal<OLD.last_ordinal OR NEW.processed_records<OLD.processed_records
 OR NEW.mapped_records<OLD.mapped_records OR NEW.review_records<OLD.review_records
BEGIN SELECT RAISE(ABORT,'automation enrichment input and completed progress are immutable'); END;
CREATE TABLE automation_enrichment_records (
 snapshot_uuid TEXT NOT NULL REFERENCES automation_enrichment_imports(snapshot_uuid),
 ordinal INTEGER NOT NULL CHECK(ordinal>0),
 post_uuid TEXT REFERENCES source_posts(uuid),
 post_reference TEXT NOT NULL DEFAULT '' CHECK(length(post_reference) IN (0,69)),
 catalog_snapshot_uuid TEXT REFERENCES catalog_snapshots(uuid),
 collection_uuid TEXT,
 collection_revision INTEGER CHECK(collection_revision IS NULL OR collection_revision=1),
 url_uuid TEXT REFERENCES source_post_urls(uuid),
 url_evidence_uuid TEXT REFERENCES source_post_url_evidence(uuid),
 target_uuid TEXT REFERENCES enrichment_targets(uuid),
 target_revision INTEGER,
 receipt_uuid TEXT REFERENCES source_enrichment_receipts(uuid),
 capture_uuid TEXT REFERENCES source_captures(uuid),
 completion_uuid TEXT REFERENCES enrichment_completions(uuid),
 alias_ordinal INTEGER,
 historical_attempts INTEGER CHECK(historical_attempts IS NULL OR (typeof(historical_attempts)='integer' AND historical_attempts>=0)),
 service_scope TEXT NOT NULL DEFAULT '',
 not_before DATETIME,
 staged_sha256 TEXT NOT NULL DEFAULT '' CHECK(staged_sha256='' OR (length(staged_sha256)=64 AND staged_sha256 NOT GLOB '*[^0-9a-f]*')),
 cooldown_kind TEXT NOT NULL DEFAULT '' CHECK(cooldown_kind IN ('','platform','account')),
 cooldown_value TEXT NOT NULL DEFAULT '',
 cooldown_until DATETIME,
 cooldown_reason TEXT NOT NULL DEFAULT '',
 seed_last_post_key TEXT,
 seed_complete INTEGER CHECK(seed_complete IS NULL OR seed_complete IN (0,1)),
 seed_counts TEXT CHECK(seed_counts IS NULL OR (json_valid(seed_counts) AND json_type(seed_counts)='object' AND length(CAST(seed_counts AS BLOB))<=65536)),
 source_platform TEXT NOT NULL DEFAULT '',
 source_last_attempt DATETIME,
 disposition TEXT NOT NULL CHECK(disposition IN ('held','historical_completion','source_present','coalesced','excluded','preserved','review','staged_review','cooldown','seed_progress','source_progress')),
 outcome TEXT NOT NULL CHECK(outcome IN ('mapped','review')),
 reason TEXT NOT NULL,
 PRIMARY KEY(snapshot_uuid,ordinal),
 FOREIGN KEY(snapshot_uuid,ordinal) REFERENCES automation_snapshot_records(snapshot_uuid,ordinal),
 FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision),
 FOREIGN KEY(target_uuid,target_revision) REFERENCES enrichment_target_history(target_uuid,revision),
 FOREIGN KEY(catalog_snapshot_uuid,alias_ordinal) REFERENCES catalog_snapshot_records(snapshot_uuid,ordinal),
 CHECK((target_uuid IS NULL)=(target_revision IS NULL)),
 CHECK((collection_uuid IS NULL)=(collection_revision IS NULL)),
 CHECK((outcome='review')=(reason!='') AND (outcome='review')=(disposition IN ('review','staged_review'))),
 CHECK(post_uuid IS NULL OR post_reference!=''),
 CHECK(url_evidence_uuid IS NULL OR url_uuid IS NOT NULL),
 CHECK(target_uuid IS NULL OR (post_uuid IS NOT NULL AND collection_uuid IS NOT NULL AND url_uuid IS NOT NULL)),
 CHECK(disposition NOT IN ('held','preserved') OR target_uuid IS NOT NULL),
 CHECK(disposition!='historical_completion' OR receipt_uuid IS NOT NULL),
 CHECK(disposition!='source_present' OR capture_uuid IS NOT NULL),
 CHECK(disposition!='coalesced' OR (catalog_snapshot_uuid IS NOT NULL AND alias_ordinal IS NOT NULL)),
 CHECK(completion_uuid IS NULL OR (target_uuid IS NOT NULL AND (receipt_uuid IS NOT NULL OR capture_uuid IS NOT NULL))),
 CHECK(disposition!='cooldown' OR (cooldown_kind!='' AND cooldown_value!='' AND cooldown_until IS NOT NULL AND cooldown_reason!='')),
 CHECK(disposition!='seed_progress' OR (collection_uuid IS NOT NULL AND seed_last_post_key IS NOT NULL AND seed_complete IS NOT NULL AND seed_counts IS NOT NULL)),
 CHECK(disposition!='source_progress' OR (source_platform!='' AND source_last_attempt IS NOT NULL))
);
CREATE INDEX automation_enrichment_review ON automation_enrichment_records(snapshot_uuid,outcome,ordinal);
CREATE INDEX automation_enrichment_targets ON automation_enrichment_records(target_uuid,target_revision) WHERE target_uuid IS NOT NULL;
CREATE INDEX automation_enrichment_completions ON automation_enrichment_records(completion_uuid) WHERE completion_uuid IS NOT NULL;
CREATE INDEX automation_enrichment_cooldowns ON automation_enrichment_records(snapshot_uuid,cooldown_kind,cooldown_value)
 WHERE disposition='cooldown';
CREATE TRIGGER automation_enrichment_record_immutable BEFORE UPDATE ON automation_enrichment_records
BEGIN SELECT RAISE(ABORT,'automation enrichment import records are immutable'); END;
INSERT INTO native_migration_history(version,name,details)
VALUES(1000058,'Historical enrichment completion proof and frozen queue import','{}');
