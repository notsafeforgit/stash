"""Offline commands. Restoring never activates an application or producer."""

import argparse
import base64
from contextlib import closing
import json
from pathlib import Path
import sqlite3

from .bundle import (FORMAT, connect_readonly, export_archive, import_archive,
                     summary, verify_archive)
from .storage import InvalidArchive, RESERVE_BYTES


def quote_identifier(name):
    return '"' + name.replace('"', '""') + '"'


def list_records(restored, table=None, *, limit=20, after=None, columns=None):
    if type(limit) is not int or not 1 <= limit <= 1000:
        raise InvalidArchive("Limit must be between 1 and 1000")
    with closing(connect_readonly(Path(restored) / "library.sqlite")) as db:
        db.setlimit(sqlite3.SQLITE_LIMIT_LENGTH, 8 << 20)
        if db.execute("SELECT lineage FROM native_schema WHERE singleton=1").fetchall() != [(FORMAT,)]:
            raise InvalidArchive("Foreign database lineage")
        tables = [r[0] for r in db.execute("SELECT name FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")]
        if table is None:
            return {"tables": tables}
        if table not in tables:
            raise InvalidArchive("Unknown native table")
        definition = db.execute(f"PRAGMA table_xinfo({quote_identifier(table)})").fetchall()
        names = {r[1] for r in definition}
        key = [r[1] for r in sorted(definition, key=lambda r: r[5]) if r[5]]
        if not key:
            raise InvalidArchive("Offline pagination requires a declared primary key")
        if columns is None:
            selected = key + [n for n in ("uuid", "name", "title", "kind", "post_uuid", "account_uuid", "canonical_uuid") if n in names and n not in key]
        else:
            selected = list(dict.fromkeys(key + columns))
            if any(n not in names for n in selected):
                raise InvalidArchive("Unknown requested column")
        sql = f"SELECT {','.join(map(quote_identifier, selected))} FROM {quote_identifier(table)}"
        parameters = []
        keys = ','.join(map(quote_identifier, key))
        if after is not None:
            if not isinstance(after, list) or len(after) != len(key) or any(type(v) not in (str, int, float) for v in after):
                raise InvalidArchive("Cursor must contain the complete primary key")
            if len(key) == 1:
                sql += f" WHERE {keys}>?"
            else:
                sql += f" WHERE ({keys})>({','.join('?' for _ in key)})"
            parameters.extend(after)
        sql += f" ORDER BY {keys} LIMIT ?"
        parameters.append(limit + 1)
        def value(raw):
            return {"base64": base64.b64encode(raw).decode("ascii")} if isinstance(raw, bytes) else raw
        rows, last_key, used, more = [], None, 0, False
        for row in db.execute(sql, parameters):
            item = dict(zip(selected, map(value, row)))
            size = len(json.dumps(item, ensure_ascii=False).encode("utf-8"))
            if size > 8 << 20:
                raise InvalidArchive("Selected record exceeds the offline list limit; request fewer columns")
            if len(rows) == limit or used + size > 8 << 20:
                more = True
                break
            rows.append(item)
            used += size
            last_key = list(row[:len(key)])
        return {"table": table, "rows": rows, "next": last_key if more else None}


def main(argv=None):
    parser = argparse.ArgumentParser(description="Portable native Stash archives; no server required")
    commands = parser.add_subparsers(dest="command", required=True)
    export = commands.add_parser("export")
    export.add_argument("--database", required=True)
    export.add_argument("--output", required=True)
    export.add_argument("--blobs", action="append", default=[])
    export.add_argument("--components", help="JSON array of explicit role/name/path objects")
    restore = commands.add_parser("import")
    restore.add_argument("archive")
    restore.add_argument("--output", required=True)
    verify = commands.add_parser("verify")
    verify.add_argument("archive")
    verify.add_argument("--temp-parent")
    verify.add_argument("--producer-origin", help="Also verify registered producers' ingestion, source admission and job receipts")
    verify.add_argument("--native-validator", help="Path to the matching native Stash executable; also verify its full schema/provenance report")
    verify.add_argument("--native-validator-timeout", type=float, default=3600,
                        help="Maximum seconds for native validation (default: 3600)")
    inspect = commands.add_parser("inspect")
    inspect.add_argument("archive")
    listing = commands.add_parser("list")
    listing.add_argument("restored")
    listing.add_argument("--table")
    listing.add_argument("--limit", type=int, default=20)
    listing.add_argument("--after", help="JSON primary-key cursor returned by the previous page")
    listing.add_argument("--columns", help="Comma-separated column names; binary values use base64")
    for item in (export, restore, verify):
        item.add_argument("--reserve-bytes", type=int, default=RESERVE_BYTES)
    args = parser.parse_args(argv)
    try:
        if args.command == "export":
            components = json.loads(Path(args.components).read_text()) if args.components else []
            if not isinstance(components, list):
                raise InvalidArchive("Components must be a JSON array")
            export_archive(args.database, args.output, blob_paths=args.blobs,
                           components=components, reserve=args.reserve_bytes)
            result = summary(args.output)
        elif args.command == "import":
            manifest = import_archive(args.archive, args.output, reserve=args.reserve_bytes)
            result = {"uuid": manifest["uuid"], "restored": str(Path(args.output).absolute()),
                      "coverage": manifest["coverage"], "contents_verified": True}
        elif args.command == "verify":
            if args.producer_origin is not None or args.native_validator is not None:
                from .verification import verify_archive_proofs
                result = verify_archive_proofs(args.archive, producer_origin=args.producer_origin,
                                               native_validator=args.native_validator,
                                               timeout=args.native_validator_timeout,
                                               temp_parent=args.temp_parent, reserve=args.reserve_bytes)
            else:
                manifest = verify_archive(args.archive, temp_parent=args.temp_parent, reserve=args.reserve_bytes)
                result = {"uuid": manifest["uuid"], "coverage": manifest["coverage"], "contents_verified": True}
        elif args.command == "inspect":
            result = summary(args.archive)
        else:
            result = list_records(args.restored, args.table, limit=args.limit,
                                  after=json.loads(args.after) if args.after else None,
                                  columns=args.columns.split(",") if args.columns else None)
        print(json.dumps(result, ensure_ascii=False, allow_nan=False))
    except (InvalidArchive, OSError, sqlite3.Error, ValueError) as error:
        parser.exit(1, f"Archive operation failed: {error}\n")


if __name__ == "__main__":
    main()
