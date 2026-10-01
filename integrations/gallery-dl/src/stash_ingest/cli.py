"""Inspect and drain a producer outbox; never interpret an ACK as media success."""

import argparse
import json
import sqlite3
import sys

from .client import Client, Unavailable, drain_once
from .encoding import InvalidData
from .outbox import Outbox


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--outbox", required=True)
    parser.add_argument("--endpoint", required=True, help="Stash HTTP origin")
    parser.add_argument("--producer", required=True, help="Stable Stash producer UUID")
    parser.add_argument("--token-env", default="STASH_INGEST_TOKEN")
    commands = parser.add_subparsers(dest="command", required=True)
    commands.add_parser("status", help="Local delivery state; does not claim media completion")
    commands.add_parser("drain", help="Deliver one bounded batch of ready events")
    retry = commands.add_parser("retry", help="Retry a reviewed event with its original bytes")
    retry.add_argument("event_uuid")
    status = commands.add_parser("receipt-status", help="Fetch actual server work status")
    status.add_argument("event_uuid")
    args = parser.parse_args(argv)
    box = None
    try:
        box = Outbox(args.outbox, args.endpoint, args.producer)
        client = Client(args.endpoint, args.producer, token_env=args.token_env)
        if args.command == "drain":
            output = {"delivery": drain_once(box, client), "outbox": box.status()}
        elif args.command == "retry":
            box.retry(args.event_uuid)
            output = box.status()
        elif args.command == "receipt-status":
            output = client.receipt_status(args.event_uuid)
        else:
            output = box.status()
        print(json.dumps(output, sort_keys=True))
        if args.command == "drain":
            counts = output["outbox"]["counts"]
            return 2 if any(counts[k] for k in ("pending", "sending", "review")) else 0
        return 0
    except (InvalidData, Unavailable) as exc:
        print(str(exc), file=sys.stderr)
        return 1
    except (OSError, sqlite3.Error):
        print("Outbox storage is unavailable; delivery was not completed", file=sys.stderr)
        return 1
    finally:
        if box is not None:
            box.close()


if __name__ == "__main__":
    sys.exit(main())
