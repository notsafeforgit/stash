-- Permanent historical decisions do not manufacture completed source windows.
-- Qualified source account keys stay independent of performer ownership/merges.
CREATE TABLE source_backfill_decisions (
  id INTEGER PRIMARY KEY,
  uuid TEXT NOT NULL UNIQUE CHECK(length(uuid)=36 AND uuid=lower(uuid)
    AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
    AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
    AND uuid!='00000000-0000-0000-0000-000000000000'),
  root_uuid TEXT NOT NULL REFERENCES media_roots(uuid),
  platform TEXT NOT NULL CHECK(platform IN ('reddit','twitter')),
  account TEXT NOT NULL CHECK(length(account) BETWEEN 1 AND 32),
  component TEXT NOT NULL CHECK(length(component) BETWEEN 1 AND 64),
  outcome TEXT NOT NULL CHECK(outcome IN ('completed','skipped')),
  basis TEXT NOT NULL CHECK(basis IN ('source_runs','legacy_completion','legacy_skip')),
  decided_at TEXT NOT NULL,
  producer_uuid TEXT REFERENCES ingest_producers(uuid),
  source_uuid TEXT,
  source_table TEXT,
  source_key TEXT,
  evidence TEXT NOT NULL CHECK(json_valid(evidence) AND json_type(evidence)='object' AND length(CAST(evidence AS BLOB))<=1048576),
  input_sha256 TEXT NOT NULL CHECK(length(input_sha256)=64 AND input_sha256 NOT GLOB '*[^0-9a-f]*'),
  created_at TEXT NOT NULL,
  UNIQUE(source_uuid,source_table,source_key),
  CHECK((platform='reddit' AND account=lower(account) AND account NOT GLOB '*[^a-z0-9_-]*' AND account!='me')
     OR (platform='twitter' AND length(account)<=30 AND account NOT GLOB '*[^0-9]*' AND (account='0' OR substr(account,1,1)!='0'))),
  CHECK((basis='source_runs' AND outcome='completed' AND component!='*' AND producer_uuid IS NOT NULL AND source_uuid IS NULL AND source_table IS NULL AND source_key IS NULL)
     OR (basis='legacy_completion' AND outcome='completed' AND component!='*' AND producer_uuid IS NULL AND source_uuid IS NOT NULL AND source_table='backfill_completion' AND source_key IS NOT NULL)
     OR (basis='legacy_skip' AND outcome='skipped' AND component='*' AND producer_uuid IS NULL AND source_uuid IS NOT NULL AND source_table='legacy_backfill_skip' AND source_key IS NOT NULL))
);
CREATE INDEX source_backfill_subject ON source_backfill_decisions(root_uuid,platform,account,component,outcome,id);
CREATE TRIGGER source_backfill_immutable BEFORE UPDATE ON source_backfill_decisions
BEGIN SELECT RAISE(ABORT,'source backfill decisions are immutable'); END;

CREATE TABLE source_backfill_requests (
  decision_uuid TEXT NOT NULL REFERENCES source_backfill_decisions(uuid),
  producer_uuid TEXT NOT NULL,
  request_uuid TEXT NOT NULL,
  PRIMARY KEY(decision_uuid,producer_uuid,request_uuid),
  FOREIGN KEY(producer_uuid,request_uuid) REFERENCES source_run_requests(producer_uuid,request_uuid)
);
CREATE INDEX source_backfill_request_reference ON source_backfill_requests(producer_uuid,request_uuid);
CREATE TRIGGER source_backfill_request_valid BEFORE INSERT ON source_backfill_requests
WHEN NOT EXISTS(SELECT 1 FROM source_backfill_decisions d
  JOIN source_run_requests q ON q.producer_uuid=NEW.producer_uuid AND q.request_uuid=NEW.request_uuid
  JOIN source_runs r ON r.uuid=q.run_uuid
  WHERE d.uuid=NEW.decision_uuid AND d.basis='source_runs' AND d.producer_uuid=NEW.producer_uuid
    AND d.root_uuid=r.root_uuid AND r.operation='download')
BEGIN SELECT RAISE(ABORT,'backfill proof requires its producer and media root'); END;
CREATE TRIGGER source_backfill_request_immutable BEFORE UPDATE ON source_backfill_requests
BEGIN SELECT RAISE(ABORT,'source backfill proof is immutable'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000025,'Permanent source backfill decisions with retained legacy evidence','{}');
