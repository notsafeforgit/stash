# Native source-account identity evidence

The native archive separates captured account identifiers, an account's owner,
and performers depicted in media. `pkg/archive.ExtractCapturedAccount` implements
the versioned `captured-account-v1` parser for producer and catalog-import work.
It returns evidence; it does not allocate accounts, merge them, change ownership,
or assign scene/image performers. Integration with ingestion and catalog import
remains pending under the [transition plan](native-archive-transition-plan.md).

The input is a reconstructed capture object, bounded by the source JSON limit.
The output contains a qualified namespace, optional display label, policy version,
and identifier claims with their evidence basis and JSON pointer into the input.
An explicitly captured ID is required before pairing any handles with it. A
missing ID returns no account claim; a malformed claimed identifier is an error
that the caller must surface for review. Retained source data is not discarded
or replaced by this derived result. Existing handle-only inventory records,
explicit associations, and legacy keys still require their own lossless import;
a nil parser result does not authorize discarding them.

| Extractor | Account ID evidence | Additional captured identity |
| --- | --- | --- |
| Reddit | `author_fullname` | `author`, or the author object's `name`; never a feed owner's `user` object |
| Twitter | `author.id` | `author.name` |
| Instagram | `owner_id` | `username` |
| Bluesky | `author.did` | `author.handle` |
| TikTok | `author.id` and/or `author.secUid`, with distinct identifier kinds | `author.uniqueId` or its captured `name` |
| Tumblr | `blog.uuid` | `blog_name` or `blog.name` |
| Coomer/Kemono | `user` in `mirror:<extractor>:<service>` | A matching profile's `public_id` remains a separately typed mirror claim |
| yt-dlp | `channel_id` or `uploader_id`, qualified by extractor/site | Uploader/channel text remains a display label |
| Other gallery-dl extractors | The author's own `id`, `did`, or `uuid`; otherwise a captured owner/uploader/user/creator object or top-level `user_id`/`uploader_id` | Explicit `username`, `account`, or `handle`; a generic `name` remains a display label |

A Reddit parent capture takes precedence over a child media-host account, and
its pointers retain the `/_reddit/` prefix. For unfamiliar extractors, a separately
named author cannot borrow the ID of another `user` object. Directory names,
filenames, post IDs, captions, and scraper target URLs do not identify the author.
Generic yt-dlp URLs establish only a site namespace, never an account ID.

Opaque IDs retain spelling and case. JSON integer IDs remain exact even above
JavaScript's safe integer range. Fractional/exponent numeric IDs, nested objects,
booleans, invalid text, duplicate JSON keys, and oversized payloads are rejected.
Known case-insensitive native handles use the shared reference normalizer;
unfamiliar handles retain case. No requests or redirects are followed.

Mirror display names are labels, not native handles. A profile's public identifier
is accepted only if both its user ID and underlying service match the captured
mirror account. Neither a mirror user ID nor its public identifier is silently
promoted into a native-service ID. This keeps a Coomer/Fansly account distinct
from a native Fansly account even when their numeric values happen to match.

The account repository's indexed lookup returns all candidates for a qualified
identifier. A returned claim is not permission to consolidate those candidates:
reused handles and conflicting IDs still need the checked review operation in
[native schema 1000014](native-schema.md). Different services remain separate
accounts, even when explicitly linked to the same performer.

Tests cover the named services, unfamiliar extractors, native/mirror separation,
TikTok secondary IDs, Reddit parent context, misleading feed profiles, exact large
IDs, malformed input, replay, unchanged extractor input bytes, and unchanged
identity after source-retention reduction or irrelevant counters change.
