-- Existing share hashes cannot be reversed. Preserve their links and sessions;
-- their owners can explicitly regenerate if they need to retrieve the URL.
ALTER TABLE shares ADD COLUMN token TEXT NOT NULL DEFAULT ''
 CHECK(length(token) IN (0,43));

-- A recipient preview now opens the ordinary saved link. Keep already-open
-- sessions with their original expiry, but discard unused one-use bootstraps.
DELETE FROM share_sessions WHERE exchange_only=1;
ALTER TABLE share_sessions DROP COLUMN preview;
ALTER TABLE share_sessions DROP COLUMN exchange_only;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000122,'Retain share links for copying and recipient previews','{}');
