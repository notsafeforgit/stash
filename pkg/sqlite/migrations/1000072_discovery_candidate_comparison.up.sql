CREATE TABLE discovery_match_targets (
 uuid TEXT PRIMARY KEY NOT NULL CHECK(length(uuid)=36),
 listing_uuid TEXT NOT NULL REFERENCES discovery_listings(uuid),
 snapshot_uuid TEXT NOT NULL,
 source_ordinal INTEGER NOT NULL CHECK(source_ordinal>0),
 source_sha256 TEXT NOT NULL CHECK(length(source_sha256)=64 AND source_sha256 NOT GLOB '*[^0-9a-f]*'),
 post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
 post_revision INTEGER NOT NULL CHECK(post_revision>0),
 policy TEXT NOT NULL CHECK(policy='retained-discovery-listing-v1'),
 revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0),
 last_page INTEGER NOT NULL DEFAULT 0 CHECK(last_page BETWEEN 0 AND 10000),
 enumeration_complete INTEGER NOT NULL DEFAULT 0 CHECK(enumeration_complete IN (0,1)),
 created_at DATETIME NOT NULL,
 updated_at DATETIME NOT NULL,
 FOREIGN KEY(snapshot_uuid,source_ordinal) REFERENCES automation_discovery_records(snapshot_uuid,ordinal),
 UNIQUE(listing_uuid,snapshot_uuid,source_ordinal),
 UNIQUE(uuid,listing_uuid),
 CHECK(revision=last_page+1 AND (last_page>0 OR enumeration_complete=0))
);
CREATE INDEX discovery_match_targets_source ON discovery_match_targets(snapshot_uuid,source_ordinal);
CREATE INDEX discovery_match_targets_pending ON discovery_match_targets(uuid) WHERE enumeration_complete=0;
CREATE TRIGGER discovery_match_target_scope BEFORE INSERT ON discovery_match_targets
WHEN NOT EXISTS(SELECT 1 FROM discovery_listing_legacy l JOIN discovery_listings d ON d.uuid=l.listing_uuid
 JOIN automation_discovery_records r ON r.snapshot_uuid=l.snapshot_uuid AND r.account_ordinal=l.account_ordinal
 JOIN automation_snapshot_records e ON e.snapshot_uuid=r.snapshot_uuid AND e.ordinal=r.ordinal
 JOIN source_posts p ON p.uuid=r.post_uuid
 WHERE d.uuid=NEW.listing_uuid AND r.ordinal=NEW.source_ordinal AND r.snapshot_uuid=NEW.snapshot_uuid
 AND e.source_table='discovery_targets' AND e.data_sha256=NEW.source_sha256
 AND r.outcome='mapped' AND r.disposition='held' AND r.account_uuid=d.account_uuid AND r.collection_uuid=d.collection_uuid
 AND p.uuid=NEW.post_uuid AND p.revision=NEW.post_revision AND p.state='active')
BEGIN SELECT RAISE(ABORT,'discovery target must bind its original reviewed source'); END;

CREATE TABLE discovery_match_pages (
 target_uuid TEXT NOT NULL,
 listing_uuid TEXT NOT NULL,
 page_ordinal INTEGER NOT NULL CHECK(page_ordinal BETWEEN 1 AND 10000),
 page_sha256 TEXT NOT NULL CHECK(length(page_sha256)=64 AND page_sha256 NOT GLOB '*[^0-9a-f]*'),
 matches_sha256 TEXT NOT NULL CHECK(length(matches_sha256)=64 AND matches_sha256 NOT GLOB '*[^0-9a-f]*'),
 candidate_count INTEGER NOT NULL CHECK(candidate_count BETWEEN 0 AND 4096),
 complete INTEGER NOT NULL CHECK(complete IN (0,1)),
 created_at DATETIME NOT NULL,
 PRIMARY KEY(target_uuid,page_ordinal),
 FOREIGN KEY(target_uuid,listing_uuid) REFERENCES discovery_match_targets(uuid,listing_uuid),
 FOREIGN KEY(listing_uuid,page_ordinal) REFERENCES discovery_pages(listing_uuid,ordinal)
);
CREATE TRIGGER discovery_match_page_immutable BEFORE UPDATE ON discovery_match_pages
BEGIN SELECT RAISE(ABORT,'discovery comparison receipt is immutable'); END;
CREATE TRIGGER discovery_match_page_scope BEFORE INSERT ON discovery_match_pages
WHEN NOT EXISTS(SELECT 1 FROM discovery_match_targets t JOIN discovery_pages p ON p.listing_uuid=t.listing_uuid
 WHERE t.uuid=NEW.target_uuid AND t.listing_uuid=NEW.listing_uuid AND t.last_page+1=NEW.page_ordinal AND t.enumeration_complete=0
 AND p.ordinal=NEW.page_ordinal AND p.digest=NEW.page_sha256 AND p.complete=NEW.complete)
BEGIN SELECT RAISE(ABORT,'discovery comparison must continue its original pages'); END;

