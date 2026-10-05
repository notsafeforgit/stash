"""Retire failed, unsealed attempts without claiming a published backup.

The host holds its backup/dedupe lock and worker barriers. Server abandonment
must succeed before local cleanup: a missing GET alone cannot fence a delayed
capture request. Permanent records retain the UUID and original request digest.
"""

import base64
import os
from pathlib import Path
import stat

from .artwork_pins import directory, MD5
from .filesystem_boundary import canonical_uuid, timestamp
from .storage import HEX, InvalidArchive, decode_json, json_bytes, open_regular, publish_bytes, sync_directory

FORMAT = "org.notsafeforgit.stash.server-checkpoint-abandonment"
STATUS_FORMAT = "org.notsafeforgit.stash.server-checkpoint-status"


def read_record(path, *, optional=False, limit=1 << 20):
    try:
        with open_regular(path) as incoming:
            body = incoming.read(limit + 1)
    except FileNotFoundError:
        if optional:
            return None
        raise InvalidArchive("Missing unsealed checkpoint ownership record") from None
    if len(body) > limit:
        raise InvalidArchive("Oversized checkpoint ownership record")
    return decode_json(body)


def retain(path, record):
    stored = read_record(path, optional=True, limit=16 << 20)
    if stored is None:
        publish_bytes(path, json_bytes(record))
    elif stored != record:
        raise InvalidArchive("Checkpoint abandonment identity changed")


def validate_abandonment(record, checkpoint, request_hash=None):
    fields = {"format", "version", "uuid", "request_sha256", "abandoned_at"}
    if (not isinstance(record, dict) or set(record) != fields or record["format"] != FORMAT
            or type(record["version"]) is not int or record["version"] != 1
            or not canonical_uuid(checkpoint) or record["uuid"] != checkpoint
            or not isinstance(record["request_sha256"], str) or not HEX.fullmatch(record["request_sha256"])
            or (request_hash is not None and record["request_sha256"] != request_hash)):
        raise InvalidArchive("Invalid unsealed checkpoint abandonment receipt")
    timestamp(record["abandoned_at"])
    return record


def response_record(client, suffix, data=None):
    with client.open(f"/{client.request_id}/{suffix}", data) as response:
        if response.headers.get_content_type() != "application/json":
            raise InvalidArchive("Checkpoint recovery did not return JSON")
        body = response.read(4097)
    if len(body) > 4096:
        raise InvalidArchive("Oversized checkpoint recovery response")
    return decode_json(body)


def checkpoint_status(client, reserve):
    record = response_record(client, "status")
    fields = {"format", "version", "uuid", "state"}
    if not isinstance(record, dict):
        raise InvalidArchive("Invalid checkpoint status response")
    state = record.get("state")
    if not isinstance(state, str):
        raise InvalidArchive("Invalid checkpoint state")
    if state != "missing":
        fields.add("request_sha256")
    if state in {"sealed", "released"}:
        fields.add("checkpoint_sha256")
    if (set(record) != fields or record["format"] != STATUS_FORMAT
            or type(record["version"]) is not int or record["version"] != 1
            or record["uuid"] != client.request_id or state not in {"missing", "partial", "sealed", "released", "abandoned"}
            or (state != "missing" and record["request_sha256"] != client.request_hash(reserve))
            or (state in {"sealed", "released"} and (not isinstance(record["checkpoint_sha256"], str)
                                                     or not HEX.fullmatch(record["checkpoint_sha256"])))):
        raise InvalidArchive("Checkpoint status differs from the original attempt")
    return record


def abandon_checkpoint(client, reserve):
    request_hash = client.request_hash(reserve)
    record = response_record(client, "abandon", json_bytes({"request_sha256": request_hash}))
    return validate_abandonment(record, client.request_id, request_hash)


