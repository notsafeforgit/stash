CREATE TABLE archive_jobs (
 id INTEGER PRIMARY KEY,
 uuid TEXT NOT NULL UNIQUE CHECK(length(uuid)=36 AND uuid=lower(uuid)
   AND substr(uuid,9,1)='-' AND substr(uuid,14,1)='-' AND substr(uuid,19,1)='-' AND substr(uuid,24,1)='-'
   AND length(replace(uuid,'-',''))=32 AND replace(uuid,'-','') NOT GLOB '*[^0-9a-f]*'
   AND uuid!='00000000-0000-0000-0000-000000000000'),
 kind TEXT NOT NULL CHECK(kind='media.verify'),
 work_key TEXT NOT NULL CHECK(length(work_key)=64 AND work_key NOT GLOB '*[^0-9a-f]*'),
 resource_key TEXT NOT NULL CHECK(length(resource_key)=64 AND resource_key NOT GLOB '*[^0-9a-f]*'),
 arguments TEXT NOT NULL CHECK(json_valid(arguments) AND json_type(arguments)='object' AND length(CAST(arguments AS BLOB))<=262144),
 state TEXT NOT NULL DEFAULT 'queued' CHECK(state IN ('queued','running','succeeded','failed','cancelled')),
 revision INTEGER NOT NULL DEFAULT 1 CHECK(typeof(revision)='integer' AND revision>0),
 priority INTEGER NOT NULL CHECK(typeof(priority)='integer' AND priority BETWEEN 0 AND 100),
 fence INTEGER NOT NULL DEFAULT 0 CHECK(typeof(fence)='integer' AND fence>=0),
 max_attempts INTEGER NOT NULL CHECK(typeof(max_attempts)='integer' AND max_attempts BETWEEN 1 AND 100),
 available_at_ms INTEGER NOT NULL CHECK(typeof(available_at_ms)='integer' AND available_at_ms>0),
 owner_uuid TEXT,
 lease_until_ms INTEGER,
 progress TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(progress) AND json_type(progress)='object' AND length(CAST(progress AS BLOB))<=16384),
 result TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(result) AND json_type(result)='object' AND length(CAST(result AS BLOB))<=16384),
 error_code TEXT NOT NULL DEFAULT '' CHECK(length(error_code)<=128),
 created_at_ms INTEGER NOT NULL CHECK(typeof(created_at_ms)='integer' AND created_at_ms>0),
 updated_at_ms INTEGER NOT NULL CHECK(typeof(updated_at_ms)='integer' AND updated_at_ms>=created_at_ms),
 CHECK(fence<=max_attempts AND (state!='queued' OR fence<max_attempts)),
 CHECK((state='running' AND owner_uuid IS NOT NULL AND length(owner_uuid)=36
   AND typeof(lease_until_ms)='integer' AND lease_until_ms>0 AND fence>0)
   OR (state!='running' AND owner_uuid IS NULL AND lease_until_ms IS NULL))
);
-- Equivalent requests share pending work, but completed requests retain their
-- own immutable acknowledgement below. Different work sharing a destination
-- cannot be running simultaneously, even across different worker processes.
CREATE UNIQUE INDEX archive_jobs_active_work ON archive_jobs(kind,work_key) WHERE state IN ('queued','running');
CREATE UNIQUE INDEX archive_jobs_running_resource ON archive_jobs(resource_key) WHERE state='running';
CREATE INDEX archive_jobs_ready ON archive_jobs(kind,available_at_ms,priority DESC,id) WHERE state='queued';
CREATE INDEX archive_jobs_expired ON archive_jobs(lease_until_ms,id) WHERE state='running';
CREATE INDEX archive_jobs_list ON archive_jobs(kind,state,id);
CREATE TRIGGER archive_job_identity BEFORE UPDATE ON archive_jobs
WHEN NEW.id!=OLD.id OR NEW.uuid!=OLD.uuid OR NEW.kind!=OLD.kind OR NEW.work_key!=OLD.work_key
 OR NEW.resource_key!=OLD.resource_key OR NEW.arguments!=OLD.arguments OR NEW.max_attempts!=OLD.max_attempts
 OR NEW.created_at_ms!=OLD.created_at_ms
