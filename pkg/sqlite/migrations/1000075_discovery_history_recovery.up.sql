CREATE TABLE discovery_listing_recoveries (
 listing_uuid TEXT PRIMARY KEY NOT NULL REFERENCES discovery_listings(uuid),
 previous_listing_uuid TEXT NOT NULL UNIQUE REFERENCES discovery_listings(uuid),
 previous_sha256 TEXT NOT NULL CHECK(length(previous_sha256)=64 AND previous_sha256 NOT GLOB '*[^0-9a-f]*'),
 CHECK(listing_uuid!=previous_listing_uuid)
);
CREATE TRIGGER discovery_listing_recovery_immutable BEFORE UPDATE ON discovery_listing_recoveries
BEGIN SELECT RAISE(ABORT,'discovery recovery references are immutable'); END;
CREATE TRIGGER discovery_listing_recovery_scope BEFORE INSERT ON discovery_listing_recoveries
WHEN NOT EXISTS(SELECT 1 FROM discovery_listings n JOIN discovery_listings p ON p.uuid=NEW.previous_listing_uuid
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
CREATE TRIGGER discovery_recovery_job_scope BEFORE INSERT ON discovery_listing_jobs
WHEN EXISTS(SELECT 1 FROM discovery_listing_recoveries WHERE previous_listing_uuid=NEW.listing_uuid)
BEGIN SELECT RAISE(ABORT,'recovered discovery searches cannot admit more jobs'); END;
CREATE TRIGGER discovery_recovery_page_scope BEFORE INSERT ON discovery_pages
WHEN EXISTS(SELECT 1 FROM discovery_listing_recoveries WHERE previous_listing_uuid=NEW.listing_uuid)
BEGIN SELECT RAISE(ABORT,'recovered discovery searches retain their original final evidence'); END;

CREATE TABLE discovery_recovery_targets (
 target_uuid TEXT PRIMARY KEY NOT NULL REFERENCES discovery_match_targets(uuid),
 previous_target_uuid TEXT NOT NULL UNIQUE REFERENCES discovery_match_targets(uuid)
);
CREATE TRIGGER discovery_recovery_target_immutable BEFORE UPDATE ON discovery_recovery_targets
BEGIN SELECT RAISE(ABORT,'discovery recovery target references are immutable'); END;
CREATE TRIGGER discovery_recovery_target_scope BEFORE INSERT ON discovery_recovery_targets
WHEN NOT EXISTS(SELECT 1 FROM discovery_match_targets n JOIN discovery_match_targets p ON p.uuid=NEW.previous_target_uuid
 JOIN discovery_listing_recoveries r ON r.listing_uuid=n.listing_uuid AND r.previous_listing_uuid=p.listing_uuid
 WHERE n.uuid=NEW.target_uuid AND n.snapshot_uuid=p.snapshot_uuid AND n.source_ordinal=p.source_ordinal
 AND n.source_sha256=p.source_sha256 AND n.post_uuid=p.post_uuid)
BEGIN SELECT RAISE(ABORT,'discovery recovery target must preserve its original evidence'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000075,'Reviewed recovery of discovery searches with missing historical bodies','{}');
