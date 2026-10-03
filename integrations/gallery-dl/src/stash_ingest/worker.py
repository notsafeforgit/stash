"""Execute one fenced native source attempt; intake completion is separate."""

import sqlite3
import threading

from .client import Client, Unavailable, drain_once
from .encoding import InvalidData, encode, identifier
from .outbox import Capacity, Conflict, Outbox
from .producer import Producer
from .runs import RunLease, SourceFailure, SourcePaused, SourceTurnComplete


class Delivery:
    """The drainer owns its SQLite connection and respects durable retry times."""

    def __init__(self, box, client):
        self.path, self.endpoint, self.producer = box.path, box.endpoint, box.producer
        self.client = Client(client.endpoint, client.producer, token_env=client.token_env, timeout=client.timeout)
        self.stop = threading.Event()
        self.thread = None
        self.failure = None

    def start(self):
        def deliver():
            box = None
            try:
                box = Outbox(self.path, self.endpoint, self.producer)
                while not self.stop.is_set():
                    result = drain_once(box, self.client)
                    self.stop.wait(0.1 if result["acknowledged"] else 2)
            except Exception:
                # A failed background thread must become visible at the next
                # source boundary without printing response bodies or secrets.
                self.failure = "outbox_delivery_failed"
            finally:
                if box is not None:
                    box.close()
        self.thread = threading.Thread(target=deliver, name="stash-outbox-delivery", daemon=True)
        self.thread.start()

    def check(self):
        if self.failure:
            raise InvalidData("Producer delivery stopped; queued evidence remains durable")

    def close(self):
        self.stop.set()
        if self.thread is not None:
            self.thread.join(min(60, self.client.timeout * 2 + 2))
            if self.thread.is_alive():
                self.failure = "delivery_still_pending"


def _confirmed_attempt(client, lease, outcome, error_code, error_scope):
    """Recover a committed finish whose HTTP response was lost."""
    attempts = client._request("POST", "/runs/" + lease.run_uuid + "/attempts",
                               encode({"after": lease.run["fence"] - 1}))
    if not isinstance(attempts, list):
        return False
    return any(isinstance(attempt, dict) and attempt.get("run_uuid") == lease.run_uuid
               and attempt.get("producer_uuid") == client.producer and attempt.get("owner_uuid") == lease.owner
               and attempt.get("fence") == lease.run["fence"] and attempt.get("outcome") == outcome
               and attempt.get("error_code", "") == error_code and attempt.get("error_scope", "") == error_scope
               and attempt.get("ended_at") for attempt in attempts)


def execute(box, client, configuration, run_uuid):
    """Never launch source work without a matching policy and live claim."""
    from gallery_dl import config, exception
    from .gallery import NativeDownloadJob, SUPPORTED_VERSION

    identifier(run_uuid)
    if (box.endpoint, box.producer) != (client.endpoint, client.producer):
        raise Conflict("Worker and outbox identify different Stash producers")
    configuration.check()
    capabilities = client.capabilities()
    if (capabilities.get("source_runs") is not True or capabilities.get("source_run_protocol") != 1
            or capabilities.get("source_run_recovery_protocol") != 1
            or capabilities.get("source_run_pacing_protocol") != 1
            or capabilities.get("source_run_fairness_protocol") != 1
            or capabilities.get("file_ingestion") is not True):
        raise Unavailable("native_download_worker_unavailable")
    current = client._request("GET", "/runs/" + run_uuid)
    if (not isinstance(current, dict) or current.get("uuid") != run_uuid
            or current.get("policy_sha256") != configuration.policy_sha256
            or current.get("root_uuid") != configuration.root_uuid or current.get("operation") != "download"):
        raise Conflict("Run does not match this worker's reviewed configuration and root")
    with configuration.activate():
        # The executable reports JSON on stdout; downloader progress belongs in
        # the caller's log stream. This does not affect retained source evidence.
        config.set(("output",), "mode", "null")
        delivery = Delivery(box, client)
        lease = RunLease.claim(client, run_uuid, configuration.policy_sha256)
        if lease is None:
            return {"state": "waiting", "run_uuid": run_uuid}
        outcome, error, result = "deferred", "worker_execution_failed", None
        error_scope = ""
        try:
            if lease.run.get("root_uuid") != configuration.root_uuid or lease.run.get("operation") != "download":
                raise InvalidData("Claimed run changed its worker root or operation")
            lease.start()
            delivery.start()

            def check_configuration():
                configuration.check()
                delivery.check()

            producer = Producer(box, lease, configuration.root, extractor_version=SUPPORTED_VERSION,
                                configuration_check=check_configuration, source_category=configuration.source_category)
            task = NativeDownloadJob(lease.run["target_url"], producer=producer, lock_directory=configuration.locks.path)
            status = task.run()
            producer.check()
            if status == 0:
                outcome, error = "succeeded", ""
            elif producer.failure_code == "source_rejected" or status & (8 | 16 | 32):
                outcome, error = "deferred", producer.failure_code or "source_access_or_configuration"
            else:
                outcome, error = "retry", producer.failure_code or "source_download_failed"
        except SourceTurnComplete:
            outcome, error = "retry", "source_turn_complete"
        except SourceFailure as exc:
            outcome = "deferred" if exc.code in {"authentication", "access_denied", "challenge", "not_found"} else "retry"
            error, error_scope = exc.code, exc.scope
        except SourcePaused:
            result = {"state": "paused", "run_uuid": run_uuid, "error_code": "source_ownership_unavailable"}
        except Capacity:
            outcome, error = "retry", "outbox_capacity"
        except (InvalidData, exception.GalleryDLException):
            outcome, error = "deferred", "worker_configuration_or_source"
        except (OSError, sqlite3.Error):
            outcome, error = "retry", "worker_storage_unavailable"
        except (KeyboardInterrupt, SystemExit):
            outcome, error = "retry", "worker_interrupted"
        except Exception:
            outcome, error = "retry", "worker_execution_failed"
        finally:
            try:
                if result is None:
                    try:
                        failure = {"error_code": error}
                        if error_scope:
                            failure["error_scope"] = error_scope
                        finished = lease.finish(outcome, **failure)
                        result = {"state": "source_succeeded" if outcome == "succeeded" else outcome,
                                  "run_uuid": run_uuid, "run_state": finished["state"], "error_code": error}
                    except (Unavailable, SourcePaused, InvalidData):
                        try:
                            confirmed = _confirmed_attempt(client, lease, outcome, error, error_scope)
                        except (Unavailable, InvalidData):
                            confirmed = False
                        result = {"state": ("source_succeeded" if outcome == "succeeded" else outcome) if confirmed
                                  else "completion_unconfirmed", "run_uuid": run_uuid, "error_code": error,
                                  "finish_recovered": bool(confirmed)}
            finally:
                delivery.close()
                try:
                    lease.close()
                except SourcePaused:
                    if result is not None:
                        result["heartbeat_error"] = "heartbeat_still_stopping"
        result["outbox"] = box.status()
        if result["state"] == "retry" and error == "source_turn_complete":
            result["state"] = "yielded"
        if error_scope:
            result["error_scope"] = error_scope
        result["intake_completion"] = "inspect_native_receipts"
        if delivery.failure:
            result["delivery_error"] = delivery.failure
        return result
