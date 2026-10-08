"""Native replacements for the host account/list scrape launchers."""

import argparse
from contextlib import closing
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import sqlite3
import sys
import time
import uuid

from .client import Client, Unavailable
from .completion import inspect_call
from .configuration import Configuration
from .encoding import InvalidData, digest, encode, identifier
from .launcher_inputs import date_min, instagram_list, instagram_target, mirror_list, mirror_target, reddit_list, reddit_name, reddit_urls, social_list, social_target, twitter_list, twitter_target
from .outbox import Capacity, Outbox
from .source_calls import SourceCalls
from . import windows

URL_SERVICES = ('coomer', 'kemono', 'bluesky', 'tiktok')


def parser_for(service):
    parser = argparse.ArgumentParser(description=f"Record native {service} scrape requests; source execution runs through the dispatcher.")
    targets = parser.add_mutually_exclusive_group()
    targets.add_argument('--url' if service in URL_SERVICES else '--username', dest='username')
    if service == "twitter":
        targets.add_argument("--user-id", "--gid", dest="user_id")
    elif service == "reddit":
        targets.add_argument("--subreddit")
        parser.add_argument("--mode", choices=("new", "top"), default="new")
        parser.add_argument("--saved", action="store_true")
        parser.add_argument("--date-min")
        parser.add_argument("--date-min-relative")
        parser.add_argument("--date-min-days", type=int)
    parser.add_argument("--config-file", default=os.environ.get(service.upper() + "_LIST_CONFIG",
                        str(Path.home() / ".config/gallery-dl" / (service + "-list.conf"))))
    parser.add_argument("--full-history", action="store_true", help="Use the reviewed global skip=true profile")
    parser.add_argument("--strict-errors", action="store_true", help="Exit 0 only after this caller's source work is confirmed complete")
    parser.add_argument("--dry-run", action="store_true", help="Preview expanded URLs and date window without creating an outbox")
    parser.add_argument("--log-level", choices=("DEBUG", "INFO", "WARNING", "ERROR"), default=os.environ.get("LOG_LEVEL", "INFO"))
    parser.add_argument("--outbox", default=os.environ.get("STASH_INGEST_OUTBOX"))
    parser.add_argument("--endpoint", default=os.environ.get("STASH_INGEST_ENDPOINT"))
    parser.add_argument("--producer", default=os.environ.get("STASH_INGEST_PRODUCER"))
    parser.add_argument("--profile", help="Reviewed worker profile; otherwise use the service's profile environment reference")
    parser.add_argument("--token-env", default="STASH_INGEST_TOKEN")
    parser.add_argument("--call", help="Stable caller UUID; defaults to this systemd invocation or a new interactive UUID")
    parser.add_argument("--until", help="Explicit RFC3339 cutoff; otherwise freeze the first request time")
    return parser


def options(service, args):
    full_history = args.full_history or (service == "reddit" and args.mode == "top")
    profile_env = "STASH_INGEST_" + service.upper() + ("_FULL_HISTORY_PROFILE" if full_history else "_PROFILE")
    profile = args.profile or os.environ.get(profile_env)
    if service == "twitter":
        direct = twitter_target(args.username, "user") if args.username else twitter_target(args.user_id, "id") if args.user_id else None
        source = {"url": direct} if direct else {"list_file": str(Path(args.config_file).absolute())}
    elif service == "instagram":
        source = {"url": instagram_target(args.username)} if args.username else {"list_file": str(Path(args.config_file).absolute())}
    elif service in URL_SERVICES:
        target = mirror_target if service in ('coomer', 'kemono') else social_target
        source = {'url': target(args.username, service)} if args.username else {'list_file': str(Path(args.config_file).absolute())}
    else:
        source = ({"user": reddit_name(args.username)} if args.username else
                  {"subreddit": reddit_name(args.subreddit, "subreddit")} if args.subreddit else
                  {"list_file": str(Path(args.config_file).absolute())})
    result = {"launcher": service + "-v1", "source": source, "full_history": bool(full_history),
              "profile": str(Path(profile).absolute()) if profile else None, "until": args.until}
    if service == "reddit":
        result.update(mode=args.mode, saved=args.saved, date_min=args.date_min,
                      date_min_relative=args.date_min_relative, date_min_days=args.date_min_days)
    return result


