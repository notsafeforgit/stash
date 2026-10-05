#!/usr/bin/env python3
"""Verify a restore using local objects and a reference tree; never request thawing."""
import argparse
from pathlib import Path
from s3_restore_performer import load_manifest, select_plan, show_plan, restore_local, verify_tree


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("search_term")
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--ledger-db", type=Path)
    parser.add_argument("--footprints", type=Path)
    parser.add_argument("--objects-dir", type=Path)
    parser.add_argument("--destination", type=Path)
    parser.add_argument("--expected-dir", type=Path)
    parser.add_argument("--include-videos", action="store_true")
    args = parser.parse_args(argv)
    if args.objects_dir and not (args.destination and args.expected_dir):
        parser.error("Verification requires --objects-dir, --destination, and --expected-dir")
    if not args.objects_dir and (args.destination or args.expected_dir):
        parser.error("--destination and --expected-dir require --objects-dir")
    manifest = load_manifest(args.manifest, args.ledger_db, args.footprints)
    plan = select_plan(manifest, args.search_term, args.include_videos)
    show_plan(plan)
    if args.objects_dir:
        if not args.expected_dir.is_dir():
            parser.error("--expected-dir must be an existing reference directory")
        restore_local(plan, args.objects_dir, args.destination)
        count = verify_tree(args.destination, args.expected_dir)
        print(f"Verified {count} restored files by SHA-256, including absence of deleted files.")
    else:
        print("Local plan only. Supply local objects and a reference directory to verify reconstruction.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
