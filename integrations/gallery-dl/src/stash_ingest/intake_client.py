"""Bounded native manual-file discovery and durable admission contracts."""

from http.client import HTTPException
from urllib.error import HTTPError, URLError
from urllib.parse import urlencode
from urllib.request import Request

from .album_client import integer, sha256
from .application_key import application_key
from .backfill_import import ImportClient
from .catalog_source import source_time
from .client import Unavailable
from .dedupe_client import relative_path
from .encoding import InvalidData, decode, encode, identifier

LIMIT = 1 << 20
TERMINAL = {"succeeded", "failed", "cancelled"}
REJECTIONS = {"intake_preview_changed", "file_not_found"}
INPUT_KEYS = ("collection_uuid", "relative_path", "media_kind")


def directory(value):
    return value if value == "." else relative_path(value)


def includes(prefix, path):
    return prefix == "." or path == prefix or path.startswith(prefix + "/")


def file_input(value):
    if not isinstance(value, dict):
        raise InvalidData("Invalid local-file input")
    identifier(value.get("collection_uuid"))
    relative_path(value.get("relative_path"))
    if value.get("media_kind") not in {"scene", "image"} or value["relative_path"].lower().endswith(".part"):
        raise InvalidData("Invalid local-file kind")
    return {key: value[key] for key in INPUT_KEYS}


def saved_request(value):
    file_input(value)
    if set(value) != set(INPUT_KEYS) | {"request_uuid", "signature"}:
        raise InvalidData("Invalid saved local-file request")
    identifier(value.get("request_uuid"))
    sha256(value.get("signature"))
    return value


def preview(value, expected, root):
    if file_input(value) != file_input(expected) or value.get("root_uuid") != root:
        raise InvalidData("Local-file preview differs from its scope")
    for key in ("signature", "file_signature"):
        sha256(value.get(key))
    for key in ("collection_revision", "root_revision", "size"):
        integer(value.get(key), 1, (1 << 63) - 1)
    integer(value.get("policy_revision"), 0, (1 << 63) - 1)
    source_time(value.get("modified_at"))
    if value.get("filename") != value["relative_path"].split("/")[-1]:
        raise InvalidData("Local-file preview filename differs")
    if value.get("existing_file_uuid"):
        identifier(value["existing_file_uuid"])
    return value


def fingerprint(value):
    return encode([value["file_signature"], value["collection_revision"], value["policy_revision"]])


def status(value, request):
    saved_request(request)
    if not isinstance(value, dict) or any(value.get(key) != item for key, item in request.items()):
        raise InvalidData("Local-file receipt differs from its saved request")
    identifier(value.get("job_uuid"))
    if value.get("state") not in TERMINAL | {"queued", "running"}:
        raise InvalidData("Invalid local-file job state")
    for key in ("revision", "max_attempts"):
        integer(value.get(key), 1, (1 << 63) - 1)
    integer(value.get("attempts"), 0, (1 << 63) - 1)
    for key in ("created_at", "updated_at", "available_at"):
        source_time(value.get(key))
    publication = value.get("publication")
    if (type(value.get("registration_committed")) is not bool or type(value.get("media_ingested")) is not bool
            or value["registration_committed"] != (publication is not None)
            or value["media_ingested"] != (value["state"] == "succeeded")
            or (value["media_ingested"] and not value["registration_committed"])):
        raise InvalidData("Contradictory local-file outcome")
    if publication is not None:
        if not isinstance(publication, dict) or publication.get("media_kind") != request["media_kind"]:
            raise InvalidData("Local-file publication differs from its request")
        for key in ("file_uuid", "content_uuid", "media_uuid"):
            identifier(publication.get(key))
        integer(publication.get("generation"), 1, (1 << 63) - 1)
    if value.get("resume_from_job_uuid"):
        identifier(value["resume_from_job_uuid"])
        integer(value.get("resume_from_revision"), 1, (1 << 63) - 1)
    return value


