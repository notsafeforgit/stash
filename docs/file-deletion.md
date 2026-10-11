# Recoverable file deletion

Stash stages a deletion in a private sibling directory:

```text
/media/original-name.mp4
/media/.stash-delete-<random>/original-name.mp4
```

The basename never changes, including for directories and repeated names in
trash. This avoids overflowing a filesystem's filename limit with a `.delete`
suffix. The staging directory remains on the original filesystem. Staging and
restoration use an atomic rename that refuses to replace an existing path;
staging never falls back to copying. Scans skip `.stash-delete-*` directories.
Total path length limits still apply to the extra directory component.

## Journal lifetime and recovery

Before moving a file, Stash writes and flushes a small recovery record beside
the database, under `.stash-deletions-<database-name-hash>/`. It also inserts a
marker into the native `file_deletions` table in the same transaction as the database edit.
The journal stores paths and filesystem identities, not media contents.

After the transaction ends, and whenever the database opens, recovery checks
the committed database state:

- A committed marker means finish deleting the staged file or moving it to trash.
- No committed marker means restore the file to its original path.
- A conflicting path, replaced directory, inaccessible mount, damaged record,
  or failed filesystem operation leaves the affected work pending and logs the
  error. Recovery does not overwrite the conflicting file.

Recovery holds SQLite's writer lock so it cannot mistake a live transaction
for an abandoned one. Directory and file identities guard against replacement
paths; nested operations restore parents first and finish children before
deleting parents. This is recovery for interrupted Stash operations, not
protection against arbitrary external edits to staged files.

**Completed records are removed immediately.** Stash flushes filesystem changes,
removes the journal record, then removes its database marker. The empty journal
directory is also removed. Ordinary transactions without deletions do not
create a journal. There is no retained deletion history or age-based rotation.

Only unresolved operations remain. They are retried at the next database open
or after another transaction stages a deletion. Pending records do not expire:
discarding them based on age could lose the only information needed to restore
a file. Once the underlying problem is resolved, successful recovery removes
them automatically. An interruption between reserving an empty staging/trash
directory and recording it can leave an empty directory, but no moved payload.

Do not delete a pending journal as cache maintenance. Keep it with its database
and staged media when making a recovery snapshot. Restore matching snapshots;
pairing an old database backup with a newer pending journal can change which
operations appear committed. Finish pending operations before renaming the
database or switching to an upstream binary, which does not run this recovery.

## Trash transfers and durability

Trash entries use `trash/stash-trash-<random>/original-name.mp4`, including the
first occurrence of a name. The unique directory avoids collision checks that
could race another deletion. Existing trash contents are preserved.

Trash transfer starts after the database commits. A same-filesystem transfer
uses rename. Only a cross-device error enables copying: Stash copies into
private scratch space, flushes it, records the completed copy, publishes it
without replacement, and then removes the staged original. Interrupted copies
can be retried. Regular files, directories and symlinks are supported; unsupported
special files remain pending. Copies preserve basic permissions and modification
times, but do not replicate ownership, ACLs or extended attributes.

Writable SQLite connections use `synchronous=FULL` so an acknowledged database
commit is durable before irreversible file removal. This adds disk-sync cost
to writes, including writes without deletions. Journal updates, copies and
directory changes are also flushed. These guarantees depend on the filesystem
and storage honoring flush requests.

Atomic no-replacement rename is implemented for Linux, macOS and Windows.
Unsupported platforms or filesystems fail staging safely. Windows uses
write-through moves and flushed journal/copy files; its directory-flush behavior
does not provide the same power-loss guarantees as the POSIX implementation.

Fork migration 9 adds the pending-marker table without changing upstream's
schema version or tables. It normally contains no rows.

## Bulk scene and image deletion

Bulk API mutations queue file paths while removing their database records, then
stage the paths in the transaction's pre-commit hook. All recovery records are
written and flushed before the first rename. Independent journal writes and
staging-directory flushes use at most eight filesystem workers; each shared
source parent is flushed once after its moves. Independent parent flushes also
share the bounded pool, including during recovery, so selections spread across
folders benefit as well. SQLite writes remain sequential
inside the same transaction. Any preparation or staging failure aborts that
transaction and restores its files through the existing recovery protocol.

Committed independent permanent deletions also share their parent and journal
directory flushes during cleanup. Nested directories and trash transfers retain
their ordered recovery protocol. Journal version, commit markers and portable
backup format are unchanged; an interrupted operation remains recoverable by
the same reader. Staging is durable before commit, and cleanup is durable before
its recovery markers are discarded. Failed cleanup remains pending as above;
the speedup removes repeated flushes rather than weakening their guarantees.

The scene and image selection dialogs submit up to 50 entities per request and
show the number of confirmed deletions. Batches run sequentially, each with its
own transaction, so earlier completed batches remain deleted if a later one
fails. The UI refreshes affected lists once at the end, including after failure,
and never automatically retries a request whose response could have been lost.
The dialog's host card stays mounted until completion; intermediate batches do
not trigger repeated list refetches.

## Validation

The deletion tests cover 255-byte ASCII and UTF-8 basenames, directories,
trash collisions, rollback conflicts, nested deletions, changed filesystem
identities, damaged records and journal pruning. SQLite tests terminate a
separate process before and after commit, then verify startup recovery agrees
with the database. Batch tests also interrupt before staging and after only one
rename, and exercise cancellation, partial preparation failures and replaced
sources. Copy protocol tests exercise interrupted publication and
retention of the original until the copy is complete.

```sh
go test -tags 'integration sqlite_stat4 sqlite_math_functions sqlite_fts5' ./pkg/file ./pkg/fsutil ./pkg/sqlite/... ./pkg/txn
```

Set `STASH_TEST_TRASH_ROOT` to a writable directory on a different filesystem
from the system temporary directory to also exercise real cross-device trash
transfers. The test creates and removes its own temporary directory there.

`go test ./pkg/file -run '^$' -bench '^BenchmarkDeletion120$' -benchtime=3x`
compares serial and batch staging with the current recovery implementation.
`STASH_TEST_DELETION_BENCH_ROOT` optionally selects the filesystem for disposable
benchmark fixtures; no pre-existing media is read or deleted. These timings do
not include database queries, generated previews, plugin hooks or HTTP latency.
Set `STASH_TEST_DELETION_BENCH_LAYOUT=scattered` to put every fixture in a
different folder instead of sharing one parent.
