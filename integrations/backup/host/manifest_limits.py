"""Shared host metadata limits for publication, history, audit and restore.

The complete media master is JSON; the much larger portable artifact inventory
is NDJSON and is transferred/validated as a stream. These are distinct from the
portable archive's small manifest and per-inventory-record size limits.
"""

MASTER_BYTES = 512 << 20
MEDIA_BINDING_BYTES = MASTER_BYTES + (1 << 20)
INVENTORY_BYTES = 1 << 30
TRANSFER_BYTES = 1 << 20
