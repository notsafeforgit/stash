-- Frozen operational input remains inspectable until source/configuration
-- bindings and legacy cursors have been explicitly converted to native work.
CREATE TABLE scan_journals (
  uuid TEXT PRIMARY KEY CHECK(length(uuid)=36),
  root_uuid TEXT NOT NULL REFERENCES media_roots(uuid),
  source_uuid TEXT NOT NULL CHECK(length(source_uuid)=36),
  input_sha256 TEXT NOT NULL CHECK(length(input_sha256)=64 AND input_sha256 NOT GLOB '*[^0-9a-f]*'),
  captured_at TEXT NOT NULL,
  record_count INTEGER NOT NULL CHECK(record_count BETWEEN 0 AND 10000),
  inventory TEXT NOT NULL CHECK(json_valid(inventory) AND json_type(inventory)='object' AND length(inventory)<=4096),
  created_at TEXT NOT NULL
);
CREATE INDEX scan_journals_source ON scan_journals(root_uuid,source_uuid,created_at,uuid);
CREATE TRIGGER scan_journal_immutable BEFORE UPDATE ON scan_journals
BEGIN SELECT RAISE(ABORT,'scan journal snapshots are immutable'); END;

CREATE TABLE scan_journal_records (
  id INTEGER PRIMARY KEY,
  uuid TEXT NOT NULL UNIQUE CHECK(length(uuid)=36),
  journal_uuid TEXT NOT NULL REFERENCES scan_journals(uuid),
  source_table TEXT NOT NULL CHECK(source_table IN ('scan_jobs','extractor_jobs','scan_deferrals',
    'backfill_scan_completion','collection_backfill_completion','backfill_policy_migrations','legacy_handoffs')),
  source_key TEXT NOT NULL CHECK(json_valid(source_key) AND json_type(source_key)='array' AND length(source_key)<=8192),
  context TEXT NOT NULL DEFAULT '' CHECK(context IN ('','host','n8n')),
  target_url TEXT NOT NULL DEFAULT '' CHECK(length(target_url)<=8192),
  disposition TEXT NOT NULL CHECK(disposition IN ('pending_binding','historical','review')),
  summary TEXT NOT NULL CHECK(json_valid(summary) AND json_type(summary)='object' AND length(CAST(summary AS BLOB))<=16384),
  evidence TEXT NOT NULL CHECK(json_valid(evidence) AND json_type(evidence)='object' AND length(CAST(evidence AS BLOB))<=1048576),
  UNIQUE(journal_uuid,source_table,source_key)
);
CREATE INDEX scan_journal_record_page ON scan_journal_records(journal_uuid,id);
CREATE INDEX scan_journal_table_page ON scan_journal_records(journal_uuid,source_table,id);
CREATE INDEX scan_journal_target ON scan_journal_records(journal_uuid,context,target_url,id);
CREATE TRIGGER scan_journal_record_immutable BEFORE UPDATE ON scan_journal_records
BEGIN SELECT RAISE(ABORT,'scan journal evidence is immutable'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000026,'Frozen scan journal evidence with explicit migration dispositions','{}');
