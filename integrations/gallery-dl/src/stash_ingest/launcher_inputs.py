"""Account/list expansion used by the installed native source launchers."""

from datetime import datetime, timedelta, timezone
from pathlib import Path
import re
from urllib.parse import urlsplit

from .encoding import InvalidData
from . import windows


_TWITTER_ID = re.compile(r"^(?:https?://)?(?:www\.)?(?:x\.com|twitter\.com)/i/user/(?P<id>\d{1,30})(?:[/?#].*)?$", re.I)
_TWITTER_USER = re.compile(r"^(?:https?://)?(?:www\.)?(?:x\.com|twitter\.com)/(?P<user>[A-Za-z0-9_]{1,30})(?:[/?#].*)?$", re.I)
_REDDIT_USER = re.compile(r"^(?:https?://)?(?:www\.)?reddit\.com/(?:user|u)/([^/?#\s]+)(?:[/?#].*)?$", re.I)
_REDDIT_SUB = re.compile(r"^(?:https?://)?(?:www\.)?reddit\.com/r/([^/?#\s]+)(?:/?(?:[?#].*)?|/(?:new|top|hot)(?:[/?#].*)?)?$", re.I)
_RELATIVE = re.compile(r"(?P<n>\d+)\s*(?P<unit>minute|minutes|min|mins|hour|hours|hr|hrs|day|days|week|weeks)\s*ago", re.I)


def twitter_target(value, kind="auto"):
    value = value.strip()
    if kind in ("auto", "id"):
        match = _TWITTER_ID.fullmatch(value)
        if match or re.fullmatch(r"[0-9]{1,30}", value):
            return "https://x.com/i/user/" + (match["id"] if match else value)
        if kind == "id":
            raise InvalidData("Invalid Twitter numeric account ID")
    if value.startswith("@"):
        value = value[1:].strip()
    if _TWITTER_ID.fullmatch(value):
        raise InvalidData("Use the account-ID option for a Twitter ID URL")
    match = _TWITTER_USER.fullmatch(value)
    if match:
        value = match["user"]
    if not re.fullmatch(r"[A-Za-z0-9_]{1,30}", value):
        raise InvalidData("Invalid Twitter account name or profile URL")
    return "https://x.com/" + value


def reddit_name(value, kind="user"):
    value = value.strip()
    match = (_REDDIT_USER if kind == "user" else _REDDIT_SUB).fullmatch(value)
    if match:
        value = match[1]
    value = re.sub(r"^(?:user|u)/" if kind == "user" else r"^r/", "", value, flags=re.I).strip("/")
    if not re.fullmatch(r"[A-Za-z0-9_-]{1,32}", value) or (kind == "user" and value.lower() == "me"):
        raise InvalidData("Invalid Reddit account or community name")
    return value


def read_list(filename):
    with Path(filename).open("rb") as stream:
        value = stream.read((8 << 20) + 1)
    if len(value) > 8 << 20:
        raise InvalidData("Saved source list exceeds 8 MiB")
    try:
        return value.decode("utf-8").splitlines()
    except UnicodeError:
        raise InvalidData("Saved source list must be UTF-8") from None


def instagram_target(value):
    value = value.strip()
    if re.fullmatch(r"@?[A-Za-z0-9_.]{1,30}", value):
        value = "https://www.instagram.com/" + value.removeprefix("@") + "/"
    try:
        parsed = urlsplit(value)
        if (parsed.scheme not in ("https", "http") or parsed.hostname not in ("instagram.com", "www.instagram.com")
                or parsed.username is not None or parsed.password is not None or parsed.port is not None
                or any(part in (".", "..") for part in parsed.path.split("/"))
                or any(c.isspace() or ord(c) < 32 for c in value)):
            raise ValueError()
        # Preserve an explicit extractor URL and its query; do not convert a
        # post/highlight target into an unrelated account name.
        from gallery_dl import extractor
        target = extractor.find(value)
        if target is None or target.category != "instagram":
            raise ValueError()
        return value
    except ValueError:
        raise InvalidData("Invalid Instagram account name or extractor URL") from None


def instagram_list(filename):
    return _explicit_list(filename, instagram_target, 'Instagram')


def mirror_target(value, category):
    value = value.strip()
    try:
        parsed = urlsplit(value)
        if (category not in ('coomer', 'kemono') or parsed.scheme not in ('http', 'https')
                or not re.fullmatch(r'(?:www\.|beta\.)?' + category + r'\.(?:cr|st|su|party)', parsed.hostname or '')
                or parsed.username is not None or parsed.password is not None or parsed.port is not None
                or any(c.isspace() or ord(c) < 32 for c in value)):
            raise ValueError()
        from gallery_dl import extractor
        from gallery_dl.extractor.kemono import KemonoUserExtractor, KemonoPostExtractor, KemonoPostsExtractor
        target = extractor.find(value)
        if target is None or target.category != category or not isinstance(target, (KemonoUserExtractor, KemonoPostExtractor, KemonoPostsExtractor)):
            raise ValueError()
        return value
    except ValueError:
        raise InvalidData('Choose a supported ' + category + ' account, post or listing URL') from None


def mirror_list(filename, category):
    return _explicit_list(filename, lambda value: mirror_target(value, category), category)


