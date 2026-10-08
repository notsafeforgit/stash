"""Real worker/API interoperability; only the website supplies fixture data."""

from contextlib import closing, nullcontext, redirect_stdout
from datetime import datetime, timedelta, timezone
import io
import json
from pathlib import Path
import re
import subprocess
import sys
import time
from unittest.mock import patch
from types import SimpleNamespace
import uuid

from gallery_dl import ytdl
from gallery_dl.extractor.ytdl import YoutubeDLExtractor
from gallery_dl.extractor.chevereto import CheveretoFileExtractor
from gallery_dl.extractor.leakgallery import LeakgalleryPostExtractor
from test_web_media import TumblrFixture
from yt_dlp.extractor.common import InfoExtractor

from stash_ingest.client import Client, drain_once
from stash_ingest.backfill_calls import BackfillCalls, advance_once
from stash_ingest.configuration import Configuration
from stash_ingest.cli import main as producer_cli
from stash_ingest.gallery import NativeDownloadJob
from stash_ingest.outbox import Outbox
from stash_ingest.n8n_runner import main as n8n_cli
from stash_ingest.source_calls import SourceCalls
from test_configuration import profile_fixture
from test_gallery import Fixture
from test_producer import reddit_data


def main():
    setup = json.load(sys.stdin)
    directory = Path(setup["directory"])
    path, profile = profile_fixture(directory)
    profile["root"]["uuid"] = setup["root"]
    native_ytdl = setup['adapter'] in ('ytdl', 'ytdl-traversal')
    native_web = setup['adapter'] in ('tumblr', 'jpgfish', 'leakgallery')
    traversal = setup['adapter'] == 'ytdl-traversal' or native_web
    if traversal:
        profile['source_mode'] = 'traversal'
    if native_ytdl:
        profile['source_category'] = 'ytdl-generic'
        profile['gallery']['extractor']['filename'] = '{id}.{extension}'
        profile['gallery']['extractor']['ytdl'] = {'module': 'yt_dlp', 'logging': False,
                                                 'raw-options': {'quiet': True, 'no_warnings': True}}
    elif native_web:
        profile["source_category"] = setup["adapter"]
        profile["gallery"]["extractor"]["filename"] = "web.{extension}"
    elif setup["adapter"] != "caller-cli":
        profile["source_category"] = "reddit"
    native_backfill = setup["adapter"] == "n8n-backfill"
    if native_backfill:
        profile["gallery"]["skip"] = True
    path.write_text(json.dumps(profile))
    profile = Configuration(path)
    client = Client(setup["endpoint"], setup["producer"])
    now = datetime.now(timezone.utc)
    database = directory / "producer.sqlite"
    call_uuid = str(uuid.uuid4())
    backfill_uuid = str(uuid.uuid4())
    if native_backfill:
        call_uuid = str(uuid.uuid5(uuid.UUID(backfill_uuid), "sources"))
    ticket = str(uuid.uuid5(uuid.UUID(call_uuid), "source/1"))
    targets = directory / "caller-targets.txt"
    targets.write_text(setup["target"] + "\n")
    command = ["--outbox", str(database), "--endpoint", setup["endpoint"], "--producer", setup["producer"],
               "queue-sources", "--call", call_uuid, "--targets-file", str(targets), "--profile", str(path),
               "--until", now.isoformat(timespec="milliseconds")]
    if setup["adapter"] == "host-launcher":
        command = [sys.executable, str(Path(__file__).resolve().parents[1] / "bin/update-reddit-media"),
                   "--outbox", str(database), "--endpoint", setup["endpoint"], "--producer", setup["producer"],
                   "--call", call_uuid, "--config-file", str(targets), "--profile", str(path),
                   "--until", now.isoformat(timespec="milliseconds")]
    if native_backfill:
        command = [sys.executable, "-m", "stash_ingest.n8n_runner", "--outbox", str(database), "--endpoint", setup["endpoint"],
                   "--producer", setup["producer"], "--call", backfill_uuid, "--mode", "reddit-profile-new",
                   "--identity", "Native_Fixture", "--profile", str(path), "--until", now.isoformat(timespec="milliseconds")]

    def record():
        if setup["adapter"] not in ('caller-cli', 'ytdl', 'ytdl-traversal', 'tumblr', 'jpgfish', 'leakgallery'):
            result = subprocess.run(command, capture_output=True, text=True, timeout=30)
            assert result.returncode == 0, result.stderr
            value = json.loads(result.stdout)
            assert value["state"] == "recorded"
            return value if native_backfill else value["call"]
        output = io.StringIO()
        with redirect_stdout(output):
            assert producer_cli(command) == 0
        return json.loads(output.getvalue())

    recorded = record()
    if native_backfill:
        assert recorded["token"] == uuid.UUID(backfill_uuid).hex, recorded
        with closing(Outbox(database, setup["endpoint"], setup["producer"])) as box:
            assert SourceCalls(box).summary()["calls"] == 0
        output = io.StringIO()
        with redirect_stdout(output):
            pending_code = n8n_cli(["--outbox", str(database), "--endpoint", setup["endpoint"], "--producer", setup["producer"],
                                   "--inspect", recorded["token"], "--strict"])
        assert pending_code == 2 and json.loads(output.getvalue())["backfill_pending"] is True, output.getvalue()
    else:
        assert recorded["counts"]["pending"] == 1, recorded
    targets.unlink()
    # A lost command response reopens the original snapshot, even if the list
    # was removed or edited while the server remained unreachable.
    assert record() == recorded
    # Restart after local admission, before any network request. The dispatcher
    # must submit and discover the work without a run UUID supplied by a caller.
    with closing(Outbox(database, setup["endpoint"], setup["producer"])) as box:
        class NativeVideoFixtureIE(InfoExtractor):
            _VALID_URL = r'https://fixture\.invalid/video/(?P<id>one)'

            def _real_extract(self, url):
                return {'id': 'one', 'title': 'Resolved video', 'uploader_id': 'publisher',
                        **({} if traversal else {'timestamp': int((now - timedelta(days=1)).timestamp())}),
                        'ext': 'mp4', 'url': 'https://cdn.invalid/video.mp4'}

        original_factory = ytdl.construct_YoutubeDL

        def factory(*args, **kwargs):
            client = original_factory(*args, **kwargs)
            client.add_info_extractor(NativeVideoFixtureIE())
            return client

        def job(target, *, producer, lock_directory):
            class CallerFixture(Fixture):
                pattern = re.escape(setup["target"])
            if native_ytdl:
                extractor = YoutubeDLExtractor.from_url('ytdl:' + target)
                extractor.url = target
                extractor.ytdl_ie_key = 'NativeVideoFixture'
            elif setup['adapter'] == 'tumblr':
                extractor = TumblrFixture.from_url(target)
                extractor.records = [{'id': 123, 'type': 'photo', 'timestamp': int((now - timedelta(days=1)).timestamp()),
                                      'blog_name': 'example', 'blog': {'uuid': 'blog-one', 'name': 'example'}, 'photos': [{'original_size': {'url': 'https://64.media.tumblr.com/one.jpg',
                                      'width': 1200, 'height': 1200}, 'alt_sizes': []}], 'caption': 'Source caption'}]
            elif setup['adapter'] == 'jpgfish':
                extractor = CheveretoFileExtractor.from_url(target)
                extractor.request = lambda *args, **kwargs: SimpleNamespace(text=
                    '<meta property="og:type" content="image"><meta property="og:title" content="Photo">'
                    '<meta property="og:image" content="https://cdn.example/one.jpg">')
            elif setup['adapter'] == 'leakgallery':
                extractor = LeakgalleryPostExtractor.from_url(target)
                extractor.request = lambda *args, **kwargs: SimpleNamespace(text=
                    '<a href="https://cdn.leakgallery.com/content/creator/watermark_one.jpg">')
            else:
                extractor = CallerFixture.from_url(target)
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
                if native_ytdl:
                    task.pathfmt.set_extension('mp4')
                    task.pathfmt.build_path()
                task.pathfmt.part_enable()
                with task.pathfmt.open("wb") as output:
                    output.write(b"fixture download " + url.encode())
                return True

            task.download = download
            return task

        with (patch("stash_ingest.gallery.NativeDownloadJob", side_effect=job),
              patch('gallery_dl.ytdl.construct_YoutubeDL', factory) if native_ytdl else nullcontext(),
              patch('requests.sessions.Session.request', side_effect=AssertionError('No live source requests')) if native_web else nullcontext()):
            output = io.StringIO()
            with redirect_stdout(output):
                status = producer_cli(["--outbox", str(database), "--endpoint", setup["endpoint"],
                                       "--producer", setup["producer"], "dispatch", "--profile", str(path)])
        result = json.loads(output.getvalue())
        assert status in (0, 2), (status, result)
        assert result["resolution"] == {"state": "queued", "call_uuid": call_uuid, "counts": {"queued": 1, "review": 0}}, result
        admitted = result["submission"]
        assert admitted["state"] == "admitted", admitted
        assert result["run_uuid"] == admitted["run_uuid"]
        assert result["state"] == "source_succeeded", result
        assert result["finish_recovered"] is True, result
        rows = list(box.db.execute("SELECT event_uuid,kind FROM events ORDER BY seq"))
        assert [row[1] for row in rows] == ["source.capture", "attachment.download", "file.completed", "attachment.download"], rows
        capture_id, start_id, file_id, end_id = [row[0] for row in rows]
    # File admission must survive closing/reopening the producer, independently
    # of source completion. Source and file receipts stay separate.
    with closing(Outbox(database, setup["endpoint"], setup["producer"])) as box:
        for attempt in range(8):
            # Advance only local delivery backoff. Recover the server's exact
            # receipt after its deliberately lost report response, then allow
            # the file-dependent terminal report through the same real API.
            box.clock = lambda attempt=attempt: time.time() + 120 * (attempt + 1)
            drain_once(box, client)
            if box.status()["counts"]["acknowledged"] == 4:
                break
        assert box.status()["counts"] == {"pending": 0, "sending": 0, "review": 0, "acknowledged": 4}, box.status()
        for event_id, state in ((start_id, "started"), (end_id, "downloaded")):
            report = box.receipt(event_id)
            assert report["capture_uuid"] == box.receipt(capture_id)["capture_uuid"]
            assert report["result"]["reported_state"] == state
            assert report["result"]["media_ingested"] is False
        assert client.receipt_status(file_id)["state"] == "queued"
        output = io.StringIO()
        with redirect_stdout(output):
            status = producer_cli(["--outbox", str(database), "--endpoint", setup["endpoint"],
                                   "--producer", setup["producer"], "ticket-status", ticket])
        completion = json.loads(output.getvalue())
        assert status == 0 and completion["state"] == "source_succeeded", completion
        assert not completion["remaining"] and not completion["unassigned"], completion
        assert completion["intake_completion"] == "inspect_native_receipts"
        output = io.StringIO()
        with redirect_stdout(output):
            status = producer_cli(["--outbox", str(database), "--endpoint", setup["endpoint"],
                                   "--producer", setup["producer"], "call-status", call_uuid])
        call_result = json.loads(output.getvalue())
        assert status == 0 and call_result["state"] == "source_succeeded", call_result
        assert call_result["counts"] == {"source_succeeded": 1} and not call_result["issues"], call_result
        queued = box.receipt(file_id)
        assert queued["capture_uuid"] == box.receipt(capture_id)["capture_uuid"]
        assert result["intake_completion"] == "inspect_native_receipts"
        if native_backfill:
            # Advance only the local scheduling clock; the native run/proof
            # retains its original real cutoff. No fixture sleeps are needed.
            box.clock = lambda: time.time() + 120
            pending = advance_once(BackfillCalls(box), client, backfill_uuid)
            assert pending["backfill_pending"] is True and pending["state"] == "active", pending
            assert box.db.execute("SELECT completion FROM backfill_calls WHERE uuid=?", (backfill_uuid,)).fetchone()[0]
    if native_backfill:
        with closing(Outbox(database, setup["endpoint"], setup["producer"], clock=lambda: time.time() + 400)) as box:
            done = advance_once(BackfillCalls(box), client, backfill_uuid)
            assert done["state"] == "completed" and not done["backfill_cached"], done
            assert not done["account_backfill_complete"], done
        output = io.StringIO()
        with redirect_stdout(output):
            done_code = n8n_cli(["--outbox", str(database), "--endpoint", setup["endpoint"], "--producer", setup["producer"],
                                "--inspect", recorded["token"], "--strict"])
        assert done_code == 0 and json.loads(output.getvalue()) == done, output.getvalue()
    print(json.dumps({"run_uuid": admitted["run_uuid"], "capture": capture_id, "file": file_id,
                      "started": start_id, "downloaded": end_id,
                      "path": "Account/one.mp4" if native_ytdl else "Account/web.jpg" if native_web else "Account/postabc123_abc123.jpg"}))


if __name__ == "__main__":
    main()
