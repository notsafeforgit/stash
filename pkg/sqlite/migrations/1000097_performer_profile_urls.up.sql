-- One observed link per source account, not another copy of every profile or
-- post. The capture retains the original metadata and attribution evidence.
CREATE TABLE account_profile_urls (
 account_uuid TEXT NOT NULL REFERENCES source_accounts(uuid) ON UPDATE CASCADE,
 url_key TEXT NOT NULL CHECK(length(url_key) BETWEEN 1 AND 8192),
 url TEXT NOT NULL CHECK(length(url) BETWEEN 1 AND 4096),
 capture_uuid TEXT NOT NULL REFERENCES source_captures(uuid) ON UPDATE CASCADE,
 PRIMARY KEY(account_uuid,url_key)
);
CREATE INDEX account_profile_urls_capture ON account_profile_urls(capture_uuid);

-- Explicit removal is independent of captures/accounts and survives replay,
-- fresh scrapes, new account associations and performer identity merges.
CREATE TABLE performer_profile_url_suppressions (
 performer_uuid TEXT NOT NULL REFERENCES archive_entities(uuid) ON UPDATE CASCADE,
 url_key TEXT NOT NULL CHECK(length(url_key) BETWEEN 1 AND 8192),
 url TEXT NOT NULL CHECK(length(url) BETWEEN 1 AND 4096),
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY(performer_uuid,url_key)
);
CREATE TRIGGER performer_profile_url_suppression_scope BEFORE INSERT ON performer_profile_url_suppressions
BEGIN
 SELECT RAISE(ABORT,'profile URL suppression requires a performer') WHERE NOT EXISTS (
  SELECT 1 FROM archive_entities WHERE uuid=NEW.performer_uuid AND kind='performer');
END;
CREATE TRIGGER performer_profile_url_suppression_update BEFORE UPDATE ON performer_profile_url_suppressions
BEGIN
 SELECT RAISE(ABORT,'profile URL suppressions retain their removal evidence')
 WHERE NEW.url_key!=OLD.url_key OR NEW.url!=OLD.url OR NEW.created_at!=OLD.created_at
  OR NOT EXISTS(SELECT 1 FROM archive_entities WHERE uuid=NEW.performer_uuid AND kind='performer');
END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000097,'Captured performer profile URLs and retained removals','{}');
