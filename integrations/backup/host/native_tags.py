"""Exclusive-publisher lifecycle tags with durable cache invalidation.

All writers hold the host backup lock. Only this publisher may change the
retirement tag. An unrelated tag is preserved. Restores never change tags.
"""

import re

from object_receipts import identity
from stash_archive.storage import InvalidArchive

RETIRED_TAG = "stash-native-retired"
MANAGED = re.compile(r"(?:native-archives/objects/[0-9a-f]{64}\.gz|"
                     r"native-archives/runs/[0-9a-f-]{36}/(?:manifest\.json|artifacts\.jsonl|verification\.json)|"
                     r"manifests/runs/[A-Za-z0-9][A-Za-z0-9_-]{0,127}/manifest\.json)\Z")


def read_tags(store, args):
    response = store.client.get_object_tagging(**args)
    tags = response.get("TagSet") if isinstance(response, dict) else None
    if (not isinstance(tags, list) or len(tags) > 10
            or any(not isinstance(tag, dict) or set(tag) != {"Key", "Value"}
                   or not isinstance(tag["Key"], str) or not isinstance(tag["Value"], str) for tag in tags)
            or len({tag["Key"] for tag in tags}) != len(tags)):
        raise InvalidArchive("Invalid native lifecycle tag response")
    return {tag["Key"]: tag["Value"] for tag in tags}


def reconcile(store, key, sha256, size, state, *, receipts=None, listed=None):
    if state not in ("live", "retired") or not MANAGED.fullmatch(key):
        raise InvalidArchive("Lifecycle tags require an owned native object")
    full_key = store.prefix + key
    item = listed.get(full_key) if listed is not None else None
    if listed is not None and item is None:
        return False
    if receipts is not None and receipts.tags_match(full_key, sha256, size, item, state):
        return True
    head = store.head(key)
    if head is None:
        return False
    store.check_head(head, sha256, size)
    args = {"Bucket": store.bucket, "Key": full_key}
    if head.get("VersionId") is not None:
        args["VersionId"] = head["VersionId"]
    try:
        tags = read_tags(store, args)
        expected = dict(tags)
        if state == "retired":
            expected[RETIRED_TAG] = "true"
        else:
            expected.pop(RETIRED_TAG, None)
        if len(expected) > 10:
            raise InvalidArchive("Native object has no room for a retirement tag")
        if tags != expected:
            if receipts is not None:
                receipts.invalidate_tags(full_key)
            try:
                store.client.put_object_tagging(**args, Tagging={"TagSet": [
                    {"Key": k, "Value": v} for k, v in sorted(expected.items())]})
            except Exception:
                # A successful write with a lost reply is recoverable only by
                # reading back the exact preserved tag set.
                if read_tags(store, args) != expected:
                    raise
            if read_tags(store, args) != expected:
                raise InvalidArchive("Native lifecycle tag write was not verified")
        # Expiration could win the race before the tag was removed. Do not
        # publish a new reference until the current object still verifies.
        current = store.head(key)
        if current is None:
            return False
        store.check_head(current, sha256, size)
        if identity(current) != identity(head) or current.get("VersionId") != head.get("VersionId"):
            raise InvalidArchive("Native object changed during lifecycle reconciliation")
        if receipts is not None:
            receipts.remember(full_key, sha256, current, tag_state=state)
        return True
    except Exception as error:
        code = str(getattr(error, "response", {}).get("Error", {}).get("Code"))
        if code in {"404", "NoSuchKey", "NotFound", "NoSuchVersion"} and store.head(key) is None:
            return False
        raise
