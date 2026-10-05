# Native archive tools

The standalone `stash-archive` package exports, inspects, verifies and restores
versioned native database/artwork/component bundles. It uses Python's standard
library and preserves SQLite and binary data losslessly.

`stash-archive verify --native-validator /opt/stash/stash --producer-origin
https://stash.example ARCHIVE` combines full native schema/provenance validation
with producer receipt checks on one temporary restore. Both proofs identify the
exact archive and library bytes. This does not certify the remaining shared
media, filesystem-journal and configuration boundary.

`export --server` captures the running native application's fixed database,
configuration and deletion recovery components after declared producer journals.
Export never releases the server's retryable temporary files. An authorized
publisher calls `release_published_checkpoint` only after it verifies durable
enclosing publication; the server retains the archive binding before removing
large components. S3 publication and abandoned-capture retention integration
remain required before production scheduling.

`ServerCheckpoint` also accepts a trusted host callback to capture an external
filesystem view during a bounded database writer guard. It verifies and retains
the confirmation, and sealed retries reuse the original view. The host-side
`ArtworkPins` provider retains original artwork inodes during this callback;
exports and retries use those pins after live changes or deletion. Its release
helper verifies the archived association and supports interrupted cleanup.
Media snapshots and producer barriers still need integration; the receipt alone
does not prove a complete filesystem backup. S3 uploads remain the responsibility
of the existing host backup script.

See the [format and commands](../../docs/native-archive-format.md) for installation,
coverage, storage budgets, manifest rules and the remaining production backup
coordination gates. Run `make validate-archive` from the repository root.
