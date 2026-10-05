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
`ZFSMedia` creates and retains actual Linux ZFS media snapshots, binding their
dataset/snapshot GUIDs and hold to the sealed checkpoint. `HostFilesystemCapture`
acquires the declared native worker barriers first, captures both providers,
then releases workers before the database copy. Sealed replay releases them
without recapturing. The caller retains the existing backup/dedupe exclusion.
Publication-aware media release keeps permanent bindings and resumes interrupted
cleanup without recursive or forced destruction.

`HostFilesystemCapture.prepare` retains the original producer/download snapshots
and external configuration in a private `ComponentStage`. Seal it before scanning
the retained media view, then pass `component_stage=stage` to the exporter. Retries
reuse those exact copies and only read an existing sealed server checkpoint;
they never pair old worker state with a newly captured server. A stage manifest
is included in the portable archive and bound to the server receipt. Its release
helper requires the complete matching archive and retains permanent records.

These providers do not establish complete worker/configuration inventory or a
matching published media manifest. Daily-script integration and remote readback
remain required. S3 uploads remain the responsibility of the existing host
backup script. An explicit disposable-dataset rehearsal is available in
`tests/zfs_media_probe.py`; it requires existing host ZFS permissions and is not
part of automatic tests.

See the [format and commands](../../docs/native-archive-format.md) for installation,
coverage, storage budgets, manifest rules and the remaining production backup
coordination gates. Run `make validate-archive` from the repository root.
