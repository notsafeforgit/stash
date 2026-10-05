"""Client-mediated filesystem capture while the native writer guard is held.

The trusted caller supplies the provider. This transport never executes a
server-selected command or treats a receipt as proof of arbitrary filesystem
claims. Providers must create and validate their actual immutable views.
"""

from datetime import datetime, timezone
import uuid

from .storage import InvalidArchive, decode_json

FORMAT = "org.notsafeforgit.stash.filesystem-checkpoint"
MAX_DETAILS = 64 << 10
MAX_RECORD = MAX_DETAILS + 4096


def timestamp(value):
    try:
        result = datetime.fromisoformat(value)
        if result.tzinfo is None:
            raise ValueError()
        return result
    except (ValueError, TypeError):
        raise InvalidArchive("Invalid filesystem checkpoint timestamp") from None


def canonical_uuid(value):
    try:
        return isinstance(value, str) and str(uuid.UUID(value)) == value
    except ValueError:
        return False


def validate_record(record, client, request_hash):
    from .server_checkpoint import request_bytes
    fields = {"format", "version", "uuid", "request_sha256", "token", "confirmed_at", "details"}
    if (not isinstance(record, dict) or set(record) != fields or record["format"] != FORMAT
            or type(record["version"]) is not int or record["version"] != 1
            or record["uuid"] != client.request_id or record["request_sha256"] != request_hash
            or not canonical_uuid(record["token"]) or not isinstance(record["details"], dict)
            or not record["details"] or len(request_bytes(record["details"])) > MAX_DETAILS):
        raise InvalidArchive("Filesystem checkpoint receipt does not match the native capture")
    timestamp(record["confirmed_at"])
    return record


def read_event(response, limit):
    body = response.readline(limit + 1)
    if len(body) > limit or not body.endswith(b"\n"):
        raise InvalidArchive("Incomplete or oversized native checkpoint event")
    event = decode_json(body)
    if not isinstance(event, dict):
        raise InvalidArchive("Invalid native checkpoint event")
    if event.get("event") == "error":
        raise InvalidArchive("Native checkpoint failed during coordinated filesystem capture")
    return event


def read_checkpoint_response(client, response, request_hash):
    from .server_checkpoint import request_bytes
    kind = response.headers.get_content_type()
    if kind == "application/json":
        body = response.read((1 << 20) + 1)
        if len(body) > 1 << 20:
            raise InvalidArchive("Native checkpoint manifest exceeds its size limit")
        return body
    if kind != "application/x-ndjson" or client.boundary is None:
        raise InvalidArchive("Unexpected native checkpoint response format")
    event = read_event(response, 4096)
    if set(event) != {"event", "ready"} or event["event"] != "boundary_ready":
        raise InvalidArchive("Expected a filesystem checkpoint challenge")
    ready = event["ready"]
    if (not isinstance(ready, dict) or set(ready) != {"uuid", "token", "request_sha256", "expires_at"}
            or ready["uuid"] != client.request_id or ready["request_sha256"] != request_hash
            or not canonical_uuid(ready["token"])):
        raise InvalidArchive("Filesystem checkpoint challenge does not match the request")
    if timestamp(ready["expires_at"]) <= datetime.now(timezone.utc):
        raise InvalidArchive("Filesystem checkpoint challenge has expired")
    # Copy the challenge so a provider cannot change the acknowledgement token.
    details = client.boundary(dict(ready))
    if not isinstance(details, dict) or not details or len(request_bytes(details)) > MAX_DETAILS:
        raise InvalidArchive("Filesystem checkpoint provider returned invalid evidence")
    confirmation = {"token": ready["token"], "details": details}
    with client.open(f"/{client.request_id}/boundary", request_bytes(confirmation)) as acknowledgement:
        if acknowledgement.headers.get_content_type() != "application/json":
            raise InvalidArchive("Filesystem checkpoint confirmation is not JSON")
        body = acknowledgement.read(MAX_RECORD + 1)
    if len(body) > MAX_RECORD:
        raise InvalidArchive("Filesystem checkpoint receipt exceeds its size limit")
    record = validate_record(decode_json(body), client, request_hash)
    if record["token"] != ready["token"] or record["details"] != details:
        raise InvalidArchive("Filesystem checkpoint confirmation changed the provider evidence")
    client.boundary_receipt = record
    event = read_event(response, (1 << 20) + 4096)
    if set(event) != {"event", "checkpoint"} or event["event"] != "sealed" or response.read(1):
        raise InvalidArchive("Native checkpoint did not finish with one sealed manifest")
    return request_bytes(event["checkpoint"])
