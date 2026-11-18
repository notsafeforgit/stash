-- Retire the frozen catalog/automation import history. Every native fact the
-- import produced (posts, captures, media links, documents, translations,
-- enrichment completions and identifiers) is ordinary native data and stays.
-- This removes the staging snapshots, per-row import receipts, activation and
-- handoff bookkeeping, and the discovery (account listing/candidate detail)
-- workflow that only those inputs could start.
PRAGMA defer_foreign_keys=ON;

-- Legacy-only posts can still be enriched by their original finder lookup.
-- Keep exactly those lookup values, keyed to the post, collection and URL the
-- enrichment target uses, instead of the whole frozen automation database.
CREATE TABLE enrichment_identity_lookups (
 id INTEGER PRIMARY KEY,
 post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
 collection_uuid TEXT NOT NULL REFERENCES source_collections(uuid),
 url TEXT NOT NULL CHECK(length(url)>0),
 source_values TEXT NOT NULL CHECK(json_valid(source_values) AND json_type(source_values)='object'
  AND length(CAST(source_values AS BLOB)) BETWEEN 2 AND 1048576)
);
CREATE INDEX enrichment_identity_lookup_target ON enrichment_identity_lookups(post_uuid,collection_uuid,url);
CREATE TRIGGER enrichment_identity_lookup_immutable BEFORE UPDATE ON enrichment_identity_lookups
BEGIN SELECT RAISE(ABORT,'enrichment identity lookups are immutable'); END;

INSERT INTO enrichment_identity_lookups(post_uuid,collection_uuid,url,source_values)
SELECT r.post_uuid,r.collection_uuid,r.candidate_url,json_extract(e.data,'$.values')
FROM automation_discovery_records r
JOIN automation_discovery_imports i ON i.snapshot_uuid=r.snapshot_uuid
JOIN automation_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
WHERE r.disposition='lookup' AND r.outcome='mapped' AND i.state!='running' AND e.source_table='discovery_targets'
 AND r.collection_uuid IS NOT NULL AND r.candidate_url!='' AND json_type(e.data,'$.values')='object'
ORDER BY r.snapshot_uuid,r.ordinal;