def expand(service, value, now):
    source, ignored = value["source"], []
    if service in ('twitter', 'instagram', *URL_SERVICES):
        if "url" in source:
            urls = [source["url"]]
        elif service in ('coomer', 'kemono'):
            urls, ignored = mirror_list(source['list_file'], service)
        elif service in ('bluesky', 'tiktok'):
            urls, ignored = social_list(source['list_file'], service)
        else:
            urls, ignored = (twitter_list if service == "twitter" else instagram_list)(source["list_file"])
        since = None
    else:
        if "user" in source:
            users, subs = [source["user"]], []
        elif "subreddit" in source:
            users, subs = [], [source["subreddit"]]
        else:
            users, subs, ignored = reddit_list(source["list_file"])
        urls = reddit_urls(users, subs, value["mode"], value["saved"])
        since = date_min(value["date_min"], value["date_min_relative"], value["date_min_days"], now)
    until = value["until"] or datetime.fromtimestamp(now, timezone.utc).isoformat(timespec="milliseconds")
    return urls, windows.normalize({"since": since, "until": until}), ignored


def caller_uuid(service, producer, supplied, mode=None):
    if supplied:
        return identifier(supplied)
    invocation = os.environ.get("INVOCATION_ID")
    if invocation:
        try:
            invocation = str(uuid.UUID(invocation))
        except ValueError:
            raise InvalidData("Invalid systemd invocation identity") from None
        return str(uuid.uuid5(uuid.UUID(identifier(producer)), f"systemd/{invocation}/{service}/{mode or 'default'}"))
    return str(uuid.uuid4())


def main(service, argv=None):
    args = parser_for(service).parse_args(argv)
    try:
        value = options(service, args)
        if args.dry_run:
            targets, window, ignored = expand(service, value, time.time())
            print(json.dumps({"state": "preview", "targets": targets, "window": window,
                              "full_history": value["full_history"], "ignored_lines": ignored}, sort_keys=True))
            return 0
        if not all((args.outbox, args.endpoint, args.producer, value["profile"])):
            raise InvalidData("Configure the native outbox, endpoint, producer and matching service profile")
        call = caller_uuid(service, args.producer, args.call, value.get("mode"))
        with closing(Outbox(args.outbox, args.endpoint, args.producer)) as box:
            calls = SourceCalls(box)

            def prepare():
                profile = Configuration(value["profile"])
                profile.check()
                if profile.source_category != service:
                    raise InvalidData("Launcher requires a reviewed profile for its source category")
                if profile.source_mode != 'published':
                    raise InvalidData('Date-based launchers require a publication-window profile')
                if value["full_history"] and profile._gallery.get("skip") is not True:
                    raise InvalidData("Full-history and top scans require a reviewed profile with global skip=true")
                targets, window, ignored = expand(service, value, box.clock())
                if ignored:
                    print("Ignored saved-list line numbers: " + ",".join(map(str, ignored[:100]))
                          + (" (additional lines omitted)" if len(ignored) > 100 else ""), file=sys.stderr)
                if not targets:
                    raise InvalidData("Saved source list contains no usable targets")
                return {"targets": targets, "root_uuid": profile.root_uuid, "operation": "download",
                        "policy_sha256": profile.policy_sha256, "cooldown_seconds": 0, "window": window}

            recorded = calls.record(call, digest(encode(value, 16384)), prepare)
            result = {"call_uuid": call, "state": "recorded", "call": recorded, "intake_completion": "inspect_native_receipts"}
            if args.strict_errors:
                result["completion"] = inspect_call(calls, Client(args.endpoint, args.producer, token_env=args.token_env), call)
                result["state"] = result["completion"]["state"]
            print(json.dumps(result, sort_keys=True))
            return 0 if not args.strict_errors or result["state"] == "source_succeeded" else 2
    except (InvalidData, Unavailable, Capacity) as error:
        print(str(error), file=sys.stderr)
    except (OSError, sqlite3.Error):
        print("Native launcher inputs or outbox storage are unavailable; source completion was not confirmed", file=sys.stderr)
    return 1


def twitter_main(argv=None):
    return main("twitter", argv)


def reddit_main(argv=None):
    return main("reddit", argv)


def instagram_main(argv=None):
    return main("instagram", argv)


def coomer_main(argv=None):
    return main('coomer', argv)


def kemono_main(argv=None):
    return main('kemono', argv)


def bluesky_main(argv=None):
    return main('bluesky', argv)


def tiktok_main(argv=None):
    return main('tiktok', argv)
