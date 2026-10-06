CREATE TABLE discovery_scope_reviews (
 id INTEGER PRIMARY KEY,
 uuid TEXT NOT NULL UNIQUE CHECK(length(uuid)=36),
 listing_uuid TEXT NOT NULL REFERENCES discovery_listings(uuid),
 previous_uuid TEXT UNIQUE REFERENCES discovery_scope_reviews(uuid),
 collection_uuid TEXT NOT NULL,
 collection_revision INTEGER NOT NULL CHECK(collection_revision>0),
 root_uuid TEXT REFERENCES media_roots(uuid),
 input_sha256 TEXT NOT NULL CHECK(length(input_sha256)=64),
 plan_sha256 TEXT NOT NULL CHECK(length(plan_sha256)=64),
 plan TEXT NOT NULL CHECK(json_valid(plan) AND json_type(plan)='object' AND length(CAST(plan AS BLOB))<=131072),
 definition TEXT NOT NULL CHECK(json_valid(definition) AND json_type(definition)='object' AND length(CAST(definition AS BLOB))<=32768),
 digest TEXT NOT NULL CHECK(length(digest)=64),
 created_at DATETIME NOT NULL,
 FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision),
 CHECK(json_extract(definition,'$.uuid') IS listing_uuid AND json_extract(definition,'$.collection_uuid') IS collection_uuid
  AND json_extract(definition,'$.collection_revision') IS collection_revision AND json_extract(definition,'$.root_uuid') IS root_uuid)
);
CREATE INDEX discovery_scope_reviews_listing ON discovery_scope_reviews(listing_uuid,id);
CREATE UNIQUE INDEX discovery_scope_review_initial ON discovery_scope_reviews(listing_uuid) WHERE previous_uuid IS NULL;
CREATE TRIGGER discovery_scope_review_immutable BEFORE UPDATE ON discovery_scope_reviews
BEGIN SELECT RAISE(ABORT,'discovery collection reviews are immutable'); END;

CREATE VIEW discovery_effective_listings AS
 SELECT d.uuid,d.account_uuid,d.collection_uuid,
 CASE WHEN r.uuid IS NULL THEN d.collection_revision ELSE r.collection_revision END AS collection_revision,
 CASE WHEN r.uuid IS NULL THEN d.root_uuid ELSE r.root_uuid END AS root_uuid,
 coalesce(r.definition,d.definition) AS definition,coalesce(r.digest,d.digest) AS digest,d.created_at
 FROM discovery_listings d LEFT JOIN discovery_scope_reviews r
 ON r.id=(SELECT max(s.id) FROM discovery_scope_reviews s WHERE s.listing_uuid=d.uuid);

CREATE TRIGGER discovery_scope_review_scope BEFORE INSERT ON discovery_scope_reviews
WHEN NOT EXISTS(SELECT 1 FROM discovery_effective_listings d
 JOIN source_collections c ON c.uuid=d.collection_uuid AND c.revision=NEW.collection_revision
 JOIN source_collection_revisions r ON r.collection_uuid=c.uuid AND r.revision=c.revision
 WHERE d.uuid=NEW.listing_uuid AND d.collection_uuid=NEW.collection_uuid AND r.state='active'
 AND r.root_uuid IS NEW.root_uuid AND d.collection_revision<NEW.collection_revision
 AND d.digest=json_extract(NEW.plan,'$.input.expected_definition_sha256')
 AND NEW.previous_uuid IS (SELECT p.uuid FROM discovery_scope_reviews p WHERE p.listing_uuid=d.uuid ORDER BY p.id DESC LIMIT 1)
 AND NOT EXISTS(SELECT 1 FROM discovery_listing_jobs j WHERE j.listing_uuid=d.uuid)
 AND NOT EXISTS(SELECT 1 FROM discovery_detail_jobs j JOIN discovery_match_targets t ON t.uuid=j.target_uuid WHERE t.listing_uuid=d.uuid)
 AND NOT EXISTS(SELECT 1 FROM discovery_listing_recoveries x WHERE x.previous_listing_uuid=d.uuid))
BEGIN SELECT RAISE(ABORT,'discovery collection review requires an unstarted current search'); END;

DROP TRIGGER discovery_listing_job_scope;
CREATE TRIGGER discovery_listing_job_scope BEFORE INSERT ON discovery_listing_jobs
WHEN NOT EXISTS(SELECT 1 FROM archive_jobs j JOIN discovery_effective_listings d ON d.uuid=NEW.listing_uuid
 WHERE j.uuid=NEW.job_uuid AND j.kind='account.list_page' AND j.state='queued' AND j.fence=0
 AND json_extract(j.arguments,'$.version')=1 AND json_extract(j.arguments,'$.listing_uuid')=d.uuid
 AND json_extract(j.arguments,'$.generation')=NEW.generation
 AND json_extract(j.arguments,'$.page_ordinal')=NEW.page_ordinal
 AND json_extract(j.arguments,'$.definition_sha256')=d.digest AND json_extract(j.arguments,'$.collection_uuid')=d.collection_uuid)
