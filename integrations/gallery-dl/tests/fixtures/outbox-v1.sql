-- Frozen producer schema before offline source-run requests were introduced.
PRAGMA application_id=0x5354494F;
PRAGMA user_version=1;
CREATE TABLE binding(id INTEGER PRIMARY KEY CHECK(id=1), endpoint TEXT NOT NULL, producer TEXT NOT NULL);
CREATE TABLE events(
    seq INTEGER PRIMARY KEY, event_uuid TEXT NOT NULL UNIQUE,
    sha256 TEXT NOT NULL, kind TEXT NOT NULL, collection_uuid TEXT NOT NULL,
    collection_revision INTEGER NOT NULL, root_uuid TEXT, run_uuid TEXT NOT NULL,
    parent_uuid TEXT REFERENCES events(event_uuid), body BLOB, receipt BLOB,
    state TEXT NOT NULL CHECK(state IN ('pending','sending','acknowledged','review')),
    owner TEXT, fence INTEGER NOT NULL DEFAULT 0, lease_until REAL,
    attempts INTEGER NOT NULL DEFAULT 0, available_at REAL NOT NULL,
    created_at REAL NOT NULL, acknowledged_at REAL, error_code TEXT,
    CHECK((state='sending')=(owner IS NOT NULL AND lease_until IS NOT NULL)),
    CHECK((state='acknowledged')=(receipt IS NOT NULL AND body IS NULL)),
    CHECK(state='acknowledged' OR (receipt IS NULL AND body IS NOT NULL))
);
CREATE INDEX ready_events ON events(state,available_at,seq);
CREATE INDEX parent_events ON events(parent_uuid);
CREATE INDEX expired_events ON events(lease_until) WHERE state='sending';
CREATE INDEX queued_sizes ON events(length(body)) WHERE body IS NOT NULL;
