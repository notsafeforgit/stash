-- Source claims are observations, not verified media_contents or playable files.
CREATE TABLE source_content_claims (
 uuid TEXT NOT NULL PRIMARY KEY
 CHECK(length(uuid)=36 AND uuid=lower(uuid) AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-'
 AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-' AND length(replace(uuid,'-',''))=32
 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*' AND uuid!='00000000-0000-0000-0000-000000000000'),
 collection_uuid TEXT NOT NULL,
 collection_revision INTEGER NOT NULL,
 reference_namespace TEXT NOT NULL CHECK(length(reference_namespace) BETWEEN 1 AND 128),
 reference_value TEXT NOT NULL CHECK(length(reference_value) BETWEEN 1 AND 4096),
 digest_algorithm TEXT CHECK(digest_algorithm IS NULL OR length(digest_algorithm) BETWEEN 1 AND 128),
 digest TEXT CHECK(digest IS NULL OR length(digest) BETWEEN 1 AND 4096),
 size INTEGER CHECK(size IS NULL OR (typeof(size)='integer' AND size>=0)),
 source_created_at TEXT NOT NULL CHECK(length(source_created_at)<=64),
 origin TEXT NOT NULL CHECK(origin IN ('migration','ingest','review','scan')),
 observed_at DATETIME NOT NULL,
 details TEXT NOT NULL CHECK(length(CAST(details AS BLOB))<=65536 AND json_valid(details) AND json_type(details)='object'),
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 CHECK((digest_algorithm IS NULL)=(digest IS NULL)),
 UNIQUE(uuid,collection_uuid),
 FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision)
);
CREATE INDEX source_content_claim_reference ON source_content_claims(reference_namespace,reference_value,uuid);
CREATE INDEX source_content_claim_collection ON source_content_claims(collection_uuid,collection_revision,uuid);
CREATE TRIGGER source_content_claim_immutable BEFORE UPDATE ON source_content_claims
BEGIN SELECT RAISE(ABORT,'source content claims are immutable'); END;

CREATE TABLE source_file_observations (
 uuid TEXT NOT NULL PRIMARY KEY
 CHECK(length(uuid)=36 AND uuid=lower(uuid) AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-'
 AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-' AND length(replace(uuid,'-',''))=32
 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*' AND uuid!='00000000-0000-0000-0000-000000000000'),
 content_claim_uuid TEXT,
 collection_uuid TEXT NOT NULL,
 collection_revision INTEGER NOT NULL,
 root_uuid TEXT NOT NULL,
 root_revision INTEGER NOT NULL,
 relative_path TEXT NOT NULL CHECK(length(relative_path) BETWEEN 1 AND 4096),
 archive_path TEXT CHECK(archive_path IS NULL OR length(archive_path) BETWEEN 1 AND 4096),
 state TEXT NOT NULL CHECK(state IN ('present','missing','pending','deduplicated')),
 role TEXT NOT NULL CHECK(role IN ('local','converted-source','source-media-reference')),
 size INTEGER CHECK(size IS NULL OR (typeof(size)='integer' AND size>=0)),
 modified_at_ns INTEGER CHECK(modified_at_ns IS NULL OR typeof(modified_at_ns)='integer'),
 source_first_observed TEXT NOT NULL CHECK(length(source_first_observed)<=64),
 survivor_path TEXT CHECK(survivor_path IS NULL OR length(survivor_path) BETWEEN 1 AND 4096),
 origin TEXT NOT NULL CHECK(origin IN ('migration','ingest','review','scan')),
 observed_at DATETIME NOT NULL,
 details TEXT NOT NULL CHECK(length(CAST(details AS BLOB))<=65536 AND json_valid(details) AND json_type(details)='object'),
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 FOREIGN KEY(content_claim_uuid,collection_uuid) REFERENCES source_content_claims(uuid,collection_uuid),
 FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision),
 FOREIGN KEY(root_uuid,root_revision) REFERENCES media_root_revisions(root_uuid,revision)
);
CREATE INDEX source_file_observation_claim ON source_file_observations(content_claim_uuid,uuid);
CREATE INDEX source_file_observation_location ON source_file_observations(root_uuid,archive_path,relative_path,uuid);
CREATE INDEX source_file_observation_collection ON source_file_observations(collection_uuid,collection_revision,uuid);
CREATE TRIGGER source_file_observation_immutable BEFORE UPDATE ON source_file_observations
BEGIN SELECT RAISE(ABORT,'source file observations are immutable'); END;