BEGIN SELECT RAISE(ABORT,'discovery job does not match its listing'); END;

DROP TRIGGER discovery_detail_job_scope;
CREATE TRIGGER discovery_detail_job_scope BEFORE INSERT ON discovery_detail_jobs
WHEN NOT EXISTS(SELECT 1 FROM archive_jobs j JOIN discovery_match_targets t ON t.uuid=NEW.target_uuid
 JOIN discovery_match_candidates c ON c.id=NEW.candidate_sequence AND c.target_uuid=t.uuid
 JOIN discovery_match_evidence e ON e.target_uuid=t.uuid AND e.page_ordinal=c.best_page AND e.namespace=c.namespace AND e.value=c.value
 JOIN discovery_pages p ON p.listing_uuid=t.listing_uuid AND p.ordinal=c.best_page
 JOIN discovery_effective_listings d ON d.uuid=t.listing_uuid
 WHERE j.uuid=NEW.job_uuid AND j.kind='post.verify_candidate' AND j.state='queued' AND j.fence=0
 AND t.revision=NEW.target_revision AND e.needs_detail=1
 AND json_extract(j.arguments,'$.version')=1 AND json_extract(j.arguments,'$.generation')=NEW.generation
 AND json_extract(j.arguments,'$.target_uuid')=t.uuid AND json_extract(j.arguments,'$.target_revision')=t.revision
 AND json_extract(j.arguments,'$.source_sha256')=t.source_sha256 AND json_extract(j.arguments,'$.post_uuid')=t.post_uuid
 AND json_extract(j.arguments,'$.post_revision')=t.post_revision AND json_extract(j.arguments,'$.candidate_sequence')=c.id
 AND json_extract(j.arguments,'$.post_namespace')=c.namespace AND json_extract(j.arguments,'$.post_value')=c.value
 AND json_extract(j.arguments,'$.url')=e.url AND json_extract(j.arguments,'$.listing_uuid')=t.listing_uuid
 AND json_extract(j.arguments,'$.definition_sha256')=d.digest AND json_extract(j.arguments,'$.page_ordinal')=p.ordinal
 AND json_extract(j.arguments,'$.page_sha256')=p.digest AND json_extract(j.arguments,'$.collection_uuid')=d.collection_uuid
 AND json_extract(j.arguments,'$.collection_revision')=d.collection_revision AND json_extract(j.arguments,'$.root_uuid') IS d.root_uuid)
BEGIN SELECT RAISE(ABORT,'discovery detail job does not match its candidate'); END;

DROP TRIGGER discovery_listing_recovery_scope;
CREATE TRIGGER discovery_listing_recovery_scope BEFORE INSERT ON discovery_listing_recoveries
WHEN NOT EXISTS(SELECT 1 FROM discovery_listings n JOIN discovery_effective_listings p ON p.uuid=NEW.previous_listing_uuid
 JOIN discovery_listing_legacy nl ON nl.listing_uuid=n.uuid
 JOIN discovery_listing_legacy pl ON pl.listing_uuid=p.uuid
 WHERE n.uuid=NEW.listing_uuid AND p.digest=NEW.previous_sha256
 AND n.account_uuid=p.account_uuid AND n.collection_uuid=p.collection_uuid
 AND nl.snapshot_uuid=pl.snapshot_uuid AND nl.account_ordinal=pl.account_ordinal
 AND json_extract(n.definition,'$.recovery_of.listing_uuid')=p.uuid
 AND json_extract(n.definition,'$.recovery_of.sha256')=p.digest
 AND json_type(p.definition,'$.recovery_of') IS NULL
 AND (json_extract(p.definition,'$.historical_pages')>0 OR json_type(p.definition,'$.initial_cursor')='object')
 AND json_extract(n.definition,'$.historical_pages')=0 AND json_type(n.definition,'$.initial_cursor')='null'
 AND NOT EXISTS(SELECT 1 FROM discovery_listing_jobs b JOIN archive_jobs j ON j.uuid=b.job_uuid
  WHERE b.listing_uuid=p.uuid AND j.state IN ('queued','running')))
BEGIN SELECT RAISE(ABORT,'discovery recovery requires its original quiescent search'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000084,'Reviewed collection bindings for unstarted discovery searches','{}');
