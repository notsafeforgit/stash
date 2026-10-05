"""Authenticated, digest-verified retrieval of a fixed native server checkpoint.

The enclosing exporter snapshots producer journals first. This client never
accepts a server-selected URL or filesystem destination, follows a redirect, or
publishes a complete-archive claim from server components alone.
"""

import base64
from contextlib import closing
import hashlib
import json
import math
import os
from pathlib import Path
import re
import shutil
import urllib.error
import urllib.parse
import urllib.request
import uuid

from .storage import InvalidArchive, HEX, decode_json, publish_bytes, require_space, sync_directory

FORMAT = "org.notsafeforgit.stash.server-checkpoint"
COVERAGE = "database-configuration-deletion-recovery"
ROLES = {"library.sqlite": "library", "deletions.zip": "file_journal",
         "config.yml": "config", "runtime-overrides.yml": "config", "tls.crt": "config", "tls.key": "config"}
RESERVED = {(role, name) for name, role in ROLES.items() if role != "library"} | {("operating_state", "server-checkpoint.json")}


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, message, headers, new_url):
        raise InvalidArchive("Native checkpoint transport refused a redirect")


def request_bytes(request):
    # Match encoding/json's struct field order and HTML escaping for the server's
    # persisted normalized request digest, independently of HTTP JSON formatting.
    body = json.dumps(request, ensure_ascii=False, separators=(",", ":"), allow_nan=False)
    for character, replacement in (("&", "\\u0026"), ("<", "\\u003c"), (">", "\\u003e"),
                                   ("\u2028", "\\u2028"), ("\u2029", "\\u2029")):
        body = body.replace(character, replacement)
    return body.encode("utf-8")