def social_target(value, category):
    from gallery_dl import extractor
    from .social_media import supported

    value = value.strip()
    try:
        parsed = urlsplit(value)
        if (category not in ('bluesky', 'tiktok') or parsed.scheme not in ('http', 'https')
                or parsed.username is not None or parsed.password is not None or parsed.port is not None
                or any(part in ('.', '..') for part in parsed.path.split('/'))
                or any(c.isspace() or ord(c) < 32 for c in value)):
            raise ValueError()
        target = extractor.find(value)
        if target is None or target.category != category or not supported(target):
            raise ValueError()
        return value
    except ValueError:
        raise InvalidData('Choose a supported ' + category + ' post or collection URL') from None


def social_list(filename, category):
    return _explicit_list(filename, lambda value: social_target(value, category), category)


def _explicit_list(filename, target, label):
    targets = []
    for number, line in enumerate(read_list(filename), 1):
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        try:
            targets.append(target(line))
        except InvalidData:
            # gallery-dl input lists are explicit URLs, not a best-effort list
            # of names. Do not silently turn a partial list into success.
            raise InvalidData(f"Invalid {label} saved-list target on line {number}") from None
    return list(dict.fromkeys(targets)), []


def twitter_list(filename):
    urls, ignored = [], []
    for number, line in enumerate(read_list(filename), 1):
        line = line.split("#", 1)[0].strip()
        if line:
            try:
                urls.append(twitter_target(line.split()[0]))
            except InvalidData:
                ignored.append(number)
    return list(dict.fromkeys(urls)), ignored


def reddit_list(filename):
    users, subs, ignored = set(), set(), []
    for number, line in enumerate(read_list(filename), 1):
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        user, sub = _REDDIT_USER.fullmatch(line), _REDDIT_SUB.fullmatch(line)
        if user and user[1].lower() == "me":
            continue
        try:
            if user:
                users.add(reddit_name(user[1]))
            elif sub:
                subs.add(reddit_name(sub[1], "subreddit"))
            else:
                ignored.append(number)
        except InvalidData:
            ignored.append(number)
    return sorted(users), sorted(subs), ignored


def reddit_urls(users, subs, mode="new", saved=False):
    if mode not in ("new", "top"):
        raise InvalidData("Unsupported Reddit scan mode")
    result = []
    for user in users:
        for period in ((None,) if mode == "new" else ("all", "year")):
            result.append(f"https://www.reddit.com/user/{user}/submitted/?sort={mode}" + (f"&t={period}" if period else ""))
            result.append(f"https://www.reddit.com/search?q=author%3A{user}+nsfw%3Ayes&include_over_18=on&sort={mode}&t={period or 'all'}")
    for sub in subs:
        for period in ((None,) if mode == "new" else ("all", "year")):
            result.append(f"https://www.reddit.com/r/{sub}/?sort={mode}" + (f"&t={period}" if period else ""))
    if saved:
        for period in ((None,) if mode == "new" else ("all", "year")):
            result.append("https://www.reddit.com/user/me/saved/?sort=" + mode + (f"&t={period}" if period else ""))
    return list(dict.fromkeys(result))


def date_min(absolute, relative, days, now):
    """Retain gallery-dl's UTC interpretation of unzoned date-min values.

    Existing relative options format local wall time without an offset before
    gallery-dl reads it as UTC. Preserve that boundary explicitly during caller
    conversion instead of silently shifting already configured date filters.
    """
    if absolute:
        try:
            # gallery-dl parses numeric -o values as JSON numbers and date
            # strings as ISO 8601. Reject invalid strings instead of dropping
            # the filter and accidentally requesting all history.
            if re.fullmatch(r"-?\d+(?:\.\d+)?", absolute):
                value = datetime.fromtimestamp(float(absolute), timezone.utc)
            else:
                value = datetime.fromisoformat(absolute).replace(microsecond=0)
                if value.tzinfo is None:
                    value = value.replace(tzinfo=timezone.utc)
        except (ValueError, OverflowError, OSError):
            raise InvalidData("Invalid absolute Reddit date minimum") from None
        return windows.timestamp(value.isoformat(timespec="microseconds"))
    if days is not None and relative:
        raise InvalidData("Choose one relative Reddit date option, or an absolute date minimum")
    if days is not None:
        if days < 0:
            raise InvalidData("Relative day count must be nonnegative")
        relative = f"{days} days ago"
    if not relative:
        return None
    local = datetime.fromtimestamp(now).replace(microsecond=0)
    value = relative.strip().lower()
    try:
        if value == "now":
            lower = local
        elif value in ("today", "yesterday"):
            lower = (local - timedelta(days=value == "yesterday")).replace(hour=0, minute=0, second=0)
        else:
            match = _RELATIVE.fullmatch(value)
            if match is None:
                raise InvalidData("Unsupported relative Reddit date minimum")
            unit, count = match["unit"].lower(), int(match["n"])
            seconds = 60 if unit.startswith("min") else 3600 if unit.startswith(("h", "hr")) else 86400 if unit.startswith("day") else 604800
            lower = local - timedelta(seconds=count * seconds)
    except (ValueError, OverflowError):
        raise InvalidData("Relative Reddit date minimum exceeds the timestamp range") from None
    return windows.timestamp(lower.replace(tzinfo=timezone.utc).isoformat(timespec="milliseconds"))
