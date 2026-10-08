"""Verify retained source admissions without inferring scrape completion."""

from datetime import datetime, timezone
import hashlib
import json
import re

from .receipts import checked_json, identifier, objects
from .storage import HEX, InvalidArchive, decode_json, json_bytes

MAX_REQUEST = 8192
MAX_WINDOWS = 64
FIELDS = ("collection_uuid", "collection_revision", "operation", "policy_sha256", "cooldown_seconds")
STAMP = re.compile(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.(\d{1,9}))?(?:Z|[+-]\d{2}:\d{2})\Z")


def producer_bytes(value):
    # Source requests contain only UUID/hash/enum strings, integer revisions and
    # normalized millisecond timestamps, unlike arbitrary captured source JSON.
    return json.dumps(value, sort_keys=True, ensure_ascii=False, separators=(",", ":"), allow_nan=False).encode()


def window(value):
    if not isinstance(value, dict) or set(value) not in ({"since", "until"}, {"since", "until", "basis"}):
        raise InvalidArchive("Invalid retained source window")
    basis = value.get('basis', '')
    if basis not in ('', 'traversal') or (basis == 'traversal' and value['since'] is not None):
        raise InvalidArchive('Invalid retained source coverage basis')
    def stamp(raw):
        match = STAMP.fullmatch(raw) if isinstance(raw, str) else None
        if match is None or any(c != "0" for c in (match.group(1) or "")[3:]):
            raise InvalidArchive("Source window requires millisecond precision")
        try:
            return datetime.fromisoformat(raw).astimezone(timezone.utc).isoformat(timespec="milliseconds").replace("+00:00", "Z")
        except (ValueError, OverflowError) as error:
            raise InvalidArchive("Invalid source window timestamp") from error
    result = {"since": stamp(value["since"]) if value["since"] is not None else None, "until": stamp(value["until"])}
    if basis:
        result['basis'] = basis
    if (result["until"] == "0001-01-01T00:00:00.000Z"
            or result["since"] is not None and result["since"] >= result["until"]):
        raise InvalidArchive("Source window does not end after its start")
    return result


def template(value):
    if not isinstance(value, dict) or set(value) != set(FIELDS):
        raise InvalidArchive("Invalid retained source request template")
    identifier(value["collection_uuid"])
    if (type(value["collection_revision"]) is not int or not 1 <= value["collection_revision"] <= 2147483647
            or value["operation"] not in ("download", "enrich")
            or not isinstance(value["policy_sha256"], str) or not HEX.fullmatch(value["policy_sha256"])
            or type(value["cooldown_seconds"]) is not int or not 0 <= value["cooldown_seconds"] <= 86400):
        raise InvalidArchive("Invalid retained source request definition")
    return value


def native_request_digest(value):
    """SourceRunRequest's existing Go JSON hash, checked by a shared corpus.

    SQLite source_run_requests stores this normalized typed representation, not
    the raw HTTP digest. Keep its field order and UTC RFC3339Nano timestamps.
    """
    if not isinstance(value, dict) or set(value) != set(FIELDS) | {"request_uuid", "window"}:
        raise InvalidArchive("Invalid source admission request")
    identifier(value["request_uuid"])
    definition = template({key: value[key] for key in FIELDS})
    normalized = window(value["window"])
    if normalized.get('basis') and definition['operation'] != 'download':
        raise InvalidArchive('Retained traversal is not a download request')
    def go_time(stamp):
        if stamp is None:
            return None
        head, fraction = stamp[:-1].split(".")
        fraction = fraction.rstrip("0")
        return head + ("." + fraction if fraction else "") + "Z"
    ordered = {"request_uuid": value["request_uuid"],
               "collection_uuid": definition["collection_uuid"], "collection_revision": definition["collection_revision"],
               "operation": definition["operation"], "policy_sha256": definition["policy_sha256"],
               "window": {"since": go_time(normalized["since"]), "until": go_time(normalized["until"])},
               "cooldown_seconds": definition["cooldown_seconds"]}
    if normalized.get('basis'):
        ordered['window']['basis'] = normalized['basis']
    raw = json.dumps(ordered, ensure_ascii=False, separators=(",", ":"), allow_nan=False).encode()
    return hashlib.sha256(raw).hexdigest()


def retained_template(row):
    value = template(checked_json(row["template"], MAX_REQUEST))
    bases = {'': hashlib.sha256(producer_bytes(value)).hexdigest()}
    if value['operation'] == 'download':
        bases['traversal'] = hashlib.sha256(producer_bytes([value, 'traversal'])).hexdigest()
    basis = next((basis for basis, digest in bases.items() if digest == row['config_sha256']), None)
    if (not isinstance(row["template"], bytes) or producer_bytes(value) != row["template"]
            or basis is None
            or value["operation"] != row["operation"]):
        raise InvalidArchive("Retained source template lost its original identity")
    return value, basis


