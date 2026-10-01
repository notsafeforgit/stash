-- Source traversal has mutable, coalesced date windows. It is separate from
-- immutable media.verify tasks, which are admitted by completed-file receipts.
CREATE TABLE source_runs (
  id INTEGER PRIMARY KEY,
  uuid TEXT NOT NULL UNIQUE CHECK(length(uuid)=36 AND uuid=lower(uuid)
    AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
    AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
    AND uuid!='00000000-0000-0000-0000-000000000000'),
  collection_uuid TEXT NOT NULL,
  collection_revision INTEGER NOT NULL,
  root_uuid TEXT,
  root_revision INTEGER,
  operation TEXT NOT NULL CHECK(operation IN ('download','enrich')),
  policy_sha256 TEXT NOT NULL CHECK(length(policy_sha256)=64 AND policy_sha256 NOT GLOB '*[^0-9a-f]*'),
  cooldown_seconds INTEGER NOT NULL CHECK(cooldown_seconds BETWEEN 0 AND 86400),
  work_key TEXT NOT NULL CHECK(length(work_key)=64 AND work_key NOT GLOB '*[^0-9a-f]*'),
  target_key TEXT NOT NULL CHECK(length(target_key)=64 AND target_key NOT GLOB '*[^0-9a-f]*'),
  destination TEXT NOT NULL DEFAULT '' CHECK(length(destination)<=8192),
  root_identity TEXT NOT NULL DEFAULT '' CHECK(length(root_identity)<=128),
  destination_prefix TEXT NOT NULL DEFAULT '' CHECK(length(destination_prefix)<=4096),
  state TEXT NOT NULL DEFAULT 'queued' CHECK(state IN ('queued','running','succeeded','deferred','cancelled')),
  revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0),
  fence INTEGER NOT NULL DEFAULT 0 CHECK(fence>=0),
  failures INTEGER NOT NULL DEFAULT 0 CHECK(failures BETWEEN 0 AND 8),
  pending TEXT NOT NULL CHECK(json_valid(pending) AND json_type(pending)='array' AND json_array_length(pending)<=64 AND length(pending)<=32768),
  completed TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(completed) AND json_type(completed)='array' AND json_array_length(completed)<=64 AND length(completed)<=32768),
  window TEXT CHECK(window IS NULL OR (json_valid(window) AND json_type(window)='object' AND length(window)<=512)),
  producer_uuid TEXT REFERENCES ingest_producers(uuid),
  owner_uuid TEXT CHECK(owner_uuid IS NULL OR (length(owner_uuid)=36 AND owner_uuid=lower(owner_uuid)
    AND substr(owner_uuid,9,1)='-' AND substr(owner_uuid,14,1)='-' AND substr(owner_uuid,19,1)='-' AND substr(owner_uuid,24,1)='-'
    AND length(replace(owner_uuid,'-',''))=32 AND replace(owner_uuid,'-','') NOT GLOB '*[^0-9a-f]*'
    AND owner_uuid!='00000000-0000-0000-0000-000000000000')),
  lease_until_ms INTEGER,
  available_at_ms INTEGER NOT NULL CHECK(available_at_ms>0),
  progress TEXT NOT NULL DEFAULT '{"items_seen":0,"files_completed":0,"cursor":""}' CHECK(json_valid(progress) AND json_type(progress)='object' AND length(progress)<=4096),
  error_code TEXT NOT NULL DEFAULT '' CHECK(length(error_code)<=128),
  created_at_ms INTEGER NOT NULL CHECK(created_at_ms>0),
  updated_at_ms INTEGER NOT NULL CHECK(updated_at_ms>=created_at_ms),
  FOREIGN KEY(collection_uuid,collection_revision) REFERENCES source_collection_revisions(collection_uuid,revision),
  FOREIGN KEY(root_uuid,root_revision) REFERENCES media_root_revisions(root_uuid,revision),
  CHECK((root_uuid IS NULL AND root_revision IS NULL) OR (root_uuid IS NOT NULL AND root_revision IS NOT NULL)),
  CHECK(operation!='download' OR root_uuid IS NOT NULL),
  CHECK((state='running' AND producer_uuid IS NOT NULL AND owner_uuid IS NOT NULL AND lease_until_ms IS NOT NULL AND window IS NOT NULL AND fence>0)
    OR (state!='running' AND producer_uuid IS NULL AND owner_uuid IS NULL AND lease_until_ms IS NULL AND window IS NULL)),
  CHECK(state NOT IN ('queued','deferred') OR json_array_length(pending)>0),
  CHECK(state!='succeeded' OR json_array_length(pending)=0)
);
CREATE UNIQUE INDEX source_runs_active_work ON source_runs(work_key) WHERE state IN ('queued','running','deferred');
CREATE UNIQUE INDEX source_runs_running_collection ON source_runs(collection_uuid) WHERE state='running';
CREATE UNIQUE INDEX source_runs_running_target ON source_runs(target_key) WHERE state='running';
CREATE UNIQUE INDEX source_runs_running_destination ON source_runs(destination) WHERE state='running' AND destination!='';
CREATE INDEX source_runs_running_root ON source_runs(root_identity,destination_prefix) WHERE state='running' AND root_identity!='';
CREATE INDEX source_runs_expired ON source_runs(lease_until_ms,id) WHERE state='running';
CREATE INDEX source_runs_collection_page ON source_runs(collection_uuid,id);
CREATE INDEX source_runs_scope_page ON source_runs(collection_uuid,root_uuid,id);
CREATE INDEX source_runs_active ON source_runs(state,id) WHERE state IN ('queued','running','deferred');
CREATE TRIGGER source_run_scope BEFORE INSERT ON source_runs
WHEN NEW.root_uuid IS NOT (SELECT root_uuid FROM source_collection_revisions WHERE collection_uuid=NEW.collection_uuid AND revision=NEW.collection_revision)
BEGIN SELECT RAISE(ABORT,'source run root differs from collection'); END;
CREATE TRIGGER source_run_identity BEFORE UPDATE ON source_runs
WHEN NEW.id!=OLD.id OR NEW.uuid!=OLD.uuid OR NEW.collection_uuid!=OLD.collection_uuid OR NEW.collection_revision!=OLD.collection_revision
  OR NEW.root_uuid IS NOT OLD.root_uuid OR NEW.root_revision IS NOT OLD.root_revision OR NEW.operation!=OLD.operation
  OR NEW.policy_sha256!=OLD.policy_sha256 OR NEW.cooldown_seconds!=OLD.cooldown_seconds OR NEW.work_key!=OLD.work_key
  OR NEW.target_key!=OLD.target_key OR NEW.created_at_ms!=OLD.created_at_ms
