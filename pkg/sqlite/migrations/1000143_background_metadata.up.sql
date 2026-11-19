-- A background metadata attempt uses spare request capacity beside running
-- downloads. Downloads do not wait for it, and its requests yield to them.
CREATE TABLE enrichment_background_attempts (
 job_uuid TEXT NOT NULL,
 fence INTEGER NOT NULL CHECK(typeof(fence)='integer' AND fence>0),
 PRIMARY KEY(job_uuid,fence),
 FOREIGN KEY(job_uuid,fence) REFERENCES archive_job_attempts(job_uuid,fence)
);
CREATE TRIGGER enrichment_background_attempt_current BEFORE INSERT ON enrichment_background_attempts
WHEN NOT EXISTS(SELECT 1 FROM archive_jobs j JOIN archive_job_attempts a ON a.job_uuid=j.uuid AND a.fence=j.fence
 WHERE j.uuid=NEW.job_uuid AND j.fence=NEW.fence AND j.kind='post.enrich' AND j.state='running' AND a.outcome='running')
BEGIN SELECT RAISE(ABORT,'background metadata requires the current running attempt'); END;
CREATE TRIGGER enrichment_background_attempt_immutable BEFORE UPDATE ON enrichment_background_attempts
BEGIN SELECT RAISE(ABORT,'background metadata attempts are immutable'); END;
INSERT INTO native_migration_history(version,name,details)
VALUES(1000143,'Background metadata beside downloads','{}');
