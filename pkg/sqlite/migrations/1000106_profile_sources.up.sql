-- Historical URL definitions remain only as immutable receipt/run provenance.
-- Current subscription discovery and edits operate on the profile source.
CREATE TABLE source_collection_aliases (
 alias_uuid TEXT PRIMARY KEY NOT NULL REFERENCES source_collections(uuid),
 source_uuid TEXT NOT NULL REFERENCES source_collections(uuid),
 CHECK(alias_uuid!=source_uuid)
);
CREATE INDEX source_collection_alias_source ON source_collection_aliases(source_uuid,alias_uuid);
CREATE TRIGGER source_collection_alias_immutable BEFORE UPDATE ON source_collection_aliases
BEGIN SELECT RAISE(ABORT,'source collection aliases are immutable'); END;
CREATE TRIGGER source_collection_alias_retained BEFORE DELETE ON source_collection_aliases
BEGIN SELECT RAISE(ABORT,'source collection aliases retain historical receipts'); END;
CREATE TRIGGER source_collection_alias_frozen BEFORE INSERT ON source_collection_revisions
WHEN EXISTS(SELECT 1 FROM source_collection_aliases WHERE alias_uuid=NEW.collection_uuid)
BEGIN SELECT RAISE(ABORT,'edit the canonical profile source'); END;

CREATE TABLE source_run_retrievals (
 run_uuid TEXT PRIMARY KEY NOT NULL REFERENCES source_runs(uuid),
 url TEXT NOT NULL CHECK(length(url) BETWEEN 1 AND 8192)
);
CREATE TRIGGER source_run_retrieval_immutable BEFORE UPDATE ON source_run_retrievals
BEGIN SELECT RAISE(ABORT,'source run retrieval is immutable'); END;
CREATE TRIGGER source_run_retrieval_retained BEFORE DELETE ON source_run_retrievals
BEGIN SELECT RAISE(ABORT,'source run retrieval retains historical execution identity'); END;

INSERT INTO native_migration_history(version,name,details) VALUES(1000106,'One subscribed Reddit profile with internal retrieval passes','{}');