BEGIN SELECT RAISE(ABORT,'source run definition is immutable'); END;
CREATE TRIGGER source_run_transition BEFORE UPDATE ON source_runs
WHEN OLD.state IN ('succeeded','cancelled') OR NEW.revision!=OLD.revision+1 OR NEW.updated_at_ms<OLD.updated_at_ms
  OR (NEW.state='succeeded' AND OLD.state!='running')
  OR (NEW.state='running' AND OLD.state!='running' AND (OLD.state!='queued' OR NEW.fence!=OLD.fence+1))
  OR ((NEW.state!='running' OR OLD.state='running') AND NEW.fence!=OLD.fence)
  OR (OLD.state='running' AND NEW.state='running' AND (NEW.owner_uuid!=OLD.owner_uuid OR NEW.producer_uuid!=OLD.producer_uuid OR NEW.window!=OLD.window
    OR NEW.destination!=OLD.destination OR NEW.root_identity!=OLD.root_identity OR NEW.destination_prefix!=OLD.destination_prefix))
BEGIN SELECT RAISE(ABORT,'invalid source run transition'); END;

CREATE TABLE source_run_requests (
  producer_uuid TEXT NOT NULL REFERENCES ingest_producers(uuid),
  request_uuid TEXT NOT NULL CHECK(length(request_uuid)=36 AND request_uuid=lower(request_uuid)
    AND substr(request_uuid,9,1)='-' AND substr(request_uuid,14,1)='-' AND substr(request_uuid,19,1)='-' AND substr(request_uuid,24,1)='-'
    AND length(replace(request_uuid,'-',''))=32 AND replace(request_uuid,'-','') NOT GLOB '*[^0-9a-f]*'
    AND request_uuid!='00000000-0000-0000-0000-000000000000'),
  digest TEXT NOT NULL CHECK(length(digest)=64 AND digest NOT GLOB '*[^0-9a-f]*'),
  run_uuid TEXT NOT NULL REFERENCES source_runs(uuid),
  created_at_ms INTEGER NOT NULL,
  PRIMARY KEY(producer_uuid,request_uuid)
);
CREATE INDEX source_run_requests_run ON source_run_requests(run_uuid);
CREATE TRIGGER source_run_request_immutable BEFORE UPDATE ON source_run_requests
BEGIN SELECT RAISE(ABORT,'source run requests are immutable'); END;

