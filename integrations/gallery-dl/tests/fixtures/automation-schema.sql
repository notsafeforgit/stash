-- Frozen legacy automation schema; data fixtures are synthetic.
PRAGMA application_id=1396920387;
PRAGMA user_version=1;
CREATE TABLE maintenance (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE translation_jobs (
 job_key TEXT PRIMARY KEY, original_text TEXT NOT NULL, target_language TEXT NOT NULL,
 priority INTEGER NOT NULL DEFAULT 100, source_hint TEXT, status TEXT NOT NULL DEFAULT 'pending', result_json TEXT,
 attempts INTEGER NOT NULL DEFAULT 0, next_attempt REAL NOT NULL DEFAULT 0,
 last_error TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE translation_targets (
 job_key TEXT NOT NULL REFERENCES translation_jobs(job_key), catalog_id TEXT NOT NULL,
 post_key TEXT NOT NULL, field TEXT NOT NULL, applied INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(job_key,catalog_id,post_key,field)
);
CREATE TABLE enrichment_jobs (
 catalog_id TEXT NOT NULL, post_key TEXT NOT NULL, version INTEGER NOT NULL,
 platform TEXT NOT NULL, account_key TEXT, url TEXT,
 status TEXT NOT NULL, priority INTEGER NOT NULL DEFAULT 0,
 attempts INTEGER NOT NULL DEFAULT 0, next_attempt REAL NOT NULL DEFAULT 0,
 last_error TEXT, staged_json TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 PRIMARY KEY(catalog_id,post_key,version)
);
CREATE TABLE enrichment_cooldowns (scope TEXT PRIMARY KEY, until_time REAL NOT NULL, reason TEXT NOT NULL);
CREATE TABLE enrichment_seed_progress (catalog_id TEXT PRIMARY KEY, last_post_key TEXT NOT NULL, complete INTEGER NOT NULL, counts_json TEXT NOT NULL);
CREATE TABLE enrichment_source_progress (platform TEXT PRIMARY KEY, last_attempt REAL NOT NULL);
CREATE TABLE discovery_accounts (
 job_key TEXT PRIMARY KEY, platform TEXT NOT NULL, account_key TEXT NOT NULL,
 profile_url TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'pending',
 cursor_json TEXT, staged_json TEXT, pages INTEGER NOT NULL DEFAULT 0,
 attempts INTEGER NOT NULL DEFAULT 0, next_attempt REAL NOT NULL DEFAULT 0,
 last_error TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE TABLE discovery_targets (
 catalog_id TEXT NOT NULL, post_key TEXT NOT NULL, job_key TEXT,
 evidence_json TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'pending',
 PRIMARY KEY(catalog_id,post_key)
);
CREATE TABLE discovery_candidates (
 catalog_id TEXT NOT NULL, post_key TEXT NOT NULL, url TEXT NOT NULL,
 basis TEXT NOT NULL, payload_json TEXT NOT NULL,
 PRIMARY KEY(catalog_id,post_key,url),
 FOREIGN KEY(catalog_id,post_key) REFERENCES discovery_targets(catalog_id,post_key) ON DELETE CASCADE
);
CREATE INDEX translation_due ON translation_jobs(status,next_attempt);
