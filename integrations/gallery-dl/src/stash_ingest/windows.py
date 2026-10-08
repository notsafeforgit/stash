"""Publication windows and configured scan requests, with shared Go fixtures."""

from datetime import datetime, timezone
import re

from .encoding import InvalidData

MAX_WINDOWS = 64
_TIMESTAMP = re.compile(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.(\d{1,9}))?(?:Z|[+-]\d{2}:\d{2})\Z")


def timestamp(value):
    match = _TIMESTAMP.fullmatch(value) if isinstance(value, str) else None
    if match is None or any(c != "0" for c in (match.group(1) or "")[3:]):
        raise InvalidData("Source windows require RFC3339 timestamps with millisecond precision")
    try:
        parsed = datetime.fromisoformat(value).astimezone(timezone.utc)
        return parsed.isoformat(timespec="milliseconds").replace("+00:00", "Z")
    except (ValueError, OverflowError):
        raise InvalidData("Invalid source window timestamp") from None


def normalize(window):
    if not isinstance(window, dict) or set(window) not in ({"since", "until"}, {"since", "until", "basis"}):
        raise InvalidData("A source window requires since and until")
    basis = window.get('basis', '')
    if basis not in ('', 'traversal') or (basis == 'traversal' and window['since'] is not None):
        raise InvalidData('A traversal request requires no publication-date lower bound')
    since = timestamp(window["since"]) if window["since"] is not None else None
    until = timestamp(window["until"])
    if until == "0001-01-01T00:00:00.000Z" or (since is not None and since >= until):
        raise InvalidData("A source window must end after its start")
    return {"since": since, "until": until, **({'basis': basis} if basis else {})}


def union(*groups):
    values = sorted((normalize(w) for group in groups for w in group),
                    key=lambda w: (w.get('basis', ''), w["since"] is not None, w["since"] or "", w["until"]))
    result = []
    for window in values:
        if (not result or window.get('basis', '') != result[-1].get('basis', '')
                or (window["since"] is not None and window["since"] > result[-1]["until"])):
            result.append(window)
        elif window["until"] > result[-1]["until"]:
            result[-1]["until"] = window["until"]
    return result


def subtract(wanted, covered):
    result = union(wanted)
    for cover in union(covered):
        remaining = []
        for window in result:
            if window.get('basis', '') != cover.get('basis', ''):
                remaining.append(window)
                continue
            if window.get('basis') == 'traversal':
                if window['until'] > cover['until']:
                    remaining.append(window)
                continue
            if ((cover["since"] is not None and window["until"] <= cover["since"])
                    or (window["since"] is not None and window["since"] >= cover["until"])):
                remaining.append(window)
                continue
            if cover["since"] is not None and (window["since"] is None or window["since"] < cover["since"]):
                remaining.append({"since": window["since"], "until": cover["since"]})
            if window["until"] > cover["until"]:
                remaining.append({"since": cover["until"], "until": window["until"]})
        result = remaining
    return result
