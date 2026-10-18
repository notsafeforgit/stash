# External client compatibility

Core Stash has no gallery-dl test/build dependency. Python unit suites and releases
live in [stash-integrations](https://github.com/notsafeforgit/stash-integrations).
The core repository retains real-server HTTP tests, restore compatibility tests,
and canonical synthetic contract corpora.

```sh
make pre-clients test-clients
```

The setup checks out the full commit pinned in `integration-clients.json`, creates
an isolated uv environment and verifies its installed producer modules. Tests
against local client edits can instead set absolute `STASH_INTEGRATIONS_ROOT` and
`PRODUCER_PYTHON` paths and run `make test-clients` without `pre-clients`.

Client tests use `internal/testclients.Runtime`. Ordinary Go tests skip only these
external-client checks, retaining server assertions and database regression tests.
An explicitly enabled client suite fails if its runtime or checkout is missing.
The runner follows Go test-helper calls to select all tests using the clients;
there is no manually maintained test-name list. It disables Go test-result caching
because external source files are outside the Go module's cache boundary.

API, schema and contract dependency changes run this suite in CI. Known unrelated
frontend and media-engine changes do not. Update the pin after a client release
and test both repositories' contracts before deploying a client that changes
capture/adapter identity. Installation-specific credentials and policies belong
in the deployment repository, not either public source repository.
