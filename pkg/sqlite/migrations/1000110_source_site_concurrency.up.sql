-- Gallery-dl selects account directories below a shared media root. That root
-- is a permitted destination, not one file owned by an entire traversal.
-- Different source services may share it; claims still exclude overlapping
-- destinations within a service, and workers lock individual output stems.
DROP INDEX source_runs_running_destination;
CREATE INDEX source_runs_running_destination ON source_runs(destination)
WHERE state='running' AND destination!='';

INSERT INTO native_migration_history(version,name,details)
VALUES(1000110,'Allow independent source services to share a download root','{}');
