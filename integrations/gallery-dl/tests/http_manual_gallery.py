"""Exercise manual registration, restart and download against the real API."""

from contextlib import closing, redirect_stdout
import io
import json
from pathlib import Path
import re
import sys
import time
from unittest.mock import patch

from stash_ingest import manual
from stash_ingest.client import Client, drain_once
from stash_ingest.encoding import encode
from stash_ingest.gallery import NativeDownloadJob
from stash_ingest.outbox import Outbox
from stash_ingest.source_calls import SourceCalls
from test_gallery import Fixture
from test_manual import runtime_fixture
from test_producer import reddit_data


def main():
    setup = json.load(sys.stdin)
    root = Path(setup['directory'])
    config_path = root / 'manual-runtime.json'
    database = root / 'producer.sqlite'
    if setup['phase'] == 'lost_policy':
        runtime_fixture(root)
        for filename in ('worker.json', 'ytdlp.json'):
            path = root / filename
            value = manual.read(path)
            value['root']['uuid'] = setup['root']
            value['gallery']['extractor']['directory'] = ['Account']
            path.write_bytes(encode(value))
    common = ['--runtime', str(config_path), '--outbox', str(database), '--endpoint', setup['endpoint'],
              '--producer', setup['producer'], '--call', setup['call']]
    output = io.StringIO()
    with redirect_stdout(output):
        result = manual.main(common + ['--queue-only'] + ([setup['target']] if setup['phase'] == 'lost_policy' else []))
    if setup['phase'] == 'lost_policy':
        assert result == 1, result
        with closing(Outbox(database, setup['endpoint'], setup['producer'])) as box:
            assert SourceCalls(box).summary()['calls'] == 0
        print(json.dumps({'verified': True}))
        return
    assert result == 0, output.getvalue()

    class WebsiteFixture(Fixture):
        pattern = re.escape(setup['target'])

    def task(target, *, producer, lock_directory):
        extractor = WebsiteFixture.from_url(target)
        extractor.records, extractor.visited = [reddit_data()], []
        job = NativeDownloadJob(extractor, producer=producer, lock_directory=lock_directory)

        def download(url):
            job.pathfmt.part_enable()
            with job.pathfmt.open('wb') as file:
                file.write(b'fixture manual media')
            return True

        job.download = download
        return job

    output = io.StringIO()
    with patch('stash_ingest.gallery.NativeDownloadJob', side_effect=task), redirect_stdout(output):
        result = manual.main(common + ['--max-wait', '30'])
    value = json.loads(output.getvalue())
    assert result == 0 and value['state'] == 'source_succeeded', (result, value)
    assert value['intake_completion'] == 'inspect_native_receipts'
    client = Client(setup['endpoint'], setup['producer'])
    with closing(Outbox(database, setup['endpoint'], setup['producer'])) as box:
        for attempt in range(8):
            box.clock = lambda: time.time() + (attempt + 1) * 120
            drain_once(box, client)
            if box.status()['counts']['acknowledged'] == 4:
                break
        assert box.status()['counts'] == {'acknowledged': 4, 'pending': 0, 'sending': 0, 'review': 0}, box.status()
        rows = list(box.db.execute('SELECT event_uuid,kind FROM events ORDER BY seq'))
        assert [row[1] for row in rows] == ['source.capture', 'attachment.download', 'file.completed', 'attachment.download'], rows
        capture_id, started_id, file_id, downloaded_id = [row[0] for row in rows]
        assert client.receipt_status(file_id)['state'] == 'queued'
        run = box.db.execute('SELECT run_uuid FROM run_requests WHERE state="admitted"').fetchone()[0]
    output = io.StringIO()
    with patch('stash_ingest.gallery.NativeDownloadJob', side_effect=AssertionError('Completed invocation must not download again')):
        with redirect_stdout(output):
            assert manual.main(common + ['--max-wait', '30']) == 0, output.getvalue()
    print(json.dumps({'verified': True, 'run_uuid': run, 'capture': capture_id, 'file': file_id,
                      'started': started_id, 'downloaded': downloaded_id}))


if __name__ == '__main__':
    main()