def verify_run_admissions(library, queue, producer, version):
    counters = {key: 0 for key in ("admitted", "pending", "sending", "review", "accepted_unacknowledged")}
    report = {"intents": 0, "pending_windows": 0, "requests": 0, "maximum_sequence": 0, "counts": counters}
    intent_digest, request_digest = hashlib.sha256(), hashlib.sha256()
    if version == 1:
        if queue.execute("SELECT 1 FROM sqlite_schema WHERE name IN ('run_intents','run_requests') LIMIT 1").fetchone():
            raise InvalidArchive("Producer schema version would omit retained source admission state")
        report.update(intent_boundary_sha256=intent_digest.hexdigest(), request_boundary_sha256=request_digest.hexdigest())
        return report
    for row in objects(queue, """SELECT uuid,config_sha256,operation,window_count,latest_until,
            CASE WHEN length(template)<=8192 THEN template END AS template,
            CASE WHEN length(windows)<=16384 THEN windows END AS windows FROM run_intents ORDER BY uuid"""):
        identifier(row["uuid"])
        _, basis = retained_template(row)
        if not isinstance(row["windows"], bytes):
            raise InvalidArchive("Source intent has no bounded retained windows")
        pending = decode_json(row["windows"])
        if not isinstance(pending, list) or len(pending) > MAX_WINDOWS or len(pending) != row["window_count"]:
            raise InvalidArchive("Source intent window count differs from its retained work")
        previous = None
        for item in pending:
            if (item != window(item) or item.get('basis', '') != basis
                    or previous is not None and (item["since"] is None or item["since"] <= previous)):
                raise InvalidArchive("Source intent windows are not normalized and disjoint")
            previous = item["until"]
        if row["latest_until"] != (previous or ""):
            raise InvalidArchive("Source intent lost its latest window")
        report["intents"] += 1
        report["pending_windows"] += len(pending)
        intent_digest.update(json_bytes({"uuid":row["uuid"], "config_sha256":row["config_sha256"],
                                         "windows_sha256":hashlib.sha256(row["windows"]).hexdigest()}))
    query = """SELECT r.seq,r.request_uuid,r.intent_uuid,r.sha256,r.state,r.run_uuid,r.until_stamp,
        length(r.body) AS body_length,length(r.receipt) AS receipt_length,
        CASE WHEN length(r."window")<=8192 THEN r."window" END AS window_body,
        CASE WHEN length(r.body)<=8192 THEN r.body END AS body,
        CASE WHEN length(r.receipt)<=65536 THEN r.receipt END AS receipt,
        g.config_sha256,g.operation,CASE WHEN length(g.template)<=8192 THEN g.template END AS template
        FROM run_requests r LEFT JOIN run_intents g ON g.uuid=r.intent_uuid ORDER BY r.seq"""
    for row in objects(queue, query):
        if (type(row["seq"]) is not int or row["seq"] <= report["maximum_sequence"]
                or row["state"] not in ("admitted", "pending", "sending", "review")
                or row["body_length"] is not None and row["body_length"] > MAX_REQUEST
                or row["receipt_length"] is not None and row["receipt_length"] > 65536):
            raise InvalidArchive("Invalid retained source request sequence, size or state")
        identifier(row["request_uuid"])
        identifier(row["intent_uuid"])
        selected_window = checked_json(row["window_body"], MAX_REQUEST)
        if selected_window != window(selected_window) or row["until_stamp"] != selected_window["until"]:
            raise InvalidArchive("Source request lost its original normalized window")
        definition, basis = retained_template(row)
        if selected_window.get('basis', '') != basis:
            raise InvalidArchive('Source request changed its retained coverage basis')
        request = dict(definition, request_uuid=row["request_uuid"], window=selected_window)
        raw = producer_bytes(request)
        if hashlib.sha256(raw).hexdigest() != row["sha256"]:
            raise InvalidArchive("Source request cannot be reconstructed from its retained template and window")
        native_hash = native_request_digest(request)
        native = list(objects(library, """SELECT r.digest,r.run_uuid,j.uuid AS native_run_uuid,
            j.collection_uuid,j.collection_revision,j.operation,j.policy_sha256,j.cooldown_seconds,j.root_uuid,j.root_revision
            FROM source_run_requests r LEFT JOIN source_runs j ON j.uuid=r.run_uuid
            WHERE r.producer_uuid=? AND r.request_uuid=?""", (producer, row["request_uuid"])))
        if len(native) > 1:
            raise InvalidArchive("Duplicate native source admission")
        server = native[0] if native else None
        if server is not None:
            if (server["digest"] != native_hash or server["run_uuid"] != server["native_run_uuid"]
                    or json_bytes({key:server[key] for key in FIELDS}) != json_bytes({key:request[key] for key in FIELDS})):
                raise InvalidArchive("Native source admission does not match the original request")
            identifier(server["run_uuid"])
        if row["state"] == "admitted":
            if server is None:
                raise InvalidArchive("Admitted producer request is missing from the native snapshot")
            receipt = checked_json(row["receipt"], 65536)
            expected = {key: server[key] for key in FIELDS + ("root_uuid", "root_revision")}
            expected.update(request_uuid=row["request_uuid"], uuid=server["run_uuid"])
            if (row["body"] is not None or row["run_uuid"] != server["run_uuid"]
                    or json_bytes({key:receipt.get(key) for key in expected}) != json_bytes(expected)
                    or receipt.get("state") not in ("queued", "running", "succeeded", "deferred", "cancelled")):
                raise InvalidArchive("Retained source admission identifies another request or run")
            evidence = {"receipt":receipt}
        else:
            if row["body"] != raw or row["receipt"] is not None or row["run_uuid"] is not None:
                raise InvalidArchive("Unadmitted source request lost its exact original bytes")
            if server is not None:
                counters["accepted_unacknowledged"] += 1
            evidence = {"payload_sha256":row["sha256"]}
        counters[row["state"]] += 1
        report["requests"] += 1
        report["maximum_sequence"] = row["seq"]
        request_digest.update(json_bytes(dict(evidence, seq=row["seq"], request_uuid=row["request_uuid"], state=row["state"],
                                             native_request_sha256=native_hash, native_run_uuid=server["run_uuid"] if server else None)))
    report.update(intent_boundary_sha256=intent_digest.hexdigest(), request_boundary_sha256=request_digest.hexdigest())
    return report
