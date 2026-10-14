"""Downloader provenance and the native adapter's stable capture contract.

Upstream patches do not change a queue's policy. The adapter and reviewed
configuration still do. Deployments may test and activate newer downloader
commits independently; installed revisions remain visible in diagnostics.
"""

from importlib.metadata import distribution, PackageNotFoundError
import json
import re

from .encoding import InvalidData

CAPTURE_VERSION = "stash-gallery-v1"
# Upgrade-only checkpoint labels, emitted before the adapter had its own version.
# Never admit new work with these labels; retain them when finishing old work.
LEGACY_CAPTURE_VERSIONS = ("1.33.0-dev", "1.32.15-dev")
REPOSITORIES = {
    "gallery-dl": "https://codeberg.org/mikf/gallery-dl.git",
    "yt-dlp": "https://github.com/yt-dlp/yt-dlp.git",
}


def downloader_identity():
    result = {}
    try:
        for name, repository in REPOSITORIES.items():
            package = distribution(name)
            direct = json.loads(package.read_text("direct_url.json") or "{}")
            vcs = direct.get("vcs_info", {})
            commit = vcs.get("commit_id", "")
            if (direct.get("url") != repository or vcs.get("vcs") != "git"
                    or not re.fullmatch(r"[a-f0-9]{40}", commit)):
                raise InvalidData("Downloader requires an identifiable official upstream Git revision")
            result[name] = {"version": package.version, "commit": commit}
    except (PackageNotFoundError, ValueError, AttributeError, TypeError):
        raise InvalidData("Install the gallery-dl and yt-dlp Git runtimes before use") from None
    return result


def capture_versions(configuration):
    current = configuration.extractor_version
    return (current, *LEGACY_CAPTURE_VERSIONS) if current == CAPTURE_VERSION else (current,)


def capture_version(value):
    if value not in (CAPTURE_VERSION, *LEGACY_CAPTURE_VERSIONS):
        raise InvalidData("Unsupported metadata capture contract")
    return value


def ready_jobs(client, collection, configuration, *, after, limit):
    """Merge old checkpoint jobs into the same ordered, bounded queue traversal."""
    rows = {}
    for version in capture_versions(configuration):
        for row in client.ready_jobs(collection, configuration.policy_sha256, version, after=after, limit=limit):
            rows[row["uuid"]] = row
    return sorted(rows.values(), key=lambda row: row["sequence"])[:limit]