CREATE TABLE discovery_match_candidates (
 id INTEGER PRIMARY KEY,
 target_uuid TEXT NOT NULL REFERENCES discovery_match_targets(uuid),
 namespace TEXT NOT NULL CHECK(namespace IN ('native:reddit','native:twitter')),
 value TEXT NOT NULL CHECK(length(value) BETWEEN 1 AND 256),
 first_page INTEGER NOT NULL CHECK(first_page BETWEEN 1 AND 10000),
 best_page INTEGER NOT NULL CHECK(best_page BETWEEN first_page AND last_page),
 last_page INTEGER NOT NULL CHECK(last_page BETWEEN first_page AND 10000),
 page_count INTEGER NOT NULL CHECK(page_count BETWEEN 1 AND last_page-first_page+1),
 UNIQUE(target_uuid,namespace,value),
 FOREIGN KEY(target_uuid,first_page,namespace,value) REFERENCES discovery_match_evidence(target_uuid,page_ordinal,namespace,value) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(target_uuid,best_page,namespace,value) REFERENCES discovery_match_evidence(target_uuid,page_ordinal,namespace,value) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(target_uuid,last_page,namespace,value) REFERENCES discovery_match_evidence(target_uuid,page_ordinal,namespace,value) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX discovery_match_candidates_page ON discovery_match_candidates(target_uuid,id);
CREATE TRIGGER discovery_match_candidate_transition BEFORE UPDATE ON discovery_match_candidates
WHEN NEW.id!=OLD.id OR NEW.target_uuid!=OLD.target_uuid OR NEW.namespace!=OLD.namespace OR NEW.value!=OLD.value
 OR NEW.first_page!=OLD.first_page OR NEW.last_page<=OLD.last_page OR NEW.page_count!=OLD.page_count+1
 OR NEW.best_page NOT IN (OLD.best_page,NEW.last_page)
BEGIN SELECT RAISE(ABORT,'invalid discovery candidate progression'); END;

CREATE TABLE discovery_match_evidence (
 target_uuid TEXT NOT NULL,
 page_ordinal INTEGER NOT NULL,
 namespace TEXT NOT NULL,
 value TEXT NOT NULL,
 url TEXT NOT NULL CHECK(length(url) BETWEEN 1 AND 8192),
 basis TEXT NOT NULL CHECK(basis IN ('exact-title-and-date','exact-original-text-and-date','exact-title-and-original-text','exact-source-url-and-date','title-needs-verification')),
 needs_detail INTEGER NOT NULL CHECK(needs_detail IN (0,1) AND (needs_detail=1)=(basis='title-needs-verification')),
 record_ordinals TEXT NOT NULL CHECK(json_valid(record_ordinals) AND json_type(record_ordinals)='array'
  AND json_array_length(record_ordinals) BETWEEN 1 AND 4096 AND length(CAST(record_ordinals AS BLOB))<=32768),
 PRIMARY KEY(target_uuid,page_ordinal,namespace,value),
 FOREIGN KEY(target_uuid,page_ordinal) REFERENCES discovery_match_pages(target_uuid,page_ordinal),
 FOREIGN KEY(target_uuid,namespace,value) REFERENCES discovery_match_candidates(target_uuid,namespace,value) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX discovery_match_evidence_candidate ON discovery_match_evidence(target_uuid,namespace,value,page_ordinal);
CREATE TRIGGER discovery_match_evidence_immutable BEFORE UPDATE ON discovery_match_evidence
BEGIN SELECT RAISE(ABORT,'discovery candidate evidence is immutable'); END;
CREATE TRIGGER discovery_match_target_transition BEFORE UPDATE ON discovery_match_targets
WHEN NEW.uuid!=OLD.uuid OR NEW.listing_uuid!=OLD.listing_uuid OR NEW.snapshot_uuid!=OLD.snapshot_uuid
 OR NEW.source_ordinal!=OLD.source_ordinal OR NEW.source_sha256!=OLD.source_sha256 OR NEW.post_uuid!=OLD.post_uuid
 OR NEW.post_revision!=OLD.post_revision OR NEW.policy!=OLD.policy OR NEW.created_at!=OLD.created_at
 OR OLD.enumeration_complete=1 OR NEW.last_page!=OLD.last_page+1 OR NEW.revision!=OLD.revision+1
 OR NOT EXISTS(SELECT 1 FROM discovery_match_pages p WHERE p.target_uuid=NEW.uuid AND p.page_ordinal=NEW.last_page
  AND p.complete=NEW.enumeration_complete AND p.created_at=NEW.updated_at
  AND p.candidate_count=(SELECT count(*) FROM discovery_match_evidence e WHERE e.target_uuid=p.target_uuid AND e.page_ordinal=p.page_ordinal))
BEGIN SELECT RAISE(ABORT,'discovery comparison progress requires its complete receipt'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000072,'Retained account-listing candidates and atomic comparison progress','{}');
