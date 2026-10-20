-- Filesystem intents are flushed before moving bytes. A marker committed with
-- the file/folder edits determines whether recovery finishes or rolls back.
-- Successful recovery deletes both the intent and this marker; no history grows.
CREATE TABLE file_moves (
  id TEXT PRIMARY KEY NOT NULL CHECK(length(id)=36)
);

INSERT INTO native_migration_history(version,name,details)
VALUES(1000124,'Recover interrupted library file moves','{}');
