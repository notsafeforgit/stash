# Expiring media shares

Sharing is available by default. Create a share from a scene,
image or gallery's operations menu, from an explicit selection or filtered list
in either list toolbar, or from a performer, studio or tag's operations menu.
The dialog summarizes the source without an item picker or a long selection list.
**Settings → Shares** supports editing expiry and display
options, previewing the recipient view, regenerating a link and revoking access.

The settings list shows **Active** shares by default. **Inactive** contains
revoked and expired shares; its **Delete share** action permanently removes the
grant, its history and guest sessions without deleting library media.
Active shares must be revoked before deletion. Status filtering happens before
pagination, so inactive history cannot hide active links on later pages.
**Delete all inactive shares** removes revoked and expired shares across every
page after confirmation. The server checks eligibility when deleting, keeps
active shares (including expired shares renewed before cleanup), and reports the
number removed. Revocation does not depend on retaining this optional history:
deleted share links remain unavailable. Cleanup never deletes library media.

The default lifetime is 24 hours; presets include one hour and seven days.
Custom expiry is limited to the next 30 days. The public label is always shown.
Saved titles and the original-download button have separate controls. The same
link can be copied again from **Settings → Shares**, and **Recipient
preview** opens that ordinary forwardable URL. Copying or previewing never
regenerates it. **Regenerate link** explicitly invalidates the previous link and
all its guest sessions.

## Scope and privacy

List selections persist across pages. On mobile, tap the selected count to open
the page picker, then select more items on another page. To share all of a
performer's videos, open **Share…** on their video tab without selecting items.
With a selection, the dialog also offers **Filtered list**. The list's complete
search and filter AST are copied, including locked performer/studio/tag context;
page number, page size and later edits to the list or saved filter do not affect
the share. Changing the list's search or filters clears its selection.

Explicit selections always create snapshots. Filtered lists and performer,
studio or tag sources offer **Snapshot** (the default) and **Ongoing**:

- **Snapshot:** resolve the complete selection in the creation transaction and
  freeze membership, titles and file identities. Later additions and metadata
  changes do not change membership. The existing limits of 2,000 selected items
  and 2,000 distinct media items apply, including gallery contents; larger or unavailable selections
  fail without creating a partial grant.
- **Ongoing:** retain immutable rules and query current matches at read time.
  New media and later metadata changes can add matches; items that stop matching
  disappear. There is no fixed media count or 2,000-item lifetime limit. The
  recipient can refresh the results and load more pages, with 50 targets per
  included type per request. Stable keyset cursors avoid offset shifts after
  deletions; refreshing restarts the listing to include changes before a cursor.

Entity sources default to scenes, images and galleries, with controls to choose
fewer types. These use direct entity relationships; a filtered list can express
additional conditions such as descendant studios. Galleries include their
images and linked scenes. Overlapping media is deduplicated. Existing historical
gallery snapshots retain precisely their original frozen contents.

Deleted media disappears from both modes and cannot be delivered through old
URLs. Deleting a shared gallery also removes access to its contents unless they
are independently included. Snapshots pin each item's primary file ID and fingerprint (size, mtime and
available indexed hashes). Deleted objects, changed primary files and changed
source files fail closed. Archive images also pin the archive's identity.
Snapshot titles stay frozen. Ongoing shares use current matching entities,
titles and primary files; every original/thumbnail/preview/stream request checks
current membership, UUID identity and the resolved file. Filter/entity references
are UUIDs, so reusing a deleted local database key cannot widen a grant.
Changing a share's rules requires a new link. Expiry, revocation and anonymous
session rules are identical for both modes; ongoing does not mean permanent.

Shares containing one distinct image or video open directly in the existing
full-screen viewer, including a gallery containing only that item. Close returns
to the overview; explicit tab and detail links still open the requested view.
Videos respect the browser's autoplay policy and may require tapping Play.
Multi-item shares use the normal virtualized entity cards, with tabs for the
media types present and no filtering, sorting or selection controls.
Previews open the existing image or scene lightbox; card titles open minimal
detail views with media and the permitted download action. Gallery details show
their applicable snapshot or ongoing membership. Browser Back closes a lightbox or returns to the
previous list and scroll position. Detail URLs use query parameters under the
same share path and still require that share's guest session.
Ongoing detail links resolve their authorized target independently of the
currently loaded page. Ongoing sources use generic link cards, even when they
temporarily contain only one item, so a preview URL cannot change its media as
membership changes.

The guest response contains only the share label, expiry, selected media,
dimensions, duration, optional titles and scoped media URLs. It omits filesystem
paths, notes, relationships, owner history, global API keys and mutation controls.
The existing video player and image viewer are composed with owner activity,
casting and library actions disabled. The standalone entrypoint loads no plugin
scripts, custom JavaScript, owner configuration, analytics or service worker.