CREATE TABLE source_file_matches (
 uuid TEXT NOT NULL PRIMARY KEY
 CHECK(length(uuid)=36 AND uuid=lower(uuid) AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-'
 AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-' AND length(replace(uuid,'-',''))=32
 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*' AND uuid!='00000000-0000-0000-0000-000000000000'),
 observation_uuid TEXT NOT NULL REFERENCES source_file_observations(uuid),
 file_uuid TEXT NOT NULL REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
 generation INTEGER NOT NULL CHECK(typeof(generation)='integer' AND generation>0),
 archive_file_uuid TEXT REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
 archive_generation INTEGER CHECK(archive_generation IS NULL OR (typeof(archive_generation)='integer' AND archive_generation>0)),
 library_root_path TEXT NOT NULL CHECK(length(library_root_path)<=4096),
 basis TEXT NOT NULL CHECK(basis IN ('exact-path','survivor-path','verified-content','review')),
 origin TEXT NOT NULL CHECK(origin IN ('migration','ingest','review','scan')),
 details TEXT NOT NULL CHECK(length(CAST(details AS BLOB))<=65536 AND json_valid(details) AND json_type(details)='object'),
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 CHECK((archive_file_uuid IS NULL)=(archive_generation IS NULL)),
 CHECK((basis IN ('exact-path','survivor-path'))=(library_root_path!=''))
);
CREATE INDEX source_file_match_observation ON source_file_matches(observation_uuid,uuid);
CREATE INDEX source_file_match_file ON source_file_matches(file_uuid,generation,uuid);
CREATE INDEX source_file_match_archive ON source_file_matches(archive_file_uuid) WHERE archive_file_uuid IS NOT NULL;
CREATE TRIGGER source_file_match_generation BEFORE INSERT ON source_file_matches
BEGIN
 SELECT RAISE(ABORT,'source file match requires the active file generation and archive member identity') WHERE NOT EXISTS (
  SELECT 1 FROM archive_entities e JOIN files f ON f.id=e.file_id
  WHERE e.uuid=NEW.file_uuid AND e.kind='file' AND e.state='active' AND f.generation=NEW.generation
  AND ((f.zip_file_id IS NULL AND NEW.archive_file_uuid IS NULL)
   OR EXISTS(SELECT 1 FROM archive_entities z JOIN files zip ON zip.id=z.file_id
    WHERE z.uuid=NEW.archive_file_uuid AND z.kind='file' AND z.state='active' AND z.file_id=f.zip_file_id
      AND zip.generation=NEW.archive_generation AND zip.zip_file_id IS NULL))
 );
END;
CREATE TRIGGER source_file_match_kind_update BEFORE UPDATE OF file_uuid,archive_file_uuid ON source_file_matches
BEGIN
 SELECT RAISE(ABORT,'source file match requires file identities') WHERE EXISTS(
 SELECT 1 FROM archive_entities WHERE uuid IN (NEW.file_uuid,NEW.archive_file_uuid) AND kind!='file');
END;
CREATE TRIGGER source_file_match_immutable BEFORE UPDATE ON source_file_matches
WHEN NEW.uuid!=OLD.uuid OR NEW.observation_uuid!=OLD.observation_uuid OR NEW.generation!=OLD.generation
 OR NEW.archive_generation IS NOT OLD.archive_generation OR NEW.library_root_path!=OLD.library_root_path
 OR NEW.basis!=OLD.basis OR NEW.origin!=OLD.origin OR NEW.details!=OLD.details OR NEW.created_at!=OLD.created_at
 OR (NEW.file_uuid!=OLD.file_uuid AND EXISTS(SELECT 1 FROM archive_entities WHERE uuid=OLD.file_uuid))
 OR (NEW.archive_file_uuid IS NOT OLD.archive_file_uuid AND (NEW.archive_file_uuid IS NULL OR OLD.archive_file_uuid IS NULL OR EXISTS(SELECT 1 FROM archive_entities WHERE uuid=OLD.archive_file_uuid)))
BEGIN SELECT RAISE(ABORT,'source file matches are immutable'); END;

CREATE TABLE source_post_file_evidence (
 uuid TEXT NOT NULL PRIMARY KEY
 CHECK(length(uuid)=36 AND uuid=lower(uuid) AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-'
 AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-' AND length(replace(uuid,'-',''))=32
 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*' AND uuid!='00000000-0000-0000-0000-000000000000'),
 post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
 observation_uuid TEXT NOT NULL REFERENCES source_file_observations(uuid),
 origin TEXT NOT NULL CHECK(origin IN ('capture','migration','review')),
 basis TEXT NOT NULL CHECK(length(basis) BETWEEN 1 AND 128),
 observed_at DATETIME NOT NULL,
 details TEXT NOT NULL CHECK(length(CAST(details AS BLOB))<=65536 AND json_valid(details) AND json_type(details)='object')
);
CREATE INDEX source_post_file_evidence_post ON source_post_file_evidence(post_uuid,uuid);
CREATE INDEX source_post_file_evidence_observation ON source_post_file_evidence(observation_uuid,uuid);
CREATE TRIGGER source_post_file_evidence_immutable BEFORE UPDATE ON source_post_file_evidence
BEGIN SELECT RAISE(ABORT,'source post file evidence is immutable'); END;
CREATE TRIGGER source_post_file_evidence_active_post BEFORE INSERT ON source_post_file_evidence
WHEN EXISTS(SELECT 1 FROM source_posts WHERE uuid=NEW.post_uuid AND state!='active')
BEGIN SELECT RAISE(ABORT,'source post has been forgotten'); END;
CREATE TRIGGER source_post_file_evidence_revision AFTER INSERT ON source_post_file_evidence
BEGIN UPDATE source_posts SET revision=revision+1 WHERE uuid=NEW.post_uuid; END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000037,'Source content claims, file observations and guarded library file matches','{}');
