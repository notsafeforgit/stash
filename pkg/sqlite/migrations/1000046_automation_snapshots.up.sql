-- Historical operating state is retained before domain mapping or activation.
CREATE TABLE automation_snapshots (
 uuid TEXT PRIMARY KEY CHECK(length(uuid)=36),
 source_uuid TEXT NOT NULL UNIQUE CHECK(length(source_uuid)=36),
 registry_import_uuid TEXT NOT NULL REFERENCES catalog_registry_imports(uuid),
 manifest_sha256 TEXT NOT NULL CHECK(length(manifest_sha256)=64 AND manifest_sha256 NOT GLOB '*[^0-9a-f]*'),
 source_sha256 TEXT NOT NULL CHECK(length(source_sha256)=64 AND source_sha256 NOT GLOB '*[^0-9a-f]*'),
 manifest BLOB NOT NULL CHECK(typeof(manifest)='blob' AND length(manifest) BETWEEN 1 AND 8388608),
 captured_at TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('receiving','received')),
 chunk_count INTEGER NOT NULL CHECK(chunk_count BETWEEN 0 AND 100000),
 record_count INTEGER NOT NULL CHECK(record_count BETWEEN 0 AND 10000000),
 byte_count INTEGER NOT NULL CHECK(byte_count>=0),
 next_chunk INTEGER NOT NULL DEFAULT 0 CHECK(next_chunk BETWEEN 0 AND chunk_count),
 received_records INTEGER NOT NULL DEFAULT 0 CHECK(received_records BETWEEN 0 AND record_count),
 received_bytes INTEGER NOT NULL DEFAULT 0 CHECK(received_bytes BETWEEN 0 AND byte_count),
 last_table TEXT NOT NULL DEFAULT '',
 last_key TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(last_key) AND json_type(last_key)='array' AND length(CAST(last_key AS BLOB))<=32768),
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 CHECK((record_count=0)=(chunk_count=0) AND (record_count=0)=(byte_count=0)),
 CHECK((state='received')=(next_chunk=chunk_count)),
 CHECK(state!='received' OR (received_records=record_count AND received_bytes=byte_count))
);
CREATE TRIGGER automation_snapshot_identity_immutable BEFORE UPDATE ON automation_snapshots
WHEN NEW.uuid!=OLD.uuid OR NEW.source_uuid!=OLD.source_uuid OR NEW.registry_import_uuid!=OLD.registry_import_uuid
 OR NEW.manifest_sha256!=OLD.manifest_sha256 OR NEW.source_sha256!=OLD.source_sha256 OR NEW.manifest!=OLD.manifest
 OR NEW.captured_at!=OLD.captured_at OR NEW.chunk_count!=OLD.chunk_count OR NEW.record_count!=OLD.record_count
 OR NEW.byte_count!=OLD.byte_count OR NEW.created_at!=OLD.created_at
 OR NEW.next_chunk<OLD.next_chunk OR NEW.received_records<OLD.received_records OR NEW.received_bytes<OLD.received_bytes
BEGIN SELECT RAISE(ABORT,'automation snapshot identity is immutable'); END;

CREATE TABLE automation_snapshot_tables (
 snapshot_uuid TEXT NOT NULL REFERENCES automation_snapshots(uuid),
 source_table TEXT NOT NULL,
 received_records INTEGER NOT NULL DEFAULT 0 CHECK(received_records>=0),
 received_bytes INTEGER NOT NULL DEFAULT 0 CHECK(received_bytes>=0),
 received_sha256 TEXT NOT NULL CHECK(length(received_sha256)=64 AND received_sha256 NOT GLOB '*[^0-9a-f]*'),
 hash_state BLOB NOT NULL CHECK(typeof(hash_state)='blob' AND length(hash_state) BETWEEN 1 AND 256),
 PRIMARY KEY(snapshot_uuid,source_table)
);

CREATE TABLE automation_snapshot_chunks (
 snapshot_uuid TEXT NOT NULL REFERENCES automation_snapshots(uuid),
 chunk_index INTEGER NOT NULL CHECK(chunk_index BETWEEN 0 AND 99999),
 sha256 TEXT NOT NULL CHECK(length(sha256)=64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
 record_count INTEGER NOT NULL CHECK(record_count BETWEEN 1 AND 1000),
 byte_count INTEGER NOT NULL CHECK(byte_count BETWEEN 1 AND 16777216),
 created_at TEXT NOT NULL,
 PRIMARY KEY(snapshot_uuid,chunk_index)
);
CREATE TRIGGER automation_snapshot_chunk_immutable BEFORE UPDATE ON automation_snapshot_chunks
BEGIN SELECT RAISE(ABORT,'automation snapshot chunk receipts are immutable'); END;

CREATE TABLE automation_snapshot_records (
 snapshot_uuid TEXT NOT NULL,
 ordinal INTEGER NOT NULL CHECK(ordinal BETWEEN 1 AND 10000000),
 chunk_index INTEGER NOT NULL,
 source_table TEXT NOT NULL,
 source_key TEXT NOT NULL CHECK(json_valid(source_key) AND json_type(source_key)='array' AND length(CAST(source_key AS BLOB))<=32768),
 data TEXT NOT NULL CHECK(json_valid(data) AND json_type(data)='object'),
 byte_count INTEGER NOT NULL CHECK(byte_count=length(CAST(data AS BLOB)) AND byte_count BETWEEN 1 AND 16777216),
 data_sha256 TEXT NOT NULL CHECK(length(data_sha256)=64 AND data_sha256 NOT GLOB '*[^0-9a-f]*'),
 PRIMARY KEY(snapshot_uuid,ordinal),
 UNIQUE(snapshot_uuid,source_table,source_key),
 FOREIGN KEY(snapshot_uuid,chunk_index) REFERENCES automation_snapshot_chunks(snapshot_uuid,chunk_index),
 FOREIGN KEY(snapshot_uuid,source_table) REFERENCES automation_snapshot_tables(snapshot_uuid,source_table)
);
CREATE INDEX automation_snapshot_record_chunk ON automation_snapshot_records(snapshot_uuid,chunk_index,ordinal);
CREATE INDEX automation_snapshot_record_table ON automation_snapshot_records(snapshot_uuid,source_table,ordinal);
CREATE TRIGGER automation_snapshot_record_immutable BEFORE UPDATE ON automation_snapshot_records
BEGIN SELECT RAISE(ABORT,'staged automation records are immutable'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000046,'Resumable receipt of frozen automation state before mapping or activation','{}');
