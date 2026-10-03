"""Bounded application operations for reviewed queue activation."""

from http.client import HTTPException
import os
from urllib.error import HTTPError, URLError
from urllib.request import Request
import uuid

from .backfill_import import ImportClient
from .catalog_source import source_time
from .client import Unavailable
from .encoding import InvalidData, decode, encode, identifier
from .events import sha256 as valid_sha256

def sha256(value):
    if not valid_sha256(value):
        raise InvalidData("Expected a SHA-256 digest")
    return value


def integer(value, minimum=0, maximum=(1 << 63) - 1):
    if type(value) is not int or not minimum <= value <= maximum:
        raise InvalidData("Invalid activation counter")
    return value


def activation_input(operation, snapshot, manifest_sha256, candidates):
    return {"uuid": identifier(operation), "snapshot_uuid": identifier(snapshot), "manifest_sha256": sha256(manifest_sha256),
            "targets": sorted(({"target_uuid": row["target_uuid"], "revision": row["revision"]}
                               for row in candidates if row["disposition"] == "eligible"), key=lambda row: row["target_uuid"])}


def validate_receipt(value, preview, limit, activated=None):
    if (not isinstance(value, dict) or set(value) != set(preview) | {"activated", "created_at"}
            or encode({key: value[key] for key in preview}, limit) != encode(preview, limit)):
        raise InvalidData("Activation receipt differs from the saved preview")
    expected = activated if activated is not None else [{"target_uuid": ref["target_uuid"], "revision": ref["revision"] + 1} for ref in preview["input"]["targets"]]
    if encode(value["activated"]) != encode(expected):
        raise InvalidData("Activation receipt changed its released revisions")
    source_time(value["created_at"])
    return value


class ActivationClient(ImportClient):
    """Domain subclasses supply bounds and semantic candidate/preview checks."""

    activation_input = staticmethod(activation_input)

    def request(self, method, path, value=None):
        key = os.environ.get(self.key_env, "")
        if not key or any(ord(c) <= 32 or ord(c) >= 127 for c in key):
            raise Unavailable("stash_application_key_missing")
        request = Request(self.endpoint + "/api/v3/archive" + path, method=method,
                          data=None if value is None else encode(value, self.max_http_bytes),
                          headers={"ApiKey": key, "Accept": "application/json", "Content-Type": "application/json"})
        try:
            with self.opener.open(request, timeout=60) as response:
                if (response.status != 200 or response.headers.get_content_type() != "application/json"
                        or response.headers.get("Content-Encoding") is not None):
                    raise Unavailable(f"invalid_{self.kind}_activation_response")
                return decode(response.read(self.max_http_bytes + 1), self.max_http_bytes)
        except HTTPError as error:
            code = error.code
            error.close()
            raise Unavailable(f"{self.kind}_activation_rejected", code) from None
        except (HTTPException, URLError, OSError, TimeoutError):
            raise Unavailable("network_unavailable") from None
        except InvalidData:
            raise Unavailable(f"invalid_{self.kind}_activation_response") from None

    def candidate_pages(self, snapshot, manifest_sha256):
        identifier(snapshot)
        sha256(manifest_sha256)
        after, count = 0, 0
        while True:
            page = self.request("GET", f"/automation-snapshots/{snapshot}/{self.kind}-import/held-targets"
                                f"?expected_manifest_sha256={manifest_sha256}&after={after}&limit={self.max_batch}")
            try:
                if not isinstance(page, list) or len(page) > self.max_batch:
                    raise InvalidData("Invalid activation candidate page")
                for row in page:
                    self.validate_candidate(row, after)
                    after = row["ordinal"]
            except (InvalidData, KeyError, TypeError):
                raise Unavailable(f"invalid_{self.kind}_candidates") from None
            count += len(page)
            if count > self.max_candidates:
                raise InvalidData("Activation plan exceeds one million original holds")
            if page:
                yield page
            if len(page) < self.max_batch:
                return

    def preview(self, snapshot, manifest_sha256, candidates):
        expected = self.activation_input(str(uuid.uuid4()), snapshot, manifest_sha256, candidates)
        result = self.request("POST", f"/{self.kind}-activations/preview", expected)
        try:
            return self.validate_preview(result, expected, candidates)
        except (InvalidData, KeyError, TypeError):
            raise Unavailable(f"invalid_{self.kind}_preview") from None

    def status(self, preview):
        try:
            result = self.request("GET", f"/{self.kind}-activations/" + identifier(preview["input"]["uuid"]))
        except Unavailable as error:
            if error.status == 404:
                return None
            raise
        try:
            return self.validate_receipt(result, preview)
        except (InvalidData, KeyError, TypeError):
            raise Unavailable(f"invalid_{self.kind}_receipt") from None

    def apply(self, preview):
        previous = self.status(preview)
        if previous is not None:
            return previous
        result = self.request("POST", f"/{self.kind}-activations", {"input": preview["input"], "expected_plan_sha256": preview["plan_sha256"]})
        try:
            return self.validate_receipt(result, preview)
        except (InvalidData, KeyError, TypeError):
            raise Unavailable(f"invalid_{self.kind}_receipt") from None
