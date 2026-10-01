import uuid

from stash_ingest.encoding import digest, encode
from stash_ingest.retention import POLICY, retain

PRODUCER = "8f063ec8-f5ae-422b-95f3-af0b0bb80386"
COLLECTION = "fe9f28b5-5ed1-4f49-b183-42e77b673b45"
ROOT = "011ceaf7-c498-4426-a4e7-6b7a559d032a"
RUN = "8276d4d9-0727-42cd-9e6b-5734d50a77a7"
NOW = "2026-10-01T12:00:00+00:00"


def capture(**changes):
    event = {"protocol": 1, "producer_uuid": PRODUCER, "event_uuid": str(uuid.uuid4()),
             "collection_uuid": COLLECTION, "collection_revision": 1, "run_uuid": RUN,
             "root_uuid": ROOT, "kind": "source.capture", "observed_at": NOW,
             "extractor_version": "fixture", "retention_policy": POLICY,
             "post": {"namespace": "native:twitter", "value": "9007199254740993"},
             "metadata": {"title": "Café <sample>"},
             "source": retain({"category": "twitter", "tweet_id": 9007199254740993,
                               "author": {"id": "99", "name": "Example"},
                               "extended_entities": {"media": [{"id_str": "101", "type": "photo"}]}})}
    event.update(changes)
    return event


def file_event(parent=None, **changes):
    event = {k: v for k, v in capture().items() if k in {"protocol", "producer_uuid", "event_uuid",
             "collection_uuid", "collection_revision", "run_uuid", "root_uuid", "kind", "observed_at"}}
    event.update(kind="file.completed", relative_path="Fixture/actual-final.mkv", size=32,
                 sha256="a" * 64, media_kind="scene")
    if parent:
        event["source"] = {"capture_event_uuid": parent["event_uuid"],
                           "attachment": {"namespace": "native:twitter:media", "value": "101"}}
    event.update(changes)
    return event


def receipt(event):
    result = {k: event[k] for k in ("producer_uuid", "event_uuid", "collection_uuid", "collection_revision",
                                  "run_uuid", "root_uuid", "kind")}
    result.update(sha256=digest(encode(event)), credential_uuid=str(uuid.uuid4()),
                  committed_at=NOW, result={"status": "committed"})
    if event["kind"] == "source.capture":
        result.update(post_uuid=str(uuid.uuid4()), capture_uuid=str(uuid.uuid4()))
    else:
        result.update(job_uuid=str(uuid.uuid4()), result={"status": "queued"})
    return result