The public document resolves its saved/system theme with a small same-origin
script before the application loads. Its canvas and fixed body must paint the
same color from the first frame: Safari can retain the initial color sampled
from a viewport-sized fixed element. Keep this script parser-blocking and
external so the share CSP can continue to prohibit inline JavaScript.

Shares use the library's existing media delivery. Images are served in their
original format, resolution and animation, and scene covers and thumbnails reuse
existing artwork, including HDR/adaptive AVIF. There is no metadata-stripping or
delivery-mode setting, and sharing does not create persistent image copies.
Missing image thumbnails use the original image; missing video covers use a
placeholder. Original image display depends on the browser's format support.

Video playback offers the original file plus the normal HLS compatibility
pipeline. The shared player prefers direct playback when supported, then remuxing
without video re-encoding where possible, and H.264/AAC transcoding when needed.
A native format/decoding error also recovers through the compatible HLS source,
keeping the playhead and without changing the saved quality preference.
Transcoding remains limited to 1080p; originals and codec-copy streams can retain
source resolution. Guest encoder sessions remain isolated from owner sessions.
Sharing adds no metadata-stripping arguments to the video pipeline.

Original images and videos stream directly without a copy or rendition cache.
Archive images decompress only the selected member, with bounded memory and
unchanged image bytes; the containing archive and unshared members remain
inaccessible. Scene covers follow the current cover selection. The source's
membership rules and file checks apply to every media request, including
generated previews, direct streams and HLS segments.

**Show download button** exposes a convenient original-file download action. Its
download always uses the original file, regardless of the format used for video
playback. Disabling the button is not copy protection: recipients can save media
and original files served for viewing. Embedded file metadata is accepted as part
of the shared media. **Show titles** controls saved library titles in the interface;
other library relationships and browsing routes remain private. Library captions
and scene markers are not exposed as player controls; tracks embedded in an original
file remain part of that file.

The retired metadata-stripped JPEG cache under `<generated>/shares` is no longer
read or written and can be deleted. It is separate from the library's generated
covers, thumbnails and the normal temporary video-stream cache.

## Credentials and enforcement

New recipient links are `/share/<random-id>/?key=<256-bit-secret>`. The server
can authenticate this URL to render link previews without JavaScript or cookies.
Older `/#<secret>` recipient links still open in the browser and become the
same `?key=` URL for copying and navigation. Crawlers need that query URL because
they never receive fragments.
Recipient links keep their `key` in the address bar, including when navigating
between tabs and item details, so browser Copy/Share and copied item links remain
usable by another recipient and by link-preview crawlers. The viewer exchanges
the key via same-origin JSON POST and uses a Secure, HttpOnly, SameSite=Strict,
host-only cookie scoped to that share's path for content and playback requests.
Reloading reuses a valid cookie without creating another guest session.

Share link keys are stored directly in `shares.token`, without application-level
encryption, so authenticated management can return the same URL later. Validation
continues to use `token_hash`; guest session tokens remain hash-only. Public
content responses never return the stored key, and a share credential is never
an owner/API session. Database files and backups therefore contain usable active
share links and must be protected accordingly. Revocation clears the saved key.
Anonymized support exports remove shares, their frozen metadata and guest
sessions, including the deleted bytes; the source library remains unchanged.

Schema 1000122 preserves older hash-only shares and their existing guest sessions.
Their keys cannot be reconstructed: management shows that **Regenerate link** is
needed to enable Copy link and Preview for these records. No link is automatically
rotated. Unused temporary preview bootstraps are removed; already-open preview
sessions retain their original expiry. New previews use ordinary recipient links.

### Telegram and other link previews

