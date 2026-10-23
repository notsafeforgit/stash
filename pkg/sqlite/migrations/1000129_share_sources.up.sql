ALTER TABLE shares ADD COLUMN source BLOB NOT NULL DEFAULT '{}' CHECK (json_valid(source));

CREATE TRIGGER share_source_immutable BEFORE UPDATE OF source ON shares
WHEN NEW.source <> OLD.source
BEGIN SELECT RAISE(ABORT,'share membership rules are immutable; create a new share'); END;

INSERT INTO native_migration_history(version,name,details)
VALUES(1000129,'Immutable snapshot and ongoing share sources','{}');
