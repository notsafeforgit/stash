# Native archive tools

The standalone `stash-archive` package exports, inspects, verifies and restores
versioned native database/artwork/component bundles. It uses Python's standard
library and preserves SQLite and binary data losslessly.

See the [format and commands](../../docs/native-archive-format.md) for installation,
coverage, storage budgets, manifest rules and the remaining production backup
coordination gates. Run `make validate-archive` from the repository root.