BEGIN SELECT RAISE(ABORT,'archive job identity is immutable'); END;
CREATE TRIGGER archive_job_transition BEFORE UPDATE ON archive_jobs
WHEN OLD.state IN ('succeeded','failed','cancelled') OR NEW.revision!=OLD.revision+1
 OR NEW.updated_at_ms<OLD.updated_at_ms
 OR (OLD.state='queued' AND NEW.state NOT IN ('queued','running','failed','cancelled'))
 OR (NEW.state='running' AND OLD.state='queued' AND NEW.fence!=OLD.fence+1)
 OR (NOT (NEW.state='running' AND OLD.state='queued') AND NEW.fence!=OLD.fence)
 OR (OLD.state='running' AND NEW.state='running' AND NEW.owner_uuid!=OLD.owner_uuid)
BEGIN SELECT RAISE(ABORT,'invalid archive job transition'); END;

CREATE TABLE archive_job_submissions (
 request_uuid TEXT NOT NULL PRIMARY KEY CHECK(length(request_uuid)=36 AND request_uuid=lower(request_uuid)
   AND substr(request_uuid,9,1)='-' AND substr(request_uuid,14,1)='-' AND substr(request_uuid,19,1)='-' AND substr(request_uuid,24,1)='-'
   AND length(replace(request_uuid,'-',''))=32 AND replace(request_uuid,'-','') NOT GLOB '*[^0-9a-f]*'
   AND request_uuid!='00000000-0000-0000-0000-000000000000'),
 digest TEXT NOT NULL CHECK(length(digest)=64 AND digest NOT GLOB '*[^0-9a-f]*'),
 job_uuid TEXT NOT NULL REFERENCES archive_jobs(uuid),
 created_at_ms INTEGER NOT NULL CHECK(typeof(created_at_ms)='integer' AND created_at_ms>0)
);
CREATE INDEX archive_job_submissions_job ON archive_job_submissions(job_uuid);
CREATE TRIGGER archive_job_submission_immutable BEFORE UPDATE ON archive_job_submissions
BEGIN SELECT RAISE(ABORT,'archive job submission is immutable'); END;

CREATE TABLE archive_job_attempts (
 job_uuid TEXT NOT NULL REFERENCES archive_jobs(uuid),
 fence INTEGER NOT NULL CHECK(typeof(fence)='integer' AND fence>0),
 owner_uuid TEXT NOT NULL CHECK(length(owner_uuid)=36 AND owner_uuid=lower(owner_uuid)
   AND substr(owner_uuid,9,1)='-' AND substr(owner_uuid,14,1)='-' AND substr(owner_uuid,19,1)='-' AND substr(owner_uuid,24,1)='-'
   AND length(replace(owner_uuid,'-',''))=32 AND replace(owner_uuid,'-','') NOT GLOB '*[^0-9a-f]*'
   AND owner_uuid!='00000000-0000-0000-0000-000000000000'),
 started_at_ms INTEGER NOT NULL CHECK(typeof(started_at_ms)='integer' AND started_at_ms>0),
 ended_at_ms INTEGER,
 outcome TEXT NOT NULL DEFAULT 'running' CHECK(outcome IN ('running','succeeded','retry','failed','cancelled','expired')),
 result TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(result) AND json_type(result)='object' AND length(CAST(result AS BLOB))<=16384),
 error_code TEXT NOT NULL DEFAULT '' CHECK(length(error_code)<=128),
 PRIMARY KEY(job_uuid,fence),
 CHECK((outcome='running' AND ended_at_ms IS NULL) OR (outcome!='running' AND typeof(ended_at_ms)='integer' AND ended_at_ms>=started_at_ms))
);
CREATE TRIGGER archive_job_attempt_valid BEFORE INSERT ON archive_job_attempts
WHEN NOT EXISTS(SELECT 1 FROM archive_jobs WHERE uuid=NEW.job_uuid AND fence=NEW.fence
 AND state='running' AND owner_uuid=NEW.owner_uuid)
BEGIN SELECT RAISE(ABORT,'archive job attempt requires owned running work'); END;
CREATE TRIGGER archive_job_attempt_immutable BEFORE UPDATE ON archive_job_attempts
WHEN OLD.outcome!='running' OR NEW.job_uuid!=OLD.job_uuid OR NEW.fence!=OLD.fence
 OR NEW.owner_uuid!=OLD.owner_uuid OR NEW.started_at_ms!=OLD.started_at_ms OR NEW.outcome='running'
BEGIN SELECT RAISE(ABORT,'archive job attempt is immutable after completion'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000019,'Durable archive jobs and fenced worker leases','{}');
