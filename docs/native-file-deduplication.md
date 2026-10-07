# Native physical file deduplication

The development backend implements a preview/apply service for redundant
physical media files in schema 1000095. The installed host dedupe launcher still
uses catalogs; its native caller conversion and production activation are
outstanding. This service is not a scene/image merge operation.

An automatic candidate must contain two distinct, indexed, nonempty regular
video or image locations under one active, reviewed media root. Both must belong
to the same single scene or image. The service keeps that media UUID, selected
metadata, performer links, source-post and album relationships. If the removed
location is the primary file, the surviving location becomes primary through
the ordinary media repository and its after-commit notifications.

Different/ambiguous owners, unindexed files, ZIP members, gallery-file links,
caption-bearing duplicates, hard links and excessive source history require
review or intake first. The service does not infer a media merge, discard
captions, or treat a filename/fclones hash as proof of identical bytes. Unrelated
media entries remain available for a separate explicit merge decision.

## Application API

These routes are under `/api/v3/archive`, behind application authentication and
the same-origin mutation guard. Source producer tokens do not grant access.

| Method and path | Purpose |
| --- | --- |
| `POST /file-deduplication/preview` | Inspect the exact root-relative pair without changing the database or files. |
| `POST /file-deduplication/apply` | Verify both complete files and commit the reviewed removal. |
| `GET /file-deduplication/requests/{request_uuid}` | Recover the original committed result after an uncertain response. |

Preview input:

```json
{
  "root_uuid": "<reviewed media-root UUID>",
  "keep_path": "account/first.mp4",
  "remove_path": "account/duplicate.mp4"
}
```

The result contains `eligible`, `blocked_reason`, a `signature`, the media/file
UUIDs, affected source-match count, duplicate byte size and whether primary
selection changes. `eligible` means the associations permit verification; it
does not assert byte equality. Linux previews use descriptor identity, size,
mtime and ctime instead of hashing whole videos. Other platforms without a
change token require a preview digest.

Apply repeats the input and adds `request_uuid` and the exact preview
`signature`. Persist that body before sending it. Repeating the same committed
request returns its original receipt, even after later file changes. A reused
UUID with different input fails. A stale preview or differing bytes fails
without committing the removal. After a known rejection, prepare a fresh
preview and request; an uncertain response must first recover its saved request.

Neither endpoint reports queued admission as completion. Apply is synchronous
and may take time to read large videos. Cancellation before commit rolls back
the operation; a lost response after commit is recovered through the receipt.
Operational responses omit the internal proof, absolute host paths and file
technical metadata.

## Verification, provenance and recovery

The signature covers both file generations and revisions, the media owner,
primary selection, root revision/binding, original file metadata and up to
1,000 affected source matches. Full server-side SHA-256 verification happens
outside the writer transaction with both descriptors held open. A separate
verification transaction retains their content proofs while both original
paths still exist; rejected deduplication may therefore leave useful hash
evidence without a deletion receipt.

The removal transaction repeats the review checks, retains a signed
`file_deduplications` receipt, and adds explicitly derived source matches to the
surviving file generation. Original observations and matches remain unchanged.
The removed file UUID redirects to the survivor; its old generation proofs and
path-removal fence remain available. Source content claims do not gain a
retroactive checksum. Files associated with different posts can share the same
surviving location without collapsing those posts.

Filesystem removal uses Stash's managed deletion journal and configured trash.
After staging the redundant entry, the service verifies the staged inode and
hashes it again: rename changes ctime, so ignoring that change would conceal a
write during staging. This final read holds the writer transaction. Both the
survivor and staged entry are rechecked before commit. A failure restores staged
files and rolls back associations; a process death is recovered from the
database's commit marker. Unfinished trash transfers retain their journal.

The database snapshot includes deduplication receipts and provenance. Startup
validates their signatures and retained verified-content references.
Anonymisation removes the private receipts before source/content evidence.

## Host conversion boundary

The native host caller must retain the existing backup/dedupe lock and acquire
the inventoried native-worker publication barriers before application writes.
Fclones may discover candidates and choose which path to keep, but must not run
its own removal command. Apply pairs sequentially with saved request identities
and report review cases. Do not perform the old orphan-NFO/text cleanup through
an unconverted catalog writer; retained sidecars need their own verified import
and cleanup boundary. Direct/manual file intake must remain available.

Tests cover scenes and images, primary replacement, unchanged selected values,
retained original/derived source matches, restart replay, changed bytes with
restored mtime, stale ownership/root/metadata/evidence, refusal of ambiguous
owners and captions, transaction rollback, and real child-process death before
and after commit. Host activation and the populated schema-95 rehearsal remain
release work.
