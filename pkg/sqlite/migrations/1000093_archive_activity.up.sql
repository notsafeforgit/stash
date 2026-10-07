-- Administrative history pages read compact projections of existing work.
-- No work is admitted, recovered or reclassified by this migration.
CREATE INDEX archive_jobs_kind_history ON archive_jobs(kind,id);
CREATE INDEX archive_jobs_state_history ON archive_jobs(state,id);
CREATE INDEX source_runs_state_history ON source_runs(state,id);
CREATE INDEX source_runs_collection_state_history ON source_runs(collection_uuid,state,id);

INSERT INTO native_migration_history(version,name,details)
VALUES(1000093,'Indexed read-only archive activity history','{}');
