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
 policy TEXT NOT NULL CHECK(policy='retained-discovery-publication-v1'),
 basis TEXT NOT NULL CHECK(basis IN ('exact-title-and-date','exact-original-text-and-date','exact-title-and-original-text','exact-source-url-and-date')),
 witness_ordinal INTEGER NOT NULL CHECK(witness_ordinal BETWEEN 0 AND 4095),
 evidence_uuid TEXT NOT NULL UNIQUE REFERENCES source_post_identifier_evidence(uuid),
 url_evidence_uuid TEXT NOT NULL UNIQUE REFERENCES source_post_url_evidence(uuid),
 record_count INTEGER NOT NULL CHECK(record_count BETWEEN 1 AND 4096),
 capture_count INTEGER NOT NULL CHECK(capture_count BETWEEN 1 AND record_count),
 created_at DATETIME NOT NULL,
 FOREIGN KEY(target_uuid,listing_uuid) REFERENCES discovery_match_targets(uuid,listing_uuid),
 FOREIGN KEY(target_uuid,page_ordinal,namespace,value) REFERENCES discovery_match_evidence(target_uuid,page_ordinal,namespace,value),
 FOREIGN KEY(listing_uuid,page_ordinal) REFERENCES discovery_pages(listing_uuid,ordinal)
);
CREATE TRIGGER discovery_match_publication_immutable BEFORE UPDATE ON discovery_match_publications
BEGIN SELECT RAISE(ABORT,'discovery publication is immutable'); END;
CREATE TRIGGER discovery_match_publication_scope BEFORE INSERT ON discovery_match_publications
WHEN NOT EXISTS(SELECT 1 FROM discovery_match_targets t
 JOIN discovery_listings l ON l.uuid=t.listing_uuid
 JOIN discovery_match_candidates c ON c.target_uuid=t.uuid
 JOIN discovery_match_evidence e ON e.target_uuid=t.uuid AND e.page_ordinal=c.best_page AND e.namespace=c.namespace AND e.value=c.value
 JOIN discovery_pages p ON p.listing_uuid=l.uuid AND p.ordinal=c.best_page
 WHERE t.uuid=NEW.target_uuid AND t.revision=NEW.target_revision AND t.enumeration_complete=1
 AND t.post_uuid=NEW.post_uuid AND t.post_revision=NEW.post_revision
 AND json_extract(l.definition,'$.historical_pages')=0 AND json_type(l.definition,'$.initial_cursor')='null'
 AND c.namespace=NEW.namespace AND c.value=NEW.value AND c.best_page=NEW.page_ordinal
 AND e.needs_detail=0 AND e.basis=NEW.basis AND p.digest=NEW.page_sha256
 AND (SELECT count(*) FROM discovery_match_candidates WHERE target_uuid=t.uuid)=1)
BEGIN SELECT RAISE(ABORT,'discovery publication requires a complete unique verified match'); END;
CREATE TABLE discovery_published_records (
 target_uuid TEXT NOT NULL REFERENCES discovery_match_publications(target_uuid),
 ordinal INTEGER NOT NULL CHECK(ordinal BETWEEN 0 AND 4095),
 capture_uuid TEXT NOT NULL REFERENCES source_captures(uuid),
 PRIMARY KEY(target_uuid,ordinal)
);
CREATE INDEX discovery_published_records_capture ON discovery_published_records(capture_uuid,target_uuid);
CREATE TRIGGER discovery_published_record_immutable BEFORE UPDATE ON discovery_published_records
BEGIN SELECT RAISE(ABORT,'discovery published records are immutable'); END;
CREATE TRIGGER discovery_published_record_scope BEFORE INSERT ON discovery_published_records
WHEN NOT EXISTS(SELECT 1 FROM discovery_match_publications p JOIN source_captures c ON c.post_uuid=p.post_uuid
 JOIN discovery_pages page ON page.listing_uuid=p.listing_uuid AND page.ordinal=p.page_ordinal
 WHERE p.target_uuid=NEW.target_uuid AND c.uuid=NEW.capture_uuid AND NEW.ordinal<page.record_count)
BEGIN SELECT RAISE(ABORT,'discovery record must publish its original post observation'); END;
INSERT INTO native_migration_history(version,name,details)
VALUES(1000074,'Atomic verified discovery identity and native capture publication','{}');
