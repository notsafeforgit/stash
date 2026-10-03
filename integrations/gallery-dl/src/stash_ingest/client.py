"""Header-only Stash API access; no website credentials or redirect following."""

import math
import os
import re
import time
from http.client import HTTPException
from urllib.error import HTTPError, URLError
from urllib.request import HTTPRedirectHandler, ProxyHandler, Request, build_opener
import uuid

from .encoding import InvalidData, MAX_BATCH_BYTES, MAX_EVENT_BYTES, decode, encode, identifier
from .endpoint import origin
from .outbox import Conflict, LeaseLost
from .retention import POLICY


class Unavailable(RuntimeError):
    def __init__(self, code, status=None, retry_after=0):
        super().__init__(code)
        self.code, self.status, self.retry_after = code, status, retry_after


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class Client:
    def __init__(self, endpoint, producer, *, token_env="STASH_INGEST_TOKEN", timeout=15):
        self.endpoint, self.producer = origin(endpoint), identifier(producer)
        if (not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", token_env)
                or not isinstance(timeout, (int, float)) or not math.isfinite(timeout)
                or not 0 < timeout <= 30):
            raise InvalidData("Invalid API token reference or request timeout")
        self.token_env, self.timeout = token_env, timeout
        # The runner connects to its configured Stash origin directly. Never
        # inherit a site's proxy, cookie jar, netrc or authentication handler.
        self.opener = build_opener(ProxyHandler({}), NoRedirect())

    def _request(self, method, route, body=None, *, timed=False, allow_empty=False, max_response_bytes=1 << 20,
                 preserve_numbers=False):
        if type(max_response_bytes) is not int or not 1 <= max_response_bytes <= (32 << 20) + 4096:
            raise InvalidData("Invalid Stash response size limit")
        token = os.environ.get(self.token_env, "")
        if not token or any(ord(c) <= 32 or ord(c) >= 127 for c in token):
            raise Unavailable("stash_token_missing")
        request = Request(self.endpoint + "/api/v3/ingest" + route, data=body,
                          method=method, headers={"Authorization": "Bearer " + token,
                          "Accept": "application/json", "Content-Type": "application/json"})
        try:
            started = time.monotonic()
            with self.opener.open(request, timeout=self.timeout) as response:
                server_date = response.headers.get("Date")
                if allow_empty and response.status == 204:
                    return (None, server_date, started) if timed else None
                if (response.status != 200 or response.headers.get_content_type() != "application/json"
                        or response.headers.get("Content-Encoding") is not None):
                    raise Unavailable("invalid_response")
                raw = response.read(max_response_bytes + 1)
            try:
                data = decode(raw, max_response_bytes, preserve_numbers=preserve_numbers)
                return (data, server_date, started) if timed else data
            except InvalidData:
                raise Unavailable("invalid_response") from None
        except HTTPError as exc:
            status = exc.code
            retry_after = exc.headers.get("Retry-After", "")
            delay = min(int(retry_after), 86400) if re.fullmatch(r"[0-9]{1,8}", retry_after) else 0
            exc.close()
            # Do not echo response bodies or exception strings containing URLs.
            code = {401: "stash_token_rejected", 403: "outside_scope", 429: "queue_full"}.get(status, "http_error")
            raise Unavailable(code, status, delay) from None
        except (HTTPException, URLError, OSError, TimeoutError):
            raise Unavailable("network_unavailable") from None

    def capabilities(self):
        data = self._request("GET", "/capabilities")
        if (not isinstance(data, dict) or data.get("protocol") != 1
                or data.get("producer_uuid") != self.producer
                or data.get("retention_policy") != POLICY
                or not isinstance(data.get("kinds"), list)
                or "source.capture" not in data["kinds"]
                or type(data.get("file_ingestion")) is not bool):
            raise Unavailable("incompatible_server")
        for key, minimum in (("max_event_bytes", MAX_EVENT_BYTES),
                             ("max_batch_bytes", MAX_BATCH_BYTES), ("max_batch_events", 8)):
            if type(data.get(key)) is not int or data[key] < minimum:
                raise Unavailable("incompatible_server")
        return data

    def batch(self, deliveries):
        if not 1 <= len(deliveries) <= 8:
            raise InvalidData("Expected between one and eight events")
        # Inner bytes must not be decoded/re-encoded: receipts bind the exact
        # bytes saved before network I/O, including the JSON number spellings.
        body = b'{"events":[' + b','.join(
            b'{"sha256":' + encode(d.sha256) + b',"event":' + d.body + b'}'
            for d in deliveries) + b']}'
        if len(body) > MAX_BATCH_BYTES:
            raise InvalidData("Batch exceeds protocol limit")
        data = self._request("POST", "/batches", body)
        results = data.get("results") if isinstance(data, dict) else None
        if not isinstance(results, list) or len(results) != len(deliveries):
            raise Unavailable("invalid_response")
        indexed = {}
        for result in results:
            if (not isinstance(result, dict) or type(result.get("index")) is not int
                    or not 0 <= result["index"] < len(deliveries) or result["index"] in indexed
                    or type(result.get("status")) is not int or not 200 <= result["status"] <= 599):
                raise Unavailable("invalid_response")
            indexed[result["index"]] = result
        return [indexed[i] for i in range(len(deliveries))]

    def receipt(self, event_uuid):
        return self._request("GET", "/receipts/" + identifier(event_uuid))

    def receipt_status(self, event_uuid):
        return self._request("GET", "/receipts/" + identifier(event_uuid) + "/status")


def drain_once(outbox, client, *, owner=None):
    if (outbox.endpoint, outbox.producer) != (client.endpoint, client.producer):
        raise Conflict("Client and outbox identify different Stash producers")
    deliveries = outbox.claim(owner or str(uuid.uuid4()), seconds=120)
    if not deliveries:
        return {"acknowledged": 0, "retried": 0, "review": 0, "lease_lost": 0}
    counts = {"acknowledged": 0, "retried": 0, "review": 0, "lease_lost": 0}

    def fail(delivery, code, review=False, delay=0):
        try:
            outbox.fail(delivery, code, review=review, retry_after=delay)
            counts["review" if review else "retried"] += 1
        except LeaseLost:
            counts["lease_lost"] += 1

    def acknowledge(delivery, receipt):
        try:
            outbox.acknowledge(delivery, receipt)
            counts["acknowledged"] += 1
        except LeaseLost:
            counts["lease_lost"] += 1
        except InvalidData:
            fail(delivery, "receipt_mismatch", True)

    try:
        capabilities = client.capabilities()
        # The server replays known file receipts even with processing disabled.
        # One batch keeps the request deadline safely inside the delivery lease.
        results = client.batch(deliveries)
    except Unavailable as exc:
        for delivery in deliveries:
            fail(delivery, exc.code, exc.status in {400, 403, 404, 409, 413, 422}, exc.retry_after)
        return counts
    for delivery, result in zip(deliveries, results):
        status = result["status"]
        expected = 200 if decode(delivery.body)["kind"] == "source.capture" else 202
        if status == expected and result.get("receipt") is not None:
            acknowledge(delivery, result["receipt"])
        elif status == 422 and expected == 202 and not capabilities["file_ingestion"]:
            fail(delivery, "file_processor_unavailable", delay=60)
        elif status in {400, 403, 404, 409, 413, 422}:
            fail(delivery, "event_rejected_" + str(status), True)
        else:
            fail(delivery, "stash_token_rejected" if status == 401 else "delivery_unresolved")
    return counts
