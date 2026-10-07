"""SQLite discovery cursors, observed versions and exact pending intake bodies."""

from contextlib import closing, contextmanager
import os
from pathlib import Path
import sqlite3
import stat
import tempfile
import uuid

from .catalog_snapshot import sync_directory
from .encoding import InvalidData, decode, encode
from .intake_client import LIMIT, TERMINAL, file_input, fingerprint, includes, preview as validate_preview, saved_request, status

JOURNAL_NAME = "intake.sqlite3"
APPLICATION_ID = 0x5344494E


@contextmanager
def database(directory, *, create=False):
    path = Path(directory) / JOURNAL_NAME
    if create and not path.exists() and not path.is_symlink():
        fd, temporary = tempfile.mkstemp(prefix=".intake-create-", dir=directory)
        os.close(fd)
        try:
            with closing(sqlite3.connect(temporary)) as db:
                db.executescript(f"""
                    PRAGMA synchronous=FULL;
                    BEGIN IMMEDIATE;
                    PRAGMA application_id={APPLICATION_ID};
                    PRAGMA user_version=1;
                    CREATE TABLE configuration(id INTEGER PRIMARY KEY CHECK(id=1), body BLOB NOT NULL,
                        completed_at INTEGER NOT NULL DEFAULT 0);
                    CREATE TABLE directories(id INTEGER PRIMARY KEY, collection TEXT NOT NULL, path TEXT NOT NULL,
                        after_key TEXT NOT NULL DEFAULT '', signature TEXT NOT NULL DEFAULT '',
                        page BLOB, position INTEGER NOT NULL DEFAULT 0, done INTEGER NOT NULL DEFAULT 0,
                        UNIQUE(collection,path));
                    CREATE TABLE observed(collection TEXT NOT NULL, path TEXT NOT NULL, version BLOB NOT NULL,
                        disposition TEXT NOT NULL, PRIMARY KEY(collection,path));
                    CREATE TABLE requests(uuid TEXT PRIMARY KEY, collection TEXT NOT NULL, path TEXT NOT NULL,
                        preview BLOB NOT NULL, body BLOB NOT NULL, result BLOB,
                        terminal INTEGER NOT NULL DEFAULT 0);
                    CREATE UNIQUE INDEX active_file ON requests(collection,path) WHERE terminal=0;
                    COMMIT;
                """)
            os.link(temporary, path)
            sync_directory(Path(directory))
        finally:
            os.unlink(temporary)
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or info.st_mode & 0o077:
        raise InvalidData("Intake database must be private and owned by this user")
    with closing(sqlite3.connect(path.as_uri() + "?mode=rw", uri=True, timeout=5)) as db:
        db.row_factory = sqlite3.Row
        db.execute("PRAGMA trusted_schema=OFF")
        db.execute("PRAGMA synchronous=FULL")
        if (db.execute("PRAGMA application_id").fetchone()[0] != APPLICATION_ID
                or db.execute("PRAGMA user_version").fetchone()[0] != 1):
            raise InvalidData("Unsupported intake journal")
        with db:
            db.execute("BEGIN IMMEDIATE")
            yield db


