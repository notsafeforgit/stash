-- Monitoring intent is separate from source provenance and downloader settings.
-- No source is subscribed implicitly, including accounts observed in feeds.
CREATE TABLE source_subscriptions (
  collection_uuid TEXT PRIMARY KEY REFERENCES source_collections(uuid),
  revision INTEGER NOT NULL CHECK(revision>0),
  enabled INTEGER NOT NULL CHECK(enabled IN (0,1))
);
CREATE INDEX source_subscriptions_enabled ON source_subscriptions(enabled,collection_uuid);
CREATE TRIGGER source_subscription_revision BEFORE UPDATE ON source_subscriptions
WHEN NEW.collection_uuid!=OLD.collection_uuid OR NEW.revision!=OLD.revision+1
BEGIN SELECT RAISE(ABORT,'subscription changes require the next revision'); END;

-- Only automatically scheduled runs carry this association. Manual/initial
-- imports remain independent, and disabling monitoring does not revoke intake.
CREATE TABLE source_run_subscriptions (
  run_uuid TEXT PRIMARY KEY REFERENCES source_runs(uuid),
  collection_uuid TEXT NOT NULL REFERENCES source_collections(uuid),
  revision INTEGER NOT NULL CHECK(revision>0)
);
CREATE INDEX source_run_subscriptions_collection ON source_run_subscriptions(collection_uuid,run_uuid);
CREATE TRIGGER source_run_subscription_immutable BEFORE UPDATE ON source_run_subscriptions
BEGIN SELECT RAISE(ABORT,'run subscription bindings are immutable'); END;

INSERT INTO native_migration_history(version,name,details)
 VALUES(1000114,'Source monitoring subscriptions','{}');