CREATE TABLE source_run_attempts (
  run_uuid TEXT NOT NULL REFERENCES source_runs(uuid),
  fence INTEGER NOT NULL CHECK(fence>0),
  producer_uuid TEXT NOT NULL REFERENCES ingest_producers(uuid),
  owner_uuid TEXT NOT NULL,
  window TEXT NOT NULL CHECK(json_valid(window) AND json_type(window)='object' AND length(window)<=512),
  progress TEXT NOT NULL CHECK(json_valid(progress) AND json_type(progress)='object' AND length(progress)<=4096),
  started_at_ms INTEGER NOT NULL,
  ended_at_ms INTEGER,
  outcome TEXT NOT NULL DEFAULT 'running' CHECK(outcome IN ('running','succeeded','retry','deferred','cancelled','expired')),
  error_code TEXT NOT NULL DEFAULT '' CHECK(length(error_code)<=128),
  PRIMARY KEY(run_uuid,fence),
  CHECK((outcome='running' AND ended_at_ms IS NULL) OR (outcome!='running' AND ended_at_ms>=started_at_ms))
);
CREATE TRIGGER source_run_attempt_valid BEFORE INSERT ON source_run_attempts
WHEN NOT EXISTS(SELECT 1 FROM source_runs WHERE uuid=NEW.run_uuid AND state='running' AND fence=NEW.fence
  AND producer_uuid=NEW.producer_uuid AND owner_uuid=NEW.owner_uuid AND window=NEW.window AND progress=NEW.progress)
BEGIN SELECT RAISE(ABORT,'source attempt requires the current lease'); END;
CREATE TRIGGER source_run_attempt_immutable BEFORE UPDATE ON source_run_attempts
WHEN OLD.outcome!='running' OR NEW.run_uuid!=OLD.run_uuid OR NEW.fence!=OLD.fence OR NEW.producer_uuid!=OLD.producer_uuid
  OR NEW.owner_uuid!=OLD.owner_uuid OR NEW.window!=OLD.window OR NEW.started_at_ms!=OLD.started_at_ms
BEGIN SELECT RAISE(ABORT,'finished source attempts are immutable'); END;

-- Pacing applies across policy changes and separate collections of the same URL.
CREATE TABLE source_run_cooldowns (
  target_key TEXT NOT NULL PRIMARY KEY CHECK(length(target_key)=64 AND target_key NOT GLOB '*[^0-9a-f]*'),
  available_at_ms INTEGER NOT NULL CHECK(available_at_ms>0)
);
CREATE TABLE source_run_reviews (
  run_uuid TEXT NOT NULL REFERENCES source_runs(uuid),
  revision INTEGER NOT NULL,
  action TEXT NOT NULL CHECK(action IN ('retry','cancel')),
  created_at_ms INTEGER NOT NULL,
  PRIMARY KEY(run_uuid,revision)
);
CREATE TRIGGER source_run_review_immutable BEFORE UPDATE ON source_run_reviews
BEGIN SELECT RAISE(ABORT,'source run reviews are immutable'); END;
INSERT INTO native_migration_history(version,name,details)
VALUES(1000023,'Coalesced source runs with fenced ownership and durable deferrals','{}');
