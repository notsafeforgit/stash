# Native archive tools

The standalone `stash-archive` package exports, inspects, verifies and restores
versioned native database/artwork/component bundles. It uses Python's standard
library and preserves SQLite and binary data losslessly.

`stash-archive verify --native-validator /opt/stash/stash --producer-origin
https://stash.example ARCHIVE` combines full native schema/provenance validation
with producer receipt checks on one temporary restore. Both proofs identify the
exact archive and library bytes. This does not certify the remaining shared
media, filesystem-journal and configuration boundary.

See the [format and commands](../../docs/native-archive-format.md) for installation,
coverage, storage budgets, manifest rules and the remaining production backup
coordination gates. Run `make validate-archive` from the repository root.
