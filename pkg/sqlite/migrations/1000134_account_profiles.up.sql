-- Retain existing receipts and their references while admitting standalone profiles.
PRAGMA defer_foreign_keys=ON;
CREATE TABLE saved_profile_receipts AS SELECT * FROM ingest_receipts;
DROP TABLE ingest_receipts;
CREATE TABLE ingest_receipts (
 producer_uuid TEXT NOT NULL REFERENCES ingest_producers(uuid),
 event_uuid TEXT NOT NULL CHECK(length(event_uuid)=36 AND event_uuid=lower(event_uuid)
  AND substr(event_uuid,9,1)='-' AND substr(event_uuid,14,1)='-' AND substr(event_uuid,19,1)='-' AND substr(event_uuid,24,1)='-'
  AND length(replace(event_uuid,'-',''))=32 AND replace(event_uuid,'-','') NOT GLOB '*[^0-9a-f]*'
  AND event_uuid!='00000000-0000-0000-0000-000000000000'),
 digest TEXT NOT NULL CHECK(length(digest)=64 AND digest NOT GLOB '*[^0-9a-f]*'),
 credential_uuid TEXT NOT NULL,
 collection_uuid TEXT NOT NULL,
 collection_revision INTEGER NOT NULL,
 root_uuid TEXT REFERENCES media_roots(uuid),
 run_uuid TEXT NOT NULL CHECK(length(run_uuid)=36 AND run_uuid=lower(run_uuid)
  AND substr(run_uuid,9,1)='-' AND substr(run_uuid,14,1)='-' AND substr(run_uuid,19,1)='-' AND substr(run_uuid,24,1)='-'
  AND length(replace(run_uuid,'-',''))=32 AND replace(run_uuid,'-','') NOT GLOB '*[^0-9a-f]*'
  AND run_uuid!='00000000-0000-0000-0000-000000000000'),
 kind TEXT NOT NULL CHECK(kind IN ('source.capture','file.completed','attachment.download','account.profile')),
 post_uuid TEXT REFERENCES source_posts(uuid),
 capture_uuid TEXT REFERENCES source_captures(uuid),
 job_uuid TEXT REFERENCES archive_jobs(uuid),
 result TEXT NOT NULL CHECK(json_valid(result) AND json_type(result)='object' AND length(result)<=16384),
 committed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY(producer_uuid,event_uuid),
 FOREIGN KEY(producer_uuid,credential_uuid) REFERENCES ingest_credentials(producer_uuid,uuid),
 FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision),
 FOREIGN KEY(post_uuid,capture_uuid) REFERENCES source_captures(post_uuid,uuid),
 CHECK((post_uuid IS NULL)=(capture_uuid IS NULL)),
 CHECK((kind='source.capture' AND capture_uuid IS NOT NULL AND job_uuid IS NULL) OR
       (kind='file.completed' AND job_uuid IS NOT NULL AND root_uuid IS NOT NULL) OR
       (kind='attachment.download' AND capture_uuid IS NOT NULL AND job_uuid IS NULL AND root_uuid IS NOT NULL) OR
       (kind='account.profile' AND capture_uuid IS NULL AND job_uuid IS NULL))
);
INSERT INTO ingest_receipts SELECT * FROM saved_profile_receipts;
DROP TABLE saved_profile_receipts;
CREATE INDEX ingest_receipts_capture ON ingest_receipts(capture_uuid);
CREATE UNIQUE INDEX ingest_receipts_job ON ingest_receipts(job_uuid) WHERE job_uuid IS NOT NULL;
CREATE TRIGGER ingest_receipt_scope BEFORE INSERT ON ingest_receipts
BEGIN
 SELECT RAISE(ABORT,'ingestion receipt root is outside its credential scope') WHERE NOT EXISTS
  (SELECT 1 FROM ingest_credential_scopes WHERE credential_uuid=NEW.credential_uuid
   AND collection_uuid=NEW.collection_uuid AND root_uuid IS NEW.root_uuid)
  AND NOT EXISTS(SELECT 1 FROM ingest_credential_roots WHERE credential_uuid=NEW.credential_uuid AND root_uuid=NEW.root_uuid);
 SELECT RAISE(ABORT,'ingestion receipt root differs from its collection revision') WHERE NEW.root_uuid IS NOT
  (SELECT root_uuid FROM source_collection_revisions WHERE collection_uuid=NEW.collection_uuid AND revision=NEW.collection_revision);
 SELECT RAISE(ABORT,'ingestion receipt requires capture provenance') WHERE NEW.capture_uuid IS NOT NULL AND NOT EXISTS
  (SELECT 1 FROM source_collection_captures WHERE capture_uuid=NEW.capture_uuid AND collection_uuid=NEW.collection_uuid AND collection_revision=NEW.collection_revision);
 SELECT RAISE(ABORT,'file ingestion receipt requires media verification work') WHERE NEW.job_uuid IS NOT NULL AND NOT EXISTS
  (SELECT 1 FROM archive_jobs WHERE uuid=NEW.job_uuid AND kind='media.verify');
