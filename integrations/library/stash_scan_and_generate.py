#!/usr/bin/env python3
"""Queue an ordinary scan of one explicit server path, including ZIP galleries."""

import argparse
import sys

from stash_library import StashClient, StashError, add_connection_arguments


SCAN_QUERY = """mutation ScanPath($input: ScanMetadataInput!) {
  metadataScan(input: $input)
}"""


def run_scan(client, path, dry_run=False):
    if not path.strip():
        raise StashError("An explicit server path is required")
    if dry_run:
        return None
    result = client.call(SCAN_QUERY, {"input": {
        "paths": [path], "scanGenerateCovers": True, "scanGeneratePhashes": True,
    }})
    job_id = result.get("metadataScan")
    if not isinstance(job_id, str) or not job_id:
        raise StashError("Scan admission did not return a job ID; inspect the job queue before retrying")
    return job_id


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("path", help="File or directory path as seen by the Stash server")
    add_connection_arguments(parser)
    args = parser.parse_args()
    job_id = run_scan(StashClient.from_args(args), args.path, args.dry_run)
    if args.dry_run:
        print(f"DRY RUN: scan {args.path!r} with cover and video perceptual-hash generation")
    else:
        print(f"Scan queued for {args.path!r}; job {job_id}. Check its status for completion.")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except StashError as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        raise SystemExit(1)
    except KeyboardInterrupt:
        raise SystemExit(130)