-- New corroborations reference the retained lookup. Existing discovery
-- resolutions are not converted: their identifiers and evidence remain native.
CREATE TABLE enrichment_identity_resolutions (
 job_uuid TEXT PRIMARY KEY NOT NULL REFERENCES enrichment_job_targets(job_uuid),
 lookup_id INTEGER NOT NULL REFERENCES enrichment_identity_lookups(id),
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
 FOREIGN KEY(job_uuid,checkpoint_revision) REFERENCES enrichment_checkpoint_receipts(job_uuid,revision),
 FOREIGN KEY(job_uuid,record_ordinal) REFERENCES enrichment_checkpoint_records(job_uuid,ordinal),
 FOREIGN KEY(job_uuid) REFERENCES enrichment_publications(job_uuid) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX enrichment_identity_resolution_lookup ON enrichment_identity_resolutions(lookup_id);
CREATE TRIGGER enrichment_identity_resolution_immutable BEFORE UPDATE ON enrichment_identity_resolutions
BEGIN SELECT RAISE(ABORT,'enrichment identity resolutions are immutable'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000142,'Retire the frozen import history',json_object(
 'identity_lookups',(SELECT count(*) FROM enrichment_identity_lookups),
 'retired_discovery_resolutions',(SELECT count(*) FROM enrichment_discovery_resolutions),
 'excluded_held_targets',(SELECT count(*) FROM enrichment_targets WHERE origin='migration' AND state='held'),
 'removed_discovery_jobs',(SELECT count(*) FROM archive_jobs WHERE kind IN ('account.list_page','post.verify_candidate')),
 'removed_discovery_policy_upgrades',(SELECT count(*) FROM metadata_worker_policy_upgrades WHERE kind!='post.enrich'),
 'catalog_snapshots',(SELECT count(*) FROM catalog_snapshots),
 'automation_snapshots',(SELECT count(*) FROM automation_snapshots)));

-- Unreleased imported enrichment can no longer be activated. Retain the
-- targets and their history, but end them with an explicit reason.
UPDATE enrichment_targets
SET state='excluded',reason='import_retired',revision=revision+1,
 updated_at=max(updated_at,strftime('%Y-%m-%d %H:%M:%f','now')||'+00:00')
WHERE origin='migration' AND state='held';

DROP TRIGGER discovery_detail_success;
DROP TRIGGER discovery_listing_success;
DROP INDEX archive_jobs_discovery_detail;
DROP INDEX archive_jobs_discovery_listing;
DROP TRIGGER metadata_worker_attempt_policy_valid;
CREATE TRIGGER metadata_worker_attempt_policy_valid BEFORE INSERT ON metadata_worker_attempt_policies
WHEN NOT EXISTS(SELECT 1 FROM archive_jobs j JOIN archive_job_attempts a ON a.job_uuid=j.uuid AND a.fence=j.fence
  WHERE j.uuid=NEW.job_uuid AND j.fence=NEW.fence AND j.state='running' AND a.outcome='running'
  AND j.kind='post.enrich' AND NEW.original_policy_sha256=json_extract(j.arguments,'$.policy_sha256')
  AND NEW.approval_uuid IS (SELECT p.request_uuid FROM metadata_worker_policy_upgrades p
    WHERE p.kind=j.kind AND p.original_policy_sha256=NEW.original_policy_sha256 ORDER BY p.id DESC LIMIT 1)
  AND ((NEW.approval_uuid IS NULL AND NEW.policy_sha256=NEW.original_policy_sha256)
    OR EXISTS(SELECT 1 FROM metadata_worker_policy_upgrades p WHERE p.request_uuid=NEW.approval_uuid
       AND p.kind=j.kind AND p.original_policy_sha256=NEW.original_policy_sha256 AND p.policy_sha256=NEW.policy_sha256
       AND p.created_at_ms<=a.started_at_ms)))
BEGIN SELECT RAISE(ABORT,'invalid metadata worker attempt policy'); END;

-- Retained legacy-enrichment checkpoints were handoff-only; none can start now.
DROP TRIGGER enrichment_completion_capture_scope;
CREATE TRIGGER enrichment_completion_capture_scope BEFORE INSERT ON enrichment_completion_captures
WHEN NOT EXISTS(SELECT 1 FROM enrichment_completions e JOIN enrichment_targets t ON t.uuid=e.target_uuid
 JOIN source_captures c ON c.uuid=NEW.capture_uuid AND c.post_uuid=t.post_uuid
 JOIN source_collection_captures b ON b.capture_uuid=c.uuid AND b.collection_uuid=t.collection_uuid AND b.collection_revision=t.collection_revision
 WHERE e.uuid=NEW.completion_uuid AND c.origin IN ('gallery-dl','gallery-dl-enrichment')
 AND e.legacy_receipt_uuid IS NULL AND e.legacy_capture_uuid IS NULL AND t.state='pending' AND t.revision=e.expected_revision
 AND e.capture_count>(SELECT count(*) FROM enrichment_completion_captures WHERE completion_uuid=e.uuid))
BEGIN SELECT RAISE(ABORT,'enrichment completion capture is outside its post or collection scope'); END;

DROP VIEW discovery_effective_listings;
DROP VIEW catalog_document_pending_imports;
DROP VIEW catalog_retirable_snapshots;
DROP VIEW catalog_retirable_payloads;

-- Children first. Rebuild the database file afterwards to return the space.
DROP TABLE automation_translation_records;
DROP TABLE catalog_account_mappings;
DROP TABLE catalog_attachment_records;
DROP TABLE catalog_cleanup_records;
DROP TABLE catalog_collection_mappings;
DROP TABLE catalog_document_records;
DROP TABLE catalog_enrichment_records;
DROP TABLE catalog_evidence_posts;
DROP TABLE catalog_file_history_records;
DROP TABLE catalog_identity_import_records;
DROP TABLE catalog_media_records;
DROP TABLE catalog_membership_groups;
DROP TABLE catalog_membership_records;
DROP TABLE catalog_payload_retirements;
DROP TABLE catalog_publisher_records;
DROP TABLE catalog_registry_import_records;
DROP TABLE catalog_relation_records;
DROP TABLE catalog_translation_records;
DROP TABLE checkpoint_evidence_captures;
DROP TABLE discovery_activation_targets;
DROP TABLE discovery_detail_checkpoint_records;
DROP TABLE discovery_detail_checkpoint_usage;
DROP TABLE discovery_detail_checkpoints;
DROP TABLE discovery_listing_legacy;
DROP TABLE discovery_listing_recoveries;
DROP TABLE discovery_published_records;
DROP TABLE discovery_recovery_targets;
DROP TABLE discovery_scope_reviews;
DROP TABLE enrichment_discovery_resolutions;
DROP TABLE enrichment_job_retained_records;
DROP TABLE enrichment_job_seed_services;
DROP TABLE enrichment_rebinding_targets;
DROP TABLE source_post_import_labels;
DROP TABLE translation_activation_targets;
DROP TABLE catalog_attachment_imports;
DROP TABLE catalog_cleanup_imports;
DROP TABLE catalog_document_imports;
DROP TABLE catalog_enrichment_imports;
DROP TABLE catalog_evidence_records;
DROP TABLE catalog_file_history_imports;
DROP TABLE catalog_membership_imports;
DROP TABLE catalog_publisher_imports;
DROP TABLE catalog_relations_imports;
DROP TABLE catalog_translation_imports;
DROP TABLE discovery_activations;
DROP TABLE discovery_match_publications;
DROP TABLE enrichment_activation_targets;
DROP TABLE enrichment_handoff_jobs;
DROP TABLE enrichment_rebindings;
DROP TABLE source_cleanup_intents;
DROP TABLE translation_activations;
DROP TABLE automation_translation_imports;
DROP TABLE catalog_evidence_imports;
DROP TABLE catalog_media_imports;
DROP TABLE checkpoint_handoffs;
DROP TABLE discovery_detail_results;
DROP TABLE discovery_match_evidence;
DROP TABLE enrichment_activations;
DROP TABLE checkpoint_evidence_acceptances;
DROP TABLE discovery_detail_checkpoint_receipts;
DROP TABLE discovery_match_pages;
DROP TABLE automation_checkpoint_records;
DROP TABLE discovery_detail_attempts;
DROP TABLE discovery_pages;
DROP TABLE automation_checkpoint_bodies;
DROP TABLE automation_checkpoint_imports;
DROP TABLE discovery_detail_jobs;
DROP TABLE discovery_job_attempts;
DROP TABLE discovery_listing_jobs;
DROP TABLE discovery_match_candidates;
DROP TABLE discovery_match_targets;
DROP TABLE automation_discovery_records;
DROP TABLE discovery_listings;
DROP TABLE automation_discovery_imports;
DROP TABLE automation_enrichment_records;
DROP TABLE automation_enrichment_imports;
DROP TABLE automation_snapshot_records;
DROP TABLE catalog_snapshot_records;
DROP TABLE automation_snapshot_chunks;
DROP TABLE automation_snapshot_tables;
DROP TABLE catalog_snapshot_chunks;
DROP TABLE catalog_snapshot_tables;
DROP TABLE automation_snapshots;
DROP TABLE catalog_snapshots;
DROP TABLE catalog_registry_imports;
DROP TABLE catalog_identity_imports;

-- Discovery jobs and their attempts end with the discovery workflow. Their
-- discovery tables are gone, so only shared job records remain.
DROP TRIGGER metadata_worker_attempt_policy_retained;
DROP TRIGGER metadata_worker_policy_retained;
DELETE FROM metadata_worker_attempt_policies
WHERE job_uuid IN (SELECT uuid FROM archive_jobs WHERE kind IN ('account.list_page','post.verify_candidate'));
DELETE FROM metadata_worker_policy_upgrades WHERE kind!='post.enrich';
CREATE TRIGGER metadata_worker_policy_retained BEFORE DELETE ON metadata_worker_policy_upgrades
BEGIN SELECT RAISE(ABORT,'metadata worker policy approvals are retained'); END;
CREATE TRIGGER metadata_worker_attempt_policy_retained BEFORE DELETE ON metadata_worker_attempt_policies
BEGIN SELECT RAISE(ABORT,'metadata worker attempts are retained'); END;
DELETE FROM enrichment_attempt_pacing
WHERE job_uuid IN (SELECT uuid FROM archive_jobs WHERE kind IN ('account.list_page','post.verify_candidate'));
DELETE FROM enrichment_job_pacing
WHERE job_uuid IN (SELECT uuid FROM archive_jobs WHERE kind IN ('account.list_page','post.verify_candidate'));
DELETE FROM source_enrichment_waiter_scopes
WHERE job_uuid IN (SELECT uuid FROM archive_jobs WHERE kind IN ('account.list_page','post.verify_candidate'));
DELETE FROM source_enrichment_waiters
WHERE job_uuid IN (SELECT uuid FROM archive_jobs WHERE kind IN ('account.list_page','post.verify_candidate'));
DELETE FROM archive_job_attempts
WHERE job_uuid IN (SELECT uuid FROM archive_jobs WHERE kind IN ('account.list_page','post.verify_candidate'));
DELETE FROM archive_job_submissions
WHERE job_uuid IN (SELECT uuid FROM archive_jobs WHERE kind IN ('account.list_page','post.verify_candidate'));
DELETE FROM archive_jobs WHERE kind IN ('account.list_page','post.verify_candidate');
