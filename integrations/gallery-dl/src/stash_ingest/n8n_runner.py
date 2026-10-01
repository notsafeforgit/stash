"""Record and inspect durable native backfills for n8n command nodes."""

import argparse
from contextlib import closing
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import re
import sqlite3
import sys
import uuid

from . import backfills
from .backfill_calls import BackfillCalls, advance_once
from .client import Client, Unavailable
from .configuration import Configuration
from .encoding import InvalidData, digest, encode, identifier
from .outbox import Capacity, Outbox
from .n8n_receipts import LegacyReceipts


def execution_uuid(producer, mode, account, workflow, execution, node, item):
    if (not isinstance(workflow, str) or not re.fullmatch(r"[A-Za-z0-9_-]{1,128}", workflow)
            or not isinstance(execution, str) or not re.fullmatch(r"[0-9]{1,30}", execution)
            or type(item) is not int or not 0 <= item <= 2147483647):
        raise InvalidData("n8n requires a stable workflow, execution, node and item identity")
    identifier(node)
    return str(uuid.uuid5(uuid.UUID(identifier(producer)),
                         encode(["n8n-v1", workflow, execution, node, item, mode, account]).decode()))


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--outbox", default=os.environ.get("STASH_INGEST_OUTBOX"))
    parser.add_argument("--endpoint", default=os.environ.get("STASH_INGEST_ENDPOINT"))
    parser.add_argument("--producer", default=os.environ.get("STASH_INGEST_PRODUCER"))
    parser.add_argument("--token-env", default="STASH_INGEST_TOKEN")
    parser.add_argument("--inspect", help="Retained native or explicitly imported legacy result token")
    parser.add_argument("--mode", choices=backfills.MODES)
    parser.add_argument("--identity")
    parser.add_argument("--call", help="Explicit stable caller UUID instead of n8n execution context")
    parser.add_argument("--workflow")
    parser.add_argument("--execution")
    parser.add_argument("--node")
    parser.add_argument("--item", type=int)
    parser.add_argument("--profile", help="Reviewed full-history profile; otherwise use the service's full-history environment reference")
    parser.add_argument("--until", help="Explicit RFC3339 cutoff; otherwise freeze the first request time")
    parser.add_argument("--strict", action="store_true", help="Exit 2 while pending, 1 on failure, 0 after accepted completion/skip")
    parser.add_argument("--retry-reviewed", action="store_true", help="Recheck this same reviewed caller and its original tickets")
    args = parser.parse_args(argv)
    try:
        if not all((args.outbox, args.endpoint, args.producer)):
            raise InvalidData("Configure the native n8n outbox, endpoint and producer")
        context = (args.workflow, args.execution, args.node, args.item)
        with closing(Outbox(args.outbox, args.endpoint, args.producer)) as box:
            calls = BackfillCalls(box)
            if args.inspect:
                if (not re.fullmatch(r"[0-9a-f]{32}", args.inspect) or any(item is not None for item in context)
                        or any((args.mode, args.identity, args.call, args.profile, args.until))):
                    raise InvalidData("Inspection requires only a retained result token")
                legacy = LegacyReceipts(box).result(args.inspect)
                if legacy is not None:
                    if args.retry_reviewed:
                        raise InvalidData("A historical command receipt cannot be retried as native work")
                    print(json.dumps(legacy, sort_keys=True))
                    return int(legacy["legacy_receipt"]["outcome"] in ("failed", "review")) if args.strict else 0
                call = identifier(str(uuid.UUID(args.inspect)))
                calls.result(call)  # Reject an unknown token before API access.
                if args.retry_reviewed:
                    calls.retry(call)
            else:
                if not args.mode or not args.identity or args.retry_reviewed:
                    raise InvalidData("A new backfill requires a mode, account and stable execution identity")
                platform = "twitter" if args.mode == "twitter" else "reddit"
                # Validate even when replay will avoid opening the profile.
                backfills.subject(str(uuid.NAMESPACE_URL), platform, args.identity)
                if args.call:
                    if any(item is not None for item in context):
                        raise InvalidData("Choose an explicit caller UUID or n8n execution context")
                    call = identifier(args.call)
                else:
                    call = execution_uuid(args.producer, args.mode, args.identity, *context)
                profile_path = args.profile or os.environ.get("STASH_INGEST_" + platform.upper() + "_FULL_HISTORY_PROFILE")
                if not profile_path:
                    raise InvalidData("Configure the reviewed service full-history profile")
                options = {"adapter": "n8n-v1", "platform": platform, "account": args.identity,
                           "component": args.mode, "profile": str(Path(profile_path).absolute()), "until": args.until}

                def prepare():
                    profile = Configuration(options["profile"])
                    profile.check()
                    if profile.source_category != platform or profile._gallery.get("skip") is not True:
                        raise InvalidData("n8n backfills require a reviewed service profile with global skip=true")
                    return {"version": 1, "root_uuid": profile.root_uuid, "platform": platform, "account": args.identity,
                            "component": args.mode, "policy_sha256": profile.policy_sha256,
                            "window": {"since": None, "until": args.until or datetime.fromtimestamp(box.clock(), timezone.utc).isoformat(timespec="milliseconds")},
                            "targets": backfills.targets(platform, args.identity, args.mode)}

                calls.record(call, digest(encode(options, 8192)), prepare)
                if not args.strict:
                    print(json.dumps({"token": uuid.UUID(call).hex, "call_uuid": call, "state": "recorded"}, sort_keys=True))
                    return 0
            advance_once(calls, Client(args.endpoint, args.producer, token_env=args.token_env), call)
            result = calls.result(call)
            print(json.dumps(result, sort_keys=True))
            return result["exit_code"] if args.strict else 0
    except (InvalidData, Unavailable, Capacity) as error:
        code = str(error)
    except (OSError, sqlite3.Error):
        code = "Native n8n inputs or outbox storage are unavailable"
    print(json.dumps({"command_failed": True, "backfill_pending": False, "exit_code": 1,
                      "network_blocked": False, "stdout_tail": "", "stderr_tail": code}, sort_keys=True))
    return 1


if __name__ == "__main__":
    sys.exit(main())
