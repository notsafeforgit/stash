"""Native detail-verification fixture through the real metadata collector."""
import copy
import sys
from unittest.mock import patch

from gallery_dl import exception
from gallery_dl.extractor.common import Message
from stash_ingest.encoding import decode, encode
from stash_ingest.gallery import SUPPORTED_VERSION
from stash_ingest.metadata_bundle import MAX_BYTES
from stash_ingest.metadata_fetch import collect
from test_metadata_fetch import Child, Post, factory

request = decode(sys.stdin.buffer.read(MAX_BYTES + 1), MAX_BYTES)
platform, url, metadata = request['platform'], request['url'], request['metadata']
assert platform in ('reddit', 'twitter')
post_type = type('SelectedPost', (Post,), {'category': platform, 'subcategory': 'submission' if platform == 'reddit' else 'tweet'})
child_url = 'https://www.redgifs.com/ifr/detailfixture'
contacts = []


def perform(data, resume=None, *, fail_child=False):
    def exact(target_url):
        contacts.append(target_url)
        if target_url == url:
            messages = [(Message.Directory, '', data)] + [
                (Message.Url, 'https://media.invalid/full-' + str(i) + '.jpg', {**data, 'num': i}) for i in range(3)]
            if platform == 'reddit':
                messages.append((Message.Queue, child_url, data))
            target = factory(post_type, 'https://fixture.invalid/post', messages)
        else:
            assert target_url == child_url and platform == 'reddit'
            failure = exception.AuthenticationError('private source failure') if fail_child else None
            target = factory(Child, 'https://fixture.invalid/child', [
                (Message.Url, 'https://media.invalid/full-child.mp4', {'id': 'detailfixture', 'large_id': 9223372036854775815})], failure)
        target.url = target_url
        return target

    return collect(url, {'extractor': {'sleep-request': 0, 'sleep-extractor': 0}}, resume,
                   factory=exact, reserve_source=lambda requested: requested in {url, child_url})


# Any accidental network or media writer is a fixture failure.
with patch('requests.sessions.Session.request', side_effect=AssertionError('network access')), \
        patch('gallery_dl.job.DownloadJob', side_effect=AssertionError('media download')), \
        patch('gallery_dl.archive.DownloadArchive', side_effect=AssertionError('archive write')):
    weak = copy.deepcopy(metadata)
    weak.pop('date', None)
    first = perform(weak)
    strong = perform(metadata, fail_child=platform == 'reddit')
    assert 'error' not in first and 'error' not in strong
    if platform == 'reddit':
        assert len(strong['pending']) == 1
        before = len(contacts)
        complete = perform(metadata, strong)
        assert contacts[before:] == [child_url]
        assert complete['records'][:len(strong['records'])] == strong['records']
    else:
        complete = strong
    assert 'error' not in complete and not complete['pending']
    assert b'private source failure' not in encode(complete)
    sys.stdout.buffer.write(encode({'runtime': SUPPORTED_VERSION, 'weak': first, 'partial': strong, 'complete': complete}, MAX_BYTES))
