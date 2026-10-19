-- Ordinary library job admission/outcomes are independent of source tracking.
-- Executors without durable checkpoints are explicitly interrupted on restart.
CREATE TABLE library_jobs (
  id TEXT PRIMARY KEY NOT NULL CHECK(length(id)=36),
  status TEXT NOT NULL CHECK(status IN ('READY','RUNNING','STOPPING','FINISHED','CANCELLED','FAILED','INTERRUPTED')),
  description TEXT NOT NULL,
  progress REAL NOT NULL,
  added_at INTEGER NOT NULL,
  started_at INTEGER,
  ended_at INTEGER,
  error TEXT,
  CHECK((status IN ('FINISHED','CANCELLED','FAILED','INTERRUPTED')) = (ended_at IS NOT NULL))
);
CREATE INDEX library_jobs_recent ON library_jobs(added_at DESC,id);
CREATE INDEX library_jobs_pending ON library_jobs(status) WHERE ended_at IS NULL;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000123,'Persist ordinary library job admission and outcomes','{}');