def safe_directory(path, *, identity=None):
    from .zfs_media import mounts
    observed = directory(path, private=True)
    if identity is not None and observed != identity:
        raise InvalidArchive("Abandoned checkpoint directory was replaced")
    if any(mount.is_relative_to(path) for mount, _, _ in mounts()):
        raise InvalidArchive("Abandoned checkpoint contains an unexpected mount")
    return observed


def remove_file(path, device):
    try:
        info = path.lstat()
    except FileNotFoundError:
        return
    if not stat.S_ISREG(info.st_mode) or info.st_dev != device:
        raise InvalidArchive("Abandoned checkpoint component is not an owned regular file")
    path.unlink()


def abandon_components(cache, client, reserve, receipt):
    from .component_stage import FORMAT as COMPONENT_FORMAT, MAX_MANIFEST
    validate_abandonment(receipt, client.request_id, client.request_hash(reserve))
    path = Path(cache) / client.request_id
    if not path.exists() and not path.is_symlink():
        return
    device, _ = safe_directory(path)
    for name in ("checkpoint.json", "release.json", "released.json"):
        if (path / name).exists() or (path / name).is_symlink():
            raise InvalidArchive("Cannot abandon sealed or published external components")
    intent = read_record(path / "intent.json", optional=True, limit=MAX_MANIFEST)
    if intent is None:
        if any(path.iterdir()):
            raise InvalidArchive("Unowned external component files require inspection")
        return
    if (not isinstance(intent, dict) or set(intent) != {"format", "version", "uuid", "server", "request_sha256", "components"}
            or intent["format"] != COMPONENT_FORMAT or type(intent["version"]) is not int or intent["version"] != 1
            or intent["uuid"] != client.request_id or intent["server"] != client.server
            or intent["request_sha256"] != receipt["request_sha256"]
            or not isinstance(intent["components"], list) or len(intent["components"]) > 16384):
        raise InvalidArchive("External component attempt identity changed")
    # Indices are generated by the stage before copying; source paths are never
    # cleanup destinations. Partial SQLite/files are allowed after this fence.
    retain(path / "abandoned.json", receipt)
    for index in range(len(intent["components"])):
        remove_file(path / f"component-{index:05d}", device)
    sync_directory(path)


def abandon_artwork(provider, ready, receipt):
    from .artwork_pins import FORMAT as ARTWORK_FORMAT, MAX_RECORD
    validate_abandonment(receipt, ready["uuid"], ready["request_sha256"])
    path = provider.path(ready["uuid"], ready["token"])
    if not path.exists() and not path.is_symlink():
        return
    intent = read_record(path / "intent.json", limit=MAX_RECORD)
    fields = {"format", "version", "uuid", "token", "request_sha256", "device", "inode", "sources"}
    if (not isinstance(intent, dict) or set(intent) != fields or intent["format"] != ARTWORK_FORMAT + "-attempt"
            or type(intent["version"]) is not int or intent["version"] != 1
            or any(intent[key] != ready[key] for key in ("uuid", "token", "request_sha256"))
            or type(intent["device"]) is not int or type(intent["inode"]) is not int
            or intent["sources"] != [base64.b64encode(os.fsencode(p)).decode("ascii") for p in provider.sources]):
        raise InvalidArchive("Artwork attempt differs from its original capture")
    device, _ = safe_directory(path, identity=(intent["device"], intent["inode"]))
    for name in ("release.json", "released.json"):
        if (path / name).exists() or (path / name).is_symlink():
            raise InvalidArchive("Cannot abandon published artwork pins")
    retain(path / "abandoned.json", receipt)
    for index in range(len(provider.sources)):
        tree = path / str(index)
        if not tree.exists() and not tree.is_symlink():
            continue
        if safe_directory(tree)[0] != device:
            raise InvalidArchive("Artwork attempt contains a different filesystem")
        for candidate in tree.iterdir():
            if MD5.fullmatch(candidate.name) or candidate.name == "inventory.jsonl":
                remove_file(candidate, device)
        sync_directory(tree)
    sync_directory(path)