Single-item shares emit Open Graph image/video and Twitter player-stream tags,
following the approach used by [FxEmbed](https://github.com/FxEmbed/FxEmbed/blob/main/src/render/video.ts).
The selected media is counted after deduplication, including shares of galleries.
Multi-item shares get a text card and continue to open the full share viewer.
Saved titles are included only when **Show titles** is enabled; otherwise the
card uses the share label. Bare share IDs reveal no card or media.

Preview assets are restricted to that one pinned item, require the link secret
on every GET/HEAD/range request, and obey expiry, rotation, revocation and response
budgets. They do not create guest sessions or increment recipient access counts.
Compatible AVC/AAC MP4 originals retain byte ranges and content length. Other
videos use a streamed 720p H.264/AAC MP4 compatibility rendition, with at most two
concurrent preview encodes, no persistent video copy and no byte-range support
for the generated stream. Images use compatible originals or a JPEG still.
The share viewer still offers the ordinary original/remux/HLS playback choices.

Use a newly created or rotated link for preview support. Existing fragment links
can also be converted by replacing `#` with `?key=` without rotating the secret.
External preview services may keep a downloaded copy after a link is revoked;
Stash can prevent future fetches, but cannot remove a copy held by Telegram.
Treat complete URLs as credentials: omit or redact the `key` query parameter in
proxy/access logs. The app sends no referrer and uses cookie-authenticated URLs
without the key when fetching normal share content. Preview media explicitly
permits cross-origin display; ordinary share/API endpoints retain their existing
cookie and origin checks.

Every content request checks the current grant, session, expiry and version. Every
thumbnail, generated-preview variant, image, original stream/range, manifest, segment and original
download also checks the pinned item. Generated previews accept only files in the
current cover manifest with its current revision, through share-scoped URLs; no
owner media URL or API key is exposed.
Revocation and rotation remove sessions, cancel active responses and stop that
grant's encoders. Expiry cancels responses and encoder leases. The viewer checks
status every 15 seconds and removes its player at expiry. Owner changes cancel
in-flight delivery; downloads disabled by an edit are denied immediately.
Extending a share does not extend a previously issued cookie: recipients can
reopen the original link to establish a session with the new expiry.

Counts are successful link exchanges, including owner visits through Recipient
preview, not distinct people, media requests or watch history. Copying a link
does not count as a visit, and reusing an existing guest cookie does not create
another exchange. There is no separate owner-preview credential.

Public responses use no-store, no-referrer, noindex, nosniff and a restrictive CSP.
The browser viewer uses same-origin resource policy. Original media documents also receive a sandboxed
CSP that prevents scripts and external resource loading.
The public router has no GraphQL, plugins, arbitrary files, directory listing,
CORS exemption or general application fallback. Malformed/encoded traversal
paths are rejected, and invalid, expired or revoked credentials look alike.

Budgets bound concurrent responses (12 per share, 64 total), encoder variants
(three per share, 12 total), archive deliveries (two total), guest sessions (1,000
per share) and throughput (20 MiB/s per share). Exchanges are limited to 30 per
minute per direct peer; behind a single proxy this is a shared limit. Proxy
headers are not trusted for this limiter. Archive delivery streams without a
whole-file memory buffer; seeking accepts one byte range per request to bound
decompression work. Video playback uses the existing stream cache and lifecycle.

## Caddy and deployment

Keep the owner's Stash hostname behind Authelia. Use a separate public hostname
that forwards **only** the share namespace, retaining the `/share` prefix. Set
**Settings → Shares → Public share address** to `https://nsfw.ak.codes/share`, or
set `sharing_public_url` in the Stash configuration before startup. An empty
setting uses the owner's current origin and `/share` path; its proxy would need
a narrowly scoped exemption for that namespace.

The example below targets the existing rootless Podman service. Caddy owns TLS;
its default HTTPS redirect remains enabled. The response marker check fails
closed with 503 if Stash is rolled back, v3 is disabled or the sharing handler
has not been deployed. It prevents the ordinary Stash SPA from being exposed
as a public fallback.

```caddyfile
nsfw.ak.codes {
    header {
        Cache-Control "no-store"
        Referrer-Policy "no-referrer"
        X-Robots-Tag "noindex, nofollow, noarchive"
        X-Content-Type-Options "nosniff"
    }
    @shares path /share /share/*
    handle @shares {
        reverse_proxy stash:8009 {
            header_up -Authorization
            header_up -ApiKey
            header_up -Remote-User
            header_up -Remote-Groups
            header_up -Remote-Email
            header_up -Remote-Name
            header_up -X-Forwarded-Prefix
            @share_response header X-Stash-Share-Version 1
            handle_response @share_response {
                copy_response
            }
            handle_response {
                respond "Sharing is unavailable" 503
            }
        }
    }
    handle {
        header Content-Type "text/plain; charset=utf-8"
        respond "Not found" 404
    }
}
```

Keep a typed response body on the fallback. An empty `404` without a
`Content-Type` can make Safari offer a download when opening the bare hostname.
The text response keeps that URL and other non-share paths unavailable while
giving the browser something it can display.

Do not use `handle_path`: it strips the namespace the backend authorizes. Do not
forward the entire hostname to Stash or inject an owner API key. The share
router is public by design; management remains on the authenticated hostname.
Use the [normal v3 deployment procedure](v3-deployment.md) to deploy the binary
and its main, share and offline entry points together. Shares and sessions
are native database records; the promotion migration preserves existing grants
from their historical fork tables.
Backups containing active grants/sessions are sensitive. Revoke shares after
restoring an older database if old capabilities must remain invalid.

Verification covers SQLite persistence, fixed selections, rotated/revoked/
expired credentials, cross-share and owner-credential rejection, range downloads,
all HLS endpoint families, response cancellation, original-file fidelity, compatible video playback,
typed public-response parsing and browser creation/edit/revocation and playback.
Verify the public host denies `/graphql`, `/scene/1/stream`, `/assets/`, `/custom/`
and `/`, even with an owner API key or Authelia cookie.
