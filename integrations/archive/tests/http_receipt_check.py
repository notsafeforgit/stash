"""Verify real API/producer acknowledgements between isolated fixture requests."""

from contextlib import closing
import json
import sys

from stash_archive.bundle import connect_readonly
from stash_archive.receipts import verify_ingestion_receipts

request = json.load(sys.stdin)
with closing(connect_readonly(request["library"])) as library, closing(connect_readonly(request["outbox"])) as outbox:
    library.execute("BEGIN")
    outbox.execute("BEGIN")
    proof = verify_ingestion_receipts(library, [outbox], request["origin"])
assert proof["coverage"] == "capture-file-download-run-and-job-receipts"
print(json.dumps({"verified": True, "producers": len(proof["producers"])}))
