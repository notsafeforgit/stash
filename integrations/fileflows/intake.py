#!/usr/bin/env python3
"""Deliver finished FileFlows clip batches to native Stash; retry across restarts."""
import argparse
from contextlib import contextmanager
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import sys
from urllib.error import HTTPError
from urllib.parse import urlsplit
from urllib.request import build_opener, HTTPRedirectHandler, Request
from uuid import NAMESPACE_URL, UUID, uuid5

LIMIT = 262144


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, *args):
        return None


def read_json(path):
    with path.open("rb") as file:
        data = file.read(LIMIT + 1)
    if len(data) > LIMIT:
        raise ValueError("JSON exceeds the batch limit")
    return json.loads(data)


def write_json(path, value):
    temp = path.with_suffix(path.suffix + ".part")
    with temp.open("w") as file:
        os.chmod(temp, 0o600)
        json.dump(value, file, ensure_ascii=False)
        file.flush()
        os.fsync(file.fileno())
    os.replace(temp, path)
    sync_directory(path.parent)


def sync_directory(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


@contextmanager
def lock(path):
    with path.open("a") as file:
        fcntl.flock(file, fcntl.LOCK_EX | fcntl.LOCK_NB)
        yield


class Client:
    def __init__(self, config):
        self.endpoint = config["endpoint"].rstrip("/")
        parsed = urlsplit(self.endpoint)
        if parsed.scheme not in {"http", "https"} or parsed.username or parsed.password or parsed.query or parsed.fragment:
            raise ValueError("Invalid Stash endpoint")
        self.key = Path(config["api_key_file"]).read_text().strip()
        self.opener = build_opener(NoRedirect())

    def request(self, route, value=None):
        req = Request(self.endpoint + "/api/v3/archive" + route,
                      data=None if value is None else json.dumps(value).encode(),
                      headers={"ApiKey": self.key, "Content-Type": "application/json", "Accept": "application/json"})
        try:
            with self.opener.open(req, timeout=30) as response:
                if response.headers.get_content_type() != "application/json":
                    raise ValueError("Unexpected Stash response")
                data = response.read(LIMIT + 1)
                if len(data) > LIMIT:
                    raise ValueError("Stash response exceeds the limit")
                return json.loads(data)
        except HTTPError as error:
            if error.code == 404 and route.startswith("/manual-intake/requests/"):
                return None
            raise


def batch_input(folder, manifest, binding, body=None):
    if manifest.is_symlink() or not manifest.is_file():
        raise ValueError("Pending import must be a regular file")
    body = read_json(manifest) if body is None else body
    if body.get("format") != "stash-clip-import-v1" or set(body) != {"format", "batch_id", "file", "size", "metadata"}:
        raise ValueError("Invalid pending import format")
    batch = body["batch_id"]
    if not isinstance(batch, str) or not re.fullmatch(r"[A-Za-z0-9-]{16,80}", batch) or manifest.name != f"stash-import-{batch}.json":
        raise ValueError("Invalid batch identity")
    name = body["file"]
    if not isinstance(name, str) or Path(name).name != name or any(c in name for c in "\\\x00\r\n") or not name.endswith(".mkv"):
        raise ValueError("Invalid finished video name")
    path = folder / name
    if not path.exists():
        return None  # Metadata is deliberately published before the completed file.
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or type(body["size"]) is not int or body["size"] < 1024 or info.st_size != body["size"]:
        raise ValueError("Finished video does not match its pending import")
    metadata = body["metadata"]
    if not isinstance(metadata, dict) or set(metadata) != {"title", "details", "date"} or not all(isinstance(v, str) for v in metadata.values()):
        raise ValueError("Invalid phone metadata")
    relative = str(Path(binding["relative_path"]) / name)
    return {"collection_uuid": binding["collection_uuid"], "relative_path": relative,
            "media_kind": "scene", "metadata": {k: v for k, v in metadata.items() if v}}


def deliver(client, state_dir, folder, manifest, binding, report):
    if manifest.is_symlink() or not manifest.is_file():
        raise ValueError("Pending import must be a regular file")
    with manifest.open("rb") as file:
        original = file.read(LIMIT + 1)
    if len(original) > LIMIT:
        raise ValueError("Pending import exceeds the limit")
    body = json.loads(original)
    request_id = str(uuid5(NAMESPACE_URL, "stash-fileflows:" + binding["collection_uuid"] + ":" + body["batch_id"]))
    state_path = state_dir / (request_id + ".json")
    digest = hashlib.sha256(original).hexdigest()
    saved = read_json(state_path) if state_path.exists() else None
    if saved and saved["manifest_sha256"] != digest:
        raise ValueError("The metadata of an admitted batch changed")
    if saved:
        request = saved["request"]
        result = client.request("/manual-intake/requests/" + request_id)
    else:
        input = batch_input(folder, manifest, binding, body)
        if input is None:
            return
        capabilities = client.request("/manual-intake/capabilities")
        if not capabilities.get("file_ingestion") or not capabilities.get("file_metadata"):
            raise ValueError("Stash does not yet support metadata with local imports")
        preview = client.request("/manual-intake/preview", input)
        if any(preview.get(k) != v for k, v in input.items()):
            raise ValueError("Stash preview differs from the requested file or metadata")
        if not re.fullmatch(r"[a-f0-9]{64}", preview.get("signature", "")):
            raise ValueError("Invalid intake signature")
        request = dict(input, request_uuid=request_id, signature=preview["signature"])
        saved = {"manifest_sha256": digest, "request": request}
        write_json(state_path, saved)  # Save the exact request before any admission.
        result = None
    if result is None:
        result = client.request("/manual-intake/apply", request)
    if not isinstance(result, dict) or any(result.get(k) != v for k, v in request.items()):
        raise ValueError("Stash receipt does not match its saved request")
    publication = result.get("publication") or {}
    report.append({"request_uuid": request_id, "state": result["state"],
                   "media_uuid": publication.get("media_uuid"), "review": publication.get("review", [])})
    if result["state"] != "succeeded" or not result.get("media_ingested") or not result.get("registration_committed"):
        return
    UUID(publication["media_uuid"])
    if publication.get("review"):
        return  # Keep the requested metadata available if a curated field conflicts.
    if manifest.read_bytes() != original:
        raise ValueError("Pending import changed during delivery")
    manifest.unlink()
    sync_directory(manifest.parent)
    state_path.unlink()
    sync_directory(state_dir)


def run(config, client=None):
    state = Path(config["state_dir"])
    state.mkdir(parents=True, exist_ok=True, mode=0o700)
    report = []
    client = client or Client(config)
    with lock(state / "delivery.lock"):
        for binding in config["folders"]:
            UUID(binding["collection_uuid"])
            relative = Path(binding["relative_path"])
            if relative.is_absolute() or ".." in relative.parts:
                raise ValueError("Output directory must be relative to the Stash media root")
            folder = Path(binding["path"])
            if not folder.is_absolute() or folder.is_symlink():
                raise ValueError("Output directory must be an absolute real directory")
            for manifest in sorted(folder.glob("stash-import-*.json"))[:100]:
                try:
                    deliver(client, state, folder, manifest, binding, report)
                except (OSError, ValueError, KeyError, TypeError) as error:
                    report.append({"pending_import": manifest.name, "error": type(error).__name__})
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", type=Path, required=True)
    args = parser.parse_args()
    os.umask(0o077)
    try:
        report = run(read_json(args.config))
    except BlockingIOError:
        return  # A previous timer invocation is still delivering.
    print(json.dumps(report))
    if any("error" in item or item.get("state") in {"failed", "cancelled"} for item in report):
        sys.exit(1)


if __name__ == "__main__":
    main()