class Journal:
    def __init__(self, directory, config):
        self.directory = Path(directory)
        self.config = config
        binding = {key: config[key] for key in ("endpoint", "root_uuid", "collections", "exclude", "library_lock")}
        with database(directory, create=True) as db:
            row = db.execute("SELECT body FROM configuration WHERE id=1").fetchone()
            if row is None:
                db.execute("INSERT INTO configuration(id,body) VALUES(1,?)", (encode(binding),))
            elif bytes(row[0]) != encode(binding):
                prior = decode(row[0], 4 << 20)
                if (any(prior.get(key) != binding[key] for key in ("endpoint", "root_uuid", "library_lock"))
                        or db.execute("SELECT 1 FROM requests WHERE terminal=0 LIMIT 1").fetchone()):
                    raise InvalidData("Intake endpoint/scope changed; recover the original journal before changing scope")
                db.execute("UPDATE configuration SET body=?,completed_at=0 WHERE id=1", (encode(binding),))
                db.execute("DELETE FROM directories")

    def pending(self):
        with database(self.directory) as db:
            rows = db.execute("SELECT body,preview,result FROM requests WHERE terminal=0 ORDER BY rowid LIMIT 101").fetchall()
        if len(rows) > 100:
            raise InvalidData("Intake journal exceeds its pending request limit")
        ret = []
        for row in rows:
            body = saved_request(decode(row["body"], 8192))
            self.validate_preview(decode(row["preview"], LIMIT), body)
            if row["result"] is not None:
                status(decode(row["result"], LIMIT), body)
            ret.append(body)
        return ret

    def prepare(self, preview):
        self.validate_preview(preview, preview)
        request = {**file_input(preview), "signature": preview["signature"]}
        request["request_uuid"] = str(uuid.uuid4())
        saved_request(request)
        with database(self.directory) as db:
            db.execute("INSERT INTO requests(uuid,collection,path,preview,body) VALUES(?,?,?,?,?)",
                       (request["request_uuid"], request["collection_uuid"], request["relative_path"],
                        encode(preview, LIMIT), encode(request, 8192)))
        return request

    def validate_preview(self, preview, request):
        validate_preview(preview, request, self.config["root_uuid"])
        # The configured base bounds discovery. A server-selected child policy
        # is authorized by the saved scan context and checked again at admission
        # and publication; it need not be copied into a host configuration.
        base = request.get("scan_collection_uuid", request["collection_uuid"])
        collection = next((item for item in self.config["collections"] if item["uuid"] == base), None)
        if (collection is None or not includes(collection["path_prefix"], request["relative_path"])
                or any(includes(prefix, request["relative_path"]) for prefix in self.config["exclude"])
                or preview["signature"] != request["signature"]):
            raise InvalidData("Saved intake request differs from its configured scope or preview")

    def record(self, request, result, *, rejection=False):
        terminal = rejection or status(result, request)["state"] in TERMINAL
        with database(self.directory) as db:
            row = db.execute("SELECT body,preview,result,terminal FROM requests WHERE uuid=?", (request["request_uuid"],)).fetchone()
            if row is None or bytes(row["body"]) != encode(request, 8192):
                raise InvalidData("Intake request is missing or changed")
            if row["terminal"]:
                if bytes(row["result"]) != encode(result, LIMIT):
                    raise InvalidData("Completed intake outcome changed")
                return
            db.execute("UPDATE requests SET result=?,terminal=? WHERE uuid=?",
                       (encode(result, LIMIT), int(terminal), request["request_uuid"]))
            if terminal:
                original = decode(row["preview"], LIMIT)
                db.execute("INSERT OR REPLACE INTO observed VALUES(?,?,?,?)",
                           (request["collection_uuid"], request["relative_path"], fingerprint(original),
                            "review" if rejection else result["state"]))

    def seen(self, preview):
        with database(self.directory) as db:
            active = db.execute("SELECT 1 FROM requests WHERE path=? AND terminal=0",
                                (preview["relative_path"],)).fetchone()
            row = db.execute("SELECT version FROM observed WHERE collection=? AND path=?",
                             (preview["collection_uuid"], preview["relative_path"])).fetchone()
            if active is not None or (row is not None and bytes(row[0]) == fingerprint(preview)):
                return True
            if row is None and preview.get("existing_file_uuid"):
                # Initial discovery adopts indexed files as its baseline. It
                # does not claim their remaining effects succeeded. Future
                # file/policy version changes are admitted normally.
                db.execute("INSERT INTO observed VALUES(?,?,?,?)",
                           (preview["collection_uuid"], preview["relative_path"], fingerprint(preview), "already_indexed"))
                return True
        return False

    def begin_cycle(self, now):
        with database(self.directory) as db:
            if db.execute("SELECT 1 FROM directories WHERE done=0 LIMIT 1").fetchone():
                return True
            completed = db.execute("SELECT completed_at FROM configuration WHERE id=1").fetchone()[0]
            if completed and 0 <= now - completed < self.config["scan_interval_seconds"]:
                return False
            db.execute("DELETE FROM directories")
            for item in self.config["collections"]:
                db.execute("INSERT INTO directories(collection,path) VALUES(?,?)", (item["uuid"], item["path_prefix"]))
        return True

    def next_directory(self):
        with database(self.directory) as db:
            row = db.execute("SELECT * FROM directories WHERE done=0 ORDER BY id LIMIT 1").fetchone()
            return dict(row) if row else None

    def save_page(self, row, page):
        with database(self.directory) as db:
            db.execute("UPDATE directories SET page=?,position=0 WHERE id=?", (encode(page, LIMIT), row["id"]))

    def advance(self, row, page, child=None):
        with database(self.directory) as db:
            if child:
                db.execute("INSERT OR IGNORE INTO directories(collection,path) VALUES(?,?)", (row["collection"], child))
            position = row["position"] + 1
            if position >= len(page["entries"]):
                after = page.get("next_after", "")
                db.execute("UPDATE directories SET page=NULL,position=0,after_key=?,signature=?,done=? WHERE id=?",
                           (after, page["signature"] if after else "", int(not after), row["id"]))
            else:
                db.execute("UPDATE directories SET position=? WHERE id=?", (position, row["id"]))

    def reset_directory(self, row):
        with database(self.directory) as db:
            db.execute("UPDATE directories SET page=NULL,position=0,after_key='',signature='' WHERE id=?", (row["id"],))

    def skip_directory(self, row):
        with database(self.directory) as db:
            db.execute("UPDATE directories SET page=NULL,position=0,done=1 WHERE id=?", (row["id"],))

    def complete_cycle(self, now):
        with database(self.directory) as db:
            if db.execute("SELECT 1 FROM directories WHERE done=0 LIMIT 1").fetchone():
                return False
            db.execute("UPDATE configuration SET completed_at=? WHERE id=1", (now,))
        return True

    def summary(self):
        with database(self.directory) as db:
            counts = dict(db.execute("SELECT disposition,count(*) FROM observed GROUP BY disposition").fetchall())
            pending = db.execute("SELECT count(*) FROM requests WHERE terminal=0").fetchone()[0]
            folders = db.execute("SELECT count(*) FROM directories WHERE done=0").fetchone()[0]
        return {"pending": pending, "remaining_directories": folders, "observed": counts}