def page(value, collection, root, folder, after="", signature=""):
    if (not isinstance(value, dict) or value.get("collection_uuid") != collection["uuid"]
            or value.get("root_uuid") != root or value.get("path_prefix") != collection["path_prefix"]
            or value.get("directory") != folder or not includes(collection["path_prefix"], folder)):
        raise InvalidData("Local directory response differs from its scope")
    for key in ("collection_revision", "root_revision"):
        integer(value.get(key), 1, (1 << 63) - 1)
    sha256(value.get("signature"))
    if signature and value["signature"] != signature:
        raise InvalidData("Local directory pagination changed")
    entries = value.get("entries")
    if not isinstance(entries, list) or len(entries) > 50:
        raise InvalidData("Invalid local directory page")
    previous = after
    for item in entries:
        if not isinstance(item, dict) or item.get("kind") not in {"directory", "scene", "image"}:
            raise InvalidData("Invalid local directory entry")
        path = relative_path(item.get("relative_path"))
        name = item.get("name")
        if not isinstance(name, str) or not name or "/" in name or path != (name if folder == "." else folder + "/" + name):
            raise InvalidData("Local directory entry escapes its folder")
        integer(item.get("size"), 0, (1 << 63) - 1)
        if item["kind"] == "directory":
            if item["size"] != 0:
                raise InvalidData("Invalid folder size")
        else:
            source_time(item.get("modified_at"))
        key = ("0/" if item["kind"] == "directory" else "1/") + name
        if key <= previous:
            raise InvalidData("Local directory entries are not strictly ordered")
        previous = key
    if value.get("next_after") and (len(entries) != 50 or value["next_after"] != previous):
        raise InvalidData("Invalid local directory continuation")
    return value


class IntakeClient(ImportClient):
    def __init__(self, endpoint, key_file=None, key_env="STASH_API_KEY"):
        super().__init__(endpoint, key_env)
        self.key_file = key_file

    def request(self, method, suffix, value=None):
        request = Request(self.endpoint + "/api/v3/archive" + suffix, method=method,
                          data=None if value is None else encode(value, 8192),
                          headers={"ApiKey": application_key(self.key_env, self.key_file),
                                   "Content-Type": "application/json", "Accept": "application/json"})
        try:
            with self.opener.open(request, timeout=60) as response:
                if (response.status not in {200, 202} or response.headers.get_content_type() != "application/json"
                        or response.headers.get("Content-Encoding") is not None):
                    raise Unavailable("invalid_local_intake_response")
                return decode(response.read(LIMIT + 1), LIMIT)
        except HTTPError as error:
            try:
                if error.headers.get_content_type() != "application/json" or error.headers.get("Content-Encoding") is not None:
                    raise Unavailable("local_intake_rejected", error.code)
                body = decode(error.read(LIMIT + 1), LIMIT)
                code = body.get("error") if isinstance(body, dict) else None
                allowed = {404: {"not_found", "file_not_found"},
                           409: {"directory_changed", "intake_preview_changed", "intake_request_changed"}}
                raise Unavailable(code if isinstance(code, str) and code in allowed.get(error.code, set()) else "local_intake_rejected", error.code)
            except (InvalidData, HTTPException, OSError):
                raise Unavailable("invalid_local_intake_response", error.code) from None
            finally:
                error.close()
        except (HTTPException, URLError, OSError, TimeoutError):
            raise Unavailable("network_unavailable") from None
        except InvalidData:
            raise Unavailable("invalid_local_intake_response") from None

    def preview(self, input, root):
        return preview(self.request("POST", "/manual-intake/preview", file_input(input)), input, root)

    def apply(self, request):
        return status(self.request("POST", "/manual-intake/apply", saved_request(request)), request)

    def receipt(self, request):
        try:
            return status(self.request("GET", "/manual-intake/requests/" + identifier(request["request_uuid"])), request)
        except Unavailable as error:
            if error.status == 404 and error.code == "not_found":
                return None
            raise

    def directory(self, collection, root, folder, after="", signature=""):
        query = {"directory": folder, "limit": "50"}
        if after:
            query.update(after=after, signature=signature)
        result = self.request("GET", "/collections/" + collection["uuid"] + "/intake-files?" + urlencode(query))
        return page(result, collection, root, folder, after, signature)