class ServerCheckpoint:
    def __init__(self, server, api_key, request_id, roots=(), *, timeout=3600):
        try:
            url = urllib.parse.urlsplit(server)
            port = url.port
            valid_id = str(uuid.UUID(request_id)) == request_id
        except (ValueError, TypeError, AttributeError) as error:
            raise InvalidArchive("Invalid native checkpoint server or request UUID") from error
        if (url.scheme not in ("http", "https") or not url.hostname or url.username is not None
                or url.password is not None or url.query or url.fragment or (port is not None and port <= 0)
                or not valid_id or type(timeout) not in (int, float) or not math.isfinite(timeout) or timeout <= 0):
            raise InvalidArchive("Invalid native checkpoint connection options")
        if not isinstance(api_key, str) or not re.fullmatch(r"[!-~]{1,8192}", api_key):
            raise InvalidArchive("A native checkpoint requires an application API key")
        roots = list(roots)
        if len(roots) > 1024 or any(not isinstance(r, dict) or set(r) != {"name", "path"}
                                  or not all(isinstance(r[k], str) for k in r) for r in roots):
            raise InvalidArchive("Recovery roots require name/path objects")
        self.server, self.api_key, self.request_id = server.rstrip("/"), api_key, request_id
        self.roots = [{"name": r["name"], "path": r["path"]} for r in roots]
        self.timeout = timeout
        self.opener = urllib.request.build_opener(NoRedirect())

    def open(self, suffix, data=None):
        request = urllib.request.Request(self.server + "/api/v3/backups/checkpoints" + suffix, data=data,
                                         headers={"ApiKey": self.api_key, "Accept-Encoding": "identity",
                                                  "Content-Type": "application/json"})
        try:
            response = self.opener.open(request, timeout=self.timeout)
        except (urllib.error.HTTPError, urllib.error.URLError) as error:
            # Do not include request headers or credential-bearing exception data.
            status = getattr(error, "code", None)
            raise InvalidArchive(f"Native checkpoint request failed{f' (HTTP {status})' if status else ''}") from None
        if response.status != 200 or response.headers.get("Content-Encoding", "identity") != "identity":
            response.close()
            raise InvalidArchive("Unexpected native checkpoint response")
        return response

    def capture(self, destination, *, reserve):
        from .bundle import connect_readonly, database_metadata
        destination = Path(destination)
        request = {"uuid": self.request_id, "recovery_roots": self.roots, "reserve_bytes": reserve}
        request_hash = hashlib.sha256(request_bytes(request)).hexdigest()
        require_space(destination.parent, 0, reserve)
        destination.mkdir(mode=0o700)
        try:
            with self.open("", request_bytes(request)) as response:
                if response.headers.get_content_type() != "application/json":
                    raise InvalidArchive("Native checkpoint response is not JSON")
                body = response.read((1 << 20) + 1)
            if len(body) > 1 << 20:
                raise InvalidArchive("Native checkpoint manifest exceeds its size limit")
            manifest = decode_json(body)
            self.validate(manifest, request_hash)
            components = []
            expected = {}
            for component in manifest["components"]:
                name = component["name"]
                expected[(component["role"], "library" if component["role"] == "library" else name)] = (component["sha256"], component["bytes"])
                require_space(destination, component["bytes"], reserve)
                with self.open(f"/{self.request_id}/components/{name}") as response:
                    if response.headers.get("X-Stash-SHA256") != component["sha256"]:
                        raise InvalidArchive("Native checkpoint component digest header differs")
                    length = response.headers.get("Content-Length")
                    if length is not None and length != str(component["bytes"]):
                        raise InvalidArchive("Native checkpoint component length differs")
                    fd = os.open(destination / name, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
                    digest, count = hashlib.sha256(), 0
                    with os.fdopen(fd, "wb") as output:
                        while True:
                            require_space(destination, max(0, component["bytes"] - count), reserve)
                            chunk = response.read(min(1 << 20, component["bytes"] - count + 1))
                            if not chunk:
                                break
                            count += len(chunk)
                            if count > component["bytes"]:
                                raise InvalidArchive("Native checkpoint component exceeded its declared size")
                            output.write(chunk)
                            digest.update(chunk)
                        if count != component["bytes"] or digest.hexdigest() != component["sha256"]:
                            raise InvalidArchive("Native checkpoint component size/digest mismatch")
                        output.flush()
                        os.fsync(output.fileno())
                if component["role"] != "library":
                    components.append({"role": component["role"], "name": name, "path": destination / name})
            native = destination / "library.sqlite"
            with closing(connect_readonly(native)) as db:
                metadata = database_metadata(db, "library")
                ids = [r[0] for r in db.execute("SELECT id FROM file_deletions ORDER BY id")]
                if ids != (manifest["committed_deletion_ids"] or []):
                    raise InvalidArchive("Native checkpoint journal markers differ from its library")
            publish_bytes(destination / "checkpoint.json", body)
            components.append({"role": "operating_state", "name": "server-checkpoint.json", "path": destination / "checkpoint.json"})
            expected[("operating_state", "server-checkpoint.json")] = (hashlib.sha256(body).hexdigest(), len(body))
            sync_directory(destination)
            return native, metadata, components, expected
        except BaseException:
            shutil.rmtree(destination)
            raise

    def validate(self, manifest, request_hash):
        fields = {"format", "version", "uuid", "coverage", "created_at", "request_sha256", "source_database_path",
                  "source_config_path", "source_working_directory", "committed_deletion_ids", "components"}
        if (not isinstance(manifest, dict) or set(manifest) != fields or manifest["format"] != FORMAT
                or type(manifest["version"]) is not int or manifest["version"] != 1
                or manifest["uuid"] != self.request_id or manifest["coverage"] != COVERAGE
                or not isinstance(manifest["request_sha256"], str) or not HEX.fullmatch(manifest["request_sha256"])
                or manifest["request_sha256"] != request_hash or not isinstance(manifest["created_at"], str)):
            raise InvalidArchive("Native checkpoint manifest does not match the requested capture")
        for key in ("source_database_path", "source_config_path", "source_working_directory"):
            if manifest[key] is not None:
                if not isinstance(manifest[key], str):
                    raise InvalidArchive("Invalid native checkpoint source path")
                base64.b64decode(manifest[key], validate=True)
        ids = manifest["committed_deletion_ids"]
        if ids is not None and (not isinstance(ids, list) or any(not isinstance(i, str) or str(uuid.UUID(i)) != i for i in ids)
                                or ids != sorted(set(ids))):
            raise InvalidArchive("Invalid native checkpoint deletion markers")
        if not isinstance(manifest["components"], list):
            raise InvalidArchive("Invalid native checkpoint component inventory")
        names = set()
        for entry in manifest["components"]:
            if (not isinstance(entry, dict) or set(entry) not in ({"role", "name", "bytes", "sha256"},
                                                                {"role", "name", "bytes", "sha256", "source_path"})
                    or not isinstance(entry["name"], str) or entry["name"] in names
                    or entry["role"] != ROLES.get(entry["name"]) or entry["role"] is None
                    or type(entry["bytes"]) is not int or entry["bytes"] < 0
                    or not isinstance(entry["sha256"], str) or not HEX.fullmatch(entry["sha256"])):
                raise InvalidArchive("Invalid native checkpoint component")
            if "source_path" in entry:
                if not isinstance(entry["source_path"], str):
                    raise InvalidArchive("Invalid configuration asset source path")
                base64.b64decode(entry["source_path"], validate=True)
            names.add(entry["name"])
        if not {"library.sqlite", "deletions.zip", "config.yml", "runtime-overrides.yml"} <= names:
            raise InvalidArchive("Native checkpoint is missing required components")
