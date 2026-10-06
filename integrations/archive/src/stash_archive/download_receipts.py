"""Verify retained transfer reports without treating them as media availability."""

from .receipts import checked_json, identifier, objects
from .storage import InvalidArchive, json_bytes


def one(connection, query, parameters, message):
    rows = list(objects(connection, query, parameters))
    if len(rows) != 1:
        raise InvalidArchive(message)
    return rows[0]


def verify_download_report(library, queue, producer, row, event, server):
    if queue.execute("PRAGMA user_version").fetchone()[0] < 16:
        raise InvalidArchive("Download report requires producer snapshot version 16")
    report = None
    if server is not None:
        if library.execute("SELECT name FROM sqlite_schema WHERE type='table' AND name='source_attachment_downloads'").fetchall() != [("source_attachment_downloads",)]:
            raise InvalidArchive("Native snapshot is missing its download reports")
        report = one(library, "SELECT * FROM source_attachment_downloads WHERE producer_uuid=? AND event_uuid=?",
                     (producer, row["event_uuid"]), "Native download receipt is missing its report")
        result = checked_json(server["result"], 16384)
        expected = {"status": "recorded", "attachment_uuid": identifier(report["attachment_uuid"]),
                    "reported_state": report["state"], "media_ingested": False}
        if (json_bytes(result) != json_bytes(expected) or server["job_uuid"] is not None
                or report["run_uuid"] != row["run_uuid"]):
            raise InvalidArchive("Native download receipt differs from its original report")
    if event is not None:
        if row["body_length"] > 16384:
            raise InvalidArchive("Oversized retained download report")
        if report is not None:
            keys = ("capture_event_uuid", "owner_uuid", "fence", "transfer_sequence", "state", "file_event_uuid")
            if (json_bytes({k: event.get(k) for k in keys}) != json_bytes({k: report[k] for k in keys})
                    or event.get("reason_code", "") != report["reason_code"]):
                raise InvalidArchive("Pending download report differs from native history")
        source = event
    else:
        source = report
    capture_id = identifier(source.get("capture_event_uuid"))
    state = source.get("state")
    if state not in ("started", "downloaded", "failed", "excluded", "skipped"):
        raise InvalidArchive("Invalid retained download state")
    file_id = source.get("file_event_uuid")
    if (state == "downloaded") != (file_id is not None):
        raise InvalidArchive("Download report lost its file receipt association")
    keys = ("run_uuid", "collection_uuid", "collection_revision", "root_uuid")

    def parent(event_id, kind):
        identifier(event_id)
        saved = one(queue, "SELECT seq,kind,run_uuid,collection_uuid,collection_revision,root_uuid,parent_uuid FROM events WHERE event_uuid=?",
                    (event_id,), "Download report lost its queued dependency")
        if (saved["kind"] != kind or saved["seq"] >= row["seq"]
                or row["root_uuid"] is None or any(saved[k] != row[k] for k in keys)):
            raise InvalidArchive("Download report dependency belongs to another source attempt")
        return saved

    parent(capture_id, "source.capture")
    if file_id is not None and parent(file_id, "file.completed")["parent_uuid"] != capture_id:
        raise InvalidArchive("Downloaded file belongs to another source capture")
    if row["parent_uuid"] != (file_id or capture_id):
        raise InvalidArchive("Download report lost its queued parent")
    if report is not None:
        capture = one(library, "SELECT * FROM ingest_receipts WHERE producer_uuid=? AND event_uuid=?",
                      (producer, capture_id), "Download report is missing its native capture receipt")
        if (capture["kind"] != "source.capture"
                or any(capture[k] != server[k] for k in (*keys, "post_uuid", "capture_uuid"))):
            raise InvalidArchive("Download report differs from its original native capture")
