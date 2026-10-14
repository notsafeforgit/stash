"""One durable, secret-free configuration per profile/policy, shared by its jobs."""

from pathlib import Path

from .configuration import CONFIG_LIMIT, Configuration, profile_bytes, policy_identity
from .encoding import InvalidData, decode, digest, encode
from .outbox import Capacity


def migrate(db):
    db.execute("""CREATE TABLE worker_configurations(
        profile TEXT NOT NULL, root_uuid TEXT NOT NULL, policy_sha256 TEXT NOT NULL, runtime_sha256 TEXT NOT NULL,
        document BLOB NOT NULL, document_sha256 TEXT NOT NULL,
        checked_order INTEGER NOT NULL DEFAULT 0, available_at REAL NOT NULL DEFAULT 0,
        PRIMARY KEY(profile,root_uuid,policy_sha256)
    )""")


class Configurations:
    def __init__(self, box):
        self.box, self.db = box, box.db

    def remember(self, configuration):
        """Must commit before admitting any work using this policy."""
        profile = str(configuration.filename)
        body = profile_bytes(configuration.document)
        with self.box.transaction():
            exists = self.db.execute("SELECT 1 FROM worker_configurations WHERE profile=? AND root_uuid=? AND policy_sha256=?",
                                     (profile, configuration.root_uuid, configuration.policy_sha256)).fetchone()
            if exists is None:
                if self.db.execute("SELECT count(*) FROM worker_configurations").fetchone()[0] >= 1024:
                    raise Capacity("Saved worker configuration capacity exhausted")
                self.db.execute("""INSERT INTO worker_configurations
                    (profile,root_uuid,policy_sha256,runtime_sha256,document,document_sha256) VALUES(?,?,?,?,?,?)""",
                    (profile, configuration.root_uuid, configuration.policy_sha256, digest(encode(configuration.runtime)), body, digest(body)))

    def rows(self, filename, *, ready=False):
        filename = str(Path(filename).resolve())
        return self.db.execute("""SELECT * FROM worker_configurations WHERE profile=? AND runtime_sha256=?
            AND (?=0 OR available_at<=?) ORDER BY checked_order,policy_sha256""",
            (filename, digest(encode(policy_identity())), int(ready), self.box.clock())).fetchall()

    @staticmethod
    def load(row):
        if digest(row["document"]) != row["document_sha256"]:
            raise InvalidData("Saved worker configuration changed")
        result = Configuration.from_document(decode(row["document"], CONFIG_LIMIT), Path(row["profile"]).parent)
        result.filename = Path(row["profile"])
        if result.policy_sha256 != row["policy_sha256"] or result.root_uuid != row["root_uuid"]:
            raise InvalidData("Saved worker policy requires an explicit runtime upgrade")
        return result

    def visited(self, row, *, delay=0):
        with self.box.transaction():
            self.db.execute("""UPDATE worker_configurations SET
                checked_order=(SELECT coalesce(max(checked_order),0)+1 FROM worker_configurations), available_at=?
                WHERE profile=? AND root_uuid=? AND policy_sha256=?""",
                (self.box.clock() + delay, row["profile"], row["root_uuid"], row["policy_sha256"]))


def remember(box, configuration):
    Configurations(box).remember(configuration)
    return configuration
