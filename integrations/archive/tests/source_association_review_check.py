"""Round-trip committed gallery and attachment choices with their receipts."""

from contextlib import closing
import json
from pathlib import Path
import sqlite3
import sys

from stash_archive.bundle import export_archive, import_archive


database, destination = map(Path, sys.argv[1:])
archive = destination / "archive"
restored = destination / "relocated"
tables = {
    "gallery_association_reviews": "request_uuid",
    "attachment_media_reviews": "request_uuid",
    "post_gallery_decisions": "uuid",
    "post_gallery_links": "post_uuid",
    "attachment_media_decisions": "uuid",
    "attachment_media_links": "attachment_uuid",
    "source_attachments": "uuid",
    "source_attachment_manifests": "uuid",
    "source_attachment_entries": "manifest_uuid,position",
}


def rows(path):
    with closing(sqlite3.connect(path.resolve().as_uri() + "?mode=ro", uri=True)) as db:
        return {table: db.execute(f"SELECT * FROM {table} ORDER BY {order}").fetchall()
                for table, order in tables.items()}


before = rows(database)
assert len(before["gallery_association_reviews"]) == 1
assert len(before["attachment_media_reviews"]) == 1
manifest = export_archive(database, archive, reserve=0)
import_archive(archive, restored, reserve=0)
assert rows(restored / "library.sqlite") == before
print(json.dumps({"archive_uuid": manifest["uuid"], "restored": str(restored / "library.sqlite")}))
