"""Read a parent execution's status for the existing n8n queue heartbeat."""

import argparse
from contextlib import closing
import json
import os
from pathlib import Path
import re
import sqlite3
import sys


def alive(identity, database):
    if not isinstance(identity, str) or not re.fullmatch(r"[0-9]{1,30}", identity):
        return False
    with closing(sqlite3.connect(Path(database).absolute().as_uri() + "?mode=ro", uri=True, timeout=10)) as db:
        db.execute("PRAGMA query_only=ON")
        db.execute("PRAGMA trusted_schema=OFF")
        row = db.execute("SELECT status,stoppedAt FROM execution_entity WHERE id=?", (identity,)).fetchone()
        return row is not None and row[0] in ("new", "running", "waiting") and row[1] is None


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--execution", required=True)
    parser.add_argument("--database", default=os.environ.get("N8N_DATABASE_PATH", "/home/node/.n8n/database.sqlite"))
    args = parser.parse_args(argv)
    try:
        print(json.dumps({"parent_alive": alive(args.execution, args.database)}, sort_keys=True))
        return 0
    except (OSError, sqlite3.Error):
        print("n8n execution status is unavailable", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
