#!/usr/bin/env python3
"""Keep room for ordinary host activity when planning archive rehearsals."""

import argparse
from pathlib import Path
import shutil


MINIMUM_FREE_BYTES = 50 * 1024**3


def require_headroom(path: Path, additional_bytes: int = 0) -> int:
    """Reject work whose estimated additional peak would consume the reserve."""
    if additional_bytes < 0:
        raise ValueError("Additional peak storage cannot be negative")
    free = shutil.disk_usage(path).free
    required = MINIMUM_FREE_BYTES + additional_bytes
    if free < required:
        raise RuntimeError(
            f"Rehearsal needs {required / 1024**3:.1f} GiB free "
            f"including the 50 GiB host reserve; "
            f"only {free / 1024**3:.1f} GiB is available. "
            "Remove superseded rehearsal copies before continuing."
        )
    return free


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--path", type=Path, default=Path(__file__).resolve().parents[1],
        help="Existing path on the filesystem that will hold rehearsal files",
    )
    parser.add_argument(
        "--additional-bytes", type=int, default=0,
        help="Estimated additional peak bytes, including copies, indexes and WAL",
    )
    args = parser.parse_args()
    try:
        free = require_headroom(args.path, args.additional_bytes)
    except (OSError, RuntimeError, ValueError) as error:
        parser.exit(1, f"{error}\n")
    print(
        f"{free / 1024**3:.1f} GiB available; "
        f"50 GiB reserved; {args.additional_bytes / 1024**3:.1f} GiB planned."
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
