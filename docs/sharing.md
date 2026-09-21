# Expiring media shares

Sharing is a v3 feature (`enable_v3_ui: true`). Create a share from a scene,
image or gallery's operations menu, from an explicit selection in either list
toolbar, or in **Settings → Shares**. The settings page can combine scenes,
images and galleries in one link. It also supports editing expiry and display
options, previewing the recipient view, regenerating a link and revoking access.

The default lifetime is 24 hours; presets include one hour and seven days.
Custom expiry is limited to the next 30 days. The public label is always shown.
Saved titles and original-file downloads require separate opt-ins. The link
secret is displayed once: copy it before closing the result dialog. Regenerating
the link invalidates the previous link and all its guest sessions.

## Scope and privacy

Shares freeze an explicit selection at creation, including each gallery's
current image membership and order. Gallery-linked scenes, performers, studios,
tags and future additions are not included. Overlapping media is deduplicated;
each share can contain at most 2,000 distinct media items. Sharing a performer,
studio, tag, saved filter or whole library is deliberately outside this release.

Each selected item pins its primary file ID and fingerprint (size, mtime and
available indexed hashes). Deleted objects, changed primary files and changed
source files fail closed. Archive images also pin the archive's identity.
Titles are a snapshot: subsequent metadata edits do not change an existing share.
Membership changes require creating a new share.

The guest response contains only the share label, expiry, selected media,
dimensions, duration, optional titles and scoped media URLs. It omits filesystem
paths, notes, relationships, owner history, global API keys and mutation controls.
The existing video player and image viewer are composed with owner activity,
casting and library actions disabled. The standalone entrypoint loads no plugin
scripts, custom JavaScript, owner configuration, analytics or service worker.

Images are decoded into metadata-free JPEG renditions: 640-pixel thumbnails and
up to 4,096-pixel full views. Animated image files currently get a still rendition;
video-backed images play as video. Scene playback uses the existing HLS pipeline,
with guest sessions separated from owner sessions and source container/track
metadata removed. Transcoding is limited to 1080p; compatible codec-copy streams
can retain the source resolution. Captions and scene markers are not exposed.
The optional original download is the original file and may contain embedded
metadata. Recipients can save or record anything they can view; revocation cannot
remove bytes already delivered to them.

## Credentials and enforcement

The link is `/share/<random-id>/#<256-bit-secret>`. The fragment is never part of
an HTTP request URL. The viewer removes it from the address bar, exchanges it
via same-origin JSON POST, and keeps access in a Secure, HttpOnly, SameSite=Strict,
host-only cookie scoped to that share's path. Only SHA-256 digests of secrets and
session tokens are persisted. A share credential is never an owner/API session.

Every content, thumbnail, image, stream manifest, segment and original download
request checks the current grant, session, expiry, version and pinned item.
Revocation and rotation remove sessions, cancel active responses and stop that
grant's encoders. Expiry cancels responses and encoder leases. The viewer checks
status every 15 seconds and removes its player at expiry. Owner changes cancel
in-flight delivery; downloads disabled by an edit are denied immediately.
Extending a share does not extend a previously issued cookie: recipients can
reopen the original link to establish a session with the new expiry.

Recipient preview uses a one-use bootstrap valid for five minutes and a guest
session lasting at most 15 minutes. It does not change the real link or increment
access counts. Counts are successful recipient link exchanges, not distinct
people, media requests or watch history.

Public responses use no-store, no-referrer, noindex, nosniff, same-origin resource
policy and a restrictive CSP. Embedded original downloads are an explicit opt-in.
The public router has no GraphQL, plugins, arbitrary files, directory listing,
CORS exemption or general application fallback. Malformed/encoded traversal
paths are rejected, and invalid, expired or revoked credentials look alike.

Budgets bound concurrent responses (12 per share, 64 total), encoder variants
(three per share, 12 total), image generators (two total), guest sessions (1,000
per share) and throughput (20 MiB/s per share). Exchanges are limited to 30 per
minute per direct peer; behind a single proxy this is a shared limit. Proxy
headers are not trusted for this limiter. Image generation has a 30-second
deadline; archive members are bounded to 256 MiB. Generated image renditions
live under `<generated>/shares` and may be removed while Stash is stopped; they
are regenerated on demand. Apply the host's usual storage and backup policy.

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
        respond 404
    }
}
```

Do not use `handle_path`: it strips the namespace the backend authorizes. Do not
forward the entire hostname to Stash or inject an owner API key. The share
router is public by design; management remains on the authenticated hostname.
Use the [normal v3 deployment procedure](v3-deployment.md) to deploy the binary
and both embedded UIs together. Fork migration 8 adds `fork_shares` and
`fork_share_sessions`; it does not alter upstream tables or schema version.
Backups containing active grants/sessions are sensitive. Revoke shares after
restoring an older database if old capabilities must remain invalid.

Verification covers SQLite persistence, fixed selections, rotated/revoked/
expired credentials, cross-share and owner-credential rejection, range downloads,
all HLS endpoint families, response cancellation, FFmpeg metadata stripping,
typed public-response parsing and browser creation/edit/revocation and playback.
Verify the public host denies `/graphql`, `/scene/1/stream`, `/assets/`, `/custom/`
and `/`, even with an owner API key or Authelia cookie.
