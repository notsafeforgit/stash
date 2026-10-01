"""Inspect and drain a producer outbox; never interpret an ACK as media success."""

import argparse
from contextlib import contextmanager, redirect_stdout
import io
import json
import os
import sqlite3
import sys

from .client import Client, Unavailable, drain_once
from .encoding import InvalidData
from .outbox import Capacity, Outbox
from .run_queue import RunQueue, submit_once
from .runs import SourcePaused


@contextmanager
def worker_output():
    """Keep Python and subprocess output out of the command's JSON response."""
    saved = None
    try:
        sys.stdout.flush()
        try:
            descriptor, errors = sys.stdout.fileno(), sys.stderr.fileno()
        except (AttributeError, io.UnsupportedOperation):
            descriptor = None
        if descriptor is not None:
            saved = os.dup(descriptor)
            os.dup2(errors, descriptor)
        with redirect_stdout(sys.stderr):
            yield
    finally:
        if saved is not None:
            sys.stderr.flush()
            os.dup2(saved, descriptor)
            os.close(saved)


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
    request = commands.add_parser("queue-run", help="Durably coalesce a source request without starting a scrape")
    request.add_argument("--collection", required=True)
    request.add_argument("--revision", required=True, type=int)
    policy_source = request.add_mutually_exclusive_group(required=True)
    policy_source.add_argument("--policy", help="Reviewed policy SHA-256; low-level caller option")
    policy_source.add_argument("--profile", help="Native worker JSON; computes the effective configuration digest")
    request.add_argument("--operation", choices=("download", "enrich"), default="download")
    request.add_argument("--cooldown", type=int, default=0)
    request.add_argument("--since", help="Absolute RFC3339 lower bound; omitted means all earlier history")
    request.add_argument("--until", required=True, help="Explicit RFC3339 exclusive upper bound")
    request.add_argument("--ticket", help="Stable caller execution UUID for command-response retries")
    commands.add_parser("submit-runs", help="Submit one ready source request; admission is not completion")
    runs_status = commands.add_parser("runs-status", help="Local source request state and optional paginated history")
    runs_status.add_argument("--intent")
    runs_status.add_argument("--ticket", help="Inspect a caller ticket and its subsequent request history")
    runs_status.add_argument("--after", type=int, default=0)
    retry_run = commands.add_parser("retry-run-request", help="Retry a reviewed source request without changing its UUID")
    retry_run.add_argument("request_uuid")
    policy = commands.add_parser("worker-policy", help="Validate local worker configuration and report its portable digest")
    policy.add_argument("--profile", required=True)
    execute = commands.add_parser("execute-run", help="Execute one claimed source attempt; media intake finishes separately")
    execute.add_argument("run_uuid")
    execute.add_argument("--profile", required=True)
    dispatch = commands.add_parser("dispatch", help="Deliver queued events, submit a request and discover one source attempt")
    dispatch.add_argument("--profile", required=True)
    args = parser.parse_args(argv)
    box = None
    try:
        box = Outbox(args.outbox, args.endpoint, args.producer)
        client = Client(args.endpoint, args.producer, token_env=args.token_env)
        requests = RunQueue(box)
        if args.command == "drain":
            output = {"delivery": drain_once(box, client), "outbox": box.status()}
        elif args.command == "retry":
            box.retry(args.event_uuid)
            output = box.status()
        elif args.command == "receipt-status":
            output = client.receipt_status(args.event_uuid)
        elif args.command == "queue-run":
            policy = args.policy
            if args.profile:
                from .configuration import Configuration
                if args.operation != "download":
                    raise InvalidData("This worker profile currently supports download operations")
                policy = Configuration(args.profile).policy_sha256
            intent = requests.enqueue({"collection_uuid": args.collection, "collection_revision": args.revision,
                "policy_sha256": policy, "operation": args.operation, "cooldown_seconds": args.cooldown,
                "window": {"since": args.since, "until": args.until}}, ticket_uuid=args.ticket)
            output = {"intent_uuid": intent, "ticket_uuid": args.ticket, "state": "recorded", "requests": requests.status()}
        elif args.command == "submit-runs":
            output = {"submission": submit_once(requests, client), "requests": requests.status()}
        elif args.command == "runs-status":
            output = {"requests": requests.status()}
            if args.intent and args.ticket:
                raise InvalidData("Choose either an intent or a caller ticket")
            if args.ticket:
                ticket = requests.ticket(args.ticket)
                if ticket is None:
                    raise InvalidData("Caller ticket was not found")
                output["ticket"] = ticket
                output["history"] = requests.history(ticket["intent_uuid"], after=max(args.after, ticket["first_sequence"] - 1))
            if args.intent:
                output["history"] = requests.history(args.intent, after=args.after)
        elif args.command == "retry-run-request":
            requests.retry(args.request_uuid)
            output = {"requests": requests.status()}
        elif args.command == "worker-policy":
            from .configuration import Configuration
            profile = Configuration(args.profile)
            output = {"policy_sha256": profile.policy_sha256, "root_uuid": profile.root_uuid,
                      "operation": "download", "state": "validated"}
        elif args.command == "execute-run":
            from .configuration import Configuration
            from .worker import execute as execute_source
            with worker_output():
                output = execute_source(box, client, Configuration(args.profile), args.run_uuid)
        elif args.command == "dispatch":
            from .configuration import Configuration
            from .dispatch import dispatch_once
            with worker_output():
                output = dispatch_once(box, client, Configuration(args.profile))
        else:
            output = {**box.status(), "source_requests": requests.status()}
        print(json.dumps(output, sort_keys=True))
        if args.command == "drain":
            counts = output["outbox"]["counts"]
            return 2 if any(counts[k] for k in ("pending", "sending", "review")) else 0
        if args.command == "submit-runs":
            state = output["requests"]
            return 2 if state["pending_windows"] or any(state["counts"][k] for k in ("pending", "sending", "review")) else 0
        if args.command == "execute-run":
            return 0 if output["state"] == "source_succeeded" else 2
        if args.command == "dispatch":
            counts = output["outbox"]["counts"]
            requests = output["source_requests"]
            incomplete = (any(counts[k] for k in ("pending", "sending", "review")) or requests["pending_windows"]
                          or any(requests["counts"][k] for k in ("pending", "sending", "review")))
            return 0 if output["state"] in ("idle", "source_succeeded") and not incomplete else 2
        return 0
    except (InvalidData, Unavailable, Capacity, SourcePaused) as exc:
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
