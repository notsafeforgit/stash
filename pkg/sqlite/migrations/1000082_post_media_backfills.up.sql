-- A reviewed historical match keeps its request receipt and original proofs.
-- This migration admits no matching work and selects no media associations.
CREATE TABLE post_media_backfills (
 uuid TEXT PRIMARY KEY NOT NULL CHECK(length(uuid)=36 AND uuid=lower(uuid)
 AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
 AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
 AND uuid!='00000000-0000-0000-0000-000000000000'),
 post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
 post_revision INTEGER NOT NULL CHECK(typeof(post_revision)='integer' AND post_revision>0),
 policy TEXT NOT NULL CHECK(policy='catalog-files-v1'),
 signature TEXT NOT NULL CHECK(length(signature)=64 AND signature NOT GLOB '*[^0-9a-f]*'),
 request_digest TEXT NOT NULL CHECK(length(request_digest)=64 AND request_digest NOT GLOB '*[^0-9a-f]*'),
 selected INTEGER NOT NULL CHECK(typeof(selected)='integer' AND selected BETWEEN 0 AND 8192),
 preserved INTEGER NOT NULL CHECK(typeof(preserved)='integer' AND preserved BETWEEN 0 AND 8192),
 review INTEGER NOT NULL CHECK(typeof(review)='integer' AND review BETWEEN 0 AND 8192),
 unavailable INTEGER NOT NULL CHECK(typeof(unavailable)='integer' AND unavailable BETWEEN 0 AND 8192),
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 CHECK(selected+preserved+review+unavailable<=8192)
);
CREATE INDEX post_media_backfills_post ON post_media_backfills(post_uuid,uuid);
CREATE TRIGGER post_media_backfill_immutable BEFORE UPDATE ON post_media_backfills
BEGIN SELECT RAISE(ABORT,'post media backfill receipts are immutable'); END;
CREATE TRIGGER post_media_backfill_scope BEFORE INSERT ON post_media_backfills
WHEN NOT EXISTS(SELECT 1 FROM source_posts WHERE uuid=NEW.post_uuid AND revision=NEW.post_revision
 AND (state='active' OR NEW.selected=0))
BEGIN SELECT RAISE(ABORT,'post media backfill requires the reviewed post revision'); END;

CREATE TABLE post_media_backfill_decisions (
 backfill_uuid TEXT NOT NULL REFERENCES post_media_backfills(uuid),
 decision_uuid TEXT NOT NULL UNIQUE REFERENCES post_media_decisions(uuid),
 PRIMARY KEY(backfill_uuid,decision_uuid)
) WITHOUT ROWID;
CREATE TRIGGER post_media_backfill_decision_immutable BEFORE UPDATE ON post_media_backfill_decisions
BEGIN SELECT RAISE(ABORT,'post media backfill choices are immutable'); END;
CREATE TRIGGER post_media_backfill_decision_scope BEFORE INSERT ON post_media_backfill_decisions
WHEN NOT EXISTS(SELECT 1 FROM post_media_backfills b JOIN post_media_decisions d ON d.uuid=NEW.decision_uuid
 WHERE b.uuid=NEW.backfill_uuid AND d.post_uuid=b.post_uuid AND d.origin='migration' AND d.state='linked'
 AND d.post_revision>b.post_revision AND d.post_revision<=b.post_revision+b.selected)
BEGIN SELECT RAISE(ABORT,'post media backfill choice requires its matching migration decision'); END;

CREATE TABLE post_media_decision_evidence (
 decision_uuid TEXT NOT NULL REFERENCES post_media_decisions(uuid),
 evidence_uuid TEXT NOT NULL REFERENCES source_media_evidence(uuid),
 post_file_uuid TEXT NOT NULL REFERENCES source_post_file_evidence(uuid),
 match_uuid TEXT NOT NULL REFERENCES source_file_matches(uuid),
 PRIMARY KEY(decision_uuid,evidence_uuid)
) WITHOUT ROWID;
CREATE INDEX post_media_decision_evidence_source ON post_media_decision_evidence(evidence_uuid);
CREATE INDEX post_media_decision_evidence_post_file ON post_media_decision_evidence(post_file_uuid);
CREATE INDEX post_media_decision_evidence_match ON post_media_decision_evidence(match_uuid);
CREATE TRIGGER post_media_decision_evidence_immutable BEFORE UPDATE ON post_media_decision_evidence
BEGIN SELECT RAISE(ABORT,'post media matching evidence is immutable'); END;
CREATE TRIGGER post_media_decision_evidence_scope BEFORE INSERT ON post_media_decision_evidence
WHEN NOT EXISTS(SELECT 1 FROM post_media_decisions d
 JOIN post_media_backfill_decisions b ON b.decision_uuid=d.uuid
 JOIN source_media_evidence e ON e.uuid=NEW.evidence_uuid AND e.post_uuid=d.post_uuid
 JOIN source_post_file_evidence p ON p.uuid=NEW.post_file_uuid AND p.post_uuid=d.post_uuid
 JOIN source_file_matches f ON f.uuid=NEW.match_uuid AND f.observation_uuid=p.observation_uuid
 WHERE d.uuid=NEW.decision_uuid AND d.origin='migration' AND d.state='linked'
 AND e.attachment_uuid IS NULL AND e.basis='legacy' AND p.origin='migration' AND p.basis='catalog-appearance'
 AND p.uuid=json_extract(e.details,'$.source_post_file_evidence_uuid')
 AND f.uuid=json_extract(e.details,'$.source_file_match_uuid') AND f.file_uuid=e.file_uuid)
BEGIN SELECT RAISE(ABORT,'post media matching evidence requires its original post and file chain'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000082,'Reviewed historical post media backfills and retained file proof','{}');