def abandon_media(provider, ready, receipt):
    from .zfs_media import FORMAT as MEDIA_FORMAT, PROPERTY, MAX_RECORD
    validate_abandonment(receipt, ready["uuid"], ready["request_sha256"])
    path = provider.path(ready["uuid"], ready["token"])
    if not path.exists() and not path.is_symlink():
        return
    safe_directory(path)
    intent = read_record(path / "intent.json", limit=MAX_RECORD)
    expected = {"format": MEDIA_FORMAT, "version": 1, **{k: ready[k] for k in ("uuid", "token", "request_sha256")},
                "dataset": provider.dataset, "dataset_guid": provider.dataset_guid,
                "mountpoint": base64.b64encode(os.fsencode(provider.mountpoint)).decode("ascii"),
                "snapshot": provider.dataset + "@stash-native-" + ready["uuid"] + "-" + ready["token"],
                "hold": "stash-native-" + ready["token"]}
    if intent != expected:
        raise InvalidArchive("Media attempt differs from its original capture")
    for name in ("release.json", "released.json"):
        if (path / name).exists() or (path / name).is_symlink():
            raise InvalidArchive("Cannot abandon published media snapshots")
    record = read_record(path / "manifest.json", optional=True, limit=MAX_RECORD)
    if record is not None:
        if (not isinstance(record, dict) or set(record) != set(intent) | {"snapshot_guid", "createtxg"}
                or {k: v for k, v in record.items() if k not in {"snapshot_guid", "createtxg"}} != intent):
            raise InvalidArchive("Retained media manifest changed")
    provider.topology()
    existing = provider.run("list", "-H", "-p", "-d", "1", "-t", "snapshot", "-o", "name", provider.dataset).splitlines()
    if intent["snapshot"] in existing:
        properties = provider.properties(intent["snapshot"])
        if any(properties[PROPERTY + k] != ready[field] for k, field in
               (("checkpoint", "uuid"), ("token", "token"), ("request", "request_sha256"))):
            raise InvalidArchive("Unsealed media snapshot has a different owner")
        if record is not None and any(properties[k] != record.get(field) for k, field in (("guid", "snapshot_guid"), ("createtxg", "createtxg"))):
            raise InvalidArchive("Unsealed media snapshot was replaced")
        holds = provider.holds(intent["snapshot"])
        if holds - {intent["hold"]}:
            raise InvalidArchive("Another owner still holds the unsealed media snapshot")
        # Keep the observed GUID even if capture stopped before its manifest.
        retirement = {"server_abandonment": receipt, "snapshot_guid": properties["guid"], "createtxg": properties["createtxg"]}
        retain(path / "abandoning.json", retirement)
        if intent["hold"] in holds:
            provider.run("release", intent["hold"], intent["snapshot"])
        # No force, recursion or deferred destruction. Foreign clones block.
        provider.run("destroy", intent["snapshot"])
    retain(path / "abandoned.json", receipt)


def abandon_host_capture(client, reserve, components, artwork, media):
    """Caller owns exclusion; success allows retiring this host run identity."""
    stage = Path(components) / client.request_id
    ready = read_record(stage / "boundary-ready.json", optional=True, limit=4096)
    if ready is not None:
        if (not isinstance(ready, dict) or set(ready) != {"uuid", "token", "request_sha256", "expires_at"}
                or ready["uuid"] != client.request_id or not canonical_uuid(ready["token"])
                or ready["request_sha256"] != client.request_hash(reserve)):
            raise InvalidArchive("Unsealed filesystem challenge identity changed")
        timestamp(ready["expires_at"])
    else:
        # The challenge is durable before either provider can create output.
        if any(p.name.startswith(client.request_id + "-") for cache in (artwork.cache, media.cache) for p in cache.iterdir()):
            raise InvalidArchive("Filesystem capture is missing its original challenge")
    receipt = abandon_checkpoint(client, reserve)
    if ready is not None:
        abandon_media(media, ready, receipt)
        abandon_artwork(artwork, ready, receipt)
    abandon_components(components, client, reserve, receipt)
    return receipt
