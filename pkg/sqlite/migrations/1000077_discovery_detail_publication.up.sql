-- Keep the publication identity and all existing receipts under the same name.
-- Foreign-key targets are restored before the migration transaction commits.
PRAGMA defer_foreign_keys=ON;
CREATE TABLE native_discovery_publication_rows AS SELECT * FROM discovery_match_publications;
DROP TABLE discovery_match_publications;
CREATE TABLE discovery_match_publications (
 target_uuid TEXT PRIMARY KEY NOT NULL REFERENCES discovery_match_targets(uuid),
 target_revision INTEGER NOT NULL CHECK(target_revision>1),
 listing_uuid TEXT NOT NULL,
 page_ordinal INTEGER NOT NULL CHECK(page_ordinal BETWEEN 1 AND 10000),
 page_sha256 TEXT NOT NULL CHECK(length(page_sha256)=64 AND page_sha256 NOT GLOB '*[^0-9a-f]*'),
 post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
 post_revision INTEGER NOT NULL CHECK(post_revision>0),
 namespace TEXT NOT NULL CHECK(namespace IN ('native:reddit','native:twitter')),
 value TEXT NOT NULL CHECK(length(value) BETWEEN 1 AND 256),
 policy TEXT NOT NULL,
 basis TEXT NOT NULL CHECK(basis IN ('exact-title-and-date','exact-original-text-and-date','exact-title-and-original-text','exact-source-url-and-date')),
 witness_ordinal INTEGER NOT NULL CHECK(witness_ordinal BETWEEN 0 AND 4095),
 evidence_uuid TEXT NOT NULL UNIQUE REFERENCES source_post_identifier_evidence(uuid),
 url_evidence_uuid TEXT NOT NULL UNIQUE REFERENCES source_post_url_evidence(uuid),
 record_count INTEGER NOT NULL CHECK(record_count BETWEEN 1 AND 4096),
 capture_count INTEGER NOT NULL CHECK(capture_count BETWEEN 1 AND record_count),
 created_at DATETIME NOT NULL,
 detail_job_uuid TEXT REFERENCES discovery_detail_results(job_uuid),
 CHECK((policy='retained-discovery-publication-v1' AND detail_job_uuid IS NULL)
    OR (policy='retained-discovery-detail-publication-v1' AND detail_job_uuid IS NOT NULL)),
 FOREIGN KEY(target_uuid,listing_uuid) REFERENCES discovery_match_targets(uuid,listing_uuid),
 FOREIGN KEY(target_uuid,page_ordinal,namespace,value) REFERENCES discovery_match_evidence(target_uuid,page_ordinal,namespace,value),
 FOREIGN KEY(listing_uuid,page_ordinal) REFERENCES discovery_pages(listing_uuid,ordinal)
);
INSERT INTO discovery_match_publications SELECT *,NULL FROM native_discovery_publication_rows;
DROP TABLE native_discovery_publication_rows;
CREATE UNIQUE INDEX discovery_publication_detail ON discovery_match_publications(detail_job_uuid) WHERE detail_job_uuid IS NOT NULL;

CREATE TRIGGER discovery_match_publication_immutable BEFORE UPDATE ON discovery_match_publications
BEGIN SELECT RAISE(ABORT,'discovery publication is immutable'); END;
CREATE TRIGGER discovery_match_publication_scope BEFORE INSERT ON discovery_match_publications
WHEN NOT EXISTS(SELECT 1 FROM discovery_match_targets t
 JOIN discovery_listings l ON l.uuid=t.listing_uuid
 JOIN discovery_match_candidates c ON c.target_uuid=t.uuid
 JOIN discovery_match_evidence e ON e.target_uuid=t.uuid AND e.page_ordinal=NEW.page_ordinal AND e.namespace=c.namespace AND e.value=c.value
 JOIN discovery_pages p ON p.listing_uuid=l.uuid AND p.ordinal=e.page_ordinal
 WHERE t.uuid=NEW.target_uuid AND t.revision=NEW.target_revision AND t.enumeration_complete=1
 AND t.post_uuid=NEW.post_uuid AND t.post_revision=NEW.post_revision
 AND json_extract(l.definition,'$.historical_pages')=0 AND json_type(l.definition,'$.initial_cursor')='null'
 AND c.namespace=NEW.namespace AND c.value=NEW.value AND p.digest=NEW.page_sha256
 AND (SELECT count(*) FROM discovery_match_candidates WHERE target_uuid=t.uuid)=1
 AND ((NEW.detail_job_uuid IS NULL AND c.best_page=e.page_ordinal AND e.needs_detail=0 AND e.basis=NEW.basis)
   OR (e.needs_detail=1 AND EXISTS(SELECT 1 FROM discovery_detail_jobs d
     JOIN archive_jobs j ON j.uuid=d.job_uuid AND j.state='succeeded'
     JOIN discovery_detail_results r ON r.job_uuid=j.uuid AND r.fence=j.fence
     JOIN discovery_detail_checkpoint_receipts h ON h.job_uuid=r.job_uuid AND h.revision=r.checkpoint_revision
     WHERE d.job_uuid=NEW.detail_job_uuid AND d.target_uuid=t.uuid AND d.target_revision<=t.revision AND d.candidate_sequence=c.id
     AND json_extract(j.arguments,'$.page_ordinal')=e.page_ordinal
     AND json_extract(j.arguments,'$.page_sha256')=p.digest
     AND json_extract(r.evidence,'$.status')='corroborated'
     AND json_extract(r.evidence,'$.basis')=NEW.basis
     AND json_extract(r.evidence,'$.witness_ordinal')=NEW.witness_ordinal
     AND h.pending_count=0 AND h.record_count=NEW.record_count))))
BEGIN SELECT RAISE(ABORT,'discovery publication requires a complete unique verified match'); END;

DROP TRIGGER discovery_published_record_scope;
CREATE TRIGGER discovery_published_record_scope BEFORE INSERT ON discovery_published_records
WHEN NOT EXISTS(SELECT 1 FROM discovery_match_publications p JOIN source_captures c ON c.post_uuid=p.post_uuid
 JOIN discovery_pages page ON page.listing_uuid=p.listing_uuid AND page.ordinal=p.page_ordinal
 WHERE p.target_uuid=NEW.target_uuid AND c.uuid=NEW.capture_uuid
 AND ((p.detail_job_uuid IS NULL AND NEW.ordinal<page.record_count)
 OR (p.detail_job_uuid IS NOT NULL AND EXISTS(SELECT 1 FROM discovery_detail_checkpoint_records r
     WHERE r.job_uuid=p.detail_job_uuid AND r.ordinal=NEW.ordinal))))
BEGIN SELECT RAISE(ABORT,'discovery record must publish its original post observation'); END;
CREATE INDEX discovery_detail_candidate_history
 ON discovery_detail_jobs(target_uuid,candidate_sequence,target_revision DESC,generation DESC);

INSERT INTO native_migration_history(version,name,details)
VALUES(1000077,'Guarded native publication from authenticated discovery details','{}');
