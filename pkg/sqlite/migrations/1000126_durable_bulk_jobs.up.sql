-- Ordinary library bulk commands share library_jobs admission and lifecycle.
-- A new server fences the previous execution generation before resuming.
CREATE TABLE library_jobs_next (
  id TEXT PRIMARY KEY NOT NULL CHECK(length(id)=36),
  status TEXT NOT NULL CHECK(status IN ('READY','RUNNING','STOPPING','PAUSED','FINISHED','CANCELLED','FAILED','INTERRUPTED')),
  description TEXT NOT NULL,
  progress REAL NOT NULL,
  added_at INTEGER NOT NULL,
  started_at INTEGER,
  ended_at INTEGER,
  error TEXT,
  generation INTEGER NOT NULL DEFAULT 1 CHECK(generation>=1),
  CHECK((status IN ('FINISHED','CANCELLED','FAILED','INTERRUPTED')) = (ended_at IS NOT NULL))
);
INSERT INTO library_jobs_next(id,status,description,progress,added_at,started_at,ended_at,error)
SELECT id,status,description,progress,added_at,started_at,ended_at,error FROM library_jobs;
DROP TABLE library_jobs;
ALTER TABLE library_jobs_next RENAME TO library_jobs;
CREATE INDEX library_jobs_recent ON library_jobs(added_at DESC,id);
CREATE INDEX library_jobs_pending ON library_jobs(status) WHERE ended_at IS NULL;

CREATE TABLE library_bulk_jobs (
  job_id TEXT PRIMARY KEY NOT NULL REFERENCES library_jobs(id) ON DELETE CASCADE,
  command_version INTEGER NOT NULL CHECK(command_version>=1),
  command_kind TEXT NOT NULL CHECK(length(command_kind) BETWEEN 1 AND 64),
  command_json TEXT NOT NULL CHECK(json_valid(command_json) AND json_type(command_json)='object'
                                  AND length(CAST(command_json AS BLOB))<=65536),
  notice_json TEXT CHECK(notice_json IS NULL OR
                        (json_valid(notice_json) AND json_type(notice_json)='object' AND length(CAST(notice_json AS BLOB))<=65536)),
  references_json TEXT NOT NULL CHECK(json_valid(references_json) AND json_type(references_json)='array'
                                     AND length(CAST(references_json AS BLOB))<=65536 AND json_array_length(references_json)<=1000),
  total INTEGER NOT NULL CHECK(total BETWEEN 0 AND 100000),
  processed INTEGER NOT NULL DEFAULT 0 CHECK(processed BETWEEN 0 AND total),
  updated INTEGER NOT NULL DEFAULT 0 CHECK(updated>=0),
  failed INTEGER NOT NULL DEFAULT 0 CHECK(failed>=0),
  notification_failed INTEGER NOT NULL DEFAULT 0 CHECK(notification_failed BETWEEN 0 AND updated),
  errors_json TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(errors_json) AND json_type(errors_json)='array'
                         AND json_array_length(errors_json)<=20 AND length(CAST(errors_json AS BLOB))<=32768),
  CHECK(updated+failed=processed)
);
CREATE TABLE library_bulk_targets (
  job_id TEXT NOT NULL REFERENCES library_bulk_jobs(job_id) ON DELETE CASCADE,
  ordinal INTEGER NOT NULL CHECK(ordinal BETWEEN 0 AND 99999),
  entity_id INTEGER NOT NULL CHECK(entity_id>0),
  entity_uuid TEXT NOT NULL CHECK(length(entity_uuid)=36),
  state TEXT NOT NULL DEFAULT 'PENDING' CHECK(state IN ('PENDING','NOTIFY','DONE','FAILED','NOTIFY_FAILED')),
  hook_attempts INTEGER NOT NULL DEFAULT 0 CHECK(hook_attempts BETWEEN 0 AND 3),
  PRIMARY KEY(job_id,ordinal),
  UNIQUE(job_id,entity_uuid)
);
CREATE INDEX library_bulk_targets_pending ON library_bulk_targets(job_id,ordinal) WHERE state IN ('PENDING','NOTIFY');

INSERT INTO native_migration_history(version,name,details)
VALUES(1000126,'Checkpoint ordinary bulk edits and bound their retained work','{}');
