"""Snapshot a local URL list and relative cutoff once per caller execution."""

from datetime import datetime, timedelta, timezone
from pathlib import Path

from .encoding import InvalidData, digest, encode
from . import windows


def record_file_call(calls, args):
    parameters = {"version": 1, "targets_file": str(Path(args.targets_file).absolute()),
                  "profile": str(Path(args.profile).absolute()) if args.profile else None,
                  "root_uuid": args.root, "policy_sha256": args.policy, "operation": args.operation,
                  "cooldown_seconds": args.cooldown, "since": args.since,
                  "lookback_seconds": args.lookback_seconds, "until": args.until}
    if getattr(args, 'source_mode', None) is not None:
        parameters['source_mode'] = args.source_mode

    def prepare():
        root, policy = args.root, args.policy
        mode = parameters.get('source_mode') or 'published'
        if args.profile:
            from .configuration import Configuration
            profile = Configuration(parameters["profile"])
            if args.operation != "download" or (root is not None and root != profile.root_uuid):
                raise InvalidData("Caller profile must match the requested operation and root")
            root, policy = profile.root_uuid, profile.policy_sha256
            if parameters.get('source_mode') is not None and mode != profile.source_mode:
                raise InvalidData('Caller mode differs from the reviewed worker profile')
            mode = profile.source_mode
        with open(parameters["targets_file"], "rb") as stream:
            raw = stream.read((8 << 20) + 1)
        if len(raw) > 8 << 20:
            raise InvalidData("Source list exceeds 8 MiB")
        try:
            lines = raw.decode("utf-8").splitlines()
        except UnicodeError:
            raise InvalidData("Source list must be UTF-8") from None
        targets = list(dict.fromkeys(line.split()[0] for line in lines if line.strip() and not line.lstrip().startswith("#")))
        until = windows.timestamp(args.until) if args.until else datetime.fromtimestamp(calls.box.clock(), timezone.utc).isoformat(timespec="milliseconds")
        since = args.since
        if args.lookback_seconds is not None:
            if type(args.lookback_seconds) is not int or not 1 <= args.lookback_seconds <= 3155760000:
                raise InvalidData("Lookback must be between one second and 100 years")
            try:
                since = (datetime.fromisoformat(until) - timedelta(seconds=args.lookback_seconds)).isoformat(timespec="milliseconds")
            except (ValueError, OverflowError):
                raise InvalidData("Lookback exceeds the source timestamp range") from None
        if mode == 'traversal' and (since is not None or args.lookback_seconds is not None or args.operation != 'download'):
            raise InvalidData('A traversal scan cannot request a publication-time range or enrichment operation')
        window = windows.normalize({'since': since, 'until': until, **({'basis': 'traversal'} if mode == 'traversal' else {})})
        return {"targets": targets, "root_uuid": root, "policy_sha256": policy, "operation": args.operation,
                "cooldown_seconds": args.cooldown, "window": window}

    return calls.record(args.call, digest(encode(parameters, 16384)), prepare)
