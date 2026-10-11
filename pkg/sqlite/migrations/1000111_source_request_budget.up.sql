-- One request budget per service, shared across host and container producers.
-- Only the latest request per live run is retained. Counters expire after 30 days.
CREATE TABLE source_request_budgets (
 scope TEXT PRIMARY KEY NOT NULL REFERENCES source_pacing(scope),
 next_at_ms INTEGER NOT NULL DEFAULT 0 CHECK(next_at_ms>=0),
 maintained_hour INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE source_request_permits (
 run_uuid TEXT PRIMARY KEY NOT NULL REFERENCES source_runs(uuid),
 fence INTEGER NOT NULL CHECK(fence>0),
 request_uuid TEXT NOT NULL CHECK(length(request_uuid)=36),
 scope TEXT NOT NULL REFERENCES source_request_budgets(scope),
 class TEXT NOT NULL CHECK(class IN ('routine','initial','recovery')),
 operation TEXT NOT NULL CHECK(operation IN ('listing','profile','media-resolution','other')),
 requested_at_ms INTEGER NOT NULL,
 touched_at_ms INTEGER NOT NULL,
 granted_at_ms INTEGER,
 reported INTEGER NOT NULL DEFAULT 0 CHECK(reported IN (0,1))
);
CREATE INDEX source_request_waiters ON source_request_permits(scope,class,touched_at_ms) WHERE granted_at_ms IS NULL;
CREATE TABLE source_request_metrics (
 hour INTEGER NOT NULL,
 scope TEXT NOT NULL,
 class TEXT NOT NULL,
 operation TEXT NOT NULL,
 requests INTEGER NOT NULL DEFAULT 0,
 responses INTEGER NOT NULL DEFAULT 0,
 rate_limited INTEGER NOT NULL DEFAULT 0,
 errors INTEGER NOT NULL DEFAULT 0,
 new_posts INTEGER NOT NULL DEFAULT 0,
 repeated_posts INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(hour,scope,class,operation)
);
CREATE TABLE source_request_posts (
 run_uuid TEXT NOT NULL REFERENCES source_runs(uuid),
 post_uuid TEXT NOT NULL REFERENCES source_posts(uuid),
 hour INTEGER NOT NULL,
 PRIMARY KEY(run_uuid,post_uuid)
);
CREATE INDEX source_request_posts_retention ON source_request_posts(hour);
CREATE TABLE source_check_health (
 collection_uuid TEXT PRIMARY KEY NOT NULL REFERENCES source_collections(uuid),
 last_success_ms INTEGER NOT NULL CHECK(last_success_ms>=0)
);
INSERT INTO source_check_health(collection_uuid,last_success_ms)
 SELECT collection_uuid,max(CAST(strftime('%s',json_extract(w.value,'$.until')) AS INTEGER)*1000)
 FROM source_runs r,json_each(r.completed) w WHERE r.operation='download'
 GROUP BY collection_uuid;
INSERT INTO native_migration_history(version,name,details)
 VALUES(1000111,'Shared source request budgets and rolling scrape metrics','{}');
