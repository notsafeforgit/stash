#!/usr/bin/env python3
"""Audit or download a published native backup. Never activate a restored system."""

import argparse
import json
from pathlib import Path

from native_store import NativeStore
from s3_restore_performer import load_manifest
from stash_archive.storage import InvalidArchive
from stash_archive.verification import verify_archive_proofs, validator_path


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, required=True, help="Published v3 master JSON from the Standard bucket.")
    parser.add_argument("--metadata-bucket", default="metadata-backup-andrew")
    parser.add_argument("--prefix", default="")
    parser.add_argument("--aws-region")
    parser.add_argument("--download-to", type=Path, help="New directory for the portable native archive; otherwise audit checksums only.")
    parser.add_argument("--native-validator", help="Trusted native Stash binary; required with --download-to.")
    parser.add_argument("--producer-origin", help="Original producer API origin; required with --download-to.")
    parser.add_argument("--reserve-bytes", type=int, default=50 << 30)
    parser.add_argument("--validator-timeout", type=int, default=3600)
    args = parser.parse_args(argv)
    catalog = load_manifest(args.manifest)
    if catalog["version"] != 3:
        raise InvalidArchive("This backup predates native archive publication; use its historical restore tools")
    if args.reserve_bytes < 0:
        parser.error("--reserve-bytes must be nonnegative")
    if args.download_to:
        if not args.native_validator or not args.producer_origin:
            parser.error("--download-to requires --native-validator and --producer-origin")
        validator_path(args.native_validator, args.validator_timeout)
        from stash_archive.receipts import origin
        origin(args.producer_origin)
    import boto3
    from botocore.config import Config
    client = boto3.client("s3", region_name=args.aws_region, config=Config(max_pool_connections=20))
    store = NativeStore(client, args.metadata_bucket, args.prefix)
    reference = catalog["native_archive"]
    if args.download_to:
        store.download(reference, args.download_to, reserve=args.reserve_bytes)
        result = verify_archive_proofs(args.download_to, native_validator=args.native_validator,
                                       producer_origin=args.producer_origin, timeout=args.validator_timeout,
                                       temp_parent=args.download_to.parent, reserve=args.reserve_bytes)
    else:
        result = store.audit(reference, reserve=args.reserve_bytes)
    print(json.dumps(result, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
