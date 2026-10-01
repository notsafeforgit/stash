"""Real worker/API interoperability; only the website supplies fixture data."""

from contextlib import closing, redirect_stdout
from datetime import datetime, timedelta, timezone
import io
import json
from pathlib import Path
import sys
import time
from unittest.mock import patch
import uuid

from stash_ingest.client import Client, drain_once
from stash_ingest.configuration import Configuration
from stash_ingest.cli import main as producer_cli
from stash_ingest.gallery import NativeDownloadJob
from stash_ingest.outbox import Outbox
from stash_ingest.run_queue import RunQueue
from test_configuration import profile_fixture
from test_gallery import Fixture
from test_producer import reddit_data


def main():
    setup = json.load(sys.stdin)
    directory = Path(setup["directory"])
    path, profile = profile_fixture(directory)
    profile["root"]["uuid"] = setup["root"]
    path.write_text(json.dumps(profile))
    profile = Configuration(path)
    client = Client(setup["endpoint"], setup["producer"])
    now = datetime.now(timezone.utc)
    database = directory / "producer.sqlite"
    ticket = str(uuid.uuid4())
    output = io.StringIO()
    with redirect_stdout(output):
        status = producer_cli(["--outbox", str(database), "--endpoint", setup["endpoint"],
                               "--producer", setup["producer"], "lookup-collections", "--root", setup["root"],
                               "--target", "https://fixture.invalid/account"])
    lookup = json.loads(output.getvalue())
    assert status == 0 and lookup["targets"][0]["state"] == "resolved", lookup
    binding = lookup["targets"][0]["candidates"][0]
    assert binding["collection_uuid"] == setup["collection"]
    with closing(Outbox(database, setup["endpoint"], setup["producer"])) as box:
        queue = RunQueue(box)
        queue.enqueue({"collection_uuid": binding["collection_uuid"], "collection_revision": binding["collection_revision"],
                       "policy_sha256": profile.policy_sha256, "operation": "download", "cooldown_seconds": 0,
                       "window": {"since": None, "until": now.isoformat(timespec="milliseconds")}}, ticket_uuid=ticket)
    # Restart after local admission, before any network request. The dispatcher
    # must submit and discover the work without a run UUID supplied by a caller.
    with closing(Outbox(database, setup["endpoint"], setup["producer"])) as box:
        def job(target, *, producer, lock_directory):
            extractor = Fixture.from_url(target)
            extractor.records, extractor.visited = [reddit_data(date=(now - timedelta(days=1)).isoformat())], []
            task = NativeDownloadJob(extractor, producer=producer, lock_directory=lock_directory)

            def download(url):
                # The independent drainer must deliver while the downloader is
                # still running, through its own SQLite connection.
                row = box.db.execute("SELECT event_uuid FROM events ORDER BY seq LIMIT 1").fetchone()
                assert row is not None
                until = time.monotonic() + 10
                while box.receipt(row[0]) is None and time.monotonic() < until:
                    time.sleep(0.02)
                assert box.receipt(row[0]) is not None, box.status()
                task.pathfmt.part_enable()
                with task.pathfmt.open("wb") as output:
                    output.write(b"fixture download " + url.encode())
                return True

            task.download = download
            return task

        with patch("stash_ingest.gallery.NativeDownloadJob", side_effect=job):
            output = io.StringIO()
            with redirect_stdout(output):
                status = producer_cli(["--outbox", str(database), "--endpoint", setup["endpoint"],
                                       "--producer", setup["producer"], "dispatch", "--profile", str(path)])
        result = json.loads(output.getvalue())
        assert status in (0, 2), (status, result)
        admitted = result["submission"]
        assert admitted["state"] == "admitted", admitted
        assert result["run_uuid"] == admitted["run_uuid"]
        assert result["state"] == "source_succeeded", result
        assert result["finish_recovered"] is True, result
        rows = list(box.db.execute("SELECT event_uuid,kind FROM events ORDER BY seq"))
        assert [row[1] for row in rows] == ["source.capture", "file.completed"], rows
        capture_id, file_id = rows[0][0], rows[1][0]
    # File admission must survive closing/reopening the producer, independently
    # of source completion. Source and file receipts stay separate.
    with closing(Outbox(database, setup["endpoint"], setup["producer"])) as box:
        drain_once(box, client)
        assert box.status()["counts"] == {"pending": 0, "sending": 0, "review": 0, "acknowledged": 2}, box.status()
        assert client.receipt_status(file_id)["state"] == "queued"
        output = io.StringIO()
        with redirect_stdout(output):
            status = producer_cli(["--outbox", str(database), "--endpoint", setup["endpoint"],
                                   "--producer", setup["producer"], "ticket-status", ticket])
        completion = json.loads(output.getvalue())
        assert status == 0 and completion["state"] == "source_succeeded", completion
        assert not completion["remaining"] and not completion["unassigned"], completion
        assert completion["intake_completion"] == "inspect_native_receipts"
        queued = box.receipt(file_id)
        assert queued["capture_uuid"] == box.receipt(capture_id)["capture_uuid"]
        assert result["intake_completion"] == "inspect_native_receipts"
        print(json.dumps({"run_uuid": admitted["run_uuid"], "capture": capture_id, "file": file_id,
                          "path": "Account/postabc123_abc123.jpg"}))


if __name__ == "__main__":
    main()
