# Host backup integration

This directory tracks the existing host daily backup, restore and audit tools
while they are converted to the native archive contract. `baseline.json` records
the exact installed files captured before conversion. The baseline commit is a
rollback point, not a second supported runtime or an installation instruction.

The copied `host/s3-backup-README.md` describes the installed policy. Preserve its
daily cadence, storage classes, missing-mount/empty-source guards, upload retry
ledger, pending/last-copy protections, restore behavior and publication-before-
cleanup ordering. S3 credentials and remote operations remain host-owned.

The baseline still calls the old catalog publisher and its regression fixture
still imports that catalog package. Both must be converted before installation.
The native implementation must use the retained filesystem views and persistent
component stages in `integrations/archive`, publish and verify the matching
native/media manifests, and update the restore/audit tools together. No script
in this directory is installed or scheduled by copying it into the repository.

The existing regression suite blocks real subprocess/AWS operations and uses
temporary local object stores. During baseline verification it also needs the
installed old catalog package and `boto3`; that dependency is temporary while
its callers are being replaced. Production cutover and installation follow the
full native transition plan and review gates.
