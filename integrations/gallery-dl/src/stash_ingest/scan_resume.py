"""One-time interpretation of retained legacy gallery-dl checkpoints."""

import hashlib
import json

PREFIX = "gallery-dl-archive-v1:"


def legacy_cursor(job, pathfmt):
    # Preserve the old archive key generator, default JSON spacing and ASCII
    # escaping. Native retained metadata is deliberately not the hash input.
    if job.archive is not None:
        value = job.archive.keygen(pathfmt.kwdict)
    else:
        data = pathfmt.kwdict
        value = {key: data.get(key) for key in ("id", "tweet_id", "post_id", "media_id", "num", "filename")}
        if not any(value.values()):
            value = str(data.get("_url") or data.get("url") or pathfmt.filename)
    encoded = json.dumps([job.extractor.category, value], sort_keys=True, default=str).encode()
    return PREFIX + hashlib.sha256(encoded).hexdigest()
