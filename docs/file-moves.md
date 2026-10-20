# Recoverable library file moves

`moveFiles` moves regular files between configured library folders. A basename
must be one filename, without separators, traversal, drive prefixes or alternate
stream syntax. Folder IDs and explicit paths use the same root validation.
Configured roots must be online; directory aliases are resolved and a child
symlink cannot escape its root. Final symlinks and special files are rejected.
Existing destinations are never overwritten.

The database edits, including archive-entry folder paths, remain one
transaction per request. Before changing a file, Stash flushes an intent under
`.stash-moves-<database-name-hash>/` beside the database. Native schema 1000124
adds `file_moves`, whose marker commits in the same transaction as those edits.
This migration creates an empty table; it moves no existing media.

The source is staged in a private `.stash-move-*` sibling directory, which scans
exclude. A same-filesystem move uses an atomic rename without replacement.
A cross-filesystem move copies to private scratch space at the destination,
flushes the completed copy and its recorded identity, then publishes it without
replacement. The staged original is retained until the database commits.
Copies preserve permissions and modification time, but not ownership, ACLs or
extended attributes.

Recovery runs after the transaction and at database open, holding the SQLite
writer lock. A committed marker finishes cleanup; its absence restores the
original. Directory and file identities prevent recovery from overwriting or
deleting replacements. An unavailable mount, conflict or damaged record retains
the intent for retry and reports the problem. Cancellation stops copying and
rolls back after filesystem work has stopped; it cannot release the writer lock
while recovery would race an active copy.

Unresolved work blocks new move batches. Path changes, removals and rescans of affected
file identities or paths are also rejected, while unrelated file and metadata
work may continue. A damaged intent whose scope cannot be determined blocks
file/folder changes until repaired. Retry startup recovery after resolving the
external conflict; never delete an intent as if it were cache data.

Completed intents and commit markers are removed immediately, after flushing
filesystem changes. The empty journal directory is removed too. No move history
or age-based expiry is retained. Admission permits at most 1,000 files per batch,
and every record is limited to 64 KiB: pending journal metadata stays below
64 MiB plus filesystem overhead, including one interrupted rewrite. Recovery
information is never evicted to make room for another request.

Temporary media copies can be as large as the selected media. They are reclaimed
on success or rollback; failed recovery preserves them. An interruption between
reserving an empty staging/target directory and recording its identity may leave
an empty directory, but no moved payload depends on that unrecorded directory.
As with deletion recovery, these guarantees assume storage honors flushes and
do not protect against arbitrary external changes to staged media.

Native checkpoint capture and offline snapshot verification reject unfinished
moves. Complete recovery before making a portable archive. The existing archive
format's deletion-recovery component cannot silently stand in for move recovery.
Do not pair an older database with newer pending intents or change the database
name while recovery is pending.

Focused tests use disposable files and databases. They cover interrupted
staging, publication and commit; partial cross-device copies; rollback/retry;
shared directories; replacement conflicts; cancellation with a competing
writer; path validation; admission limits; pruning; and schema preservation.
Set `STASH_TEST_MOVE_ROOT` to a writable filesystem different from the system
temporary directory to exercise actual cross-device copies:

```sh
go test -tags 'integration sqlite_stat4 sqlite_math_functions sqlite_fts5' \
  ./pkg/file ./pkg/sqlite -run 'TestMove|TestFileMove'
```
