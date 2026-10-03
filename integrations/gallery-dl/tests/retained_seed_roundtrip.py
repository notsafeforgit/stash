"""Offline Go/Python retained-checkpoint contract fixture; never uses a website."""
import sys

from gallery_dl.extractor.common import Message
from stash_ingest.encoding import decode, encode
from stash_ingest.metadata_bundle import MAX_BYTES
from stash_ingest.metadata_fetch import collect
from test_metadata_fetch import Child, factory

saved = decode(sys.stdin.buffer.read(MAX_BYTES + 1), MAX_BYTES, preserve_numbers=True)
expected = {entry["url"] for entry in saved["pending"]}
requested = []


def find(url):
    assert url in expected and url != saved["url"], "Only saved children may be requested"
    requested.append(url)
    return factory(Child, "https://fixture.invalid/child", [
        (Message.Url, "https://media.invalid/full.jpg", {"id": "fixture-child"})])


# Fixture extractors match a private URL pattern; retain the reviewed source URL
# in the returned evidence while preventing any actual network access.
def exact_child(url):
    target = find(url)
    target.url = url
    return target


result = collect(saved["url"], {}, saved, factory=exact_child, reserve_source=lambda url: url in expected)
assert requested == list(dict.fromkeys(entry["url"] for entry in saved["pending"]))
assert "error" not in result and not result["pending"]
sys.stdout.buffer.write(encode(result, MAX_BYTES))