END;
CREATE TRIGGER ingest_receipt_immutable BEFORE UPDATE ON ingest_receipts
BEGIN SELECT RAISE(ABORT,'ingestion receipts are immutable'); END;

CREATE TRIGGER attachment_download_receipt BEFORE INSERT ON ingest_receipts
WHEN NEW.kind='attachment.download'
BEGIN
 SELECT RAISE(ABORT,'attachment download receipt requires its atomic report') WHERE NOT EXISTS(
  SELECT 1 FROM source_attachment_downloads d JOIN ingest_receipts c ON c.producer_uuid=d.producer_uuid AND c.event_uuid=d.capture_event_uuid
  WHERE d.producer_uuid=NEW.producer_uuid AND d.event_uuid=NEW.event_uuid AND d.run_uuid=NEW.run_uuid
  AND c.collection_uuid=NEW.collection_uuid AND c.collection_revision=NEW.collection_revision
  AND c.root_uuid=NEW.root_uuid AND c.post_uuid=NEW.post_uuid AND c.capture_uuid=NEW.capture_uuid);
END;

CREATE INDEX ingest_receipts_collection ON ingest_receipts(collection_uuid);

CREATE TABLE source_account_profiles (
 uuid TEXT PRIMARY KEY NOT NULL CHECK(length(uuid)=36),
 producer_uuid TEXT NOT NULL,
 event_uuid TEXT NOT NULL,
 collection_uuid TEXT NOT NULL,
 collection_revision INTEGER NOT NULL,
 account_uuid TEXT REFERENCES source_accounts(uuid) ON UPDATE CASCADE,
 observed_at DATETIME NOT NULL,
 extractor_version TEXT NOT NULL CHECK(length(extractor_version) BETWEEN 1 AND 128),
 source TEXT NOT NULL CHECK(json_valid(source) AND json_type(source)='object' AND length(CAST(source AS BLOB))<=65536),
 review TEXT NOT NULL CHECK(review IN ('','locator_only_candidates','ambiguous_stable_id','stable_id_conflict')),
 CHECK((account_uuid IS NULL)=(review!='')),
 UNIQUE(producer_uuid,event_uuid),
 FOREIGN KEY(producer_uuid,event_uuid) REFERENCES ingest_receipts(producer_uuid,event_uuid) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision)
);
CREATE INDEX source_account_profiles_account ON source_account_profiles(account_uuid);
CREATE TRIGGER source_account_profile_immutable BEFORE UPDATE ON source_account_profiles
BEGIN SELECT RAISE(ABORT,'account profile observations are immutable'); END;
CREATE TRIGGER account_profile_receipt BEFORE INSERT ON ingest_receipts
WHEN NEW.kind='account.profile'
BEGIN
 SELECT RAISE(ABORT,'profile receipt requires its atomic observation') WHERE NOT EXISTS(
 SELECT 1 FROM source_account_profiles p WHERE p.producer_uuid=NEW.producer_uuid AND p.event_uuid=NEW.event_uuid
 AND p.collection_uuid=NEW.collection_uuid AND p.collection_revision=NEW.collection_revision
 AND p.uuid=json_extract(NEW.result,'$.profile_uuid')
 AND coalesce(p.account_uuid,'')=coalesce(json_extract(NEW.result,'$.account_uuid'),'')
 AND p.review=coalesce(json_extract(NEW.result,'$.review'),'')
 AND json_extract(NEW.result,'$.status')='recorded' AND json_extract(NEW.result,'$.media_ingested')=0);
END;
CREATE TABLE account_observed_profile_urls (
 account_uuid TEXT NOT NULL REFERENCES source_accounts(uuid) ON UPDATE CASCADE,
 url_key TEXT NOT NULL CHECK(length(url_key) BETWEEN 1 AND 8192),
 url TEXT NOT NULL CHECK(length(url) BETWEEN 1 AND 4096),
 profile_uuid TEXT NOT NULL REFERENCES source_account_profiles(uuid),
 PRIMARY KEY(account_uuid,url_key)
);
CREATE INDEX account_observed_profile_urls_profile ON account_observed_profile_urls(profile_uuid);
CREATE TRIGGER account_observed_profile_url_scope BEFORE INSERT ON account_observed_profile_urls
BEGIN
 SELECT RAISE(ABORT,'profile link requires its observed account') WHERE NOT EXISTS(
 SELECT 1 FROM source_account_profiles WHERE uuid=NEW.profile_uuid AND account_uuid=NEW.account_uuid);
END;
CREATE TRIGGER account_observed_profile_url_update BEFORE UPDATE ON account_observed_profile_urls
BEGIN
 SELECT RAISE(ABORT,'profile link requires its observed account') WHERE NOT EXISTS(
 SELECT 1 FROM source_account_profiles WHERE uuid=NEW.profile_uuid AND account_uuid=NEW.account_uuid);
END;
INSERT INTO native_migration_history(version,name,details)
VALUES(1000134,'Independent account profile observations and links','{}');
