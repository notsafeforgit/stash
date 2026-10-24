-- Recipient URLs use the capability alone; management and sessions retain the
-- stable share ID. Preserve existing keys, frozen membership and guest sessions.
CREATE UNIQUE INDEX shares_token_hash ON shares(token_hash);

INSERT INTO native_migration_history(version, name, details)
VALUES(1000133, 'Indexed recipient share capabilities', '{}');
