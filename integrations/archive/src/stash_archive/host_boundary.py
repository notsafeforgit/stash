"""Coordinate retained host views with the native checkpoint handshake.

The enclosing daily backup already owns its backup/dedupe exclusion lock. Keep
that lock across publication; this context manages only worker mutation barriers.
"""

import base64
import os

from .server_checkpoint import ServerCheckpoint
from .storage import InvalidArchive


class HostFilesystemCapture:
    def __init__(self, worker_lock_roots, artwork, media, *, timeout=300):
        from stash_ingest.publication_lock import PublicationBarrier
        self.barrier = PublicationBarrier(worker_lock_roots, timeout=timeout)
        self.artwork, self.media = artwork, media

    def __enter__(self):
        self.barrier.__enter__()
        return self

    def client(self, server, api_key, request_id, roots=(), **options):
        if not self.barrier.acquired:
            raise InvalidArchive("Acquire producer barriers before requesting the native checkpoint")
        return ServerCheckpoint(server, api_key, request_id, roots, boundary=self,
                                boundary_release=self.barrier.release, boundary_validate=self.validate, **options)

    def worker_roots(self):
        return [{"path": base64.b64encode(os.fsencode(path)).decode("ascii"),
                 "device": identity[0], "inode": identity[1]}
                for identity, path in self.barrier.roots]

    def validate(self, boundary):
        if boundary.get("details", {}).get("producer_barriers") != self.worker_roots():
            raise InvalidArchive("Worker barrier inventory differs from the sealed checkpoint")
        self.artwork.open_bound(boundary)
        self.media.open_bound(boundary).verify()

    def __call__(self, ready):
        if not self.barrier.acquired:
            raise InvalidArchive("Producer barriers were released before filesystem capture")
        # Both captures happen while the server holds its writer guard. Nothing
        # waits for the server while acquiring these producer barriers.
        artwork = self.artwork.capture(ready)
        media = self.media.capture(ready)
        return {"artwork": artwork, "media": media, "producer_barriers": self.worker_roots()}

    def __exit__(self, *_):
        self.barrier.release()
