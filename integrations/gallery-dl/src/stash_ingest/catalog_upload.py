"""Stage a frozen catalog snapshot through Stash's resumable migration API."""

import argparse
from http.client import HTTPException
import json
import os
from pathlib import Path
import stat
import sys
from urllib.error import HTTPError, URLError
from urllib.request import Request

from .backfill_import import ImportClient
from .catalog_snapshot import MAX_CHUNK_BYTES, MAX_MANIFEST_BYTES, verify
from .catalog_source import source_time
from .client import Unavailable
from .encoding import InvalidData, decode, digest, identifier


def read_regular(path, limit):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, "rb") as source:
        if not stat.S_ISREG(os.fstat(source.fileno()).st_mode):
            raise InvalidData("Snapshot input must be a regular file")
        body = source.read(limit + 1)
    if len(body) > limit:
        raise InvalidData("Snapshot input exceeds its size limit")
    return body


class CatalogUploadClient(ImportClient):
    def request(self, method, suffix, body, manifest_sha256, content_type):
        key = os.environ.get(self.key_env, "")
        if not key or any(ord(c) <= 32 or ord(c) >= 127 for c in key):
            raise Unavailable("stash_application_key_missing")
        request = Request(self.endpoint + "/api/v3/archive/catalog-snapshots" + suffix,
                          data=body, method=method,
                          headers={"ApiKey": key, "Accept": "application/json", "Content-Type": content_type,
                                   "X-Stash-Manifest-SHA256": manifest_sha256})
        try:
            with self.opener.open(request, timeout=60) as response:
                if (response.status != 200 or response.headers.get_content_type() != "application/json"
                        or response.headers.get("Content-Encoding") is not None):
                    raise Unavailable("invalid_catalog_snapshot_receipt")
                return decode(response.read((64 << 10) + 1), 64 << 10)
        except HTTPError as error:
            status = error.code
            error.close()
            raise Unavailable("catalog_snapshot_rejected", status) from None
        except (HTTPException, URLError, OSError, TimeoutError):
            raise Unavailable("network_unavailable") from None
        except InvalidData:
            raise Unavailable("invalid_catalog_snapshot_receipt") from None

    @staticmethod
    def validate_receipt(result, manifest, manifest_sha256, minimum=0):
        try:
            if not isinstance(result, dict):
                raise InvalidData("Receipt must be an object")
            count = result["next_chunk"]
            chunks = manifest["chunks"]
            expected = {key: manifest[key] for key in ("snapshot_uuid", "registry_source_uuid", "catalog_id", "captured_at", "records")}
            expected.update(manifest_sha256=manifest_sha256, chunks=len(chunks), bytes=sum(c["bytes"] for c in chunks),
                            pending_families=sorted(manifest["tables"]), state="received" if count == len(chunks) else "receiving")
            if (type(count) is not int or not minimum <= count <= len(chunks)
                    or any(type(result.get(key)) is not int for key in ("chunks", "records", "bytes", "received_records", "received_bytes"))
                    or any(result.get(key) != value for key, value in expected.items()) or result.get("imported") is not False
                    or result.get("received_records") != sum(c["rows"] for c in chunks[:count])
                    or result.get("received_bytes") != sum(c["bytes"] for c in chunks[:count])):
                raise InvalidData("Receipt does not match the frozen input")
            for key in ("registry_import_uuid", "collection_uuid"):
                identifier(result[key])
            for key in ("created_at", "updated_at"):
                source_time(result[key])
        except (InvalidData, KeyError, TypeError):
            raise Unavailable("invalid_catalog_snapshot_receipt") from None
        return result

    def upload(self, directory, expected_sha256):
        directory = Path(directory).resolve(strict=True)
        checked = verify(directory, expected_sha256)
        body = read_regular(directory / "manifest.json", MAX_MANIFEST_BYTES)
        if digest(body) != checked["manifest_sha256"]:
            raise InvalidData("Snapshot manifest changed after verification")
        manifest = decode(body, MAX_MANIFEST_BYTES)
        result = self.validate_receipt(self.request("POST", "", body, expected_sha256, "application/json"), manifest, expected_sha256)
        binding = {key: result[key] for key in ("registry_import_uuid", "collection_uuid", "created_at")}
        while result["next_chunk"] < len(manifest["chunks"]):
            index = result["next_chunk"]
            chunk = manifest["chunks"][index]
            body = read_regular(directory / chunk["file"], MAX_CHUNK_BYTES)
            if len(body) != chunk["bytes"] or digest(body) != chunk["sha256"]:
                raise InvalidData("Snapshot chunk changed after verification")
            suffix = f"/{manifest['snapshot_uuid']}/chunks/{index}"
            result = self.validate_receipt(self.request("PUT", suffix, body, expected_sha256, "application/x-ndjson"),
                                           manifest, expected_sha256, index + 1)
            if any(result[key] != value for key, value in binding.items()):
                raise Unavailable("catalog_snapshot_binding_changed")
        return result


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--snapshot", required=True, help="Directory produced by stash-prepare-catalog")
    parser.add_argument("--expected-sha256", required=True, help="Manifest digest from the reviewed frozen snapshot")
    parser.add_argument("--endpoint", required=True, help="Explicit native Stash origin")
    parser.add_argument("--api-key-env", default="STASH_API_KEY")
    args = parser.parse_args(argv)
    try:
        result = CatalogUploadClient(args.endpoint, args.api_key_env).upload(args.snapshot, args.expected_sha256)
        print(json.dumps(result, sort_keys=True))
        return 0
    except (InvalidData, Unavailable) as error:
        message = str(error)
    except (OSError, KeyError, TypeError, AttributeError, ValueError):
        message = "Frozen catalog snapshot is unavailable or invalid"
    print(json.dumps({"error": message, "acknowledged": False, "imported": False,
                      "resume": "repeat_same_snapshot_and_manifest_digest"}), file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
