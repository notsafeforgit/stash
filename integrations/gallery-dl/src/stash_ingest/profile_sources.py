"""Expand a subscribed Reddit profile into internal, independently tracked passes."""

import re


PROFILE = re.compile(r"https://(?:www\.|old\.)?reddit\.com/(?:user|u)/([A-Za-z0-9_-]{1,32})/?\Z")


def expand_profiles(targets):
    from .backfills import targets as backfill_targets
    expanded = []
    for target in targets:
        match = PROFILE.fullmatch(target)
        if match:
            account = match[1].lower()
            expanded.extend(backfill_targets("reddit", account, "reddit-new"))
            expanded.extend(backfill_targets("reddit", account, "reddit-top"))
        else:
            expanded.append(target)
    return list(dict.fromkeys(expanded))
