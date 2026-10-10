# Native archive implementation progress

This is the implementation record for the
[full transition plan](native-archive-transition-plan.md). It does not narrow
that plan's scope or replace its completion criteria. Development remains on
`v3-rewrite`; merge into `develop` requires verification and the owner's success
review. The native replacement and its schedules are running; observation of
scheduled work and independent restore verification remain in progress.

## Verified GIF image-to-scene transition — implementation, 2026-10-10

Schema 1000109 retains an explicit conversion between the old image UUID/file
lifetime and the new scene UUID/verified MKV. Automatic publication requires an
absent original GIF, unchanged admission fences, unique single-file ownership,
a fresh destination scene and the selected attachment or its retained original
file evidence. It retires only the stale database entries; the producer owns
the completed filesystem conversion. Ordinary cross-kind merges remain invalid.

Common metadata values, explicit clears, relationship choices and source/policy
provenance carry over. The old typed identity and field history remain retained;
photographer attribution remains distinct from director. Source choices, manual
and source gallery memberships and selected covers follow the scene. Explicit
attachment/post/gallery unlinks stay unlinked. Undated image counters retain
their counts without invented scene activity dates, including through later
merges, decrements and JSON import/export. The worker checkpoints the conversion
before previews and durable after-success notifications, so retry does not
repeat the domain mutation.

The full Go integration suite passed. Subsequent focused regressions cover
reviewed post provenance, relationship/custom-field transfer, real GIF/MKV
intake, original-file and ownership conflicts, transaction rollback, interrupted
completion, UUID adoption, explicit unlinks, migration and counter round trips.
The populated-copy rehearsal is running; no production writer or producer
runtime has been changed for this increment. Evidence is retained under
`.local/gif-domain-transition-20261010/`.

## Image and scene gallery covers — deployed, 2026-10-10

Schema 1000108 replaces the image-membership cover flag with one canonical media
UUID per gallery. The API and gallery cover URL resolve either an image or a
scene; mixed Media menus and the separate scene/image lists can select either.
Cover changes refresh only that gallery's cover fields. Reaffirmed membership,
canonical UUID adoption and same-kind merges preserve the choice when the
destination remains a member. Source-album updates protect selected scenes;
removing the selected member clears the cover.

The isolated 21.66 GiB copy rehearsal preserved all 16,314 galleries and gallery
identities, 541,975 image memberships, 3,610 scene memberships, 14,974 post links
and nine explicit covers. Semantic digests matched before and after migration,
and the migrated database reopened successfully. Historical fixture checks
caught a missing migration-history entry; the final statement was then applied
to the already verified copy and checked by fresh migration tests. The temporary
database has been removed; `.local/gallery-media-covers-20261010/` retains the
reports and the exact SQL distinction.

All Go integration tests, 805 UI tests, 680 producer tests, 317 backup tests and
the other pre-push suites passed. The final aggregate run's only remaining
failure was a test-helper resource-cleanup lint rule; that cleanup was corrected,
then lint and the focused migration/cover tests passed. The production data
migration was unchanged by that test-only fix.

Chromium's eight local gallery checks passed, including changing both cover
types, targeted refresh, retained pagination and retry after a failed mutation.
All six shards of the [GitHub browser run](https://github.com/notsafeforgit/stash/actions/runs/38041354341)
passed, including WebKit. The [source publisher](https://github.com/notsafeforgit/stash/actions/runs/38041354350)
and [wrapper publisher](https://github.com/notsafeforgit/stash-s6/actions/runs/38042395496)
also passed. Production runs source `9c6d507f633a257db99f73d808fb9814e858c030`,
wrapper digest `sha256:b8e15454972887d866f4ad0d6fb04a11cc6db79c0d6c2091f6512bd776a20da7`,
and schema 1000108. The pre-migration backup remains retained. Live gallery
#3664 resolves its scene cover and gallery #6274 its image cover; both return
HTTP 200 with JPEG data.

The application restarted without draining scrapes or pausing the worker timer.
n8n retained its process identity; the existing source run retained its progress
and was reclaimed with a new ownership fence and zero failures. The in-progress
backup had already sealed its immutable checkpoint, so its original process,
configuration and validator remain in place until that attempt finishes. A
lock-protected handoff selects the new validator for subsequent backups. The
independent restore retained its original process. This release does not yet
complete automatic GIF image-to-scene conversion or the overall transition.

## Dedupe discovery lock scope — deployed, 2026-10-10

The first resumed fclones pass exposed an overly broad lock: it held the backup
lock and every worker publication barrier while scanning the entire library.
The native client now holds only its own state lock during read-only discovery.
Each candidate separately acquires the backup/worker locks for preview, receipt
recovery and verified removal, releasing them between pairs. If backup wins a
later lock, the pending manifest survives and the next invocation resumes it
without repeating discovery. Stash's existing full-byte verification and file
lifetime checks still authorize every removal.

The sixteen focused client/HTTP tests pass, including real flock checks during
discovery and between pairs, blocked retry without the original report, and
recovery of a lost removal response. The backup already snapshots this journal
as an operating SQLite database; concurrent candidate-only discovery cannot
remove media across that boundary. The full fork gate passed in 200.91 seconds,
including 680 producer, 317 backup and 805 UI tests, Go integration tests, and
lint.

Commit `f3f4340a2` is installed in a separate versioned dedupe runtime; the host
scraper fingerprint and n8n image remain unchanged. Both shell launchers and the
systemd environment select that runtime. All 21 dedupe/host tests also pass
against the installed wheel, and the service returned its expected daily
cooldown without rescanning. The wheel, module digests and release selection
are declared for the next coordinated backup; this does not claim their cloud
publication has already finished. No application image was rebuilt locally or
restarted for this host-tool change.

The preceding live pass completed with 952 verified removals and 804
`file_requires_intake` review outcomes out of 1,756 candidate pairs, with no
pending requests. Its scan was allowed to finish before activation. The existing
cloud restore retained its original process identities and its I/O counters
advanced. Evidence: `.local/dedupe-lock-scope-20261010/`.

## Native schedule resumption — observing, 2026-10-10

The seven held host timers have resumed with their original cadences. Native
host ingestion and local-file intake are enabled, and the already running n8n
worker stayed running. All ten selected timers are active. Nine inactive legacy
catalog units are reversibly masked, with their original definitions retained.
The resumption checked the current deployment, all ten native service commands,
22 host profiles and the completed coordinated backup publication; it did not
reuse the older helper's obsolete runtime and inactive-n8n assumptions.

The first scheduled Twitter, Reddit and Instagram submissions succeeded. The
host dispatcher is admitting their queued work, local intake is advancing, and
411 media-verification jobs completed after resumption. The existing n8n source
run also advanced its downloaded-file checkpoint. These observations do not yet
establish completion of every newly queued profile or enrichment pass.

Dedupe initially deferred while another operation held its lock. The initial
scheduled backup encountered a busy backup lock; a later retry was blocked by
the resumed fclones pass, whose lock scope is corrected above. Backup remains
on its existing fifteen-minute retry. Its best-effort dedupe launcher
also lacked its execute bit; the installed launcher's mode is now corrected to
0700, with its contents unchanged. The independent cloud restore remains the
same live process and has not been restarted. Root free space is about 135 GiB.
Evidence: `.local/native-schedule-rollout-20261010/`.

## Browser regression fixtures — corrected, 2026-10-10

The fourteen failing browser cases came from expectations left behind by earlier
native changes. Source review now mocks the current deduplicated capture-history
response, verifies one displayed post description and observation counts, and
requires reads to remain lazy and limited to the selected post. Recovery of a
saved source-order choice verifies one gallery refresh after confirmation and
no repeated write. Retired-root tests now exercise explicit restoration to both
Active and Disabled while preserving the UUID, binding and revision history;
opening the root still performs no write or folder probe.

All 64 checks across the three affected suites pass in Chromium and WebKit.
The complete local fork gate passes in 37.89 seconds with its existing caches.
Only test fixtures/assertions and this record changed. The [GitHub publisher](https://github.com/notsafeforgit/stash/actions/runs/38035132447)
passed, and the registry image's revision matches `a768fb592`. This test-only
increment did not deploy an application image.

The [complete browser run](https://github.com/notsafeforgit/stash/actions/runs/38035132436)
initially had an additional WebKit clipboard-fallback failure. Its trace stopped
responding after Copy; on the retry, the correct error message appeared before
reopening the menu hung. Ten focused repetitions and the complete sixteen-check image-file
suite passed locally in the pinned Playwright container. Rerunning only the
failed WebKit shard on a fresh GitHub runner passed all 168 checks in 9.4 minutes,
and the full workflow is now successful. No assertions were weakened or source
changes made for that rerun; the earlier browser hang did not reproduce and its
trace remains retained. Evidence: `.local/browser-repair-20261010/`.

## Parallel native validation — implemented, 2026-10-10

The publisher and native PRs share one validation/build workflow. Go tests,
lint, Python suites and compilation run in separate jobs after their actual
prerequisites; the UI is generated, checked and built once. Native branch
pushes no longer repeat backend work in the older Build and Lint workflows.
Publication requires all selected jobs, with explicit rejection of failed,
cancelled and unexpectedly skipped jobs.

Frontend-only changes can skip backend/Python suites relative to a successful
ancestor publish. The comparison includes intervening failed/cancelled pushes;
unknown paths, tooling/schema changes and unavailable baselines run everything.
Embedded-entry tests and compilation always run. Real-Git and gate regressions
cover selection, deleted/renamed paths and publication failures.

pnpm now installs into its cached store. Go compilation caches advance per
revision and suite; Python dependency caches distinguish their installers.
BuildKit retains container-library layers independently of the application
binary. Manual publication can explicitly refresh those dependency layers.
The local full gate also runs independent suites concurrently and builds its
own validated UI before Go checks. A tiny backup fixture now models available
space instead of requiring 50 GiB in the test temporary directory; the actual
restore reserve remains unchanged.

The complete local gate passed in 399.64 seconds: 805 UI tests, 678 producer,
12 library, 146 archive and 317 backup tests, all Go integration tests/lint, and
workflow lint with ShellCheck. Final cache edits passed the CI checks again.
Evidence: `.local/ci-speed-20261010/`. This build-tooling change does not require
a production restart.

The first GitHub run exposed a missing cross-job dependency: Go's Python
interop/restore tests also need the installed producer runtime. The Go runners
now install it, and `make it` rejects a missing runtime before compiling or
executing the suite. SQLite tests now have their own runner, separate from other
Go packages; the complete tagged package list is partitioned without a maintained
package allowlist. Markdown deployment notes no longer defeat the next
frontend-only comparison. Fifteen CI-selection/partition/gate checks pass.
The corrected full local gate passed in 41.44 seconds, reusing unchanged Go
results. The actual 78-package inventory partitions into 3 SQLite and 75 other
packages without omissions or overlap; a missing-runtime check fails before
the Go command starts.
The independent browser workflow reproduced the same 14 failing cases as its
previous run; these remain separate UI regression work.

The corrected [GitHub publication run](https://github.com/notsafeforgit/stash/actions/runs/38032936438)
passed every required job at `3940a14f74046e5bb7e0769a6b6c9955ba68f7e5`.
Validation finished 12 minutes 25 seconds after the run started, and publication
finished in 18 minutes including the initial Docker cache population. Cached
backend generation took 4 seconds and Linux compilation took 3 seconds. The
registry image's revision label matches that commit; its digest is
`sha256:fec95e82cf66f160951e04786ff03ba9c3fb78dfd7bdd8607657c902ad9f976f`.
This documentation update also exercises the normal frontend/documentation-only
selection path against that successful baseline, without a skip-CI directive.

## GIF conversion provenance and restart recovery — implemented, not deployed, 2026-10-10

The producer now retains the configured GIF-to-MKV transformation with its
completed-file event, including the original GIF path and unchanged source
attachment reference. Admission freezes the original file UUID/generation and
removal fence as server-owned work. Published source-media evidence retains the
portable conversion claim. The claim is not a byte-equivalence assertion and
cannot select a Stash image to replace. Workers negotiate the new capability;
delivery defers unsupported conversion events without discarding their bytes or
blocking unrelated files.

A new interruption regression exposed a second gap: when conversion succeeded
before the completion/archive write, the retry tried to download the GIF again.
The adapter now recognizes the configured converter's final MKV in that case,
retaining completion and source evidence after reopening the outbox. Forced
downloads still work; an unrelated MKV without the converter does not count as
a completed GIF.

All 678 producer tests and the complete ingestion package pass. Focused native
tests cover admission/replay across a database restart, retained original-file
lifetimes, invalid claims, and a real
probed video with portable source evidence. The HTTP capability regression
passes. Private validation evidence is in `.local/gif-conversion-20261010/`.

The existing-image-to-scene domain transition is still pending. It must preserve
metadata choices (including explicit clears), identity/history, source links,
manual gallery membership and covers, and restartable after-success work before
removing the stale image/file entries. Do not treat the new transformation claim
alone as authority to bypass the media-kind conflict. A bounded live inspection
found three failed association jobs, all for the already repaired gallery #3664
attachment now linked to scene #373593; it found no other failed MKV association
in that queue. This increment changes no production runtime or database schema.

## Internal account migration identifiers — deployed, 2026-10-10

Account review now shows handles, service IDs and profile URLs without listing
imported `legacy_key` references as ordinary identifiers. The same projection
serves cards, expanded identifier lists, consolidation previews and pickers.
Filtering happens before pagination, so migration keys neither crowd out real
identifiers nor create a misleading additional-identifiers indication. Internal
lookup, import replay, consolidation and retained evidence keep those keys.

The full fork gate passed. HTTP regressions cover summary bounds, paginated
identifier discovery and retained internal lookups. Live desktop/mobile checks
confirmed the example Twitter account displays its handle and service ID while
its original migration reference remains in the database. No schema migration,
reimport, production data cleanup or new cloud backup was required. The update
restarted Stash without draining scrapes, restarting n8n or pausing its timer.

The [source build](https://github.com/notsafeforgit/stash/actions/runs/38024954204) and
[wrapper build](https://github.com/notsafeforgit/stash-s6/actions/runs/38026549490) published successfully.
Production uses their verified GHCR digests. Deployment and live evidence:
`.local/account-identifiers-20261010/`.

## Scrape restart recovery — deployed, 2026-10-10

Schema 1000107 separates archive-job failure counts from ownership generations.
Expired leases keep checkpoints and return to the queue without exhausting the
failure allowance. Download runs similarly retain their window/cursor and treat
lease expiry or worker interruption separately from source failures. Existing
terminal states and receipt histories are preserved. Genuine errors retain
bounded retries and source cooldowns; stale workers cannot publish.

The producer validates retries using the new failure count, including pending
local failure acknowledgements after many restarts. Job displays show failures
separately from attempts. Routine deployment no longer needs to drain a full
scrape pass or stop n8n. Recovery can wait for lease expiry, polling and existing
source cooldowns. A runtime fingerprint change still requires a bounded policy
handoff for pending work.

The full fork gate passed, including 805 UI tests, 673 producer tests and the
complete Go suite. Tests cover repeated database reopen/lease expiry, progress
retention, stale-worker rejection, real failure exhaustion and the real Python
worker resuming through an HTTP outage without downloading a completed file
again. A full database migration rehearsal retained 42,626 jobs, their attempts
and receipts, and source-run progress; the temporary rehearsal copy was removed.

Production was restarted with a scrape active, without draining the pass,
restarting n8n or pausing its worker timer. The interrupted run automatically
returned to the queue with its window, checkpoint and failure count intact.
The existing timer also resumed another queued profile. Verification did not
hold the runtime update for that profile pass to finish; same-run completion
without redownloading is covered by the real HTTP worker test.
The separate producer-runtime handoff then preserved queued run identities and
progress, installed the matching worker and resumed scheduling. Future backups
select the deployed application and worker recovery files; the previously
published backup proof remains unchanged. This update triggered no new cloud
backup or restore download.

The [source build](https://github.com/notsafeforgit/stash/actions/runs/38019838758) and
[wrapper build](https://github.com/notsafeforgit/stash-s6/actions/runs/38021342750) published successfully.
Production runs their verified GHCR digest. Private migration, deployment and
live recovery evidence: `.local/scrape-restart-20261010/`.

## Mixed gallery browsing — deployed, 2026-10-10

Gallery pages now open a unified Media view with images and scenes in selected
source order, including shared Twitter replies. Manually added items remain
visible, repeated source attachments share their existing gallery member, and
explicit exclusions remain excluded. A bounded GraphQL read reuses existing
membership and attachment selections; it adds no schema migration or catalog
reimport. The viewer plays both kinds across pages and stops on changed
membership/order. Images and Scenes tabs retain their normal list controls.
Cards include both kinds in their count and use a scene screenshot when there
are no images. Groups remain the separate movie/scene organization.

Focused SQLite tests cover ordering, gaps, repeated media, manual additions,
exclusions, video-only galleries, out-of-order thread arrivals and indexed
queries. Six Chromium regressions cover desktop/mobile mixed playback,
pagination, changed membership, failed-page recovery, video-only and empty
galleries. All fork-gate checks passed, including 805 UI tests and full Go
integration tests. The combined gallery/source-album Chromium suite passed
20 tests. Live API checks verified galleries #3664 (one image/one scene),
#4340 (eleven images/one scene), and #3594 (nine scenes), including paginated
counts and unique membership. Desktop/mobile checks verified the default
Media view and video-only contents without writing library data.

The [source build](https://github.com/notsafeforgit/stash/actions/runs/38015430425) and
[wrapper build](https://github.com/notsafeforgit/stash-s6/actions/runs/38016734820) published successfully. Production
runs their verified GHCR digest, recorded below, and scraper services resumed.
Checks used the direct production service; the hostname was already unreachable
from this host. No migration, reimport or new backup run was required.
Private evidence: `.local/mixed-gallery-20261010/`.

## Incidental post authors outside account review — deployed, 2026-10-10

Account review and the general review inbox now default to accounts with a direct
current source association or an ownership choice. Merely capturing an author
through a subreddit, saved feed, repost or another profile creates attribution,
not account review work. All known accounts remains searchable, with an explicit
incidental-author label. Adding a source or reviewing ownership automatically
includes that account. Source pickers and exact identity lookups still see all
accounts, including consolidation candidates.

This changes no schema or captured data. The production audit identified 100
incidental authors; the reported Herb-Anderson example came from the
ZeldaHentaiAI subreddit. Three additional accounts had real imported OnlyFans
folders with missing account associations. Those associations were repaired
through the native API, preserving their existing disabled metadata rules, so
they remain in tracked-account review. Live API verification found **102**
incidental authors outside the default queue: **277** tracked undecided accounts
out of **379** known undecided accounts. The example's two post publisher
associations remain intact. Desktop and mobile Chromium checks verified the
default search, All known accounts, and the original account deep link.

The [source build](https://github.com/notsafeforgit/stash/actions/runs/38010776665) and
[wrapper build](https://github.com/notsafeforgit/stash-s6/actions/runs/38012781179) succeeded; production uses their verified
GHCR digest, recorded below. No migration was needed, and the scraper services
resumed. All fork-gate components passed, together with 11 focused Chromium
account-review tests. Local WebKit execution was unavailable because the
Fedora host lacks the browser's required Ubuntu libraries.
The HTTPS hostname was unreachable from the deployment host before and after
deployment; API and browser checks used the direct production service.
Private release and verification evidence is under
`.local/incidental-account-review-20261010/`.
See [account review scope](native-source-collections.md#account-review-and-incidental-authors).

## One source per Reddit profile — deployed, 2026-10-09

Schema **1000106** consolidates **2,553** standard Reddit retrieval
definitions into **427 profile sources**, each with one canonical profile URL.
New/top/search passes retain independent coverage internally. New n8n subscriptions
create one source, and pausing a profile also gates its pending historical passes.
Old source links open the profile; its activity includes previous retrievals.
Subreddits, saved feeds and arbitrary searches remain separate sources.

The full-copy rehearsal and production comparison preserved original run,
request, attempt and source-definition records, metadata/translation policies,
and media associations, with no conflicting groups. The deployment used the
[GitHub source build](https://github.com/notsafeforgit/stash/actions/runs/38004612763) and
[wrapper build](https://github.com/notsafeforgit/stash-s6/actions/runs/38006980918) pinned by verified GHCR digests.
The published application's migration API saved its pre-migration database on
the media volume. Host/n8n scraper runtimes were updated and queued work retained
its original windows, progress and retry deadlines. The n8n service and its worker
timer resumed. Live API lookup resolves all six passes to lila-gw's one profile;
desktop/mobile checks verify old links open that profile's editor.

Validation passed: Go lint/integration gate, UI build and 805 UI tests,
670 producer tests, 146 archive tests, 12 library tests and 317 backup tests.
Backup tools support the new retrieval receipts. The current backup publisher
retains its sealed configuration until publication; its pending successor selects
this release and its matching backup tools.
Private release and migration evidence is under
`.local/reddit-profile-sources-20261009/`.
See [profile retrievals](native-source-collections.md#reddit-profile-retrievals).

## Historical Twitter filename recovery — 2026-10-09 UTC

The `legacy-twitter-filename-v1` album policy now implements recovery of missing
partial source lists from the archive's known original `<tweet-id>_<number>`
filenames. It follows imported file matches to surviving media, retains gaps and
unknown totals, preserves saved choices, and uses the existing durable album
worker and after-success notifications. It creates no duplicate source captures
or media. [Recovery semantics](native-source-albums.md#recovering-historical-twitter-albums)
describe the provenance and review boundaries.

A read-only production audit found **11,924** posts with album numbering:
**11,892** unselected posts with registered media evidence, **31** without that
evidence, and **one** with an existing source list. **343** candidate posts have
numbering gaps. These are candidates, not a claim that all will pass current
file/ownership and ambiguity checks. The example
`00005ea0-989f-5bce-888d-b6c9884a68f2` retains slots 1 and 2 for images
276637 and 276638. The private scope is saved under
`.local/twitter-album-recovery-20261009/`.

The GitHub publication restriction cleared for this push. Both the
[source build](https://github.com/notsafeforgit/stash/actions/runs/37994520342) and
[wrapper build](https://github.com/notsafeforgit/stash-s6/actions/runs/37997504208)
succeeded. Production now runs the verified GHCR wrapper below; the scraper
timer resumed after API readiness. The retrospective application completed all
**11,892** candidates in batches of 25 through durable album jobs, creating
**11,891 galleries** with **29,967 attachment/media links**. All submitted jobs
finished their after-success notifications; none failed or required conflict
review. One candidate retained only its first registered image, with its later
recorded files missing, so it remained unchanged.

**344 recovered lists** retain numbering gaps among registered attachments.
Every recovered list remains partial with an unknown total. The manifest readback
found no synthetic capture links. A live deduplication example verified that the
original filename supplies the position while the selected surviving file has a
different path. The original example is now gallery **6388**, with images 276637
and 276638 in positions 1 and 2. Existing selected source lists were preserved.

The separate 31 posts without registered media remain ungrouped: 12 have no
surviving local files, while 19 have 48 local files not registered in Stash (also
absent under the same basenames elsewhere in its file index). These exclusions
and the one unchanged candidate are recorded with the private recovery results.
The in-flight backups retain their original sealed configuration. Their pending
successor configuration selects the actual registry binary for future backups.

Validation passed: binding generation and UI build, Go lint and the full Go
integration suite, UI validation (802 tests), producer/library/archive tests,
and backup tests. One backup fixture needed its temporary destination on the
media volume to satisfy its reserved-space check; the isolated rerun passed.
Focused SQLite regressions cover original paths, deduplicated/repeated survivors,
numbering gaps, ambiguous candidates, stale previews, existing choices and
durable job replay. Chromium desktop/mobile review and policy-switching checks
also passed.

## Current release position — 2026-10-10 UTC

Production is healthy on schema **1000107**, source
`4e1c7c8ce3e6a764b425ab7926182abc7690d3c4`, pinned wrapper
`ghcr.io/notsafeforgit/stash-s6@sha256:c1633d72e6b4dadfab373bb2976503331dde52548d8a2349d650db38ced77c47`.
The verified source image is
`ghcr.io/notsafeforgit/stash@sha256:8be4c8b0aeca77b4997248ac3a8391efb29568763edf025f9d4ce6f2d5be0a1e`,
with wrapper source `4f0f0594e95e000a75e4819f2494c3a44ce7f04d`.
The current application release is recorded under
`.local/account-identifiers-20261010/`. The worker remains on the release recorded
under `.local/scrape-restart-20261010/`, and its timer is active. The earlier
mixed-gallery release is recorded under `.local/mixed-gallery-20261010/`, and
source consolidation under `.local/reddit-profile-sources-20261009/`.
The coordinated post-write backup has published successfully. Future backups
now select this application and worker release, preserving the original sealed
publication evidence and the unchanged backup serializer runtime.
The explicit [catalog association repair](catalog-association-repair.md) now
promotes unambiguous imported post-account claims and known author directories
to ordinary publisher decisions. Accounts without an ownership choice can link
to a unique existing performer name or alias. Conflicting authors, ambiguous
performers, saved/subreddit directories and existing choices remain protected.
Publisher ownership does not add depicted-performer tags to media. The example
post `0000e604-6fd6-5f99-8512-ad9409be43f6` now exposes its Reddit account and
the existing CuteLilAsya performer link; live desktop/mobile checks passed.
The repair completed all **266,597** imported posts in batches of 25 through the
admin API, creating **208,188 publisher decisions** and **869 account-to-performer
ownership decisions**. Existing choices were preserved. A follow-up recognized
retained mirror-account usernames when their display labels were numeric IDs:
those 25,252 posts already had publisher links, and resolving the aliases enabled
26 additional ownership links, included in the total above. The targeted retry
also checked one representative post per previously unmatched owner account.
The live example `000a6a0b-dc74-577d-9a0c-0d3ae391ea9e` resolves the retained
`innocentbeautypremium` identity to the existing Kayla Kapoor performer (#401).

The final report leaves 8,278 posts without usable imported author evidence,
207 with conflicting folder/account identities, 80 from a saved-feed directory,
five with oversized capture scopes and seven with conflicting captured authors.
There are 92 considered accounts without a uniquely matching existing performer.
These outcomes do not remove existing source links or prevent later manual
review. Receipts and unresolved matches are recorded under
`.local/catalog-association-20261009/` and `.local/catalog-alias-20261009/`;
the authoritative links and ownership history live in the normal Stash database.

All fork-gate components passed for the initial association change. The backup fixture suite used
disk-backed scratch for its free-space requirements; Go integration tests were
rerun in `/tmp` after the disk-backed test phase was interrupted for excessive
filesystem latency. The complete Go run passed there. The mirror-alias follow-up
passed the publisher/ownership SQLite and API suites and Go lint; its regression
failed before the fix. No database migration was
needed for this application update. The worker timer resumed after readiness,
and the pending future-backup runtime selector now targets this release while
the running backup retains its original sealed configuration.

New Twitter captures retain exact conversation, parent-post and publisher IDs.
The **Thread and replies** section provides local links or source-site links for
missing ancestors. Same-account self-replies share one gallery while retaining
each post's metadata and attachment order. Manual exclusions, disabled/deleted
galleries and conflicting identities remain protected. No historical thread
reconstruction ran; the migration preserved all 2,928 existing post-gallery
links. Existing database-opening checks accounted for most deployment downtime.

The host and n8n producer adapters preserve the numeric reply-target account ID
before gallery-dl transformation. The new installed producer matches all 127
source modules and the staged fingerprints of 22 host and 10 n8n profiles.
Compatible execution-policy approvals preserved progress and retry times for
42 queued runs and six metadata policies. The n8n worker timer resumed after
server and producer verification; other previously held schedules remain held.

UI validation passed 801 tests, with 50 Chromium/WebKit source-post/album browser
checks. Producer, library, archive and backup suites passed (668, 12, 144 and 317
tests). The full Go run exposed only a missing-table diagnostic ordering
regression; that was corrected and the affected lineage/thread tests passed.
A populated shared gallery also survived reopening without duplicate membership.
Live desktop/mobile checks verified the new endpoint and section, no horizontal
overflow, and no fabricated relationship on an old imported post.

The matching static recovery binary, producer wheel and exact worker image are
retained. A replacement one-time backup-config updater selects this release for
future backups after the current post-write publication completes, under the
existing backup lock. The running publisher and original independent restore
retain their original configurations. Other schedule-resume checks now also
require that matching future backup configuration to be selected.

Gallery detail pages now distinguish Source-post album, Folder gallery, ZIP
gallery and Manual gallery, with parent-post links or backing paths above the
cover. The summary shares its bounded lookup with the Source albums tab; failed
lookups do not imply a manual gallery. All 28 Chromium/WebKit source-album browser
checks and 800 UI unit tests passed, along with UI validation and embedded-asset
checks. Live desktop/mobile verification of gallery 6274 confirmed its Reddit
parent link, working Source albums action and no horizontal overflow. The native
n8n worker timer resumed after deployment. No membership or metadata was changed.

Schema 1000101 consolidated 37,434 redundant post versions while preserving
source text, media links, first/last sightings and repeat counts. Schema 1000102
makes collection and media-root retirement reversible through a new revision;
the UUID, media associations and prior history survive restoration. The UI now
allows editing retired entries and explains active, disabled and retired states.
Explicit source recovery can restore a retired collection; automatic source
registration never silently reactivates a paused or retired one.

All 3,090 targets matched from the live gallery-dl lists were already active.
The confusing disabled entries were imported catalog groupings, distinct from
the URL-bearing scrape targets. A reviewed repair activated 138 such groupings
with exact migration-generated state, an active root and an active listed
target for the same account and directory. This includes the `lila-gw, reddit`
grouping. Before schema 1000106, its six retrieval definitions were enabled;
there were no native run records attached to those definitions, so this did not
establish scrape execution. They now appear as one profile source. Unrelated
disabled or retired entries were left unchanged. Desktop and mobile checks passed.

The n8n producer now runs immutable image ID
`c2e1344642a3432031adf74ec2fbfa62aa6aeee94bac84f934bd8e5dddf477b2`.
Captured Reddit Redgifs/direct-file references can identify their downloaded
attachments, including retained Reddit fallback previews, image permalinks and
legacy `v3.redgifs.com/ifr/` links. Image permalinks retain their original URL
identity; only matching captured API item IDs and observed rendition URLs can
resolve the download. Unsupported external
gallery membership is not inferred from filenames. The QSV GIF helper pads odd
dimensions before encoding; temporary copies of both failing library GIFs
converted successfully and the originals were unchanged. The host producer
runtime and eight helper-asset bindings were updated together.

External hosting is not an exclusion policy. The earlier Reddit failures were
attachment-matching gaps for linked Redgifs clips/images and a legacy iframe
hostname. Both installed host and n8n runtimes pass all 29 shared external-media
fixtures. Previously rejected controlled host and n8n windows subsequently
completed with four and 47 media files respectively. A live check found no
current `source_rejected` run; recent Redgifs API errors report deleted source
items. This does not establish support for every external album format or
completion of the remaining source queue.

Schema 1000103 adds owner-approved execution-policy upgrades for queued or
deferred source runs. This preserves their original admission, UUID, coverage,
progress and retry deadlines. All 38 current pending n8n runs were upgraded;
the ten attachment-error deferrals were reopened through the existing review
API. The subsequent metadata-worker release upgraded all 42 then-pending runs
to its matching producer, preserving their original policy, progress and retry
deadlines. The 169 imported review holds remain unchanged.

Schema 1000104 allows compatible metadata-worker repairs for an exact original
profile and job kind without rewriting admissions or listing digests. Claims
require the approved execution policy; each attempt retains the policy it ran.
Four deployed approvals cover the existing Reddit/Twitter metadata profiles.
All 71 metadata job records and four listing definitions were unchanged by those
approvals. The Reddit discovery initialization repair is deployed: it attaches
the page interceptor after the actual extractor creates its API. Both host and
n8n installations match all 127 source modules. The unused old-runtime metadata
worker definitions and their backup entries were removed with local preimages.
The final hostname repair approved 46 pending source runs and five metadata
profiles for its matching runtime. All 72 metadata job records and four listing
definitions were unchanged by these approvals; the 169 imported holds remain.

The current release passed 668 producer tests, focused backend queue, migration,
API and Python HTTP integration checks, and the pinned backend lint gate. Tests
include retaining checkpoints across an upgrade, migrating existing attempts,
anonymized exports, and recovering a staged page under a changed worker without
refetching it. n8n and the main worker timer are active. The first repaired
Twitter listing attempt used its approved policy but retried with
`extraction_failed`; its new cooldown is retained. The first repaired Reddit
listing attempt reached a terminal `not_found` result. These are actual attempts,
not proof of successful source completion. A later real metadata-only job
completed with one captured record and four existing linked media items. Its
priority and retry deadline were preserved, and it created no file-completion
events. The queued enrichment jobs still fill their admission limit between
executions.

Manual folder discovery now filters root-wide scrape targets by direct folder
ownership before applying the scope limit. Previously, 3,138 unrelated targets
could block a local folder with HTTP 503. The changed lookup passed regression
tests with 300 unrelated sources, source-folder protection and the existing
manual intake/API tests, plus backend lint. The deployed lookup allowed the
reviewed extensionless Matroska file to be renamed with `.mkv` and ingested as
scene 373319. Its bytes are unchanged, its title comes from the filename, it
remains unorganized, and it has no fabricated source-post association. Replaying
the same admission returned the same completed job. This backend-only release
retains schema 1000104 and the deployed producer image.

The two existing performer-folder defaults also passed read-only scene/image
draft previews against their protected performer assignments. Twelve actual
host launchers/environment files were found still selecting the frozen producer
directory; they now select the verified current runtime. Their arguments,
source lists and schedule definitions are unchanged, and all twelve files are
included in the existing backup inventory.

Controlled source ingestion passed through both launch paths. n8n completed a
saved scan window with 47 media files, 224 acknowledged events and 73 captures.
The first host window was legitimately empty. A second already-queued account
exposed the legacy Redgifs hostname bug; after repair, that same window completed
with four media files, 24 acknowledged events and seven captures. Read-only
checks verified receipt digests, native file/media links and sampled live bytes.
These are completed sample windows, not a claim that the remaining scrape queue
has finished. n8n and its worker timer are active.

The translation worker is enabled and has completed real provider-backed jobs.
Bing sometimes returns valid translated text without detecting a source
language; the parser now accepts that result while retaining an unknown language
and the exact original text/hash. Generic input reproduces the provider behavior,
and two actual completed results passed read-only preservation checks. Provider
failures retain their normal retry deadlines.

The final dependency inventory resolves 32 worker profiles and 258 explicit or
discovered components, plus the publisher's generated records. The local-only
n8n and task-runner images now have verified OCI exports included in future
backups, totaling 718,808,576 bytes. Unchanged exports are reused; recovery can
load the exact image IDs without relying on intermediate local build images.
The installed publisher uses 4 MiB SQLite chunks and a 50 GiB free-space reserve.
The post-write incremental backup has started with its normal coordinated
capture and publication locks. It has not yet produced a completion receipt.
No second full restore was started.

The remaining rollout includes confirming that backup publication, reversibly
masking obsolete catalog units, and resuming/observing the held schedules. The
independent baseline restore is still running. No merge into `develop` or change
to the frozen compatible release has occurred.

## Earlier cutover and NFO cleanup — schema 1000100

The NFO-to-catalog import and subsequent catalog-to-native import are already
complete. The requested NFO work removes redundant storage from that completed
import. Schema 1000100 and the explicit offline `cmd/nfo-cleanup` command now
collate original text, translations, dates, URLs and performer/studio defaults,
then discard XML, document paths, duplicate parsed payloads and per-document
import bookkeeping in one transaction. The completion marker is per catalog
snapshot. Existing capture/post/media identities and native policies survive;
no per-NFO retirement ledger is created. A strict recovery path handles the old
writer's unescaped text without treating it as arbitrary XML. Recovered posts
are matched to existing media only through unambiguous current file matches.
Saved display translations become references to shared native translation
results, retaining unknown language/provider values. A saved choice of original
text needs only a null reference. Known default title/details mappings receive
new native policy revisions; their history and all other settings stay intact.
Custom expressions that still depend on discarded NFO inputs stop cleanup.

Production cleanup and deployment are complete on schema 1000100. It removed
272,556 NFO documents and their per-file bookkeeping, normalized 414,561 captures,
scrubbed 686,599 redundant staging payloads and compacted all 1,711 catalogs.
Recovery retained 520 malformed documents as ordinary post metadata and linked
1,334 existing media associations; 11 old file references had no current match.
The 4,817 default policies now select shared native translations. Every original
NFO-derived post metadata record matches the pre-cleanup digest, and all four
library counts are unchanged. The temporary 23.3 GB rehearsal copy is removed.

A subsequent live check confirmed all eight NFO document/bookkeeping tables and
the NFO staging rows are empty, with all 1,711 catalog snapshots compacted. Two
policies created after that cleanup still used the old default NFO expressions;
their scene/image title and details mappings now use the same tested native
translation expressions through revision-checked API updates. No current policy
references the discarded NFO inputs. No additional document deletion or deployment
was needed; original post text, translations and media associations remain.

The final copy passed native snapshot verification and semantic reconciliation.
Backend lint and tests passed except for a second-boundary race in the intake
response-loss fixture; its explicit settled clock passes three reruns. Focused
tests cover capture-specific saved translations, original-text choices, policy
history, rollback and database reopening. Source `2337eef9e` is running from
`localhost/stash-native-s6@sha256:ea42c1891b270f25b17d6a8d9da98e991b62f32509e263c458e19c55209f247b`,
with a healthy container and API. The subsequent `c34f25177` changes only that
test fixture. The matching backup validator and runtime/reconstruction components
are installed for future backups; the sealed baseline and its validator remain
unchanged. This deployment has not yet received its post-write cloud backup.

GitHub reports Actions enabled and the publishing workflow active, but dispatch
returns HTTP 422 claiming Actions is disabled. This API failure does not prove
the repository setting is disabled. Image publication is pending; the live
deployment uses the tested local image, and its binary and pinned runtime-base
reconstruction files are included in the next configured backup.

All 1,711 catalogs and 925,869 automation records are imported and reconciled.
Post/media associations, source albums, disabled policies and source URL
registration have passed their independent checks. The original compatible
database and held source snapshot remain intact. The production application is
healthy on the existing Stash port; scraper schedules remain paused.

The first coordinated native backup has sealed and packed its checkpoint with
the 22.2 GB database, 240,872 saved artwork files and coordinated host state.
Its streaming verification, native archive upload, final cloud manifest,
provider releases and local cleanup are complete. The publication gate passed
at 10:47 UTC. The independent restore is running separately and does not block
rollout. The first attempt failed at the media-view guard; the fixed publisher
resumed and completed the same sealed generation without repeating its audit or
upload. The failed attempt and its evidence remain intact.

The missing translation executable is fixed, published and verified in the
exact selected wrapper. The n8n dependency, translation image, home-backup and
daily-backup launcher overlays are installed, with their workers still inactive.
The home-backup repair passed eight fixture tests and read-only copies of all
19 worker/archive databases. Final inventory confirms all 32 worker profiles,
95 installed files and 209 effective backup components after the launch cleanup
and native credential provisioning. The sealed baseline backup is unchanged.

The live media root is bound, two ingest workers have native credentials, and
the six local intake changes preserve the two existing folder performer defaults.
Queue handoff activated 202,561 enrichment targets, 177,320 translation targets,
544 discovery targets and 207 saved scan plans. Review holds, retry deadlines
and retained progress remain preserved; no scrape or background job has executed.
The private application completed startup. Database promotion passed in 96 seconds,
including the closed WAL checkpoint and an inode-preserving move into the final
configuration directory. Production startup passed at 11:27 UTC in 129 seconds.
The initial cutover reported schema 1000099; the NFO cleanup release now reports
1000100 with the same 273,546 scenes, 503,711 images, 4,197 galleries and 1,440
performers. The application uses the promoted database
and live media directory, with no old catalog mount. "Public startup" in the
cutover records means the normal production Stash service, in contrast to the
temporary localhost-only migration instance.
Ordinary startup still performs the full domain audit. The separately saved
optimization patch has not been tested or deployed.

Separate steps are prepared for controlled host/n8n/manual/enrichment checks,
native translation startup and job verification, and a fresh post-write backup
and isolated restore. After the controlled checks and confirmed post-write backup
publication, the old catalog services may be reversibly masked,
seven existing timer cadences resume, three native worker timers start and the
two saved n8n parent versions be published. Actual scheduled cycles, retirement
verification, home-backup capture/upload and desktop/mobile owner acceptance
remain. `develop` has not been merged and the frozen compatible release is unchanged.

### Separate application audits from publication — 2026-10-09

A completed native archive publication can now resume finalization using its
saved remote verification record, upload receipts and a fresh object listing.
It does not recapture state, reconstruct databases or repeat the completed audit.
An expired ZFS snapshot mount is accepted only after rechecking the original
snapshot GUID, creation transaction, checkpoint properties, hold and read-only
topology. All 317 backup tests pass, including missing-object repair, altered
remote proof rejection and snapshot revalidation failures. The failed controller
and its original publication receipt remain intact. Finalization on `e9cfa08a9`
reused that publication and completed the master and provider releases. The same runtime is selected for the prepared
daily launchers, with 13 fixture, five CLI and six schedule-guard checks passing.
Use `install-finalization-launch-cleanup.py` after the n8n/home overlays; this
supersedes the earlier capture-runtime preparation.

The prepared post-controlled backup helper now expects `captured-contents`,
matching the selected deployment. Its stale `streamed-contents` assertion would
have rejected the new runtime before starting a backup. Independent restore
still requires `isolated-restore`; no active process or archive was changed.

Capture evidence reuse now also removes the repeated reconstruction and
SQLite/producer audit from new backups. The exporter records its successful
checks against the exact packed hashes before publishing the archive manifest.
Publication validates that evidence and the media/checkpoint binding, then
verifies new or changed upload bytes and reuses verified remote objects. The
proof says `captured-contents`, without claiming an application audit or restore.
Older sealed archives without that evidence retain the streaming fallback; an
invalid record fails rather than silently changing verification methods.

All 144 archive tests and 313 backup tests pass. New coverage compares capture
evidence with independent streamed verification, publishes/retries without
creating verification snapshots, rejects altered evidence and corrupted upload
bytes, and performs an independent download/restore. Failed evidence persistence
cannot publish a success manifest. Initial capture and packing still read the
selected inputs; the running baseline is unchanged. The `87638e5b0` runtime is
installed and verified inactive, with 13 launch fixture checks, five actual CLI
checks and six schedule-guard checks passing. Use `install-capture-launch-cleanup.py`
after publication and the n8n/home overlays. It supersedes the earlier launch
preparations, preserving all 209 backup components and worker/profile bindings.

The full native validator repeats SQLite integrity and foreign-key checks, hashes
the database twice, and scans native relationships, provenance and ingestion
history. Requiring that audit before each upload is excessive. The host publisher
now verifies archive contents, all SQLite components and the producer boundary
without invoking that additional application audit. Its scoped `sqlite_snapshots`
evidence identifies the exact checked components and does not claim native audit
success. Explicit audits and cloud restore drills still require a successful
native validator report. Existing immutable full-audit publications remain readable.

All 141 archive tests and 310 backup tests pass. They cover publication without
an audit, corruption with otherwise valid transport checksums, missing or changed
SQLite evidence, historical proof reads, and a subsequent restore whose native
audit can fail independently of the successful publication. Full archive reads,
SQLite checks and historical producer scans remain; this change does not finish
the broader recurring-cost work.

The `37331d454` runtime is installed separately and verified inactive. All 162
installed modules match the tested source, and the installed publisher omits the
native validator while the explicit restore command still invokes it. The new
launch preparation passes 13 fixture and five actual launcher checks, retaining
the same 209-component configuration and all 32 worker profiles/79 private
bindings. Six schedule-guard checks select this runtime for future backups.
That preparation is superseded by the capture-evidence runtime described above.

The independent restore gate now explicitly requires the native audit as well
as `isolated-restore`, with 12 gate tests passing. It still rejects the real
unfinished baseline for rollout. The running baseline uses its original
installed runtime; its validator has exited and the publisher remains active,
with no publication receipt as of 09:46 UTC. No running publisher, installed
launcher or schedule was changed.

### Owner-approved publication gate — 2026-10-09

The owner explicitly permits rollout once the backup is confirmed, while the
separate cloud restore continues and can be repaired during live operation.
The new operator gate binds the publisher's completion and provider-release
receipts to the selected run, checkpoint, configuration, master and inventory.
The rollout helpers now consume a publication receipt, independently of restore
success. They still require completed migration/reconciliation, their existing
runtime checks, and the applicable completed backup before any rollout action.
The current packed-but-unpublished backup remains ineligible.

The original publisher was stopped with the owner's approval, and its controller's
actual interrupted result is retained. A new controller resumes the same sealed
run using streaming verification, with the original configuration and archive.
A separate restore receipt requires the restore child to exit
successfully and validates its output against the downloaded archive's native
database and producer receipts. Neither a running child nor a failed restore is
reported as successful. Original snapshots and recovery copies remain protected
until restore verification passes; complete restore coverage and owner acceptance
remain transition completion requirements.

Eleven publication-gate tests pass, including mismatched receipts, unfinished
publication, independent restore failure and successful restore-child handling.
The revised launch helpers also pass their ten fixture and five launcher checks.
A read-only check against the actual unfinished run rejects rollout and creates
no publication receipt. The fifteen updated rollout helpers retain their prior
versions for recovery; no installed configuration or running service changed.

### Routine backup verification cost — 2026-10-09

The first publisher is doing a full local restore before upload: it reconstructs
240,872 artwork files, syncs every file and parent directory, then rereads the
artwork. That path was also selected for every scheduled native backup, making
routine verification unnecessarily expensive. The owner authorized stopping that
publisher and continuing the same sealed archive with streaming verification.

The daily publisher now selects streamed content verification. It checks every
compressed/raw chunk, complete artifact hash/size, artwork MD5 and exact artwork
inventory, while materializing only SQLite components for full integrity,
foreign-key, identity, native binary and producer-receipt checks. Its proof
explicitly records `streamed-contents`. Explicit local/cloud restore commands
retain full reconstruction and durability checks and report `isolated-restore`.
Historical publication proofs remain readable. This changes neither the daily
host S3 schedule nor media storage classes and adds no routine cloud download.

All 138 archive tests and 305 backup tests pass. The backup suite used temporary
directories on the workspace disk because this host's 31 GiB `/tmp` cannot meet
an existing restore fixture's 50 GiB reserve. The original failure is retained;
production reserves were not reduced. Coverage includes corruption in streamed
artwork/configuration, missing artwork, database identity/integrity failures,
exact producer proofs, no artwork materialization, historical proof reads and a
stream-verified publication followed by an independent full restore.

The new package is installed as an inactive versioned runtime. The prepared
launch cleanup now routes the existing daily service and three manual commands
through it and updates the inventoried release record. Its 12 prepared files
retain the original preimages, and the backup configuration still has 209
effective components. Thirteen fixture checks and five actual launcher checks
pass; the installed daily module uses streaming and the explicit restore module
uses full reconstruction. The future controlled backup selects that same reviewed
runtime. The cloud-restore receipt gate explicitly rejects streamed-only proof.
Installation of these daily/manual launch changes still waits for the current
publication. The current one-time publisher already uses the new runtime and
the sealed generation is unchanged.

A same-generation resume is running for the current publisher. Read-only checks
confirmed the retained server checkpoint, exact packed manifest/inventory and
unchanged configuration; the installed journal resumed copied identity records
without creating a new generation. Five stop/resume guard tests pass. Automatic
approval review initially rejected sending the graceful stop signal without
explicit owner authorization. The owner then approved it. Both original processes
exited, the backup lock was released, and the streaming publisher started at
08:24:47 UTC. Its log confirms the original run and checkpoint; the packed
manifest/inventory and active journal are unchanged. The original controller's
exit-code-1 result remains intact. Publication is still pending, and rollout
continues to require confirmed publication and provider release.

The owner's follow-up identified a remaining recurring-cost problem. Streaming
removes artwork reconstruction, but the current daily path still repeats full
SQLite/application validation, historical producer-receipt checks and complete
archive reads. Packing already checks the input and encoded content, and upload
hashes encoded objects again. The streaming change alone therefore does not
complete the backup-runtime performance work. Routine capture must keep a
consistent database/producer boundary, expected-component coverage, immutable
object identity, transfer checksums and publication ordering. A lighter routine
path should reuse verified unchanged content and capture evidence, with explicit
full audits and restore drills preserving deeper validation. The initial
migration/reconciliation checks remain separate from this recurring policy.
The broader routine/audit split remains unimplemented. A first source improvement
now skips the upload-stage local reread when a previous full-checksum receipt and
a fresh S3 listing identify the same remote bytes. It still clears retirement
tags before reuse and validates the local replacement if expiration wins that
race. Missing, changed and unverified objects retain full input validation;
explicit audits and restores retain their independent checksum checks. All 308
backup tests pass, including corrupt replacement files and expiration races.
This improvement is not installed in the current runtime. It does not change the
running baseline backup or the owner's confirmed-publication rollout gate.

The owner also clarified that nightly database uploads must be incremental.
The initial archive's 40.2 GiB includes original photos/covers and operating state;
the database itself is 22,244,438,016 bytes, encoded as 5,817,898,827 bytes in 332
64 MiB chunks. Publication already shares identical objects between snapshots,
but the earlier production-size rehearsal uploaded 188 MB after one title edit.
The coarse database chunks therefore amplify small changes unnecessarily.

The source now uses 4 MiB chunks for SQLite components, retaining larger chunks
for other files and the existing portable format. A bounded SQLite/fake-S3
measurement uploads 4.2 MB after one SQL edit versus 16.8 MB with the old chunks;
an unchanged retry uploads zero database bytes. Both snapshots restore correctly
with reused objects. All 139 archive and 308 backup tests pass. The first archive
suite invocation used the host-only environment and could not import the producer
test dependencies; the complete suite passes in its intended producer environment.
No full production database copy, cloud request or live write was needed for this
regression measurement. These results are not measured nightly production churn.

The tested package is now installed as a separate inactive runtime. A separate
launch preparation routes the daily service and manual backup commands through
it, with 13 fixture and five actual launcher checks passing. All 209 planned
backup components and original preimages remain covered. Use the incremental
launch installer after publication and the n8n/home overlays; it supersedes the
earlier streaming-only preparation for future jobs. The original resume helpers
are retained unchanged for the running baseline. No command or schedule has
been switched yet.

The schedule-resumption helper now requires that exact incremental runtime,
its reviewed module hashes, deployment/release records and effective daily
service definition before opening any timer barrier. Six fixture checks pass,
including rejection of the previous runtime, a mismatched preparation, missing
module coverage, an old release selection and a changed daily service. The helper
has not run against live schedules; the baseline publication is still pending.

The current sealed baseline keeps its original bytes and runtime. Switching chunk
sizes requires one new database baseline, then shares its unchanged chunks across
later backups; unchanged photos/covers need no new baseline. The existing Google
Drive/rclone backup remains part of the independent backup plan. The broader
routine-versus-audit cost split is still outstanding.

The Standard download was measured at 40.2 GiB across 241,410 unique content
objects, plus three metadata objects. One HEAD and GET per object yields about
483,000 requests, approximately $0.19 at the current Oregon rate. Internet
transfer depends on the remaining shared monthly free allowance; the drill
does not thaw or download the Deep Archive media. It verifies native state and
its media mappings, not retrieval of every cold media byte.

## Earlier production cutover records — 2026-10-08 UTC

The production maintenance boundary is active. Compatible Stash, n8n, Redis,
scrapers and their related schedules are stopped behind reversible admission
gates. The public native replacement has **not** been activated. The compatible
release, original databases, workflow state and held media snapshot are retained.
Development remains on `v3-rewrite`; merge into `develop` requires the owner's
success review. Historical entries below record the evidence available at their
dates and are not the current task list.

The selected application is schema **1000099**, source `0ea2c05fb`; the producer
and backup packages are `d863a41af`. The exact source and wrapper images are pinned
in [release verification](native-schema99-release-verification.json). Complete
source checks, populated migration/startup/restart, browsing during ingestion,
original-row preservation, installed worker verification and the bounded real S3
publication/restore passed. These checks do not replace the current-data cutover.

The fresh compatible production database has now migrated separately to schema
1000099 in 108 seconds. Independent reconciliation verified **8,290,833 unchanged
typed rows**, all **1,941 performer names/aliases**, **83 selected saved filters**
and the original fork history, with clean integrity and no foreign-key violations.
The original compatible database is unchanged. The exact native image is running
on a private localhost endpoint against this candidate, with external workers off.

Current imports have preserved the saved performer UUIDs and merged identity,
**1,237 source accounts** and **1,711 catalog groupings**. Nine resolved saved
account links were restored; one nonstandard account label remains reviewable.
All **575 scan-journal records** and **1,350 permanent backfill decisions** match
the original evidence. No source jobs have been activated. Two inactive producer
identities and their staged outboxes retain all **34 legacy n8n result receipts**
byte-for-byte, without treating them as native ingestion-completion proof.

All 1,711 catalog databases and the automation database have completed frozen
input preparation. The original copy is complete, including all **240,872 saved
photos and covers**. Native imports are running through durable services.
Completion requires agreement between the imported database slice and the full held boundary, followed by
independent source-byte and domain reconciliation. The same snapshot remains
held and mounted read-only; the original compatible database is unchanged.

The private candidate now uses SSD storage behind its original logical path.
The closed database and search files were copied and checksum-verified, with the
original directory retained and SQLite checks passing. Recovery corrected an
unavailable automatic ZFS snapshot path and the temporary container supervisor's
lifetime. The replacement private container uses the existing stable read-only
mount of the same held snapshot. Import resumed from all 4,563 saved phase
receipts with unchanged snapshot identities; the dependent services were restored
without repeating completed work. Final promotion must move this candidate on
the same SSD filesystem rather than allocate another full database copy.

Current settings match the rehearsed conversion exactly: 38 retained layered
values and 27 effective settings. All seven explicit folder defaults resolve
against the current library. A fresh assessment qualifies 516 catalog groupings
as local directories; 49 nonportable historical paths remain reviewable.
Post/media matching, source albums and policy conversion have dependent services
prepared to use current imported records. Their preparation does not constitute
completed domain migration or policy activation. A dependent service will retain
the converted policies through the API and independently compare their receipts,
leaving them disabled for the final scope and worker handoff.

Frozen subscription lists and interrupted scan evidence identify **3,106 exact
source URLs**: 2,566 Reddit, 500 Twitter, 39 Coomer and one Instagram. Registration
is sequenced after policy reconciliation, with account links resolved through
current native identifiers and existing source metadata defaults. It leaves the
canonical root disabled and starts no jobs. The **207 interrupted scan groups**
retain their original windows, retries and deferrals. One group has a unique
usable checkpoint; five retain multiple candidates for an explicit replay
decision. These prepared inputs are not completed source registration or scan
activation.

All 207 interrupted scans now have frozen recovery bindings against the installed
profiles: 131 host Reddit, 69 host Twitter and seven n8n full-history Reddit
requests. Independent expectations preserve the original lower bounds, retry
deadlines and deferrals: 38 can queue normally and 169 remain deferred. One
checkpoint is retained; 206 requests explicitly replay their saved windows,
including the five ambiguous checkpoint groups. Activation waits for the verified
live root and source registrations. A read-only dependent service will also
review current enrichment, translation and discovery holds against the final
imported scopes before any operational activation.

The final inactive deployment's **78 files** and **32 worker profiles** are now
installed with their original preimages retained. Both producer outboxes are at
their final paths, including the 34 original n8n receipts; producer credentials
and final runtime environments remain pending. The **nine workflow replacements**
have been installed in n8n's production database while its runtime remains
stopped. The original workflow conversion preserved 142 tables, 18,650 rows and
all 34 unrelated workflows. Installation included committed WAL and independently
matched all **148 tables and 19,737 rows** to the reviewed, paused candidate.
The original production database and its sidecars are retained. Actual publication pointers must be
verified after controlled startup. The pinned runtime uses regular execution
mode; retained running executions follow crash recovery, with no queued/waiting
legacy executions eligible for restart. A fresh final Stash configuration is
prepared from the current candidate, preserving authentication, StashDB settings
and its published default-filter migration checkpoint; installation is pending.
The backup configuration includes both n8n
and Redis in its brief container pause and retains Redis's complete AOF directory;
the captured queue passed the existing Redis runtime's AOF validation. No runtime
code change or artifact rebuild was required. AWS access and metadata retention
configuration are verified; bucket versioning remains unchanged.

Two parent n8n queue-dispatch schedules are now paused in the installed database
using the pinned offline CLI. Only their activation fields and update timestamps
changed; all 41 other workflows and the values in 147 other tables remain
unchanged. The nine converted workflow versions and their pending
publications are preserved. Startup dependency review finds no active scheduled
scrape trigger; five old direct gallery-dl workflows are already archived and
inactive. The two dispatch schedules resume only after controlled runtime and
backup/restore verification. Provisioning inputs for the two existing producer
identities and three environment files are prepared; no credentials have been
issued because root grants require an active live root.

The remaining private handoff has prepared drivers for binding the actual live
root, issuing the two scoped credentials, installing the three environment files,
and applying the six qualified local/manual policy operations. Separate drivers
retain and independently compare all 207 scan previews before activation, and
prepare current enrichment/translation previews after qualified source scopes
are activated. Their syntax and installed-runtime imports were checked; these
operations have not run. The n8n database install initially stopped before any
file changes because a zero transient-service timeout meant immediate expiry;
the corrected launcher completed with the original failure evidence retained.

[Current production verification](native-production-cutover-verification.json)
records this partial cutover checkpoint. Private input manifests, immutable
receipts and continuation state remain outside source control. The historical
cold-media audit has finished with 273,077 verified videos and 19 diagnosed
exceptions. The fresh current census contains 274,466 videos and reuses 273,075
of those historical byte proofs. Hashing only the remaining 1,391 files required
10.4 GB of reads. A fresh cloud assessment verified 273,769 current videos and
9,081 archive objects using one metadata GET, 300 LIST requests and 824 HEADs.
The remaining 697 video paths contain 678 unique contents totaling 6.87 GB;
their immutable Deep Archive uploads and checksums are verified. All **274,466
videos in the backup inventory** now have byte-level backup evidence. The image
plan covers **513,333 files**, including two extension-only `.jpg` filenames the
temporary census initially missed. Only 2,774 files need uploading: 40 deltas and
two new bases, totaling 1.45 GB before tar overhead. All **42 packs** are uploaded
and checksum-verified in Deep Archive, using an isolated ledger. The current
master manifest and production ledger remain unchanged until coordinated native
publication succeeds. An untracked 65 MB extensionless Matroska
file has a guarded filename/intake plan for the native handoff; a 715-byte SVG
avatar remains outside supported media extensions. Both remain unchanged.
The current coordinated
metadata/image backup and isolated restore remain outstanding. The system
filesystem now has approximately **125 GiB free** after retiring three unused
schema 94/96/97 rehearsal database copies (61 GiB). Their completed verification
reports and scripts remain; running-container and open-descriptor checks found
no consumers. Production originals, the held snapshot and the active candidate
were preserved. The minimum reserve remains 50 GiB.

Remaining release work:

| Work | Required outcome |
| --- | --- |
| Current import | Finish all catalog record families; independently reconcile current domain references, operational work, policies and source scopes. |
| Writer handoff | Provision scoped credentials and final paths, preserve pending work, replace obsolete workflow execution paths and switch the pinned application/host/manual/n8n runtimes together. |
| Backup and restore | Adopt the verified media ledger, publish the coordinated native backup and verify isolated restoration with original images, queues and pending filesystem work. |
| Observation and retirement | Verify actual scrape, enrichment, manual intake and scheduled recovery/backup cycles; then retire obsolete catalog writers, mounts, services and packages. |
| Acceptance | Publish final evidence and limitations for desktop/mobile owner review; merge into `develop` only after success is accepted. |

The full [transition plan](native-archive-transition-plan.md) remains the
completion contract. A passing development gate or catalog receipt is not proof
of production cutover, complete source coverage or a complete backup.

## Local rehearsal storage budget

Keep at least **50 GiB free** on the host filesystem for downloads and normal
activity. Before write-heavy checks or a full-copy rehearsal, run
`python3 scripts/check_rehearsal_space.py --additional-bytes <estimated-peak-bytes>`.
Include the new copy, migration growth, indexes and WAL in that estimate; check
again during long copies/imports. The check reserves space for future writes;
it does not allocate or lock that space against other host processes.

Retain the original compatible snapshot, frozen migration inputs, the latest
verified native database and, while needed, one candidate under verification.
After verifying its replacement, remove the superseded database and its search
indexes, WAL files and compressed copies once no process has them open. Do not
accumulate a full archive for every schema increment. Small reconciliation
reports and scripts can remain. Production databases, media, active downloads
and ordinary service backups are outside this cleanup policy.

On 2026-10-03, the owner authorized pruning superseded goal artifacts. Removing
197 obsolete database/archive/index files reclaimed about 140 GB and left about
162 GB free. Historical sections below describe evidence recorded at the time;
their intermediate database paths are no longer retained. The original
`native-archive-rehearsal-20260930/compatible-snapshot.sqlite`, frozen import
inputs remain under `.local/`. `.local/native-rehearsal-current.json` identifies
the latest verified native database, schema and reconciliation receipt. The
superseded copies are removed after their replacements pass comparison. After
the schema-56 checks finished, pruning Go cache entries unused for more than
49 hours reclaimed another 33.7 GB. During schema-60 verification, pruning
regenerable Go cache entries older than 24 hours reclaimed 31.5 GiB, leaving
139.3 GiB free with both rehearsal copies still present. Recently used and open
cache files were retained. Cache pruning applies to generated build artifacts;
it does not remove source files or recovery inputs.

## Frozen baseline

- Stash source: `0a603d07e9ec3dd9863a7340858a635525bb2c11`, pushed annotated tag
  `v2.5-compatible-final`; primary schema 86, fork schema 9.
- The running binary reports `v0.26.2-1182-g0a603d07e`. Its wrapper index is
  `sha256:b9c30164d591cb9764f809505208fbf7d42d0bfdf209230b94f0dba69b46014b`.
- The local Quadlet now uses that digest and has registry auto-update disabled.
  A daemon reload succeeded; the existing container was not restarted. The
  original Quadlet is retained in the owner's config backup directory under
  `backups/native-archive-baseline-20260930/`.
- The [release manifest](releases/v2.5-compatible-final.json) records all frozen
  image digests. Base-image preservation passed in
  [run 36757532460](https://github.com/notsafeforgit/stash/actions/runs/36757532460),
  including embedded revision and exact manifest-digest checks. Wrapper image
  preservation passed in
  [run 36758167764](https://github.com/notsafeforgit/stash-s6/actions/runs/36758167764)
  for all three variants and their child manifests.

## Incremental implementation

| Repository / commit | Work and evidence |
| --- | --- |
| Stash `781ee4b93` | Full transition plan and documentation links |
| Stash `b76c600a8` | Frozen release manifest, non-overwriting preservation script, child-manifest retention, isolated native preview tags; seven safety tests and actionlint passed |
| Stash `5c4a4c057` | Independent-fork policy and baseline progress record |
| stash-s6 `5dab164` | Explicit digest selection for native builds, isolated preview variants, preservation workflow using pinned Stash release tooling; actionlint and [resolved bake validation](https://github.com/notsafeforgit/stash-s6/actions/runs/36757903303) passed |
| Stash `bc77846a6` | Native lineage 1000000, sidecar promotion, pre-write refusal, historical migration audit, full-copy rehearsal, native plugin/current-operation validation; complete validation gate passed |
| Stash `43d8bba99` | Canonical saved-filter ASTs, legacy conflict evidence, strict persistence validation, full-copy semantic reconciliation; complete validation gate passed |
| Stash `e8fb366d2` | Unified performer names, per-name auto-tag policy, nonunique display names, full-copy semantic reconciliation; complete validation gate passed |
| Stash `e4fa14045` | Native default filters, durable config publication, revision-checked review, full-copy reconciliation; complete validation gate passed |
| Stash `bd7f2af84` | Portable archive UUIDs, transactional merge redirects and catalog UUID adoption; full-copy reconciliation and complete validation gate passed |
| Stash `4d1c7571d` | Qualified source accounts, evidence replay, and audited ownership choices; full-copy reconciliation and complete validation gate passed |
| Stash `02d3c67c2` | Shared post/profile evidence, lossless captures, versioned retention, replay and integrity checks; full-copy reconciliation, catalog size inventory, and complete validation gate passed |
| Stash `c1d2c9387` | Portable gallery identities, membership revisions, and explicit source-album requirements; full-copy reconciliation and complete validation gate passed |
| Stash `e31ced0d9` | Ordered shared attachment manifests and audited media associations; full-copy reconciliation and complete validation gate passed |
| Stash `a7ab4a1cc` | Reviewed attachment selections and compatible partial source lists; full-copy reconciliation, complete validation gate, CI lint/build, and preview image publication passed |
| Stash `377f7a6ad` | Source albums with persistent manual membership intent; full-copy reconciliation, complete validation gate, CI lint/build, and preview image publication passed |
| Stash `5bc0149ea` | Portable tag/studio/group identities and tag-merge redirects; full-copy reconciliation, complete validation gate, CI lint/build, and preview image publication passed |
| Stash `5fd4d0f93` | Scalar field decisions with explicit clear/inherit, preserved legacy values and new-album capture provenance; full-copy reconciliation, complete validation gate, CI lint/build, and preview image publication passed |
| Stash `b2ecfe605` | Typed metadata relationships and coalesced collection choices; full-copy reconciliation, complete validation gate, CI lint/build, and preview image publication passed |
| Stash `f96577925` | Reviewed source-account consolidation with retained identity evidence and ownership history; full-copy reconciliation and complete validation gate passed |
| Stash `05105cb15` | Versioned captured-account claims across native/mirror services and unfamiliar extractors; complete validation gate and CI lint/build passed |
| Stash `e3db87bec` / `7f2a87c09` | Explicit source album extraction and current documentation; complete validation gate, CI lint/build, and preview image publication passed |
| Stash `d2d6fcf9f` | Revisioned logical roots and source collections, confined file opening, and intake provenance; full-copy reconciliation, complete validation gate, CI lint/build, and preview image publication passed |
| Stash `bd7848d58` | Captured publisher decisions connect source evidence to accounts; full-copy reconciliation and complete validation gate passed |
| Stash `dd9ee3768` | Isolated native test fixtures retain real migration coverage while avoiding repeated empty-schema construction; complete validation gate passed |
| Stash `367ea96ed` | Scoped capture ingestion and durable receipts; full-copy reconciliation, complete validation gate, CI lint/build, and preview image publication passed |
| Stash `2215503f7` | Verified media preparation through the shared scanner; complete validation gate, Windows package cross-compilation, CI lint/build, and preview image publication passed |
| Stash `5777a6b60` | Verified content identities, immutable root/file verification history, and file-generation guards; full-copy reconciliation, complete validation gate, CI lint/build, and preview image publication passed |
| Stash `b5e996347` | Durable archive jobs, fenced leases, coalesced submissions and atomic publication; full-copy reconciliation, complete validation gate, targeted race tests, CI lint/build, and preview image publication passed |
| Stash `a2fdd056e` | Verified file/media publication and persistent path removal fences; full-copy reconciliation, complete validation gate, CI lint/build, and preview image publication passed |
| Stash `372f3267b` | Collection/source intake and selected media feed native album galleries; full validation, targeted index checks, CI lint/build, and preview image publication passed |
| Stash `d24eb5074` | Durable file admission, worker checkpoints, previews, scoped status and retryable plugin delivery; full-copy reconciliation, complete validation gate, targeted race tests, CI lint/build, and preview image publication passed |
| Stash `f244fd4f3` | Revisioned metadata policies, typed jq mapping, guarded previews and ordinary-scan defaults; full-copy reconciliation, complete validation gate, targeted race tests, CI lint/build, and preview image publication passed |
| Stash `c84540426` | Gallery-dl lifecycle, source lease checks and final-file outbox publication; complete validation gate, targeted race tests, CI lint/build, and preview image publication passed |
| Stash `bad3f2ca5` | Offline source request coalescing, caller tickets and immutable admission replay; producer migration fixtures, shared window corpus, complete validation gate, targeted race tests, CI lint/build, and preview image publication passed |
| Stash `5af18a486` | Scoped source dispatch and pinned n8n worker image; complete validation gate, producer tests on both host runtimes and the installed container package, CI lint/build, and preview image publication passed |
| Stash `a2b8a0de1` | Caller completion tied to original submissions and exact source windows; producer migration/restart fixtures, complete validation gate, CI lint/build, and preview image publication passed |

## Phase status

| Phase | Status |
| --- | --- |
| 0 Baseline and contract | Compatible source/images are frozen and production is pinned. Final coordinated backup boundary, deployment inventory and measured release budgets remain. |
| 1 Native schema and services | Implemented and repeatedly rehearsed through schema 97, including canonical identities, promoted sidecars, source evidence, field choices, file recovery, jobs, review receipts, merges and captured profile links with persistent removal choices. Final dependency and invariant audit remains. |
| 2 Ingestion and producer adapter | Transport, outboxes, pacing and Reddit/Twitter/Instagram/Coomer/Kemono/Bluesky/TikTok download adapters are implemented and tested. Direct manual invocation, ThisVid/yt-dlp and remaining generic paths still require integration before host/n8n activation. |
| 3 Catalog importer | Frozen catalogs, policies and operational families are imported and reconciled on copies. Final live snapshots, current review outcomes and cutover reconciliation remain. |
| 4 Native UI and client conversion | Core account/source/collection/metadata/album/manual-intake workflows, shared activity, import history, saved-action recovery and the current review queue are implemented and verified. Actual host/n8n caller conversion remains. |
| 5 Compatibility removal and packaging | V3 is the sole embedded UI and v3 plugin contract. Remaining runtime/client compatibility removal and final pinned deployment artifacts require the dependency audit. |
| 6 Backup and cutover rehearsal | Native snapshot/export, host publisher/retention and restore tools are implemented and tested. Complete production capture inventory, cost measurements and full relocated restore/cutover drill remain. |
| 7 Production cutover | Not started. |
| 8 Retirement | Not started; depends on successful cutover, observed scheduled cycles and owner acceptance. |

## First native schema rehearsal

On 2026-09-30, an online SQLite backup of the running compatible library produced
a 1,071,263,744-byte isolated snapshot in 3.289 seconds. A separate copy promoted
from primary 86 / fork 9 to native 1000000 in 3.207 seconds. This measurement
covers the initial table promotion, not the later catalog import or production
cutover downtime.

The snapshot contains 270,760 scenes, 498,646 images, 769,643 files, 1,426
performers, and 83 saved filters. Streaming row digests matched across all 65
retained data tables after accounting for table names and equivalent empty
nullable filter fields. Foreign-key checking found zero violations. The private
snapshot, migrated copy, and full comparison receipt are retained locally in
`.local/native-archive-rehearsal-20260930/`; no private library data is committed.

The actual frozen compatible binary was run against an isolated empty native
database and refused schema 1000000 as an unknown legacy fork version. The
native version stayed unchanged. Unit/integration tests also reject foreign
lineage, unsupported versions, missing native tables, unknown legacy objects,
destination collisions, dirty states, and partial SQL promotion.

The full Go unit/integration suite (`GOTOOLCHAIN=auto make it`), SQLite/sharing
integration tests, targeted Go lint, and the retained v3 plugin/current-operation
check passed. `GOTOOLCHAIN=auto make validate-fork` also passed: generation,
frontend lint/types/locales, 527 UI tests in 90 files, all-repository Go lint and
unit/integration tests, and retained native contract checks.

## Canonical saved filters

Migration 1000001 stores ASTs directly on `saved_filters`, removes the live legacy
projection/shadow state, and retains pending alternatives as migration review
evidence. Invalid ASTs fail before conversion; create/update reject invalid or
unserializable criteria without replacing the selected filter. A cleared AST
stays cleared across restart.

The full-copy migration from native 1000000 took 45.6 milliseconds. All 83 filter
ASTs and their name/mode/UI/find options reconciled, 65 other retained tables had
identical streaming row digests, and foreign-key checking found zero violations.
The copied library has no pending legacy filter conflicts. The reconciliation
receipt is `.local/native-archive-rehearsal-20260930/saved-filter-reconciliation.json`.
Focused SQLite/import tests and the complete `make validate-fork` gate passed,
including all 527 frontend tests and retained native contract checks.

## Unified performer names

Migration 1000002 consolidates canonical names, ordered aliases, and per-name
auto-tag policy into `performer_names`. Display names and disambiguation no longer
have to be globally unique. Existing alias selection transfers its policy and
keeps the previous canonical name; database writes replace a name set atomically.
The old name column and both name-related tables are removed. Existing search,
filter, sorting, auto-tag, merge, and anonymised-export callers use the new model.

The full-copy migration took 77.3 milliseconds. All 1,426 performer records,
1,927 names (including 501 aliases), and their policies reconciled. Streaming
digests matched for the other 64 retained tables; foreign-key checking found
zero violations. The receipt is
`.local/native-archive-rehearsal-20260930/performer-name-reconciliation.json`.
Focused migration/domain/API tests and the full validation gate passed.

## Native default filters

Migration 1000003 makes default filters database records with persistent
revisions. A durable configuration checkpoint commits the original filter-only
input, native records, and conflict evidence before atomically publishing the
cleaned config. Generic UI configuration writes cannot bypass the native API;
resolving an alternative requires the revision shown to the reviewer.

Interruption tests cover both publication boundaries, restart, changed inputs,
invalid criteria, preserving edits after import, and stale review actions.
Configuration writes retain permissions and symlinks. Anonymised exports remove
filter evidence and criteria, including freed pages. New database creation no
longer runs old configuration rewrites against modern settings. Pre-migration
backups remain available after success.

The full-copy SQL migration took 31.4 milliseconds; staging and publishing the
nine actual default filters took 20.5 milliseconds. Historical string pagination
was converted without losing sort/display options or the original input. All
66 other retained tables matched by streaming row digest, with zero foreign-key
violations and no pending default conflicts. The receipt is
`.local/native-archive-rehearsal-20260930/default-filter-reconciliation.json`.
The complete `make validate-fork` gate passed, including 528 UI tests, native
contract checks, Go lint, and all Go unit/integration tests.

## Portable archive identities

Migration 1000004 assigns stable UUIDs to performers, scenes, images, and file
records while retaining their integer IDs. Typed foreign keys and partial unique
indexes connect identities to the existing records. Creation, edits, deletion,
and current performer/scene merge callers maintain the identity lifecycle.
Redirects survive merges and subsequent deletion; reused local IDs cannot
resurrect an old identity. Partial merges that leave the source without a UUID
are rejected at commit.

The archive repository supports revision-checked adoption of catalog UUIDs,
preserving generated UUIDs as redirects and refusing conflicting assignments.
It rejects cycles and cross-kind redirects. Anonymised exports replace UUIDs
without breaking the graph. Actual catalog import and API/UI exposure remain
outstanding.

The full-copy migration took 12.6 seconds and created 1,540,475 identities for
1,426 performers, 270,760 scenes, 498,646 images, and 769,643 file records. All
69 retained tables matched by streaming row digest, every existing record had
its identity, and foreign-key checking found zero violations. Database growth
was 249,470,976 bytes (about 238 MiB). The private reconciliation receipt is
`.local/native-archive-rehearsal-20260930/archive-identity-reconciliation.json`.
The full validation gate passed, including migration/lifecycle and merge tests,
anonymised-export checks, 528 UI tests, native contracts, and Go lint/tests.

## Source account foundation

Migration 1000005 adds independent source accounts, qualified identifier claims,
observation evidence, and ownership decisions. Indexed candidate queries retain
reused handles and conflicting IDs for review. Native IDs, handles, and mirror
identifiers remain distinct; matching never assigns media performers. Accounts
and ownership are optional for directly scanned or purchased media.

Evidence replay preserves large numeric identifiers and subsecond timestamps,
extends observation intervals, and rejects changed contents under the same key.
New evidence invalidates stale review revisions. Linked, explicitly unlinked,
and undecided decisions retain immutable history with a checked current head.
Applying a link validates both account and performer revisions. Profile discovery
cannot undo an existing explicit choice. Merge redirects, adopted UUIDs, and
deleted-performer tombstones preserve previous decisions. Anonymised exports
remove source account evidence.

The full-copy migration took 32.6 milliseconds and added 69,632 bytes (68 KiB).
All 70 existing tables, including 1,540,475 archive identities, matched by streaming
row digest, and foreign-key checking found zero violations. New account tables
were empty: schema promotion does not invent provenance for library records.
The private receipt is
`.local/native-archive-rehearsal-20260930/source-account-reconciliation.json`.
The complete `make validate-fork` gate passed: 528 UI tests, current native
contracts, Go lint, and all Go unit/integration tests. Go compilation used
workspace scratch space through `GOTMPDIR` after the shared temporary filesystem
hit its quota; no checks were skipped.
Source equivalence/review services, actual catalog import, and API/UI exposure
are still outstanding. Production remains compatible.

## Physical catalog inventory

The read-only schema inventory on 2026-09-30 found 1,700 databases under the
catalog root: 1,697 schema-3 source catalogs plus the registry, automation, and
run-journal databases. It covers 54 table families and six exact schema variants,
with no unreadable databases after allowing SQLite's transient shared-memory
files. Base database files totalled 2,979,741,696 bytes; this excludes WAL files
and producer state outside the root.

The migration coverage now explicitly includes older plugin profile/binding
tables, collection backfill completions, and one-time backfill policy receipts.
These records must be reconciled with the newer registry and job state rather
than applied twice or used to restart completed work. The private inventory
manifest includes complete schema definitions and per-file schema hashes.
This is an individually consistent schema inventory, not a coordinated backup
boundary or a complete integrity assessment; those remain release requirements.

## Shared source evidence

Migration 1000006 adds portable source posts, shared post revisions and profile
bodies, immutable captures, and checked profile references. Post bodies remain
shared across attachments and meaningful profile edits. Evidence reconstructs
exactly, including large numeric IDs and nanosecond capture times. Versioned
retention removes redundant Reddit renditions and noisy Twitter/Reddit profile
fields for new data; trusted historical import preserves already-retained data.
The 26 synthetic reference fixtures exercise the existing Python policy.

Capture replay is idempotent across restart and rejects changed content under
an existing UUID. Integration tests cover rollback after a late write failure,
cross-post foreign keys, immutable evidence, missing-reference and payload
corruption, bounded indexed queries, and forgotten-post protection. Anonymised
exports remove the new data while preserving the original database. Direct-scan
performer assignments survive migration without invented source posts.
The complete `make validate-fork` gate passed, including 528 UI tests, current
native contracts, Go lint, and all Go unit/integration tests.

The full-copy schema migration took 31.4 milliseconds and added 90,112 bytes
(88 KiB). All 75 retained tables matched by streaming row digest, with zero
foreign-key violations and empty new source tables. The private comparison
receipt is `.local/native-archive-rehearsal-20260930/source-evidence-reconciliation.json`.
An additional read-only inventory covered all 1,697 catalogs: 366,963 observation
records, 430,910 capture-detail records, and 776 separately stored profile bodies.
The largest reconstructed payload size bound was 2,928,259 bytes, below the new
4 MiB limit; no catalog failed inspection. The calculation covers both embedded
and shared profile formats, and deliberately overcounts shared/patch overlap.
It is not a full decode/depth/import validation or a common backup boundary.
Its private receipt is `catalog-evidence-size-inventory.json` in the rehearsal
directory; the all-catalog import rehearsal remains required.
Source-account/capture associations, media appearances, gallery-dl API ingestion,
actual catalog import, and UI exposure remain subsequent work. Production has
not been migrated.

## Source album requirement and gallery identities

The owner's album request is included in the transition plan and acceptance
matrix: one logical gallery per evidenced album post, source attachment order,
mixed images/videos, partial and late downloads, and preservation of manual
choices and existing galleries. Source grouping does not infer performers from
an aggregator. Catalog backfill and standalone export/restore cover albums.

Migration 1000007 gives existing and future galleries portable identities in the
shared registry. Creation, renaming, deletion, UUID adoption, and redirects retain
their semantics; gallery memberships and related metadata advance revisions so
intervening edits invalidate stale review actions. Tests cover existing covers,
image/scene/performer memberships, account links, redirect preservation, deletion,
ID reuse, revision conflicts, and anonymised exports.
The complete `make validate-fork` gate passed, including 528 UI tests, native
contracts, Go lint, and all Go unit/integration tests.

The full-copy migration took 9.56 seconds. All 1,540,475 prior identities and
their UUIDs matched exactly, all 81 other retained tables matched, and all 1,328
existing galleries received identities. Foreign-key checking found no violations.
The rebuild grew the database file by 150,364,160 bytes (143.4 MiB), with
150,929,408 bytes (143.9 MiB) on the reusable free-page list afterward; that space
can serve subsequent native writes. The private receipt is
`.local/native-archive-rehearsal-20260930/gallery-identity-reconciliation.json`.

Source-gallery construction is added in the later checkpoint below; the native
album UI remains required. Ordered attachment associations are described below.
No production galleries or files have been modified.

## Ordered source attachments and media associations

Migration 1000008 adds shared ordered attachment manifests and capture-to-manifest
references. Source order, repeated attachment slots, declared albums, expected
counts, and incomplete source lists survive independently of download state.
Repeated captures share one list; partial captures retain prior snapshots.
Media evidence must cite a capture containing that attachment and typed archive
identities. Observed/verified candidates require an actual current file link.

Selecting media is an audited revision-checked operation. Ingestion can select a
unique supported match; ambiguous candidates require review. Explicit unlinks
survive later evidence. Merge redirects, UUID adoption, deletion tombstones,
restart, keyset pagination, late-write rollback, and anonymisation are covered
by the focused tests. Core/API ingestion must still validate producer evidence;
these repositories are not exposed directly to untrusted producers.

The full-copy schema migration took 54.9 milliseconds. Streaming digests matched
all 82 retained tables, foreign-key checks found zero violations, and all seven
new tables were empty. The database file did not grow because existing free
pages could hold the new schema. The private receipt is
`.local/native-archive-rehearsal-20260930/attachment-reconciliation.json`.
The complete `make validate-fork` gate passed: generation, frontend checks and
528 UI tests, retained native contracts, Go lint, and all Go unit/integration
tests. The previous gallery checkpoint also passed CI lint, build, and preview
image publication.

Source manifest selection and gallery synchronization are described in later
checkpoints below. General field decisions, importer integration, and native UI
remain outstanding.
Production remains on the compatible release.

## Reviewed source-list selection

Migration 1000009 records the selected attachment evidence for each source post.
Compatible partial lists accumulate known positions and counts without erasing
earlier knowledge. A complete capture can replace redundant partial references;
all original captures and earlier decisions remain available. Different IDs at
one position, conflicting known media kinds/counts, and out-of-count positions
produce explicit conflicts rather than a guessed list.

Read-only previews carry the post revision. Applying a choice requires that
revision; pinned and disabled selections survive later captures. Review can
choose a specific capture or explicitly resume automatic selection from it.
Equivalent automatic replay retains the original decision. Bounded bulk queries
load supporting manifests and entries; source payloads are not duplicated.

Focused tests cover accumulation, complete/partial semantics, redundant evidence,
conflicts, stale actions, pin/disable/re-enable, restart/replay, pagination,
late-head failure rollback, cross-post foreign keys, corruption detection,
forgotten posts, migration preservation, and anonymised copies.

The full-copy migration took 45.1 milliseconds. All 89 retained tables matched
by streaming row digest, foreign-key checks found zero violations, and the three
new tables were empty. Existing free pages held the new schema without growing
the file. The private receipt is
`.local/native-archive-rehearsal-20260930/selection-reconciliation.json`.
The complete `make validate-fork` gate passed: generation, frontend validation
and 528 UI tests, retained native
contracts, Go lint, and all Go unit/integration tests. The previous attachment
checkpoint also passed CI lint, build, and preview image publication.

The source-list repository remains separate from gallery synchronization, added
below. Native review API/UI, general field decisions, catalog importer integration,
and the remaining transition phases are still required. Production has not been
migrated.

## Source gallery synchronization and manual membership intent

Migration 1000010 adds explicit gallery origin, audited post-to-gallery choices,
and persistent membership intent. Source synchronization creates one logical
gallery per evidenced album, including partial albums and mixed images/videos.
Repeated slots and reposts reuse existing media. Ordinary single-media posts do
not create galleries. Read-only previews preserve source order and distinguish
unselected, unlinked, deleted, and manually excluded entries; they do not pretend
a linked library entity proves a completed download.

Fresh synchronization replay makes no duplicate galleries, memberships, or audit
events. Manual additions/removals, covers, and deliberately empty metadata survive
later synchronization. Existing manual galleries need an explicit reviewed link;
folder/ZIP galleries cannot be adopted. Disabled associations and deleted galleries
suppress recreation, and gallery redirects require review. No names or titles
establish identity, and source publishers do not become depicted performers.

Repository integration tests cover partial/late media, mixed/repeated/shared
attachments, replay/restart, stale previews, both sides of membership editing,
disable/re-enable, UUID adoption and media redirects, integer-ID reuse, preserved
existing galleries, and atomic rollback after late failures. Anonymised exports
remove association and membership history. Pre-commit and startup guards reject
unfinished source-write context.

The full-copy schema migration took 72.7 milliseconds. All prior columns across
92 retained tables matched by streaming row digest, including every existing
gallery and archive identity. The new gallery-origin values matched the prior
folder/file associations. Foreign-key checking found zero violations, the five
new tables were empty, and the database file did not grow. The private receipt is
`.local/native-archive-rehearsal-20260930/source-gallery-reconciliation.json`.
The complete `make validate-fork` gate passed on the final implementation:
generation, frontend validation and 528 UI tests, native contracts, Go lint,
and all Go unit/integration tests. Existing gallery fixtures now assert their
explicit creation origin. Regression tests also cover reaffirming membership
through scene/image add operations and file associations without a primary file.

The native API/UI, per-field source/review policies, manual mixed-media ordering,
durable after-success delivery, and producer/catalog import integration remain
required. This checkpoint initializes new album title/description/date and
preserves all existing gallery metadata. Production has not been migrated.

## Portable metadata relationship targets

Migration 1000011 adds tag, studio, and group UUIDs to the archive registry. They
retain identity across renames and preserve tombstones after deletion or reuse
of an integer ID. Tag merges retain redirects to the surviving tag while moving
the existing aliases, remote IDs, and media relationships. Definition and
hierarchy edits advance revisions; moving a relation invalidates both owners.
These identities are a prerequisite for typed references in metadata history,
not an implementation of field decisions themselves.

Focused tests cover all three kinds, stale revisions, UUID adoption, redirects,
transaction rollback, indexed lookup, relationship edits, existing source-gallery
references, and anonymised exports. The full-copy migration took 10.28 seconds.
All 1,541,803 prior identities matched exactly, all 96 other retained tables
matched by streaming row digest, and all 118 tags, 124 studios, and five groups
received identities. Foreign-key checking found zero violations. File growth
was 1,642,496 bytes, with 146,558,976 bytes available on the reusable free-page
list. The private receipt is
`.local/native-archive-rehearsal-20260930/metadata-identity-reconciliation.json`.
The complete `make validate-fork` gate passed, including generation, frontend
validation and 528 UI tests, native contracts, Go lint, and all Go unit/integration
tests. Production has not been migrated.

## Scalar metadata choices

Migration 1000012 introduces set, clear, and inherit for curated scene/image/gallery
scalar fields. Existing values, including empty ones, remain protected until
reviewed. Their prior values enter history on the first edit, avoiding a copy of
every entity or a separate initial row for every field. Ordinary edits record
library intent in the database. An explicit empty title survives later source
and filename suggestions, and filename fallback cannot replace a nonempty
source title. New albums retain the capture provenance of their initial fields.

Focused tests cover typed targets, date precision, invalid inputs, canonical
integer round trips, clear/inherit/replay, stale revisions, ordinary same-value
edits, lazy legacy history, adoption and deletion/ID reuse, indexed pagination,
scope and immutability constraints, late rollback, startup refusal, and removal
of private history from anonymised exports. The initial full gate caught two
unused test-result assignments; the tests now assert those results. Broader album
tests also caught and corrected a query that treated revision metadata as a
standalone column rather than reading its JSON member.

The full-copy migration took 1.62 seconds. Streaming comparison matched all 97
retained tables, and all 770,734 existing scene/image/gallery identities received
a preservation baseline. No decisions were fabricated during migration.
Foreign-key checking found zero violations; the database file did not grow, with
110,927,872 bytes remaining on its reusable free-page list. The private receipt
is `.local/native-archive-rehearsal-20260930/metadata-fields-reconciliation.json`.
Existing selected text fields were also checked against the scalar value bound.
The final complete `make validate-fork` gate passed: generation, frontend
validation and 528 UI tests, retained native contracts, Go lint with zero issues,
and all Go unit/integration tests.

Relationship choices, policy/source resolution, native API/UI and creation-intent
conversion, importer/producer integration, and durable after-success delivery
remain required. This checkpoint does not enable automatic rescanning or overwrite
existing album headers. Production has not been migrated.

## Collection and relationship metadata choices

Migration 1000013 adds the same set/clear/inherit behavior to performers, tags,
studio, URLs, custom fields, and scene groups. Relationship decisions retain
normalized UUID references, with reviewed target revisions and immutable sealed
history. Performer/tag merges preserve the meaning of historical associations;
deleting a target cannot silently rebind old history when its integer ID is
reused. Native browsing fields remain the selected state.

Ordinary bulk edits retain the previous value and record one final choice per
field at commit. Explicit empty sets and reaffirming existing relationships also
protect user intent. SQL guards, transaction checks, and startup validation reject
unfinished decisions. The existing scalar history survives migration intact.
Anonymised exports remove the added references and transaction state.

Focused tests cover every field type across scenes/images/galleries, target
revision conflicts, wrong-kind references, coalescing, clear/inherit/replay,
empty updates through existing APIs, merge/adoption/delete/ID reuse, previous
values without recorded history, preserved history sequence counters, late
rollback, startup refusal, and indexed lookup. Numeric tests include large
doubles and exact signed 64-bit integers, including writes after replay. The
query-plan test identified a missing scene-to-group index, which is
included in this migration. Full-copy probing found no full scans for selected
metadata lookups; a representative single studio edit took about one millisecond.
Existing relationship sizes were within the 4096-target bound.

The final full-copy schema migration took 2.30 seconds. Streaming row digests
matched all 101 retained tables and columns, with zero foreign-key violations.
The new reference and pending tables were empty, no provenance was invented, and
the file did not grow; 110,845,952 bytes remained available on its free-page list.
The private receipt is
`.local/native-archive-rehearsal-20260930/metadata-collections-reconciliation.json`.

The complete `make validate-fork` gate passed on the final code: generation,
frontend validation and 528 UI tests, the retained v3 extension contract and
71 application operation files, Go lint with zero issues, and all Go unit and
integration tests. The SQLite suite completed in 402.1 seconds.

Source/policy resolution, native review API/UI, creation-intent conversion,
producer/catalog import integration, and durable after-success delivery remain
required. Production has not been migrated.

## Reviewed source-account consolidation

Migration 1000014 adds explicit consolidation of duplicate account records in
one service namespace. Original account UUIDs, identifier evidence, and ownership
history remain available. Canonical account and identifier indexes follow the
surviving record through checked foreign keys, including after nested merges.
Lookup remains indexed and paginated instead of traversing redirect chains.

A preview covers the selected account components and their current performer
revisions. Compatible ownership choices can be retained; contradictory ownership
requires an explicit resulting choice. Conflicting stable IDs require a separate
acknowledgement and stay in the evidence. Cross-service and native/mirror accounts
remain distinct, and consolidation does not assign depicted performers to media.
An optional operation UUID provides exact replay without duplicate history.
Transaction/startup guards reject unfinished work, and anonymised exports remove
the added private history.

Focused tests cover retained identifiers and local ownership history, canonical
lookups and query plans, nested consolidation, stale identity and performer
reviews, explicit unlink preservation, conflict resolution, replay, rollback,
startup refusal, anonymisation, and migration with existing decisions. Fixtures
exercise Reddit, Twitter, Instagram, Bluesky, TikTok, Patreon, OnlyFans, Fansly,
Coomer/Kemono namespaces, and an unfamiliar extractor. A regression fixture also
requires acknowledgement before consolidating conflicting TikTok `secUid`
claims. Review bounds are 4096 account records and 8192 identifiers.

The full-copy migration took 1.054 seconds. Streaming comparisons matched all
103 retained tables and columns, with zero foreign-key violations. The new
consolidation/context tables were empty, and every existing account began as its
own canonical identity. No links were inferred and the database file did not
grow, with 110,796,800 bytes remaining on its reusable free-page list. The private
receipt is
`.local/native-archive-rehearsal-20260930/account-consolidation-reconciliation.json`.
The Stash copy has no imported source accounts yet; populated migration fixtures
separately verify existing identifiers and ownership choices.

The final complete `make validate-fork` gate passed: generation, frontend
validation and all 528 UI tests, retained v3 extension contracts and 71 application
operation files, Go lint with zero issues, and all Go unit/integration tests.
The SQLite suite completed in 450.9 seconds. An earlier run exhausted the shared
temporary-files quota during linking; both Go build and linker temporary files
were redirected to the workspace disk for successful validation.

Producer evidence matching, native account-review API/UI, and catalog import
remain required integration work. Production has not been migrated.

## Captured account identity extraction

Core now implements `captured-account-v1` for deriving qualified identifier claims
from reconstructed gallery-dl/yt-dlp captures. Each claim retains its source JSON
pointer and evidence basis. The parser preserves exact numeric IDs and handles
Reddit parent context, Twitter, Instagram, Bluesky, TikTok (including `secUid`),
Tumblr, native subscription services, Coomer/Kemono, and unfamiliar extractors.
It leaves missing IDs unresolved and reports malformed claims for review.

Mirror user IDs and matching public identifiers retain the mirror/service
namespace; display labels do not become native handles. Directory names, scraper
target URLs, and unrelated feed-owner profiles cannot establish the publisher's
ID. A generic extractor's display name remains a label. Existing legacy links
and handle-only inventory records still require lossless import independently
of whether the parser can derive new claims.

Focused fixtures cover these service shapes, exact integers above 2^53, duplicate
keys and malformed identifiers, replay, immutable input bytes, irrelevant profile
noise, retained-payload equivalence, and missing/mismatched mirror profiles.
The complete `make validate-fork` gate passed on this code with the consolidation
checkpoint: 528 UI tests, native contracts, zero Go lint issues, and every Go
unit/integration package. No database migration is required for this parser.

See [native source identity](native-source-identity.md) for the contract and
evidence limits. Account resolution, publisher associations, ingestion/import
integration, and review API/UI remain required. The parser does not create
accounts, merge candidates, change ownership, or assign depicted performers.

## Source-list extraction for albums

Core now derives `captured-album-v1` manifests from Reddit gallery lists and
complete Twitter media lists. It retains qualified post/media IDs, source order,
crosspost and parent evidence pointers, missing slots, unavailable source items,
and repeated attachments. Manifest completeness stays independent of whether
the associated media has downloaded. Unknown lists remain unresolved.

Inspection of the installed gallery-dl 1.32.15-dev extractors confirmed that
Reddit download numbers skip items with unavailable URLs and Twitter's `count`
counts extracted output files, including optional renditions/card images. The
parser therefore does not infer a complete source album from `num`, `count`,
filenames, or a per-file media ID. The native Twitter adapter must supply the
post's media list before extractor filtering; old count-only captures require
additional evidence or review during backfill.

Focused parser fixtures and SQLite integration passed. They cover source order,
partial identity evidence, unavailable items, repeated IDs, crosspost identity,
conservative type hints, contradictory input, exact large IDs, retained-payload
equivalence, bounds, and replay. Parsed lists pass through capture storage and
selection into a gallery with an image, then accept a later video association
without making a second gallery or duplicating memberships.

The complete `make validate-fork` gate passed on the final implementation:
generation, frontend validation and 528 UI tests, retained native contracts,
Go lint with zero issues, and all Go unit/integration packages. The SQLite suite
completed in 394.2 seconds.

See [native source albums](native-source-albums.md). No schema migration is
required for the parser. Producer integration, catalog backfill, native API/UI,
and source-to-file verification remain required; production has not changed.

## Logical media roots and source collections

Migration 1000015 gives roots and scrape/manual collections stable UUIDs,
revision-checked definitions, immutable history, and bounded indexed queries.
Collection targets remain separate from performer attribution. Purchased MP4
batches can retain intake provenance without creating an account or source post;
aggregator/feed collections can share captures without assigning their publisher
as a depicted performer. Historical target URLs return current collection
candidates rather than establishing unique identity.

Media roots retain identity when a deployment binding changes. File opening
checks the actual open directory identity and confines paths to it; inactive or
unbound roots, replaced mounts, escaping symlinks, nonregular files, and `.part`
paths are rejected. A file descriptor opened before a rename still references
the original file. Authorization, complete-file verification, and producer
receipts remain separate required checks for the ingestion service.

Capture provenance pins collection revisions. Manual intake uses replayable
event UUIDs, typed scene/image references, and original submitted UUIDs retained
through archive UUID adoption. Deleted media retain their evidence via
tombstones. These operations do not edit metadata or gallery membership. SQL
revision guards and deferred foreign keys prevent stale publication and orphan
identities; anonymised exports remove private definitions and provenance.

Focused tests passed for mount replacement, symlink confinement, regular-file
checks, descriptor identity, stale edits, retirement, URL ambiguity, indexed
lookup, composite pagination, ignored-error rollback, exact/conflicting replay,
UUID adoption, deletion, populated migration, and anonymisation. The filesystem
package also compiled for Windows.

The isolated full-library copy migrated from 1000014 to 1000015 in 0.885 seconds.
Streaming semantic digests matched all 105 retained tables, with zero foreign-key
violations and no invented rows in the six new tables. The 1,473,081,344-byte file
did not grow; 110,706,688 bytes remain reusable. The private receipt is
`.local/native-archive-rehearsal-20260930/source-collections-reconciliation.json`.
The copy still has no imported catalogs; populated fixtures separately cover
actual collection histories and intake records.

The complete `make validate-fork` gate passed on the final implementation:
generation, frontend validation and all 528 UI tests, retained v3 extension
contracts and 71 current application operation files, Go lint with zero issues,
and all Go unit/integration packages. The SQLite suite completed in 499.6 seconds.
An initial validation run found a test cursor-cleanup lint issue; the final gate
includes that correction.

Native API/UI, folder/batch defaults, ingestion, and catalog import remain
required; production has not changed.

## Captured publishers and account resolution

Migration 1000016 adds publisher choices, replay identity, current selections,
and references to the captured identifier evidence used by each choice. The
service verifies one stored capture and uses qualified IDs to reuse or create
accounts. Handle-only candidates and conflicting IDs remain for review; explicit
unlinks survive automatic processing. Review can select a publisher without
merging account records. Current reads follow later account consolidation while
preserving the original association.

Publisher identity does not assign performers or change account ownership.
Post/profile bodies remain shared. Preview signatures cover relevant identity
changes and current choices while ignoring unrelated observation counters, so
ongoing scrapes of the same account do not constantly invalidate review. An
account-leading identifier index and a post-first capture query keep checks
scoped to the affected records. Candidate previews are bounded and disclose
truncation; direct target lookup remains available for explicit review.

Focused fixtures passed for allocation and ID reuse, handle changes/reuse,
ambiguous IDs, TikTok secondary-ID contradictions, native/mirror namespace
separation, missing/malformed claims, explicit unlink/inherit, stale review,
unrelated captures, exact/conflicting replay, account consolidation, source
retirement, late-error rollback, startup refusal, anonymisation, pagination,
and query plans. A populated actual schema-15 fixture retains signed captures,
shared profiles, identifiers, logical roots, and collection associations through
migration, then successfully resolves its publisher.

The isolated 1,473,081,344-byte library copy migrated from 1000015 to 1000016 in
0.606 seconds. Streaming semantic digests matched all 111 retained tables, with
zero foreign-key violations and no file-size growth. Migration left all four
publisher tables empty rather than inventing associations. The private receipt
is `.local/native-archive-rehearsal-20260930/capture-publishers-reconciliation.json`.
The copy still has no imported catalogs; populated fixtures separately verify
actual source captures and existing associations.

The complete `make validate-fork` gate passed on the final implementation:
generation, frontend validation and all 528 UI tests, retained v3 extension
contracts and 71 current application operation files, Go lint with zero issues,
and all Go unit/integration packages. The SQLite suite completed in 567.3 seconds.
The initial focused query-plan fixture caught a broad decision scan; the final
query fixes its join order to start with the selected post's indexed captures.

Native producer/API integration, review UI, account-profile presentation,
metadata policies, and catalog import remain required. Production has not changed.

## Isolated native test fixtures

Domain tests now copy a closed, empty database built once through the real
initializer, then insert their own fixture entities. Every test still opens its
own database and receives fresh archive UUIDs. Historical migration and new-install
tests continue constructing their actual input schemas. An isolation test verifies
that identities and edits do not leak between copies.

The complete `make validate-fork` gate passed with 528 UI tests, native contracts,
zero Go lint issues, and all Go unit/integration packages. The SQLite suite took
154.3 seconds, down from 567.3 seconds before avoiding repeated empty-schema
migrations. This changes test setup only.

## Native capture ingestion and durable receipts

Migration 1000017 adds producer identities, scoped access tokens, collection/root
grants, and immutable event receipts. These are tokens for calling Stash's API;
third-party service credentials stay with gallery-dl. Token administration uses
the existing authenticated application router. The isolated producer router
accepts bearer tokens and has no application, GraphQL, or plugin fallback.

`/api/v3/ingest` now exposes capability discovery, bounded capture batches, and
producer-scoped receipt lookup. A capture transaction verifies the source post
identity and retention policy, records shared evidence and collection provenance,
resolves its publisher, selects compatible album manifests, and publishes the
receipt. Metadata receipts explicitly report that media has not been ingested.
The initial identity adapters accept Reddit and Twitter; other extractors are
reported as unsupported until their post identity adapters are implemented.

Exact retries preserve the original acknowledgement; changed event bytes
conflict. Batch items commit independently. Receipt-storage failure rolls back
all domain changes. Delayed events can retain historical collection definitions
within their granted root scope. Pinned/disabled albums and ambiguous publisher
or album evidence remain reviewable. Publisher identity never assigns depicted
performers. Token rotation preserves producer/event identity; expiry and permanent
revocation are checked in the transaction that writes each event.

Focused service, HTTP, and SQLite fixtures passed for concurrent duplicates,
restart/lost acknowledgement, conflicting replay, strict JSON and retention,
large source IDs, scope isolation, delayed definitions, partial batches,
revocation/expiry, album conflicts and protected choices, rollback, immutable
receipt constraints, indexed lookup, anonymisation, and actual schema-16 promotion
with populated source evidence and publisher decisions.

The isolated 1,473,081,344-byte library copy migrated from 1000016 to 1000017 in
0.068 seconds. Streaming semantic digests matched all 115 retained tables, with
zero foreign-key violations and no size growth; 110,592,000 bytes remain reusable.
All four new tables remain empty after schema migration. The private receipt is
`.local/native-archive-rehearsal-20260930/ingest-reconciliation.json`. This copy
still has no imported catalogs; populated fixtures separately verify source-data
retention through the actual historical schema.

The complete `make validate-fork` gate passed on the final implementation:
generation, frontend validation and all 528 UI tests, retained v3 extension
contracts and 71 current application operation files, Go lint with zero issues,
and all Go unit/integration packages. The SQLite suite completed in 180.9 seconds.
An initial gate found response-encoding and test cursor-cleanup lint issues;
the final gate includes both corrections.

The [ingestion contract](native-ingestion.md) documents request bytes, bounds,
access tokens, and receipt semantics. File completion/verification, producer
outboxes, run leases, gallery synchronization during media ingestion, additional
post identity adapters, native UI, catalog import, and actual host/n8n conversion
remain required. Production has not changed.

## Verified media preparation

`ingest.PrepareMedia` verifies a confined root-relative regular file, computes
SHA-256, checks optional producer size/digest claims, and holds its descriptor
through shared scanner preparation. New-file scans and native preparation now
reuse `Scanner.PrepareFile` for matching fingerprints and media decorators.
Independent descriptor readers preserve seeking, cancellation, and ownership;
closing a reader does not close the held file.

FFprobe reads that same descriptor on Linux, with a private descriptor-copy
fallback on other platforms. Its native intake path has a self-contained format
allowlist, no network/playlist inputs, bounded JSON output, and a timeout. A
promoted `bytes.Buffer.ReadFrom` method would bypass the output writer's limit;
the bounded writer deliberately does not embed that type. Audio-only output
cannot become a video file. Animated images retain the existing clip behavior.

Revalidation detects changed bytes, file/root replacement, changed bindings,
symlink retargeting, and restored modification times using Linux ctime or a
portable digest recheck. Preparation performs no database writes or handlers.
The eventual worker must revalidate inside its publication transaction and
enforce authorization, collection policy, and file-generation fences. The API
continues to advertise `file_ingestion: false`; file completion, durable workers,
and transactional file/source/gallery/receipt publication remain unfinished.

Focused tests passed for the filesystem checks, independent readers, cancellation,
real image/animated-image/audio-only probing, trailing-MP4 metadata, and a file
replaced between hashing and probing. Windows cross-compilation of the archive,
file, and FFprobe packages passed. No database migration or production changes
are part of this checkpoint.

The complete `make validate-fork` gate passed: generation, frontend checks and
528 UI tests, retained extension contracts and 71 application operation files,
zero Go lint issues, and every Go unit/integration package. The SQLite suite took
186.1 seconds. The [ingestion documentation](native-ingestion.md) also clarifies
that token revocation controls access to Stash's API; scraper website credentials
remain with gallery-dl.

## Verified content identities and file generations

Migration 1000018 adds shared SHA-256 content identities and immutable verification
history for each file generation. File UUIDs remain separate from content and
scene/image identities: equal bytes do not merge library items or their metadata.
Location, size, modification time, and matching-fingerprint changes advance the
file's generation. Ordinary file updates require the generation read by the
scanner or task. Fingerprint updates preserve identical rows, avoiding spurious
generation changes on an unchanged rescan.

`PreparedMedia.RecordContent` retains the descriptor through publication, records
the reviewed root revision and exact relative path, and checks the root, file,
and generation before commit. Changes or closure before commit roll back the
proof and associated writes. Historical proofs retain their original root
definition through later edits and survive file deletion and UUID adoption.
Indexed content lookup returns only active, current file generations. Anonymised
exports remove content hashes and private verification evidence.

Focused SQLite and media-publication tests passed for shared bytes, stale scans,
replaced/deleted paths, root revisions, immutable history, generation changes
before commit, disabled roots, closed descriptors, UUID adoption, managed
transactions, migration, and anonymisation. The existing file/media fixtures now
retain storage-assigned generations while continuing to compare complete records.

The final isolated copy migrated from 1000017 to 1000018 in 0.304 seconds. All
119 retained tables matched by streaming semantic digest, with zero foreign-key
violations and no size growth. All 769,643 existing files start at generation 1;
both new content tables remain empty, so old checksums are not claimed as newly
verified bytes. The private receipt is
`.local/native-archive-rehearsal-20260930/file-content-final-reconciliation.json`.
This copy remains a library-schema rehearsal without catalog imports. Production
has not changed, and the ingestion API still advertises `file_ingestion: false`.

The final complete `make validate-fork` gate passed: generation, frontend checks,
all 528 UI tests, retained v3 extension contracts and 71 application operation
files, Go lint with zero issues, and every Go unit/integration package. Durable
file jobs and atomic file/source/gallery/completion-receipt publication remain
the next ingestion work.

## Durable archive jobs and publication

Migration 1000019 stores archive jobs, immutable submission acknowledgements,
and per-attempt history. Replayed submissions return their original job even
after completion. Equivalent active submissions share one job; different work
sharing a resource cannot run concurrently. Pending capacity, attempt limits,
bounded recovery, and indexed pagination keep queue work bounded. Repeat
submissions may promote priority but cannot bypass retry delays.

Claims record an owner, deadline, and increasing fence. Renewal, progress, and
publication require that exact unexpired lease. Cancellation uses the reviewed
job revision and invalidates ownership. Expired attempts remain in history and
are requeued or failed at their attempt limit. `job.Durable.Publish` commits
domain writes and the result together, checking expiry and final job state before
commit. Failures roll back both, including expiry inside an earlier commit hook.
Result/progress JSON is bounded and error codes exclude raw worker output.

Focused tests passed for coalesced and conflicting replay, queue capacity,
concurrent submissions/claims, restart recovery, stale owners, renewal, deferred
retry, retry exhaustion, shared destinations, cancellation, SQL immutability,
atomic domain/result rollback, query plans, schema-18 promotion, startup guards,
and anonymisation. The concurrent claim and bounded recovery tests also passed
under Go's race detector. The pinned linter reported zero issues after correcting
the error-code predicate and test cursor cleanup.

The isolated full copy migrated from 1000018 to 1000019 in 0.060 seconds. All
121 retained tables matched by streaming semantic digest, with zero foreign-key
violations and no size growth. Existing file generations and content history
remained intact; all three job tables begin empty. The private reconciliation
receipt is `.local/native-archive-rehearsal-20260930/archive-job-reconciliation.json`.

The only accepted job kind is currently `media.verify`. File-completion admission,
path/file reservations, the actual worker, file/source/gallery/receipt integration,
source-run coordination, and producer/host/n8n delivery remain subsequent work.
No worker starts automatically, no legacy journals were imported or retired,
and production remains on the pinned compatible release.

The final complete `make validate-fork` gate passed with all corrections included:
generation, frontend checks and 528 UI tests, retained v3 extension contracts and
71 application operation files, zero Go lint issues, and all Go unit/integration
packages. The SQLite suite completed in 204.5 seconds and the API suite in
118.9 seconds.

## Verified file and media publication

Migration 1000020 retains regular-file path removal counters independently of
file lifetimes. Deletion, rename, and folder moves invalidate queued intake,
including when a concurrent scan created and removed a file after admission.
Recreation retains the removal counter; delayed completions cannot automatically
restore an absent removed path. Ordinary scans can establish a new file lifetime.
Anonymised exports remove path history after rewriting paths, and startup checks
require the new table, lookup index, and triggers.

File publication validates the original descriptor and any case-insensitive
database path spelling, reuses a concurrent scan's file record, and preserves
generated fingerprints for unchanged verified bytes. Media publication reuses
the exact file owner or one unique owner of current verified bytes. Multiple
owners and incompatible kinds require review; legacy matching fingerprints and
stale proofs cannot establish these associations. Existing media metadata stays
intact. File, proof, scene/image association, and durable job result commit
together; final checks also reject deleted or detached media.

Focused SQLite and ingestion tests passed for removals/recreation/restart,
case-folded path history and descriptor aliases, migration/startup/anonymisation,
concurrent scans, replay, UUID adoption, unchanged generated fingerprints,
verified/unverified/stale/ambiguous matches, exact-owner precedence, image/video
classification, metadata preservation, indexed candidate lookups, and atomic
rollback after file/media removal, detachment, conflicting ownership, physical
replacement, or lease expiry.

The isolated full copy migrated from 1000019 to 1000020 in 0.062 seconds. All
124 retained tables matched by streaming semantic digest, with zero foreign-key
violations and no size growth. The new removal history starts empty and all
769,643 existing file generations are preserved. The private receipt is
`.local/native-archive-rehearsal-20260930/file-publication-reconciliation.json`.

File-completion HTTP admission, the actual worker, source/gallery/receipt
integration, durable generated assets/notifications, and producer conversion
remain unfinished. No new worker starts automatically, `file_ingestion` remains
false, and production remains pinned to the compatible release.

The complete `make validate-fork` gate passed: generation, frontend checks and
all 528 UI tests, retained v3 extension contracts and 71 application operation
files, zero Go lint issues, and every Go unit/integration package. The SQLite
suite completed in 236.0 seconds, API tests in 136.3 seconds, and ingestion tests
in 125.3 seconds. The final checks include the query cursor cleanup correction.

## Collection, source attachment and album publication

`PreparedMedia.PublishIntake` now combines verified file/media publication with
collection provenance, attachment evidence and source album synchronization.
The captured collection revision must permit the root/path, and the current
definition must still be active and permit that location. Historical label edits
do not invalidate accepted provenance. A referenced capture must belong to that
collection revision and contain the attachment. Manual intake records provenance
without inventing a scraped account, capture or gallery.

An existing attachment media choice directs an unowned file to the selected
item, resolving canonical UUID adoption. The same choice can select one owner
of an intentionally shared file without moving or merging other owners. Existing
file ownership is preserved when it conflicts with the selected source item;
evidence is retained for review. Deleted selections require review instead of
recreation. Explicit unlinks, disabled albums, and manually excluded gallery
members remain intact. No publisher is implicitly assigned as a depicted performer.

Unambiguous observed attachments can establish their media association and add
arriving items to a shared source album. Replays retain one intake/evidence fact
and reuse the gallery. File, content proof, library media, collection provenance,
source choices, album changes, and durable job result share the transaction;
failure rolls them back together. Before-commit checks reject disabled collection
scope, removed media/file associations and changed source selections.

Focused tests passed for partial album arrivals, replay, manual membership
exclusion, direct unsourced intake, preserved explicit choices, conflicting
evidence, deleted/adopted selections, shared files with more than two owners,
selected-target priority over a separate byte match, historical/current scope,
and gallery-failure rollback including the job outcome. Indexed ownership
lookup was checked read-only against the full schema-20 library copy: both UUID
lookups and media/file joins used indexes; 1,000 repeated cached lookups took
0.0033 seconds locally. This is not an end-to-end ingestion throughput claim.
The private query-plan receipt is
`.local/native-archive-rehearsal-20260930/intake-owner-lookups.json`.

No schema migration is added in this checkpoint. Field policy and performer
defaults, durable generated assets/notifications, file-completion admission and
receipts, the actual worker and producer conversion remain unfinished.
The ingestion API still advertises `file_ingestion: false`; production remains
on the frozen compatible release.

The complete `make validate-fork` gate passed after the selected-media and lint
corrections: generation, frontend checks and 528 UI tests, retained v3 extension
contracts and 71 application operation files, zero Go lint issues, and every Go
unit/integration package. The SQLite suite completed in 268.4 seconds, ingestion
in 241.5 seconds, and API tests in 135.3 seconds.

## File event admission, worker and runtime

Migration 1000021 connects immutable file-event receipts to durable verification
jobs while preserving all source-capture receipts. File intake can reference an
acknowledged source attachment, or omit source data for manual media. Admission
checks the producer's Stash API scope, final root-relative file, collection
revision and attachment membership. Receipt failure rolls scheduling back.
Replaying the same event returns its original receipt, including after restart,
file disappearance or API token rotation; changed bytes under that event conflict.
Website credentials remain entirely with the scraper.

The v3 HTTP server exposes `file.completed` and a receipt status route when its
media tools are configured. File admission returns 202 and never calls a claimed
hash verified. The worker hashes/probes outside SQLite, renews its lease, and
commits verified registration with a resumable progress checkpoint. It then
creates missing previews and delivers scene/image and gallery hooks with stable
event identities. Errors remain pending/failed; retries retain the original
creation/link facts and do not create another item. Plugin delivery is at least
once. Cancellation waits for the worker before SQLite closes; expired attempts
are recoverable after restart. General edit-hook persistence remains unfinished.

Status separates `registration_committed` from `media_ingested`. An effect failure
or cancellation does not hide an earlier committed registration. Final validation
rejects changed roots/scopes, replaced files, obsolete generations and removed
ownership. Existing cover choices remain protected. File case detection now uses
filesystem identity instead of equal timestamps and skips letters without case.

Focused tests passed for duplicate and concurrent delivery, token rotation,
source/manual scope, rollback, restart, retry delays, cancellation, heartbeat
renewal, checkpoint expiry, descriptor changes before final commit, HTTP lifecycle,
actual image/video preview generation, and plugin retry identities/cancellation.
Schema-20 receipt promotion and anonymisation with file jobs also passed.

The isolated full copy migrated from 1000020 to 1000021 in 0.680 seconds. All
125 retained tables matched by streaming semantic digest, with zero foreign-key
violations and no size growth. Reconciliation took 49.0 seconds. The private
receipt is `.local/native-archive-rehearsal-20260930/file-ingest-reconciliation.json`.

This development capability does not switch existing gallery-dl/n8n producers.
Native field policy and performer defaults, source-run leases/coalescing,
additional source adapters, producer outboxes, catalog import, review UI, and the
remaining transition phases are still required. Production remains pinned to
the compatible release; no production database or catalog was migrated.

Targeted race checks passed for worker publication/resumption/renewal, the HTTP
worker lifetime, and plugin cancellation (ingestion 25.4 seconds, API 3.8 seconds,
plugins 1.1 seconds). Receipt replay also remains available when media tools are
unavailable, while new admissions are rejected.

The complete `make validate-fork` gate passed on the final implementation:
generation, frontend checks and all 528 UI tests, retained v3 extension contracts
and 71 application operation files, zero Go lint issues, and all Go packages.
The API suite completed in 135.1 seconds, ingestion in 304.1 seconds, manager in
43.2 seconds, and SQLite in 285.6 seconds.


## Native metadata policies and ordinary scans

Migration 1000022 makes collection policy definitions and their immutable history
native database records. Automatic field decisions retain a foreign-key reference
to the applied policy revision. Scoped directory matching uses root/path indexes;
performer canonical/alias matching uses the native name index, reports collisions,
and resolves reviewed UUID defaults through merge/adoption redirects.

The shared evaluator constrains targets to curated native field schemas. It
preserves manual and legacy choices, explicit empties, source alternatives, and
existing organized state. Filename initialization requires an empty inherited
title and does not oscillate when another file joins an item. A newer capture of
the same post can update an inherited value; competing posts require review.
No publisher or source account becomes a depicted performer implicitly.

Verified intake pins the current policy at admission and applies it inside the
registration checkpoint. A later policy/scope edit produces a review outcome
without losing the verified media. Ordinary scene/image scan handlers use the
same evaluator and record direct/manual intake provenance without inventing a
scrape. Eligible unchanged files are evaluated during rescans. Ambiguous directory
rules do not select a winner by enumeration order.

Application-authenticated root/collection/policy endpoints, typed field discovery,
and preview/apply now exist under `/api/v3/archive`. Preview can show disabled
rules and protected alternatives; applying requires a current digest. Jq data
contains the selected source, entity, and relevant file context, with no internal
plugin settings. The plugin and native policy services share one bounded jq
implementation while retaining their different documented input limits.

The full library copy migrated from 1000021 to 1000022 in 0.059 seconds. All
125 existing tables matched by streaming semantic digest, with zero foreign-key
violations and no file growth. Reconciliation took 50.0 seconds. No policies,
decisions or intake facts were fabricated. The private reconciliation receipt is
`.local/native-archive-rehearsal-20260930/metadata-policy-reconciliation.json`.

The backend integration remains separate from the unfinished native settings and
review UI. Installed mapping conversion/comparison, ZIP-member folder policies,
review-queue persistence for scan conflicts, source-run coordination, producer
conversion, catalog import and the remaining transition phases are still needed.
Production and existing gallery-dl/n8n writers remain unchanged.

Focused tests passed for immutable/dry previews, protected alternatives, disabled
policies, explicit empty values, typed target validation, canonical/alias
collisions, redirected performer UUIDs, partial name matches, source precedence,
queued policy changes, actual scene/image scan handlers and idempotent rescans,
HTTP guarded apply, and anonymisation of populated policy history. The actual
directory-matching SQL uses `source_collection_directory` for its root/prefix
lookup; this was checked read-only on the full copy. A separate jq regression
confirms larger native input does not relax plugin input or shared output limits.

The final `make validate-fork` gate passed: backend generation, all 528 UI tests,
71 native application operation contracts, zero Go lint issues, and every Go
unit/integration package. API tests took 155.1 seconds, ingestion 332.4 seconds,
manager 43.6 seconds and SQLite 306.0 seconds. Targeted race checks also passed
for SQLite policy application, queued file-policy processing and HTTP preview/
apply (14.9, 8.2 and 7.3 seconds respectively). The final logs are
`/tmp/stash-native-transition/metadata-policy-final-validation.log` and
`/tmp/stash-native-transition/metadata-policy-race.log`.


## Native source-run coordination

Migration 1000023 persists source traversal requests, coalesced windows, worker
leases, checkpoints, attempt outcomes, target cooldowns and owner review actions.
This specialized traversal state is separate from immutable media-verification
jobs. Host and n8n producer identities can submit equivalent work for the same
configured collection. Expanding a running date range keeps its missing portions
without queuing another complete scan of the already claimed range. Disjoint
windows preserve their gaps, with explicit queue and interval capacity limits.

Claims serialize a collection, shared target URL and overlapping destinations;
root identity and resolved paths account for host/container mount mappings and
existing symlinks. Newest pending ranges and pending download work take precedence
over older ranges and enrichment. Every claim advances a fence. Lease expiry,
retry, cancellation and process restart cannot let an obsolete worker finish a
newer attempt. Same-window retries retain their cursor; changing the traversal
window cannot blindly reuse that cursor. Repeated failures remain durably deferred
until application-authenticated review. Repeated timers preserve retry delays.

Producer routes expose typed submit, scoped listing/status, claim, renewal,
progress, completion and attempt history. Policy fingerprints identify effective
worker configuration without accepting commands or storing website credentials.
API-token rotation preserves producer identity. Late source evidence delivery
does not alter run state. The application-only review endpoint uses the reviewed
revision for retry/cancellation. The protocol is documented in native-ingestion.md.

The isolated full library copy migrated from 1000022 to 1000023 in 0.070 seconds.
All 128 existing tables matched by streaming semantic digest; prior migration
history also matched, with zero foreign-key violations and no file growth.
Reconciliation took 154.4 seconds while other validation ran. All five new run
tables remained empty. The private receipt is
`.local/native-archive-rehearsal-20260930/source-run-reconciliation.json`; the migrated copy is
`native-source-run-final-rehearsal.sqlite`.

Focused tests cover real SQLite concurrency, duplicate admission/claim responses,
wider and disjoint windows, capacity, mount/directory exclusion, token rotation,
root-scoped history, retry/deferral, cancellation, restart recovery, expiry during
publication, HTTP operations, late capture delivery, and anonymisation. Targeted
race checks passed (SQLite 7.0 seconds, API 2.5 seconds, interval/path helpers
1.1 seconds). Existing gallery-dl/n8n processes and journals remain unchanged.

The external adapter, local outage coalescing/outbox, actual shared filesystem
locking and lease enforcement, legacy checkpoint/deferral migration, source
adapters, catalog import, UI/client conversion and later transition phases remain
required. A coordinator lease does not physically stop an old process; production
cutover must verify the converted workers pause before further source requests
and preserve download locks. No production schema or writer was changed.

The complete `make validate-fork` gate passed on the final per-item checkpoint
contract: backend generation, all 528 UI tests, 71 native operation contracts,
zero Go lint issues, and every Go unit/integration package. API tests took
160.7 seconds, ingestion 343.1 seconds, manager 44.9 seconds, and SQLite 318.9
seconds. The final validation log is
`/tmp/stash-native-transition/source-runs-final-validation.log`.
Final race results are in `/tmp/stash-native-transition/source-runs-final-race.log`.

## Producer retention and durable delivery

The supported Python package now lives under `integrations/gallery-dl`, with a
standard-library outbox, bounded HTTP delivery and an inspection/retry CLI.
Source payloads must satisfy the shared retention policy before persistence;
runtime extractor objects and known secret fields are removed from the copied
metadata. Added common fixtures cover unusable rendition URLs and nondecimal
dimensions so Go and Python select the same retained preview.

The outbox has its own explicit SQLite lineage, stable origin/producer binding,
FULL WAL transactions, source-before-file dependencies and bounded queued bytes
and events. Concurrent drainers use expiring fenced leases. A server receipt must
match the event identity, digest, collection revision, root, run and kind before
acknowledgement and payload release commit together. Receipt rows remain for
replay and dependent events. Restart, lost HTTP responses and Stash API token
rotation preserve event identity. Transient errors back off durably; rejected
events retain their payloads for explicit review. File admission remains distinct
from completed verification. No website credential is stored or managed here.

The Python tests cover shared retention, strict input, capacity, process death,
concurrent claims, receipt dependency/replay, endpoint binding, header-only token
rotation, redirects, capability outages, partial responses, mismatched receipts
and disabled file processing. The real Go HTTP/SQLite test runs the Python client,
loses a response after the commit and confirms receipt replay plus independent
rejection. Targeted interoperability passed in 1.5 seconds. The build workflow
and `make validate-fork` now include producer validation; Python 3.12 or newer is
required. All 18 Python tests passed on Python 3.14 and in the installed gallery-dl
Python 3.12 environment. Building a wheel and installing its CLI into an isolated
environment also passed. The final real HTTP test and shared retention corpus
passed under Go's race detector (7.0 and 1.1 seconds); final Go lint reported zero
issues. Python source reads are registered with Go's test cache so producer edits
invalidate the interoperability result.

Actual gallery-dl hooks, native source-run leases, filesystem locks, launcher
conversion, local outage run coalescing and catalog import remain unfinished.
Integration inspection also found that ordinary single-media Reddit posts need
explicit attachment evidence in the server adapter before their file events can
be linked; the existing capture parser handles declared Reddit galleries and
Twitter media manifests. Do not omit attribution or invent albums to bypass that
remaining work. Host/n8n configuration, production data and the frozen compatible
release remain unchanged. This increment changes no native database schema.

The complete `make validate-fork` gate passed: backend generation, all 528 UI
tests, 71 native operation contracts, all 18 producer tests, zero Go lint issues
and every Go unit/integration package. API tests took 177.8 seconds, ingestion
345.0 seconds, manager 44.1 seconds and SQLite 323.1 seconds. A subsequent final
test-cache dependency update passed targeted race tests and Go lint. Logs are
`/tmp/stash-native-transition/producer-outbox-final-validation.log`,
`producer-outbox-final-race.log`, `producer-outbox-final-lint.log`, and
`producer-outbox-python312.log` in that directory. The isolated wheel/CLI check
is under `/tmp/stash-native-transition/producer-wheel.Tll9Rf`.

## Gallery-dl lifecycle and source ownership

The development producer SDK now connects actual gallery-dl download/skip hooks
to native source leases and its durable outbox. Captures are queued before
download. Final files are flushed, hashed and queued with their capture dependency
before gallery-dl's archive is acknowledged. Existing unarchived files can retry
failed metadata/exec processing; unresolved archive skips retain source evidence
without inventing a completed file. Atomic metadata writes and the existing
GIF-to-MKV output convention are supported. UTF-8 filename budgeting preserves
source IDs and reserves space for configured temporary files and yt-dlp formats.

Run responses include the target URL and destination prefix from their immutable
collection revision. Historical responses keep that definition after collection
edits. The worker verifies its reviewed local root identity and prefix, shares
destination locks across writers, disables asynchronous source prefetch and
checks ownership before traversal and each source HTTP attempt, including retries.
Lease deadlines use server time and a conservative monotonic budget. A current
file can finish queuing after ownership loss; subsequent source work pauses.
Bounded cursor replay preserves the prior checkpoint until it is encountered.

The server attachment parser now accepts direct single-media Reddit evidence,
including explicit video stream URLs and validated crosspost context. Single
items can link to library media without creating a gallery. The Twitter adapter
preserves original attachment membership before gallery-dl expands video previews
or transforms tweet metadata, retaining each output's attachment ID. Source order
and identity do not come from local output numbers or filenames. The parser policy
is `captured-attachments-v2`; the retained payload policy is unchanged.

Actual runtime fixtures cover download/skip paths, archive recovery, failed or
missing postprocessors, capacity exhaustion, transformed paths, source retry
fencing, missing resume cursors, destination checks and both raw/transformed
Twitter payloads. A real Python-to-Go HTTP test covers source-run submission,
claim exclusion, renewal, checkpoints and completion in addition to durable
delivery/replay. Backend publication tests confirm that a verified single Reddit
attachment links without an album. The isolated pinned runtime installs with
`make pre-producer`; CI and the full fork gate require the runtime tests.

All 59 Python tests passed on Python 3.12 and 3.14. Targeted Go race tests passed for the
parser, pinned definitions, single-attachment publication and real HTTP client
(1.0, 15.2, 7.5 and 7.2 seconds respectively). No native migration is added.
The production host/n8n wrappers, website credentials, catalog writers and frozen
compatible deployment have not changed.

The SDK is not yet a production launcher. Effective window/configuration
handling and policy fingerprints, durable offline run requests, metadata-only
enrichment, other platform/external-host adapters, multi-entry yt-dlp output
association, legacy checkpoint migration and host/n8n/recovery conversion remain
required. Catalog import and later transition phases are also unfinished.

The complete fork gate passed: backend generation, 528 UI tests, 71 native
operation contracts, zero Go lint issues and every Go unit/integration package.
API tests took 173.4 seconds, ingestion 345.1 seconds, manager 44.8 seconds and
SQLite 318.8 seconds. The producer suite had 58 tests during that gate; the added
source HTTP-retry regression and final adapter passed all 59 afterward on both
Python versions. Logs are `producer-lifecycle-final-validation.log`,
`producer-lifecycle-final-python.log`, `producer-lifecycle-python312.log` and
`producer-lifecycle-race.log` under `/tmp/stash-native-transition`.

## Offline source requests and admission receipts

The producer now records scheduled source requests locally without requiring
Stash to be online or starting a downloader. Requests with the same collection
revision, operation, policy and cooldown coalesce overlapping date windows while
preserving gaps. New downloads take priority over enrichment, with newer ranges
first. Once a request is claimed for submission, its UUID and exact bytes remain
immutable across backoff, worker interruption and lost responses. Wider requests
retain only the ranges outside that frozen submission. A review state holds that
configuration until an explicit retry; repeating a timer cannot bypass it.

Optional caller tickets identify one scheduling execution. Replaying a ticket
and its original absolute window after a lost command response returns the
existing intent, even after server admission. Reusing the ticket for different
work conflicts. Local history is paginated and records admission separately
from actual run completion. The native POST response now echoes its committed
request UUID and advertises submission receipts as a capability, allowing the
producer to release exactly the acknowledged request.

Producer database schema 2 adds request groups, frozen submissions and caller
tickets transactionally. Migration fixtures retain schema-1 event bytes,
dependencies, receipts and in-flight delivery ownership. Concurrent opening,
wrong-destination rollback and interrupted migration are covered. No native
Stash schema migration is added. Shared Python/Go window fixtures now cover
overlap, adjacency, gaps, UTC normalization and millisecond precision, including
offsets that carry dates outside the supported year range.

All 75 Python tests passed on Python 3.12 and 3.14. Tests cover offline timers,
concurrent submission claims, leases, process death, capacity, caller replay and
review. The real Python-to-Go HTTP fixture loses a committed run-submission
response, reopens the producer database and recovers the same request and run.
Focused Go race tests passed for source-window algebra and HTTP interoperability.
The full fork gate passed backend generation, 528 UI tests, 71 native operation
contracts, producer tests, zero Go lint issues and every Go unit/integration
package. API tests took 161.3 seconds, ingestion 336.8 seconds, manager 43.9
seconds and SQLite 309.6 seconds. Logs are `source-request-final-validation.log`,
`source-request-python312.log`, `source-request-focused-go.log` and
`source-request-race.log` under `/tmp/stash-native-transition`.

Effective worker configuration and window enforcement, other source adapters,
metadata enrichment and actual host/n8n/recovery conversion remain. The live
launchers and catalog writers have not switched, and production remains pinned
to the compatible release. Catalog import and later transition phases remain
unfinished.

## Claimed source windows and postprocessor boundaries

The download adapter now owns enforcement of the claimed source-post window.
The lower bound is inclusive and the upper bound exclusive. Reddit's raw
publication timestamp and Twitter's Snowflake retain the milliseconds discarded
by gallery-dl's formatted dates; raw and transformed Twitter payloads agree.
Unknown dates stop before file processing. Older pinned posts are skipped
without ending traversal, and linked child downloads inherit the accepted
parent post's date instead of using an unrelated child upload date.

Per-extractor date configuration replaces inherited limits without editing the
shared configuration file. Other filters, skip rules, pacing and authentication
remain with the configured worker. Keywords cannot overwrite the source identity
or date used for the window. Postprocessor initialization waits for an accepted
post. Initial and subsequent proposed directories are formatted with gallery-dl's
own path implementation and checked against the claimed destination before init
or post callbacks run.

Real gallery-dl fixtures cover Reddit pagination, raw/transformed Twitter albums,
subsecond boundaries, all-outside results, pinned posts, parent/child date
semantics, retained filters and invalid destinations before callbacks. All 85
producer tests passed on Python 3.12 and 3.14. The full fork gate passed backend
generation, 528 UI tests, 71 native operation contracts, zero Go lint issues and
all Go unit/integration packages. It ran 84 producer tests before the final
initialization/directory regression; the final 85 passed on both runtimes after
that addition. API tests took 158.6 seconds, ingestion 332.7 seconds, manager
43.8 seconds and SQLite 306.1 seconds. Logs are
`source-window-final-validation.log`, `source-window-final-python.log` and
`source-window-python312.log` under `/tmp/stash-native-transition`.

Read-only inspection of the effective host and n8n Reddit/Twitter configuration
confirmed that their source keywords pass the new check, and both still resolve
the existing prepare/after/skip catalog writers. Those launch paths have not
switched. Full configuration fingerprints, launcher conversion, additional
source adapters and the later migration phases remain unfinished. No native
schema change or production deployment is included here.

## Portable worker profiles and attempt execution

The producer now accepts a reviewed `stash-gallery-worker-v1` profile. Portable
gallery-dl settings, local path bindings, reviewed helper assets and private
website-access references have separate roles. Policy identity includes the
settings, asset digests, pinned gallery-dl/yt-dlp runtime and adapter source;
host/container mount paths and rotated website logins do not change it. Roots
and shared lock directories require explicit device/inode identities. Python
postprocessor functions must name the reviewed asset exactly; changed helpers
and legacy catalog writer definitions stop source work. Trusted local processors
remain executable worker code, not a sandbox supplied by Stash.

Website credentials are read locally from existing JSON settings through a
JSON Pointer or a named environment reference. They stay with gallery-dl and
are excluded from policy serialization and the outbox. Stash ingestion API
tokens still have their separate environment reference. Configuration activation
rejects concurrent use of gallery-dl's process-global settings and restores the
previous settings when the attempt ends.

The CLI can validate a profile, use its digest when queuing a request, and execute
one admitted download attempt. Execution checks capabilities, root, operation,
policy and live ownership before extraction. A separate drainer owns its SQLite
connection and delivers captures while downloading continues. Source failure,
storage/capacity failure, changed assets, interruption and lost ownership retain
retry/deferred/paused states. Current-file evidence remains durable. Python and
subprocess logs go to stderr, preserving the JSON command result on stdout.

After a lost finish response, the worker checks the server's attempt history for
the exact producer, owner, fence and outcome; it never infers completion merely
because ownership disappeared. Successful source attempts remain distinct from
remaining source windows, event admission and verified media intake. Final lease
responses now validate the immutable definition and allowed resulting states.

The real Python-to-Go fixture runs the download adapter against the HTTP router
and native SQLite, delivers a capture during download, loses a committed finish
response and recovers it, then reopens the producer database and admits the
dependent file. The native file job remains queued and no library file is
declared verified. Unit coverage also exercises portable paths, secret rotation,
asset changes, failure outcomes, exact attempt matching, drainer restart and
stdout isolation.

All 102 producer tests passed on Python 3.12 and 3.14. The full fork gate passed
backend generation, 528 UI tests, 71 native operation contracts, producer tests,
zero Go lint issues and all Go unit/integration packages. Focused API race checks
passed. After tightening the exact helper-asset reference, both Python suites
and the real Python-to-Go fixtures passed again. Logs are
`worker-final-validation.log`, `worker-final-python.log`,
`worker-final-python312.log`, `worker-http-final.log` and `worker-http-race.log`
under `/tmp/stash-native-transition`.

Actual host/n8n configuration conversion and launchers, additional source
adapters, enrichment, catalog import and later transition phases remain. The
production deployment, website login configuration and existing catalog writers
have not switched. This checkpoint adds no native or producer schema migration.

## Local configuration conversion and source-scoped policies

`stash-ingest-config` now converts ordered gallery-dl JSON layers into a new,
inactive worker profile. Its merge retains object insertion order and replaces
arrays/scalars as the pinned gallery-dl runtime does. Recognized catalog
prepare/complete hooks are removed from named, typed and inline definitions;
unknown legacy callbacks stop conversion. The converter preserves archive
segment-list formatting and existing download IDs, skip/early-stop policy,
original-quality flags, pacing and remaining processors. Local helper scripts
receive checked asset digests. Publication flushes a temporary file and links it
exclusively into place with private permissions; it never overwrites an input
configuration or an existing output.

Website-access fields retain references to the original private files. Layered
headers/cookies merge from their respective JSON Pointers, including ancestor
replacement semantics, without copying secret values into the profile. Known
yt-dlp credential argument values use references while other arguments, including
format selection, remain part of policy identity. Conditional filename/directory
map order is now retained both when saving a profile and computing its digest;
sorting those maps would change first-match behavior without changing the old
digest.

Optional source categories constrain the root extractor before source work.
Twitter profiles omit unrelated service settings. Reddit profiles can do the
same with its finite supported child-host whitelist, retaining child base and
parent-specific settings. Unknown dependency graphs retain their configuration.
Unused named processors are omitted from scoped profiles. This matters for the
actual deployment: host ThisVid recovery refreshes cookies, while n8n asks for a
host refresh and exits. That intentional difference remains in full profiles
but no longer splits otherwise equivalent Reddit/Twitter policies.

Read-only conversion of the actual host configuration and merged n8n configuration
produced matching ordered portable settings and helper digests for Reddit and
Twitter. The n8n check ran in an isolated copy of its image, with networking
disabled and read-only mounts. Both mounts report the same media/lock identities.
The installed n8n gallery-dl reports version 1.32.15.dev0 but source commit
`0d2966061c5c5138a6961da26eab5583861962a1`, while the supported worker requires
`c40eb2a42fbaa1d26a2bb7c96804b7f47d1f73f8`. Its native profile validation rejects
that mismatch as intended. The runtime must be aligned before activation.

Private staging artifacts are in `.local/native-worker-conversion-20261001`.
Their shared root UUID is explicitly unregistered and for rehearsal only;
regenerate deployment profiles against the actual registered root after the
cutover gates. The packaged converter was installed and exercised in the isolated
producer environment. No live config, wrapper, workflow, credential, catalog or
production deployment was changed. Host/n8n/recovery launcher conversion,
additional source adapters, enrichment, catalog import and the later transition
phases remain unfinished.

Validation passed the full `make validate-fork` gate: 528 v3 tests, native
contracts for 71 operation files, Go lint and the complete Go test suite. All 113 producer tests passed
on Python 3.12 and 3.14. After the final argument-type guard, both Python suites
and the real HTTP producer/download-worker fixtures passed again. Logs are
`config-conversion-final-validation.log`, `config-conversion-final-python.log`,
`config-conversion-final-python312.log` and `config-conversion-http-final.log`
under `/tmp/stash-native-transition`. This checkpoint adds no schema migration.

## Scoped dispatch and a packaged n8n worker

Workers can now discover download work using `POST /api/v3/ingest/runs/ready`.
The bounded active-run index filters by the token's collections, exact media
root, reviewed policy and server-side eligibility time. Results contain only
run UUIDs and sequence cursors. Discovery never claims work: source execution
still requires the existing fenced claim, including definition, destination and
cooldown checks. Expired leases go through normal recovery and retry delay;
explicit deferrals remain outside automatic dispatch.

`stash-ingest dispatch --profile FILE` drains one event batch, submits one
offline request and scans one candidate page, executing at most one attempt.
Producer schema 3 stores pagination and discovery backoff in the same outbox.
Cursor revisions handle competing dispatchers, and advancing before execution
preserves fairness across crashes and pages of busy runs. Reaching a page end
wraps the cursor for a subsequent pass; it does not certify queue completion.
Existing schema-1/schema-2 payloads, receipts, leases, requests and tickets are
preserved transactionally. The Stash database schema remains unchanged.

`integrations/gallery-dl/Containerfile.n8n` now installs the pinned worker into
`/opt/stash-ingest` on an explicitly selected custom n8n base. It retains n8n's
entry point, PATH and system interpreter. The isolated rehearsal used base image
`2fcb84852f4a6dfc898796126764638ee43c2183a961061e42bcfe4103404a8c`; the resulting
local image is `localhost/stash-n8n-native-rehearsal:20261001`. Its worker validates
the supported gallery-dl commit rather than accepting the older system build's
identical version label. With the real configuration/helper/media mounts read
only and networking disabled, its Reddit/Twitter profile hashes and complete
worker runtime identity match the host profiles. No source requests were made.

The full fork gate passed: 528 v3 tests, native contracts for 71 operation files,
Go lint and all Go tests. All 122 producer tests passed on host Python 3.12 and
3.14, and against the installed package in the n8n rehearsal image. The real
HTTP fixture now invokes the dispatch CLI after outbox restart, verifies queued
submission/discovery, recovers a lost finish response and retains file intake
as a separate durable result. Migration fixtures cover populated schema-2
queues as well as concurrent schema-1 upgrades and transaction rollback.
Logs are `dispatch-final-validation.log`, `dispatch-python312.log`,
`dispatch-n8n-python-final.log` and `dispatch-n8n-profile-validation.json`
under `/tmp/stash-native-transition`.

A read-only inventory of n8n's current and published workflow graphs is staged
at `.local/native-launcher-inventory-20261001.json`, with command hashes and
connections rather than raw commands or credentials. Workflow conversion still
needs to retain caller receipts and account backfill decisions. Host/n8n
launchers, activation, additional adapters, enrichment, catalog import and later
phases remain unfinished. The production image tag, services and workflow
database were not changed.

## Completion tied to original caller requests

Caller tickets now retain the source submissions assigned to each part of their
original time window. Assignments are written in the same transaction as frozen
requests, including when a new caller shares an existing in-flight request. A
later successful rescan cannot make an earlier cancelled ticket appear complete.

`stash-ingest ticket-status UUID` validates the original admission identities and
reads each assigned native run's actual completed windows. It reports remaining
coverage, deferral, cancellation, review and API outages, and exits successfully
only when the entire requested source range is covered. A wider shared run may
still have other work. File intake remains a separate status; source completion
does not assert that its media has finished importing.

Producer schema 4 adds ticket assignments and unassigned ranges. Its transactional
migration reconstructs the first covering submissions from the old tickets'
original request sequences, preserves later rescan boundaries and retains
existing event, lease, receipt and dispatch state. Both assignment and migration
use bounded pages. The Stash database schema remains unchanged.

All 134 producer tests passed on Python 3.12 and 3.14. Migration coverage includes
populated schema-3 queues, multiple pages of waiting tickets and rollback on
invalid input. The real HTTP fixtures verify queued/running/completed ticket
states after lost admission responses and check the CLI after restart while
media intake remains queued. The full `make validate-fork` gate passed: 528 v3
tests, native contract validation, Go lint and all Go tests. Logs are
`ticket-completion-python312.log`, `ticket-completion-http-final.log` and
`ticket-completion-final-validation.log` under `/tmp/stash-native-transition`.

Inspection of the current n8n backfill wrapper found that it records completion
from process exit status. Launcher conversion must instead use these original
ticket ranges and preserve existing permanent backfill decisions during import.
The host/n8n launchers and live services have not changed; the prior n8n rehearsal
image remains at `5af18a486` and must be rebuilt for this producer revision before
activation. Additional adapters, catalog import and later phases remain open.

Documentation now calls credential revocation **Stash API-token revocation**.
It controls a producer's access to Stash; website passwords, cookies and login
configuration remain with gallery-dl in the host/n8n worker environments.

## Scoped lookup for actual source URLs

`POST /api/v3/ingest/collections/lookup` now resolves up to 50 exact source URLs
against the producer's permitted collection IDs and current media-root binding.
Results group candidate UUIDs, revisions and states by the requested URL. They
exclude labels, account associations, directory names and local mount paths.
Every authorized duplicate remains visible; unrelated matches cannot consume
a page and hide a permitted candidate. Historical URLs do not redirect work to
a changed target; a moved collection does not match requests under its old root grant.

The Python adapter and `stash-ingest lookup-collections` validate the complete
response before returning bindings. Missing visible matches, duplicates and
inactive definitions remain explicit. This operation neither creates collections
nor queues source work. The real HTTP metadata and download fixtures now resolve
their source URLs through the API before submitting native requests, including
the CLI entry point for download callers.

The full `make validate-fork` gate passed, including 528 v3 tests, native contract
validation, Go lint and all Go tests. All 139 producer tests passed on Python
3.12 and 3.14. HTTP coverage includes 55 permitted duplicates among unrelated
matches, unbound collections, inactive states, changed targets/roots, rejected
credentials and API-token revocation. Logs are `collection-lookup-http-final.log`,
`collection-lookup-python312.log` and `collection-lookup-final-validation.log`
under `/tmp/stash-native-transition`. No database schema version changed.

A read-only check of the installed timers confirms that regular Reddit scans
include saved posts, and the weekly scan adds `--date-min-relative '1 week ago'`.
The saved lists currently contain 497 distinct first-token entries for Twitter
and 421 for Reddit. Those lists exceed the current 128-collection token limit;
bulk worker access and durable unresolved caller/list requests must be addressed
before the launchers switch. Collection registration/import, preservation of
full-history policy, n8n receipt conversion and the remaining transition phases
are still required. Production services and workflow graphs remain unchanged.

## Explicit media-root grants for bulk producers

Native schema 1000024 adds root grants for a producer's Stash API token. A grant
covers all registered collections at that logical root, including subsequent
additions, so the existing 497-entry Twitter and 421-entry Reddit lists no longer
require a grant per source. Named collection/root grants remain available and
retain their existing authority. Tokens hold at most 128 combined grant records;
neither form grants source administration. Root grants exclude unbound metadata.
Website logins and cookies remain with gallery-dl/n8n.

Capture/file admission, receipt reads, collection lookup, source-run admission,
dispatch and run history use the same permissions. Historical admission replay
checks the recorded root after a collection moves; a token for only the new root
cannot acquire access to the old run. Rejected admissions roll back. Issuance
requires active registered roots, and capabilities expose the explicit grants.
Root grants are created only through administration, never schema migration.

Collection lookup applies permission filters before its per-URL limit. It returns
up to 128 candidates with `has_more` when additional matches exist. The Python
client preserves that ambiguity and allows a bounded 4 MiB lookup response;
other responses retain their 1 MiB limit. The new target/root index supports
these lookups without returning unrelated collection information.

The migration preserves receipt values, including accepted file-job references.
SQL guards validate either form of permission and retain grant evidence while
receipts reference it. Populated fixtures verify named-token preservation,
receipt replay, queued file work, rejection of forged unbound receipts, root
movement, later collection registration, revocation and anonymisation. The real
Python HTTP download fixture now uses a root-only token through capture and
dependent file admission.

A concurrent outbox startup test exposed SQLite journal-mode lock contention.
Commit `88e87e162` adds a bounded retry without replacing the queue. A real reader
lock regression verifies preservation of pending events, leases and receipts.
All 141 producer tests pass on Python 3.12 and 3.14.

A separate 1,473,081,344-byte copy of the schema-1000023 rehearsal migrated in
0.132 seconds. Streaming semantic comparison checked all 133 existing tables:
no differences, no foreign-key violations, all 1,542,050 archive identities and
770,734 metadata baselines retained, and no invented root grants. Reconciliation
took 140.440 seconds; evidence is
`.local/native-archive-rehearsal-20260930/root-grant-reconciliation.json`.
The populated receipt fixtures cover values absent from this production-derived
copy, whose native producer/job tables remain empty.

The full `make validate-fork` gate passed: 528 v3 tests, native contract checks,
all 141 producer tests, Go lint with zero issues and the complete Go suite.
Logs are `root-grants-final-validation.log`, `root-grants-host-python.log`,
`root-grants-preservation-final.log`, `root-grants-outbox-contention.log`,
`root-grants-full-copy-migration.log` and
`root-grants-full-copy-reconciliation.log` under `/tmp/stash-native-transition`.

Durable caller/list requests, source registration/import, actual host/n8n
launcher conversion, full-history policy preservation and later transition
phases remain unfinished. No production service, configuration, workflow,
website login or live catalog was changed.

## Durable caller snapshots before collection lookup

Producer schema 5 adds `source_calls` and `source_call_targets`. A caller records
its execution UUID, ordered URL list, reviewed policy, root, operation and
absolute time window before network access. The command options have a stable
digest; retries retain the original snapshot without reopening changed or
missing list/profile files or recalculating relative time bounds. Concurrent
first requests keep the snapshot that commits first. Different options require
a different caller UUID.

`queue-sources` accepts a UTF-8 URL list and an absolute lower bound or a lookback
interval. `resolve-sources` binds up to 50 pending URLs through scoped native
lookup. Resolution uses fenced leases and retained backoff, prioritizing downloads
and newer cutoffs before backfill/enrichment. Each chosen collection/revision
and deterministic source ticket commit in the same outbox transaction; a failed
commit leaves neither half behind. Existing queue coalescing shares equivalent
work between different callers. Bound targets never silently change collection.
Missing, ambiguous, disabled or retired matches remain in review; `retry-call`
retries only those unresolved targets after their definitions/access are fixed.
The dispatcher now resolves one bounded page before source admission/discovery.

`calls-status` provides local counts and 50-target pages. `call-status` checks all
original source tickets, verifies their frozen definitions/root and requires
confirmed coverage of every requested window. It shares a bounded run-status
cache only within that inspection and limits detailed issue output to 20 URLs.
It cannot report successful media intake or let a later rescrape complete an
earlier cancelled caller request. Temporary API failure remains unavailable.

The schema-4 promotion fixture preserves every existing table row, including
pending events, active leases, receipt/ticket assignments and dispatch cursors.
Unknown conflicting tables roll back migration without recreating the outbox.
Existing schema-1/2/3 promotion tests still pass. No Stash database migration or
production queue conversion is part of this increment.

The required `make validate-fork` gate passed: 528 v3 tests, native contracts,
Go lint and all Go tests. The final producer suite passes all 154 tests on
Python 3.12 and 3.14. Coverage includes 500-source lists, offline admission,
frozen cutoffs, concurrent retries, expired resolution leases, atomic
binding/ticket rollback, priority, review, capacity, original-root validation
and whole-call completion. Both real HTTP producer fixtures pass without cached
Go results. The download fixture now records a URL-list caller through the CLI,
replays it after deleting the input file, restarts, resolves/submits/executes it,
recovers a lost finish response and checks both ticket/call status while actual
media intake remains queued. Evidence is in `source-calls-final-validation.log`,
`source-calls-python-final.log`, `source-calls-host-python-final.log` and
`source-calls-http-final.log` under `/tmp/stash-native-transition`.

The existing host helpers and n8n workflows still need their handle/list parsing,
Reddit mode/date expansion, full-history archive/skip policies and receipt/result
contracts converted to these caller records. Source registration/import,
additional extractors, actual launcher activation and subsequent transition
phases remain unfinished. Production services and data remain on the frozen
compatible deployment. Parent commit `f4edf75b7` passed all three CI workflows.

## Staged host launchers preserve scrape inputs

`stash-ingest-twitter` and `stash-ingest-reddit`, with matching scripts under
`integrations/gallery-dl/bin`, now record the installed host helpers' input
formats as durable source calls. Twitter keeps handle/ID/profile normalization,
first-token/comment handling and list order. Reddit keeps sorted accounts and
communities, exact profile/search URL expansion, new/top modes, saved targets
and date filters. Absolute dates take precedence; relative dates retain the old
gallery-dl boundary and freeze at first recording. Systemd invocation IDs derive
stable call UUIDs; other retrying callers supply an explicit UUID.

Default exit 0 acknowledges only local recording. Strict inspection returns
pending until every original source ticket succeeds and still directs callers
to separate file-intake receipts. Reviewed profiles replace arbitrary downloader
flag passthrough. Invalid/empty source lists cannot claim successful completion.
`stash-ingest-config --full-history` publishes a separate global `skip=true`
profile; full-history and Reddit top launchers require it. This preserves the
old override of extractor/child `abort:4` rules, including archived-file skipping,
while retaining any explicit date minimum.

A read-only comparison against the installed scripts' parsing functions and
actual saved lists matched every URL and its order: 497 Twitter targets, 416
Reddit users and four communities; Reddit new expands to 836 URLs (837 with
saved), top to 1,672 (1,674 with saved). No list lines were ignored. The comparison
and source-file hashes are recorded privately in
`.local/native-worker-conversion-20261001/host-launcher-comparison.json`.
Separate host full-history profiles were staged there and validated against the
actual Python 3.12 gallery-dl configuration, helpers and mount identities. Input
configurations were unchanged. These use the existing unregistered rehearsal
root and have not been activated.

All 164 producer tests pass on Python 3.12 and 3.14. The real Go/Python HTTP
download fixture now also executes the staged Reddit script, replays its call
after deleting the input list, restarts the outbox, resolves/submits/downloads,
recovers a lost finish response, and verifies source completion independently of
queued file intake. The focused HTTP fixture passed in 7.674 seconds.
The full `make validate-fork` gate also passed: 528 v3 tests, native contracts,
Go lint and all Go tests (API 187.977 seconds, ingest 338.767 seconds, SQLite
317.017 seconds). Logs are `host-launchers-validation.log`,
`host-launchers-python312.log`, `host-launchers-focused.log`,
`host-launchers-converter.log` and `host-launchers-http-initial.log` under
`/tmp/stash-native-transition`. This increment adds no database migration.

n8n conversion remains outstanding. Its old runner's result receipts and
permanent backfill decisions must be retained before switching workflow commands.
A read-only journal inspection found 1,329 completed components (497 Twitter,
416 Reddit new and 416 Reddit top), one per-scan completion and no legacy-skip
rows. Of those component records, 1,328 contain user-confirmation provenance;
40 explicitly say exhaustive history was unverified under the old `abort:4`
policy but accepted as complete. Import must preserve those decisions and their
provenance without inventing verified source-window coverage or rescraping them.
The old skip-table format must remain supported as historical input even when
this snapshot has no rows. Production launchers, workflows and services remain
on the compatible deployment; parent commit `66d7e91e2` passed all three CI jobs.

## Native permanent backfill decisions and journal import

Native schema 1000025 adds `source_backfill_decisions` and foreign-keyed original
request proof. Historical completion and deliberate skips retain the entire
source record, including the exact result JSON and user-confirmation provenance.
Stable UUIDs derive from an input-database UUID and the original table/key;
replaying unchanged rows preserves the original decision, while changed evidence
conflicts. Migration creates no decisions, runs or proof for existing rows.

The API now exposes root-scoped compact status and native completion proof.
Account-wide reads/writes require an explicit producer root grant; a collection
grant alone does not widen. Application-authenticated endpoints import historical
assertions in atomic bounded batches and expose retained evidence. The component
definitions preserve the eight existing Reddit/Twitter n8n modes. Qualified
source-account lookup is independent of performer identity or ownership.

Native completion checks the authenticated producer's original request hashes,
root, exact target URLs, policy and actual completed ranges for the entire
component. Queue admission, partial coverage, another account's URLs, missing
requests and fabricated windows cannot become completion. Imported acceptance
remains distinct from native source-run proof; neither certifies media intake
or exhaustive availability from a source website. Startup checks preserve proof
integrity, and anonymised exports remove this private operational history.

`stash-import-backfills` opens a journal snapshot read-only, validates the two
recognized account-backfill table shapes and all rows before network writes,
then submits at most 50 records/4 MiB per batch through the application API.
Both passes use one SQLite read snapshot. Stable returned IDs and outcomes are
checked before acknowledging a batch. The real Go/Python HTTP fixture commits a
batch and drops its response, then verifies successful replay with 52 decisions,
no duplicates, no fabricated source runs and unchanged source bytes.

The isolated full-copy rehearsal is
`.local/native-backfill-rehearsal-20261001/native-backfill-rehearsal.sqlite`.
Schema 1000024 → 1000025 took 0.071 seconds on the 1,473,081,344-byte copy.
Before importing, all 134 existing tables matched their source semantically;
the two new tables were empty and foreign-key violations were zero. The
reconciliation took 53.521 seconds and found no size growth.

The actual journal snapshot then supplied 1,329 completion records: 497 Twitter
and 416 each for Reddit new/top. Import and replay took 0.299 seconds through the
core repository services. Every original field and result-JSON byte matched;
replay and reopening preserved every decision. Source runs, source requests and
native proof rows stayed empty. Indexed status checks on this full-library copy
had median 0.01869 ms, p95 0.02498 ms and maximum 0.14606 ms in one read transaction;
these are local repository timings, not network or UI latency guarantees.
The snapshot has no legacy-skip rows, so synthetic fixtures cover those semantics.

Private evidence includes `review.json`, `input-records.json`,
`backfill-migration-reconciliation.json` and `backfill-import-reconciliation.json`
in that rehearsal directory. The preserved schema-24 copy and live journal were
not changed. Parent commit `7852533f1` passed all three CI workflows.

The full `make validate-fork` gate passed: 528 v3 tests in 91 files, native
contracts covering 71 application operation files, all 167 producer tests,
Go lint with zero issues and all Go tests (API 205.548 seconds, ingest 350.598
seconds, SQLite 329.962 seconds). The same 167 producer tests also passed on the
host's Python 3.12 runtime. Focused store/API checks and the real HTTP importer
fixture passed separately. Logs are `backfill-final-validation.log`,
`backfill-python312.log`, `backfill-focused-v2.log`,
`backfill-http-import-v2.log` and `backfill-real-import-rehearsal.log` under
`/tmp/stash-native-transition`.

The installed n8n runner still needs to consume this history, freeze/persist its
caller receipts, inspect original source tickets and submit completion proof.
Per-scan completions, deferred/ignored work, the other catalog families, source
registration and subsequent transition phases remain outstanding. This is not a
live migration or deployment; production remains on the frozen compatible image.

## Durable native n8n backfill callers and staged workflow conversion

`stash-ingest-n8n` records the existing eight account-backfill modes using stable
producer/workflow/execution/node/item identities. The first invocation freezes
the account spelling, exact targets, profile policy and full-history cutoff.
Replay returns the same token without reopening changed or missing profile
inputs. Normal record success means local acceptance; strict inspection remains
pending until native completion or an imported acceptance/deliberate skip.

Producer schema 6 adds `backfill_calls` to the existing outbox. Before any source
call exists, a root-authorized native history lookup must succeed. A needed
decision and its child source snapshot commit together. Historical completion
and skip create no source tickets. Fenced leases, bounded capacity and backoff
retain interrupted checks across restart. Migration preserves all populated
schema-5 tables, including pending/bound source calls and their original
submissions; unknown table collisions roll back instead of replacing history.

An active call remains tied to its original source tickets. Another call's later
account history cannot complete or replace unfinished/cancelled work. Completion
requires every target's assigned windows, and the adapter saves its exact proof
before sending it. A lost server response replays the same proof/decision UUID.
The dispatcher advances these calls alongside existing source requests, and its
exit status stays pending when a backfill check or completion is outstanding.
Finished workflow receipts remain local and separate from media-intake receipts.

`stash-ingest-n8n-config` stages the known command/inspection graph contract in
a new private file. It preserves workflow/node IDs, credential references,
unrelated parameters, inputs and existing error/success outputs. Converted
commands include stable execution context. Pending results retain their token,
enter a 90-second persisted Wait and inspect that same call again. Unknown
command/connection/result changes require review rather than speculative edits.

Read-only inspection found three active backfill child workflows. Each current
graph exactly matched its published version. Their three parent references all
explicitly wait for subworkflow completion. Private input exports, staged graphs
and review manifests are in `.local/native-n8n-adapter-20261001/`; the live n8n
database was not changed. The actual installed n8n evaluator resolved 62 command
and result expressions across the three conversions, including rejected account
and token text. Its real Wait implementation checkpointed all three waits and
retained the input token. That isolated check executed no scraper command.
A read-only comparison also matched all eight old runner modes: five direct
URLs and three delegated host expansions. Its source hash and result are in
`legacy-mode-parity.json` beside the staged workflow exports.

The real Go/Python download fixture now includes the packaged n8n command
contract, local record replay, pending inspection, native download completion,
a lost permanent-completion response, outbox reopening and exact-proof replay.
It separately verifies that file admission remains queued after source success.
Imported acceptance/skip, concurrent recording, fenced ownership, atomic guard
publication, unrelated later history and cancelled original jobs have focused
coverage. Parent commit `f5385af46` passed all three CI workflows.

The full `make validate-fork` gate passed: 528 v3 tests, the 71-operation native
contract check, Go lint with zero issues and all Go tests (API 210.155 seconds,
ingest 344.951 seconds, SQLite 323.350 seconds). After the final Python dispatch
pending-state fix, all 183 producer tests passed on Python 3.12 and 3.14 and in
the installed n8n package. The final real HTTP/download fixture passed in 11.722
seconds. Logs are `n8n-final-validation-v2.log`, `n8n-python312-final.log`,
`n8n-python314-final.log`, `n8n-http-final.log`, `n8n-image-python-final.log` and
`n8n-workflow-runtime-final.log` under `/tmp/stash-native-transition`.

The final isolated image is
`localhost/stash-n8n-native-rehearsal:adapter-final-20261001`, ID
`e1248da527bcde3276967768825b9cb339b71cf9a047945549a9910731b4edbc`, built from
the explicit existing n8n base `2fcb84852f4a…`. Its installed adapter fingerprint
matches the final workspace files. Read-only mounts of the actual ordered n8n
configuration, helpers, archives and media validated separate Reddit/Twitter
full-history profiles; their source files were unchanged. The profiles and
runtime review live under `.local/native-n8n-adapter-20261001/current/`, with
image/graph provenance in `rehearsal-review.json`. They retain the unregistered
rehearsal root UUID and have not been activated. This increment adds no Stash
database migration.

The remaining operational-history migration includes old result-file tokens,
per-scan completions, deferred/ignored work and the other catalog families.
Source registration, worker service configuration, profile/image activation,
recovery callers and all later transition phases remain outstanding. Converted
workflow files and profiles are staging artifacts, not a live deployment.

## Retained legacy n8n receipts and concurrent outbox startup

`stash-import-n8n-receipts` now validates a frozen receipt-directory snapshot
before opening the destination outbox. Apply requires the reviewed input digest
and a stable source UUID. One transaction retains every original token and JSON
byte, its classification, and an immutable import manifest. Replay after a lost
response preserves those records. Conflicting input bytes, source identities or
native caller tokens roll back the whole import. Unknown directory entries,
nonregular/changing files and malformed JSON block import; valid but unsupported
result shapes remain review records. Bounded capacity never evicts history.

Producer schema 7 adds `legacy_n8n_receipts` and `n8n_receipt_imports`. Promotion
preserves populated earlier delivery, ticket, source-call and backfill tables,
including pending leases and finished results. A native call cannot reuse an
imported token. Inspection recognizes the original token locally without opening
a network client or requiring the old directory. It distinguishes historical
success, deliberate skip, failure and review. A recorded network block remains
a failure even if the old child exited zero; this matters because the actual
Reddit workflows inspect only `command_failed`. Raw original fields remain in
the immutable receipt body. Historical results cannot be retried as native jobs.

Receipts contain no account/execution identity, so the importer does not infer
one from their log tails. It creates no source requests, permanent account
decisions or native completion proof, and never certifies file intake. Original
successful result flags remain historical claims. General producer status lists
these receipts separately from current work. Saved n8n execution graphs still
need drain/resume handling before their old command paths can be removed.

The installed-image concurrency test exposed an existing startup race: another
first opener could publish a schema between the version/application/table reads,
causing a valid outbox to appear foreign. Those checks now use one SQLite read
snapshot before the existing transactional migration and binding validation.
A deterministic regression publishes the schema between those reads and verifies
the original queued event survives. Concurrent receipt imports publish one
manifest, and an injected mid-import failure leaves no partial rows.

The actual source directory contained one 3,640-byte successful command receipt.
Its private snapshot and rehearsal are in `.local/native-n8n-receipts-20261001/`.
Import, replay, restart and packaged command inspection preserved its exact
bytes and original token. Native events, requests, source calls and backfill
calls stayed empty; integrity and foreign-key checks passed. No API requests or
production writes occurred. Three synthetic failure/review cases joined that
actual result in the real n8n evaluator: 166 expressions, 12 result branches and
three durable Wait checkpoints passed across the three staged workflows.

The full `make validate-fork` gate passed with 528 v3 tests in 91 files, native
contract checks, all then-current 191 producer tests, zero Go lint issues and
all Go tests (API 211.930 seconds, ingest 350.228 seconds, SQLite 329.835 seconds).
After the startup fix, all 192 producer tests passed on Python 3.12 and 3.14 and
inside the final installed n8n image. The real HTTP/download fixture was rerun
for that fix. Logs are `n8n-receipts-validation.log`,
`n8n-receipts-python312-final.log`, `n8n-receipts-python314-final.log`,
`n8n-receipts-image-tests-final.log`, `n8n-receipts-http-final.log`,
`n8n-receipts-actual-rehearsal.log`, `n8n-receipts-packaged-rehearsal.log` and
`n8n-receipts-workflow-runtime.log` under `/tmp/stash-native-transition`.
Parent commit `8454a0b7f` passed all three CI workflows.

The final isolated image is
`localhost/stash-n8n-native-rehearsal:receipts-final-20261001`, ID
`acbc5b7077d6bd11e5da74e9abf4ffcddc0af7d16f7ca965d49ef0bfc2377761`.
Its installed adapter fingerprint matches the workspace. Refreshed full-history
Reddit/Twitter profiles under the rehearsal's `current/` directory match that
runtime and retain the unregistered rehearsal root. Existing configuration and
media mounts were read-only; no worker was activated. The live n8n container and
`localhost/n8n:latest` remained on the original `2fcb84852f4a…` image.
This increment adds no Stash database migration.

The scan journal, extractor checkpoints, deferrals, per-scan/collection/policy
completion records, historical handoffs and other catalog families remain to
be migrated. A read-only audit found 907 queued scans, 28 extractor checkpoints,
166 deferrals, one per-scan completion, four collection completions, one policy
migration and no handoffs at inspection time. These are live changing counts,
not a cutover boundary. Source registration, recovery callers, profile/service
activation and subsequent transition phases remain outstanding.

## Native retention and inspection of frozen scan journals

Migration 1000026 adds `scan_journals` and `scan_journal_records` to the native
database. The application API retains one complete snapshot atomically, with
its UUID, original database/root identity, capture time, document digest, table
inventory and every original row. Replay returns the same receipt; reusing an
identity with different evidence conflicts. Indexed pagination exposes compact
summaries, while a separate record lookup returns the original evidence.
Startup reconciles retained counts and requires the schema guards/indexes.
Anonymised exports remove this private operational history.

The seven supported families are `scan_jobs`, `extractor_jobs`, `scan_deferrals`,
`backfill_scan_completion`, `collection_backfill_completion`,
`backfill_policy_migrations` and `legacy_handoffs`. Empty tables and older
extractor column variants remain identifiable. Embedded command/result/detail
JSON strings retain their values, including timestamp spelling and large IDs.
Exact old scope IDs connect extractor evidence to pending scans; manual or
unbound scopes remain reviewable. Deferrals retain their reasons and retry
requirements. Unknown tables/columns and unsupported command forms block import;
no command, PID or historical service handoff is executed or resumed.

`stash-import-scan-journal` reads one SQLite snapshot, inventories every table/view,
and prepares a bounded document without modifying the source. Apply requires an
explicit application endpoint, a fixed snapshot identity/time and the reviewed
input digest. Its acknowledgement must match the expected inventory, root,
source, snapshot and document hash. The two permanent account-backfill tables
are explicitly inventoried as external to this importer and retain their
separate account-history migration. Producer tokens cannot use these routes.

This completes an evidence-retention layer, not operational activation. Imported
records have `pending_binding`, `historical` or `review` dispositions. They create
no native runs, attempts, requests or completion decisions. The old archive-key
cursor hash differs from the native post/attachment hash; copying it into a
native progress record would not implement correct resume. Activation still
needs source/profile bindings, a reviewed cutoff, retry/ignored-work handling,
cursor conversion and a common quiesced cutover boundary. Historical collection
and hashed per-scan receipts do not become native source-window proof.

The full-copy rehearsal is
`.local/native-scan-journal-rehearsal-20261001/native-scan-journal-rehearsal.sqlite`.
Schema 1000025 → 1000026 took 0.066752 seconds on the 1,473,081,344-byte copy.
All 136 preexisting tables matched their source semantically, the two new tables
were empty before import, and foreign-key violations and size growth were zero.
Reconciliation took 48.715 seconds. The source schema-25 copy was preserved.

A fresh read-only backup of the actual journal supplied 1,063 retained records:
863 scans, 28 extractor checkpoints, 166 deferrals, one per-scan completion,
four collection completions and one policy migration, with no handoff rows.
Its separate account-history inventory reports 1,329 completions and zero skips.
Import/replay took 0.100069 seconds through the core repository service. Every
original evidence row and derived summary matched after replay and reopening.
Dispositions are 1,050 pending bindings, seven reviews and six historical rows.
Native source work remained empty. These are local rehearsal timings, not API
or UI latency guarantees, and this changing live journal is not a final cutover
boundary. `review.json`, `input.json`, the frozen SQLite input and both
reconciliation reports are retained privately in that rehearsal directory.

The real Go/Python HTTP fixture commits an import, drops its response, and then
verifies exact replay, paginated summaries, every evidence record, application
authority and unchanged source bytes. Repository tests cover older extractor
shapes, unknown inputs, conflicting identities, atomic rollback, immutable rows,
restart and detection of missing evidence. The shared fixture preserves all
seven families and never fabricates native work.

The full `make validate-fork` gate passed: 528 v3 tests in 91 files, native
application contract checks, all 195 producer tests, zero Go lint issues and
all Go tests (API 229.979 seconds, ingest 361.327 seconds, SQLite 339.588 seconds).
The producer suite also passed on the host's Python 3.12 runtime. Focused store
and real HTTP checks passed separately. Logs are `scan-journal-validation-final.log`,
`scan-journal-python314.log`, `scan-journal-python312.log`,
`scan-journal-focused.log`, `scan-journal-http.log`, `scan-journal-migration.log`,
`scan-journal-reconciliation.log` and `scan-journal-real-import.log` under
`/tmp/stash-native-transition`. Parent commit `220c20dc1` passed all three CI jobs.

Production, installed workers and workflows remain unchanged. Remaining work
includes activation of retained operational state, source registration, the
other catalog families and all later transition phases. This increment neither
finishes the migration nor changes the frozen compatible release.

## Native activation of retained scan requests

Schema 1000027 adds immutable activation plans and original-job bindings. The
application preview binds a retained scan to the exact native collection URL,
logical root revision, reviewed worker policy and an explicit cutoff. All scans
in that snapshot/context/URL group consolidate into one window when their old
command policies agree. Original date minima, UTC interpretation, retry delays
and failure limits survive. An existing deferral remains deferred; ordinary timer
requests cannot release it. Manual/unbound extractor scopes and historical
service handoffs remain review evidence.

`stash-activate-scan-journal` saves the normalized preview and applies its reviewed
plan digest. Activation creates one native queued/deferred run, without inventing
producer requests, access tokens, process ownership, attempts or completion proof.
Replay returns the same receipt after response loss or restart. Original scan
keys are unique within their source database across snapshots, preventing a later
snapshot from scheduling the same pending job again. A collision with an existing
active native run rolls back instead of replacing its progress or lease.

An explicitly selected checkpoint must match a selected scan's consolidated
scope. The first real claim seeds its old item count and qualified archive-key
cursor, with zero claimed completed files. The worker reproduces the original
gallery-dl key hash and JSON encoding, replays through the position, restores
the archive stop rule and then writes native cursors. Missing checkpoints remain
unfinished. Full-history/no-skip profiles without a stop rule preserve the old
behavior of ignoring that checkpoint. A wider window replays without archive
stopping, including on subsequent retries; omitting a checkpoint deliberately
selects that replay behavior as well.

The server advertises recovery protocol 1 and requires workers to acknowledge it
when claiming recovered work. Older workers cannot silently ignore that policy
and mark a partial traversal successful. Current workers require the capability,
and verify that recovery policy remains fixed during their lease. Lease/status
queries read only the bounded recovery fields through the run's unique index,
not the complete retained maintenance plan. Producer outbox schema stays at 7.

The isolated rehearsal database is
`.local/native-scan-activation-rehearsal-20261001/native-scan-activation-rehearsal.sqlite`.
Schema 1000026 → 1000027 took 0.079122 seconds on the 1,473,081,344-byte copy.
All 138 preexisting tables matched before activation, with zero foreign-key
violations or size growth; reconciliation took 55.085 seconds. Three selected
actual journal requests exercised a bound checkpoint, a deferral and full replay
against an empty isolated media root and an explicitly unusable placeholder
worker policy. Activation/replay took 0.056358 seconds, producing two queued runs
and one deferred run. Reopening preserved all three receipts; no worker, producer
request or attempt was created. The snapshot, its 1,063 rows and all 1,329 permanent
account-backfill decisions still matched their originals exactly afterward.
These are local rehearsal timings, not production latency guarantees.

The rehearsal inputs, migration and retained-evidence reconciliation reports,
activation results and helper source are retained in that private directory.
Final production source/profile bindings have not been created. Earlier staged
worker profiles/images need their adapter fingerprints refreshed before they can
be activated with the new recovery contract. The live database, host launchers,
n8n workflows and frozen compatible deployment remain unchanged. Broader catalog
migration, recovery callers and subsequent transition phases remain open.

Final `make validate-fork` passed: 528 v3 tests in 91 files, native application
contracts, 200 producer tests, zero Go lint issues and the complete Go suite.
The same producer suite passed separately on the host's Python 3.12 runtime.
Focused database and real Go/Python HTTP checks cover response loss, exact replay,
deferral/backoff preservation, old-worker rejection, incompatible scope/policy,
expanded-window replay, cross-snapshot duplicate protection and rollback of a
native-run collision. Gallery-dl lifecycle fixtures cover restoration of the
archive stop rule, missing cursors and full-history replay; hash fixtures preserve
legacy spacing, Unicode escaping and large IDs.

Validation logs are `scan-activation-validation-verified.log`,
`scan-activation-python312-verified.log`, `scan-activation-final-http.log` and
the focused database logs under `/tmp/stash-native-transition`; rehearsal logs
are `scan-activation-migration.log`, `scan-activation-reconciliation.log` and
`scan-activation-real-activation.log`. Parent `298ac862a` passed all three CI jobs.

## Reviewed performer-registry import

Schema 1000028 imports the five performer-registry families and both older
plugin-binding families through an application-only preview/apply API. The
`stash-import-catalog-identities` command reads one frozen SQLite snapshot,
inventories every supported table/column, and requires a reviewed server plan
before applying changes. Original JSON text and all row outcomes remain retained;
the account map is temporary migration input, not a restored plugin setting.

Saved catalog UUIDs are adopted by explicitly bound local performers while
selected metadata and local IDs remain unchanged. Historical catalog redirects
record already-completed merges without deleting or recreating local performers.
Missing/reused IDs, competing bindings, UUID collisions, and unbound identities
remain review records. Foreign-library IDs are never interpreted locally.
Explicitly mapped accounts import saved links/unlinks or reuse an equal native
decision; later native choices and contradictory saved alternatives require
review. Older plugin bindings covered by their migration receipt cannot resurrect
a later unlink. A late write error rolls back all domain and receipt changes,
even if a caller accidentally swallows the error.

Receipts preserve the reviewed plan after response loss, restart and subsequent
native edits. A second snapshot cannot replay the same source-registry/namespace
cutover. Each source row has a previewed outcome; retained evidence is available
through bounded record pages. Anonymised exports remove the import evidence.
Unresolved records need native review operations in a later increment. Bulk
account-identifier/source routing import and the other catalog families remain
separate required work; this is not the complete catalog migration.

The private rehearsal is in
`.local/native-performer-registry-rehearsal-20261001/`. It contains a read-only
backup of the actual registry, its complete table inventory, exact prepared/bound
inputs, preview, immutable receipt, original records, reconciliation reports and
helper source. The library copy migrated from 1000027 to 1000028 in 0.069857
seconds. All 140 existing tables matched before import, with zero foreign-key
violations and zero size growth; that comparison took 53.614 seconds.

The actual registry contains four performer identities, four local bindings,
five saved account choices, fourteen identity events, one migration receipt,
and two older plugin rows: thirty retained records. All four performer UUIDs
were adopted. Elizabeth Tran retained catalog UUID
`1d8d50be-1de6-4320-8725-b617f8ceb062`, local performer 721, and the existing
canonical name/aliases. This registry has no recorded UUID redirects or merge
events for the older imelizabethtran merge; the importer preserved its survivor
and historical names without inventing a deleted performer identity.

One native Twitter account was explicitly prepared from the registry's captured
ID/handle evidence for account 742448640, then linked to that surviving performer.
The other four saved account choices were retained for review pending native
account mapping. This includes the directory-derived Instagram label, which was
not silently converted into a service identity. Import and exact replay took
0.011440 seconds; reopening retained the same receipt. The outcomes were nine
mapped rows, fifteen copied historical rows, four review rows and two superseded
plugin rows.

The post-import comparison checked all 140 preexisting tables. Selected library
metadata and all 1,542,046 other archive entities matched exactly; only the four
reviewed UUID adoptions/redirects and the prepared account/ownership records
changed. All thirty source rows matched their frozen evidence, with no unexpected
differences or foreign-key violations. Reconciliation took 97.796 seconds. These
are local rehearsal measurements, not production latency guarantees.

The compatible deployment, live registry/library, workers and n8n workflows
remain unchanged. Parent `f5f70f5dc` passed all three CI jobs. `make validate-fork`
passed: 528 v3 tests in 91 files, native application contracts, 204 producer tests,
zero Go lint issues and the complete Go suite. The producer suite also passed on
the host's Python 3.12 runtime (6.165 seconds). After the final transaction guard,
the focused SQLite tests, real Go/Python HTTP replay test and Go lint passed again.

Coverage includes response loss, restart, native edits after successful import,
stale previews, changed historical local bindings, explicit unlinks, preserved
native choices, competing catalog identities, missing/cyclic redirects, conflicting
and coalesced account mappings, unchanged selected metadata, late failure rollback,
swallowed-error rollback, strict snapshot shapes, and anonymised evidence removal.
The main log is `catalog-identity-validation.log`; final focused evidence is in
`catalog-identity-atomic-final.log`, `catalog-identity-http-final.log`,
`catalog-identity-lint-final.log`, and `catalog-identity-python312.log`. Rehearsal
logs are `catalog-identity-migration.log`, `catalog-identity-reconciliation.log`,
`catalog-identity-real-import.log` and `catalog-identity-import-reconciliation.log`,
all under `/tmp/stash-native-transition`.

## Reviewed account and catalog registry import

Schema 1000029 imports the six remaining registry families through an
application-authorized preview/apply API and `stash-import-catalog-registry`.
The frozen input must complement the completed performer-registry import, with
matching source UUID, capture time and table inventory. Original rows, plan
outcomes, source-qualified account/catalog mappings and an immutable receipt
remain in the native database. Exact retry and reopening preserve the receipt;
stale plans and later native ownership choices cannot silently reapply history.

Captured qualified IDs create or resolve source accounts. Captured handle/ID
pairs share that account; reused aliases remain ambiguous. Different ID kinds
can resolve together only through an already established native account.
Conflicting native candidates are reported with their actual UUIDs. Mirror and
native service namespaces stay separate; mirror display names never become
native handles. Known legacy locators carry explicitly provisional evidence,
while unknown services and directory-derived account labels remain review items.
No name-only performer match or website lookup is performed.

Every surviving catalog becomes a disabled `legacy_catalog` collection; historical
redirects point to its survivor. Publisher accounts remain separate from depicted
performers. Routes, media keys, links and identifier checkpoints are retained
without activating jobs or inventing scrape URLs/root bindings. The actual registry
contained 1,078 empty identifier checkpoint timestamps; the old index writes
these when no account capture exists. They survive as historical checkpoints.
ID evidence without a handle also retains its empty alias key.

The private full-copy rehearsal is in
`.local/native-registry-rehearsal-20261001/`. It reuses the previous increment's
frozen registry and starts from a new copy of its schema-1000028 library.
Promotion to 1000029 took 0.074117 seconds. All 142 preexisting tables matched
before import, with no foreign-key violations or database-file growth; comparison
took 63.700 seconds. The prepared input, reviewed plan, receipts, retained records,
helper source and reconciliation reports are saved privately there.

The snapshot contains 1,697 catalogs, 4,032 routes, 914 identifier rows and 1,697
checkpoints, with empty link/profile-URL tables: 8,340 retained rows. Catalogs
include 1,237 creator catalogs, 456 other collections and four subreddits. Preview
took 0.121751 seconds. It created 1,181 source accounts and reused the previously
bound Twitter account: 662 captured identity groups and 520 provisional locator
groups. It mapped 1,358 old account keys and retained 138 unqualified keys for
review. All 1,697 catalog collections were created disabled; 1,099 have a resolved
publisher account. None received a scrape target or filesystem root.

Three previously unresolved saved ownership choices were imported: Elizabeth
Tran's Instagram account and the Reddit accounts for petitebbygirl and
Southern-Lobster-808. Elizabeth Tran's existing Twitter choice was preserved.
The directory-derived miaxmall account label remains unresolved rather than
being silently asserted as an Instagram identity. All performer UUIDs, local IDs,
selected names and aliases remained unchanged, including the existing Elizabeth
Tran merge survivor.

Import plus exact replay took 1.156696 seconds, and reopening returned the same
receipt and ownership. All 8,340 source rows matched their frozen evidence:
2,611 mapped records and 5,729 copied historical records. The post-import
comparison checked all 142 preexisting tables; all 1,542,054 archive entities,
selected library metadata, previous import evidence and preexisting collection
definitions remained unchanged. Only the planned account evidence/revisions,
new ownership choices and collection records changed. There were no unexpected
differences, foreign-key violations or database-file growth. Reconciliation took
51.728 seconds. These are isolated rehearsal measurements, not production timing
guarantees.

`make validate-fork` passed: 528 v3 tests in 91 files, native contracts, 207
producer tests, zero Go lint issues and the full Go suite. After the final account
matching/checkpoint changes, the focused SQLite suite passed in 6.598 seconds,
the real Go/Python HTTP checks passed, and Go lint reported zero issues. The final
snapshot-reader regression brought the Python 3.12 suite to 208 passing tests
(5.954 seconds); all eight registry/identity tests passed on Python 3.14, and the
real HTTP checks passed again in 3.388 seconds. Coverage includes exact response-
loss replay, stale previews, native choices after import, mirror separation,
reused handles, multiple identifier kinds, empty historical checkpoints, missing
aliases, strict inventory, rollback after swallowed errors and anonymisation.
Parent `29445f5b5` passed all three CI jobs.

Validation logs are `catalog-registry-validation.log`,
`catalog-registry-verified-focused.log`, `catalog-registry-verified-lint.log`,
`catalog-registry-python312-final.log`, `catalog-registry-python314-final.log`
and `catalog-registry-python-final-http.log`. Rehearsal logs are
`catalog-registry-migration.log`, `catalog-registry-reconciliation.log`,
`catalog-registry-real-preview.log`, `catalog-registry-real-import.log` and
`catalog-registry-import-reconciliation-final.log`, all under
`/tmp/stash-native-transition`.

This imports the registry, not the individual catalog bodies. Native review
resolution, validated source/root registration, media/post/profile history,
worker activation and the later transition phases remain required work. The live
library, catalog registry, host/n8n workers and frozen compatible deployment remain
unchanged.

## Bounded snapshots of individual catalog bodies

The producer package now includes `stash-prepare-catalog`, a versioned read-only
reader and durable snapshot command for recognized catalog schema versions 1–3.
It inventories every physical family and schema object, validates table/key
shapes, checks SQLite and logical references, and rejects unknown tables/columns
or views. Older physical sidecars and normalized document/source layouts remain
distinct supported inputs; the normalized view is not exported as duplicate data.

The snapshot preserves original SQLite values and JSON strings, with an explicit
binary representation for exact sidecar bytes. Ordered chunks contain at most
1,000 records and 16 MiB; each table and chunk has a row count and digest. The
manifest retains the original registry/catalog/snapshot identities, schema,
reference counts and reconstructed capture inventory. Profile bodies and shared
observations remain physical rows rather than being expanded into repeated stored
payloads. The reader verifies profile hashes and paths, per-capture patches and
sidecar content hashes. Shared observations with detail rows create no extra
synthetic capture. Profile caching has both entry and byte bounds.

Files and directories are flushed before publishing a private snapshot directory.
Existing destinations are not overwritten; interrupted or lost acknowledgements
can be resolved by verifying the saved manifest and chunks. Verification checks
names, digests, counts, ordered unique keys, binary checksums and table/reference
inventories. The command reports `imported:false`: native batch mapping, review
outcomes and import completion receipts remain required subsequent work.

The full rehearsal is in
`.local/native-catalog-source-rehearsal-20261001/`. All 1,697 catalogs listed in
the frozen registry were copied and prepared; there were no missing or additional
catalog files. Initial sandboxed backups could not create SQLite's transient WAL
shared-memory files for some sources. The isolated helper was stopped, its 227
completed copies were preserved, and the remaining 1,470 read-only backups were
completed with access for those temporary files. That backup pass took 97.718
seconds. No catalog data or native library was modified.

The final preparation/verification covered 4,764,236 physical rows, including
256,991 posts, 379,449 observations, 444,898 detail captures, 781 profile bodies,
272,556 sidecar documents and their references. Reconstruction yielded 526,348
original captures with 18,245 profile references; the largest payload was
2,928,257 bytes. All 22 present physical families were retained, including empty
edit, file-event and prune queues. Preparation plus verification took 201.312
seconds with peak resident memory of 84,004 KiB. Frozen databases occupy
2,940,776,448 bytes and prepared record chunks 2,816,108,769 bytes, excluding their
manifests. These individually consistent backups are rehearsal inputs, not a
coordinated production cutover boundary.

An independent comparison then read every original SQLite row and every exported
record, preserving SQLite value types, JSON strings and binary bytes. It also
compared all 526,348 reconstructed payloads, capture IDs, timestamps, metadata and
extractor versions with the existing catalog reader's `observations.expand` path.
All 1,697 catalogs, all 4,764,236 rows and all captures matched, with zero errors.
Original database file hashes matched before and after inspection. This pass took
136.975 seconds with peak resident memory of 134,852 KiB. Private manifests,
source hashes, helper code and both reconciliation reports are retained locally.

The producer validation gate passed all 216 tests on Python 3.14 (5.344 seconds)
and the complete suite also passed on Python 3.12 (6.003 seconds). Eight new tests
cover older layouts, shared/flat captures, nested profile references, exact binary
preservation, unknown/corrupt inputs, deterministic bounded output, destination
protection, tampering, publication response loss and the CLI. The rebuilt isolated
package's installed command prepared and verified the Elizabeth Tran catalog with
the exact same manifest digest as the source checkout. Parent `e172751f6` passed
lint, build and preview image publication in CI.

Logs under `/tmp/stash-native-transition` are `catalog-snapshot-backup.log`,
`catalog-snapshot-full-rehearsal-final.log`,
`catalog-snapshot-independent-reconciliation.log`,
`catalog-snapshot-producer-final.log`, `catalog-snapshot-python312-final.log`,
`catalog-snapshot-focused-final.log` and `catalog-snapshot-package-install.log`.
The native database remains at schema 1000029. Production, workers and n8n have
not switched, and the full transition remains in progress.


## 2026-10-01: Resumable receipt of individual catalog snapshots

Native schema 1000030 now receives the prepared individual catalog bodies through
application-authorized `CatalogSnapshot` services. Each manifest is bound to its
original registry import, source UUID, catalog ID and native collection mapping.
Unknown schema shapes, missing mappings, changed identities and out-of-order or
altered chunks fail. A source/catalog pair cannot silently acquire another frozen
snapshot under a new UUID.

Chunks contain at most 1,000 rows and 16 MiB. Original JSONL bytes, embedded JSON
strings, binary sidecar encodings and row keys survive in indexed temporary
migration records. Chunk receipts and resumable table hashes commit in the same
transaction as their records. Lost responses, restart and exact replay do not
create extra rows; a swallowed write failure still aborts the transaction. The
server checks table/chunk counts and hashes, supported physical columns/keys,
SQLite key ordering, embedded JSON and binary checksums. Retained schema SQL is
never executed. Historical sidecar-view triggers remain supported evidence.

`stash-upload-catalog` verifies the full local snapshot, submits its original
bytes to the explicit native endpoint, checks receipt identity/counts and resumes
from the next chunk. The installed command uses the existing application key;
producer tokens and website credentials are unrelated to this migration access.
The receiver reports `received` with `imported:false` and all record families
explicitly pending. Native graph/capture reconciliation, domain mapping, review
outcomes and retirement of temporary bodies remain subsequent work. Reception
creates no posts, media, ownership or metadata choices, and activates no jobs.

A fresh copy of the completed schema-1000029 registry rehearsal promoted in
0.121 seconds. All 1,697 frozen catalogs were then received as 4,764,236 records in
5,954 chunks, preserving 2,816,108,769 original JSONL bytes. The rehearsal includes
reopening/replaying an acknowledged chunk and checking all receipts after the
final reopen. Receipt staging and these checks took 251.832 seconds; the final
open/consistency check took 29.508 seconds while the full input staging remained.
Inputs are the individually consistent frozen preparation copies, not a live
production cutover boundary. Private input manifests and the complete receipt
set remain under `.local/native-catalog-upload-rehearsal-20261001`.


Independent reconciliation compared the exact staged manifest, every record's
bytes/key/hash, every chunk/table digest and all 146 pre-existing native tables.
All 1,697 catalogs and all 4,764,236 rows matched; existing entity identities,
selected metadata, ownership and prior receipts were unchanged. SQLite integrity
returned `ok` and foreign-key checks found zero violations. This pass took
277.876 seconds. The rehearsal database grew from 1,473,081,344 to 6,986,661,888
bytes while retaining the temporary input rows and their lookup indexes. No
expanded capture/profile copies or live catalog projections were added.

All required fork-gate components passed: 528 v3 tests in 91 files, native client
contracts, 220 producer tests on Python 3.14, backend lint with zero issues, and
all Go tests. The final backend run passed the API
package in 380.579 seconds and SQLite package in 471.006 seconds. The complete
220-test producer suite also passed on Python 3.12 in 6.145 seconds. Focused tests
cover actual Python-to-Go HTTP response loss, restart, exact replay, atomic
rollback, checksum/ordering/lineage rejection, original binary/JSON preservation,
receipt validation and anonymisation. The rebuilt isolated package exposes the
installed `stash-upload-catalog` command. Parent `1b04b94fd` passed lint, build
and preview image publication in CI.

Logs under `/tmp/stash-native-transition` are `catalog-upload-focused.log`,
`catalog-upload-python-focused.log`, `catalog-upload-validation.log`,
`catalog-upload-backend-final.log`, `catalog-upload-python312.log`,
`catalog-upload-promotion.log`, `catalog-upload-full-rehearsal.log`,
`catalog-upload-independent-reconciliation.log`,
`catalog-upload-package-install.log` and `catalog-upload-installed-command.log`.
Production, workers and n8n remain on their existing deployment. The full native
archive transition is still in progress; the next domain import must reconcile
posts/captures and then every remaining physical record family before any
snapshot can be reported as imported.


## 2026-10-01: Native post, profile and capture import

Native schema 1000031 now maps received catalog source evidence through core
services. Application-only checkpoint and outcome routes support
`stash-import-catalog-evidence`; producer tokens cannot run historical imports.
Each transaction handles at most 50 source rows and stops between records after
16 MiB of decoded input. Indexed dependency reads load only the relevant post,
URLs, observation and profiles. Native records, immutable row dispositions and
progress commit together, including protection against swallowed write errors.
The same frozen manifest resumes from the last committed ordinal after restart
or a lost response. Completed mapping receipts replay unchanged.

Qualified post IDs share native identities across physical catalogs. Coomer and
Kemono URLs retain mirror/service namespaces even when old catalog rows called
their platform onlyfans, fansly or patreon. Unknown local keys stay scoped to the
source catalog. Captured Reddit/Twitter identifiers can qualify a local post;
conflicting identities, reused reserved UUIDs and forgotten posts require review.
These mappings never infer performer ownership from names, folders or shared
content. Collection provenance uses the original imported definition even after
a later native edit or retirement.

The Go reader reproduces the legacy Python JSON checksum encoding, verifies
profile hashes and hydration paths, and preserves parent Reddit patch semantics.
Detail rows become original captures; their shared observation receives a shared
disposition without an invented extra timestamp capture. Flat observations each
produce one capture. Core storage deduplicates post revisions, payloads and
profiles, including retention of unreferenced profile bodies. Identical copied
events can share a capture across catalogs; reusing an old capture ID with
different bytes or provenance preserves a separate event.

The complete frozen corpus passed reader reconciliation: 526,348 captures from
1,697 catalogs, including 81,450 flat observations and 18,245 profile references,
matched the Python manifest checksums. Every payload round-tripped through the
native representation without loss. This check took 58.894 seconds. It used
read-only frozen inputs and made no library changes.

A new copy of the schema-1000030 upload rehearsal promoted in 10.372 seconds.
All 1,697 received catalogs then completed the evidence mapping pass: 1,082,119
original rows, 526,348 capture mappings and 781 profile mappings, with zero review
outcomes. The helper used 22,916 bounded transactions, reopened after its first
batch to check the committed checkpoint, and replayed every completed receipt
after the final reopen. Import and these checks took 1,224.670 seconds; the final
database open/consistency check took 28.580 seconds. These are individually
consistent frozen rehearsal inputs, not the coordinated live cutover boundary.
Private source inventories, checkpoints and receipts are retained in
`.local/native-catalog-evidence-rehearsal-20261001/`.

Independent reconciliation then read the original catalogs with the Python source
reader and reconstructed every stored native capture from its database payloads,
profile references and patches. All 526,348 capture payloads, projected metadata,
timestamps, provenance and original header hashes matched. All 781 profile bodies
and every original post-key mapping matched. The 297,999 shared observations
created no additional captures. Native storage contains 256,990 posts, 379,449
shared revisions, 526,348 captures, 781 profile bodies and 18,245 profile
references. One qualified post identity is shared between original catalog rows;
this corpus has no exact copied capture events to coalesce, which the HTTP
fixture tests separately.

All 142 pre-existing tables outside the intended source-evidence writes matched
the baseline, including original snapshot bytes, existing entity metadata and
associations. SQLite integrity returned `ok` with no foreign-key violations.
This comparison took 222.289 seconds. The rehearsal database is 8,975,618,048
bytes while the temporary snapshot input remains present. Original source
catalogs, the upload baseline and production were not modified.

Validation passed all required fork-gate components: backend generation, v3
generation/types/formatting, 528 v3 tests and 71 operation contract files, 223
producer tests on Python 3.14 and Python 3.12, zero final lint issues, and the
entire Go suite. Two initial lint findings were fixed before the successful
backend gate. API tests took 432.370 seconds and SQLite tests 543.334 seconds
while the separate full-copy import was running. Focused tests cover rollback,
reopening/replay, stale checkpoints, forgotten/conflicting posts and historical
collection scope. The real HTTP/Python test loses a committed bounded batch,
resumes it, retains an unused profile, and imports a second physical catalog
with both copied captures and changed data under a reused old capture ID. The
rebuilt isolated package exposes the installed import command. Parent
`b7f61ecb3` passed lint, build and preview publication in CI.

Logs under `/tmp/stash-native-transition` are
`catalog-evidence-reader-reconciliation.log`, `catalog-evidence-promotion.log`,
`catalog-evidence-full-import.log`,
`catalog-evidence-independent-reconciliation.log`,
`catalog-evidence-validation.log`, `catalog-evidence-backend-final.log`,
`catalog-evidence-python312.log`, `catalog-evidence-http-copies.log`,
`catalog-evidence-historical-scope.log` and `catalog-evidence-lint-final.log`.

The evidence pass reports `mapped` or `review` with `imported:false`. It covers
four source families; assets, files, appearances, memberships, sidecar documents,
translations, edits and other retained histories still require native mappings
and final reconciliation. It does not select metadata, associate library media,
construct galleries or activate jobs. Temporary snapshot bodies remain private
migration inputs until the complete importer can retire them. Production and
workers remain on the frozen compatible deployment; the full transition remains
in progress.

## 2026-10-01: Native post link evidence services

Native schema 1000032 adds the core relationship services needed by the next
catalog import pass. `SourcePostLinks` stores each exact post URL once, with
separate observations carrying their original evidence time and provenance. It
also retains evidence for qualified post identifiers and unselected publisher
claims. Identifier changes require the reviewed post revision and cannot take
over another post's identifier. These services do not fetch URLs or infer that
two posts with the same URL are one post.

Publisher claims retain their original account UUID through consolidation and
resolve its current canonical UUID on reads. They cannot choose a capture's
publisher, override its explicit unlink, or assign depicted performers. The
legacy writer inspection showed why this boundary is necessary: an old
`source-id` account can come from a directory label, and native-service-labelled
IDs can represent mirror accounts. The relationship importer must preserve those
claims with their original basis; actual captured publisher evidence and native
review decisions remain separate.

Read-only inventory of the frozen source confirms 1,173 original account rows:
1,095 already have a registry mapping and 78 remain unmapped. The existing
OnlyFans/Fansly source-ID mappings correctly resolve to Coomer namespaces and
Patreon mappings to Kemono; their old platform labels must not create native
service IDs during the next pass. The private grouped inventory is saved with
the post-link rehearsal inputs. A post-association preflight found 218,338 rows
eligible for retained unselected claims, 32,822 whose account is still unmapped,
and 5,831 without a legacy account. Existing mapped post/account namespaces had
no conflicts; eligibility is not a publisher selection or performer attribution.

Evidence writes are immutable and replayable, reject changed request contents,
and refuse new observations for forgotten posts. SQL guards preserve scope and
retirement rules. A managed transaction guard rolls back a late failure even
when a caller ignores its error. Read APIs use bounded indexed cursors. Startup
validates the new tables, indexes, triggers, timestamp column types and evidence
references. Anonymised exports remove these private records before their parent
posts and accounts.

Focused regression tests pass for URL deduplication with separate observations,
post identity conflicts, account consolidation, explicit unlink preservation,
restart/replay, immutable rows, invalid inputs, failed-write rollback, incomplete
startup data and anonymisation. An initial timestamp declaration mismatch was
found and fixed before validation; the final startup guard also verifies its SQL
type. Final focused tests took 22.725 seconds, and final lint reported zero issues.

The corrected schema promoted a new copy of the schema-1000031 full-corpus
rehearsal in 113.983 seconds while the broader validation suite was running.
Independent comparison found all 153 pre-existing data tables unchanged,
including the complete staged snapshots and native post/capture/profile graph.
The four new relationship tables remain empty until their import pass. SQLite
integrity returned `ok` and foreign-key validation found no violations. The
comparison took 398.389 seconds, and the resulting database is 8,975,687,680
bytes. Its private path is
`.local/native-post-links-rehearsal-20261001/verified.sqlite`; the previous
evidence rehearsal and original catalog snapshots were not modified.

All required gate components passed: backend generation, v3 generation/types and
format checks, 528 v3 tests, 71 application operation contracts, 223 producer
tests, final lint and the entire Go package set. The earlier broad run used an
ownership assertion corrected during focused validation; the final complete
SQLite rerun passed in 127.021 seconds. Other Go packages passed in that broad
run, including API tests in 376.154 seconds. Parent `22e96850b` passed lint, build
and preview image publication in CI.

Logs under `/tmp/stash-native-transition` are `post-links-focused-final.log`,
`post-links-lint-final.log`, `post-links-validation.log`,
`post-links-sqlite-final.log`, `post-links-promotion-final.log` and
`post-links-independent-reconciliation-final.log`. The superseded rehearsal
copy with the initial timestamp declaration was removed after the corrected
copy passed reconciliation.

This increment provides core services only. Original `post_urls`, `post_aliases`,
account/handle rows and legacy publisher associations still need their bounded
import passes and per-row reconciliation. No received snapshot becomes imported
through this schema upgrade. The full transition remains active, with production,
workers and n8n on the frozen compatible deployment.

## 2026-10-01: Native catalog relationship import

Schema 1000033 and `stash-import-catalog-relations` now map the original
`accounts`, `handles`, `posts`, `post_urls` and `post_aliases` families through
core services. The pass requires a received snapshot and completed source
evidence mapping. Application-authorized API batches bind the exact manifest
and ordinal, process at most 50 rows, and commit native records, immutable
source-row receipts and progress together. A late error rolls back the entire
batch even if its caller ignores the error. Lost responses resume from the
committed checkpoint, and completed passes replay unchanged.

Account rows use the snapshot's frozen registry mappings and preserve original
account UUIDs through consolidation. They retain legacy keys without promoting
folder-derived or mirror IDs into native-service identities. Handle evidence
keeps its original observation time; mirror handle fields become legacy labels
because they can contain display names. Post URLs share exact native URL rows
while retaining separate evidence. Aliases retain physical-catalog scope and
cannot take over another post's identifier. Old post/account associations become
unselected claims, without selecting publishers or assigning depicted performers.

Every receipt retains the complete original values, including old identity
basis and creation timestamps. Unmapped accounts, invalid values, conflicting
aliases and forgotten posts retain explicit review outcomes. Posts without an
old account are counted as unassigned. Bounded API summaries omit unusually
large keys with an explicit marker; the individual record endpoint retains the
full key and source values. Startup validates source correspondence, progress
continuity and native evidence scope. Anonymised exports remove these records.

Focused tests passed for all five families, interrupted batches, stale cursors,
reopening/replay, mirror scope, alias conflicts, failed-write rollback, forgotten
posts, oversized values, incomplete startup data and anonymisation. The real
HTTP/Python test loses a committed relationship batch, resumes it, and imports a
second physical catalog whose shared post URL retains separate evidence. It also
exercises invalid routes, bounded summaries and individual record inspection.

All required fork-gate components passed: backend generation, v3 generation,
types and format checks, 528 v3 tests, 71 application operation contracts, 226
producer tests on Python 3.14 and Python 3.12, final lint and the complete Go
suite. One initial lint finding was corrected before the successful backend
gate. API tests took 384.817 seconds and SQLite tests 502.426 seconds while the
private full-corpus rehearsal ran. The rebuilt isolated package exposes the
installed `stash-import-catalog-relations` command. Parent `781eb6e72` passed
lint, build and native preview image publication in CI.

A fresh copy of the verified schema-1000032 rehearsal was promoted in 16.210
seconds. Importing all 1,697 frozen snapshots processed 510,366 relationship rows
in 11,497 bounded transactions over 525.312 seconds, including restart and exact
terminal replay checks. Reopening the completed copy took 53.851 seconds while
the broader tests were running. The results are 471,557 mapped rows, 32,978 review
rows and 5,831 unassigned posts. These are relationship dispositions;
`imported:false` remains the whole-catalog status.

The review count consists of 78 original account records without a frozen
registry mapping, 78 dependent handle records and 32,822 dependent post/account
claims. All 250,944 URL rows and 71 aliases mapped successfully, along with 1,095
account references, 1,109 handle observations and 218,338 unselected claims.
Resolving the underlying account mappings and reviewing actual captured
publisher identities remain separate from these immutable migration receipts.

Independent Python reconciliation reread all original snapshot chunks, verified
their hashes, and compared every relationship key, checksum and complete source
value with its native receipt. It independently checked mapped account references,
handle normalization and timestamps, scoped alias hashes, URL identities,
publisher claims and provenance. All 148 unaffected tables matched the baseline
exactly. Existing post/account fields and identities were preserved, with exact
revision increments accounted for by the new evidence. All 504,810 prior post
identifiers, 1,831 account identifiers and 2,296 account-evidence rows survived;
the pass added 71 scoped post identifiers, 1,152 account references and 2,204
account-evidence rows. The 250,944 URL observations share 250,943 native URL rows.
Integrity returned `ok`, and foreign-key validation found no violations. Final
reconciliation took 182.286 seconds; the resulting database is 9,898,385,408 bytes.

Logs under `/tmp/stash-native-transition` are
`catalog-relations-focused-final.log`, `catalog-relations-promotion.log`,
`catalog-relations-full-import.log`,
`catalog-relations-independent-reconciliation-final.log`,
`catalog-relations-validate-fork.log`, `catalog-relations-backend-final.log`,
`catalog-relations-python312.log` and `catalog-relations-package-install.log`.
The final independent checker corrected an initial manifest-field lookup in its
standalone script; the native importer required no change from that check.

The original schema-1000032 baseline and frozen catalog snapshots were not
modified. The new private rehearsal is
`.local/native-catalog-relations-rehearsal-20261001/native-relations-rehearsal.sqlite`.
Captured publisher decisions, assets/files, appearances, memberships, sidecars,
translations, edits and other histories still require their remaining mappings
and final semantic reconciliation. No source jobs are activated, library media
associated, galleries constructed or existing metadata selections changed by
this pass. The full transition remains active; production, workers and n8n
remain on the frozen compatible deployment.

## 2026-10-01: Captured publishers from retained catalog evidence

Schema 1000034 and `stash-import-catalog-publishers` now select publishers from
actual captured account IDs after the evidence and relationship passes complete.
They reuse the native ingestion policy, preserving existing publisher choices
and explicit unlinks. Qualified IDs can resolve an existing account or create
one when the core policy allows it; ambiguous candidates and invalid identities
retain review context. Missing captured identities and forgotten posts remain
unavailable. Feed-owner profiles, folder names and historical catalog links never
supply a publisher, and source publisher selection does not assign depicted
performers or account owners.

The pass processes actual flat/detail captures; shared observation parents do
not create extra events. Immutable receipts reference existing source rows,
captures and decisions, retaining a small decision context without copying the
payload or profile. Original account UUIDs survive consolidation, while reads
also expose their current canonical account. Application-authorized batches bind
the exact manifest and ordinal, process at most 50 rows, and check a 16 MiB
retained-payload threshold between rows. Publisher decisions, identifier evidence,
receipts and progress commit together. Late failures roll back the entire batch,
including account creation, even if the caller ignores the error.

Focused tests cover account creation/reuse, absent and invalid identities,
unmapped source rows, ambiguous names/IDs, forgotten posts, explicit unlinks,
late-write rollback, prerequisites, stale bindings/cursors, restart/replay,
startup integrity and anonymisation. Existing performer and scene associations
remain unchanged. The real HTTP/Python test loses a committed publisher batch,
resumes it, and imports copied captures from another physical catalog: existing
decisions are preserved and only the genuinely new capture gets a new choice.
Bounded summary and individual context routes are covered.

All required fork-gate components passed: backend generation, v3 generation,
types and format checks, 528 v3 tests, 71 application operation contracts, 229
producer tests on Python 3.14 and Python 3.12, final lint and the complete Go
suite. One test-style lint finding was corrected before the successful backend
gate. API tests took 310.908 seconds and SQLite tests 445.270 seconds alongside
the private full-corpus rehearsal. Reinstalling the isolated package exposes
the `stash-import-catalog-publishers` command. Parent `db22eba99` passed lint,
build and native preview image publication in CI.

A fresh copy of the verified schema-1000033 rehearsal was promoted in 71.176
seconds. Importing all 1,697 frozen snapshots processed 526,348 capture rows in
11,820 bounded transactions over 517.000 seconds, including restart and exact
terminal replay checks. Reopening the completed copy took 67.937 seconds. The
results are 33,533 linked publishers and 492,815 unavailable captures lacking
captured account IDs, matching the read-only preflight. Every resolved publisher
already had an account; this corpus required no new accounts and produced no
publisher conflicts. Historical relationship review outcomes remain separate.
All receipts still report `imported:false` for the full catalog migration.

Independent Python reconciliation reconstructed every capture from the original
frozen catalog databases, checked its header checksum and independently derived
the qualified author identifiers. It verified all 33,533 decisions, current
heads and 60,625 captured identifier claims, including original observation
times, source origins and evidence paths. All 152 unaffected tables matched the
baseline exactly. Existing post/account fields remained unchanged apart from
the precisely accounted revision increments. All 2,983 account identifiers and
4,500 prior identifier-evidence rows survived; the pass added 60,625 evidence
rows without adding identifiers. Integrity returned `ok`, with no foreign-key
violations. Reconciliation passed in 256.629 seconds; the resulting database is
10,262,614,016 bytes.

Logs under `/tmp/stash-native-transition` use the `catalog-publishers-` prefix:
`focused-final.log`, `http.log`, `promotion.log`, `full-import.log`,
`independent-reconciliation.log`, `validate-fork.log`, `backend-final.log`,
`python312.log` and `package-install.log`. The private rehearsal and machine-readable
receipts are retained in `.local/native-catalog-publishers-rehearsal-20261001/`.
The original schema-1000033 baseline and frozen catalog snapshots were unchanged.

Assets/files, appearances, memberships, sidecars, translations, edits and other
histories still need their remaining mappings and final semantic reconciliation.
This pass activates no source jobs and constructs no galleries. The full
transition remains active; production, workers and n8n remain on the frozen
compatible deployment.

## 2026-10-01: Catalog attachment evidence and protected source selections

Schema 1000035 and `stash-import-catalog-attachments` now recover explicit source
attachment lists from received captures. The pass requires completed evidence
mapping and validates the captured post against its native identity. It shares
identical manifests, retains each capture reference and combines compatible
partial lists. Source positions, missing slots, declared albums and known counts
remain separate from download availability. Direct Reddit media establishes a
single attachment without declaring an album; Reddit gallery and retained
Twitter extended-entities lists preserve their source order and media-kind hints.

Automatic migration selection now uses the same combination and protection rules
as ingestion while retaining `origin:migration`. Pinned and disabled choices
remain intact. Contradictory lists retain review context and their source
manifests without replacing the current selection. Unsupported or absent lists
remain unavailable; filename/download counters and folders cannot invent source
order. A mismatching existing capture manifest remains a review outcome.

Application-authorized batches bind exact manifests and ordinals, process at
most 50 rows and check the retained-payload threshold between records. Source
lists, selections, immutable receipts and progress commit atomically. Late
failures roll back all changes even if the caller ignores the error. Summaries
are bounded, full source keys are retrieved individually, and conflict samples
are limited to 128 with an explicit truncation marker. Complete manifests remain
intact. Startup validates progress and reference scope; anonymised exports remove
these receipts. Completed passes remain `imported:false` for the full migration.

The read-only preflight examined 526,348 captures in 196.424 seconds. It found
5,250 captures with supported lists across 1,091 posts, including 559 evidenced
albums; all lists for a given post agreed. The other 521,098 captures lacked
supported attachment-list evidence. Older Twitter captures in this frozen corpus
retain individual-file metadata but no original extended-entities lists. The
native producer already preserves those lists for new captures; original legacy
appearances and file associations still require their separate mappings.

Focused SQLite tests passed for shared lists, mixed image/video hints, Twitter
lists, complementary partial captures, ordinary single-media posts, protected
selections, contradictory/invalid evidence, absent evidence, forgotten posts,
unmapped captures, rollback, stale cursors, prerequisites, startup integrity,
replay and anonymisation. Existing media, metadata and performer associations
remain unchanged. The real HTTP/Python test passed lost-response recovery and
cross-catalog reuse: 55 captures share one manifest and selection, with no extra
selection on copied evidence. The installed isolated package exposes the new
command. Parent `5151e78ae` passed lint, build and native preview publication in CI.

The full fork gate passed: backend generation, v3 generation/types/formatting,
528 v3 tests, 71 current application operation contracts, 232 producer tests,
lint and all Go tests. The same 232 producer tests passed on Python 3.12. API
tests took 288.057 seconds and SQLite tests 430.765 seconds during the rehearsal.
Final conflict context uses consistent JSON field names; the focused regression
suite passed again in 30.214 seconds after that adjustment, including a 200-slot
conflict reduced to 128 marked samples. Final lint reported zero issues.

A fresh copy of the verified schema-1000034 publisher rehearsal was promoted
in 141.375 seconds. The full import processed all 1,697 snapshots and 526,348
captures in 11,820 bounded transactions over 508.812 seconds. Restart and exact
terminal replay checks passed; reopening the completed copy took 112.265 seconds.
Results matched preflight: 5,250 mapped captures, 521,098 unavailable captures,
1,091 changed post selections, and no conflicts or protected choices in this
corpus. These are source-list outcomes, not whole-catalog or gallery completion.

A read-only inventory for the next media phase is retained in
`.local/native-catalog-media-assessment-20261001/inventory.json`. Of 776,976
legacy assets, only 890 have a declared digest algorithm (`fclones-blake3`);
the others cannot be treated as verified content hashes. The 778,523 file rows
include present, missing, pending, converted and deduplicated states. Of 422,442
appearances, 1,533 retain both a source media ID and position, 11,471 retain a
position without a media ID, and 409,438 retain neither. Their original path and
association evidence still require explicit mapping or review. That work must
preserve uncertain order, surviving paths and file states rather than fabricate
complete source manifests or available media.

Independent Python reconciliation reconstructed every original capture, verified
its header checksum, and derived the Reddit lists and direct-media references
without calling the Go extractor. It checked each source position, media-kind
hint, known count, completeness flag and evidence path, plus the shared manifest
and selection signatures. The 5,250 capture associations share 1,091 manifests
and selections containing 2,986 attachment entries; 559 posts carry album
evidence. All 153 unaffected tables matched the baseline exactly, including
existing library media, performer links, gallery memberships, publisher choices
and retained source payloads. Every post revision increment was accounted for.
Integrity returned `ok`, with no foreign-key violations. Reconciliation passed
in 248.450 seconds; the resulting database is 10,486,972,416 bytes.

Private copies, helpers and machine-readable receipts are retained under
`.local/native-catalog-attachments-rehearsal-20261001/`. Logs under
`/tmp/stash-native-transition` use the `catalog-attachments-` prefix:
`preflight.log`, `focused.log`, `focused-final.log`, `http.log`,
`python-focused.log`, `promotion.log`, `full-import.log`,
`independent-reconciliation.log`, `validate-fork.log`, `python312.log`,
`lint-final.log` and `package-install.log`. The original schema-1000034 copy and
frozen catalog sources remain unchanged.

Assets/files, legacy appearances, memberships, sidecars, translations, edits and
remaining histories still need native mapping and final semantic reconciliation.
Verified media associations and gallery construction follow that work. No source
jobs were activated and production, workers and n8n remain on the frozen
compatible deployment. The full transition remains active.

## Post media associations with incomplete legacy provenance

Schema 1000036 extends the existing `source_media_evidence` domain rather than
adding a parallel catalog association table. An association always identifies
its source post and library scene/image. Legacy/review evidence can omit an
attachment or capture when the original catalog does not establish that
relationship. Supplying both still requires the attachment's actual presence
in that capture's manifest. Observed/verified file evidence retains its full
attachment, capture and current file-association requirements.

This is needed for the 409,438 frozen appearances with neither a media ID nor
position, and for other appearances whose download counters cannot establish
source order. The repository retains uncertain provenance without manufacturing
captures, attachment IDs, positions, galleries or playable library records.
Evidence alone cannot select media or change performer attribution. A bounded
post lookup includes both general and attachment-specific associations.

Foreign keys enforce post scope even for direct SQL writes. Nullable scope
fields remain immutable; added provenance is separate evidence. A single insert
advances post/attachment review revisions atomically, while exact replay leaves
them unchanged. Forgotten posts reject new evidence but retain historical
replay. Archive UUID adoption and merge/deletion history remain supported.

Focused SQLite tests passed in 35.192 seconds and ingestion tests in 57.277
seconds. They cover unknown scope, multiple posts using one image, targeted
pagination, invalid references, replay, retirement, nullable-field mutation,
atomic rollback after a revision-trigger failure, preserved UUID adoption,
source-choice protection, and real schema-1000035 migration with populated
attachment evidence. Startup rejects corrupt scope before modifying the file.

The isolated 10,486,972,416-byte attachment rehearsal copy promoted to schema
1000036 in 252.940 seconds. Migration retains existing evidence IDs, capture
membership, details, timestamps and review revisions. Independent reconciliation
passed in 399.685 seconds: all 162 unaffected tables match exactly, the earlier
migration ledger is retained, integrity is `ok`, and there are no foreign-key
violations. The result is 10,486,980,608 bytes. This corpus still has zero native
media-evidence rows; the populated schema-1000035 fixture separately verifies
conversion of existing attachment evidence. Normal application startup reopened
the migrated copy successfully in 110.318 seconds.

The required validation set passed. `make validate-fork` completed generation,
v3 types/format/locales, 528 v3 tests, 71 native operation contracts and 232 Python
producer tests. Lint identified the unused revision helper replaced by the SQL
trigger; after its removal, `make lint it` passed with zero lint issues and the
complete Go suite. The API package took 350.572 seconds, ingestion 477.271
seconds, and SQLite 484.752 seconds.

Private copies, the independent row-digest verifier, startup helper and
machine-readable reconciliation are under
`.local/native-post-media-rehearsal-20261001/`. Logs under
`/tmp/stash-native-transition` use the `post-media-` prefix, including
`focused-final.log`, `promotion.log`, `independent-reconciliation.log`,
`validate-fork.log`, `backend-final.log` and `reopen.log`.

A separate read-only path assessment used the historical `/media/porn/` mount
against the frozen Stash copy; it did not register or activate a root. Its
769,643 file rows each have one scene/image owner and no ZIP membership. Of
778,523 catalog file rows, 769,655 present rows have an exact recorded path and
size match, while 6,288 present rows have no matching Stash path. Catalog rows
can repeat a file across catalogs. There are also 1,058 deduplicated rows with
matching survivor paths/sizes, and 179 converted/source-reference rows with a
survivor but no retained original size. Other missing/pending/survivor cases
remain unresolved. This comparison verifies database claims only, not current
filesystem bytes, availability or hash identity. The assessment passed in
6.919 seconds and is retained in
`.local/native-catalog-media-assessment-20261001/path-preflight.json`.

Asset/file binding and appearance import remain the next work, with explicit
root/path review, ambiguity handling, original state preservation and no
replay of historical filesystem actions. No catalog appearances have yet been
associated to native library media by this change. Production and the original
schema-1000035 rehearsal copy remain untouched.

## Shared source claims and unavailable file evidence

Schema 1000037 adds the `SourceFile` domain for shared source content claims,
file observations, guarded matches to existing library files, and post-file
evidence. Original asset references and declared digests remain source claims;
they do not become verified hashes or playable files. Observations preserve
present, missing, pending and deduplicated states, converted-source roles,
original size/modification times and surviving paths. A collection's locations
can share one claim across later collection definition revisions.

Matches retain the current file identity and generation when recorded. ZIP
matches also require the real archive identity/generation and member relation.
Exact paths are literal and reject conflicting reported sizes or modification
times; converted survivors use the shared asset's size. Declared SHA-256 can
match only existing server-verified content for that generation. Historical
replay survives file changes, deletion and UUID adoption. New matches must
validate the current state. Post-file evidence retains unavailable appearances
without inventing attachments, capture membership, source order or galleries.

The focused SQLite suite passed in 8.971 seconds, including shared claims,
offline roots, cross-collection rejection, replay, generation conflicts, ZIP
identity, declared-versus-verified hashes, forgotten posts, startup corruption
and anonymisation of populated private evidence. The final backend suite also
covers the added modification-time conflict case. `make validate-fork` passed
generation, 528 v3 tests, 71 native contracts and 232 producer tests. Lint found
a test helper available only under integration tags; replacing that dependency
allowed `make lint it` to pass with zero lint issues and the complete Go suite
(API 310.169 seconds, ingestion 437.782 seconds, SQLite 449.904 seconds).

The isolated schema-1000036 copy promoted in 232.491 seconds and reopened through
normal application startup in 112.446 seconds. Independent reconciliation passed
in 395.835 seconds: all 163 existing data tables match exactly, the earlier
migration ledger is preserved, the four new tables start empty, integrity is
`ok`, and foreign-key violations are zero. The resulting copy is 10,487,074,816
bytes. Private helpers, the copy and reconciliation report are under
`.local/native-source-files-rehearsal-20261001/`; logs under
`/tmp/stash-native-transition` use the `source-files-` prefix.

This commit supplies the native evidence model, not the asset/file/appearance
import pass. That importer follows with explicit root/mount bindings and
per-record receipts; memberships, gallery construction and the remaining
catalog families still need conversion. No production deployment, live data
migration, source activation or develop merge was performed.

## Catalog assets, file locations and post appearances

Schema 1000038 adds a resumable catalog media import with immutable root/mount
bindings and one receipt per original asset, file and appearance. The API and
`stash-import-catalog-media` client require the frozen manifest digest, logical
root/revision, collection revision and historical library mount. The root can
remain disabled and unbound; this mapping grants no live filesystem access.
Each transaction processes at most 50 records, through assets, files and
appearances in dependency order. The advance cursor is the processed-record
count, while receipt inspection uses original source ordinals. Restart and lost
responses resume committed progress without changing the binding.

Asset rows become shared source claims, preserving their declared digest, size
and original time. File observations retain the original state, role, path,
nanosecond modification time, first-observed value and survivor. Exact path
matches are literal and require agreeing recorded metadata; survivor matching
uses the asset size. Existing verified hashes can establish content matches,
but a catalog's declaration never becomes fresh byte verification. ZIP matches
require actual archive/member identities and both generations. Appearances
retain post-file evidence even when playable library media is unavailable.
An association requires a unique current scene/image owner and rechecks the
matched file generation. Legacy download positions remain evidence rather than
invented source attachment identities or album order.

The timestamp preflight found 73,920 differences caused entirely by subsecond
precision: Stash's existing file timestamps were stored at whole-second
precision. There were no whole-second conflicts. Matching now compares the
precision actually retained by Stash while preserving every original source
nanosecond value. Regression coverage separately rejects real time conflicts.

The isolated schema-1000037 copy promoted in 187.705 seconds. All 1,697 frozen
catalogs then completed the pass in 2,498.429 seconds, including normal startup
reopening in 200.936 seconds and exact completed-receipt replay. The pass used
40,522 bounded transactions and accounted for all 1,977,941 input rows:

| Input | Mapped | Unavailable | Review |
| --- | ---: | ---: | ---: |
| Assets | 776,976 | 0 | 0 |
| File locations | 770,843 | 7,631 | 49 |
| Post appearances | 415,028 | 7,393 | 21 |

Native results are 776,976 shared claims, 778,474 file observations, 770,843
guarded file matches, 422,421 post-file evidence rows and 415,028 post-to-media
associations. Unavailable file rows include 211 pending downloads. These are
historical database matches, not confirmation of current filesystem bytes.
The original source rows remain retained, including invalid paths and affected
appearance references requiring review. Forty-nine file paths contain literal
backslashes rejected by the native relative-path contract; 18 appearances refer
to those files. Three other appearances still name older path-based assets
where the same catalog file now names a deduplicated BLAKE3 asset. Those claims
remain distinct pending reconciliation with the historical dedupe evidence.
This pass changes no media metadata,
performer attribution, selected attachments, existing galleries or media files.

Independent reconciliation against the original frozen SQLite catalogs passed
in 363.477 seconds. It compares every original asset/file/appearance value with
its staged record and native outcome, verifies deterministic identities and
exact path/size/time/generation matches against the pre-import library, and
checks every post/media association against the original unique file owner.
All 159 unaffected tables and the prior migration ledger match exactly; every
post revision increment is accounted for. Integrity is `ok` and foreign-key
violations are zero. The resulting copy is 14,250,086,400 bytes.
Normal startup with the final, strengthened receipt-scope checks also passed
in 156.786 seconds (`catalog-media-current-reopen.log`).

Focused SQLite and real HTTP tests passed, including shared claims, converted
survivors, unavailable media, bounded phase checkpoints, lost responses, replay,
conflicts, ZIP members, stale generations between phases, rollback after an
injected receipt failure, startup rejection and anonymisation. Generation, v3
validation, 528 v3 tests, 71 native contracts and 235 producer tests passed.
The full producer suite also passed under Python 3.12 (7.072 seconds), and the
installed package exposes the new CLI. Go lint reported zero issues. The full
API suite passed in 460.519 seconds; ingestion and SQLite initially reached the
default ten-minute package deadline while the large import was running. Both
passed when rerun with a 25-minute package deadline (564.794 and 589.070 seconds),
with the rest of the full Go suite already passing.

Private copies, import/startup helpers, per-catalog receipts and independent
reconciliation are under `.local/native-catalog-media-rehearsal-20261001/`.
Timestamp assessments are under
`.local/native-catalog-media-assessment-20261001/`. Logs under
`/tmp/stash-native-transition` use the `catalog-media-` prefix, including
`promotion.log`, `full-import.log`, `independent-reconciliation.log`,
`focused-http.log`, `validate-fork.log`, `backend-final.log`,
`backend-retry.log` and `python312.log`.

Memberships, source gallery construction and remaining catalog families still
need conversion, so the snapshot's `imported` result remains false. Production,
source activation and develop remain unchanged.

## Native post collection memberships

Schema 1000039 adds immutable direct post-membership evidence, separate from
capture membership, media intake and performer attribution. The native model
and paged application API retain the post, historical collection revision,
observation time and provenance. Exact replay survives collection renaming or
retirement and post forgetting; forgotten posts reject new evidence. New
memberships advance the post's review revision without changing its metadata.

`stash-import-catalog-memberships` maps the original membership rows after the
snapshot's evidence pass, independently of file availability. Registry-qualified
collection keys identify shared groups across downloaded catalogs. Legacy
`creator` values came from punctuation in folder names and map to directory
groups without owner attribution. Subreddit groups retain their kind. New
groups are disabled with no inferred account, target URL or root. Conflicting
labels and unsupported identities retain the complete original row for review.
Later native edits do not overwrite historical group definitions or receipts.

Each transaction processes at most 50 records, with a between-row byte bound,
and commits native groups, memberships and receipts together. The client resumes
from the last committed original ordinal after lost responses. HTTP inspection
includes bounded receipt lists, full original values, collection-to-post evidence
and post-to-collection evidence. This supplements capture membership; these
groups do not by themselves create source albums or start scrapes.

Focused SQLite and real HTTP tests passed in 11.565 and 5.459 seconds. Coverage
includes bounded restart, shared groups across two independent catalog snapshots,
lost responses, immutable replay, conflicting definitions, original-value
inspection, later collection retirement, forgotten posts, atomic rollback after
an injected receipt failure, corrupt-scope startup rejection and anonymisation.
The full validation set passed generation, v3 formatting/types/locales, 528 UI
tests, 71 native contracts, 238 producer tests and Go lint with zero issues.
All 238 producer tests also passed under Python 3.12 in 6.741 seconds; package
installation and the new CLI entry point were checked.

The full Go run passed ingestion in 446.721 seconds and SQLite in 477.369 seconds.
Its API package initially failed three downloader-worker cases because the direct
Go command selected system Python, which lacked gallery-dl. Those cases passed
in 26.068 seconds with `PRODUCER_PYTHON` pointing at the prepared environment;
the remaining Go tests passed in the original run. Tests used a 25-minute package
deadline while the independent full-copy migration was using disk I/O.

The isolated 14,250,086,400-byte schema-1000038 copy promoted in 547.148 seconds.
All 1,697 catalog snapshots completed membership import in 413.086 seconds,
including 154.108 seconds to reopen through normal startup and exact terminal
receipt replay. The 6,456 transactions mapped all 257,001 original memberships
into 912 shared native groups, with zero review conflicts. The original
schema-1000038 copy and production remain unchanged.

Independent reconciliation passed in 349.851 seconds against all original
frozen SQLite catalog membership rows. It verifies the original values and
source digests, deterministic group and evidence identities, post mappings,
historical collection definitions, timestamps, provenance and terminal receipts.
All 166 unaffected tables and the prior migration ledger match exactly. Every
existing collection definition is unchanged; the only additions are the 912
disabled groups. All 257,001 post revision increments are accounted for.
Integrity is `ok` and foreign-key violations are zero. The resulting copy is
14,483,116,032 bytes.

Private helpers, copies, per-catalog receipts and the independent reconciliation
report are under `.local/native-catalog-membership-rehearsal-20261001/`.
Logs under `/tmp/stash-native-transition` use the `catalog-membership-` prefix,
including `promotion.log`, `full-import.log`, `independent-reconciliation.log`,
`focused-http-final.log`, `validation.log`, `backend.log`, `worker-retry.log`,
`python312-full.log` and `package-install.log`.

Source gallery construction, remaining catalog families, native UI and the
other transition phases remain open. No live migration, source activation,
production deployment or develop merge was performed.

## Imported media matching for source albums

The native gallery service now previews and applies attachment-to-media
associations from an individual post's imported file evidence. Callers choose
qualified retained media IDs or explicitly enable the historical Reddit
`post-id_media-id` filename convention. Matching uses original observations,
including those whose files were deduplicated or converted, and validates the
current library file generations and unique ownership. It never takes album
identity or source order from a filename, folder or download counter.

Duplicate catalog proofs share one media candidate. Existing attachment choices,
including explicit unlinks and undecided reviews, are preserved. Ambiguous
candidates, changed files/ownership and media-kind conflicts remain unresolved.
The complete bounded candidate set participates in preview validation. Applying
the signature records legacy attachment evidence without invented captures,
selects supported media, and uses the existing gallery sync service in one
managed transaction. Manual membership/exclusions, cover and metadata choices,
source gaps, repeated attachments and deletion suppression remain intact.
Pre-commit validation and an atomic-completion guard prevent partial publication.

Focused gallery, file and media SQLite tests passed in 17.798 seconds. The new
cases cover original-versus-survivor paths, explicit Reddit/Twitter IDs, service
qualification, malformed/conflicting IDs, filename boundaries, duplicate proofs,
missing slots, existing decisions, ambiguous ownership, stale previews, forgotten
posts, disabled selections, manual exclusions, deletion, candidate limits,
restart replay and injected/late-write rollback. Generation, all v3 checks
(528 tests in 91 files plus 71 native contracts), all 238 producer tests and Go
lint passed. The complete Go integration suite passed with the prepared producer
runtime: API 325.452 seconds, ingestion 448.220 seconds and SQLite 477.203 seconds.

An isolated copy of the fully imported schema-1000039 library was previewed and
applied with the explicit historical Reddit policy. Of 1,091 selected posts,
559 were evidenced albums and 532 were ineligible single-media posts. The core
created 559 logical galleries, selected 1,253 attachments and added 1,253 gallery
memberships, with zero ambiguous candidates and zero removals. Another 1,201
album attachments remain unselected because the retained evidence did not
establish a usable match.
All posts previewed in 5.198 seconds; application took 13.236 seconds. The whole
rehearsal took 589.541 seconds, including normal startup under concurrent test
load (391.272 seconds), reopening (176.324 seconds), and verifying that replay
reuses every gallery without another membership or media decision.

Independent reconciliation passed in 275.015 seconds. It rederives the matches
from the original observations and post/attachment IDs, checks every new proof
and decision, and verifies ordered source slots, gallery metadata and actual
memberships. The 1,504 new legacy proofs retain their original evidence links
without invented capture membership. All 159 unaffected tables match exactly;
existing records in the changed tables also remain intact. All 3,316 post and
2,757 attachment revision increments are accounted for. Integrity is `ok` and
foreign-key violations are zero. The resulting copy is 14,488,055,808 bytes.

The memberships comprise 1,240 images and 13 scenes. Of the 559 galleries, 266
have matches for every retained attachment, 42 have some matches, and 251 have
none yet. Empty source albums retain their known manifests and metadata without
inventing playable entries. This is distinct from proving that every source
attachment has downloaded or that the source list itself is complete.

Private copies, previews, results, helpers and independent reports are under
`.local/native-source-album-rehearsal-20261002/`. Logs under
`/tmp/stash-native-transition` use the `album-backfill-` prefix: `focused.log`,
`validation.log`, `backend.log`, `rehearsal.log` and
`reconciliation-final.log`.

The public Apply workflow, durable hook checkpoint/worker, native UI and remaining
transition phases still need implementation. This increment adds no schema
migration and does not expose an HTTP mutation. The original imported copy,
production deployment, source activation and develop remain unchanged.

## Durable historical album Apply and restartable notifications

Native schema 1000040 adds `album.backfill` to the existing durable job service
and an indexed per-resource history lookup. The table rebuild preserves job
identities, submissions, attempts, receipt references and progress/result
checkpoints. File ingestion receipts still require the `media.verify` kind.

The application now exposes read-only album previews and durable Apply,
submission/job status, bounded post/attempt history, cancellation and explicit
retry under `/api/v3/archive`. Requests pin the post UUID, matching policy and
preview signature. Exact lost-response replay returns its original job, even
when the successful application has since changed the preview. Responses use
snake_case names and calendar dates, with one public signature and initial
metadata only for gallery creation.

A metadata-only worker runs independently of FFmpeg and producer configuration.
It rechecks the preview and commits media choices, gallery changes and a compact
publication checkpoint in one transaction. Hooks run afterwards under a renewed
lease. Status distinguishes a committed publication from finished notifications.
Restart and transient failures resume the checkpoint; explicit retry retains
terminal history and the original publication event identity. Cancelling a
queued notification retry also preserves that publication. Later library edits
are never reapplied by a notification retry, and unpublished stale work requires
a fresh preview. Plugin delivery remains at least once, with stable event IDs.
Deleted gallery UUIDs do not resolve to a replacement that reused a numeric ID.

Real SQLite tests cover admission/coalescing, lost acknowledgements, queue
backpressure, targeted history, stale evidence, checkpoint rollback, retry
exhaustion, cancellation before/after publication, lease renewal and lost
ownership, restart recovery, and terminal retry chains. HTTP tests exercise the
actual handlers and worker lifecycle; a JavaScript plugin verifies event IDs
and gallery create/update fields. The populated schema-39 migration fixture
preserves queued, running and terminal file jobs, their attempts/submissions and
ingestion receipt, then verifies the retained guards and indexed album history.
A missing history index is rejected before startup changes database bytes.

The final focused album tests passed (SQLite 38.901 seconds, HTTP 10.489 seconds,
manager hooks 10.345 seconds). Backend generation, all v3 checks (528 tests in
91 files and 71 native contracts), all 238 producer tests, and Go lint passed.
The complete Go integration suite also passed: API 330.924 seconds, ingestion
451.294 seconds and SQLite 490.939 seconds. Final affected tests include the
explicit 404 for submitting work against a missing post.

The complete selected-post rehearsal used a fresh copy of the schema-1000039
membership-import baseline, upgraded through the normal migration path. All
1,091 requests completed: 559 source galleries, 1,253 selected attachments and
memberships, no removals or ambiguous candidates, and 1,201 unavailable album
attachments retained as gaps. The 532 single-media posts completed as ineligible
no-ops. Each original request remains replayable without a second job.

The rehearsal intentionally interrupted notification delivery after publication,
closed/reopened the database and recovered the expired lease. The same original
publication and event identity survived, with one expired attempt followed by
success. There are 1,091 jobs/submissions and 1,092 attempts, with no duplicate
gallery or media decision from recovery. Per-post history also matches every
original submission.

Normal opening/promotion under concurrent test load took 584.474 seconds and
reopening took 149.610 seconds. Previewing took 3.397 seconds, admission 5.573
seconds and post-restart processing 22.490 seconds; the full rehearsal including
replay checks took 766.832 seconds. Startup validation is measured separately
from the targeted preview and durable Apply operations.

Independent reconciliation passed in 364.935 seconds. All 154 unaffected tables
match exactly; every original record in changed domain tables is preserved.
The verifier rederives attachment matches from source evidence and checks the
1,504 new proofs, 1,253 media choices/memberships (1,240 images and 13 scenes),
3,316 post revision increments and 2,757 attachment revision increments. It also
verifies every job's resource/work key, original submission, publication, outcome
and attempt history, including the recovered event. Integrity is `ok`, with zero
foreign-key violations. The final copy is 14,489,649,152 bytes.

Private copies, persisted requests, previews, statuses, interruption evidence,
helpers and independent reports are under
`.local/native-album-jobs-rehearsal-20261002/`. Logs under
`/tmp/stash-native-transition` use the `album-jobs-` prefix: `final-focused.log`,
`validation.log`, `lint.log`, `backend.log`, `rehearsal.log` and
`reconciliation.log`.

The native album review UI and migration command remain subsequent work, along
with remaining catalog families, native UI/caller conversion, activation,
backup/restore drills and final cutover reconciliation. No production database,
source job, deployment or develop merge was changed by this increment.

## Supported source album migration command

`stash-backfill-source-albums` now prepares and applies historical album work
through the native application API. A private saved plan binds each post to its
matching policy, complete preview, signature and durable request UUID. The command
checks the manifest digest, every record and the explicit Stash origin before
applying. Status verifies the job's post, policy, original signature, publication
counts and retry parent. Interrupted calls reuse the saved submissions; stale
previews remain conflicts instead of being refreshed silently.

Indexed selected-post discovery exposes UUID pagination without materializing
all manifests. Disabled and forgotten posts remain accounted for; unselected
posts do not enter the plan. Forgotten entries and previews requiring review
are retained without submission. Separate commands inspect, cancel and prepare
an explicit retry. Preparing a retry makes no mutation, and a committed
publication retains its original event through later notification retries.
Plan completion remains distinct from full catalog migration or complete media
downloads. No new schema migration is required.

Real SQLite tests verify bounded discovery, exclusions, restart, canonical
cursors and indexed queries. The supported Python CLI runs against the actual
Go HTTP handlers and worker with lost admission, cancellation and retry replies;
restarting the client recovers the original jobs with one POST per mutation.
Producer tests also cover record tampering, changed endpoints, private plan
publication, notification retry chains, review outcomes, contradictory server
responses and HTTP transport boundaries. All ten new tests pass on Python 3.12
and 3.14. Backend generation, all v3 validation (528 tests in 91 files and 71
native contracts), all 248 producer tests and Go lint passed. The complete Go
suite passed: API 411.114 seconds, ingestion 527.513 seconds and SQLite 561.364
seconds. The uppercase-cursor fixture was subsequently made deterministic and
rechecked separately.

The installed command was rehearsed through a private local HTTP server against
a fresh 14,489,649,152-byte copy of the previous schema-1000040 rehearsal. All
1,091 selected posts completed: 559 existing galleries synchronized without new
memberships or media choices, and 532 single-media posts remained ineligible
no-ops. The plan retained 1,253 existing attachment choices and 1,201 unavailable
attachments. Repeated Apply before and after processing returned the same jobs
without another submission. The server loaded no production plugins and used
no production API key, website credentials or media mount.

Copying took 72.119 seconds. Normal database opening under concurrent test load
took 503.493 seconds; previews took 5.879 seconds, admission and pending replay
7.642 seconds, and worker processing 21.552 seconds. The complete command
rehearsal, including finished-status replay, took 539.788 seconds. Startup
validation remains separate from targeted API operation timings.

Independent reconciliation passed in 295.859 seconds. All 172 tables outside
job history match the source copy exactly. Every original job, submission and
attempt is preserved; precisely 1,091 rows were added to each of those three
tables. The verifier checks saved plan hashes, post coverage, work/resource keys,
submission identity, publication counts, results and attempts directly against
SQLite. Integrity is `ok`, with zero foreign-key violations. The resulting copy
is 14,491,217,920 bytes.

Private plans, API reports, copied data, rehearsal helpers and the independent
report are under `.local/native-album-cli-rehearsal-20261002/`. Logs under
`/tmp/stash-native-transition` use the `album-cli-` prefix: `focused.log`,
`python.log`, `python312.log`, `discovery-final.log`, `validation.log`,
`backend.log`, `rehearsal.log` and `reconciliation.log`.

Native album review UI, remaining catalog families, caller conversion,
activation, backup/restore drills and final cutover reconciliation remain open.
This increment does not migrate live data, activate source jobs, deploy
production, or merge develop.

## Native retained documents and application inspection

Schema 1000041 adds shared original document bytes, distinct parser
interpretations, collection/path/post associations, historical selected-head
claims and revisioned native choices. A single document can serve many paths
or posts; differing interpretations reuse its content bytes. Empty, malformed
and repaired NFO input remains evidence with its original parser status,
warnings and unknown fields. Source times retain their exact spelling and
precision. Paths remain literal historical evidence, including backslashes and
unattributed folder defaults; reading them cannot open a server filesystem path.

The application API exposes one document or byte download at a time, with
indexed bounded post/location association, claim and decision-history pages.
Selection requires the current revision and exact collection/path/claim scope.
Explicit unlinks survive later capture or migration attempts. New evidence
advances post review revisions but creates no media or performer attribution
and applies no scene/image metadata. These operations use application
authentication; source producer routes cannot access them.

Core and HTTP tests cover shared bytes, preserved interpretations, empty and
invalid content, exact replay, conflicting scope, forgotten posts, guarded
choices, lost edit replies, strict inputs, indexed lookups and restart. The
normal SQLite backup retains populated document bytes, claims and explicit
selection history; anonymised exports remove their private content. Startup
refuses missing guards and corrupted bodies/interpretations without rewriting
the database. A real schema-40 upgrade preserves existing entities; an injected
table collision rolls back all new document tables and leaves a dirty migration
that normal startup refuses.

Backend generation, v3 validation (528 tests in 91 files and 71 native operation
contracts), all 248 producer tests and Go lint passed. The complete Go suite also
passed: API 356.100 seconds, ingestion 465.573 seconds and SQLite 520.524 seconds.

A read-only assessment passed all 272,556 documents, 415,919 path associations
and 415,613 selected-head timestamps from the 1,697 frozen catalog copies.
This includes 520 invalid, 218 repaired and one empty original document, plus
1,358 associations with no post. Original hashes, parser JSON, paths and times
all satisfy the native retention contract. The source already shares 272,556
distinct byte bodies, totaling 81,558,266 bytes before native compression;
the new system can preserve those shared references. This assessment imports
no catalog records.

A fresh 14,491,217,920-byte copy of the preceding album CLI rehearsal was made
in 40.426 seconds. Promotion from schema 1000040 to 1000041 completed in
265.576 seconds, including the existing native validation before migration.
Normal opening of the resulting schema-41 database passed in 173.131 seconds.
Independent reconciliation passed in 593.804 seconds: all 173 pre-existing data
tables match exactly, every earlier migration-history row is preserved, and
only the schema version and one history entry changed. The six new document
tables remain empty pending the importer. Integrity is `ok`, with zero
foreign-key violations. The resulting copy is 14,491,320,320 bytes.
Private copies, assessment and reconciliation helpers/results are under
`.local/native-documents-rehearsal-20261002/`; logs use the `source-documents-`
prefix under `/tmp/stash-native-transition`: `generation.log`, `validation.log`,
`backend.log`, `http.log`, `migration.log`, `backup.log`, `assessment.log`,
`rehearsal.log`, `reopen.log` and `reconciliation.log`.

To provide rehearsal space, the older schema-39 source-album copy was compressed
from 14,488,055,808 to 3,184,896,088 bytes. Decompression matched the original
SHA-256 before the redundant raw copy was removed. Its archive, checksum report
and original verification artifacts remain in
`.local/native-source-album-rehearsal-20261002/`.

The resumable catalog-document importer, native document UI, remaining catalog
families, caller conversion, activation, coordinated backup/restore and final
cutover reconciliation remain open. Production data, deployments, source jobs
and develop remain unchanged.

## Resumable retained catalog document import

Schema 1000042 adds a bounded document-import pass and immutable per-row
receipts. The supported `stash-import-catalog-documents` command uses the native
application API after the received snapshot's evidence pass. It maps normalized
documents and path associations, or the older flat sidecar format, before
selected-head rows. Original bytes, exact parser evidence, literal paths and
source timestamp spelling survive. Shared documents reuse one byte body and
interpretation while preserving each collection/path/post association.

Explicit historical heads take precedence. A path without one retains the
legacy reader's timestamp-text/hash ordering as a labeled `legacy_fallback`
claim. Invalid explicit heads require review. The importer preserves existing
native choices, including manual unlinks, and retains conflicting historical
claims for inspection. Associations retain the registry-created collection
revision even after later renaming, root binding or retirement. Importing
evidence advances post review revisions without applying entity metadata,
changing performer ownership, creating library media or constructing galleries.

Each transaction processes at most 50 records with a 16 MiB batch threshold.
The processed-record checkpoint commits with domain facts and receipts. A
restarted client reads saved progress after a lost response, and completed
replays issue no further writes. Startup validates phase coverage, counts and
native reference scope. Bounded API summaries keep large original values in
individual record responses. Anonymised exports clear the new private receipts.
Every completed pass still reports `imported:false` pending the remaining
families and final reconciliation.

Focused tests cover dependency-phase resume, rollback after a receipt failure,
shared bytes, exact source values, invalid timestamp review, forgotten posts,
write/checkpoint guards, renamed collections, manual unlinks and later review
changes. The HTTP test runs actual Python preparation/upload/import through
the Go API, loses a committed batch response, replays completion, checks copied
catalogs, and imports real v1 flat storage and an empty document family.
Python receipt checks reject changed bindings, phase regression, stalled
progress and false completion. Python 3.12 validation and package installation
also pass.

Backend generation, v3 validation (528 tests in 91 files and 71 native operation
contracts), all 251 producer tests and Go lint passed. The full Go suite passed:
API 357.549 seconds, ingestion 468.535 seconds and SQLite 521.282 seconds.

An isolated 14,491,320,320-byte schema-41 copy was made in 49.179 seconds.
Promotion to schema 1000042 passed in 587.242 seconds while the full test suite
was also running. All 1,697 frozen catalogs then mapped successfully in 23,350
bounded transactions: 1,104,088 source rows and zero review outcomes. The pass
retained 272,556 shared documents, 415,919 path associations and 415,613 explicit
head claims/selections, including 1,358 associations without a post. The shared
original bytes total 81,558,266 before compression. Completed receipt replay
after reopening passed; reopening took 242.983 seconds, and import plus reopen
and replay took 1,196.500 seconds.

Independent reconciliation passed in 271.930 seconds. Fresh row fingerprints
for all 172 unrelated data tables match the preceding baseline's verified
fingerprints. Every physical catalog document/source/head row matches its
original frozen SQLite values, including binary content, exact parser text,
paths, times, post mappings, historical collection revisions and native
decisions. Native byte hashes and document identities were checked independently
of the Go reader. The only changes to pre-existing post rows are the expected
1,243,071 review-revision increments. Earlier migration history and sequence
values are preserved. Integrity is `ok`, with zero foreign-key violations.
The resulting isolated database is 16,420,761,600 bytes.

Private helpers, receipts, copy metadata and independent reports are under
`.local/native-catalog-documents-rehearsal-20261002/`. Logs under
`/tmp/stash-native-transition` use the `catalog-documents-` prefix:
`focused.log`, `generation.log`, `backend.log`, `validation.log`, `package.log`,
`python312.log`, `promotion.log`, `rehearsal.log` and `reconciliation.log`.

For rehearsal space, two older generated database copies were compressed and
their decompressed SHA-256 digests verified before removing the redundant raw
files. The membership rehearsal changed from 14,483,116,032 to 3,183,657,884
bytes; the album-job rehearsal changed from 14,489,649,152 to 3,185,332,761
bytes. Their `.sqlite.zst` archives, checksum reports and original verification
artifacts remain in their respective private rehearsal directories. The
schema-41 baseline and original frozen catalog inputs remain available.

Native document/album review UI, translations, remaining catalog families,
caller conversion, activation, coordinated backup/restore and final cutover
reconciliation remain open. This checkpoint imports only isolated copies;
production data, deployments, source jobs and develop remain unchanged.

## Shared translation results and retained catalog import

Schema 1000043 stores exact original/output text and nullable language/provider
facts once per shared result, with separate post and historical collection
provenance. Native original-text hashes cover exact UTF-8 bytes. Historical
declared hashes retain their algorithm and do not replace that verified identity.
Unknown languages stay unknown, including during English-language lookups.
Application read APIs page post evidence without repeating text bodies.

`stash-import-catalog-translations` imports the received frozen snapshot after
its evidence pass. Transactions process at most 50 rows with a 16 MiB threshold;
the source-ordinal checkpoint commits with results, assertions and immutable
receipts. The original collection revision survives later renaming or retirement.
Missing hashes/originals remain explicit. Conflicting hashes, invalid provenance
and forgotten posts retain review outcomes. Import neither applies entity
metadata nor starts provider jobs; whole-catalog status stays `imported:false`.

Tests cover shared results, exact Unicode/control characters, nullable identity,
historical Python JSON hashes, language filters, malformed values, stale cursors,
transaction rollback, forgotten posts, corrupt startup data, ordinary backup
restore and anonymisation. The actual Python-to-Go HTTP fixture loses the first
committed batch response, resumes/replays, imports copied/old/empty catalogs,
checks bounded routes and reopens the database. The Python client rejects changed
bindings, stalled progress and false completion. Package installation and its
new command entry point passed.

Backend generation, v3 validation (528 tests in 91 files and 71 native operation
contracts), all 254 producer tests on Python 3.14 and 3.12, and final Go lint
passed. The full Go suite passed: API 366.265 seconds, ingestion 472.693 seconds
and SQLite 531.604 seconds. Final focused checks also passed after the validator
cursor cleanup and added backup assertion.

The isolated schema-42 copy took 34.285 seconds and 16,420,761,600 bytes.
Promotion to schema 1000043 passed in 734.252 seconds under concurrent test load.
All 1,697 catalogs mapped 59,023 translation rows in 2,692 bounded transactions,
with zero review outcomes. Import, normal opening, reopening and completed-receipt
replay took 609.322 seconds; reopening accounted for 301.423 seconds. Startup
validation remains a material measured cost for the final performance gates.

Independent reconciliation passed in 448.862 seconds. Every physical source row,
shared result, original UTF-8 hash, declared historical JSON hash, exact timestamp,
post reference and original collection revision matched. There are 52,078 shared
results and 59,023 provenance assertions; all 59,023 historical hashes verified.
Target language/provider remain unknown for 56,168 rows; 2,855 explicitly record
English and translate-shell/Bing. Fresh row comparisons against the preceding
copy confirm all 180 unrelated tables are unchanged. The only existing post-row
changes are 59,023 expected review-revision increments. Prior migration history
and sequences are preserved. Integrity is `ok`, foreign-key violations are zero,
and the final copied database is 16,522,178,560 bytes.

Private copy metadata, importer/verifier helpers, receipts and reports are in
`.local/native-catalog-translations-rehearsal-20261002/`. Validation logs under
`/tmp/stash-native-transition` use the `source-translations-` prefix. To retain
rehearsal space, the unused schema-41 copy was compressed from 14,491,320,320 to
3,185,745,882 bytes; its decompressed SHA-256 was verified before deleting the
redundant raw copy. Its archive, checksum report and earlier evidence remain.

A separate read-only automation assessment is retained in
`.local/native-translation-queue-assessment-20261002/`. It contains 240,397
translation jobs, 396,679 targets and 42,732 cached outcomes, including 40,777
English detections, 1,954 translations and one no-text outcome. Of the targets,
324,195 remain unapplied; retry delays, priorities and original status must
survive the next migration. Input hashes, integrity and foreign keys passed.
Enrichment and discovery tables are inventoried too. This assessment snapshot
is not coordinated with the frozen catalogs or a production cutover.

Translation queue/cache/target migration, remaining catalog histories, native
UI, caller conversion, compatibility removal, coordinated backup/restore and
cutover reconciliation remain open. Production and develop are unchanged.

## Shared translation work and inspection

Schema 1000044 retains shared versioned translation requests, immutable cache
outcomes and per-post/field targets with scheduling history. Exact input text
and output results are shared across targets. Cache outcomes distinguish a
translation, unchanged text already in the target language, and no-text work.
Original provider timestamps remain separate from native receipt times; replay
does not overwrite them or create orphan results for conflicting outcomes.

Held work stays held when a cache arrives or the same target is retained again.
Explicit scheduling requires the current revision and preserves the chosen
priority/deadline. Publication checks the due time and target revision, retains
historical collection scope after renaming/retirement, and atomically records
post provenance. It does not overwrite scene/image fields. Forgotten posts
retain review outcomes; no-text completion invents no translation evidence.

Six application inspection routes expose requests, caches, targets and history.
Target/history pages are bounded and reference shared text. Tests cover cache
sharing, Unicode/control characters, held/delayed work, stale revisions, terminal
replay, missing languages, no-text completion, failed transaction rollback,
forgotten posts, indexed pagination, ordinary backup restoration, anonymisation,
startup corruption refusal and upgrade collisions. The actual Python catalog
import HTTP fixture also passes with the new schema.

The frozen queue assessment was matched read-only against schema 43's retained
post identifiers. Of 396,679 targets, 395,878 have exact imported post references;
801 have no matching post in these copies (793 historically applied and eight
pending). There are no missing collection mappings or ambiguous matches. The
snapshots were taken separately, so these are review inputs for coordinated
reconciliation, not proof that live posts are missing. All 240,397 job inputs
target English; the largest original is 15,711 UTF-8 bytes. The assessment is
`.local/native-translation-queue-assessment-20261002/native-reference-assessment.json`.

A further cache assessment found 371 of the 42,732 cached outcomes labelled
`english` with detected language `en` but output text different from the exact
original. Those historical outputs cannot be passed unchanged to the native
`unchanged` outcome, which requires the original text. The importer must retain
the original rows/results and record an explicit mapping disposition. Counts
and classification are retained in `native-cache-assessment.json` and
`native-cache-conflict-assessment.json` in the same assessment directory.

Execution admission/provider workers and the actual automation import remain
unfinished. This storage checkpoint does not activate translation work, migrate
the live queue, or complete the full archive transition.

Backend generation, v3 validation (528 tests in 91 files and 71 native operation
contracts), all 254 producer tests and final Go lint passed. The full Go suite
passed: API 380.380 seconds, ingestion 481.929 seconds and SQLite 554.317 seconds.
Final focused translation/schema/API tests passed after the lint fixes.
Checkpoint `6f74b8846` also passed CI lint, build and native preview publication.

The isolated schema-43 backup took 44.210 seconds and 16,522,178,560 bytes.
Promotion to schema 1000044 passed in 823.506 seconds under concurrent test load.
Independent reconciliation passed in 341.906 seconds: all 185 existing data
tables match the preceding copy row by row, prior schema definitions/history
and sequences are preserved, and the four new work tables are empty. Integrity
is `ok`, with zero foreign-key violations. The resulting copy is 16,522,244,096
bytes. Normal reopening passed in 211.083 seconds.

The startup CPU profile attributes about half the cost to each of two identical
lineage validations: the primary-version lookup and the obsolete fork-version
lookup both create a new validated migrator. This redundant pass is a concrete
performance follow-up; the remaining integrity queries still require budgeting.
Private copy metadata, comparison report, CPU profile and assessment are in
`.local/native-translation-work-rehearsal-20261002/`. Logs under
`/tmp/stash-native-transition` use the `translation-work-` prefix.

For rehearsal space, the unused schema-42 copy was compressed from 16,420,761,600
to 3,641,362,723 bytes, with its decompressed SHA-256 verified before removing
the redundant raw copy. The compressed archive, checksum report and earlier
reconciliation remain in `.local/native-catalog-documents-rehearsal-20261002/`.
The schema-43 baseline, frozen inputs and production data remain unchanged.

## One integrity pass during native startup

Database startup now reads schema versions through one validated connection.
Native databases no longer open another validated migrator to read an obsolete
fork ledger. Historical import inputs still read their fork version through the
same connection. Every new migrator continues to run the complete pre-write
validation, with no cross-open validation cache or weakened corruption checks.

On the same schema-44 library copy, the measured normal reopen decreased from
211.083 to 136.714 seconds. The CPU profiles show integrity-validation work
decreasing from 204.20 to 107.55 seconds, and the second version-lookup pass is
absent. These are individual rehearsal measurements; the remaining SQL checks
and explicit release performance budgets remain open work.

Focused lineage, historical promotion and corruption-refusal tests pass,
including refusal of an unexpected active fork ledger in a native database.
Backend generation and Go lint pass. The new profile and reopen report are
`reopen-single-pass-cpu.pprof` and `reopen-single-pass.json` beside the original
measurements in `.local/native-translation-work-rehearsal-20261002/`.
The full Go regression suite passed: API 352.198 seconds, ingestion 454.305
seconds and SQLite 512.664 seconds. Logs use the `native-startup-` prefix in
`/tmp/stash-native-transition`. No schema or stored values change in this step.

## Bounded native translation execution

Schema 1000045 adds durable translation jobs bound to at most fifty exact target
revisions. Admission preserves priority, due time and held state, with one active
batch per shared request and a separate translation queue ceiling. Later targets
cannot bypass an existing retry delay or join an already admitted batch. Request,
revision-binding and active-request lookups use indexes; a query-plan regression
test caught and removed SQLite's affinity-induced expression-index scan.

The worker checkpoints cached output under its lease before atomically publishing
its target evidence and outcome. Retries and restored workers reuse that cache.
Cancellation, changed target revisions and lease expiry prevent stale publication;
terminal failures require explicit retry. Forgotten posts retain review without
requiring a provider call. No operation selects scene/image metadata. Application
routes support target creation, scheduling, explicit retry, bounded admission,
status/history and cancellation; scoped producer tokens cannot administer them.

The optional translate-shell/Bing provider retains exact original text, processes
HTML and Unicode chunks, bounds output/runtime, disables init files and terminates
its Unix process group on cancellation. Tests use local fake executables, with no
provider network requests. Server startup leaves execution disabled by default;
missing provider configuration also leaves the worker stopped. Production has
not been activated or migrated.

The historical automation import, automatic capture scheduling, remaining archive
record families, native UI, compatibility removal, coordinated backup/restore and
cutover gates remain open. The full transition goal is still active.

Backend generation, v3 validation (528 tests in 91 files and 71 native operation
contracts), all 254 producer tests and final Go lint passed. The full Go suite
passed: API 402.665 seconds, ingestion 496.017 seconds and SQLite 575.472 seconds.
Targeted race tests passed for the worker, repository, API and provider. The
translation package cross-compiles for Windows with the Unix provider disabled.
Final provider tests additionally reject corrupt Unicode/duplicate JSON keys and
prove that stdout copying cannot bypass the output ceiling. Local process tests
verify cancellation also stops the provider's child process.

Migration fixtures preserve preexisting jobs, receipts, attempts and checkpoints;
upgrade collisions roll back the recreated jobs table. Additional tests cover
bounded concurrent admission, priority, backoff across new targets, stale leases,
held/superseded targets, forgotten posts without cached text, atomic admission
and publication failure, restored caches, startup corruption refusal, full job
anonymisation, origin checks and producer-access isolation.

The isolated schema-44 backup took 38.005 seconds and 16,522,244,096 bytes.
Promotion to schema 1000045 passed in 332.487 seconds. Independent reconciliation
passed in 784.869 seconds: all 189 existing data tables match the baseline row
for row, prior migration history and sequences are preserved, and the only old
schema-definition change is the added archive-job kind. The new binding table is
empty. Integrity is `ok`, foreign-key violations are zero, and the resulting copy
is 16,524,034,048 bytes. Normal reopening passed in 352.523 seconds while the
full backend suite and reconciliation were also running; this is verification
under load, not an accepted startup performance budget.

Private copy metadata, independent comparison and startup profile are under
`.local/native-translation-jobs-rehearsal-20261002/`. Validation logs under
`/tmp/stash-native-transition` use the `translation-jobs-` prefix. For space, the
unused schema-43 copy was compressed from 16,522,178,560 to 3,671,721,011 bytes.
Its decompressed SHA-256 was verified before removing the redundant raw copy;
the archive, checksum report and original reconciliation remain retained. The
schema-44 baseline, frozen catalogs/automation snapshot and production remain
unchanged. No historical translation queue has been imported in this step.

## Frozen automation snapshot preparation

`stash-prepare-automation` now prepares and verifies bounded, deterministic input
for all ten maintenance, translation, enrichment and discovery families. The
reader accepts the recognized `SCPC` schema at versions 1–3, inventories retained
DDL without executing it, and records the original database checksum. Frozen
inputs open immutably; active journals, source replacement/writes, unknown schema
objects/columns and corrupt databases are rejected before publication.

Records preserve exact SQLite text, nulls, binary values, integers and finite
real values. Legacy JSON remains an original string even when malformed; broken
foreign-key references remain inventoried review evidence. Primary keys use
SQLite numeric/binary ordering. Private chunks are limited to 1,000 records and
16 MiB, with individual table/chunk hashes and counts. Publication flushes the
files and directory, never replaces an existing destination, and permits
verification after a lost acknowledgement. Empty inputs retain all ten families.
The matching Go parser shares framing/table validation with catalog snapshots,
while preserving the different rules for raw legacy automation values.

The actual frozen automation copy has application ID 1396920387 and version 1.
Preparation retained 895,886 records in 896 chunks (429,249,192 bytes including the
manifest), taking 19.981 seconds. Python verification passed in 10.841 seconds;
an independent direct-SQL comparison matched every original column value and
floating-point representation in 6.748 seconds. Go independently validated all
records, ordering, table totals and hashes in 5.477 seconds. The original source
checksum remains unchanged. The source contains 240,397 translation jobs,
396,679 translation targets, 252,050 enrichment jobs and the remaining seven
families. Inputs, scripts and reports are retained privately in
`.local/native-automation-snapshot-rehearsal-20261002/`.

All 260 producer tests passed. The complete scrape package tests, catalog SQLite
snapshot tests and actual Python/Go HTTP upload regression passed after sharing
the validator; final repository Go lint reports zero issues. Synthetic fixtures
cover lossless binary/JSON/integer/retry values, semantic defects, supported
versions, unknown shapes, empty inputs, changed files, limits and interrupted
publication. The preceding translation execution checkpoint `9f79d3589` also
passed CI build, lint and native image publication.

This checkpoint prepares input only: summaries explicitly report
`imported:false` and every family pending. Native receipt, historical cache/job
mapping, held pending work and reviewed activation are still required. The
automation and catalog copies were captured separately, so they do not establish
the coordinated production cutover boundary. No live schema, queue, service or
configuration has changed, and the full transition remains in progress.

## Resumable native automation receipt

Schema 1000046 and `stash-upload-automation` retain the complete frozen operating
input through the native application API. One snapshot binds to an imported
registry source. Exact manifests and chunks resume after lost responses or
restart; changed bytes, gaps and source rebinding are rejected. Original records,
per-family hashes and receipt checkpoints commit together. Empty snapshots retain
all ten family descriptors without creating work.

Catalog and automation upload now share the bounded transport and transactional
chunk receiver. Stored hash state is checked before appending new records.
Automation startup validation verifies source bindings, exact original bytes,
ordering, chunk receipts and per-table hashes before accepting writes. Indexed
chunk ranges avoid scanning the entire snapshot for every chunk. Anonymisation
removes all four new tables' records.

Receiving remains distinct from native mapping: receipts report `imported:false`
and all families pending. No translation targets, execution jobs, source posts or
provider requests are created. Historical outcome mapping, held pending work,
reviewed activation and the remaining native transition phases stay open.

Backend generation, Go lint, all 263 producer tests and v3 validation passed
(528 tests in 91 files and 71 native operation contracts). The complete backend
run passed ingestion in 543.772 seconds and SQLite in 629.277 seconds. Its only
failure used system Python for the existing gallery-dl worker interop fixture;
rerunning that fixture and the new automation API tests with the prepared producer
runtime passed in 39.794 seconds. Focused race tests passed for SQLite in 27.262
seconds and the API in 8.822 seconds. Fixtures cover concurrent receipt, restart,
exact replay, rejected corruption without database writes, source conflicts,
atomic rollback and migration collisions. The preceding preparation checkpoint
`e18d5b917` passed CI lint, build and preview publication.

The isolated schema-45 copy took 41.976 seconds and 16,524,034,048 bytes. Promotion
to schema 1000046 passed in 709.360 seconds under concurrent test load. The actual
Python client then received all 895,886 records in 896 chunks through the native
HTTP routes in 76.270 seconds, including local verification, lost begin/chunk
responses and completed replay. Startup before that upload took 105.761 seconds.

Independent reconciliation passed in 424.463 seconds: all 190 existing data
tables match the preceding copy row for row, prior schema definitions/history
and sequences are preserved, and every retained automation line matches its
frozen chunk. There are ten family descriptors, 896 immutable chunk receipts
and 895,886 staged records. Integrity is `ok`, with zero foreign-key violations.
The resulting copy is 17,579,802,624 bytes. This proves retained input and library
preservation, not domain import or production cutover.

Normal reopening after receipt passed in 104.798 seconds, including full stored
record validation. Its CPU profile remains with the rehearsal artifacts. These
individual timings do not establish an accepted startup performance budget;
reducing and budgeting the remaining integrity-query cost remains release work.

Private rehearsal artifacts are in
`.local/native-automation-receipt-rehearsal-20261002/`; validation logs under
`/tmp/stash-native-transition` use the `automation-receipt-` prefix. Production
and `develop` remain unchanged.

For rehearsal space, two unused validated copies were compressed and their
decompressed SHA-256 values verified before removing the redundant raw files.
The schema-44 translation-work copy shrank from 16,522,244,096 to 3,671,724,172
bytes; the older catalog-media copy shrank from 14,250,086,400 to 3,126,674,522
bytes. Their compressed archives, checksum reports and original reconciliation
reports remain retained. The schema-45 baseline and frozen source inputs remain
unchanged.

## Frozen translation history and held native work

Schema 1000047 and `stash-import-automation-translations` map the two translation
families from a received frozen automation snapshot. Requests preserve exact
text and the historical English provider policy. Valid outcomes share native
caches; conflicting native outcomes and malformed source values retain explicit
review receipts. Original attempts, errors, state and cached JSON remain linked
to every source ordinal. Older English rewrites stay in that immutable input;
the native unchanged result preserves the exact original text.

Applied targets require an exact source-qualified post and a proven cached
outcome. Completion records migration evidence with unknown provider capture
time. It does not invent execution attempts or copy job update times into capture
timestamps. Unapplied targets remain held with their original priority and retry
deadline. Multiple legacy aliases may share one target; a later historical
completion can promote only an untouched hold created by the same import.
Preexisting native targets and later scheduling edits retain their choices.

Known unfinished catalog evidence imports block the affected batch, preventing
premature unmatched classifications. Missing or forgotten posts remain reviewable.
Requests, caches, target revisions, evidence and receipts commit atomically in
batches of at most 200 records or 16 MiB. An indexed source range excludes the
other eight automation families. Application inspection exposes bounded summaries
and exact source details; producer tokens cannot administer the importer.

Generation, Go lint, the full backend suite and all 266 producer tests passed.
The backend run passed API tests in 390.122 seconds, ingestion in 476.398 seconds
and SQLite in 576.977 seconds. V3 validation passed 528 tests in 91 files and
71 current application operation contracts. Focused race checks passed SQLite
in 42.049 seconds and API interop in 9.717 seconds. Fixtures cover lost committed
HTTP responses, sparse checkpoints, restart/replay, aliases across batches,
native edits/cache conflicts, catalog prerequisites, backup/restore,
anonymisation, late-write rollback, corruption refusal and upgrade collisions.

The full-source semantic assessment accepted all 240,397 original jobs: 197,665
requests without cached outcomes, 42,361 ordinary cached outcomes and 371
normalized English outcomes. That assessment took 5.120 seconds and retained
the original frozen source unchanged. A built wheel installed into a fresh
runtime and exposed the new migration command.

The isolated schema-46 backup took 56.977 seconds and 17,579,802,624 bytes.
Promotion to schema 1000047 passed in 509.282 seconds under concurrent test load.
The actual Python client imported all 637,076 translation rows through HTTP in
422.228 seconds, including deliberately lost responses during both job and
target batches and completed replay. Opening before that run took 231.877
seconds while the backend suite was also running.

There are 240,397 shared requests, 42,732 cached outcomes and 395,826 native
targets: 324,169 held and 71,657 historically completed. The 52 duplicate source
target rows retain receipts against shared targets. All 801 review records are
the source-qualified post references already missing from the separate frozen
catalog boundary; they do not create posts or restart work. New history includes
467,483 target revisions and 71,640 migration evidence records; empty-text
completions create no evidence. Existing album work is preserved, and no
translation execution jobs or pending targets are created.

Independent reconciliation passed in 348.533 seconds. All 187 unaffected tables,
including 895,886 original automation records and 2,182 prior successful album
jobs, match the baseline row for row. All previous translations/evidence,
schemas, migration history and sequences are preserved. Every native request,
cache, target and receipt agrees with its source text, outcome, qualified post,
collection and schedule. The only post changes are the exact evidence-driven
revision increments on 41,095 posts. Selected scene/image metadata and identities
remain unchanged. Integrity is `ok`, foreign-key violations are zero, and the
resulting database is 18,417,156,096 bytes. An initial verifier assumption that
the archive-job table was empty was corrected to preserve and compare its
existing album jobs before the complete comparison was rerun.

Private rehearsal artifacts are under
`.local/native-automation-translation-rehearsal-20261002/`; validation logs under
`/tmp/stash-native-transition` use the `automation-translations-` prefix.
Normal reopening passed in 133.773 seconds, including the imported request,
cache, target, receipt and completion checks. Its CPU profile is retained with
the rehearsal. This timing is an individual measurement; startup performance
budgets remain release work.

For space, the unused schema-45 copy was compressed from 16,524,034,048 to
3,672,096,945 bytes and the older catalog-attachment copy from 10,486,972,416 to
2,248,297,009 bytes. Both decompressed SHA-256 values matched before removal of
their redundant raw files. Archives, checksum reports and prior reconciliations
remain retained; the schema-46 baseline and frozen source inputs are unchanged.

Reviewed activation, automatic scheduling, other operational/history families,
native UI/caller conversion, compatibility removal, backup/restore/export drills,
performance budgets and cutover remain open. Production and `develop` are
unchanged.

## Reviewed translation activation API

Schema 1000048 adds application-only preview/apply/status operations for bounded
translation activation. One immutable receipt releases at most 100 exact held
target revisions, preserving priorities and retry deadlines. The reviewed input,
concrete plan and released revisions commit together. Lost responses replay the
original receipt after restart, completion or later scheduling edits. A changed
member rejects the whole batch, and a failed final receipt write rolls back all
releases even if a caller accidentally swallows the error.

Optional frozen-snapshot bindings restrict activation to its original imported
holds. Indexed candidate paging reports later scheduling choices, completed
targets and forgotten posts without treating those as eligible work. Original
source ordinals and target revisions remain available. Activation creates no
execution jobs or provider calls; ordinary worker admission retains its active
job limit, shared result cache and target deadlines.

Focused archive, SQLite and API checks passed, covering bounds, reviewed hashes,
concurrent replay, backup/restore, anonymisation, source-scoped candidate paging,
forgotten posts, late failures, corrupt receipts and schema upgrade collisions.
The actual Python import fixture also passed with candidate inspection through
HTTP. Race checks passed SQLite in 21.294 seconds and API in 4.494 seconds. Go
lint reported zero issues. One new completion fixture initially tried to publish
before its later activation timestamp; its simulated clock was corrected and
the check passed. The full backend regression run passed, including API in
393.991 seconds, ingestion in 468.332 seconds and SQLite in 575.694 seconds.
Validation logs under `/tmp/stash-native-transition` use the
`translation-activation-` prefix.

The preceding `198de4686` checkpoint passed CI build, lint and preview image
publication. The resumable backlog client and full-source activation rehearsal
are recorded in the following checkpoint. Automatic capture scheduling, remaining
catalog and operational families, native UI, live host/n8n conversion, compatibility removal,
release backup/restore/export drills, performance budgets and cutover remain
unfinished. Production and `develop` are unchanged.

## Resumable bulk translation activation

`stash-activate-automation-translations` now prepares private, hashed candidate
pages and reviewed operation identities before releasing imported holds. Apply
validates the complete saved plan, checks each page again on use and inspects
existing receipts before sending another mutation. Lost committed responses
resume the original operations. Excluded native choices remain excluded; stale
batches are reported without preventing independent batches from completing.
Activation status proves release of the selected holds, not provider execution.
Local page review reads only the manifest and requested page; all pages still
require validation before any Apply.

All 275 producer tests passed in 6.565 seconds. The real Python/Go HTTP fixture
passed in 2.514 seconds, including a lost committed response, status, replay and
database reopening. Its race run passed in 4.471 seconds, and Go lint reported
zero issues. A built wheel installed into a fresh runtime, exposed the command
and read a page from the full saved rehearsal plan. The preceding API checkpoint
`a59d8cd20` passed CI lint, build and native preview publication; its complete
backend regression gate remains the latest full-suite result.

The isolated schema-47 backup took 47.146 seconds and 18,417,156,096 bytes.
Promotion to schema 1000048 passed in 379.687 seconds. The actual CLI then
prepared and activated all 324,169 original holds through the Go HTTP routes in
3,242 batches. It recovered deliberately lost committed responses after the
first batch and at 150,000 targets, then verified status and exact replay. The
complete client sequence took 277.542 seconds after opening in 131.392 seconds.
Provider execution remained disabled. With the final client, single-page review
took 15.771 milliseconds and full saved-plan validation took 8.494 seconds;
these measurements do not establish general UI or release performance budgets.

Independent reconciliation passed in 593.196 seconds. All 194 unaffected tables
match the preceding copy row for row, including original import receipts,
selected scene/image metadata and successful album jobs. Every activation plan,
receipt and exact held-to-pending revision matches its saved source binding.
All 467,483 prior history rows and 71,657 completed targets are preserved;
324,169 new revisions record only the intended releases. There are no translation
execution jobs, integrity is `ok`, and foreign-key violations are zero. The
resulting database is 18,794,524,672 bytes.

Normal reopening passed in 164.042 seconds, including the new activation receipts
and historical revision checks. Its CPU profile is retained with the rehearsal.
Startup cost remains part of the unfinished release performance work.

Private scripts, saved plan, comparisons and installed-wheel evidence are under
`.local/native-translation-activation-rehearsal-20261002/`. Validation logs under
`/tmp/stash-native-transition` use the `translation-activation-cli-` prefix.
For space, the unused schema-36 post-media copy and schema-46 automation-receipt
copy were compressed to 2,248,298,297 and 3,805,953,734 bytes respectively. Both
decompressed SHA-256 values matched before removing the redundant raw files.
Their archives and earlier reconciliation reports remain retained; the schema-47
baseline and frozen source inputs are unchanged.

Automatic capture scheduling, the remaining operational/history imports, native
UI and caller conversion, compatibility removal, coordinated backup/restore and
portable export, performance budgets and production cutover remain unfinished.
The full transition goal remains active; production and `develop` are unchanged.

## Automatic translation scheduling for captured source text

Schema 1000049 adds independent, revisioned collection translation policies and
immutable per-capture decisions. The policy selects title/caption, target
language, provider behavior and priority. Missing policies disable scheduling.
An enabled capture queues at most two targets in the same transaction as its
source evidence and ingestion receipt. Equal text shares requests/cache results;
repeated captures retain existing targets, holds, deadlines and outcomes. Receipt
replay preserves the original decision after policy changes and worker completion.
Translation never selects scene/image metadata.

Collection changes require policy review before new scheduling. Original evidence
still commits, with an explicit review result. Empty text receives a `no_text`
entry. The scheduler reads shared text metadata without reconstructing provider
payloads. Application routes expose policy configuration/history and individual
capture decisions; scoped producer tokens cannot administer these routes.
Startup validation, backup/restore and anonymisation cover the new records.

Backend generation, Go lint and all 275 producer tests passed. Focused tests cover
real capture acceptance/replay, shared provider results, preserved holds,
disabled policies, stale collection revisions, large text, HTTP authorization,
late transaction failures, backup/restore, corrupt-state refusal and migration
collisions. Race checks passed SQLite in 19.354 seconds, ingestion in 16.204
seconds and the API in 8.572 seconds. The complete backend run passed API in
487.656 seconds and ingestion in 555.283 seconds; SQLite exceeded Go's default
ten-minute package timeout while the full-copy backup was running. Its complete
rerun passed in 271.287 seconds. `make test` and `make it` now share an overridable
twenty-minute package timeout; operation, worker and provider deadlines are
unchanged. The preceding `fe3ab0525` checkpoint passed CI lint, build, tests and
native preview publication.

The isolated schema-48 backup took 177.101 seconds and 18,794,524,672 bytes under
concurrent regression-test load. Promotion to schema 1000049 passed in 487.469
seconds. Independent reconciliation passed in 421.142 seconds: all 198 existing
data tables match row for row, including their SQLite value types; earlier schema
definitions, migration history and sequences are preserved. The four new tables
are empty. Integrity is `ok`, foreign-key violations are zero, and the resulting
database is 18,794,569,728 bytes. Existing pending/completed translation work,
selected metadata and original import receipts remain unchanged.

Normal reopening passed in 162.647 seconds, including policy and capture-decision
validation. Its CPU profile is retained with the rehearsal. Startup performance
remains unfinished release work.

Private rehearsal artifacts are under
`.local/native-translation-policy-rehearsal-20261002/`; validation logs under
`/tmp/stash-native-transition` use the `translation-policy-` prefix. For space,
the unused schema-47 copy was compressed from 18,417,156,096 to 4,042,088,994
bytes. Its decompressed SHA-256 matched before removing the redundant raw file;
the archive and prior reconciliation remain retained. The schema-48 baseline
and frozen source inputs remain unchanged.

The next frozen enrichment inventory contains 252,050 jobs, eight cooldowns and
eight source-progress records, with no staged result payloads. Completed, pending,
retry, coalesced, missing-URL, identity-conflict and deliberately excluded states
require separate native outcomes. Their import/execution, remaining histories,
existing policy conversion, native UI, compatibility removal, coordinated
backup/restore/export, performance work and cutover remain unfinished. Production
has not changed, and the full transition goal remains active.

## Metadata-only producer extraction

The supported producer package now includes the isolated extraction component
for native enrichment, replacing its dependency on the old catalog library.
It admits known single-post extractor classes across Reddit, Twitter, Bluesky,
TikTok, Instagram, Kemono/Coomer, Patreon and Fansly. Mirror post/feed classes
are distinguished even when gallery-dl uses the service name as their instance
subcategory. Root feeds remain ineligible. Linked Redgifs/Imgur extraction is
bounded; other children remain explicit unresolved references.

Compact transcripts share post fields with per-record patches, removals and
parent references. Original observation times survive checkpoint replay and
child-only retry. The existing retention policy removes redundant previews and
incidental profile fields before persistence. Temporary child failures discard
partial child results while keeping the parent; a missing child is an explicit
source limitation. Root failures, empty results, changed requests and damaged
checkpoints cannot appear successful.

The helper projects source access/pacing settings without creating download jobs,
archives, postprocessors or cookie writes. Its cache is in memory. Originals
remain selected for Kemono/Coomer metadata. The child suppresses private logs,
disables downloader construction and stops on the first HTTP 429. The parent
bounds input/output and elapsed time, and terminates the process group after
cancellation, timeout or oversized output. Website access values remain local.

All 293 producer tests passed in 8.050 seconds, including 18 new tests for compact
reconstruction, removal/null semantics, parent retries, source configuration,
actual pinned Reddit/Twitter transformations, mirror extractor admission,
unchanged media/cookie files, subprocess isolation, rate limits, cancellation
and limits. Existing native producer/API HTTP fixtures passed in 14.892 seconds.
The final wheel installed into an isolated target and passed all 18 extraction
tests in 0.981 seconds; installed module hashes matched the working tree.
Packaging evidence is retained in `.local/native-metadata-fetch-20261002/`, and
validation logs under `/tmp/stash-native-transition` use the `metadata-fetch-`
prefix. The preceding `81c75c25d` checkpoint passed CI generation, tests, build,
lint and native preview publication.

These transcripts are producer checkpoints, not native captures or completed
jobs. The next work is the native enrichment queue, fenced result publication
against existing post identities, and import of the frozen enrichment and
discovery state. Live service conversion, remaining history/policy migration,
native UI, compatibility removal, coordinated backup/restore/export, performance
budgets and cutover remain unfinished. Production and `develop` are unchanged.

## Native post enrichment targets and retained completion

Schema 1000050 adds revisioned enrichment targets for retained post URLs and
exact collection revisions. Repeated requests preserve holds, exclusions, retry
deadlines, original provenance and completion. Application routes inspect targets
and history and allow explicit revision-checked rescheduling. Indexed readiness
omits future work, forgotten posts and outdated or disabled source bindings.
Producer tokens cannot administer these routes.

Completion binds an immutable operation to the exact pending target revision
and a bounded set of retained gallery-dl captures for that post and collection
revision. Existing valid captures can be reused; an NFO or another post/source
binding cannot certify completion. Receipt, capture references, target transition
and history commit atomically, including when a caller swallows a late error.
Exact replay preserves the receipt after later collection changes and post
tombstones. Backup and anonymisation include the new lifecycle, and startup
validation checks identities, histories, scopes, counts and content digests.

Backend generation, v3 lint/types/locales and all 528 frontend tests passed.
All 293 producer tests and Go lint passed. Focused enrichment tests cover direct
post admission, preserved choices and deadlines, stale source bindings, capture
scope, atomic late failures, HTTP authorization, backup/restore, anonymisation,
corrupt-state refusal, bounded indexes and migration collision rollback. The
complete backend gate passed, with API in 420.301 seconds, ingestion in 488.678
seconds and SQLite in 615.076 seconds. That gate was rerun with durable logs after
the earlier process handles and temporary logs disappeared.

The isolated schema-49 backup took 48.984 seconds and 18,794,569,728 bytes.
It reached schema 1000050 cleanly; the original promotion timing was not retained.
Independent reconciliation passed in 859.199 seconds under concurrent regression
load: all 202 previous data tables match row for row, including SQLite value
types. Existing schema definitions, migration history and sequences are intact;
the four new tables are empty, `quick_check` is `ok`, and foreign-key violations
are zero. The resulting database is 18,794,643,456 bytes. Selected library
metadata, earlier catalog receipts, album jobs and translation state are unchanged.

Normal reopening of the promoted copy passed in 125.846 seconds, with its CPU
profile retained. This verifies startup against the copied data; release startup
performance remains unfinished work.

A read-only check of all 252,050 frozen enrichment jobs admitted 248,862 retained
URLs. The 2,023 missing URLs and 1,165 unsupported URLs all belong to existing
missing-URL outcomes or intentionally excluded sources. This checks URL coverage
only; post identity validation and operational-state mapping remain required.

Private reconciliation and backend-gate results are retained under
`.local/native-enrichment-work-rehearsal-20261002/`; URL coverage is under
`.local/native-enrichment-assessment-20261002/`. The unused schema-30 upload
rehearsal was compressed from 6,986,661,888 to 1,205,156,664 bytes. Its decompressed
SHA-256 matched before the redundant raw copy was removed; the archive and its
earlier reconciliation are retained. Frozen inputs and the schema-49 baseline
are unchanged.

These domain records do not execute extraction or certify a worker attempt.
Fenced extraction/publication, operational cooldowns and fairness, legacy
enrichment/discovery mapping, remaining policies/histories, native UI and caller
conversion, compatibility removal, backup/restore/export, performance work and
production cutover remain unfinished. Production and `develop` are unchanged.

## Selected durable job claims

The shared job repository can claim one explicitly selected job revision without
selecting higher-priority unrelated work or running global expiry recovery.
Readiness, retry deadlines, attempt limits and shared-resource exclusion still
apply. A changed selection is rejected; concurrent callers obtain one owner.
Recovery remains a separate trusted queue operation, and old leases stay fenced.
Both claim paths now reject a transaction that tries to swallow a failed attempt
write after changing the job to running.

This is an internal primitive for the remaining enrichment coordinator. Its
producer service must authorize the selected source and producer within the
same transaction; the primitive adds no generic producer claim endpoint. Durable
enrichment binding, transcript checkpoints, verified capture publication,
cooldowns, fairness and legacy queue mapping remain separate unfinished work.
There is no schema or production configuration change.

Focused durable-job tests passed in 9.167 seconds; the selected and concurrent
claim tests also passed under the race detector in 7.360 seconds. Coverage includes
changed selections, concurrent owners, preserved retry deadlines/checkpoints,
resource exclusion across job kinds, separate expiry recovery, indexed lookup,
and rollback when a caller swallows an injected attempt-write failure.

The first full backend run exhausted disk space while creating fixture databases.
Closed compiler/test scratch was removed after checking for open handles. The
unused schema-31 rehearsal was compressed from 8,975,618,048 to 1,940,025,791
bytes, and schema 49 from 18,794,569,728 to 4,119,869,771 bytes. Each decompressed
SHA-256 matched before its redundant raw file was removed; the archives, hashes
and prior reconciliation receipts remain retained. The current schema-50 copy
and original frozen inputs remain unchanged. Cleanup/validation records are in
`.local/native-enrichment-work-rehearsal-20261002/` under `selected_claim_*` and
`selected-claim-scratch-cleanup.json`.

The complete backend gate then passed in 632.150 seconds, with zero lint issues:
API tests took 424.623 seconds, ingestion 501.975 seconds and SQLite 627.888
seconds. The preceding `a775480d8` checkpoint also passed CI tests, generation,
lint, build and release. Production and `develop` remain unchanged; the complete
transition goal is still active.

## Validated compact enrichment transcripts

The native archive package now validates and reconstructs the metadata
collector's compact checkpoints. Shared base fields, explicit removals, nested
parent context, large numeric IDs and original observation timestamps retain
their semantics. Strict envelope/reference validation rejects malformed,
duplicate and unretained evidence. Resumed checkpoints cannot rewrite earlier
observations or silently discard pending or unresolved children.

Producer and server both enforce the 32 MiB compact limit, 4 MiB per expanded
record and 128 MiB aggregate expansion limit, with bounded record/reference
counts and parent depth. Producer size accounting includes native Unicode
separator escaping; rejected appends leave the prior checkpoint intact.

All archive tests passed in 0.580 seconds. All 296 producer tests passed in
30.097 seconds, and the repository Go lint gate reported zero issues. A shared
Go/Python fixture covers reconstruction and resume, with additional malformed
input, timestamp, retention and compact-amplification cases. Validation logs and
reports are retained under `.local/native-enrichment-work-rehearsal-20261002/`
with the `transcript_` prefix. The preceding `6dc7d53ca` checkpoint passed CI
generation, tests, lint, build, release and native preview image publication.

This adds no schema migration, producer endpoint or execution permission.
Target/job binding, producer-owned attempts, checkpoint persistence, verified
capture publication, cooldown/fairness coordination and legacy enrichment queue
mapping remain next. The remaining plan phases and production deployment are
unchanged.

## Producer-owned enrichment jobs and durable checkpoints

Schema 1000051 adds `post.enrich` jobs bound to exact target and collection
revisions, logical roots, worker policy digests and extractor versions. Every
attempt retains its authenticated producer. The internal coordinator authorizes
historical scope and checks current eligibility, credentials and lease deadlines
through commit. A replacement worker preserves the original producer of earlier
observations. Lost claim/checkpoint responses replay without transferring
ownership or replacing later evidence.

Each job stores one current compact transcript, small immutable acknowledgements
and per-record hashes/provenance. Receipt, body, accounting and progress writes
are atomic. Admission, attempts, checkpoint revisions and total staging bytes are
bounded; failed/cancelled evidence is retained. Target schedule changes cancel
active jobs atomically. Explicit application retry preserves backoff and creates
a new target revision; ordinary submissions cannot release held or exhausted
work. Automatic retries and expired-lease recovery impose exponential backoff.

Focused archive/SQLite tests passed in 23.516 seconds, and concurrency/failover
tests passed under the race detector in 43.266 seconds. The complete backend
gate passed in 700.867 seconds, with API tests in 457.182 seconds, ingestion in
531.981 seconds and SQLite in 674.160 seconds. Final lint reported zero issues.
Coverage includes producer ownership and token rotation, historical root scope,
late transaction expiry, swallowed write failures, storage limits, retry delays,
backup/reopen, anonymisation and corrupted-state refusal. Migration fixtures
preserve all three prior job kinds and attempts across states and roll back on
an unrelated table-name collision.

The schema-50 copy took 48.262 seconds and 18,794,643,456 bytes. Initial validation
under concurrent regression load took 578.193 seconds, followed by migration in
177.350 seconds. The migration harness completed and retained its report; its
outer runner then failed on a duplicate report filename. That reporting collision
is recorded separately, without replacing evidence or rerunning the migration.
Independent reconciliation passed in 256.292 seconds: all 206 prior data tables
match row for row with their SQLite types, existing schema objects differ only
by the added job kind, and prior migration history/sequences are intact. Five new
tables are empty, the storage singleton is zero, `quick_check` is `ok`, and there
are no foreign-key violations. The resulting copy is 18,796,433,408 bytes.

Normal reopening passed in 127.352 seconds with a retained CPU profile. This is
integrity/startup evidence, not a completed release performance budget. Reports,
logs and the migrated copy are in
`.local/native-enrichment-jobs-rehearsal-20261002/`. The unused schema-33 rehearsal
was compressed from 9,898,385,408 to 2,147,424,901 bytes; decompressed SHA-256
matched before its redundant raw file was removed. Its archive, proof and earlier
reconciliation remain retained. Original frozen inputs and the schema-50 baseline
are unchanged. The preceding `2bd35b07d` checkpoint passed CI build, lint and
native preview publication.

The new coordinator has no public producer route or dispatch worker yet.
Checkpoints do not publish captures or complete targets. Verified publication,
shared source cooldowns/fairness, stale-job maintenance, checkpoint cleanup after
publication, legacy queue mapping and remaining history/policy migration are
still required. Native UI, live host/n8n conversion, compatibility removal,
backup/restore/export, performance and production cutover remain unfinished.
Production and `develop` are unchanged; the full transition goal remains active.

## Verified enrichment capture publication

Schema 1000052 adds publication receipts and indexed record-to-capture
associations. The internal coordinator consumes an exact saved checkpoint,
rejects pending child lookups and verifies every reconstructed source identifier
against the existing target post. It preserves the original observing producer
and timestamp across worker handoff. Equal observations within one job share a
capture even when the transcript repeats them as parent context. Unresolved
external references remain retained limitations rather than successful fetches.

Publication reuses the download capture services for shared metadata, source
provenance, publisher matching/review, attachment manifests and translation
scheduling. Those writes commit together with record associations, target
completion and the successful job/attempt result. Generic success cannot bypass
native evidence. Expiry or a source edit before commit rolls back all effects;
lost completion responses replay without rerunning normalization or policy
scheduling. No media files or selected scene/image fields change.

Focused archive, SQLite and capture tests passed in 34.387 seconds. Tests cover
worker handoff, original attribution, duplicate context sharing, pending child
refusal, wrong-post rollback, translation coalescing, injected publication errors,
late expiry/source changes, concurrent replay, backup/reopen, anonymisation and
corrupted associations that still satisfy foreign keys. The race run passed in
35.432 seconds. Migration fixtures preserve staged work, roll back on a name
collision and refuse unverifiable schema-51 success assertions.

The complete backend gate passed in 750.601 seconds with zero lint issues:
API tests took 510.348 seconds, ingestion 577.351 seconds and SQLite 723.973
seconds. The preceding `f71a7bfd9` checkpoint passed CI build, lint and native
preview image publication.

An isolated schema-51 backup took 218.584 seconds under regression load. Initial
validation took 127.221 seconds and migration 127.398 seconds. Independent
reconciliation passed in 206.577 seconds: all 212 prior data tables match row for
row, including SQLite value types; all existing schema definitions, sequences
and prior migration history are intact. The two new tables are empty,
`quick_check` is `ok`, and foreign-key violations are zero. The copy remains
18,796,433,408 bytes. Normal reopening passed in 128.674 seconds, with its CPU
profile retained; release startup performance remains unfinished.

Reports, logs and the migrated copy are under
`.local/native-enrichment-publication-rehearsal-20261002/`. The superseded schema-50
rehearsal was compressed from 18,794,643,456 to 4,119,872,195 bytes. Its decompressed
SHA-256 matched before the redundant raw copy was removed; the archive, hash proof
and earlier reconciliation remain retained. The schema-51 baseline and original
frozen inputs are unchanged.

Public producer routes and dispatch are not enabled. Verified checkpoint-body
cleanup, shared cooldown/fairness, stale-job maintenance, additional post identity
adapters and legacy queue mapping remain required. Native UI, live host/n8n
activation, remaining policy/history migration, compatibility removal,
backup/restore/export, performance and cutover remain unfinished. Production and
`develop` are unchanged; the complete transition goal remains active.

## Verified release of completed enrichment staging

Schema 1000053 releases completed enrichment staging after verifying its native
publication. The internal coordinator commits the release with captures and
job/target completion; failed or cancelled work keeps its staging. Original
acknowledgement hashes, observation/producer associations, native payloads and
unresolved URLs with their parent/depth/reason remain retained. A fixed, versioned
checksum projection binds that evidence without duplicating full source bodies.

Earlier publications keep their staging through migration. Scoped cleanup can
release them after verification, without a new attempt. Lost checkpoint and
completion acknowledgements still replay after cleanup, expiry, source edits,
worker handoff and restore. Startup verifies either the original body or a release
proof and validates native payloads, profiles and collection associations before
opening a writer. Cleanup failure rolls back publication/storage accounting;
swallowing a repository write failure cannot commit a partial release.

The complete backend gate passed in 687.942 seconds with zero lint issues.
Final focused enrichment/capture tests passed in 107.643 seconds after freezing
the checksum projection independently of API structs: SQLite took 103.516 seconds
and ingestion 47.064 seconds under concurrent regression load. Cleanup and
concurrent publication also passed under the race detector in 64.919 seconds;
final lint reported zero issues. Coverage includes old-publication migration,
name-collision rollback, retained failed/cancelled evidence, cleanup failure,
swallowed errors, original-producer acknowledgement replay, backup/reopen,
anonymisation, and corruption that leaves foreign keys valid. The preceding
`ee4b68260` checkpoint passed CI build, lint and native preview image publication.

An isolated schema-52 backup took 38.201 seconds. Initial validation took
134.598 seconds, migration 129.497 seconds and reinitialization 0.134 seconds.
Independent reconciliation passed in 221.153 seconds: all 214 prior data tables
match row for row with their SQLite types; previous schema definitions,
sequences and migration history are preserved. The new release table is empty,
`quick_check` is `ok`, and there are no foreign-key violations. The copy remains
18,796,433,408 bytes. Normal reopening passed in 132.071 seconds with a retained
CPU profile; startup performance budgets remain unfinished.

Reports, logs and the migrated copy are under
`.local/native-enrichment-release-rehearsal-20261002/`. The superseded schema-51
rehearsal was compressed from 18,796,433,408 to 4,120,174,179 bytes, and schema 32
from 8,975,687,680 to 1,940,027,833 bytes. Each decompressed SHA-256 matched before
its redundant raw copy was removed. Both archives, hash proofs and earlier
reconciliations remain retained. The schema-52 baseline and original frozen
inputs are unchanged.

Public producer routes/dispatch, additional post identity adapters, shared source
cooldowns/fairness, stale-job maintenance, legacy queue mapping and remaining
policy/history migration are still required. Native UI, live host/n8n activation,
compatibility removal, backup/restore/export, performance and cutover remain
unfinished. Production and `develop` are unchanged; the full goal remains active.

## Additional source post adapters

Native capture intake and internal enrichment publication now identify Bluesky,
TikTok, Instagram posts/reels, Patreon, Fansly and Kemono/Coomer posts. The Go
server and Python producer share 51 identity/metadata cases. Bluesky identities
include the author's DID; mirror identities include extractor, service and
account. Instagram uses the enclosing post ID rather than an individual file ID.
Disagreeing, incomplete and ambiguous identifiers cannot establish a post.

Metadata uses each extractor's post caption and publication fields. Image alt
text and attachment dates do not replace them. Mirror import dates are not
misreported as publication dates. Current Fansly `account` envelopes and older
retained publisher envelopes both work. Imgur/Redgifs children preserve the
enclosing post's identity, publisher, caption and source evidence paths; social
profile/feed parents do not replace the actual post. Capture provenance retains
the actual mirror extractor independently of its qualified post identity.

All 304 producer tests passed in 100.021 seconds, including offline execution of
the pinned gallery-dl transformations for all seven additional source types.
Backend persistence tests cover original provenance, child attribution, mirror
account separation, collection authority, restart, staging release and exact
publication/receipt replay. Focused archive/enrichment/capture checks passed in
74.824 seconds. Both complete backend gates passed: the final run took 664.086
seconds. The subsequent API capability check passed in 13.761 seconds, and final
lint reported zero issues. Capabilities now list the additional native namespaces
and distinguish Coomer/Kemono namespace prefixes from native service support.
The preceding `7cc3e5b4a` checkpoint passed CI build, lint and native preview image
publication.

A read-only audit of the schema-53 rehearsal reconstructed and verified 17,694
retained captures across Bluesky, Instagram and the mirrored services. It matched
17,681 to their existing native post identifiers, with no conflicting identifiers
or newly required associations. Thirteen Instagram story/highlight captures
remain outside the regular-post adapter; their stored evidence is preserved.
The audit took 7.828 seconds. Existing collection namespace scopes also agree
with the newly supported identities. Reports, exceptions and test logs are under
`.local/native-post-adapters-rehearsal-20261002/`. No schema migration or source
payload rewrite was needed.

Attachment manifests, file selection and source-window adapters beyond
Reddit/Twitter, additional service-specific post/profile partitioning, and
Instagram story/highlight semantics remain required. Post identity support alone
does not enable a complete media download adapter. Public enrichment routes and
dispatch, shared scheduling/cooldowns, operational-history/policy import, native
UI, live host/n8n activation, compatibility removal, backup/restore/export,
performance and cutover remain unfinished. The full transition goal remains
active on `v3-rewrite`; production and `develop` are unchanged.

## Scoped enrichment worker transport

The producer API now exposes revision-pinned enrichment admission, inspection,
claim/renew, compact checkpoint transfer, verified publication and release
receipts. Authentication precedes large request decoding. Domain transactions
recheck historical collection/root grants, current producer ownership and lease
eligibility. Bounded target discovery returns unadmitted work for one selected
collection; it does not claim or discover already queued retries.

Controlled failure outcomes use the existing immutable attempt records.
Temporary errors retain checkpoints and receive server backoff; the eighth
attempt becomes terminal. The same producer can replay an exact failure receipt
after a successor starts without affecting that successor. Credential rotation
preserves producer ownership, while revoked or unrelated credentials cannot
confirm the old attempt. An expired attempt cannot commit a late failure.
Only verified publication can report success or complete the target.

The Python transport validates job bindings, checkpoint hashes/counts and
publication receipts. Its heartbeat uses the HTTP server date and monotonic
request start, and stops further extraction if renewal fails. Checkpoints remain
JSON objects with bounded responses and native Unicode encoding. Original
decimal tokens, exponent spelling, negative zero and large IDs survive HTTP,
resume cloning and extraction subprocesses without changing previous record
hashes. Ordinary event encoding and existing receipt digests are unchanged.

All 318 producer tests passed in 38.081 seconds. The complete backend gate passed
in 726.519 seconds: API tests took 487.998 seconds, ingestion 528.752 seconds and
SQLite 704.202 seconds. Final lint reported zero issues. Focused race checks
passed in 69.958 seconds, covering concurrent failure/publication replay and
real HTTP/Python recovery. Final focused worker/HTTP tests passed in 41.572
seconds after adjusting the race fixture to the actual server clock and the
supported 30-second request timeout. Final client validation checks also passed.

Coverage includes authentication before body reads, collection/producer
isolation, invalid and oversized requests, retry budgets, credential rotation,
checkpoint retention/reopen, stale failure replay, pending-child refusal, and
lost admission/claim/checkpoint/publication replies. The real transport fixture
round-trips a checkpoint larger than 4 MiB with HTML characters, Unicode
separators, exact source decimals and original provenance. Logs and next-worker
notes are under `.local/native-enrichment-worker-rehearsal-20261002/`. The preceding
`fad491032` checkpoint passed CI build, lint and native preview image publication.

No native schema migration was needed; the database version remains 1000053.
Durable local execution/staging, queued-job dispatch, shared source cooldowns
and fairness, stale-job recovery, reviewed metadata-only profiles and legacy
queue mapping remain unfinished. Other operational-history/policy migration,
additional download adapters, native UI, live host/n8n activation, compatibility
removal, backup/restore/export, performance and production cutover still remain.
Production and `develop` are unchanged; the full transition goal remains active.

## Durable selected-job enrichment execution

Producer outbox schema 8 adds a local journal for immutable job definitions,
stable claim requests, returned checkpoint bytes and pending delivery intents.
The executor reserves a full checkpoint allowance against the existing outbox
quota before requesting source data. Downloads and enrichment share that byte
budget. A separate process lock allows one enrichment executor per outbox while
ordinary event delivery continues through short database transactions.

Returned metadata is persisted before a late ownership check can pause execution.
A matching checkpoint acknowledgement releases the local body and atomically
stages publication or the precise pending-child failure. Lost responses replay
the original operations after restart. Expired attempts can deliver unchanged
local evidence through a new claim, subject to the native prefix/revision checks;
divergent successor checkpoints leave the original local body in review. Native
publication is the only completion proof. Review or rejection cannot silently
discard unacknowledged observations.

Reviewed metadata-only profiles bind the source category, pinned runtime,
projected access/pacing configuration and asset identities. They require no media
root, destination locks or download archive. Website credentials remain local
references and stay out of the job, journal and portable policy digest. New CLI
commands validate profiles, execute selected jobs, recover pending deliveries
without website credentials, and inspect retained local state. The executor
requires an already admitted native job UUID.

All 328 producer tests passed in 36.315 seconds, including existing download and
configuration behavior. Focused native HTTP/Python restart tests passed in 27.105
seconds, and the same scenarios passed under the race detector in 35.438 seconds.
Coverage includes lost claim/checkpoint/publication and
failure responses, child-only continuation, ownership loss after extraction,
expired attempts, conflicting successor evidence, rejected checkpoints,
capacity exhaustion before fetch, and no false completion. Local journal tests
also cover process death, concurrent ownership, tampered bytes, receipt mismatch
and preservation/rollback across schema 7 → 8.

The complete backend gate passed in 693.046 seconds with zero lint issues:
API tests took 542.951 seconds, ingestion 519.655 seconds and SQLite 677.023
seconds. The initial sandboxed gate could not access the existing Go cache;
the successful run used the existing cache with permission. The preceding
`7a5b1d19a` checkpoint passed CI build, lint and native preview publication.
Reports and next-dispatch notes are in
`.local/native-enrichment-execution-rehearsal-20261003/`.

Native Stash schema remains 1000053; this increment changes only producer storage.
Queued-job discovery/dispatch, shared source cooldowns/fairness, stale-job
maintenance, review resolution and legacy queue mapping remain required before
enrichment service activation. Remaining historical policy/operational migration,
download adapters, native UI, host/n8n activation, compatibility removal,
backup/export/restore, performance and cutover remain unfinished. Production and
`develop` are unchanged; the full transition goal remains active.

## Scoped enrichment dispatch and native maintenance

The producer can now discover admitted enrichment jobs before admitting fresh
work for an explicitly selected collection. Native discovery uses the existing
partial active-job index, bounded by the 64-job limit, with current source,
policy/runtime and producer-grant checks. Unadmitted target pages preserve
priority/deadline/UUID order. Claim remains the only authority to contact a source.

Producer schema 9 persists separate delivery, local-job, native-job and target
cursors with outage/idle backoff. Selection is saved before network work; lost
admission replies are rediscovered from the native queue. Unsupported URLs do
not stall later pages. Pending delivery precedes new lookups and can run without
a website profile. Expired delivery reports an ownership requirement, or reclaims
with the matching reviewed profile and resends the original bytes. A mismatched
profile leaves the evidence intact. An idle pass is not collection completion.

The HTTP server owns a maintenance loop independent of media/translation workers.
It cancels stale source/target jobs and recovers expired attempts with native
backoff and attempt limits. Checkpoints and receipt history remain retained.
Maintenance stops before database shutdown, makes no website requests and does
not enable held targets. Its writes are atomic even if a caller catches a
repository failure. Existing generic job recovery shares the same expiry logic.

All 339 producer tests passed in 43.410 seconds. Focused native discovery and
maintenance tests passed in 2.736 seconds; actual HTTP/CLI restart and runtime
lifecycle tests passed in 9.221 seconds. The focused race run passed: SQLite took
5.985 seconds and API 17.400 seconds. Tests cover concurrent maintenance, source
changes, backoff, stale-lease refusal, retained checkpoints, rollback after a
caught write failure, lost admission/checkpoint/publication responses, original
metadata reuse after expiry, cursor contention, target pagination, nanosecond
ordering, profile isolation and populated outbox migration/collision rollback.
A historical migration fixture now initializes its own configuration instead of
depending on earlier tests.

The full backend gate passed in 742.076 seconds with zero lint issues. API tests
took 599.056 seconds, ingestion 545.426 seconds and SQLite 715.610 seconds.
A read-only query-plan audit on the schema-53 rehearsal selected
`archive_jobs_active_work`; that copy has zero active
enrichment jobs, so this proves index selection rather than loaded performance.
No native migration was required; schema remains 1000053. Reports, logs and
next-source-scheduling notes are under
`.local/native-enrichment-dispatch-rehearsal-20261003/`. The preceding `9aa831a60`
checkpoint passed CI build, lint and native preview image publication.

Shared download/enrichment pacing and fairness remain required before production
service activation: source_runs and native post-enrichment jobs still have
separate ownership coordination. Legacy enrichment queue mapping, remaining
policy/operational history import, additional download adapters, review UI,
host/n8n conversion, compatibility removal, coordinated backup/export/restore,
performance and cutover remain unfinished. Production and `develop` are unchanged;
the complete transition goal remains active.


## Shared native pacing and enrichment service reservations

Schema 1000054 binds immutable source runs and enrichment jobs to the contacted
service, independently of performer/account ownership. Versioned host rules
recognize service aliases, preserve distinct unknown hosts and keep mirror
services separate from upstream creator namespaces. Claims coordinate cooldowns,
collection ownership and service reservations in their write transaction.
Eligible queued downloads precede enrichment; independent downloads retain their
existing destination/target locks. Blocked enrichment claims no longer cause
successive dispatch passes to keep admitting fresh work.

Each enrichment attempt retains its own service reservations. Claims reserve
known pending child services atomically; a scoped producer route reserves newly
discovered Redgifs/Imgur services before extractor initialization. Its replies are
bound to the current job/fence. A busy child is checkpointed without a request,
retaining its parent for child-only retry. The bounded subprocess exchange keeps
website access settings local and continues preserving native JSON number tokens.
Finished, cancelled or expired attempts no longer hold their services, while
attempt history and checkpoints remain intact.

Native typed rate-limit, timeout and extraction failures pause the affected
service for at least one hour; authentication/challenges use one day. A longer
retry deadline wins. Child failures pause the child service, and old failure
receipts cannot prolong a cooldown. Busy reservations, missing posts, denied
accounts and local worker failures do not declare a service outage. The generic
download adapter still needs typed source-failure reporting and equivalent
reservations for its own linked extractors; its existing scheduling binding
covers the source run's target service. These remain activation requirements.

All 344 producer tests passed in 46.863 seconds. Focused source/enrichment tests
passed, including real Python/native HTTP execution and recovery. Race checks
passed for service identity, concurrent ownership and the HTTP lease boundary
(SQLite 26.650 seconds, API 27.079 seconds). Coverage includes child-only retry,
reservation replay, producer/fence rejection, expired ownership, shared service
cooldowns, independent mirrors, caught-write rollback and historical migration
preservation/collision rollback. The full backend gate passed in 816.074 seconds
with zero lint issues (API 650.095, ingestion 587.493, SQLite 793.301 seconds).

The 18,796,433,408-byte isolated schema-53 copy migrated to schema 1000054.
Initial validation took 202.518 seconds and migration/revalidation 163.429 seconds
while other checks ran; these are not production downtime estimates. Independent
row/type comparisons and digests matched all 215 retained tables (34,810,754 rows),
with zero foreign-key violations and successful integrity checking. Three source
runs gained bindings to two services; this frozen copy has no admitted enrichment
jobs. Populated fixtures cover running attempts and pending children. Query plans
use the intended active-work indexes; the sparse copy is not a load benchmark.

Reports and scripts are under `.local/native-source-pacing-rehearsal-20261003/`.
The comparison passed before deleting the superseded schema-53 copy, reclaiming
another 18.8 GB. The original compatible snapshot, frozen migration inputs and
latest verified native copy remain, with the 50 GiB host reserve checked before
and during copying. No production service or library was migrated.

Download-side linked-service reservations and typed failures, cross-collection
fairness, legacy enrichment queue/cooldown/history import and live conversion
remain. The broader policy/operational migration, adapters, native UI,
compatibility removal, coordinated backup/export/restore, performance and cutover
requirements are still unfinished. The complete transition goal remains active.

## Download linked services and failure attribution

Schema 1000055 adds attempt-scoped requested/held services and immutable typed
failure receipts. The source-run API authenticates the producer, checks the
current owner/fence, and rechecks the definition and lease through commit.
Download extractors reserve their root or supported Redgifs/Imgur child service
before initialization. Busy requests remain durable dependencies without holding
the service. Exact-window retries, including expired attempts, wait for their
recorded dependencies before repeating parent extraction and reserve them in the
claim transaction. Widened traversals discover their own dependency set.

The pinned download adapter now reports controlled source error codes and the
contacted service, without retaining website exception text or URLs. Source
rate-limit and timeout errors stop immediate HTTP retry loops. Child errors
affect the child's cooldown; individual media download failures and intentional
archive stops retain their separate meanings. Lost ownership stops further work.
Lost finish acknowledgements require the exact attempt, outcome, code and service.
The executor requires `source_run_pacing_protocol: 1`.

All 352 producer tests passed in 31.827 seconds. Focused backend checks passed,
covering competing child reservations, authorization/fences, release, typed
failure attribution, busy dependencies across retry/expiry, widened windows,
caught-write rollback and historical migration preservation/collision handling.
Race checks passed (SQLite 44.525 seconds, API 10.296 seconds). The full backend
gate passed in 848.292 seconds with zero lint issues (API 696.224 seconds,
ingestion 636.398 seconds, SQLite 819.788 seconds).

The isolated 18,796,433,408-byte schema-54 copy migrated to schema 1000055 without
increasing its file size. Initial validation took 149.260 seconds and
migration/revalidation 144.357 seconds while other checks ran. Independent
row/type comparisons and digests matched all 219 retained tables (34,810,759 rows)
in 408.890 seconds; integrity checking passed with zero foreign-key violations.
The frozen copy has three source runs but no attempts, so populated fixtures
provide the historical/running-attempt migration coverage. Query plans use the
active-run and attempt-key indexes. These timings and sparse query plans are
not production downtime or loaded-performance estimates.

Reports and scripts are under `.local/native-download-pacing-rehearsal-20261003/`.
The verified schema-55 copy replaces the disposable schema-54 copy under the
50 GiB free-space policy; the original compatible snapshot and frozen migration
inputs remain. The preceding `484533461` checkpoint passed CI build, lint and
preview-image publication. Production and `develop` remain unchanged.

Cross-collection fairness, legacy queue/cooldown/history import, remaining policy
and operational migration, additional download adapters, native UI, host/n8n
conversion, compatibility removal, coordinated backup/export/restore, performance
and production cutover remain unfinished. The full transition goal stays active.

## Bounded download preference and cooperative turns

Schema 1000056 records actual waiting enrichment claims with a 90-second expiry
and per-service scheduling counters. Four download starts or two minutes of
continuously live waiting make an enrichment turn due. The oldest eligible
requester wins across collections, while existing downloads drain. An enrichment
start resets the affected counters and waiting-age budget. Abandoned requests,
held/stale targets and cooling dependencies cannot indefinitely reserve unrelated
services. Claim authority is checked through commit even when the only write is
the waiting hint. Replay does not count another start.

The download API exposes a five-minute `turn_until` derived from the attempt's
recorded start. The worker finishes the current file/checkpoint before yielding,
and heartbeats cannot extend the budget. A resumed traversal may finish replay
and one new checkpoint first, preventing a long saved prefix from trapping it
in repeated replay. This is cooperative scheduling, not a hard timeout. Yield
preserves pending windows and progress, applies the ordinary target cooldown and
does not consume failures or failure backoff. The updated executor requires
`source_run_fairness_protocol: 1` and reports an acknowledged turn as `yielded`.

All 356 producer tests passed in 35.764 seconds. Focused checks passed in 13.880
seconds, including competing claims from different collections, oldest-request
selection, abandoned-interest expiry, held targets, child cooldowns, exact-window
progress, authorization at blocked-claim commit, rollback and restart validation.
Race checks passed in 51.580 seconds overall (SQLite 23.487, API 14.791 seconds).
The full backend gate passed in 814.504 seconds with zero lint issues (API
666.543, ingestion 611.419, SQLite 792.532 seconds).

The isolated 18,796,433,408-byte schema-55 copy migrated to schema 1000056 without
growing its file. Initial validation took 155.899 seconds and migration/revalidation
152.656 seconds while the backend checks ran. Independent row/type comparisons
and digests matched all 221 retained tables (34,810,759 rows) in 282.396 seconds,
with unchanged prior schema objects, append-only migration history, successful
integrity checking and zero foreign-key violations. Two existing service scopes
gain zeroed counters; no historical waiting requests are invented. Populated
fixtures cover real attempts and waiting jobs. Service/expiry lookups use their
indexes; this sparse rehearsal is not a loaded performance benchmark.

Evidence is under `.local/native-source-fairness-rehearsal-20261003/`. The verified
copy replaces the disposable schema-55 rehearsal under the 50 GiB host reserve.
The original compatible snapshot and frozen migration inputs remain. The
preceding `8c69a3fc1` checkpoint passed CI build, lint and preview-image publication.
Production and `develop` remain unchanged.

Dispatch across multiple profiles, legacy queue/cooldown/history import, remaining
policy and operational migration, additional download adapters, native UI,
host/n8n conversion, compatibility removal, coordinated backup/export/restore,
performance and cutover remain unfinished. The complete transition goal is active.

## Worker profile rotation and automatic collection discovery

`stash-ingest dispatch-all --profiles FILE` now rotates across a bounded local
list of download and metadata profiles. Profile selection is persisted before
execution so a restart or busy first profile cannot repeatedly displace its
peers. It first attempts saved metadata delivery without loading website
profiles; a missing access binding therefore cannot strand already fetched
evidence. Unavailable profiles report a fixed code and allow other entries to
run. Native receipts, permissions, leases and shared service scheduling remain
authoritative.

The scoped enrichment API now discovers eligible collections automatically,
including later collections under an existing root grant. Exact collection
grants remain tied to their current root, including unbound collections. Current
grants apply before pagination, and moving a collection does not expose it under
an old grant. Ready collections contain either eligible unadmitted targets or
due queued jobs for the requested policy/runtime. Discovery is read-only.
Metadata profiles retain independent collection rotation and discovery backoff;
the existing collection dispatcher still validates URLs, admissions and claims.

Producer outbox schema 10 adds two cursor tables in one transaction, preserving
all earlier rows, receipts and staged bodies. The native database stays at
1000056. Fixtures verify schema-9 preservation and collision rollback, profile
and collection rotation across restart, competing cursor updates, failure/backoff
handling, current grants, moved sources and job-state boundaries. The actual
Python CLI/native HTTP test automatically discovers its collection, survives a
lost checkpoint response and delivers after restart with the website profile
unavailable; the source is fetched exactly once.

All 368 producer tests passed in 42.409 seconds. Focused checks passed in 16.354
seconds, actual HTTP/CLI checks in 14.109 seconds and race checks in 50.201 seconds
overall (SQLite 6.320, API 12.815 seconds). The full backend gate passed in
752.243 seconds with zero lint issues (API 608.602, ingestion 541.824, SQLite
728.180 seconds).

Evidence is under `.local/native-worker-profiles-rehearsal-20261003/`. A read-only
query check on the verified native copy uses the root, collection-state and job
target indexes. That copy has 2,612 collections but no enrichment targets; it
does not establish loaded performance. This increment needs no new full database
copy. The existing schema-56 copy and frozen inputs remain authoritative, and
135.5 GiB was free after validation under the 50 GiB host reserve. The preceding
`fa297624e` checkpoint passed CI build, lint and preview-image publication.

Legacy enrichment queue/cooldown/history import is next. Remaining policy and
operational migration, additional download adapters, native UI, live host/n8n
conversion, compatibility removal, coordinated backup/export/restore, performance
and cutover still apply. Production and `develop` remain unchanged, and the full
transition goal remains active.

## Legacy enrichment semantics and frozen-input reconciliation

Native preparation policy `automation-enrichment-v1` now interprets the legacy
queue, cooldowns, seed/source progress and catalog enrichment receipts. Retry
deadlines use exact decimal arithmetic and round upward to milliseconds;
platform/account pauses never shorten another preserved deadline. Original
attempt counts and creation/update times remain available without fabricating
native leases or attempts. Contacted services come from the URL, independently
of historical account-platform labels. Unknown versions, states, malformed
staging and unsupported cooldown reasons require review.

Preparation distinguishes held-work candidates, historical completion requiring
a catalog receipt, existing source metadata requiring a gallery-dl capture, and
coalescence requiring a post alias. It preserves explicit exclusions and leaves
missing URLs/identity conflicts reviewable. Staged metadata keeps its exact hash
and requires conversion/review even under a completed queue label. Completed
enumeration can still leave pending jobs; neither seed progress nor the last
source attempt certifies execution completion.

The read-only assessment checked all 252,050 frozen queue rows, eight cooldowns,
eight source-progress rows and all 16,186 retained catalog receipts with matching
source hashes. No frozen seed-progress rows exist; fixtures cover them. The final
pass took 4.894 seconds while validation ran. Queue candidates comprise 227,443
pending/retry holds, 21,368 historical completions, five existing-source outcomes,
44 coalescences, 2,443 exclusions and 747 review states. Every queue post and
catalog resolves to an imported native identity. These are preparation results,
not newly created targets or successful native jobs.

Exact queued URLs are absent for 213,826 held candidates and two historical
completions even though their posts match. The old selector rewrites Reddit and
Twitter URLs to short post routes, so the domain importer must retain those URL
associations with their original queue evidence instead of requiring exact text
already present in `source_post_urls`.

Of the 21,368 `done` rows, 16,186 have a valid exact-key catalog receipt. The other
5,182 have no receipt through their imported post/collection identities either;
all have queue updates newer than their individual catalog snapshots (5,180
OnlyFans-labeled mirror jobs and two Reddit jobs). They remain unproven by this
rehearsal input. This confirms the coordinated fresh-snapshot requirement at
cutover; it does not justify manufacturing missing receipts or silently repeating
that completed source work.

Focused tests cover status/proof distinctions, source mirrors, account-specific
cooldowns, deadline rounding below floating point precision, original staging
bytes, unknown input, receipt timestamps and unresolved children. The full backend
gate passed in 731.215 seconds with zero lint issues (API 583.246, ingestion
517.440, SQLite 704.689 seconds). Final timestamp/cooldown checks passed with the
complete scrape package (0.037 seconds), final lint reported zero issues, and the
full frozen projection was rechecked unchanged.

Evidence is under `.local/native-enrichment-import-preparation-20261003/`.
The native schema remains 1000056 and no database copy was added. Free space was
134.6 GiB after checks under the 50 GiB reserve. The preceding `346083b93`
checkpoint passed CI build, lint and preview-image publication.

Native queue/receipt/cooldown mapping, staged checkpoint conversion, reviewed
activation and coordinated cutover inputs still need implementation. The broader
policy/operational migration, download adapters, UI, host/n8n conversion,
compatibility removal, backup/export/restore, performance and cutover gates remain
unchanged. The complete transition goal remains active.


## Historical catalog enrichment receipt import

Schema 1000057 retains historical enrichment assertions in native receipt storage
with a manifest-bound import ledger and application inspection API. Batches of
50 records resume from the committed source ordinal, including after a lost HTTP
response. Original completion timestamps, version, enriched-link counts and
unresolved-child counts are retained. Each receipt binds the mapped post to the
catalog's migrated collection revision 1; later collection edits/retirement do
not rewrite that scope. Invalid or unsupported receipts, unmapped posts and
forgotten posts retain review outcomes and their source values.

Historical completion does not create a native job, attempt, target or capture,
and does not claim exhaustive child coverage or particular capture IDs. Imported
source provenance remains distinct by catalog. The new client is
`stash-import-catalog-enrichment`; native post/receipt readers provide bounded
inspection. Ordinary backup includes these tables and anonymisation removes
them. Startup checks progress, scope, deterministic identity, retained counts and
both historical completion and import timestamps.

Focused SQLite/API tests cover interrupted batches, stale checkpoints, retired
collection scope, exact times, unresolved children, forgotten posts, caught-write
rollback, migration collisions and startup rejection of altered receipt facts.
The real Python CLI/native HTTP fixture recovers a lost committed completion
response, inspects retained source values, handles absent legacy receipt tables
and rejects invalid requests. All 371 producer tests passed in 118.269 seconds.
The full backend gate passed in 823.868 seconds with zero lint
issues (API 677.598, ingestion 607.518, SQLite 796.750 seconds). The
final stricter import-time validation passed focused checks in 28.368 seconds,
final lint and a fresh full-database open in 131.571 seconds.

The isolated full-copy import mapped all 16,186 receipts across 1,697 frozen
catalogs, with zero review outcomes, in 33.482 seconds
and 2,003 bounded transactions. Every terminal cursor replay matched. Source
counts include 4,657 enriched attachment links and zero unresolved children;
fixtures separately preserve nonzero unresolved counts. This maps historical
assertions only and does not change the unresolved 5,182 later queue completions
whose catalog snapshots predate their proof.

Independent comparison took 275.379 seconds: all 224 pre-existing
tables, retained schema objects, prior migration history and sequences matched.
All 16,186 receipt identities, timestamps, counters and post/collection references
were independently checked against the frozen source rows. Integrity was clean
with zero foreign-key violations. Post, collection/post and receipt-source
lookups use their targeted indexes. The native copy is 18,809,282,560
bytes, about 12.3 MiB larger than its predecessor.

Evidence is under `.local/native-enrichment-history-rehearsal-20261003/`. After
validation, retiring the superseded schema-56 database and derived files reclaimed
18,796,515,328 bytes; 133.4 GiB was free
under the 50 GiB host reserve. The current pointer names the schema-57 copy;
frozen inputs and the original compatible snapshot remain. The preceding
`2f2d91453` checkpoint passed CI build, lint and preview-image publication.

Legacy queue/cooldown/progress mapping, staged checkpoint conversion and reviewed
activation are next. The broader policy/operational migration, download adapters,
native UI, host/n8n conversion, compatibility removal, backup/export/restore,
performance and coordinated cutover remain required. Production and `develop`
remain unchanged, and the complete transition goal is active.

## Frozen enrichment queue import

Schema 1000058 maps the received automation snapshot's enrichment jobs,
cooldowns, seed cursors and source progress into native import records. The
application API and `stash-import-automation-enrichment` client resume bounded
200-record/16 MiB transactions. Original source rows and staged bytes remain
available, and whole-migration completion remains `imported:false`.

Pending/retry work becomes held enrichment targets with original priority and
the later of its job delay and exact account/platform cooldown. Historical
attempt counts do not become invented native attempts. Old `done` status needs
a scoped catalog receipt; `already_native` needs a scoped existing gallery-dl
capture. Historical completion has its own proof basis and zero new capture
associations. Missing proof remains review. Coalesced work needs an explicit
catalog alias; exclusions remain retained. Legacy staging remains review rather
than being discarded to refetch or misrepresented as a native checkpoint.
Existing native choices are preserved; only unchanged holds owned by this import
can combine schedules or finish from historical proof.

The isolated full-copy pass processed all 252,066 operational rows in 1,261
batches and 342.218 seconds. It retained 227,443 held jobs, 16,186 receipt-backed
historical completions, five existing-capture completions, 44 proven coalescences,
2,443 exclusions, eight cooldowns and eight source-progress records. The 5,929
review outcomes comprise 747 original review states and 5,182 completion claims
whose catalog snapshots lack proof. No frozen seed rows exist; fixtures cover
their counters and cursor semantics. The native targets comprise 227,443 holds,
16,191 historical completions and two review targets; unsupported/missing URLs
retain their source decisions without fabricated execution targets. No native
job or pending target was activated.

The previous schema validation and SQL promotion took 771.488 seconds while the
full backend suite ran. Reopening the completed copy passed in 159.694 seconds.
The database is 19,482,345,472 bytes, 641.883 MiB larger than its predecessor.
These measurements do not establish the release's startup or cutover performance
budget. Source-ordinal, import-owned-target, cooldown and historical-proof
lookups use targeted indexes.

The full backend gate passed in 882.809 seconds with zero lint issues (API
729.864, ingestion 650.272 and SQLite 855.299 seconds). All 374 producer tests
passed in 66.285 seconds. Focused fixtures cover sparse cursor restart, exact
account cooldowns, preserved native exclusions, source aliases, staged metadata,
missing proof, forgotten posts, atomic rollback, backup/restore, anonymisation,
native-publication preservation through schema replacement, migration collision
rollback and startup rejection of altered source projections. A real Python
CLI/native HTTP test recovers a committed response being lost and inspects
original progress data without changing its frozen source.

Independent reconciliation took 483.063 seconds and compared 224 pre-existing
tables. All prior media, captures, post bodies, original input ledgers, native
execution state, migration history and sequences were preserved. The only
changes to existing post records are revision increments for 213,826 retained
queued-URL associations, each linked to its original queue row and explicit
snapshot-boundary provenance. All 252,066 operational rows and their post,
collection, deadline, alias and historical completion bindings were independently
checked. Native target/completion identities matched their documented inputs.
Integrity was clean with zero foreign-key violations.

Evidence is under `.local/native-automation-enrichment-rehearsal-20261003/`. The preceding
`3b9a9e1d7` checkpoint passed CI build, lint and native preview-image publication.

After reconciliation and checking that no process held the old files open,
retiring the schema-57 database and derived files reclaimed 18,809,323,520 bytes.
The current pointer now names the schema-58 copy; original compatible and frozen
input snapshots remain. Free space was 131.1 GiB under the 50 GiB host reserve.

Reviewed enrichment activation, staged checkpoint conversion, review resolution,
remaining policy/operational migration, additional download adapters, native UI,
host/n8n conversion, compatibility removal, backup/export/restore, performance
and coordinated cutover remain required. Production and `develop` remain on
their existing releases; the complete transition goal remains active.

## Reviewed enrichment activation and collection handoffs

Schema 1000059 introduces saved-preview activation through the native application
API and `stash-activate-automation-enrichment`. Original frozen holds are listed
once per target using indexed ordinal pages, including when legacy aliases share
a target/revision. Preview binds the original target, current post revision,
retained URL, chosen collection revision, release target, policy and schedule.
Apply rechecks the preview atomically, preserves priority and exact delays, and
retains receipts that replay after subsequent completion, edits or forgetting.
It does not start collectors or assert their completion.

Imported collections are disabled at revision 1. Reviewing and enabling a
collection creates a new revision; activation now explicitly carries the held
work to it. The old target remains excluded with `activation_rebound`, and the
new pending target retains the reviewed source scope. Existing native destination
work is preserved as a conflict. Disabled/retired collections, changed original
holds and forgotten posts require review. General application activation also
supports holds already bound to an active collection revision.

Translation and enrichment clients share bounded HTTP and immutable saved-plan
handling; translation plan format and receipt semantics are unchanged. Plans
validate every page before Apply, preserve operation UUIDs after response loss,
and distinguish activation from execution. Enrichment review candidates prevent
an empty eligible selection from claiming completion. A packaged-wheel smoke
check exercised both installed command entry points from the built artifact.

The full backend gate passed in 885.167 seconds with zero lint issues (API
727.260, ingestion 634.416, SQLite 867.252 seconds). All 379 producer tests passed
against the current source in 124.096 seconds. Focused SQLite/API fixtures cover
scope changes, occupied destinations, original alias deduplication, delayed work,
late-failure rollback, historical receipt replay, backup/anonymisation, migration
collision and corrupt-receipt rejection. The real Python/native HTTP fixture
imports 205 holds, reviews their collection, hands work to revision 2 and recovers
a committed batch response being lost while preserving its saved plan.

The full-copy rehearsal reviewed 815 imported collection definitions from
disabled revision 1 to active revision 2, then activated all 227,443 original
holds through the real Python CLI and native HTTP API in 2,275 batches. Lost
responses after the first batch and after 100,000 committed targets recovered
from their original receipts. Saved plan bytes remained unchanged across resume,
status and replay. This was confined to the disposable copy; no collector job,
source fetch or new capture was started.

Independent reconciliation passed in 968.249 seconds. All 225 unaffected tables,
original import ledgers, previous collection definitions, target history,
migration history and sequences match the baseline. Each original hold was
consumed exactly once, and each replacement has the reviewed collection revision,
retained URL, priority and exact deadline. All 16,193 other historical targets
remain unchanged. The report verifies all 2,275 receipts and their hashes,
227,443 replacement identities, and 454,886 new history rows. Integrity was
clean with zero foreign-key violations.

The database is 20,085,633,024 bytes, an increase of 575.340 MiB. The activation
workflow, including plan preparation, two response-loss recoveries, status and
replay, took 478.411 seconds. Opening the migrated copy took 158.286 seconds;
fresh startup after activation passed in 192.032 seconds. These measurements do
not satisfy the release's startup or cutover performance requirement. The
indexed candidate query returned 100 records in 0.001661 seconds.

Evidence is under `.local/native-enrichment-activation-rehearsal-20261003/`.
The schema-59 copy replaces its disposable predecessor under the documented
retention policy; original compatible and frozen input snapshots remain. Free
space stayed above 110 GiB while both copies were present, with the 50 GiB reserve
enforced for planned rehearsal writes. Production and `develop` are unchanged.
The preceding `c9c9be9af` checkpoint passed build, lint and preview-image
publication CI.

The full transition goal remains active. Staged checkpoint conversion, import
review resolution, remaining operational/policy families, additional download
adapters, native UI, host/n8n conversion, compatibility removal,
backup/export/restore, performance and coordinated cutover still require work.

## Retained legacy enrichment checkpoints

Schema 1000060 adds resumable conversion and application inspection of saved
legacy `enrichment_jobs.staged_json`. Each outcome binds the frozen manifest,
original source ordinal and source/staged hashes. Converted documents share
equal metadata bodies, optional shallow deltas and explicit parent references,
while retaining every ordered post/media record, pending child and unresolved
reference. Exact large JSON numbers, explicit nulls and the historical retention
policy are preserved. Equal converted documents are shared across source rows.
Malformed and unknown formats retain their original values with a review reason.

Legacy staging did not record observation times or owned producer attempts.
Conversion explicitly records observation time as unrecorded, retains the
reported extractor version separately, and leaves unscoped unresolved URLs
unscoped. It cannot impersonate a native worker transcript. Conversion does not
fetch sources, create captures, change target choices or release review holds;
reviewed handoff into native execution remains required.

`stash-import-enrichment-checkpoints` verifies frozen input and the exact number
of staged rows, sends bounded batches and resumes from committed source ordinals
after response loss. Application routes expose progress, paged summaries and
selected original/converted evidence. Completed replay is read-only. Native
startup validates deterministic conversion and source bindings, backup retains
the new evidence, and anonymisation removes it with its source records.

The full backend gate passed in 890.153 seconds with zero lint issues (API
731.068, ingestion 621.203, SQLite 872.698 seconds). All 382 producer tests passed
against current source in 36.094 seconds. Focused fixtures cover sparse cursors,
shared bodies with distinct original hashes, exact source reconstruction,
unsupported formats, caught late-failure rollback, schema collision, backup,
anonymisation and startup rejection of altered evidence. The real Python/native
HTTP fixture converts 205 staged rows in resumable batches, retains one malformed
row for review, and recovers a committed batch response being lost. A packaged
wheel smoke test exercised the new command and both related enrichment commands
from the built artifact.

The full-copy rehearsal used SQLite's online backup to include the baseline's
committed WAL data. Schema-60 opening/migration took 837.629 seconds under
concurrent backend checks. The real CLI then completed response-loss recovery,
resume and replay in 62.456 seconds. The frozen operational input contains zero
non-null staged values, so this rehearsal verifies the empty-input result and
whole-library preservation; populated conversion semantics are exercised by the
fixtures above. It does not claim that a fresh cutover snapshot will also have no
staging. No collector or provider execution started, and the migration-wide
`imported` flag remains false.

Independent reconciliation passed in 364.765 seconds. All 231 pre-existing
tables, original schema objects, earlier migration history and sequence counters
match the baseline. Captures, media, source bodies, held/released work and prior
activation receipts remain unchanged. The three new tables contain only the
verified empty-input progress receipt. Integrity is clean with zero foreign-key
violations. The verified database is 20,085,768,192 bytes; it replaces the
schema-59 disposable copy under the existing retention policy, while original
compatible and frozen source inputs remain available.

Fresh startup passed in 156.991 seconds. Startup and cutover performance remain
release requirements; these results do not close them. Evidence is under
`.local/native-enrichment-staging-rehearsal-20261003/`. The preceding `7535da1a7`
checkpoint passed CI build, lint and native preview-image publication.

The full transition remains active. Reviewed native execution of imported
staging, import review resolution, remaining operational/policy families,
additional download adapters, native UI, host/n8n conversion, compatibility
removal, coordinated backup/export/restore, performance and cutover still require
work. Production and `develop` remain unchanged.

## Unknown historical observation times

Schema 1000061 lets retained source evidence distinguish an unknown original
observation time from the time it entered the archive. Known captures keep their
original timestamp and signature, with a null recording time. Undated captures
have a null observation time and a required archive recording time under a
separate signature domain. Identical capture UUID replay cannot change that
meaning. JSON emits null for an unknown observation, and capture pagination
uses the applicable timestamp plus UUID through a dedicated index.

Undated publisher evidence requires review. An explicit account association is
retained without fabricating first/last-observed dates for identifiers. Existing
publisher choices remain selected. The normal producer boundary continues
requiring actual observation times. This is a prerequisite for imported staging
handoff; it does not itself turn a legacy checkpoint into an owned worker result,
release a held target or start a collector.

The full backend gate passed in 893.177 seconds with zero lint issues (API
715.939, ingestion 618.804, SQLite 874.963 seconds). Fixtures cover nullable JSON,
known/unknown capture paging through the index, exact replay, changed-time
rejection, direct SQL ambiguity guards, publisher review, backup, anonymisation
and startup rejection of altered evidence without writing to the database.
An actual schema-60 fixture migrates while retaining signed observations and a
noncontiguous rowid; a destination-name collision rolls back the table rebuild.
Other historical migration fixtures now copy their historical column sets
explicitly rather than assuming every later schema has identical columns.

SQLite online backup created one replacement rehearsal copy, including committed
WAL state. Normal opening, migration and reinitialisation passed in 704.150
seconds while the backend checks ran. No extra full database archive was retained.

Independent reconciliation passed in 381.105 seconds. All 234 existing data
tables retain their original values and types, including all 526,348 captures'
UUIDs, rowids, timestamps and signatures. The new recording-time column is null
for these known observations. Earlier migration history, sequence counters and
unrelated schema objects are unchanged. The new capture pagination index is
used; integrity is clean with zero foreign-key violations. The verified database
is 20,265,979,904 bytes. Undated behavior is covered by populated fixtures; no
unknown historical times were inferred or added to the frozen library.

Fresh startup passed in 162.056 seconds. Startup and cutover performance remain
release requirements. Evidence is under
`.local/native-capture-time-rehearsal-20261003/`. Both copies left about 138.6 GiB
free during the final checks, above the 50 GiB reserve. The verified copy replaces
its schema-60 predecessor under the existing retention policy; original compatible
and frozen source inputs remain available. The preceding `4a69a1e4b` checkpoint
passed build, lint and preview-image publication CI.

The full transition remains active. Reviewed execution of imported staging,
import review resolution, remaining operational/policy families, additional
download adapters, native UI, host/n8n conversion, compatibility removal,
coordinated backup/export/restore, performance and cutover still require work.
Production and `develop` remain unchanged.


## Reviewed checkpoint evidence acceptance — 2026-10-03

Schema 1000062 adds application preview, acceptance and receipt lookup for one
converted legacy checkpoint. It records shared native captures with unknown
observation times and the actual acceptance time, while preserving original
record slots, both parent references, exact numbers, pending children and
unscoped references. Duplicate source slots share a capture. It neither applies
a newer retention policy to old source bytes nor invents producer ownership,
missing extractor versions or source URLs.

A preview binds the frozen snapshot and manifest, original ordinal and target
revision, post revision, source hashes and reconstructed capture identities.
When a post only has a catalog-local identifier, its first service identifier is
an explicit reviewed change. Conflicting identifiers and another post's existing
claim are rejected. Acceptance, captures, optional identifier and historical
collection associations are atomic. Original review state, scheduling/history,
worker state, publisher decisions and scene/image metadata remain unchanged.
A lost response can be recovered by receipt UUID or exact replay after later
review decisions. This is evidence acceptance; reviewed child execution remains
to be implemented.

The preceding `c42b84d9e` build and lint CI passed, but its preview publication
failed during a migration test. A two-connection regression reproduced the cause:
SQLite could expose an old column count to the comparison helper before stepping
a statement after another connection rebuilt the table. Commit `983e9b4d8` pins
a read transaction and refreshes its schema before preparing the row comparison.
The new test failed before that fix and passed afterward; comparison assertions
were retained.

The full backend gate passed in 838.666 seconds with zero lint issues (API
666.155, ingestion 557.603, SQLite 819.757 seconds). Coverage includes shared
bodies and deltas, both parent references, large numeric IDs, missing historical
fields, invalid graphs and oversized payloads, conflicting post identities,
stale previews, caught late-write rollback, duplicate acceptance, backup/restore,
anonymisation and startup refusal of missing evidence bindings without writes.
The application API fixture imports frozen registry/catalog/automation inputs,
loses a committed acceptance response, recovers the receipt, and verifies that
the original target stays in review. A schema-61 fixture preserves staging and
captures across upgrade; a destination-table collision rolls back the migration.

One SQLite online backup produced the replacement copy. Normal opening,
migration and reinitialisation passed in 808.922 seconds. Independent comparison
passed in 437.223 seconds: all 234 existing data tables retain their exact values
and types, including all 526,348 capture UUIDs, rowids, observation/recording times
and signatures. Existing schema objects, prior migration history and sequence
counters remain unchanged. The two new evidence tables are empty, as expected:
the frozen operational snapshot has no saved legacy checkpoint bodies. Populated
acceptance and recovery behavior is demonstrated by fixtures, not inferred from
that empty historical input. Integrity is clean with zero foreign-key violations.
The database remains 20,265,979,904 bytes.

Fresh-process startup passed in 158.812 seconds. Startup/cutover performance
remains a release gate. Evidence is under
`.local/native-checkpoint-evidence-rehearsal-20261003/`. The large rehearsal was
assigned idle disk-I/O priority while other tests and services ran. Free space
stayed above 136 GiB with both database copies present. The verified replacement
supersedes schema 61 under the retention policy; original compatible and frozen
source inputs remain available.

The full transition remains active: reviewed staging execution, import review
resolution, remaining operational/policy families, additional download adapters,
native UI, host/n8n conversion, compatibility removal, coordinated backup/export/
restore, performance and final cutover are still required. Production and
`develop` remain unchanged.

## Retained context and child retry prerequisites — 2026-10-03

Schema 1000063 introduces signed capture context bindings. A newly observed
capture can retain exact older parent metadata while identifying the original
capture UUID at each parent path. Capture and bindings are atomic, and missing
or replaced bindings fail integrity checks even if another capture contains the
same text. The new retention policy applies current reduction to fresh child
fields and verifies older embedded parents against their original captures.
No existing capture, publisher decision or identifier date is rewritten.

Publisher assessment follows the original bound observation. Known dates remain
the parent's dates; unknown dates still require review and never become today's
first/last observed dates after an explicit link. Nested media-host context is
supported without confusing a feed owner with a post's actual author. Ordinary
captures without bindings take a fast path with no additional reconstruction.
Backup/restore and anonymisation include the new relationships.

The Go and Python collector contracts now support a retained-context prefix for
future reviewed child retries. It preserves accepted capture UUIDs, exact large
numbers, both parent branches and historical payload bytes without inventing
observation times, source URLs or producer identities. A retry can fetch only
saved children; it cannot append more historical captures, copy old fields as a
new delta, or attach unbound inline parents. The seed retains pending parent/depth
associations, deduplicates identical child work and leaves original unscoped
references in the frozen acceptance. Invalid/oversized or ambiguous graphs remain
review cases. Offline Go-to-Python-to-Go tests exercise the real collector, its
inherited source settings, child reservation and error recovery.

This is a prerequisite checkpoint, not an enabled execution handoff. Native job
admission, checkpoint storage/startup validation and publication still reject
this new execution format. Evidence acceptance keeps its review hold. The next
step must bind an application-reviewed handoff to the exact evidence, target,
post and active collection revisions; deliver its seed without a fabricated
lease/receipt; and connect worker/outbox recovery, publication and versioned
release proofs. Existing v1 argument, receipt and release hashes remain intact.
Context provenance also needs to be applied to newly created native child
publications through that versioned contract.

The full backend gate passed in 844.413 seconds with zero lint issues (API
667.981, ingestion 560.558, SQLite 826.582 seconds). All 387 producer tests passed
in 36.539 seconds. A subsequent small publisher fast path passed the relevant
publisher/context/recording-time tests (18.169 seconds) and a final clean lint
check. Tests cover unknown and known parent times, exact old payload retention,
changed bindings, missing parents, future parent clocks, unretained fresh fields,
caught late-write rollback, read-only startup refusal of deleted/rebound context,
backup/restore, anonymisation and schema-62 upgrade/collision rollback. The
migration comparison helper now orders WITHOUT ROWID tables by their primary
keys, while retaining the earlier schema-refresh fix.

An online backup took 43.852 seconds. Normal open, migration and reinitialisation
passed in 705.276 seconds. Fresh-process reopen passed in 242.653 seconds while
independent reconciliation was also running; this is not an isolated startup
benchmark, and startup performance remains a release gate. Independent
reconciliation passed in 506.582 seconds: all 236 existing data tables retain
exact values and types, including all 526,348 capture UUIDs, rowids, observation/
recording times and signatures. Existing schema objects, migration history and
sequence counters are unchanged. The new context table is empty, as expected for
these frozen inputs; populated context behavior is covered by the fixtures.
Integrity is clean with zero foreign-key violations, and the database remains
20,265,979,904 bytes. This verified replacement supersedes schema 62 under the
retention policy, subject to the final host-visible open-file check.

Obsolete paged review exports were retired after host-visible open-file checks,
retaining their small manifests. This reclaimed another 580,788,224 bytes without
removing frozen source inputs. With both large database copies present, free
space stayed above 135 GiB; the existing guard reserves 50 GiB plus estimated
peak work space. Evidence is under
`.local/native-checkpoint-handoff-rehearsal-20261003/`.

The full transition remains active. Reviewed execution, migration review,
remaining operational/policy families, additional adapters, native UI, host/n8n
conversion, compatibility removal, coordinated backup/export/restore, performance
and final owner-reviewed cutover are still outstanding. Production and `develop`
remain unchanged.

## Exact checkpoint handoff review — 2026-10-03

Schema 1000064 adds an application review receipt for child-only execution from
accepted legacy captures. Preview binds the acceptance and plan hash, original
target, current post revision, active destination collection revision, runtime
and configuration hash, capture policy and exact resume document hash/size/counts.
The destination target identity is explicit when a newer collection definition
requires a replacement. Requests cannot invent an acceptance or silently reuse a
preview after post, target, collection or runtime changes.

Receipt and seed lookup recover a committed review after a lost response or
later edits. The seed is reconstructed from frozen evidence, with original
capture identities, payloads, extractor versions and recording times verified.
The database stores only the small review plan. Original unscoped references
remain in the acceptance and are counted separately; null historical observation
times do not acquire an observing producer or a new timestamp. Backup/restore,
anonymisation and read-only startup verification include the review.

This increment does not consume the review, release the target or create a worker
job/checkpoint/lease. Native job arguments, checkpoint storage and publication
remain gated to their existing execution format. The next increment must bind
native admission to this receipt, deliver the exact seed through producer scope,
include saved child services in scheduling/fairness, preserve prefixes during
retry, publish retained and new observations with distinct provenance, and
version release proofs. New ordinary child captures also need bound context.

Targeted tests cover retained bodies and unknown clocks, stale post/target/
collection/runtime review, caught late-error rollback, missing/corrupt seed
bindings, backup/restore, anonymisation, and schema-63 upgrade/collision rollback.
The real HTTP fixture loses both committed evidence and handoff responses,
recovers their receipts, parses the seed through the Python collector contract,
and confirms that the original target remains in review. An initial broad run
found an invalid namespace in the new stale-post test fixture; correcting that
fixture passed all handoff tests with integration settings in 7.232 seconds.
The corrected full backend gate passed in 870.028 seconds with zero lint issues
(API 688.716, ingestion 579.586, SQLite 860.681 seconds).

One online backup took 40.931 seconds. Normal opening, migration and
reinitialisation passed in 1,058.161 seconds with idle disk-I/O priority during
other host work. Fresh-process opening passed in 560.044 seconds while independent
comparison and host work were running; this is not an isolated benchmark. The
independent comparison passed in 1,377.083 seconds: all 237 existing data tables
retain their exact values and types, including 526,348 capture UUIDs, rowids,
observation/recording times and signatures. Existing schema objects, prior
migration history and sequence counters are unchanged. The new handoff table is
empty, as expected for the frozen inputs; populated review behavior is covered
by the fixtures. Integrity is clean with zero foreign-key violations, and the
database remains 20,265,979,904 bytes. Startup/cutover performance remains a
release gate. Evidence is under
`.local/native-checkpoint-review-rehearsal-20261003/`. The verified replacement
supersedes schema 63 under the retention policy after the host-visible open-file
check; original compatible and frozen source snapshots remain available. Free
space stayed above 135 GiB with both rehearsal copies present.

The full transition remains active, including execution handoff, migration
review, remaining operational and policy families, adapters, native UI, host/n8n
conversion, compatibility removal, coordinated recovery/export, performance,
owner-reviewed cutover and retirement.

## Execute reviewed checkpoint handoffs — 2026-10-03

Schema 1000065 connects the reviewed seed to native admission, worker execution
and publication. Admission rechecks the exact reviewed post, target, collection,
root, runtime and policy, then atomically releases the planned target and creates
one job. The small binding tables contain original capture identities, digests
and pending service names. They do not duplicate payloads or invent a producer
attempt/checkpoint receipt. A repeated admission recovers the original job even
after completion or later collection edits.

New native jobs pin version-2 capture semantics. Existing native v1 jobs and
released proofs retain their original identities and checksums. The producer
uses the verified seed only before its first real checkpoint, and recovers saved
delivery before another fetch. Pending child services participate in pacing and
fairness before any checkpoint exists. A child-only failure with no saved child
failure evidence does not pause the parent service.

Publication reuses original retained captures, including unknown observation
times and extractor versions. It associates them with the destination collection
without treating them as fresh publisher/translation evidence. Fresh child
captures receive their real observation times and signed parent-capture bindings;
ordinary new child enrichment uses the same contract. Version-2 release proofs
bind the review and seed to original signatures, new captures and submitting
producer records. Original unscoped references remain in accepted evidence.
Startup, backup/restore and anonymisation include these relationships.

The real HTTP fixture drops committed admission, checkpoint and publication
responses, then recovers through three fresh Python worker processes. It fetches
the saved child once, never refetches the retained root, and finishes with a
single attempt. Exact large integers and missing historical clocks survive the
round trip. Other fixtures cover stale review, late-error rollback, child
cooldowns and fairness across restart, rewritten prefixes, completion outside
publication, missing projections, and migration of existing v1 released proofs.

Both build pipelines for the preceding commit failed twice while fetching the
pinned gallery-dl repository from Codeberg (HTTP 504 before code tests). The
dependency now uses the upstream GitHub repository at the exact same commit;
its local installation passed. This changes the transport URL, not the pinned
extractor version or runtime identity.

The full backend gate passed in 850.368 seconds with zero lint issues (API
681.549, ingestion 562.567, SQLite 841.071 seconds). All 388 producer tests passed
in 38.226 seconds. The real HTTP review/execution fixtures passed in 6.153 seconds.
The first complete backend run found one older migration fixture creating a new
v2 job before reconstructing schema 57; switching that fixture to a historical
v1 job passed its targeted check in 8.468 seconds and the subsequent full gate.

One SQLite online backup took 70.908 seconds. Normal opening, migration and
reinitialisation passed in 784.913 seconds, and fresh-process opening passed in
157.299 seconds. The large-copy operations used idle disk-I/O priority during
other tests and host activity; these are not isolated startup benchmarks.

Independent comparison passed in 869.229 seconds. All 238 existing data tables
retain their exact values and types, including 526,348 capture UUIDs, rowids,
observation/recording times and signatures. Prior migration history and sequence
counters are unchanged. Only the expected release-table definition and three
scope triggers changed; the three new execution tables are empty. These frozen
inputs contain no saved legacy checkpoint bodies, so populated handoff execution
is demonstrated by fixtures. Integrity is clean with zero foreign-key violations,
and the database remains 20,265,979,904 bytes. Startup/cutover performance remains
a release gate.

Evidence is under `.local/native-handoff-execution-rehearsal-20261003/`. This
verified replacement supersedes schema 64 under the retention policy after the
host-visible open-file check. Free space stayed above 133 GiB with both copies
present. After the checks, 763 closed temporary SQLite test files were removed,
reclaiming 973,881,344 bytes. Original compatible and frozen source inputs remain.

The full transition remains active: refreshed coordinated snapshots, migration
review, remaining operational/policy families, additional adapters, native UI,
host/n8n conversion, compatibility removal, coordinated backup/export/restore,
performance, owner-reviewed cutover and retirement are still required.
Production and `develop` remain unchanged.

## Native file edit and deduplication history — 2026-10-03

Schema 1000066 promotes catalog metadata edits, file-state changes and
deduplication assertions into native history, with source-file observations and
declared content claims as its references. Events preserve source IDs, original
timestamps and locations, including archive members. The receipt timestamp stays
separate from an unknown source clock. Path-specific edits remain separate;
legacy null means inherit, relationship values remain source names, and unknown
fields or representations remain inspectable extensions. Importing this history
does not change selected Stash fields, create files or execute old deletions.

The catalog import uses the completed media mapping's frozen root/collection
revisions and commits bounded 50-record/16-MiB batches. Its CLI resumes from the
native cursor after a lost response. Application APIs expose import receipts,
individual events and bounded observation/claim history. Startup verifies event
signatures, complete children, scope and exact source receipts; backup and
anonymisation include the graph.

Populated fixtures cover path-specific alternatives, inherit, unknown fields,
large integers, missing members, archive paths, prepared/finished assertions,
replay, swallowed late errors, altered receipts, read-only corruption rejection
and migration collisions. The real HTTP fixture drops a committed batch response
and resumes through fresh CLI processes. The frozen inputs contain no metadata
edits or file-state events, so their populated behavior is demonstrated by those
fixtures rather than claimed from an empty production sample.

The backend gate passed in 977.652 seconds with zero lint issues (API 774.741,
ingestion 647.853, SQLite 939.224 seconds). After strengthening exact receipt
verification, all affected history/HTTP tests passed in 35.640 seconds and final
lint passed in 17.157 seconds. All 391 producer tests passed in 73.095 seconds.

One SQLite online backup took 55.076 seconds. Normal migration and
reinitialisation passed in 1,070.266 seconds. Importing all 1,697 frozen catalogs
and verifying completed receipt replay took 210.964 seconds, including 194.690
seconds to open the database. All 927 deduplication assertions mapped without
review failures. A fresh process reopened the populated database in 209.793
seconds. Large-copy checks used idle I/O priority alongside other host work;
these are not isolated startup benchmarks. Startup/cutover performance remains
a release gate.

Independent comparison passed in 719.399 seconds. All 241 existing data tables
retain their exact values and types, including 526,348 capture UUIDs, rowids,
clocks and signatures. Existing schema objects, prior migration history and
sequence counters remain unchanged. The 927 assertions preserve 913 source
event IDs and 2,047 locations, including separate catalog copies and their
original timestamps. Integrity is clean with zero foreign-key violations. The
database remains 20,265,979,904 bytes.

Evidence is under `.local/native-file-history-rehearsal-20261003/`. The verified
replacement supersedes schema 65 under the retention policy after commit/push
and the host-visible open-file check. Original compatible and frozen source
inputs remain available. With both rehearsal copies present, free space stayed
above 133 GiB; each import batch also checks the 50-GiB host reserve plus its
write allowance.

The full transition remains active: historical metadata review/application,
remaining operational/policy families, refreshed snapshots, additional adapters,
native UI, host/n8n conversion, compatibility removal, coordinated recovery and
export, performance, owner-reviewed cutover and retirement still require work.
Production and `develop` remain unchanged.

## Reviewing historical metadata choices — 2026-10-03

Schema 1000067 adds immutable receipts for explicitly selecting a retained
catalog edit. The application API discovers alternatives through a selected
scene/image's indexed file associations, without scanning all catalogs. Preview
shows the current value, protection, proposed value and source context without
writing. Applying a ready preview commits the typed field decision and its
source-history/file-match receipt together. Unknown fields remain inspectable
evidence and cannot select arbitrary database columns. Migration creates no
reviews and changes no selected metadata.

Performer candidates include canonical names and aliases without prioritizing
either. Missing or ambiguous names block the complete relationship replacement
until explicitly resolved; bounded results report truncation. Studio, tag and
group selections also use native identities and revisions. Apply rechecks the
entity, selected field, candidate revisions, file ownership and file/ZIP
generations. A historical inherit choice releases protection while retaining
the current value until an allowed policy evaluates it. Different historical
paths remain separate choices even when their files were deduplicated.

Clients can recover the original receipt after a lost response by replaying the
same saved request UUID and body. Later edits, UUID adoption or deletion of the
matched file do not reapply a committed request. Changed bodies are rejected.
Late errors roll back the field decision and receipt together, including when a
caller swallows the error. Successful new changes use the existing after-commit
notification contract; retries do not register another notification. Current
field summaries expose compact provenance without duplicating their values or
including plugin settings. Native UI controls are still pending.

Populated SQLite and HTTP fixtures cover exact and deduplicated alternatives,
protected fields, inherit, URL normalization, name ambiguity and overflow,
unmatched names, target revisions, ZIP containers, source ownership, retry,
adoption/deletion, rollback, corrupt receipts, migration collisions and origin
protection. The full fork gate passed in 1,080.440 seconds, including 528 v3
tests, 391 producer tests, generated contracts, zero lint issues and the full Go
integration suite. Final candidate-overflow/migration-collision and HTTP summary
checks passed in 12.065 and 32.794 seconds. The last review-specific replay-error
change passed affected API/SQLite tests in 23.704 seconds and lint in 16.735
seconds. These final focused checks followed compilation of the full gate.

One online backup took 132.558 seconds. Normal migration and reinitialisation
passed in 996.212 seconds; a fresh process reopened the database in 205.753
seconds. The reopen profile places most CPU time in SQLite stepping during
earlier catalog validation. Startup performance remains a release gate; the
checks used idle I/O priority alongside other host activity and are not isolated
benchmarks.

Independent reconciliation passed in 791.971 seconds. All 248 existing data
tables retain their exact values and types, including capture rowids, clocks and
signatures. Existing schema objects, previous migration history and sequence
counters are unchanged. A separate generated-column comparison also verified
all 1,927 performer primary-name flags and their table definitions. Integrity is
clean with zero foreign-key violations. The database remains 20,265,979,904 bytes.
The frozen catalogs have no metadata-edit rows, so populated review behavior is
demonstrated by fixtures; the new receipt table is correctly empty in this copy.

Evidence is under `.local/native-metadata-review-rehearsal-20261003/`, with the
full fork gate report in the preceding file-history rehearsal directory. The
verified copy supersedes schema 66 under the retention policy after commit/push
and the host-visible open-file check. Free space was 178 GiB with both copies
present, above the 50 GiB host reserve. Original compatible and frozen migration
inputs remain available.

Remaining work includes native review controls, other operational/policy import
families, refreshed snapshots, additional adapters, host/n8n conversion,
compatibility removal, coordinated recovery/export, performance and the reviewed
production cutover. Production and `develop` remain unchanged.

## Scene and image metadata review controls — 2026-10-03

Scene and image pages now expose native Metadata review through the shared
layout's desktop tabs and mobile section menu. The panel loads only the selected
entity's current fields and a bounded page of file-linked historical edits.
Current protection and field decisions are expandable; individual alternatives
and their previews load on demand. Applying a choice refreshes active affected
library queries. Unrelated configuration and plugin settings are not included.

Previews compare current and proposed values and explain replacement of a
protected choice or release to automatic updates. Performer candidates show
canonical names, disambiguation and local library IDs. Ambiguous or missing
names require explicit choices, including a search for a differently named
existing performer. Studios, tags and groups use the same typed selection path.
Unknown retained fields remain unsupported. This is depicted metadata review,
not source-account ownership or a performer merge.

A deployment-scoped IndexedDB journal saves each exact Apply body before any
transmission. Transactions coordinate tabs and retain one unresolved choice per
scene/image. Opening the panel does not send mutations. Recovery checks the
original receipt before replaying the saved body, including after a page reload.
Mismatched receipts, uncertain responses and storage failures retain the pending
request. Only a definitive stale-preview refusal permits starting a new review.
Changed files and ZIP containers now report that refusal consistently, while
preserving the underlying repository error. Display refresh failures remain
separate from a successfully committed choice.

The final v3 validation gate passed with 540 tests, generation, type checks,
formatting, locales and retained contracts. The embedded v3 build and Go asset
checks passed. Eighteen browser checks passed in 28.7 seconds using the cached
version-matched Playwright image,
covering scene/image pages on desktop/mobile, ambiguous and unmatched names,
lost-response recovery, stale previews, multiple tabs and two deployment prefixes
on one origin. Chromium and WebKit screenshots were inspected. The initial host
browser invocation lacked its browser binaries; the cached container supplied
both engines without another browser installation.

The full fork gate passed in 976.756 seconds, including Go lint/integration tests
and all 391 producer tests. A subsequent UI refinement uses secure random bytes
for request UUIDs on LAN HTTP deployments where randomUUID is unavailable; the
final v3 gate, browser run, build and Go embedding checks above include it.
Schema stays 1000067 and this change requires no additional full-size rehearsal
database. Evidence is under `.local/native-metadata-ui-20261003/`.

Cleanup removed the closed schema-66 predecessor and temporary tests, reclaiming
20,307,513,344 bytes. Five unused intermediate native n8n rehearsal image tags
were removed while retaining the latest verified image. After the checks, 29
closed temporary test files were also removed; about 189 GiB was free with the
50 GiB reserve in force. Original compatible/frozen migration inputs and the
verified schema-67 copy remain.

Broader source/account/collection management, remaining import/policy families,
source adapters, live host/n8n conversion, compatibility removal, coordinated
recovery/export, performance and reviewed production cutover remain open.
Production and `develop` remain unchanged.

## Startup validation with stale import statistics — 2026-10-03

Catalog receipt validation now requires the existing unique snapshot index when
joining a publisher, attachment, media or document receipt to its import
checkpoint. The rehearsal's publisher/attachment planner statistics still
described one checkpoint after 1,697 catalogs had been imported. SQLite chose a
complete checkpoint-table scan for every receipt. Validation runs before writes,
so updating database statistics is not a prerequisite for opening it safely.
All validation predicates, counters, reference checks and rejection behavior
remain unchanged; no schema or stored-data migration is needed.

Read-only probes of the retained schema-67 database measured publisher receipt
validation at 26.464 seconds before and 0.837 seconds with the indexed lookup;
attachment validation measured 27.349 and 0.564 seconds. Both returned the same
valid result. Media/document lookups already chose that index in this snapshot;
they receive the same protection against stale statistics, without claiming a
measured query-plan improvement here. These are sequential checks under other
host activity, not isolated benchmarks or production startup guarantees.

The regression fixture builds the real native schema and installs the observed
one-row planner estimate. Removing the index requirement through a temporary
compiler overlay reproduces all four unwanted parent scans; the final code
passes. Existing import, damaged-receipt and before-write rejection fixtures
also pass. Complete read-only lineage validation passed before and after the
change, taking 300.363 and 113.474 seconds respectively. Its sampled CPU cost
fell from 175.08 to 112.03 seconds. Concurrent host load and cache state also
affect elapsed time; the profiles identify removal of the repeated scans.
The full fork gate passed in 979.024 seconds, including Go lint/integration tests,
540 v3 tests and 391 producer tests. The final one-row-statistics regression also
passed independently. Evidence is under
`.local/native-startup-performance-20261003/`.

The checks reuse the latest verified 20,265,979,904-byte native database through
read-only connections. No extra database archive is created. The 50 GiB host
reserve remains in force. Startup performance and the rest of the transition
plan remain release gates; production and `develop` remain unchanged.

## Native account ownership review API — 2026-10-03

Schema 1000068 adds durable receipts for explicit account ownership choices.
Application routes now page canonical accounts, qualified identifier candidates,
retained claim evidence and ownership history. Cards summarize at most eight
identifiers and mark truncation. Individual previews and links read the selected
account and performer; they do not scan catalogs or rebuild the library.

Linking requires an explicitly selected performer UUID and revision. Shared
names or identifiers remain candidates. Preview binds the account revision,
current resolved owner and proposed performer; changes after preview require
review again. Account owners remain separate from depicted media performers.
Unlinked and undecided choices are distinct. Retrying the same request returns
its original receipt after later links, unlinks, account consolidation, performer
UUID adoption, merges or deletion, without restoring a superseded choice.

Focused repository and HTTP checks pass, covering ambiguity, bounded discovery,
read-only previews, stale targets, response-loss retries, rollback after receipt
failure, identity changes, anonymisation and corrupt/missing schema rejection.
Migration fixtures preserve existing decisions and refuse destination collisions
without modifying the original file. The full fork gate passed in 1,027.769
seconds, including Go lint/integration tests, 540 v3 tests and 391 producer tests.
The full-copy migration passed in 1,099.134 seconds, and a fresh reopen passed in
165.359 seconds. On that copy, a 25-account page took 10.618 ms and selected
account details plus an unlinked-choice preview took 0.242 ms, without applying
a choice. These are local repository timings, not browser/network benchmarks.
Independent reconciliation passed in 635.003 seconds: all 249 existing tables,
their schema, history and sequence counters are preserved, including 1,182
accounts and four saved ownership decisions. Generated performer-name values
also match. The new receipt table is empty, integrity is clean and foreign-key
violations are zero. Evidence is under
`.local/native-account-review-rehearsal-20261003/`.

The 50 GiB host reserve applies to the candidate copy and validation work. Once
verified, the candidate replaces the preceding rehearsal rather than retaining
another full database archive. Native account review controls, other management
UI, remaining import/policy families, caller conversion, compatibility removal,
backup/export/restore and reviewed cutover remain unfinished. Production and
`develop` have not changed.

## Native account ownership review controls — 2026-10-03

Account review is now a native route in the desktop utility menu and mobile
navigation drawer. It pages canonical accounts and filters ownership status,
while identifiers, retained evidence and ownership history load on demand.
Existing links offer Change link. The performer picker displays disambiguation
and local IDs and requires an explicit selection before preview. Linking,
explicit unlinking and returning an account to review are separate choices;
account ownership does not assign depicted scene/image performers.

The browser durably saves an exact request before Apply. Recovery checks the
original receipt before resending, coordinates competing tabs and preserves
uncertain requests across reloads. Stale previews require fresh review. Successful
writes refresh the selected account and update its queue card; the existing
page cursor and scroll position survive returning to the queue. A failed refresh
does not erase confirmed success or permit another write from stale state.
Shared transport/storage helpers preserve the prior metadata-review journal.

Focused protocol tests cover response loss, concurrent tabs, endpoint/account
isolation, failed storage, wrong receipts and late completion. All 558 v3 tests,
type/locale/contract checks and the real embedded UI build pass. All 38 account
and metadata browser checks pass in Chromium and WebKit, including mobile
navigation and scroll restoration. Desktop/mobile screenshots were inspected.
The full fork gate passed in 979.647 seconds, including Go
lint/integration checks and 391 producer tests. The preceding `583e88f48` account
API checkpoint also passed CI lint, build and native image publication. Evidence
and the two retained UI screenshots are under
`.local/native-account-ui-20261003/`.

This increment changes no database schema and creates no additional full database
copy. The verified schema-68 rehearsal remains current, and the 50 GiB host
reserve remains enforced. Account equivalence review, broader source/collection
management, remaining import/policy families, host/n8n activation, compatibility
removal, native backup/export/restore and reviewed production cutover remain open.
Production and `develop` remain unchanged.

## Native account consolidation review — 2026-10-03

Application routes and Account review controls now expose explicit consolidation
of duplicate account records in one qualified service namespace. The selected
account's search is bounded and can resolve an exact archive UUID. Preview shows
both components, retained identifier evidence and the proposed owner. Conflicting
ownership requires an explicit choice; different stable IDs require acknowledgement.
Separate service accounts can still share a performer without being consolidated.
No consolidation changes depicted performers or media files.

The existing event's request digest supports exact read-only receipt checks and
replay after later consolidation, ownership edits, performer UUID adoption,
merges and deletion. The original domain request serialization stays unchanged.
There is no new schema migration, duplicate receipt body or full rehearsal copy.
The browser saves before sending, recovers pending requests after reload, updates
the affected cards, retains the queue and loads consolidation history on demand.
Stale review disables the old form before refreshing; the shared performer input
also honours disabled state while pending requests are checked.

Focused backend tests and all 24 consolidation client/storage checks pass. All
50 account, consolidation and metadata browser checks pass in Chromium/WebKit,
covering mobile layout, conflict resolution, response loss, exact retries and
stale-review recovery. Desktop/mobile screenshots were inspected. The full fork
gate passed in 998.494 seconds, including all 582 v3 tests, type/locale/contract
checks, Go lint/integration tests and 391 producer tests. The real embedded v3
build also passed. Evidence is under
`.local/native-account-consolidation-20261003/`. The preceding `df3534c90` UI
checkpoint passed CI build, lint, browser checks and native image publication.

The schema-68 rehearsal remains current and the 50 GiB host reserve still
applies. Broader source/collection management, remaining import/policy families,
live caller conversion, compatibility removal, backup/export/restore and reviewed
production cutover remain open. Production and `develop` remain unchanged.

## Frozen discovery and maintenance mapping — 2026-10-04

Schema 1000069 gives the retained discovery queue and maintenance records bounded
native mapping and inspection. The application API and
`stash-import-automation-discovery` share manifest-bound progress, atomic batches
and exact recovery after a lost response. Compact records reference original
payloads instead of duplicating them. Account cursors, page/attempt counts,
retry delays, staged results and candidate URLs keep their original semantics.
Lookup work reuses its enrichment mapping; completion requires existing scoped
proof. Maintenance summaries remain historical inputs. This pass never activates
scrapes, confirms candidate matches or changes performer ownership or selected
scene/image metadata.

The isolated full copy migrated normally from schema 68 to 69. Its 6,744 source
records mapped to 6,568 native outcomes and 176 review outcomes, all for the
original `no_known_profile` targets. These comprise six held account listings,
5,966 existing lookup targets, 569 held discovery targets, 20 proven historical
completions, 176 unresolved targets and seven maintenance entries. Independent
reconciliation verified original value hashes, native associations, exact retry
deadlines, both saved cursors and all 67 historical listing pages. Restart after
the first committed batch, terminal replay and complete paged inspection passed.

Parser, SQLite migration/restart/rollback and real Python CLI/API tests pass,
including a committed response being lost. All 50 account and metadata browser
checks pass in Chromium/WebKit. The prior `aa2a88896` CI browser failure was a
test race: it reloaded after the browser saved a request, before the original
apply reached the server. The corrected test waits for the intended lost
response and asserts one committed request before reloading. Its original
timeouts and no-duplicate-apply checks remain unchanged.

The full fork gate passed in 1,053.019 seconds, including all 582 v3 tests,
394 producer tests, Go lint/integration tests and UI type/locale/contract checks.
Fresh application open passed. A 100-record page took 8.4 ms and selected original
record inspection took 0.12 ms. Independent comparison preserved all 250 existing
tables, their values/types, migration history, sequences and all 1,927 generated
performer-name flags. SQLite integrity and foreign-key checks passed. Evidence is
under `.local/native-discovery-import-20261003/`. The verified schema-69 copy can
now replace its predecessor under the 50 GiB host reserve policy.
Discovery activation/execution and post-identity resolution, broader management
UI, policy conversion, live host/n8n callers, compatibility removal, coordinated
backup/export/restore and reviewed cutover remain open. Production and `develop`
remain unchanged.


## Verified legacy lookup identities — 2026-10-04

Schema 1000070 allows a completed native enrichment fetch to establish an
unclaimed Reddit/Twitter identifier on the original legacy post. Publication
requires one retained discovery mapping for that post, collection and exact
candidate URL, plus corroborating source account, strict filename or original
text/date evidence. The post UUID stays intact. A small immutable resolution
references the original snapshot, checkpoint record and identifier evidence;
source payloads are not copied into another store.

The identifier, captures, resolution, publication and target completion commit
atomically. A standalone resolution cannot commit. Missing proof, ambiguous
mappings and identifiers owned by another post keep the fetched checkpoint and
record an unsuccessful attempt for review. Successful receipt replay survives
restart, and startup reconstructs its proof after compact staging is released.
Scoped producer descriptions and an application inspection route expose the
resolution. This does not merge posts, assign performers or apply selected
scene/image metadata.

Focused tests cover original UUID preservation, source-parent context,
corroboration failures, conflicting ownership, late write and lease failure,
standalone commit rejection, changed proof at startup, migration collisions,
restart/replay and anonymisation. The real Python worker/API test verifies that
an identity rejection retains the server checkpoint and does not fetch again
on restart. The full fork gate passed in 1,139.242 seconds, including all 582
v3 tests, 394 producer tests, Go lint/integration tests and contract checks.

The isolated full copy migrated normally from schema 69 to 70. Independent
comparison preserved all 252 prior tables and their value types, migration
history, sequences, original schema objects and all 1,927 generated performer
name flags. SQLite integrity and foreign-key checks passed. All 5,966 lookup
targets retain one exact mapping; 5,963 candidate IDs remain unclaimed and the
three existing owners remain protected conflicts. Query-plan inspection confirms
the bounded candidate lookup uses its new covering index. Fresh application open
passed in 185.403 seconds; this is a measured rehearsal result, not a production
cutover estimate. Evidence is under `.local/native-discovery-identity-20261004/`.

The schema-70 copy can replace its verified predecessor under the 50 GiB host
reserve policy. Account-listing discovery and saved-cursor execution, reviewed
post consolidation and remaining association/policy migration, broader management
UI, live host/n8n conversion, compatibility removal, coordinated backup/export/
restore and reviewed production cutover remain required full-plan work.
Production and `develop` remain unchanged.


## Native account listing collector — 2026-10-04

The producer now has an isolated metadata-only page collector for the retained
Reddit submitted listings and Twitter timelines. It preserves the original
cursor, returns the next page boundary separately from listing completion,
uses source reservations before extractor initialization and does not construct
media download jobs, archives or postprocessors. Empty final pages, unchanged
cursors, fetch failures and rejected runtime/profile inputs have distinct
outcomes. The compact page codec preserves observation times, source numbers,
shared metadata and the existing retention/expansion limits. Discovery retains
its historical 4,096-record page bound; individual post enrichment remains
limited to 1,024 records.

All 401 producer tests pass. Focused malformed-cursor and page-boundary checks
pass, including an actual isolated child refusing source access before network
initialization. The existing real Python enrichment/API recovery suite also
passes after the shared child-process changes. Read-only reconciliation accepts
all six retained account URLs and both saved cursors, preserving their 67
historical pages. Evidence is alongside the identity increment under
`.local/native-discovery-identity-20261004/`.

This is the fetch component of the remaining native discovery implementation.
It does not activate those accounts, commit cursor progress, accept matches or
create completion receipts. Native page validation/storage, reviewed activation,
worker delivery/dispatch and reviewed post consolidation remain open, alongside
the remaining full transition phases. There is no new native schema or full
rehearsal copy. Production and `develop` remain unchanged.

## Shared native discovery page contract — 2026-10-04

The backend now parses the producer's `stash-discovery-page-v1` envelopes without
relaxing the existing post-only enrichment parser. It restores shared record
deltas and parent context with the same retention, observation-time, exact-number
and expansion checks. Discovery has its own invalid-work error and retains the
4,096-record, 32 MiB serialized and 128 MiB expanded limits. Fresh pages cannot
claim retained captures. An empty final page is valid; an incomplete page must
advance its service-specific cursor. Unsupported profiles, embedded URL cursors,
invalid versions, unknown envelope keys and malformed records fail validation.

Go and Python use one corpus containing six page cases, 27 profile forms and 39
rejected page variants. Both preserve original large integers, decimal spelling,
negative zero, nanosecond timestamps, shared album metadata and nested context.
Independent boundary tests cover UTF-8 cursor bytes, the full 4,096-record bound,
compact expansion, duplicate JSON keys and retained provenance forgery. All
archive tests, all 405 producer tests and the Go lint gate pass. The final cursor
tests pass after their addition. Read-only validation of the current schema-70
copy accepts all six original account profiles and both saved cursors, with 67
historical pages unchanged. Those synthetic validation envelopes are not fetched
pages or completion proof. Evidence is in
`.local/native-discovery-jobs-20261004/`.

This implements the page format, not cursor storage or discovery activation.
Binding requests and owned attempts, atomic page receipts/cursor advancement,
shared download/enrichment pacing, delivery/dispatch, matching and reviewed post
consolidation remain open. No schema migration or extra database copy was needed;
the verified schema-70 copy remains current. The remaining full transition phases
and reviewed production cutover are still required. Production and `develop`
remain unchanged, with over 180 GiB free during this increment.

## Durable account listing jobs — 2026-10-04

Schema 1000071 adds immutable listing definitions, original legacy-resume
references, page-job bindings, authenticated attempt ownership and compact page
receipts. Each `account.list_page` job retains one page and releases its source
reservation. The next job uses that exact next cursor; retries keep earlier
pages. Listing completion remains separate from post matching, native capture
publication and catalog migration completion.

Definitions pin the account/profile, collection revision/root, runtime/policy
and not-before deadline. Frozen resumes must prove their original cursor,
historical page count, collection and mapped cooldown; staged legacy results
cannot be skipped. New pages validate their original observation times and
cursor chain, rejecting gaps, repeated cursors and writes after completion.
The internal scoped coordinator checks credentials, source revisions and the
original lease deadline through commit. Lost page and failure acknowledgements
retain their original producer and attempt; a later successful retry cannot
acknowledge an earlier failed job.

Discovery participates in shared download/enrichment reservations, service
cooldowns and bounded download preference. Migration preserves existing jobs,
attempts, pacing and their guards while extending supported job kinds. Bounds
allow 16 active listing jobs, 10,000 new pages per listing and 2 GiB of compact
discovery bodies. Startup validates retained definitions, frozen resume evidence,
job generations and complete page histories.

Focused tests pass for page-by-page release, download preference, metadata
exclusion, producer failover, imported cursor/cooldown preservation, failure
replay, expired-lease/source-change rejection, caught-error rollback, migration
collision recovery, corruption refusal and anonymised reopen. The complete
`make validate-fork` gate passes in 1,117 seconds, including 582 UI tests, all 405
producer tests, Go lint and the backend suite. The final coordinator tests and
the subsequently added failed-predecessor receipt regression also pass.
The normal full-copy upgrade passed in 915 seconds. Independent reconciliation
preserved all 253 prior data tables, all 6,744 frozen discovery records, generated
performer-name flags, migration history and sequences. Only the five intended
existing schema objects changed; all other prior definitions matched. The five
new tables remain empty, so migration activates no listings. Integrity checking
passed with zero foreign-key violations. Comparison took 1,228 seconds.

A fresh native open passed in 389 seconds under concurrent comparison/test load.
The bounded 100-record historical listing took 7.3 ms and a selected original-row
lookup took 0.12 ms. Startup and comparison timings are not a SQL-only migration
or production-cutover estimate. The verified copy remains 20,265,979,904 bytes.
After committing this increment, it replaces the schema-70 rehearsal under the
existing retention policy; the original compatible snapshot and frozen inputs
remain retained.

This increment implements core storage and the scoped coordinator. Producer
HTTP routes, durable page delivery/dispatch, stale-definition maintenance,
reviewed activation, candidate matching and reviewed post consolidation remain
open, along with the remaining full transition phases. Production, its live
catalogs and scrapers, and `develop` remain unchanged. Evidence is under
`.local/native-discovery-jobs-20261004/`.

## Scoped discovery worker HTTP — 2026-10-04

The producer API now admits existing listing definitions by their pinned hash,
policy and runtime, describes the requested page, claims/renews owned attempts,
checks the exact profile's source reservation, retains pages and records
controlled failures. It advertises discovery protocol 1 and source-pacing
protocol 1, without advertising dispatch or activating imported work. Page
receipts remain bound to their original producer, job and fence; old failure
acknowledgements cannot affect a later successful attempt.

HTTP checks cover complete and nonfinal pages, renewal, exact source scope,
failure/cooldown replay, original cursor descriptions, malformed and duplicate
keys, oversized bodies, credentials rejected before body reads, credential
revocation and collection/producer isolation. The real Python transport test
preserves a page larger than 4 MiB, exact decimals, negative zero and Unicode
while recovering deliberately lost admission, claim, page and failure replies.
Each job retains exactly one attempt, and the nonfinal page remains incomplete
enumeration even after successful delivery.

The full `make validate-fork` gate passes in 1,079 seconds, including 582 UI tests,
all 405 producer tests, Go lint and the backend suite. The final added Python
HTTP interoperability and route tests pass separately in 45 seconds, and final
Go lint is clean. This API increment adds no schema or new rehearsal copy; it
uses the verified schema-71 storage increment. Evidence remains under
`.local/native-discovery-jobs-20261004/`.

The dedicated Python discovery client, durable outbox/lease execution, readiness
and stale-definition maintenance, reviewed activation, candidate matching,
verified staging release and reviewed post consolidation remain open. The full
transition's remaining import, UI, caller conversion, compatibility removal,
backup/restore, cutover and retirement gates still apply. Production and
`develop` remain unchanged.

## Validated discovery client and shared job lease — 2026-10-04

The supported Python discovery client now verifies the exact protocol and page
capacity, canonical listing definitions, original requested cursors, immutable
job arguments and returned ownership. Page delivery checks its original request
before sending a compact object-valued body, then validates the acknowledgement
against the exact body hash, page ordinal, producer and attempt fence. Successful
job descriptions require their own retained receipt; a failed predecessor cannot
inherit a successor's completion. Controlled failure acknowledgements preserve
the original attempt and outcome.

Discovery and enrichment now share `JobLease`, retaining server-clock deadlines,
monotonic elapsed time, stable claim owners, source reservations and heartbeat
failure behavior. Their native body encoders also share canonical Unicode
escaping without changing ordinary ingest event bytes or hashes.

All 411 producer tests pass, including six new discovery client tests. Real
Go/Python discovery, enrichment transport and enrichment restart tests pass in
48 seconds. The discovery fixture now uses the supported client and shared lease
to renew ownership, reserve the source and recover lost admission, claim, page
and failure replies. It preserves a page larger than 4 MiB, exact number tokens
and Unicode, and checks the next cursor without treating a nonfinal page as
completed enumeration. Build and lint passed on the preceding API revision.
Evidence is in `.local/native-discovery-client-20261004/`.

This increment adds no database schema or full rehearsal copy. Durable discovery
outbox/execution, readiness and maintenance, reviewed activation, matching,
verified staging release and reviewed post consolidation remain open, along with
the full transition's later phases. Production and `develop` remain unchanged.

## Durable producer discovery journal — 2026-10-04

Producer outbox schema 11 retains each selected discovery page's immutable
listing/cursor definition, original claim intent, owned attempt, exact compact
body and acknowledgement. Claims survive lost responses with their original
owner. A verified page receipt and body removal commit together; a nonfinal page
still means only page delivery. Controlled failures retain their attempt outcome
and clear an ended claim only after acknowledgement. Same-producer failover can
rebind saved bytes to a newly owned attempt without changing source observations.

Discovery, enrichment and ordinary event admission now share one transactional
byte budget. Extraction reserves a full 32 MiB page first, then retains only its
actual bytes. Pending/review evidence is never evicted for capacity. The separate
discovery process lock releases on process death without blocking ordinary
delivery or enrichment. A native terminal status can settle a claim that fetched
no data, but cannot discard an unacknowledged page or failure intent. Local
finished receipts are bounded conveniences; native receipts remain authoritative.

All 425 producer tests pass in 48 seconds, including 14 new journal tests. They
cover forced process death, original claim recovery, receipt/body atomicity,
wrong-cursor/owner rejection, shared concurrent capacity, retained review data,
controlled failure replay, later-attempt byte reuse and foreign completion that
cannot erase local evidence. A real SQLite schema-10 promotion fixture preserves
every prior table and its exact staged enrichment bytes; a schema collision
rolls back without replacing unknown evidence. Integrity and foreign keys pass.

Real Go/Python discovery and enrichment interoperability checks pass in 50
seconds. The discovery fixture reopens the outbox between claim and delivery
attempts, recovers deliberately lost native replies and frees a page larger than
4 MiB only with its checked receipt. Original source numbers, Unicode and cursor
progress remain intact. Build and lint also passed on the preceding client
commit. Evidence is in `.local/native-discovery-client-20261004/`.

This changes the producer outbox only; native schema 1000071 and its verified
rehearsal remain current. No new full database copy was created, and about
182 GiB remained free after validation. Producer execution/dispatch, readiness
and maintenance, reviewed activation, matching, verified native page release and
reviewed post consolidation remain open, alongside the rest of the transition.
Production and `develop` remain unchanged.

## Selected discovery page execution — 2026-10-04

The producer now executes or recovers one admitted listing page with
`execute-discovery`, replays saved intents with `deliver-discovery`, and exposes
`discovery-policy` and `discovery-status`. Account-listing profiles explicitly
bind `account.list_page` under `stash-gallery-discovery-v1`; they accept the
supported Reddit submitted or Twitter timeline forms for their reviewed service.
Private access references stay worker-side. Shared profile preparation preserves
the existing enrichment policy hash and its post-only scope.

Execution delivers retained evidence first, records claim ownership before
network access, reserves local capacity before extraction and supplies server
lease/source checks to the isolated collector. A returned page is persisted
before possibly expired ownership is checked. Recovery tries the original
acknowledgement before acquiring a new attempt for unchanged same-producer bytes.
A later attempt's completion cannot discard an unacknowledged page. Controlled
source failures retain their own delivery intents and server retry deadlines;
capacity failure releases the claimed attempt before any source fetch.

The delivery-only CLI runs without a website profile and cannot claim or fetch.
Exit 0 means `page_delivered`; the receipt still distinguishes a final from a
nonfinal page. Neither result claims accepted matches or a completed catalog
import. Dispatch across definitions and activation of imported accounts remain
separate work.

All 427 producer tests pass in 37 seconds. Ten real Go/Python worker scenarios
pass in 33 seconds, using separate processes for recovery: lost claims, pages
and failures; expired ownership; pause after extraction; capacity exhaustion;
rejected pages; a later attempt's completion; empty final pages; and policy
mismatch before claim. They retain exact source decimals, recover without
refetching, preserve review evidence and exclude private access values from
outbox and command output. Existing enrichment/discovery API regressions pass
in 58 seconds, and the Go lint gate reports zero issues. Evidence remains under
`.local/native-discovery-client-20261004/`.

No native schema or rehearsal copy changed; producer schema 11 remains current.
Discovery dispatch, readiness/stale-definition maintenance, reviewed activation,
matching, verified native staging release and post consolidation remain open.
The remaining import, native UI, host/n8n conversion, compatibility removal,
coordinated backup/restore, production cutover and retirement gates still apply.
Production and `develop` remain unchanged.

## Bounded discovery readiness and ownership maintenance — 2026-10-04

The producer API now exposes scoped readiness for existing account-listing
definitions and due admitted jobs. Collection UUID, pinned policy and extractor
runtime are required. The listing endpoint limits inspected rows, including
mismatched or stale definitions; its continuation cursor advances even when a
filtered page contains no candidates. The active-job query uses the bounded
queue index. Neither lookup admits work, claims a lease or loads large retained
page bodies. Admission and claim still revalidate the selected definition.

Successful nonfinal pages permit the next page's admission. Completed listings,
failed/cancelled jobs, changed definitions and future retry deadlines are
excluded. The Python client negotiates `discovery_readiness_protocol: 1` and
validates candidate ordering, exact hashes, page sizes and forward cursor
movement. The separate autonomous dispatch capability remains unadvertised.

Application-owned maintenance runs with the HTTP server every 30 seconds,
independently of producer connections. It cancels stale collection/account/root
bindings and expires abandoned claims with the existing retry delay and attempt
budget. Concurrent maintenance recovers an attempt once; a caught write error
cannot commit an ended attempt without its job transition. Source account
consolidation does not redirect previously authorized work. Prior pages and
original producer/attempt receipts remain replayable after cancellation.

Focused SQLite, HTTP and Python client tests pass, covering bounded filtered
pagination, current grants, retries, terminal jobs, concurrent recovery, server
shutdown, account consolidation, disabled roots and rollback after a write
failure. A rooted-collection fixture initially omitted its required relative
path; the fixture was corrected and that check passes. Real HTTP transport
checks retain large-page/lost-acknowledgement behavior while exercising readiness.
The complete `make validate-fork` pre-push gate passes in 1,048 seconds: backend
generation, v3 validation with all 582 tests, all 429 producer tests, Go lint
with zero issues and the full tagged Go suite. Evidence is retained under
`.local/native-discovery-client-20261004/`. A separately prepared producer
dispatcher is not included in this increment's gate or implementation claims.

A read-only query-plan check on the 20.27 GB schema-71 rehearsal confirms the
active-work, collection/UUID and listing/ordinal indexes. The empty readiness
lookups take less than 0.1 ms each despite 2,182 historical archive jobs. Native
discovery tables are not yet populated in this rehearsal, so these timings are
an index check, not a throughput claim for activated discovery. Populated-state
pagination and recovery are exercised by the SQLite fixtures.

Native schema 1000071 and producer schema 11 are unchanged; no additional full
database rehearsal copy is needed. About 182 GiB remained free when validation
started. Durable producer dispatch, reviewed legacy discovery activation,
candidate matching, verified native page release and reviewed post consolidation
remain open alongside the broader transition gates. Production and `develop`
remain unchanged.

## Durable per-collection discovery dispatch — 2026-10-04

`dispatch-discovery --collection UUID --profile PATH` now advances existing
reviewed account-listing definitions through scoped readiness. A cycle prioritizes
saved deliveries from any previous profile, local recovery and due admitted jobs
before fresh admission. Saved pages require their original matching profile only
when new ownership is necessary. Delivery-only dispatch omits the website profile
and cannot claim or fetch. The server advertises discovery dispatch protocol 1.

Producer schema 12 retains separate delivery, local-work, job and listing cursors
with revision-checked updates and persisted backoff. Empty filtered pages advance
the inspection cursor; blocked claims cannot cause successive polls to fill the
native queue. Lost admission responses are recovered through indexed job readiness.
Idle traversal, successful page delivery, final enumeration and accepted post
matches remain distinct results. This is an explicit collection/profile command;
global worker-profile rotation and activation of imported definitions remain open.

Promotion from producer schema 11 adds only the dispatch table. Tests preserve
every existing table, exact staged pages and original claims/receipts; an unknown
table collision leaves the prior version and evidence intact. Earlier migration
fixtures now remove the new table when constructing historical outbox inputs.
Native schema 1000071, the verified rehearsal and the original compatible snapshot
are unchanged.

All 441 producer tests pass in 40 seconds, including twelve dispatch/migration
tests. Three real Go/Python restart scenarios pass in 18 seconds. Each cycle runs
in a new process against native HTTP; lost admissions, lost page acknowledgements,
delivery-only CLI recovery and empty filtered pages reach the final cursor with
exactly two page fetches and one native attempt per page. Private website access
values stay out of the outbox and command output. Go lint passes with zero issues.
The existing discovery/enrichment transport and restart regressions pass in
102 seconds. Their earlier run found one old assertion expecting dispatch to
remain unadvertised; the expectation now checks the implemented protocol, and
the entire selected set passes. Evidence is under
`.local/native-discovery-client-20261004/`.

The preceding readiness increment passed the complete pre-push gate. This producer
increment still requires the remaining global dispatch integration, reviewed
activation, matching, staging release and post-consolidation work before native
discovery can replace the production recovery services. The full transition's
remaining import, UI, caller conversion, compatibility removal, coordinated
backup/restore, cutover and retirement requirements remain in effect. Production
and `develop` remain unchanged.

## Shared discovery worker dispatch — 2026-10-04

`dispatch-all` now includes `account.list_page` profiles alongside downloads and
post enrichment. It recovers one saved delivery from each metadata journal
before loading website profiles, and persists independent cursors before each
selection. Busy profiles and collections rotate across process restarts. A
competing cursor update prevents further stale selection without losing an
already returned delivery receipt.

The scoped collection endpoint follows current active collection/root grants
and returns small container references with existing listing definitions.
Per-container readiness still checks policy, runtime, eligibility and deadlines.
A root grant sees later registrations and excludes collections moved outside
that root. Lookup neither admits work nor loads retained page bodies.

Producer schema 13 adds discovery collection rotation and preserves the old
worker delivery cursor as the enrichment cursor. Migration tests compare both
journals and prior table contents, including exact pending bodies and receipts.
A forced failure after the column rename rolls back all changes and preserves
unknown input evidence. No native schema migration or full database copy is
required; schema 1000071 and its verified rehearsal remain current.

All 452 producer tests pass in 41 seconds, including eleven new global dispatch,
collection traversal, protocol and migration checks. Native collection-scope and
HTTP tests pass. Four real Go/Python restart scenarios pass in 14 seconds; the
global case runs the actual CLI and discovers a later collection under the same
root grant, finishing both listings with one attempt and fetch per page. Its
initial fixture failure came from sorting the saved gallery configuration and
therefore changing the reviewed policy hash; preserving configuration order
fixes the fixture, with an explicit round-trip policy assertion.

Read-only plans on the 20.27 GB rehearsal use the collection/root and listing
indexes, returning in less than 0.3 ms with 2,612 collections. Native listing
tables are empty there; these are access-plan checks, not populated throughput
claims. Evidence is under `.local/native-discovery-client-20261004/`. The complete
fork gate passed in 1,056 seconds against the shared-dispatch code checkpoint
`b4df7c5de`: v3 validation (582 tests), producer validation (452 tests), Go lint
(zero issues) and the full tagged Go suite. About 179 GiB remains free.

Reviewed legacy activation, candidate matching, verified native page release
and post consolidation remain open, together with the full plan's remaining
imports, native UI, actual host/n8n conversion, compatibility removal, coordinated
backup/export/restore, reviewed production cutover and retirement. Production,
`develop` and the frozen compatible release remain unchanged.

## Account-listing candidate comparison — 2026-10-04

The native importer now has a pure, bounded candidate matcher for retained held
discovery targets. It compares original title/text, UTC source date and qualified
original post URLs, rejects contradictory publisher IDs and keeps title-only
matches provisional. A repeated detail fetch cannot promote the inferred URL,
matching publisher or strict filename into corroboration. It does not mutate
the original target or select a native identity.

Page matching reconstructs the compact source envelope and groups candidates by
qualified post ID, retaining record ordinals instead of copying payloads.
Multiple images, videos and nested source context from one post share one
candidate. Distinct posts with identical captions remain competitors; final and
empty pages do not establish a match or complete catalog import.

The matching cases and the complete `pkg/scrape` suite pass, and Go lint reports
zero issues. Cases cover weak detail rechecks, publisher conflicts, retained
source context, renamed handles with stable IDs, Unicode lengths, UTC dates,
translated/duplicate text, exact large source IDs, album grouping and competing
posts. A read-only audit validates all 569 retained target inputs (356 Reddit,
213 Twitter), checks their original source hashes and leaves the schema-71
database unchanged. Preparation takes 18 ms; it does not fetch source data or
claim any accepted matches. Evidence remains under
`.local/native-discovery-client-20261004/`.

This increment adds the comparison policy only. Durable native candidate storage,
cross-page reconciliation, reviewed activation, detail execution, atomic match
publication, staging release and reviewed post consolidation remain open, as do
the broader transition gates. The preceding shared-dispatch checkpoint passed
the full fork gate. These pure helpers in `fa510dd13` additionally pass their
focused checks, the full importer package suite and final Go lint. Production
and `develop` remain unchanged.

## Durable discovery candidate comparison — 2026-10-04

Native schema 1000072 binds original held targets to an existing legacy-backed
listing, exact frozen-record hash and reviewed native post revision. It keeps
immutable per-page comparison receipts and original record references, with one
candidate per qualified post ID across listing pages. Repeated attachments share
that candidate, later corroboration retains earlier provisional evidence, and
different IDs remain competitors. Original source payloads are not copied into
candidate rows.

Preparation runs under a read transaction. A short managed write rechecks the
reviewed source and post, commits receipt/references/grouping/cursor together,
and rejects a native edit made between preparation and commit. Caught errors
cannot commit partial results. Exact receipts survive later source changes;
new comparisons stop. Limits preserve pending source pages and roll back the
entire comparison instead of truncating a candidate set.

The HTTP server now owns a separate comparison worker. It inspects at most 32
pending targets per step, advances through waiting/stale rows and resumes from
durable per-target progress after restart. It reads retained batches without
website access or source admission. A comparison reaching the final cursor
does not establish coverage of historical pages, choose an identity, publish
metadata, finish an import or authorize staging release.

Focused tests pass for migration from schema 71, preservation of existing pages
and scheduling/import rows, rollback on an unknown destination table, corrupt
receipt/reference rejection, anonymisation, concurrent replay, interrupted
writes, source changes, exact binding, capacity overflow, empty final pages,
bounded readiness, worker restart and cancellation. The combined SQLite and
server-lifecycle selection passes in 20 seconds. Initial failures were invalid
corruption/capacity fixtures (a foreign-key guard prevented the injected orphan,
and a compact record referenced itself); corrected fixtures exercise the intended
invariants and pass. Evidence is under `.local/native-discovery-client-20261004/`.

The complete fork gate and independent full-copy schema-72 migration/reconciliation
are running at this checkpoint. The previous schema-71 rehearsal remains the
verified baseline until those copy checks finish. The new copy leaves about
159 GiB free, above the 50 GiB reserve. Reviewed activation, historical coverage,
weak-detail execution, match publication, staging release and reviewed post
consolidation remain open, alongside the full plan's remaining native UI, caller
conversion, compatibility removal, coordinated backup/export/restore, production
cutover and retirement gates. Production and `develop` remain unchanged.

### Candidate comparison verification — 2026-10-04

Checkpoint `457ad026b` passed the complete fork gate in 1,219 seconds: generated
bindings, v3 validation (582 tests), producer validation (452 tests), Go lint
with zero issues and the full tagged backend suite. The schema-72 full-copy
migration also passed: all 258 prior data tables and generated columns match
exactly, all 6,744 frozen discovery records remain, existing schema objects are
unchanged, integrity is clean and there are no foreign-key violations. The four
new comparison tables are empty; importing a database did not activate discovery.

Reopening the 20.27 GB archive passed using a verifier pinned to `457ad026b` while
the next activation increment was developed separately. A 100-row discovery
inspection took 8 ms and one original-record lookup took 0.16 ms. These are
read-only inspection timings, not activated-work throughput claims. Schema 72 is
now the verified rehearsal baseline. After confirming no readers remained, the
superseded schema-71 database and its sidecars/search index were removed,
reclaiming about 20.27 GB and leaving about 177 GiB free. The original compatible
snapshot and frozen import inputs remain retained.

Evidence is under `.local/native-discovery-match-20261004/` and
`.local/native-discovery-client-20261004/`. The verification covers the candidate
storage/worker checkpoint only; separately developed activation changes still
require their own checks. Production, `develop` and the frozen compatible release
remain unchanged, and the full transition goal remains open.

## Reviewed discovery activation API — 2026-10-04

Schema 1000073 adds application-authorized preview/apply and immutable receipts
for binding an explicit set of retained discovery targets to one account listing.
The reviewed input pins the original snapshot manifest and target record hashes,
saved account cursor/history and deadline, current collection/root, and worker
policy/runtime. Preview derives the current native post revisions. Apply binds
the listing, targets and receipt in one transaction; late failures roll back all
of them. A repeated operation returns its original receipt after later native
edits, including after a committed HTTP response was lost.

Bounded application routes inspect the listing, page receipts, target progress,
candidate post IDs and original evidence references. Producer credentials do not
grant these review operations. Activation admits no source job and makes no
identity, historical coverage, metadata publication or import-completion claim.

Focused archive, SQLite migration/replay/corruption and real Go/Python HTTP
checks pass. The HTTP fixture retains two targets, the saved cursor and 67
historical pages, recovers a deliberately dropped committed response, and then
compares three album attachments as one candidate per target. Tests also cover
stale reviews, caught transaction errors, pre-commit changes, unknown migration
objects and authorization boundaries. The full fork gate and independent
schema-73 rehearsal are running; schema 72 remains the verified baseline.
The copy check initially rejected the empty WAL created by opening SQLite
read-only. No committed frames or main-database changes occurred; the corrected
check treats an empty WAL as absence and the replacement backup passed.

Evidence is under `.local/native-discovery-client-20261004/` and
`.local/native-discovery-activation-20261004/`. The operator client/UI, activation
of the real held accounts, historical coverage, detail execution, match
publication and staging release remain open alongside the broader transition
gates. Production and `develop` remain unchanged.

## Saved discovery activation operator command — 2026-10-04

`stash-activate-automation-discovery` now prepares, shows, applies and inspects a
single reviewed listing/target batch through the native application API. It saves
a private file atomically without overwriting an earlier review, checks its exact
digest and endpoint before use, and preserves generated operation/listing UUIDs
across retries. Original target ordinals/hashes, saved cursor/history, collection
scope and exact deadline are validated before preview; the saved response also
pins native post UUIDs/revisions. Website configuration and credentials remain
local to the producer.

Apply first looks for the original receipt. A lost committed response therefore
recovers without issuing another binding request or refreshing the review.
Conflicting source changes preserve the file and require a new explicit review;
status and offline Show do not mutate native state. Results distinguish retained
bindings from execution, accepted identities and historical coverage.

All 458 producer tests pass, including six focused activation tests with the
1,000-target boundary, changed files/endpoints, cursor and hash mismatches, exact
deadline normalization, lost responses and stale review. The real Go/Python HTTP
fixture now invokes the shipping command for prepare/show/status/apply and passes
in 13 seconds, including response-loss recovery and unchanged saved plan bytes.
Evidence is under `.local/native-discovery-client-20261004/`.

The schema-73 full-copy rehearsal and full backend gate remain in progress. The
real held accounts have not been activated. Native review UI, historical
coverage, detail/publication/staging-release work and the full plan's remaining
conversion, restore and cutover gates remain open.

### Activation code verification — 2026-10-04

The complete fork gate for the schema-73 API checkpoint passed in 1,256 seconds,
including generated bindings, v3 validation, producer checks, zero-issue Go lint
and the full tagged backend suite (SQLite: 1,142 seconds). The subsequent
operator increment passed the 458-test producer suite and the real HTTP test;
the final out-of-range UTC deadline correction passed its focused tests.
These changes are separate incremental commits on `v3-rewrite`.

A read-only audit of the verified archive groups all 569 held targets into six
account/collection batches: 198, 186, 3, 2, 22 and 158 targets. All associated
native posts are active. Five collections were already active in the rehearsal;
the 198-target collection remains disabled. The two retained cursors account
for 37 and 30 historical pages, with no staged account-page bodies. This audit
does not activate the accounts or prove source coverage. The schema-73 migration
rehearsal is still running; retain schema 72 until independent reconciliation
and reopen checks pass.

## Real discovery activation rehearsal — 2026-10-04

The independent schema-73 migration comparison passed: all 262 pre-existing
data tables and generated columns match, the 6,744 frozen discovery records are
intact, integrity is clean and there are no foreign-key violations. Checkpoint
`4c9a72d02` also passed Build and Lint CI; preview-image publication is still
running.

The shipped operator command then bound the six retained account batches on that
isolated copy, preserving all 569 original target hashes, account references and
post revisions. All six review files were durable before binding began. A
deliberately lost committed response recovered through the original receipt,
and repeated Apply/status preserved every saved file. Client execution took
2.8 seconds after the database opened in 120 seconds. No source job was admitted
or website contacted. These bindings do not accept post identities or establish
coverage of the two saved cursors' 67 historical pages.

The rehearsal derives metadata-only Reddit/Twitter profiles from the previously
staged host configuration, keeping private credential references local and
pinning the actual worker policy/runtime. One previously disabled collection
received an explicit rehearsal revision; the other five retained their current
definitions. `GET /api/v3/archive/collections/{collection}` now retrieves that
selected definition directly. The metadata and activation HTTP checks pass in
14 seconds, and Go lint reports zero issues.

An independent receipt-graph check verifies the six operations, all 569 original
records, deterministic target UUIDs, native post revisions, hashes and saved
cursor/deadline values. Full reconciliation after those writes and a populated
archive reopen remain in progress; schema 72 remains the verified baseline until
they pass. Evidence is under `.local/native-discovery-activation-20261004/` and
`.local/native-discovery-client-20261004/`. Production, the frozen compatible
release and `develop` remain unchanged. Historical coverage, detail execution,
publication, staging release and the broader plan's remaining gates remain open.

### Populated activation archive verification — 2026-10-04

Schema 73 is now the verified rehearsal baseline. Reconciliation after activation
preserved all 257 unaffected prior tables, including generated values, and
independently verified the six receipt graphs, 569 target bindings and one
expected collection revision. The original account cursors, 67 historical pages,
retry deadlines, post revisions and record hashes remain intact. Integrity and
foreign-key checks pass, with zero admitted account-listing jobs.

A fresh populated reopen passed in 118 seconds; reading all six activation
receipts took 17 ms, a 100-record import page took 8 ms, and one original record
took 0.17 ms. The 89-file producer package used for the reviewed profiles is
retained separately and reproduces both policy hashes, allowing later producer
development without losing the matching runtime source. Expanded website
credentials are not copied into these artifacts.

After checking for open readers, the verified pointer was advanced to schema 73
and the superseded schema-72 database and sidecars/search index were removed.
That reclaimed 20.27 GB and left about 176 GiB free. The original compatible
snapshot and frozen import inputs remain retained. Evidence is in
`activation-reconciliation.json`, `reopen.json`, `pinned-profile-verification.json`
and `predecessor-cleanup.json` under
`.local/native-discovery-activation-20261004/`. This completes the isolated
activation rehearsal; native matching/publication and the remaining transition
work still precede production cutover.

## Discovery coverage and candidate review — 2026-10-04

The application API now reviews one discovery target's coverage and candidates
in a single read transaction. It distinguishes received batches from completed
comparisons and makes missing historical batches explicit. Finishing a resumed
search cannot report complete coverage from a saved cursor or an imported page
count. Reviews also report weak/competing candidates, changed source or post
choices, already identified targets and candidate identifiers owned by another
native post, including forgotten posts. The endpoint reads indexed receipts and
references without loading source page bodies, admitting work or writing metadata.
An empty blocker list is neither identity acceptance nor a publication receipt.

Focused SQLite and real Go/Python HTTP tests passed in 16 seconds; Go lint
reports zero issues. Coverage includes incomplete listing/comparison, stronger
evidence replacing weak evidence, repeated attachments/pages sharing a candidate,
competing IDs, empty searches, native edits, collisions, authorization and restart.
The retained schema-73 archive also passed inspection of all 569 targets: 188
correctly report the two saved cursors' 37 and 30 missing historical batches.
Opening the archive took 191 seconds; all target reviews then took 112 ms total,
with a median selected-target lookup of 0.16 ms. Evidence is under
`.local/native-discovery-client-20261004/discovery_review_*` and
`review-rehearsal.json`. No schema or production change was needed.

Historical coverage resolution, detail execution, atomic publication, staging
release and the broader transition gates remain open. This review provides
inspection of those distinctions; it does not complete them.

## Atomic discovery match publication — 2026-10-04

Schema 74 now retains application publication of a complete, unique strong
listing match. The operation preserves the imported native post UUID and keys,
accepts its verified source ID, records canonical URL provenance and publishes
the selected post's observations from the strongest page through shared native
capture, publisher, album and translation services. The original observing
producer, observation times, shared bodies/profiles and parent context survive.
Publication and per-record capture associations commit atomically; unrelated
page contents remain staged. Historical gaps, incomplete comparison, weak or
competing candidates, changed native choices and an ID owned by another post
continue to block publication.

Focused capture, SQLite, migration/reopen and real Go/Python HTTP tests pass,
including concurrent replay, a deliberately lost committed response, caught
partial-write errors, forged capture metadata, pre-commit source changes,
corroborating observation time and immutable replay after a post tombstone.
The API test also verifies native publisher and album services and rejects an
implicit merge of two imported posts claiming one source ID. Go lint reports
zero issues. Captured titles retain their original source capitalization.

The full schema-73 archive was copied without changing its source. Schema-74
migration and the full fork gate are being verified; schema 73 remains the
retained verified baseline. Evidence is under
`.local/native-discovery-client-20261004/discovery_publication_*` and
`.local/native-discovery-publication-20261004/`. Production, `develop` and the
frozen compatible release remain unchanged. Historical coverage resolution,
weak-candidate execution, automatic publication dispatch, staging release,
explicit post consolidation and the broader transition gates remain open.


### Populated publication archive verification — 2026-10-04

Schema 74 is now the verified rehearsal baseline. Its migration passed in 597
seconds. Independent typed row comparison preserved all 264 prior tables,
including generated values, six activation receipts, 569 target bindings and
6,744 frozen discovery records. Integrity is clean and there are no foreign-key
violations. A fresh populated reopen passed in 107 seconds; reading the six
receipts took 15 ms and a selected original record took 0.13 ms. No source jobs
or real-target publications were created by this rehearsal.

Checkpoint `6d3e316cd` passed the complete fork gate in 1,164 seconds, including
v3 validation, generated bindings, 458 producer tests, zero-issue Go lint and
the full tagged backend suite (SQLite: 1,065 seconds). Build, Lint and preview
image CI also passed. The populated archive evidence is under
`.local/native-discovery-publication-20261004/`; check logs are under
`.local/native-discovery-client-20261004/discovery_publication_*`.

After host open-file inspection, the verified pointer advanced to schema 74 and
its predecessor's database, sidecars and search index were removed. This reclaimed
20.27 GB and left about 173.6 GiB free. The compatible snapshot, frozen inputs,
activation plans, original receipts and pinned producer runtime remain retained.
Production remains on the compatible deployment.

## Automatic discovery publication — 2026-10-04

The server now publishes eligible strong matches after comparison, using the same
native service and immutable receipts as the application endpoint. Each inspection
reads at most 32 primary-key target rows, skips published and unresolved targets,
and never loads source bodies for readiness. Publication reparses the evidence
and rechecks the selected revision and current source/post choices atomically.
Competing workers and process restart preserve one original publication. No
website request, source-job admission, depicted-performer choice or staging
release is added.

Both comparison and publication now reach a real idle pause after traversing
waiting targets. Previously, comparison could wrap an empty final batch straight
to the first batch and poll continuously when more than 32 targets were waiting.
The regression covers repeated traversals and a ready target beyond the first
batch. Integrated SQLite/service/HTTP tests passed in 23 seconds, covering
competing workers, restart, cancellation after inspection, receipt recovery and
blocked weak/incomplete/competing evidence. Go lint reports zero issues. This
increment requires no additional schema change.

Historical coverage resolution, weak-candidate detail execution, verified staging
release, explicit post consolidation, review UI and the broader transition gates
remain open. Production, `develop` and the frozen compatible release remain
unchanged.


## Reviewed recovery of incomplete historical searches — 2026-10-04

Schema 75 now links a reviewed fresh account search to its original incomplete
listing. The original cursor, historical page count, retry deadlines, attempts,
retained batches and candidate evidence remain intact. Each selected recovery
target points to its original binding, source hash and native post UUID. The
existing application preview/apply routes and saved-plan Python command retain
that relationship and recover the original receipt after a lost response.

Recovery waits for a running producer and atomically cancels queued predecessor
work with the replacement and bindings. The original search then stops admitting
pages, while its existing receipts remain replayable. Pending comparisons of its
retained batches continue. A fresh search cannot publish while an earlier batch
is uncompared or an earlier candidate differs from its sole new match. Both the
publication service and startup proof reconstruction enforce those checks.
A fresh search supplies current evidence; it cannot recover posts a website no
longer exposes or prove complete historical source coverage.

The focused capture/repository/migration/HTTP suite passed in 36 seconds,
including the real Python client's saved recovery plan, lost activation and
publication responses, fresh-page publication, restart, competing candidates,
quiescent handoff, preserved retry deadlines, caught partial-write rollback and
unknown migration objects. The final original-reference and predecessor receipt
regressions pass in 7 seconds, and Go lint reports zero issues. All 459 producer
tests pass. Checkpoint `b625bf86ea` also passed the full fork gate in 1,116 seconds,
including v3 validation, generated bindings and the tagged backend suite
(SQLite: 1,032 seconds). Build, Lint and preview-image CI passed.

The populated copy migrated in 217 seconds. Independent typed comparison
preserved all 266 prior tables, including generated values, six activation
receipts, 569 bindings and 6,744 frozen discovery records. The application API
then applied saved recovery plans for the two original searches with 37 and 30
historical batches. Their 188 new bindings preserve the original target/post
references; the earlier bindings and cursors remain intact. Both plans were
saved before Apply, and a deliberately lost acknowledgement recovered the same
receipt. The client work took 1.3 seconds after database opening.

A second independent comparison verified all original values and exactly the
reviewed additions: two listings and receipts, 188 target bindings and their
recovery links. The other 261 tables remain unchanged. Integrity is clean, with
no foreign-key violations. A fresh process reopened schema 75 in 111 seconds;
checking eight receipts, 757 bindings and all 188 recovery reviews took 76 ms.
The six original and two recovery definitions also match their separately pinned
producer runtimes and profiles. Neither activation nor reopening fetched source
data, admitted jobs or published real-target matches. Actual source coverage
remains pending.

After host open-file inspection, schema 75 became the retained verified copy.
Removing only its predecessor's database and sidecars reclaimed 20.27 GB and
left about 172.4 GiB free. The compatible snapshot, frozen inputs, original and
recovery plans, receipts, profiles and runtimes remain retained. Evidence is
under `.local/native-discovery-recovery-20261004/` and
`.local/native-discovery-client-20261004/discovery_recovery_*`.

Weak-candidate detail execution, verified staging release, explicit post
consolidation, review UI and the wider transition gates remain open. Production,
`develop` and the frozen compatible release remain unchanged.

## Original-evidence detail comparison and previews — 2026-10-04

Weak discovery candidates can now be compared with a compact post-detail
transcript using their original saved listing and frozen catalog evidence.
The requested post/runtime must agree, every record must belong to that post,
and a contradictory publisher rejects the response. Confirming an inferred URL,
account or filename cannot manufacture corroboration. Pending child requests
stay pending; unresolved references stay explicit. A negative detail response
does not discard a competing candidate. Results retain hashes and record/witness
ordinals instead of duplicating source payloads.

Native capture preparation preserves original observation times, parent context,
exact source numbers and per-record observing producers across later deliveries.
The real Python metadata collector fixture covers Reddit/Twitter albums, compact
shared post fields, failed-child resume and native capture reconstruction, with
network and download writers prohibited by the fixture. The affected model,
archive and scrape package suites pass. Integrated SQLite/review/activation/
publication/recovery HTTP tests pass in 22 seconds; Go lint reports zero issues.

The application-only `detail-preview` endpoint uses selected indexed reads in
one read transaction. Stale revisions, changed selections and malformed bodies
are rejected. Even a corroborated preview returns `preview_only: true` and the
unchanged current blockers; it cannot accept an identity or publish captures.
This increment needs no schema migration. The verified schema-75 rehearsal,
production deployment and frozen compatible release remain unchanged.

Candidate-specific owned jobs, durable result retention and guarded publication
of their verified evidence remain to be integrated. Review UI, actual source
coverage, staging release and the wider transition gates remain open.


## Durable candidate detail backend — 2026-10-04

Schema 1000076 introduces candidate-bound metadata jobs without registering the
inferred URL as an accepted native association. Admission pins the original
frozen source and listing/page evidence, native post/target revisions and the
selected detail runtime. Immutable attempt and checkpoint receipts preserve the
original producer/time of every record across delivery, child retries and worker
handoff. Empty and negative responses remain explicit uncorroborated results;
completion never discards a competing candidate or clears missing-history and
native-choice blockers.

The scoped worker API supports admission, readiness, claim/renewal, service
reservations, compact checkpoint save/resume, controlled failures, explicit retry
and result recovery. Detail jobs use the same collection/service exclusion,
download preference and child cooldowns as existing work. Application maintenance
recovers expired ownership and cancels stale selections. Source, credential and
original lease checks guard commit. Startup validates the entire retained graph
and reconstructs completed comparisons; anonymisation removes the private bodies.

Focused tests cover producer handoff, lost acknowledgements, reopen, empty/negative
results, changed selections, commit-time changes, retries/deadlines, shared child
cooldowns, exact HTTP input/auth boundaries, migration rollback and corruption.
Checkpoint `334144b49` passed the full fork gate in 1,212 seconds, including
582 v3 tests, 459 producer tests, zero Go lint findings and the tagged backend
suite (SQLite: 1,110 seconds).
The populated copy migrated successfully in 543 seconds. Independent typed row
comparison, including generated values, preserved all 268 pre-existing domain
tables. It verified exactly seven new tables and four changed SQL objects;
SQLite integrity is clean, with no foreign-key violations. Reconciliation took
1,076 seconds. A fresh process reopened schema 76 in 109 seconds and inspected
all eight activation receipts, 757 target bindings and 188 recovery links in
83 ms. The 6,744 frozen discovery records remain intact. These checks admitted
no source jobs and accepted no real-target identities.

After host open-file inspection, schema 76 became the retained verified copy.
Removing only the superseded schema-75 database and sidecars reclaimed 20.27 GB,
leaving about 169.7 GiB free. The original compatible snapshot, frozen inputs,
saved plans, receipts, profiles and producer runtimes remain retained. Evidence
is under `.local/native-discovery-detail-20261004/` and
`.local/native-discovery-client-20261004/discovery_durable_*`.

The Python detail worker's outbox/dispatch integration, publication of verified
detail receipts and staging release remain open. The full transition also still
requires native management UI, remaining migration/review reconciliation,
host/n8n cutover, compatibility removal, coordinated backup/restore and the
production acceptance gates. Production and frozen release tags are unchanged.


## Candidate detail producer and dispatch — 2026-10-04

The Python producer now executes candidate-bound detail jobs through the native
API, using shared metadata lease/checkpoint mechanics with separate comparison
receipts. Its schema-14 outbox retains exact pending bytes, stable claims,
completion/failure intents and dispatch cursors. All metadata journals share the
same capacity budget. Successful empty responses retain negative evidence;
source failures remain failures. Delivery recovery works without website
credentials or the original profile, and never silently discards local evidence.

Explicit candidate admission, selected execution, retry and status commands are
implemented. Collection-scoped dispatch resumes admitted jobs; shared profile
rotation includes `post.verify_candidate`. The native collection endpoint
inspects only the bounded active-job set, enforcing current collection/root
grants, pinned runtime, eligibility and retry delays. Readiness cannot admit new
candidates or accept identities. No new native schema migration is needed.

The real Python/Go HTTP fixture deliberately loses both checkpoint and completion
acknowledgements. Separate producer processes recover the original receipt with
one source fetch, unchanged observation bytes and the original producers; the
native review stays unchanged. Populated outbox migration tests preserve every
pre-existing value and pending body, while unknown-object collisions roll back.
All 476 producer tests pass. The native detail and Python/Go interoperability
suite passed in 115 seconds, including existing enrichment and listing workflows.
The full fork gate passed in 1,152 seconds: v3 validation, all 476 producer tests,
Go lint with zero issues, and the tagged backend suite (SQLite 1,051 seconds).

Automatic candidate admission, publication from authenticated detail receipts,
verified staging release and the broader transition gates remain open.
Production, `develop` and the frozen compatible release remain unchanged.

## Authenticated detail publication — 2026-10-04

Schema 1000077 connects completed candidate detail results to the native
publication service and its automatic worker. A unique weak candidate can use its
latest authenticated corroboration after complete listing comparison; earlier
successful detail work survives later listing progress without refetching the
same post. Negative results remain visible, and a later negative result cannot
be bypassed by selecting an older positive one. The original weak candidate,
source values, page references and every competing candidate remain unchanged.

Application publication pins `detail_job_uuid`. Preparation rederives the saved
comparison and capture inputs; commit checks original native choices, complete
coverage, recovery predecessors and identifier ownership again. Identity and URL
evidence, native captures, publisher/album/translation effects and the receipt
commit together. Each capture retains its original observing producer and time
across worker failover. Existing listing publication receipts retain all values;
the new nullable detail reference identifies the alternate observation source.
Startup reconstructs either proof, and anonymisation removes children in foreign
key order. Staging bodies remain retained for subsequent verified release.

Focused SQLite/API tests passed in 63 seconds. They cover failover provenance,
later listing comparisons, native edits, competing/earlier candidates, missing
history, negative/preview-only evidence, caught-error rollback, forged captures,
reopen corruption checks and anonymisation. The real Python/Go HTTP workflow
loses checkpoint, comparison and publication responses, then recovers original
receipts across processes and database reopen. It also proves that publication
uses shared publisher/album services and refuses implicit post consolidation.
Populated predecessor migration fixtures retain existing receipts, values and
guards; unknown-object collisions fail without losing their input.

The complete `make validate-fork` gate passed in 1,260 seconds, including 582 UI
tests, 476 producer tests, Go lint with no issues, and the tagged Go suite. CI
lint and build also passed for `b55232898`; its preview-image workflow was still
running when this checkpoint was recorded.

The isolated 20.27 GB schema-76 copy migrated to schema 77 in 222 seconds.
Independent comparison checked all 275 prior domain tables, including generated
values, and retained every existing value. Only the planned publication table,
two scope triggers and two new indexes changed. Integrity checking passed with
zero foreign-key violations. A fresh open took 111 seconds and retained all eight
activation receipts, 757 bound targets and 188 recovery links. Receipt inspection
took 87 milliseconds, a 100-row record page eight milliseconds, and a selected
record 0.15 milliseconds. No source jobs were admitted on the rehearsal.

Schema 77 is now the retained verified rehearsal. After checking that neither
database had open file holders, the retention step removed only the superseded
schema-76 database and its sidecars, recovering 20.27 GB and leaving 167 GiB free.
The original compatible snapshot, frozen inputs, activation plans and worker
profiles remain retained. Reports are in
`.local/native-discovery-detail-publication-20261004/` and the current rehearsal
pointer records the verified code and validation report.

Automatic candidate admission, actual source coverage, verified staging release
and the broader transition remain open. Production is unchanged.

## Automatic candidate detail admission — 2026-10-04

The selected `post.verify_candidate` producer profile can now discover and admit
new detail work after recovering saved delivery and visiting existing jobs.
Automatic admission requires a completed retained comparison, exactly one weak
candidate, and no other review blocker. Existing job history for the same
target/candidate prevents automatic readmission, including earlier comparison
revisions, retry delays, cancelled/failed jobs and completed negative results.
Explicit admission/retry remains separately available; exact original admission
replays after later edits or publication.

New scoped inspection routes return permitted collection containers and bounded
candidate pages. Each request visits at most 32 listing definitions and 32
targets using existing indexes. The cursor advances over empty and blocked rows;
the filtered result count does not indicate completion. The producer checks its
profile's URL support, while the server repeats review eligibility inside the
admission transaction and before commit. This increment requires no new native
schema or database copy.

Producer schema 15 adds the candidate cursor without replacing any schema-14
journal, pending body, receipt, retry deadline or rotation state. Dispatch saves
selection before sending, recovers uncertain admission through ready-job lookup,
and pauses after complete collection traversals, including multiple-page passes.
Strict response validation rejects nonadvancing, duplicate or altered candidate
pages. Saved delivery continues without website profiles.

The complete discovery SQLite/API regression group passed in 59 seconds. The
real Python/Go workflow loses automatic admission, checkpoint, comparison and
publication replies and recovers their original jobs and receipts across
processes. Focused tests cover incomplete and competing searches, earlier
recovery conflicts, native choices, all previous job states, transactional
rollback, bounded empty-listing traversal and indexed query plans. All 484
producer tests passed, including populated schema-14 preservation and upgrade
collision rollback. After correcting resource cleanup in a query-plan test,
the complete fork gate passed in 1,177 seconds, including the tagged backend
suite. Commit `0dcc2c3da` also passed Build, Lint and image publication in CI.

The retained schema-77 archive reopened in 114 seconds. Inspection found its six
permitted collections in 0.35 milliseconds and traversed 757 bound targets in
41 requests totaling 116 milliseconds; the slowest request took 5.2 milliseconds.
These unstarted retained searches produced no eligible candidates. Job, detail,
publication, target and migration-history counts were unchanged, and no source
jobs were admitted. The report is
`.local/native-discovery-detail-admission-20261004/inspection.json`.
Production and its scraper launchers remain unchanged. Actual source coverage,
verified staging release and the broader transition remain open.

## Sole native application and build — 2026-10-04

Removed the v2.5 source, dependency tree, embedding and UI selector from the
native branch. `make pre-ui`, `generate`, `ui`, formatting, validation and UI
archives now target v3; existing v3 command names refer to those same targets.
CI installs and packages one frontend, including the share/offline HTML inputs
in its build cache. Both Docker recipes copy only native UI sources and login
assets. The development-tag update now pushes only its named floating tag,
without force-pushing every local tag. The frozen compatible tag is unchanged.

Native file intake workers, preview/cover generation, scene selection, downloads,
segmented streaming, shares and offline worker headers no longer depend on a UI
flag. Required encoder checks, permissions and public media/share URLs remain.
JPEG endpoints still have their encoding fallback. Login translations now come
from v3; all 41 login objects matched the retired source and generation changed
no translated output. The obsolete CLI flag is rejected. No schema change was
needed; the verified archive remains at native schema 1000077.

The actual native build and binary compiled with the old UI directory absent.
The isolated HTTP check used a fresh temporary native database and verified
owner authentication, login/locales, main and deep-link assets, proxy prefixes,
share assets and isolation, offline entry/worker/manifest, icons and default
download capabilities. It stopped its server and removed the test database.
The native ZIP matched the built HTML and included all 769 archive entries.
The shared standard/CUDA Docker frontend stage also built successfully on
Alpine in 57 seconds, using its locked pnpm 12.4.2 toolchain.

UI embedding and manager/configuration tests passed. An initial API run used an
insufficient eight-minute package timeout and ended while translation migration
fixtures were still progressing. The complete gate passed in 1,187 seconds using
the repository's standard twenty-minute package timeout: 582 UI tests, 484
producer tests, Go lint with zero issues and the tagged backend suite. Build,
Lint, browser tests and image publication also passed for `dc774c4f5`. Reports are under
`.local/native-discovery-client-20261004/native_ui_retirement_*`.

Contributor, architecture, feature and deployment documentation now describes
the sole native application. The deployment runbook was checked against the
wrapper's actual digest-pinned native preview workflow. Its entrypoint does not
inject the retired flag. The compatible production Quadlet retains its existing
flag until the prepared native replacement is activated at the reviewed cutover.
No production unit, image, database or scraper was changed. Remaining API/plugin
adapters, client conversion, import/reconciliation, backup/restore and production
cutover gates are still open; this does not complete the transition.

## Native saved-filter contract — 2026-10-04

Saved filters now read and write canonical ASTs only. Removed the lossy flat
projection, empty legacy string field, ignored legacy writes to complex filters,
config-backed default-filter operations and obsolete manual migration task. The
native UI no longer sends or decodes flat saved/default filters, and its settings
page no longer offers that task. Default filters continue through the native
revision-checked service. No new database schema is required.

Historical database/configuration promotion and JSON imports still accept old
criteria at their import boundaries. Native JSON exports carry only the AST;
the runtime model no longer carries a second representation. The compact URL
codec and historical reconciliation helpers remain. Malformed historical default
criteria fail rather than becoming an empty native filter.

Caller inspection found no filter-operation calls in 43 live n8n workflow
definitions, 582 installed plugin source files, the catalog source or the
retained catalog/title plugins. The wider source audit found the old manual
`saved_filter_experiments.py` transformer and sibling upstream client checkouts;
these are outside the native caller contract. No configured service, timer,
workflow or proxy references them. The standalone TV checkout is documented as
a behavioral reference; built-in native TV already uses canonical filters.
The old transformer remains a compatible-release diagnostic, not a native
automation entry point. Current v3 callers use canonical saved filters and
`configureDefaultFilter`. Generation and the native build pass.
All 583 UI tests and the retained plugin/application contract checks pass. Real
GraphQL tests exercise nested/repeated criteria, edits, rejected legacy inputs,
invalid-write rollback and explicit clearing. SQLite tests restore native and
historical JSON into a separate database, preserving labels, exclusions and
canonical precedence. Existing migration/restart/conflict tests also pass.

The saved-filter full fork gate passed in 1,203 seconds. The combined filter
and playback gate also passed in 1,184 seconds. Production, scraper launchers
and the frozen compatible release remain unchanged. Reports are in
`.local/native-discovery-client-20261004/saved_filter_native_*`.

## Single native playback catalog — 2026-10-04

`Scene.sceneStreams` now exposes the native direct and segmented playback
catalog. Removed the duplicate `sceneStreamsV3` field, unused legacy root query,
parallel upstream catalog and its endpoint/MIME shims. Native fragments and
Apollo cache tests use the field directly. The built-in TV, lightbox, shared and
offline players retain their existing source shape. Public HTTP media paths
remain available, and shares keep their original-file policy.

The native catalog implementation is unchanged apart from function names and
comments. Codec/GOP checks, rotation handling, source MIME types, generated
transcodes and streaming-resolution limits remain in the shared service. Actual
workflow/plugin/host inspection found no stream callers needing conversion;
sibling upstream/reference checkouts remain outside the native release contract.

Generation and the native build pass. All 583 frontend tests and the current
application/plugin contract checks pass. Real SQLite/GraphQL tests verify direct
WebM MIME, resolution caps, proxy prefixes, API keys, user signatures, missing
authentication, empty scenes and rejection of removed entry points. The stream,
signed-URL and original-share regressions passed in 109 seconds.

All 70 focused scene-detail, lightbox, sharing and source-switching checks passed
in Chromium and WebKit in 196 seconds, using the cached Playwright 1.63.0
Resolute container with network access disabled. The initial host attempt could
not launch because browser executables were absent. The full gate for this
increment passed in 1,184 seconds: 583 UI tests, 484 producer tests, Go lint
with zero issues and the complete tagged backend suite. Production and the
verified schema-77 rehearsal are unchanged. Reports are under
`.local/native-discovery-client-20261004/native_stream_contract_*`.


## Native plugin manifest and discovery boundary — 2026-10-04

Only manifests declaring `apiVersion: 3` now enter the executable plugin cache.
Removed unversioned decoding, settings adaptation and legacy discovery/settings
GraphQL types and fields. The old script/CSS injection fields, concatenation
routes and automatic CSP origins are gone. Native ESM asset URLs and explicit
CSP declarations remain. The native settings, jq, preview, operation, task and
notification contracts are retained; historical JSON-text mapping overrides
still read without rewriting saved configuration.

Rejected manifests are reported through `pluginLoadErrorsV3` and an Alert in
Settings → Plugins, including their relative path and load error. They cannot
register hooks or serve assets. Reloading after a repair/removal clears the
error; package changes refresh the diagnostics. The old DOM-patching React
example was replaced with a native ESM example. Raw, RPC and server-side
JavaScript examples now declare version 3 and pass strict manifest validation.

The read-only caller audit covered 43 live n8n workflow definitions, 52 host
scripts, the plugin sources and 582 installed plugin/environment files. The sole
installed manifest is Catalog Metadata 1.14.1, already version 3. Its installed
and repository code use their own v3 settings client. Old `stashapi` helpers
remain in an unused PythonToolsInstaller environment, without a loading manifest;
no active consumer of the removed plugin endpoints was identified. Two host
scripts still call synchronous bulk edit mutations, so that separate retirement
must convert those callers before removing their endpoints.

Generation and the native application build pass. Package tests cover rejected
versions, manifests and hooks, diagnostics reload, native settings and declared
operations. The API checks pass, including actual GraphQL settings, retained
assets, removed endpoints and after-success edit/deletion notifications. All 583
frontend tests and the retained v3 contract checks pass. Eight Chromium/WebKit
checks cover desktop/mobile plugin navigation, error display, constrained mapping
choices and nonmutating previews. The full fork gate passed in 1192.797 seconds,
including 484 producer tests, Go lint with zero issues and the tagged backend
integration suite (SQLite: 1097.032 seconds). Reports are under `.local/native-discovery-client-20261004/native_plugin_retirement_*`.

No production plugin, service, scraper or configuration was changed. Schema
1000077 and the verified archive rehearsal remain unchanged. Remaining client
conversion, import/reconciliation, native review, general durable notifications,
backup/restore and cutover work are still open.


## Native bulk update acknowledgment — 2026-10-04

Consolidated the eight entity bulk mutations into `bulk*Update` with one
`BulkUpdateResult`: explicit IDs commit atomically and return `COMPLETED` plus
the updated IDs; filter selections return `QUEUED`, a job ID and selected count.
Removed duplicate `*UpdateJob` fields, synchronous entity-refetch adapters,
movie bulk aliases and their unused input. The separate file-mtime operations
retain their current scalar contract. This does not make the existing general
bulk queue durable; that plan requirement remains open.

All seven native edit sheets and their GraphQL operations use the new result.
Completed edits refresh immediately; queued work keeps completion monitoring
outside the sheet. Aliased mutations resolve the proper response key. Failed
or cancelled jobs still refresh potentially changed records. Submission errors
and contradictory results keep the draft open with visible feedback, and the
admission message now explicitly says “Bulk update queued.”

The caller audit found two manual host clients that require conversion. Native
replacements for `tag_stash_collections.py` and `stash_autotag_with_aliases.py`,
plus a shared stdlib client, are staged in `integrations/library`. They use
canonical filter ASTs, bounded ID-sorted pages, existing aliases/path boundaries,
explicit ignored-performer handling, and verified completion counts. Queued or
invalid results stop later batches. Keys come from configuration instead of
embedded source. Compatible live helpers remain unchanged until the coordinated
cutover. The installed catalog plugin and audited n8n definitions do not call
the removed bulk fields.

Generation and embedded builds pass. Focused real SQLite/GraphQL tests cover
all eight empty-result contracts, retired fields, atomic rollback, notifications
after commit, selection versus execution, custom-field preservation and partial
dates. The frontend gate passed 583 tests and current native contracts. Eight
helper tests pass and now run in CI/the fork gate. Sixteen Chromium/WebKit
checks pass for mobile/desktop completion, queue admission, error handling and
retaining the open draft. The full fork gate passed in 1192.6 seconds, including
484 producer tests, Go lint with zero issues and the tagged backend integration
suite (SQLite: 1096.965 seconds). Reports and browser artifacts are under
`.local/native-discovery-client-20261004/native_bulk_*` and
`bulk-browser-results-*`.

No production deployment or database migration occurred. Schema 1000077 and
the verified archive rehearsal remain unchanged. Remaining client retirement,
imports, native review, general durable notifications, backup/restore and cutover
continue under the full transition plan.

## Native entity query contract — 2026-10-04

The eight entity list queries and scene/image duplicate queries now accept only
their filter-expression arguments. Removed the old flat arguments and the
scene/image/performer integer-ID aliases. Pagination/search remain separate,
and string `ids` retains explicit selection. Updated the native query documents,
gallery image viewer, offline refresh and scene/performer merge clients.
Offline refresh and merge loading continue to skip empty ID selections.

Scene and image aggregates now use the repository's filtered aggregate path.
This corrects page-only scene totals and zero image totals under expression
filters. Megapixel division now retains fractions. Filtered totals include
secondary files; explicit-ID lookups keep their existing primary-file semantics.
File/folder nested filters, movie aliases and internal object-filter models
remain separate retirement work. See [native queries](native-queries.md).

The caller audit covers source/installed plugins, host helpers, wrapper code,
producer code and 43 live n8n workflow definitions. Catalog Metadata 1.15.0 is
staged at CommunityScripts `native-api-transition` commit
`07112bf2d05c302bca61e6e64b72d8668076c5ac`. It uses native expressions and group
requests, preserves existing group imports, fixes its unapplied group-name
filter, retains group URLs and removes the empty-result full-library image
retry. Its published package branch and installed compatible copy are unchanged.
The native manual helper replacements were staged in the preceding bulk commit.

Generation and embedded builds pass. SQLite/GraphQL tests cover all eight list
queries, boolean expressions with search, paging, explicit IDs, rejected old
arguments, gallery membership, full-selection/fractional aggregates and both
duplicate filtering modes. All 583 frontend tests and native contracts pass.
The staged plugin passes 108 tests; ten actual generated plugin requests and
their variables validate against the native schema. All 24 Chromium/WebKit
merge-dialog checks pass at 320, 390 and 1280 pixels, with strict checks on the
selected string IDs. The full fork gate passed in 1,283.1 seconds, including
backend generation, frontend/producer/helper checks, zero Go lint issues and
the tagged backend integration suite. Reports are under
`.local/native-discovery-client-20261004/native_query_*`.

No production service, database, installed plugin or scraper was changed. Schema
1000077 and the verified archive rehearsal remain unchanged. The full transition
still requires remaining client/model retirement, imports, native review,
durable notifications, backup/restore and coordinated cutover verification.

## Native group contract — 2026-10-04

Groups now have one native API representation. Removed Movie queries,
mutations, nested relationships/counts, scrape types and export-request aliases.
Scene edits and filename-parser results use `groups`/`group_id`, and background
scene update payloads preserve group IDs and scene indexes in that same shape.
Group edits emit only their Group notification after commit; a queued operation
leaves notification ownership to the worker instead of sending a duplicate
Movie hook inside its transaction. Native manifests reject the old triggers.

Provider movie-shaped data is accepted at the scraper input boundary and
converted into native groups once. Explicit groups retain precedence, matching
runs once and all URLs survive. Filename parsing accepts `{group}` and retained
historical `{movie}` patterns, returning only the native relationship shape.
Historical export files, internal import representations and saved configuration
keys remain separate migration inputs. StashDB protocols are unchanged.

The caller audit includes 43 n8n workflows and the installed/source plugins.
The retained native catalog client already uses group operations on its staged
branch; ten real plugin requests and variables validate against this schema.
The filename-parser document and mutation invalidation are converted. Focused
SQLite/GraphQL tests cover native create/update/delete, after-commit hooks,
atomic bulk rollback, queued operation ownership, scene membership and rejected
aliases. Provider/parser and scene-notification relationship tests also pass.
Generation and embedded client builds pass. The final full fork gate passed
in 1,230.0 seconds, including 583 frontend, 484 producer and eight helper tests,
zero Go lint issues and the tagged backend integration suite (SQLite: 1,114.0
seconds). Reports use `native_group_*` under
`.local/native-discovery-client-20261004/`.

The production instance, database, installed plugin and active scrapers remain
unchanged. This completes another contract increment, not the full archive
transition. Backup/restore, live producer activation and production cutover
remain open alongside the other plan gates.

## Portable archive foundation — 2026-10-04

The independent `stash-archive` command exports native SQLite snapshots,
original artwork and explicitly declared configuration/operating-state
components. The versioned format streams its inventory and stores deterministic
compressed chunks by SHA-256, retaining every database value and binary source
document. The manifest is published last. Import requires a new directory,
checks every object, database and original-artwork reference, and writes its
restore receipt only after successful verification and filesystem durability.
Offline list/inspect/verify/import commands require neither a running server
nor the old plugin. See [portable archives](native-archive-format.md).

Thirteen temporary SQLite regression tests cover committed WAL state,
merged/manual performers, ordered album attachments, binary documents, pending
producer events and interrupted-operation bytes. They also cover changed inputs,
missing/corrupt artwork and chunks, altered chunk order, malformed/duplicate
inventories, foreign database/producer identities, broken foreign keys, space
reserve failures and bounded offline output. The package runs in the fork gate
and backend CI.

A read-only inventory found all 238,574 original artworks referenced by the
verified native rehearsal database: 36,496,053,336 bytes, with none missing or
invalid. A complete export/restore rehearsal is running against that copied
20,265,979,904-byte database. Its terminal reconciliation and native repository
reopen remain pending; the inventory alone does not prove a successful restore.
The tool retains 50 GiB of free space by default.

Coverage is explicitly **declared components**. Coordinated live server and
producer snapshots, acknowledged-event verification, matching media manifests,
S3 publication and the remaining restore/cutover drills are still required.
This increment does not change the production backup or deployment.

## Backup producer receipt verification — 2026-10-04

`stash-archive verify --producer-origin` now verifies capture/file acknowledgements
and source-run admissions after restoring and checking every archive component.
It requires an outbox for each registered producer and binds the resulting proof
to the archive manifest and exact library/outbox hashes. Missing or conflicting
native acknowledgements fail verification. Unacknowledged events retain their
original bytes, including when the server accepted them before a response was
lost. Sending and review states are preserved rather than reset.

The admission check reconstructs each frozen request from its retained template
and window, verifies the producer's original byte digest, then checks the native
normalized digest and original run association. Coalesced requests keep separate
receipts and unsubmitted windows remain queued. A later run state never turns an
admission receipt into scrape-completion proof. The check also rejects a downgraded
producer schema that would hide retained source requests.

Thirty-two Python tests pass using actual Outbox/RunQueue transitions, the native
receipt table definitions and the complete verified archive CLI. A shared six-case
digest corpus also passes against the backend's actual hashing function, covering
offsets, fractional times and year limits. The core storage format is unchanged.
Reports use `native_archive_receipts_root` and
`native_archive_run_digest_go` under `.local/native-discovery-client-20261004/`.
Package-scoped Go lint also passes with zero issues.

The preceding full gate passed all frontend, producer, helper, archive and lint
checks and all other Go packages. API and SQLite exhausted their aggregate
20-minute package timeout during concurrent archive I/O, without assertion
failures. Both unchanged packages passed their targeted retry with a 45-minute
budget: API in 1,443.8 seconds and SQLite in 1,559.7 seconds. The full-library
export/restore/reconciliation rehearsal remains running.

This verifies ingestion and admission receipts only. Enrichment/discovery journal
acknowledgements, matching filesystem/config/download state, media manifests,
remote publication and complete restore/cutover verification remain required.
Production services and backup publication are unchanged.

## Producer job journal backup verification — 2026-10-04

The portable archive receipt check now includes enrichment, listing-page and
detail-verification journals. It checks each immutable job definition, retained
claim/attempt owner, checkpoint, publication/comparison and failure acknowledgement
against native history. Native jobs may have advanced beyond an older producer
snapshot; historical acknowledgements remain valid without renewing the old lease.
Pending bodies retain their exact bytes and reservations, including review work.
A matching native receipt after a lost reply is reported separately from a later
worker's completion after the original attempt expired. Neither case changes
producer state or discards unacknowledged source evidence.

Listing acknowledgements require the retained native page. Metadata checkpoint
acknowledgements require the current native checkpoint body or the published
enrichment release receipt. The native database's schema validation still owns
the release/capture provenance proof; receipt correspondence does not reconstruct
a deliberately released transcript. The archive format and runtime used by the
ongoing full-library rehearsal are unchanged.

All 45 archive tests pass, including actual producer journal transitions, stale
snapshot rejection, lost responses, retry receipts, terminal observations and
changed payload/history rejection. The affected Go API package passes lint with
zero issues. The verifier also runs between requests in the existing real
producer/Go API tests for listing, enrichment and detail execution/publication;
all affected scenarios pass in 259.1 seconds. Evidence is under
`.local/native-discovery-client-20261004/native_archive_job_journal_final.json`,
`native_archive_job_api_lint.json` and `native_archive_real_journal_host.json`.

The receipt proof's coverage is `capture-file-run-and-job-receipts`. Coordinated
producer/download/config/filesystem snapshots, a matching media manifest, remote
publication, the full restore drill and production cutover remain outstanding.
Production services and backup publication are unchanged.

## Native snapshot validation before recovery — 2026-10-04

`stash --verify-native-snapshot PATH` now checks a closed native database before
application/configuration initialization. It requires the exact clean current
schema, checks SQLite integrity and foreign keys, and reuses every native startup
lineage/domain/provenance validator, including released enrichment proofs. It
holds a read transaction, verifies that the database's identity/size/timestamp and
SHA-256 remain unchanged, and returns a JSON receipt bound to those exact bytes.
Nonempty WAL or rollback-journal inputs require a SQLite-aware snapshot first.

The command never calls the normal application database open or file-deletion
recovery path. Real crash fixtures before and after a deletion commit retain
their staged media, gob journal and database bytes unchanged. Pending deletion
markers are reported; `filesystem_recovery_verified` remains false. A deliberately
altered enrichment release proof is rejected even with intact SQLite foreign
keys and guards. Missing/foreign/dirty/newer/older schemas, broken references,
changed guards, uncheckpointed WAL, cancellation and symlinks are also exercised.

All new SQLite and command tests pass in 80.6 seconds; the affected packages pass
lint with zero issues. The standalone binary builds, and direct empty/missing
input checks exit without creating configuration or starting the application.
Evidence is under `.local/native-discovery-client-20261004/` with labels
`native_archive_snapshot_final`, `native_archive_snapshot_lint_final`,
`native_archive_snapshot_binary_final` and `native_archive_snapshot_binary_smoke`.

This provides the native semantic validation step for the coordinated restore;
connecting its receipt to archive publication, matching filesystem/media/producer
state and S3 publication remain required. The full-library export/restore drill
is still running. Production remains on the frozen compatible release.

## Archive-bound native validation — 2026-10-04

`stash-archive verify --native-validator /path/to/stash` now verifies native
schema/provenance after the complete transport restore and binds the returned
database SHA-256, byte count and schema to the archive inventory. Its proof
includes the same archive UUID and canonical manifest digest used by the producer
receipt proof. Supplying `--producer-origin` runs both checks against one temporary
restore, avoiding a second database/artwork copy. Success is printed only after
all requested checks pass; failed verification discards only that temporary copy.

The validator path is explicit local configuration, never supplied by an archive.
Invocation uses no shell, bounds both output streams, and enforces a configurable
deadline. Invalid options fail before the restore. Malformed, duplicate-key,
unsupported, mismatched, failed, oversized or stalled responses cannot produce a
success proof. Native database validation still reports filesystem recovery as
unverified, including when pending deletion markers are present.

All 51 Python archive tests pass in 61.1 seconds. Command tests pass in 24.7
seconds and lint reports zero issues. A real native SQLite fixture passes through
the portable CLI and the actual Stash main/flag path, with both native and producer
proofs bound to its artifact. Removing a required native guard leaves ordinary
SQLite/transport checks valid but makes the combined command fail without a
success result, application configuration or a leftover temporary restore.
The existing nonempty producer fixture also exercises the combined protocol.
Evidence is under `.local/native-discovery-client-20261004/` with labels
`native_archive_proof_python_final`, `native_archive_proof_go` and
`native_archive_proof_lint`.

The full-library rehearsal continues on its frozen transport runtime. Coordinated
download/configuration/filesystem/media snapshots, S3 publication and production
cutover are still required; this commit changes no production services.

## Producer-first backup checkpoints — 2026-10-04

The export path now snapshots every declared download archive before any
producer outbox, then snapshots the native library. All SQLite copies are
captured before artwork/library compression starts. Previously, the library was
captured first and producer queues were copied after artwork packing, allowing
newer queue acknowledgements to refer to native receipts absent from the backup.
Component-list order no longer controls this causal snapshot order.

`stash-archive export --producer-origin ORIGIN` checks every registered
producer's ingestion, source-admission and job receipts against those closed
copies before packing or sealing a manifest. Missing queues, an incorrect origin
or inconsistent acknowledged state abort the export and remove only its new
output. This does not discover all worker download archives or establish the
remaining config/filesystem/media boundary; coverage stays declared components.

All 57 archive tests pass in 37.4 seconds. WAL-backed fixtures commit additional
deliveries between archive, queue and library snapshots, then again during
packing. The restored artifacts retain the correct older prefixes and their
matching acknowledgements. Corrupt archives stop before queue/library snapshots;
invalid receipt boundaries cannot reach packing or manifest publication. All 29
gallery-dl lifecycle tests pass in 4.6 seconds, including an independent SQLite
reader proving that capture/file events are committed before `archive.add`.
The actual native command/portable verifier integration passes in 8.9 seconds.
Evidence is under `.local/native-discovery-client-20261004/` with labels
`native_archive_ordered_checkpoint`, `native_archive_download_order` and
`native_archive_ordered_go`.

The full-copy export on the separate frozen rehearsal runtime completed in
8,744.2 seconds under concurrent validation and idle I/O priority: 238,575
artifacts, 56,762,033,240 uncompressed bytes and 41,468,466,273 compressed object
bytes. Restore and full semantic reconciliation are still running; export alone
does not establish a successful round trip. Production remains unchanged.

## Native server checkpoint exclusion — 2026-10-04

`sqlite.WithNativeCheckpoint` now holds the same SQLite writer exclusion used by
file-deletion staging and recovery while a capture callback copies state. It
requires an existing regular database, exact clean native schema/lineage and
regular SQLite sidecars. The callback receives the matching journal directory
and committed deletion IDs. It never calls `Database.Open`, configuration
initialization, migration or deletion recovery, and never commits application
changes. Its dedicated driver retains native functions/collations while omitting
the normal close-time optimization.

The guarded `CopyDatabase` method uses the online backup API through a separate
read-only source connection. It preserves committed WAL state, supports escaped
paths, refuses existing outputs and stale destination sidecars, permits disk
reserve checks before allocation and each step, handles cancellation, and flushes
the finished copy. A retained checkpoint cannot copy after its callback ends.
The guard remains held until a copy already in progress finishes.

Tests pass in 44.9 seconds; SQLite package lint reports zero issues in 12.5
seconds. Real subprocess crashes before/after deletion commit preserve their
original gob journals and staged media byte-for-byte while the matching native
copy and raw filesystem pieces are captured. Native semantic validation reports
the expected deletion-marker count without recovering those files. A concurrent
real repository deletion cannot stage bytes until capture releases its guard;
the captured database and media retain the earlier state. Invalid sources,
callback/space failures, cancellation, retries and output protection also pass.
Evidence is under `.local/native-discovery-client-20261004/` with labels
`native_server_checkpoint_tests` and `native_server_checkpoint_lint`.

This is the server-side capture primitive. Production export still needs the
configuration/recovery-tree capture coordinator, portable identity/path rebinding,
complete media/download inventory and S3 publication. No production services or
backup publication changed. The full-library restore remains active.

Checkpoint cancellation also retains writer exclusion until capture finishes.
The SQL guard's lifetime is deliberately independent of request cancellation;
otherwise `database/sql` could roll it back while a filesystem copy was still
running. Copy/query work still uses the request context, and a cancelled callback
cannot produce success. A regression test cancels during capture and proves an
independent writer remains blocked until the callback returns. The complete
checkpoint suite passes in 45.9 seconds and lint reports zero issues in 11.4
seconds (`native_server_checkpoint_cancel.json` and
`native_server_checkpoint_cancel_lint.json`).

## Portable deletion recovery state — 2026-10-04

`NativeCheckpoint.CopyDeletionSnapshot` now packages the raw deletion journals
and their actual staged/replacement/trash trees while retaining the checkpoint's
writer exclusion and commit markers. Explicit source roots bound reads. The
single ZIP component stores each hard-linked file body once, hashes its contents,
and preserves byte filenames, nested staged directories, trash aliases and
unfinished copies. Unknown or corrupt journal entries, unsafe paths, special
files, observed changes and disk-reserve failures prevent publication.

`file.RestoreDeletionSnapshot` verifies that component against the copied
database's commit markers and restores it into a new isolated directory. It
recreates hard links and rebinds all journal paths and inode identities. Missing
expected identities remain explicitly unmatchable; existing replacements stay
conflicts. Recovery is not executed by extraction, and the original absolute
paths are never used as restore destinations. The coordinator still owns full
media/library root relocation, journal placement and activation.

The tests exposed a recovery issue when an interrupted cross-filesystem trash
copy is restored onto one filesystem: the original rename can now succeed while
an unfinished private copy remains. Recovery now removes that partial copy only
after verifying and durably publishing the original destination. Unexpected
files in the copy wrapper remain intact and keep the operation pending.

The full file-package suite passes. The deletion/checkpoint suite passes in
82.7 seconds, including actual process crashes before and after deletion commit,
then startup recovery at new media/trash roots. Original journal and staged-media
hashes remain unchanged. Nested rollback/commit, replacement conflicts, partial
and completed copies, byte filenames, hard links, symlinks, missing identities,
corruption, cancellation and reserve failures pass. The additional unexpected
wrapper-content regression also passes. Lint reports zero issues in 11.0 seconds.
Reports are `native_deletion_snapshot_file_final`,
`native_deletion_snapshot_final`, `native_deletion_snapshot_wrapper_protection`
and `native_deletion_snapshot_lint_final` under the October 4 rehearsal directory.

This completes the recovery-component capture/rebinding primitive, not the
assembled production backup. Configuration capture, complete producer/media
inventory, remote publication and the final restore/cutover gates remain open.
The separate full-library restore is still running; production is unchanged.

## Full portable library restore verified — 2026-10-04

The full-copy rehearsal has completed successfully. The exported archive contains
238,575 artifacts: the native library and all 238,574 original artwork files.
It contains 56,762,033,240 uncompressed bytes in 41,468,466,273 compressed object
bytes. Restore verified every artifact, then compared all schema objects and
36,974,038 typed rows across 280 native/internal SQLite tables against the frozen
source. The matching native validator subsequently passed the full schema,
integrity, foreign-key and retained-provenance checks. Its receipt is bound to
the archive UUID, manifest digest and exact library bytes.

The restored library then opened successfully through the actual native
repositories at schema 1000077, without starting an API or workers. Retained
discovery records, the selected detail, all eight activation receipts, 757 bound
targets and 188 recovery bindings passed the reopen checks. The copied fixture
has no registered producers, ingestion receipts or pending deletion markers;
nonempty protocol/recovery cases are covered by separate integration fixtures.
This rehearsal verifies the declared library/artwork round trip, not the still
unfinished complete production backup and cutover.

Measured export/restore/comparison times were 8,744.2 / 4,522.8 / 508.7 seconds
under concurrent validation and idle I/O priority. Native semantic validation
took 733.0 seconds and repository reopening took 179.4 seconds. Reports are
`reconciliation.json`, `native-verification.json` and `reopen.json` under
`.local/native-backup-rehearsal-20261004/`. After all processes exited, only the
disposable `restored/` tree was removed, reclaiming about 54 GiB. The compressed
bundle, source snapshots and reports remain. Production is unchanged.

## Application checkpoint API and portable export — 2026-10-04

The native server now has an application-authenticated checkpoint API, consumed
by `stash-archive export --server`. It captures the main settings, separate
runtime overrides, configured TLS assets, deletion recovery trees and native
database. Private settings remain in private components. Requests have stable
UUIDs and a sealed manifest written last; matching retries rehash and reuse the
original capture. Changed requests, overlapping captures, incomplete directories,
redirected cache directories, corruption and disk-reserve failures are rejected.
New failed captures remove only their own output.

`CaptureNativeSnapshot` pins a WAL read transaction while holding the database
writer guard. Configuration/recovery capture runs under that guard; the large
database copy then runs with ordinary writers released while retaining the same
read view. Configuration locking follows database exclusion and ends before large
recovery/database copies. The default 50 GiB reserve is checked on both the output
and live database volumes, including the latter's retained WAL growth. Non-WAL
sources are rejected. A cancelled operation cannot release writer exclusion while
its configuration/filesystem callback is still active.

The Python exporter snapshots declared download archives and producer outboxes
before requesting the server view. It verifies response identity, component
digests and deletion markers, then receipt correspondence before packing. Server
components are checked again during packing, and the downloaded library does
not undergo a second SQLite copy. Producer-aware server exports require an
explicit origin; API keys remain in headers and redirects are refused.

The checkpoint/configuration tests and actual Go HTTP → Python CLI export/import
→ native verification/recovery-component restoration pass in 38.0 seconds.
All 62 portable archive tests pass in 27.9 seconds; lint reports zero issues in
19.1 seconds. The fixed-read test proves independent writers remain blocked
during filesystem capture but can commit during subsequent copy steps, while the
copied database keeps its original rows. Configuration updates remain excluded
through asset capture and resume after a failed callback. Reports are
`native_server_checkpoint_final_regressions`,
`native_server_checkpoint_final_python` and
`native_server_checkpoint_final_lint` under the October 4 client rehearsal
directory.

This is implemented development code, not an enabled production backup job.
Publication-aware checkpoint cleanup, complete external configuration/worker
inventory, matching ordinary-media coverage, restore root bindings and S3
publication remain. Server checkpoint files currently remain after download;
scheduled capture must wait for retention integration. Production-scale writer
exclusion, copy duration and WAL growth still need measurement. No production
deployment, branch merge or frozen release change has occurred.

## Checkpoint release after archive publication — 2026-10-04

An authorized publisher can now release a sealed server checkpoint's temporary
components after verifying its durable enclosing archive. The release binds the
exact checkpoint manifest digest to the portable archive UUID and manifest
digest. The server records that binding atomically without replacing another
writer's release, and flushes it before deleting any listed component. The small
checkpoint/release records remain in the private cache; they must survive
restarts and cache relocation. Released UUIDs return 410 for capture or component
access, so retries cannot silently capture newer live state.

Repeating the same release completes interrupted cleanup and returns the same
receipt. A different archive binding, changed checkpoint digest, corrupt release
record or nonregular component replacement is rejected. Unrelated files remain
untouched. Reading a release receipt proves the retained binding, not completion
of a previously interrupted cleanup. Normal export/download never releases a
checkpoint automatically.

The Python publisher helper validates the complete inventory, saved server
report and all required checkpoint component digest/size bindings before
requesting release, then checks the returned receipt. It does not claim to
verify remote storage; production callers must finish object verification and
the master-manifest commit first. This separation permits retrying cleanup after
a lost response without republishing or recapturing data.

The Go checkpoint tests pass in 20.9 seconds, including interrupted removal,
fresh-manager retries, preservation of unexpected files, permanent rejection of
recapture, competing archive bindings and real HTTP/Python interoperability.
That integration exports a durable private bundle, fully restores it, releases
the server components, retries the release and verifies the restored native
database/recovery component. All 64 Python archive tests pass in 21.1 seconds,
including wrong checkpoint, missing/replaced components and mismatched release
receipts. Final lint reports zero issues in 9.6 seconds. Evidence labels are
`native_checkpoint_release_go`, `native_checkpoint_release_python` and
`native_checkpoint_release_lint_final` under the October 4 client rehearsal
directory.

Live S3 publication and automatic cleanup of unsealed abandoned captures are
still outstanding. No production backup cadence, storage policy, current
manifest, retention tags or running services changed.

## External filesystem checkpoint coordination — 2026-10-04

The application checkpoint API can now hold the native writer guard for a
bounded host confirmation after configuration/deletion capture. A streamed
challenge binds the request digest, checkpoint UUID, fresh token and expiry.
The authorized host supplies evidence for its own filesystem view through a
separate confirmation route. The server seals that record before releasing
ordinary writers for the large database copy. It never executes a requested
provider command. Timeouts, cancellation and failed stream delivery remove the
unsealed output and release the guard.

Matching confirmations return the same receipt during the copy and from the
sealed component after restart. Changed evidence or a token from an abandoned
attempt is rejected. The Python client verifies the challenge, acknowledgement,
terminal stream event and downloaded component. Replaying a sealed checkpoint
does not invoke the provider again. The record is bounded, preserves large
filesystem identifiers without float conversion, and is removed by the existing
publication-aware component release.

All checkpoint manager/API tests pass in 31.5 seconds, including a separate
SQLite writer blocked during confirmation, timeout/cancel recovery, stale-token
rejection, receipt tampering, large identifiers and actual Go HTTP/Python
export/restore/release. The HTTP test uses a temporary view as a provider
stand-in; it is not a ZFS snapshot test. All 68 Python archive tests pass in
28.9 seconds. Lint reports zero issues in 11.5 seconds. Evidence labels are
`native_checkpoint_boundary_final_go`, `native_checkpoint_boundary_python` and
`native_checkpoint_boundary_lint_final` under the October 4 client rehearsal
directory. The preceding checkpoint-release commit passed Build, lint and GHCR
publication on its exact revision.

Actual immutable media snapshots, producer publication barriers and original
artwork preservation remain to be integrated. The host must acquire its producer
barrier before the server's writer guard to avoid a producer/server lock cycle.
The confirmation is coordination evidence, not independent proof of its caller's
filesystem claims; declared coverage remains database/configuration/deletion
recovery. No production deployment, scheduled backup or storage policy changed.

## Artwork publication and cleanup exclusion — 2026-10-04

Filesystem artwork writes now publish a complete, flushed replacement inode and
close the file before returning. Rewriting or repairing a checksum no longer
truncates an inode retained by a reader or hard-linked backup. Existing file
permissions and configured symlink targets retain the atomic-file helper's
behavior. Failed publication leaves the existing destination intact.

Orphan-artwork cleanup first checks references in a read transaction, then
rechecks only possible orphans under the native writer guard. Deletion uses the
persistent journal and normal transaction hooks. A concurrently added reference
survives cleanup, checkpoints exclude removal, and failed commits restore staged
artwork. Empty-directory cleanup now uses nonrecursive removal so files created
after the empty check cannot be erased.

The blob/filesystem/file suites pass in 2.7 seconds. Native SQLite deletion and
checkpoint regressions pass in 44.7 seconds. Cleanup tests pass in 8.5 seconds,
covering a real concurrent reference, an independent checkpoint guard, dry run
and rollback after actual staging. The first task test run lacked the required
migration registration import; that fixture setup was corrected before the
successful rerun. Lint reports zero issues. Evidence labels are
`native_blob_atomic_tests`, `native_blob_cleanup_tests`,
`native_blob_cleanup_final` and `native_blob_atomic_lint` under the October 4
client rehearsal directory.

The backup provider still needs to create and retain artwork pins. This change
makes that mechanism safe against native artwork writes and cleanup; it does
not establish a complete media snapshot or alter the running compatible server.

## Retained artwork provider and archive integration — 2026-10-04

The host-side `ArtworkPins` provider now captures real hard links during the
authenticated filesystem boundary callback. It uses a private cache on the
artwork filesystem, flat pin directories, flushed files/inventories and bounded
capture deadlines with the normal free-space reserve. Each record binds its
checkpoint UUID, one-use token, request digest and original source roots. Failed
attempts remove only their own output; existing attempts are never recaptured.

Portable export can now use these pins instead of live artwork paths. It checks
the manifest and inode inventory against the sealed server component, then
checks each packed artwork checksum against the fixed database. Retained bytes
survive later atomic replacement, unlinking and removal of the live source tree.
Link-count/ctime changes during packing are allowed only for retained artwork
with a verified expected checksum; ordinary live-file checks remain strict.

The publication-aware artwork release helper derives its association from the
verified archive, obtains the matching server release, and durably records the
local binding before unlinking any listed inode. Interrupted cleanup is
retryable. Small identity/release records remain; inventories are removed only
after durable cleanup completion. Unexpected files, altered records and changed
inodes cannot be silently removed. Export never releases pins automatically.

All 73 archive tests pass in 21.4 seconds. The real Go HTTP/Python integration
passes in 8.9 seconds: it captures actual artwork, removes the live tree, exports
and restores from the pins, replays the same server capture, and resumes pin
cleanup interrupted immediately before its completion marker. Lint reports zero
issues. Evidence labels are `native_artwork_pins_durable_python`,
`native_artwork_pins_durable_http` and `native_artwork_pins_lint` under the October
4 client rehearsal directory.

Media snapshots, producer barriers, full-inventory timing and daily-script/S3
integration remain. The installed daily backup script and running production
server are unchanged; this provider runs in the host backup tooling.

## Native producer filesystem publication barrier — 2026-10-05

Native gallery-dl jobs now hold cooperative shared locks through file download,
postprocessing, final publication, durable completion and archive updates.
Initialization, directory/finalization work and callbacks outside a file also
participate. Reentrant callbacks can finish a current file after lease loss
without reacquiring the gate; new work continues to validate its lease. Existing
destination-stem locks and after-completion ordering remain in place.

The host-side `PublicationBarrier` acquires exclusive gates and active locks
across explicitly inventoried worker lock directories in physical-identity
order. Waiting for existing downloads stops new mutations from entering.
Acquisition has a bounded timeout and releases every acquired handle on failure.
Explicit early release lets the host resume downloads after retaining immutable
views; context cleanup is idempotent. Lock files remain permanent shared objects,
and redirected, nonregular or replaced files/directories are rejected.

All 489 producer tests pass in 44.3 seconds. Three actual multiprocess lock tests
cover a waiting backup, existing/nested/new mutations, early release, acquisition
failure, timeout, cancelled waits and redirected lock files. All 30 gallery-dl
lifecycle tests pass, including lock checks inside downloads and every supported
postprocessor phase, skip repair and callbacks outside a file. Evidence labels
are `native_publication_lock_tests`, `native_gallery_publication_tests` and
`native_publication_producer_suite` under the October 4 client rehearsal
directory.

Host orchestration must acquire these barriers before Stash's database guard,
then release them after snapshot capture, before the large copy/upload. This
protocol does not pause the installed legacy scrapers, dedupe or unrelated
filesystem tools. Actual ZFS snapshot orchestration, worker inventory, existing
backup/dedupe exclusion and daily-script/S3 integration remain required before
cutover. No running worker, schedule or production service changed.

## Retained ZFS media and host capture coordination — 2026-10-05

`ZFSMedia` now creates an actual retained media snapshot during the native
filesystem handshake. It checks an explicit dataset GUID, physical mountpoint,
ZFS descendants and the Linux mount namespace. Child datasets or other nested
mounts fail closed. Private intent precedes creation; the snapshot has a unique
checkpoint/token name and properties, a retention hold, and a sealed record of
its dataset/snapshot GUIDs and creation transaction. Replay verifies that exact
view and its read-only path. Failed attempts retain inspectable identities and
never recapture newer files under an existing challenge.

Publication-aware media release validates the archived association, obtains the
matching permanent server release and records intent before removing its own
hold and exact snapshot. It never recurses, forces destruction or removes other
holds. Interrupted cleanup resumes after either hold removal or destruction;
small identity and release receipts remain. This helper requires the host to
verify durable media and archive publication first; it performs no S3 operations.

`HostFilesystemCapture` now acquires the inventoried native worker barriers
before requesting the server guard, then captures both artwork and media. The
checkpoint client releases those barriers before acknowledgement/large copying,
on sealed replay and on failures, including local setup failures. It retrieves
the sealed filesystem component first and validates retained views and the
original worker inventory before the large library download. The existing host
backup/dedupe exclusion must still surround the operation.

All 86 archive tests pass in 24.3 seconds. The actual Go HTTP/Python checkpoint,
export, restore and release fixture passes in 10.9 seconds, checking both fresh
capture and sealed replay free the real worker lock and validate retained views
before library transfer. API lint reports zero issues. An initial test selector
matched no Go test and is not verification evidence; the exact named fixture was
then run successfully. Final evidence labels are `native_zfs_archive_final`,
`native_zfs_host_http_final` and `native_zfs_host_lint` under the October 4 client
rehearsal directory.

The explicit real-ZFS probe created a unique disposable dataset with a 64 MiB
quota, captured its held snapshot in 0.68 seconds, edited the live file, verified
the original bytes and read-only rejection, reopened without recapturing, and
released/retried cleanup. The owned dataset and snapshot were removed. Small
evidence remains in `zfs-media-probe-20261005`; this isolates actual filesystem
behavior and does not claim a production archive/S3 restore. No production media
snapshot, service switch, delegation change or installed backup-script change
occurred.

The daily host publisher/restore tools still need conversion, complete worker
and external-configuration inventory, persistent staged producer components for
replay, a matching media-manifest binding and verified remote publication.
Abandoned capture retention and full-inventory pin/pause measurements also remain
before cutover. Existing compatible production and frozen release remain intact.

## Persistent external components and sealed-only retries — 2026-10-05

The host coordinator now prepares a private `ComponentStage` under the original
worker barriers. It snapshots all declared download archives before outboxes,
copies exact external profile/configuration bytes, and persists their original
paths, SQLite identities, sizes and hashes. The stage inventory is bound into
the native filesystem receipt and included in the portable archive. Export uses
these retained copies directly and verifies their actual packed hashes, while
retaining the existing native/producer receipt proof.

Only the original uninterrupted preparation may request a new native capture.
Request intent is durable before transport. Reopened stages and uncertain first
requests only GET an already sealed server checkpoint; missing/incomplete views
cannot trigger a later capture with old worker state. A lost response after
sealing reopens the original view. Changed component inventory, server request,
retained bytes or sealed manifest fails closed. `ServerCheckpoint.seal` obtains
the manifest and filesystem receipt without downloading the library, allowing
the host publisher to build its media manifest from the retained media view
before packing the metadata archive.

The component release helper validates the full archived stage association and
every declared component before requesting release. It records intent before
removing private copies, resumes interrupted cleanup, preserves unknown files,
and keeps small permanent manifests/receipts. It does not read or remove live
source files, acquire worker barriers for cleanup, or verify S3 publication.

All 96 archive tests pass in 26.7 seconds. New cases exercise real producer WAL
queues and SQLite snapshots, later live events/profile edits, restoration of the
original pending payload, lost responses, incomplete preparation, altered
inventory and tampering during packing. The real native Go HTTP/Python fixture
passes in 10.6 seconds: it seals before library download, exports/reopens using
the original download archive after live changes and deletion of its profile
input, restores exact bytes, refuses incomplete-archive release and resumes
cleanup interrupted after file removal. Evidence labels are
`native_component_stage_suite` and `native_component_stage_http` under the
October 4 client rehearsal directory. The preceding ZFS-provider commit's lint,
Build and image publication all passed.

Complete active-worker/configuration inventory, the installed daily publisher
and restore/audit conversion, matching media-manifest/S3 publication, abandoned
capture retention and production-scale pause/restore gates remain. Production,
installed scripts and schedules have not switched.

## Host backup tooling rollback baseline — 2026-10-05

The daily publisher, performer restore helper, backup audit, offline restore
tester, regression suite and installed policy documentation are now tracked in
`integrations/backup/host`. `integrations/backup/baseline.json` records their
original lengths and SHA-256 digests. All six copies match the installed files
byte for byte. No installed file, timer, cloud object or delegation changed.

All 103 existing host regression cases pass in 42.5 seconds from the repository
copies. The fixture blocks real subprocess/AWS access and uses temporary local
object stores. Evidence is `native_host_backup_baseline_tests` under the October
4 client rehearsal directory. This establishes the existing upload, publication,
restore and cleanup behavior as a rollback point before conversion.

These files still invoke the old catalog publisher, and their baseline tests
still use the installed old catalog package. That live dependency must be removed
in the next conversion; this baseline is not a completed native integration or a
second supported runtime. Historical backup restore formats remain input data,
independent of the retired v2.5 application contract.

### Host native publication and restore conversion — 2026-10-05

The tracked daily caller now seals the native checkpoint, external components,
artwork pins and ZFS media view before media scanning. It publishes a v3 media
manifest bound to the native archive UUID, checkpoint UUID and exact selection
digest. The same selection and filesystem receipt digest are packed inside the
archive. Real archive restore, native snapshot validation and complete producer
receipt checks precede upload. Host-owned boto3 publication requires the exact
full-object SHA-256, length and Standard storage class for every archive object;
it uses immutable conditional writes and independently checks lost replies.
Immutable per-run manifests precede the current JSON pointer and remote cleanup.
No AWS credentials or SDK enter the Stash server.

The existing host lock, media storage tiers, mounted/nonempty-source guards,
upload retry ledgers, pending/last-copy safeguards and cleanup order remain.
Media reads use the retained ZFS view; the returning-video guard uses the live
path so a file restored after capture cannot be marked obsolete. Dry runs never
create a native or filesystem checkpoint. Successful publication releases all
retained providers after their last use, including when later obsolete tagging
fails. Completed runs reclaim their own compressed objects and copied host
ledgers, retaining release receipts/manifests and preserving unknown files.

The media restore tool now validates native references before subset selection;
subsets cannot claim whole-library native coverage. The new native restore tool
audits remote SHA-256 metadata or downloads the complete bundle for isolated
content/native/producer verification. The existing audit can opt into these
checks with `--native-checksums`; otherwise it explicitly reports reference-only
native coverage. Historical text/v2 media inputs remain readable. Native restore
does not activate the server, workers, mounts or deletion recovery.

`make pre-backup validate-backup` installs the isolated package and passes all
119 host regression cases in 29.2 seconds (33.9 seconds including installation).
Evidence is `native_host_package_gate` under the October 4 client rehearsal
directory. Tests use strict fake S3 and real portable bundle export/restore,
exercise wrong/missing/composite checksums, denied HEAD, lost PUT replies,
cross-checkpoint/selection/proof rejection, native-validation failure, WAL ledger
capture, publication/cleanup ordering, returning live files and owned-copy
release. The native schema executable is a protocol fixture in these host unit
tests; the real Go/Python checkpoint interoperability gate is recorded above.
Two inherited assertions accidentally targeted an unrelated catalog copy and
were corrected to check the actual video upload. The old catalog Python package
is no longer required. All five packaged entry points load successfully. CI and
the fork gate now include the host suite.

All six installed baseline files still match their recorded lengths and SHA-256
digests. No installed script, service, timer, bucket object or privilege changed.
The package is staged, not production-ready: complete worker/config inventory,
interrupted/unpublished-run discovery/resume/pruning, complete media generation
and restore reconciliation, full artwork capture timing and writer/WAL costs,
relocated restore and cutover review remain required.

### Durable host run recovery — 2026-10-05

The host now records one active attempt before capture and resumes its original
run ID, checkpoint UUID, configuration, destination and options after restart.
It saves the completed media selection before native packing; publication retries
skip scans and compaction and cannot substitute a newer selection. A current
request to defer cleanup remains effective. Published attempts reopen only the
state needed to complete release, without requesting a new native view or
reopening already removed snapshots. Release interruption and partial local
object reclamation resume across new session instances. Finished identities
remain reserved, and the active pointer clears only after durable completion.
An invocation that recovers an already published attempt performs cleanup only;
the following invocation can capture newer state.

Packing and verification use privately owned scratch directories with durable
inode identities. Interrupted copies can be removed without following symlinks
into original data; replaced roots and nested mounts are rejected. A bundle
sealed before a crash is promoted without re-export. Upload and verifier child
processes inherit the enclosing backup lock so a surviving child continues to
exclude a competing backup after its parent dies. The current S3 pointer retains
its original conditional-write token; retries can adopt exact published bytes
after lost replies but cannot overwrite a newer publication. SHA-256 remains the
content check; ETag serves only to exclude concurrent pointer replacement.

`native_host_recovery_package_gate` passes all 133 host cases in 33.2 seconds
(37.9 seconds including package installation). `native_archive_inherited_lock_gate`
passes all 97 archive cases in 35.0 seconds. Evidence is under the October 4
client rehearsal directory. Coverage includes identical master replay, immutable
run conflicts, saved options and defer overrides, missing-pointer recovery,
new-instance partial-release cleanup, owned scratch replacement/mount/symlink
checks and a real child process retaining the lock after the parent closes its
descriptor. Earlier checks in this change also passed 123 and 131 host cases;
the final package run covers the added defer-cleanup regression.

For parent commit `920875c3e`, Build and lint succeeded. Its image-publication
workflow failed because it invoked `validate-fork` without installing the newly
required backup runtime. The workflow now runs `pre-backup` before that gate,
matching the successful local package setup and the Build workflow.

This closes sealed/published host retry handling, not unsealed abandonment.
Attempts that never established a valid native/filesystem boundary still need a
permanent abandonment record and scoped cleanup before the host can safely
advance to another UUID. Complete worker/config inventory, media-generation
reconciliation, full capture measurements and relocated restore/cutover review
remain open. Installed scripts, production services, credentials and cloud objects
were not changed.

### Unsealed host backup retirement (2026-10-05)

Native checkpoint admission now flushes a permanent `attempt.json` before
database, recovery-tree or filesystem-provider effects. Failed capture no longer
removes that identity or permits the same UUID to capture newer state. The
application-authenticated status endpoint distinguishes missing, partial,
sealed, released and abandoned attempts under the same exclusion as capture.
The abandonment endpoint refuses active or sealed checkpoints, binds the exact
request digest, and flushes a permanent failure receipt before removing only
known regular components. Missing UUIDs can be fenced against delayed requests;
repeated retirement completes interrupted cleanup without claiming publication.

The actual host session now inspects resumed unsealed attempts while holding its
backup/dedupe and worker barriers. It obtains the server fence before cleaning
partial component copies, artwork links or a media snapshot. The filesystem
challenge is saved before either provider runs; artwork admission records bind
the retained directory inode and source paths before linking. Cleanup preserves
unknown files and originals, rejects symlinks and nested mounts, and validates
ZFS ownership properties plus any retained GUID/creation transaction. Foreign
holds and clones block retirement; no force, recursive or deferred destruction
is used. Small server/provider/host receipts remain permanently. The retired
invocation reports failure; its next scheduled/manual invocation uses a fresh
identity. Sealed runs still follow publication and archive-bound release.

ZFS commands now run under a small host supervisor that retains the existing
backup lock independently of the configured command, including `sudo`. Caller
timeout or cancellation leaves the supervisor draining that command while
excluding a competing backup. The daily lock context closes its descriptor
instead of explicitly unlocking a shared description retained by children.
This also preserves exclusion for surviving upload/validator children on normal
exception exit, extending the earlier parent-death protection.

Verification under the October 4 client rehearsal directory:

- `native_checkpoint_abandon_server_gate`: all native checkpoint manager tests
  pass in 28.2 seconds, including missing/partial fencing, active/sealed rejection,
  restart replay, symlink refusal and preservation of unknown files.
- `native_abandon_api_auth_gate`: real Go HTTP/Python portable export,
  publication release and abandonment pass in 12.7 seconds; application-key and
  same-origin enforcement also cover the new endpoints.
- `native_abandon_archive_final_gate`: 106 archive cases pass in 27.5 seconds.
  The nine retirement/supervisor tests also pass after the final validation
  adjustment (`native_abandon_final_targeted_gate`). They exercise partial links,
  failed ZFS holds, foreign holds/clones, replaced snapshot identity, lost destroy
  completion, incomplete worker/config stages and real inherited flock exclusion.
- `native_abandon_host_corrected_gate`: 137 host cases pass in 34.3 seconds,
  including the real session's incomplete-request retirement without recapture,
  interrupted terminal-pointer cleanup, fresh UUID allocation and close-only
  backup-lock release. Every cloud boundary remains fake.

The first new fixture runs omitted the worker-lock directory required by the
publication barrier; supplying an isolated real lock directory fixed those
fixture errors. Parent `d2a98211d` now has successful Build, lint and GHCR runs.
No live provider/S3 mutation or installed-script/service change was performed.
Complete worker/config inventory, cold-media generation reconciliation,
production-sized capture measurements and relocated restore/cutover review
remain required by the transition plan.

## Backup storage policy and request reduction — 2026-10-05

The owner's cost clarification keeps native database snapshots and saved record
photos/logos/covers in S3 Standard, with bulk media in Deep Archive. The retained
full-library rehearsal has 5,257,301,389 compressed database bytes and
36,211,164,884 compressed original-image bytes: about 38.6 GiB total. At the
verified Oregon first-tier Standard rate, the one-baseline storage estimate is
about $0.89/month before requests, retained changes or other components. These
are measurements and estimates, not deployed usage or a billing report.

Native publication now maintains private, bucket/key-scoped full-checksum
receipts in `state_directory/object-receipts.sqlite3`. A fresh complete LIST can
reuse a receipt only when the expected content digest/size and remote ETag,
modification time and storage class agree. New or unknown objects still require
full SHA-256 HEAD verification. A missing immutable object is uploaded and
verified again. Denied/incomplete inventories and mismatched checksums fail
publication. Receipt loss causes re-verification without unnecessary upload.
Receipts are rebuildable; they are not required for restore. Explicit audit and
download ignore cached evidence and retain full verification.

The portable writer now uses 64 MiB maximum raw chunks, reducing the measured
20,265,979,904-byte library from 19,328 potential 1 MiB chunk uploads to 302
64 MiB chunks. Actual changed-chunk volume still needs measurement. Larger
chunks trade increased bytes per small edit for fewer requests; retained-history
limits remain required. Existing 1 MiB archives stay readable and keep their
own enforced chunk limit. No existing archive was repacked or cloud object moved.

Verification:

- `native_request_cost_host_corrected`: 147 host tests pass in 47.8 seconds.
  A persisted/reopened index lets 1,003 unchanged objects use exactly two LIST
  requests and no HEAD or PUT. Real bundle publication/reopen/restore fixtures
  cover missing objects, changed remote identities, cache loss, bucket isolation,
  failed/truncated inventories, and explicit audits bypassing the cache.
- `native_request_cost_archive_corrected`: 108 archive tests pass in 36.2 seconds,
  including legacy archive restore, declared chunk limits and a 64 MiB boundary.
- `native_request_cost_http`: the real Go HTTP/Python portable-export,
  release/abandonment fixture passes in 7.0 seconds.
- `native_request_cost_package`: isolated package installation passes in 4.6
  seconds; all five installed host entry points load. The retained full-size
  archive's inventory still validates with its original 238,575 artifacts and
  258,014 objects. This inspection is not a repeated full content restore.
- The initial checks found a function/local-name collision in the new inventory
  path and two old tests assuming multi-chunk small fixtures. The implementation
  and fixtures were corrected before the passing checks above.

The uncommitted cold-media version-ID prototype was withdrawn after the owner's
clarification. It is not a released backup format and has not changed IAM,
versioning, storage classes or remote objects. Its small source diff is retained
locally as design/test reference. Cold-media checksum identity and immutable-key
replacement still need integration, including existing-key reuse, restore path
mapping, and cleanup protections. No stable filename/size manifest is being
claimed as complete historical-content proof. Installed scripts, production
services and schedules remain unchanged. Parent `c5d781ee0` has successful Build,
lint and GHCR workflows. All wider migration/UI/caller, restore, cutover and
retirement gates remain in the full transition plan.

## Immutable media restore identities — 2026-10-05

The host readers now accept a version 4 media manifest with a bound bucket/prefix,
full-object checksum descriptors, and separate video restore paths. Different
paths can reference shared bytes without duplicate download or thaw requests.
New content keys use `media/sha256/<sha256>`; historical keys remain valid when
their complete checksum evidence is available. The descriptor supports S3's
full SHA-256/SHA-1/CRC64NVME/CRC32C/CRC32 checksums, including cold multipart
objects with full CRC evidence. Adoption does not copy or thaw existing objects;
missing/composite-only evidence requires review, not automatic reupload.

Native selection version 2 covers the media store, every required descriptor,
and all restore paths. It survives real portable packing, Standard publication,
download and reconstruction; changing a path/store/checksum breaks the original
binding. Selection version 1 and historical text/v2/v3 inputs remain readable.
Subset plans retain only their required descriptors and cannot inherit the
whole-library native claim.

Offline restore checks all required bytes before creating output, detects source
replacement during reconstruction, and rejects colliding paths or overwrites of
archive members. Explicit v4 status/thaw/download use the bound store, deduplicate
keys and verify checksum evidence. Corrupt downloads are removed, existing files
are preserved, and missing/changed objects never select newer replacement bytes.
Planning constructs no cold client. Default remote coverage audit uses only a
complete paginated listing for cold objects; the new `--media-checksums` option
explicitly requests one HEAD per distinct object, without payload reads or thaw.

Verification:

- `native_immutable_restore_host_final`: all 166 host tests pass in 35.9 seconds,
  including 19 new identity/restore/audit and real-bundle binding regressions.
  The previous receipt/request-count regression remains passing. The first
  164-test pass was followed by additional archive-member collision and explicit
  checksum-audit coverage; this final gate includes both.
- `native_immutable_restore_readers_final`: all 18 focused reader/audit tests
  pass after the final report-label and fixture-resource cleanup.
- `native_immutable_restore_package`: isolated package installation passes in
  4.6 seconds with the pinned native checksum dependency. No installed host
  script or live service is changed.
- Parent `8c0721a52ab5edf7cf6e4ef11bd00d940a174451` Build, lint and GHCR workflows
  are all successful. No production cloud request or data migration was performed
  by these tests.

This commits the read/verification contract before converting the publisher.
The daily writer still produces v3 path/size selections. Durable cold upload
receipts, immutable replacement publication, existing-object reuse, shared-path
cleanup and interrupted-run reconciliation remain necessary before activation.
Reader support alone does not close that cutover gate. The earlier full-size
archive rehearsal remains retained and was not repeated. Wider migration,
caller/UI, scheduling, restore and owner-review gates remain unchanged.


## Immutable media publication and cleanup — 2026-10-05

The staged host publisher now writes v4 manifests with separate restore paths,
full-checksum descriptors and a bound cold store. New/replaced videos use
`media/sha256/<sha256>` keys with immutable Deep Archive uploads. Matching legacy
objects remain reusable after checksum comparison; unsupported historical
checksums stop for review without reuploading or thawing. Existing known objects
with changed bytes stop publication. New archive bases/deltas retain append-only
keys and receive the same independent full-checksum verification.

The existing cold backup ledger retains object receipts, current path bindings
and pending upload associations. A complete paginated inventory serves all
normal verification and cleanup calls. Duplicate/renamed video paths can share
one upload. Cleanup protects all current references, retires absent path bindings
independently of shared objects, and checks live path changes after the captured
view and after remote tag reads. An interrupted upload retains enough path
evidence for later cleanup. Old manifests retain their original bytes until the
existing lifecycle expires superseded objects; retention reconciliation remains
required before cutover.

A resumed v4 selection verifies every required video and archive without scanning
newer files, repairing missing objects or changing its selection. Local NUL
manifests retain paths; the text remote manifest is only an object allowlist.
Content-addressed restores require the JSON path mapping. Historical filename
text restores remain supported, while an unfinished v3 native attempt requires
its original writer.

Verification records under `.local/native-discovery-client-20261004/` cover
publication/restart, duplicate and renamed files, old-manifest restoration after
replacement, lost upload responses, late returning files, missing objects,
checksum collisions and cache loss. The cold-index test verifies 1,003 unchanged
objects after reopening with exactly two LIST requests and no HEAD/upload calls.
The whole-backup test also exercises orphan maintenance with no extra listing,
HEAD, cold upload or tag for an unchanged second run. No production S3 operations
are used by the test transport.

`native_immutable_writer_final` passes all 200 host tests in 45.3 seconds.
`native_immutable_writer_package` installs the isolated package in 4.5 seconds,
and all five installed test entrypoints pass. The disk check reports 164.1 GiB
available with the 50 GiB reserve maintained. Production
scripts, services, lifecycle/IAM policy and the frozen compatible release remain
unchanged. Parent `12b05abcb` Build and lint passed; its GHCR job failed fetching
Go checksum-service data (`INTERNAL_ERROR`) and was retried. Full native bundle
restore evidence is retained without repeating the large rehearsal. Remaining
release gates include complete component inventory, retention/cost measurements,
full capture timings, relocated restore, migration/UI/caller completion and the
owner's production cutover review.


## Worker backup dependency closure — 2026-10-05

The host publisher can now resolve an explicit `worker_inventory` under the
worker publication barriers. It follows download/enrichment/listing/detail
profiles, layered private JSON references, reviewed helper assets, cookie files,
yt-dlp file arguments and fixed-directory download archive templates. Container
paths use declared longest-prefix mappings; relative bindings use the profile
folder, while gallery paths use the declared worker cwd/home. Reports retain
paths and hashes without resolved credentials. Duplicate paths share one captured
component; conflicting backup roles and unheld publication roots fail.

A run retains its resolved report before staging. Parsed dependency hashes must
match the actual staged bytes before requesting the native checkpoint. A retry
uses that original report and component stage, even after live profiles or cookie
files disappear. New backups re-enumerate archive templates so a newly used
service's archive is included. Standalone `stash-s3-inventory` provides read-only
inspection, with a private non-overwriting output file. This is dependency proof
for declared workers, not automatic discovery or activation of every launcher.

Verification:

- `native_worker_inventory_final`: all 211 host tests pass in 46.3 seconds,
  including 11 new tests for private references, mount mappings, new archives,
  helper changes, missing/symlink files, metadata profiles, barrier coverage,
  real SQLite component snapshots, captured-file drift and original retry state.
- `native_worker_inventory_package`: isolated installation passes in 4.6 seconds.
  All six entrypoints pass `--help`; the installed inventory command reproduces
  the source implementation's report and creates it with mode 0600.
- Seven previously staged host/n8n profiles were inspected against current
  production dependencies: 15 download archives, 11 shared configuration/cookie/
  helper files, the inventory declaration and seven profiles. Their shared lock
  root resolves to `/tank/media/scrape_metadata/.download-locks`. This inspection
  used an explicitly synthetic local outbox; native production outboxes are not
  yet provisioned and the profiles were not activated.
- A separate read-only inventory records 49 current unit/launcher/list/build
  files, three legacy operational databases and 14 relevant n8n workflows. All
  nine active relevant workflows have matching draft, active-history and published
  graph evidence. The live status check found Stash and n8n running and the
  scheduled Reddit service in progress. No service was changed.

Private evidence is retained in `.local/native-backup-inventory-20261005/`.
The earlier service read initially encountered an approval-review capacity
failure; the approved retry completed successfully. Disk free remains 164.1 GiB,
above the 50 GiB reserve. Parent `3e7eeee0d` Build/lint succeeded; GHCR passed its
validation step and was building the image at the last observation. The older
`12b05abcb` GHCR retry was superseded/cancelled, not mistaken for successful
publication.

Complete the final native launcher/profile/outbox/environment declarations and
reviewed host configuration after staging their actual deployment locations.
Include source lists, unit/environment files and workflow exports in explicit
components; profile closure alone does not establish complete deployment coverage.
No production writer, S3 policy or schedule was changed. Retention/cost/capture
measurements and all remaining migration/UI/caller/restore/cutover gates stay open.


## Successful backup history and retained media — 2026-10-05

The host publisher now records a checksum-verified immutable publication receipt
after the current manifest commits and before releasing the original capture. A
failed receipt write leaves the same attempt recoverable; retry completes the
receipt without republishing the current pointer or recapturing newer state.
Prepared per-run masters alone never enter successful backup history.

Retention defaults to seven successful snapshots plus explicitly pinned archive
UUIDs. The current snapshot is always included. Immutable retirement receipts
record which older snapshots cease to carry a restore guarantee. Unknown or
already-retired pins, missing/changed receipts, mismatched cold stores, missing
retained media and failed listings stop cleanup. Increasing retention cannot
resurrect a retired snapshot. Production has no native history yet; predecessor
native publishers require explicit history reconciliation before replacement.

Every retained snapshot's video/base/delta references now protect cold-media
cleanup, including the optional legacy/orphan reconciliation paths. Historical
protection preserves pending deletion and source-path evidence until the last
reference retires. Current library references retain their existing behavior.
The final tagger also refuses bypasses of this protection. Deferred cleanup
publishes history but performs no retirement or obsolete tagging.

Private destination-scoped caches rebuild from immutable remote receipts and
verified masters. Full pagination is required; unchanged history needs one LIST
per page and a current-pointer checksum check, with no per-media HEAD or tag
reads. Large derived media graphs are pruned after retirement while small UUID
receipts remain. Corrupt caches require inspection/rebuild before cleanup.

All 236 host tests passed in 58.4 seconds. Isolated package installation passed
in 5.7 seconds; all six installed entrypoints passed their help checks, and the
installed history module reports the intended seven-snapshot default.
New coverage includes failed/lost
publication receipts, retirement failure/retry, pinned and shared references,
cache loss/corruption, full pagination, unchanged request counts, changed current
pointers, missing historical objects and media deletion/compaction across real
backup runs using fake cloud transports. The first integration test exposed a
fixture that neither triggered compaction nor separated snapshot timestamps; the
corrected scenario explicitly compacts and advances capture times.

The isolated encoder measurement also completed in 746 seconds. A copy of the
20,265,979,904-byte schema-1000077 database compressed to 5,242,807,047 bytes in
302 chunks. Cumulative batches of 1/100/1000 spaced image-title edits added
8/24/24 objects (188,399,162 / 600,577,622 / 600,012,930 compressed bytes). The
source signature remained unchanged, no cloud requests occurred, and the
disposable database/encoded objects were removed. Root free space is 164.1 GiB.
Sanitized evidence is in `docs/native-backup-cost-measurement.json`. Synthetic
SQL edits do not establish real daily production churn or total monthly costs.

Standard chunk/per-run file expiration, large local run-inventory reclamation,
verified bucket versioning/lifecycle and final production inventory remain open.
No bucket policy, production service, native schema, frozen compatible release
or scraper configuration changed. Full capture/pause measurements, relocated
restore and all broader migration/UI/client/cutover/retirement gates still apply.


## Full-size host manifests and streamed native downloads — 2026-10-05

A locked copy of the host ledger/video manifest established the current shape:
272,373 video paths with recorded sizes, 1,328 active archive units and 281,421
representative objects. The backup lock was held for 1.22 seconds while copying;
all subsequent processing used the copy. Synthetic distinct content keys and
full SHA-256 descriptors produced a valid 137,128,906-byte master, which the old
128 MiB history/publication cap rejected. CRC64NVME produced 128,967,697 bytes,
already close to that cap. These measurements are not a remote checksum audit.

Host publication, history, retained-run recovery, audit and restore now share a
512 MiB master bound. The archived media-selection envelope permits another
1 MiB. Portable artifact inventories have a separate 1 GiB host transport bound;
the portable small manifest and individual inventory-record limits remain
unchanged. Formats and existing backup identities are unchanged.

Native metadata and encoded-object downloads now use bounded 1 MiB reads within
one GET per object. Complete SHA-256 and length verification, flush and fsync
precede publication to a new local file. Existing outputs remain protected;
truncated, oversized, corrupt, interrupted and space-constrained transfers remove
their temporary files. The streamed inventory is validated through the existing
portable reader rather than loaded as one byte buffer. JSON master parsing still
uses memory proportional to the declared bounded document.

The measured SHA-256 case passed local readers, real archive packing/binding,
checksum-verified host publication, history reconstruction, streamed download
and portable restore with a fake S3 transport. The restored 137,128,318-byte
media binding matched its complete original SHA-256. That round trip took
63 seconds, used 77 fake transport operations, made no cloud requests and peaked
at about 2.12 GiB RSS. It used the existing tiny native-schema verification
fixture and does not replace the earlier populated-database restore proof.
Sanitized evidence is in `docs/native-backup-manifest-scale.json`.

All 247 host tests passed in 59.2 seconds, including a generated inventory larger
than 128 MiB whose transport rejects unbounded reads. Shared exact-limit/overflow
checks cover publication, history, recovery, audit and restore; transfer tests
cover wrong hashes, length mismatches, network errors, existing destinations and
reserved disk headroom. Isolated packaging passed in 5.0 seconds; all six installed
entrypoints and the installed limit module passed their smoke checks. The
installed restore/history readers also accepted the 137,128,906-byte measured
master. The ledger copy, fake transport/bundle and generated full-size master
files were removed after verification, preserving only small scripts/reports.
Parent `fa72a4d3f` passed Build, Lint and image-publication CI.

Standard expiration/resurrection and local run-metadata reclamation remain the
next backup implementation work. Final deployment inventory, real capture and
writer-pause measurements, relocated restore and the full migration/UI/client/
cutover/retirement requirements remain open. No production service, scraper,
bucket policy, native database schema or frozen compatible release changed.


## Native Standard retirement and local run cleanup — 2026-10-05

The host now has opt-in Standard cleanup after successful publication and durable
snapshot retirement. It protects every active native graph, including pinned
snapshots, and tags only known retired chunks and their per-run master, manifest,
inventory and verification files. Unknown uploads remain untouched. Retiring
snapshots are processed individually, avoiding an in-memory accumulation of all
historical inventories. Graph-cache writes honor the configured disk reserve.

A small permanent chunk-retirement receipt precedes expiration of the old
inventory. Interrupted tag requests resume, and cache loss after actual inventory
expiration does not lose the remaining cleanup work. Upload receipt schema 2
caches verified tag state and invalidates it durably before mutation. Reusing a
retired object clears the tag, preserves unrelated tags and verifies current
bytes/presence before publication. If expiration won the race, publication uses
the original retained local bytes again. Already-versioned objects use the
checked version for tag operations; the implementation does not enable versioning.

After release and retirement, known large local master/prepared-master/catalog
files and the artifact inventory can be removed. A small durable intent preserves
retry after partial deletion. Identity, publication, release and completion
receipts, unknown files, active attempts and unfinished runs remain. The native
archive already contains the exact host ledgers; duplicate latest/per-run ledger
uploads are removed. Existing compatible ledger backups remain untouched and
require separate inventory/retirement review.

`standard_cleanup` defaults to false pending activation review. The publisher
checks the exact scoped lifecycle configuration and versioning state before
cleanup, and cannot install policy. A checked-in JSON example covers native
retirement, noncurrent versions and delete markers without changing cold-media
rules. AWS documents re-evaluation of tags at expiration execution; see the
linked primary documentation in the host README. The actual metadata bucket's
missing lifecycle policy and unverified versioning remain deployment gates.

All 262 host tests passed in 62.4 seconds, including real small archive bundles,
shared references, missing retained data, lost/failed tags, expiration/reuse,
version-specific tagging, schema-1 receipt migration, cache loss after expiration,
partial local deletion and preservation of active/unknown files. Warm cleanup
performs listings and one current-pointer HEAD, with no per-object HEAD/tag/GET
sweep. The existing 1,003-object publication request test also passes. Package
installation passed; all six installed entrypoints and the generated policy's
AWS SDK input schema validated without cloud requests. Root free space is
164.0 GiB. Build, lint and GHCR for parent e120ff389 all passed.

No live backup, scraper, Stash schema, bucket policy, release pin or develop
branch changed. Observed cloud expiration, final inventory, capture measurements,
relocated restore and all broader migration/UI/client/cutover/retirement gates
remain open.

## Instagram download ingestion and staged host timer

The pinned gallery-dl adapter now retains `instagram_media` version 1 before
filtering unavailable items or reversing downloads. The Python producer and Go
server share a fixture corpus for parent identity, attachment membership, missing
slots and story/highlight containers. Each carousel file links to its source
attachment; an evidenced carousel can create a gallery before every source file
is available. Singleton posts and individual stories do not become albums.

Stories use their original media IDs and publication times, so a later highlight
capture identifies the same post. The profile dispatcher can route verified
same-service children without a post timestamp; each child applies the original
window. Disabled music-sticker audio does not block visual downloads or invent
attachments. Static-video policy remains effective. File-specific capture fields
share the enclosing post body only when explicit new membership evidence exists;
historical capture signatures retain their original partition for replay and
enrichment proof validation.

The new packaged `stash-ingest-instagram` launcher freezes source-list URLs and
the window into durable caller requests. It preserves explicit extractor URLs,
fails on invalid list lines, and replays saved work after its input list/profile
disappears. Exit 0 means locally recorded; actual scraping requires the separate
dispatcher and receipt inspection. Source-scoped configuration conversion now
excludes unrelated extractors and their private references.

Read-only inspection of the actual host timer and its one saved profile produced
`.local/native-instagram-conversion-20261005/host-instagram-worker.json`, a staged
unit override and `review.json`. The effective `stories,highlights,posts` selection,
video flag, `abort:4` rule and `{username}, instagram` directory remain. The
validated dispatcher routes were stories, highlights and posts. Input hashes
were unchanged; no website/API requests or download-archive writes occurred.
The staged root UUID is explicitly unregistered rehearsal state, not final
production registration. Outbox/environment provisioning, dispatcher/recovery
scheduling, runtime installation and observed native receipts remain required.

Pinned extractor tests cover reversed mixed-media carousels, missing source slots,
story/highlight reuse, per-story time boundaries, profile dispatch, music stickers
and static-video outputs. Real native capture/file publication tests verify gallery
creation versus singleton attribution and immutable receipt replay. Existing
historical retention fixtures and all archive package tests pass. The installed
launcher help check passes. All 499 producer tests pass in 44 seconds; package
installation and scoped Go lint also pass. No live timer, scraper, archive, Stash schema or frozen
release changed; this work does not close the remaining service/caller coverage
or any production release gate.

## Original Kemono/Coomer ingestion and staged launchers — 2026-10-05

The pinned mirror user, post and posts-listing adapters now retain `mirror_media`
version 1 before file selection, download ordering or mutation of attachment
dictionaries. Producer and backend share a manually specified contract corpus.
Membership preserves primary files, ordered attachment arrays, missing source
slots, repeated positions and deduplicated inline references. A primary alias of
an attachment does not invent a second slot or turn a singleton into an album.
Configured file selection cannot shrink the original source list.

Both mirrors require `original=true`. Scan windows use the original `published`
timestamp with fractional precision, without falling back to import time. Audio,
archives and other unsupported outputs retain explicitly excluded captures and
checkpoints without downloads, file receipts or download-archive acknowledgements.
Native and retained legacy cursors can resume past those exclusions. GIF conversion
keeps the reviewed host helper. Qualified mirror/service/account/post identities
remain separate from native service identities. New captures share post bodies
and retain file details in patches; historical unmarked capture partitions remain
unchanged for replay and migration proofs.

Packaged `stash-ingest-coomer` and `stash-ingest-kemono` launchers preserve explicit
URLs, saved-list order, original windows and caller recovery after input files
disappear. Invalid list lines fail the request, and strict status retains pending
work. Source-scoped configuration conversion excludes unrelated extractors while
retaining original quality, filenames, skip/archive behavior and helper bindings.

Actual host configuration inspection staged normal and full-history profiles for
both mirrors in `.local/native-mirror-conversion-20261005/`. All 39 saved Coomer
targets use the supported user extractor. No saved Kemono list or dedicated
Coomer/Kemono timer was found in the inventoried paths; Kemono is staged for
explicit URL callers. Input hashes were unchanged, with no website/native API
requests or download-archive access. The root UUID remains unregistered rehearsal
state. Final producer/root/collection registration, outbox/environment, dispatcher,
recovery and actual host/n8n activation remain open. Discord, favorites and artist
containers require separate adapters.

All 510 producer tests pass in 45 seconds. Pinned extractor tests cover source
order, missing slots, configured file selection, source dates, nonvisual exclusion
and native/legacy cursor recovery. Real capture/file publication tests cover a
singleton primary alias and a mixed-media album with replay. The archive package,
focused backend suite and scoped Go lint pass. Isolated package installation and
both installed launchers' help/dry-run checks pass without workspace `PYTHONPATH`.
No live service, Stash schema, frozen release, bucket policy or develop merge
changed. This increment does not close the remaining source coverage or release
gates.

## Bluesky/TikTok post ingestion and staged launchers — 2026-10-05

The pinned producer now records `bluesky_media`/`tiktok_media` version 1 with the
qualified post reference and original ordered source membership. Backend
validation recomputes that evidence from original embeds or photo/video fields.
Bluesky attachments use blob CIDs within DID/record post scope; TikTok photo keys
exclude URL signatures and rendition suffixes, while videos use post-scoped keys.
Missing source slots and repeated positions remain visible. Multiple source slots
can create a gallery as files arrive; one photo remains attributable without a
new gallery. New captures share post bodies, keeping selected images, alt text,
dimensions and generated titles in patches. Historical unmarked partitions remain
unchanged for replay and import proofs.

Bluesky windows retain full `createdAt` precision, and quoted posts keep separate
identity and source time. Video selection does not shrink source membership.
TikTok uses original `createTime`, skips unavailable photo outputs while retaining
their original positions/numbers, and propagates extraction errors that the pinned
upstream loop otherwise catches. Supported profile and shortlink dispatch keeps
the claimed window in every child. Native TikTok profile defaults select posts;
explicit unsupported artwork/following includes fail before source access.

The new packaged URL/list launchers record durable caller requests and preserve
pending status and replay after input profiles disappear. Source-scoped conversion
retains relevant settings/private references and excludes unrelated extractors.
Actual host settings produced normal/full-history profiles in
`.local/native-social-conversion-20261005/`. A separate inactive TikTok overlay
disables audio/covers/subtitle files and removes avatar from the configured
`avatar,posts,stories` selection, retaining posts and stories. The original
references remain in captured source metadata. The review records these
intentional differences; source configuration hashes are unchanged. No saved
Bluesky/TikTok lists or dedicated host user units were found in the inventoried
paths, so this stages explicit URL callers rather than adding schedules. Synthetic
profile route checks made no website/API requests or download-archive accesses.
Root/producer/collection registration, durable outboxes, dispatch/recovery and
actual caller activation remain cutover work.

The shared 27-case Go/Python corpus covers identity, partial/repeated membership,
URL variants, invalid evidence and retained payloads. Pinned offline download
tests cover source windows, quotes, video selection, missing photos, profiles,
shortlinks, rejected auxiliary outputs and extraction failures. Real native
capture/file publication tests cover Bluesky partial albums and TikTok album versus
singleton behavior with receipt replay. All 524 producer tests pass in 44 seconds;
the full archive suite, focused backend checks and scoped Go lint also pass.
Isolated package installation and both installed launchers' help/dry-run checks
pass without workspace `PYTHONPATH`. No live scraper, service, database schema,
bucket policy, frozen release or develop merge changed. Remaining service
adapters, native management interfaces, historical policies, full caller
conversion and the production release gates remain open.

Installed runtime comparison also found an obsolete `enrichment_lease.py` in
reused wheel output. Development `PYTHONPATH` caused pip to locate source metadata
instead of uninstalling the old installed distribution, retaining the removed
module in the test environments. Producer wheel builds now discard obsolete
Python modules from their build output. Both setup targets run pip in Python
isolated mode and verify the installed module set and file digests. An
intentionally stale build-module fixture was removed during wheel creation;
producer and backup installs both match all 98 current source modules. The
installed producer fingerprint now exactly matches the staged profiles. These
checks correct packaging evidence without activating any live worker.

## Native collection management — 2026-10-05

The application now has a Source collections route in shared desktop/mobile
navigation. It searches current names, URLs and relative folders with bounded
pages and type/state filters. The editor supports source targets, qualified
accounts, registered roots and relative folders, manual-batch/folder types,
active/disabled/retired state and recorded reasons. Source accounts remain
publishers, independently of depicted performers. Account/root choices search
the server rather than downloading every catalog. Immutable collection history
loads only when expanded. Existing-card updates preserve the page cursor and
remove a card when the updated definition no longer matches its filter.

Collection/root list APIs support bounded literal-text searches without reading
captures or library media. New root-detail and collection-history endpoints make
selected lookups and uncertain-save recovery possible. Existing anonymous list
callers retain their default page size. Invalid definitions and missing or
mismatched root/account references return client errors without partial records.
The populated rehearsal has 2,612 collections and 3,428 definition revisions.
Read-only measurements confirm indexed current-revision joins; the recorded
SQLite timings include warm cache and are not end-to-end UI latency claims.

The browser persists a caller UUID and exact revision-guarded PUT before sending.
Recovery verifies its historical definition, even after a later concurrent edit.
Competing choices require review, while unchanged definitions create no revision.
Pending writes survive reload, coordinate across tabs and remain isolated by
backend mount. Confirmed success remains visible if refreshing the saved view
fails. The full UI gate passes 591 tests. Chromium and WebKit pass 34 collection
and account-review checks, including both navigation surfaces, folder/account
selection, invalid paths, concurrent edits, lost responses, same-origin deployment
isolation, filtered-card updates and failed post-save refresh. Reviewed screenshots
and query measurements are retained under `.local/native-collection-ui-20261005/`.

All required fork checks pass, including the full Go integration suite. The
SQLite package took 1,203 seconds, just beyond the default 20-minute limit; this
run used the documented `GO_TEST_TIMEOUT=30m` override without changing the
repository default. The earlier collection assertion now tests early reference
validation separately from the SQL constraints that prevent orphan identities.
The original runner lost its terminal report during continuation, so the finished
log was reconciled against every tagged Go package and each prerequisite stage;
that evidence is in `collection_management_gate_reconciliation.json` under the
local verification directory. No production service, database, worker, frozen
release or bucket policy changed. Root creation/mount review and
metadata-policy/manual performer controls still need management screens. The
current populated rehearsal has no metadata policies; their historical import
and actual saved choices remain required. A collection definition change also
requires reviewing policies bound to the previous revision. Remaining source
adapters, host/n8n activation, full reconciliation and production release gates
remain open. See [collection management](native-source-collections.md).

## Native metadata policy management — 2026-10-05

The application now has collection-scoped metadata rule editing, with separate
scene/image controls, backend-schema target choices, plain jq expressions and
typed constants. Fixed performer/studio/tag choices use existing library entries
and portable UUIDs. Direct folder scans can inherit selected performers and a
filename title without source metadata. Source publishers remain independent of
depicted performers. Creation/organized behavior, existing-item processing,
protected metadata, disabled folder masks and stale collection revisions are
explained in the controls. Policies remain separate from worker activation.

Read-only draft previews select an existing scene/image, a physical file inside
the saved folder and an optional evidenced capture/attachment. Indexed, bounded
selectors respect current media associations, collection revisions and merge
redirects. Drafts can simulate creation without saving a policy, intake or field
decision, and have no Apply digest. Internal settings and mapping definitions do
not enter jq sample data. Definition validation includes both media kinds.

Policy history loads lazily in bounded pages. Browser saves retain exact bytes
before sending, recover committed historical revisions after lost responses,
require review of concurrent changes, retain rejected drafts for correction and
preserve confirmation when a post-save refresh fails. Validation details are
shown with the rejected expression/value. Retired collections remain read-only.

The focused backend overlay checks passed before copying the same nine Go files
into the repository. Twelve client/form tests and the complete UI gate passed
(603 tests in 100 files). After the final FieldContent layout correction,
Chromium/WebKit passed all 26 metadata-policy and collection checks. These cover
scene/image previews, simulated creation, absence of preview writes, fixed
performer selection, saved-request recovery, invalid-draft correction, concurrent
edits, refresh failures, bounded history and existing collection workflows.
Screenshots and read-only query measurements are retained under
`.local/native-metadata-policy-ui-20261005/`.

The final embedded-UI build and complete fork gate passed in 1,400 seconds as
`metadata_policy_editor_full_gate`. This includes 603 UI tests, 524 producer tests,
8 library tests, 108 archive tests, 262 backup tests, Go lint and the full tagged
Go suite. The SQLite package took 1,209 seconds; the documented 30-minute timeout
override was used without changing the repository default. The current populated
rehearsal still has no migrated metadata policies. Historical
policy choices, root/mount management, remaining operational import families,
source adapters/caller activation, complete reconciliation, backup activation
and reviewed production cutover remain required. No live worker, database,
bucket policy or production service changed. See the expanded
[collection and metadata policy guide](native-source-collections.md).

## Native media-root management — 2026-10-05

The application now exposes Media roots in shared desktop/mobile navigation.
Bounded name/path searches and state filters open one root definition at a time.
A root can exist without a local folder, retaining its UUID and portable
collection paths. Checking an existing server directory returns its canonical
path and directory identity without creating a root or folder. Saving a new
binding rechecks that exact identity; a directory replaced after inspection is
rejected before creating any identity or revision. Changing a path clears its
check. Registration does not move files, scan media or authorize a worker.

Labels and disabling an unchanged binding work while its folder is offline.
Reactivation verifies the directory again, and permanent retirement preserves
the prior definitions and bindings. These actions do not delete media or
collections. A new paginated history endpoint supports both inspection and
recovery. The API now accepts an exact checked binding instead of probing an
unchecked `server_path` during the write; existing test callers and API guides
have been converted. Production callers have not yet activated these routes.

Browser writes retain a caller UUID and exact request before transmission.
Recovery checks the original immutable revision even after a later relocation
or mount loss; it never accepts a newly probed replacement directory implicitly.
Rejected writes require review of the current definition. Confirmed saves remain
visible when refreshing fails. Retired definitions remain inspectable. Source
collections continue to reference the same root UUID across binding revisions.
Binding a relocated root does not rewrite existing library file paths; restored
file-path reconciliation and worker configuration remain cutover requirements.

The focused HTTP/store overlay checks, five client recovery tests, TypeScript,
Biome and all 24 Chromium/WebKit root/collection checks pass. Browser coverage
includes directory checks without writes, stale checks, changed-directory
rejection, lost responses, offline disabling, lazy history, post-save refresh
failure, retirement and shared mobile navigation. Desktop/mobile screenshots are
retained under `.local/native-root-management-20261005/screenshots/`.

The complete embedded-UI build and fork gate passed in 1,386 seconds as
`root_management_full_gate`, including 608 UI tests, 524 producer tests, 8 library
tests, 108 archive tests, 262 backup tests, zero Go lint issues and the full tagged
Go suite. The documented 30-minute package timeout override was used without
changing the repository default. Historical policy import, remaining operational
imports, caller activation, reconciliation, backup activation and reviewed
production cutover remain open. Live services,
databases, workers, bucket policies and the frozen compatible release are
unchanged. See [media-root management](native-source-collections.md#media-roots-and-server-folders).

## Native organized completeness requirements — 2026-10-05

Collection metadata rules now accept optional required fields before marking a
scene or image organized. The editor offers that media kind's schema fields,
excluding the organized flag itself. Requirements use the values left after
permitted changes: protected current values count, proposed clears leave a field
empty, and rejected candidates cannot fill it. Blank strings, empty containers
and null are missing; zero is a valid selected rating. Unmet requirements explain
the omitted organized change. They never clear an existing organized flag and
remain separate from successful-mapping and unresolved-name checks. An empty or
omitted requirement list adds no restriction or repeated no-op revision.

The installed catalog plugin assessment found a concrete migration need for
this behavior: its undeclared Python defaults require seven fields before
marking organized. That list includes `cover_image`, which is not a supported
native metadata-policy target; its conversion still needs an explicit
disposition. No legacy requirement is silently omitted from the assessment.
The seven retained folder documents were verified against their stored hashes
and parsed from original XML. Five performer defaults and one studio default
each have a unique current name/alias candidate; one document is empty. Their
historical collections have no local root/folder binding, so usable directory
scopes must be established before importing these rules. These read-only findings
are retained under `.local/native-policy-migration-20261005/` and do not activate
policies or assign source publishers as depicted performers.

Focused SQLite and pure service tests pass, including restart, preserved values,
explicit clears, schema restrictions, unchanged definitions and protection of
already organized items. The complete UI gate passes 610 tests; all 12 existing
Chromium/WebKit metadata-policy workflows pass with schema-driven completeness
selection and save persistence. Two final Chromium/WebKit checks also verified
the disable/re-enable behavior and retained mobile/desktop screenshots.

The embedded-UI build and complete fork gate passed in 1,351 seconds as
`organized_completeness_full_gate`: 610 UI tests, 524 producer tests, 8 library
tests, 108 archive tests, 262 backup tests, zero Go lint issues and the full tagged
Go suite. SQLite took 1,156 seconds with the documented 30-minute package timeout
override. Historical policy migration remains open; these requirements do not
activate or import any live policy. Production services and the frozen compatible
release are unchanged.

## Historical metadata policy import — 2026-10-05

Native policy migration now has a bounded preview/apply API and immutable
receipts containing original source values, per-setting conversion decisions,
and the exact resulting policy revision. Its schema-1000078 tables reference
native policy revisions and selected folder document heads. Preview guards the
current collection, policy and fixed relationship targets. Changed selections
require a fresh preview; replay after a successful commit returns its original
receipt even after later policy edits. Failed or interrupted transactions cannot
leave a policy without its receipt. Migration does not edit existing media.

The standard-library client validates native input and plan hashes, retains
frozen bindings for retry, and handles lost responses. Its source reader
inspects Python literal settings without executing plugin code and preserves
Python, manifest and saved-override layers. Every retained setting requires an
explicit mapped, replaced, retired or review disposition. Unresolved settings
require a disabled policy. Selected folder evidence must describe the target
folder; publisher links cannot supply depicted-performer defaults implicitly.
Database reopening verifies receipt hashes/references, and anonymisation removes
private settings and their migration provenance.

Focused pure-service, SQLite and real Python/HTTP tests pass, including lost
responses, restart, later edits, object-valued constants, stale target revisions,
changed document selections, rollback and a receipt failure whose caller ignored
the error. Python settings-reader tests also pass. The isolated draft was copied
only after the organized-completeness increment was validated, committed and
pushed as `81393477f`. The populated schema migration completed in 300 seconds.
Independent comparison preserved all 275 pre-existing tables, every old schema
object and the retained discovery/recovery records; integrity and foreign-key
checks passed. A fresh application reopen took 110 seconds and verified eight
activation receipts, 757 bound targets and 188 recovery targets.

The real retained documents exposed an invalid assumption: all seven selected
`folder.nfo` sources also retain historical catalog post identities. Folder scope
now follows the selected document head and exact folder path while preserving
those post references. A regression reproduced the rejection before the fix;
the corrected tests also reject unrelated documents and folder paths.

Seven frozen bindings preserve five performer defaults, one studio default and
one empty folder document. Each retains 39 original source values with explicit
conversion dispositions. The real Python client imported them through an
isolated application API in 284 milliseconds after opening the database.
Lost-response retry and a fresh restart returned the same plans and receipts
without creating another policy revision. These are disabled migration drafts:
cover completeness, implicit source mappings and tag filtering remain unresolved.
No rule has been activated and no existing scene/image metadata was changed by
the import operation.

The first full Go run exposed predecessor fixtures that had changed the schema
version without removing the two new tables. Their shared fixture chain now
constructs the actual older layout. Focused tagged tests verify policy/document
preservation, rollback on either unknown-table collision, corrupt-provenance
rejection and the corrected fixture chain. Independent post-import comparison
passed: all 275 original tables remain unchanged except the exact seven new
scope/policy additions, all seven original document/post links survive, and
integrity and foreign-key checks pass. The new scopes have review origin through
the ordinary collection API; imported policies retain migration origin.

The next full fork gate exposed a database-path bug: connection strings treated
a literal `#` as a URI fragment. Repeated migration tests consequently reused a
truncated filename. A regression also reproduced incorrect writes and percent
escape handling. Main connections and closed-database backups now encode literal
paths consistently with the lineage validator; the independent test connection
does the same. Focused repeat-run validation passed in 22 seconds, including the
new path/backup regression and predecessor migration checks. The final complete
fork gate passed in 1,396 seconds: 610 UI tests, 526 producer tests, eight library
tests, 108 archive tests, 262 backup tests, zero lint issues and every tagged Go
package. API and SQLite tests took 1,109 and 1,225 seconds respectively, using the
documented 30-minute package timeout override. All 22 final source hashes match
that successful run. Schema 1000078 is the latest fully verified rehearsal;
the seven imported policies remain disabled pending complete rule conversion.
Production and worker configuration are unchanged.
See [policy migration](native-metadata-policy-migration.md).


## Native relationship name mapping — 2026-10-05

Collection rules can now resolve studio, tag and performer names and individual
aliases, plus group names with optional scene indexes. The editor exposes the
option only for relationship fields, using plain studio text, one name per line
for performers/tags and structured group values. Existing native performer-name
rules retain their immutable representation; new edits use `reference_names`.
No v2.5 plugin or runtime adapter is introduced.

Shared exact lookups serve both intake rules and retained file-edit review.
Canonical-name/alias collisions remain explicit candidates, repeated spellings
of one entity count once, and more than 100 candidates produce an overflow flag.
Schema 1000079 adds five nonunique name/alias indexes for studios, tags and groups;
startup validates their table, columns and collations. Matching uses SQLite's
ASCII case folding and exact non-ASCII spelling. It does not create missing
entities, choose approximate names or assign account owners as depicted performers.
Partial unambiguous results preserve unrelated inherited relationships. Unresolved
names prevent automatic organization, and changed candidates invalidate previews.

Pure service, real SQLite and native HTTP checks pass, including stale aliases,
canonical/alias collisions, repeated input names, large alias sets, partial group
results, schema rollback and invalid-index refusal. All 14 Chromium/WebKit policy
workflows pass; two further browser checks retain mobile/desktop screenshots under
`.local/native-policy-name-mapping-20261005/screenshots/`. The actual UI validation
and embedded build pass. An actual-source staticcheck suggestion was corrected
before the complete fork gate passed in 1,409 seconds: 612 UI tests, 526 producer
tests, eight library tests, 108 archive tests, 262 backup tests, zero lint issues
and every tagged Go package. SQLite took 1,218 seconds with the documented
30-minute package timeout override. All 30 frozen input source hashes still match.

The populated migration passed in 306 seconds. Independent comparison verified
36,972,108 typed rows across all 277 pre-existing data tables, unchanged original
schema objects, exactly the five new indexes, clean integrity and no foreign-key
violations. The comparison took 803 seconds alongside the full test suite.
A fresh application reopen passed in 193 seconds, preserving seven disabled policy
imports with 39 source settings each, their original folder references, eight
activation receipts, 757 bound targets and 188 recovery targets. All 795 indexed
lookup calls matched 159 independently derived name samples; they took 55 ms
combined on the rehearsal copy. This measures lookup work, not whole-app startup
or browser latency.

Schema 1000079 is the latest fully verified rehearsal. Historical rule conversion,
remaining cleanup intent, complete source coverage, broader UI/caller conversion,
backup activation and reviewed production cutover remain open. Live services,
workers, bucket policies and the frozen compatible release are unchanged.

## Shared post URLs in metadata policies — 2026-10-05

Native mapping data now exposes the selected post's complete distinct URL set as
`source.urls`, with `source.urls_complete`. Indexed 100-row pages read only that
post. URLs are sorted consistently and may include later observations than the
selected capture. Duplicate observations neither duplicate links nor invalidate
an otherwise unchanged preview; a newly observed URL is rechecked on apply.
The mapping context still excludes internal plugin settings and duplicate edit
input. The ingestion guide includes a mapping that preserves selected entity
links while adding this post's URLs.

A set exceeding 4,096 URLs or 1 MiB of encoded text returns `null` with an
incomplete flag. It never supplies a partial replacement that could erase links.
Other field mappings can still run. Pure and real SQLite tests cover pagination,
empty and oversized sets, unrelated-post exclusion, replay, changed-preview
rejection and restart preservation. The regression failed against the preceding
implementation before the fix; focused actual-source tests and lint pass.

The complete fork gate passed in 1,404 seconds: 612 UI tests, 526 producer tests,
eight library tests, 108 archive tests, 262 backup tests, zero lint issues and
every tagged Go package. SQLite took 1,238 seconds with the documented 30-minute
package timeout override. All five frozen source hashes match the successful run.
This increment changes no schema or UI assets; the independently verified
schema-1000079 rehearsal remains the migration baseline.

Separately, the browser CI for `354693922` initially reported one WebKit
shared-video preview failure in shard 4: playback stayed at time zero. The
targeted test passed twice locally, all 11 local sharing workflows passed, and
the failed-job CI rerun passed on attempt 2. No UI change was needed; the original
failure's cause was not established. Build, lint and image publication also
passed for `112a266fb`. Broader migration, caller conversion, backup and
production cutover gates remain open.

## Retained catalog cleanup and complete source coverage — 2026-10-05

Schema 1000080 retains historical `metadata_prune_queue` entries as immutable,
held native cleanup intents. Those original requests concern obsolete background
metadata targets; they are not media-deletion commands. The importer preserves
the original collection revision, namespaced post key and request time even
when that post no longer exists. Malformed entries retain their original bytes
with a review outcome. No current post, file, alias or background target is
deleted or cancelled by this import.

The bounded native API and standard-library client support resumed batches,
inspection and lost-response recovery. Focused tests cover nonempty queues,
malformed records, absent/empty queues, changed cursors and source hashes,
transaction rollback, schema collisions, startup integrity and anonymisation.
The complete fork gate passed in 1,603 seconds: 612 UI tests, 529 producer tests,
eight library tests, 108 archive tests, 262 backup tests, zero lint issues and
every tagged Go package. API and SQLite tests took 1,186 and 1,311 seconds with
the documented 30-minute package timeout override. All 25 frozen source hashes
match the successful run.

The populated migration passed in 295 seconds. A real Python client imported
cleanup progress for all 1,697 frozen catalogs through an isolated application
API in 99 seconds after opening the database; a deliberately lost response
recovered the same receipt. These snapshots contain no queued cleanup entries,
so populated behavior for nonempty queues is established by the regression
fixtures, not claimed from this archive. Independent comparison passed in
745 seconds: all 36,972,108 typed rows in 277 pre-existing tables and every
original schema object survived unchanged, with exactly the three new tables
and their declared indexes/triggers. Integrity checks passed with no foreign-key
violations. A fresh application reopen passed in 129 seconds, returning all
1,697 cleanup receipts unchanged and preserving the seven disabled policy
imports, eight discovery activation receipts, 757 bound targets and 188 recovery
targets. Schema 1000080 is the latest fully verified rehearsal.

A separate read-only audit compared all 1,697 original manifests, exact
`catalog_info` values, staged record counts/bytes and required source ordinals
against the eleven native import passes. All 22 retained record families and
4,764,236 original rows have their expected outcomes, including explicit review
and unavailable-evidence outcomes. There are no missing or extra outcomes and
no inconsistent counters or source bindings. The audit took 69 seconds. This
closes the frozen catalog source-coverage gap; it does not replace domain
reconstruction checks, historical policy conversion, operational reconciliation
or the fresh production cutover inventory.

Historical policies remain disabled drafts. Cleanup execution/review disposition,
remaining UI/caller conversion, backup activation and production cutover remain
separate work. Live services, workers, bucket policies and the frozen compatible
release are unchanged.

## Relationship names in policy migration clients — 2026-10-05

The native backend already accepts `reference_names`, but the Python policy
migration client still rejected that key. The client now retains relationship
name options in constant and jq mappings, validates boolean switches and their
allowed targets, and removes false switches consistently with native JSON
hashing. Existing native performer-only definitions retain their representation.

The regression reproduced the old rejection before the fix. All 532 producer
tests pass in 46 seconds; the real native API test passes in six seconds with
performer, studio, tag and group mappings, matching plan hashes, a deliberately
lost response and exact receipt replay. The three applied Python files match
their tested source hashes. This client-only change retains schema 1000080.

Complete historical rule conversion remains open. Inspection also confirmed
that the old reader selected cached translations and combined file metadata with
explicit folder defaults. Those behaviors and the replacement for its hidden
cover completeness requirement still need explicit conversion before activating
the seven retained policy drafts.


## Retained translations in metadata policies — 2026-10-05

Native jq mapping data now exposes compact translation choices for the selected
post and the capture's exact original title/caption. Repeated observations and
equal title/caption strings share one result body. Each choice retains language,
provider and the latest known evidence reference; unknown observation times stay
null, and known times normalize to UTC for chronological selection. Original
source metadata remains unchanged. The ingestion guide includes an explicit
English-selection rule with original-title and filename fallbacks.

Indexed reads are limited to the selected post and exact original hashes. The
complete set is bounded to 128 results, 4,096 evidence assertions and 1 MiB of
encoded choices; overflow reports an incomplete set instead of returning a
partial replacement. Changed selected values invalidate an earlier Apply digest.
Explicit clears and preserved entity values retain their precedence. Translation
completion alone does not edit existing media or activate a metadata policy.

The regression reproduced the absent-translation behavior before implementation.
Focused actual-source metadata, SQLite and API tests passed in 19 seconds, and
lint reported zero issues. A read-only assessment independently reconstructed
100 real post samples containing 112 distinct results and 139 matching evidence
assertions. All 300 native helper calls matched those expectations, fetching
shared result text once per request. Choice lookup work took 37 ms combined
(p95 0.13 ms); opening the populated database took 147 seconds separately. These
figures do not measure full source-payload loading, HTTP or browser latency.

The complete fork gate passed in 1,413 seconds: 612 UI tests, 532 producer tests,
eight library tests, 108 archive tests, 262 backup tests, zero lint issues and
every tagged Go package. API and SQLite tests took 1,120 and 1,246 seconds with
the documented 30-minute package timeout override. All five frozen source hashes
match the successful run.

This increment changes no schema or UI assets. Schema 1000080 remains the verified
rehearsal baseline. Complete historical rule conversion, late-result refresh,
remaining UI/caller work, backup activation and reviewed production cutover
remain open. No production service, worker, bucket policy or imported policy
activation changed.


## Historical captures in metadata policy review — 2026-10-05

Changing a collection definition no longer hides its earlier capture evidence
from policy samples. A newly reviewed current policy can use those captures while
retaining the current entity link, file scope and collection guards. Repeated
collection memberships collapse to one selectable capture. Different source
observations remain distinct, and neither preview nor apply rewrites the
original capture's collection revision. Producer admission still requires the
exact revision recorded by the source event.

The regression reproduced both missing samples and rejected previews before the
fix. Focused metadata, SQLite, ingestion and API tests passed against the isolated
draft, covering stale previews, policy rebinding, original provenance, duplicate
memberships, cross-collection exclusion, explicit unlinks and restart. A read-only
assessment checked 100 real historical memberships against independently selected
history rows; the lookup uses the existing collection/capture/revision index.
This SQL assessment does not measure whole preview or browser latency.

The applied source passed the collection, metadata-policy, ingestion and native
HTTP regression selection in 65 seconds. Repository lint reports zero issues,
and all seven applied file hashes match the tested draft. No schema, UI assets,
producer implementation or admission checks changed. The preceding translation
commit passed the complete fork gate; this focused follow-up did not repeat the
unchanged UI, producer, archive or backup suites. The schema-1000080 rehearsal
remains current. Live services and imported policy activation are unchanged.


## Strict UTC dates in native and plugin mappings — 2026-10-05

The shared jq evaluator now provides `utc_date` for calendar dates and explicitly
zoned RFC3339 timestamps, including fractional seconds. It converts an instant
to UTC before selecting its day, retains date-only input, and reports invalid
calendar values or timezone-less timestamps. Missing values return null so a
rule can explicitly omit or clear them. Original source timestamps and date
basis remain unchanged. Native and plugin documentation include an omission
rule that preserves existing dates when the source provides none.

Inspection reproduced two problems with a simple stock jq recipe: fractional
seconds were rejected, and February 30 silently became March 2. The new filter
rejects invalid dates and offsets, supports UTC day crossings and nanoseconds,
and never reads the host timezone. Focused jq, plugin, metadata, SQLite and native
HTTP tests passed in 22 seconds, including application provenance, invalid-date
review and protection of an explicitly cleared date. Repository lint reports
zero issues. All seven implementation and documentation hashes match validation.

A read-only audit examined all 379,449 retained post revisions: 264,884 calendar
dates, 29,078 whole-second timestamps, 78,333 fractional timestamps and 7,154
missing dates. The actual jq evaluator matched independent Python projections
for all 102,537 distinct values in 256-value batches. No invalid or timezone-less
value occurs in this frozen input; rejection behavior is verified by fixtures.
This comparison excludes full source loading and HTTP/browser latency.

Schema 1000080 and the existing selected media metadata are unchanged. No policy
was activated or applied to the rehearsal library. Full file/folder/source rule
conversion and the remaining release gates continue separately.


## Typed defaults for native jq mappings — 2026-10-05

Native jq mappings can now specify a destination-typed `fallback`. It is selected
only when the expression returns an empty stream. Explicit null, false, zero,
empty strings and empty arrays remain real results; expression errors and missing
or ambiguous names still require review. Relationship defaults use native UUIDs,
even when the expression resolves source names. This supports explicit performer
or studio defaults for unsourced scans while retaining source metadata when it
exists. Default decisions record policy provenance without inventing a capture.

Save, draft-preview and migration-import paths validate every default and its
references, including currently unused rules. Migration plans retain target
revision guards and exact replay. Existing definitions omit the optional field,
so their serialized bytes and digests remain stable. The producer migration
client preserves null and empty values through actual API request hashing.

The policy editor provides the existing typed value controls for defaults,
including explicit relationship selection. It retains values across saves and
reloads, clears incompatible defaults when changing the field or source mode,
and explains when a preview uses a configured default. All 16 policy browser
workflows passed in Chromium and WebKit; the final layout also passed the two
focused workflows. Desktop/mobile before-and-after screenshots were reviewed.

Focused native tests passed in 17 seconds, and real Python/API replay tests in
14 seconds. They cover manual scans, source precedence, invalid defaults,
ambiguous names, explicit clears, stale targets, redirects and restart. After
building the real embedded UI, the complete fork gate passed in
1463 seconds: 614 UI tests, 533 producer tests, eight library tests,
108 archive tests, 262 backup tests, zero lint issues and every tagged Go package.
API and SQLite tests took 1124 and 1281 seconds with the
documented 30-minute package timeout override. All 20 source hashes match the
successful run. An earlier invocation stopped on a missing final newline in a
generated browser report; moving that report outside the UI source tree resolved
the formatting failure without changing application code.

Schema 1000080 remains the verified rehearsal baseline. The seven historical
policies are still disabled; complete rule conversion and preview reconciliation
remain open. Production services, workers, bucket settings and the frozen
compatible release are unchanged.


## Shared readable caption mappings — 2026-10-05

The native and plugin jq evaluator now provides `readable_text`, reusing the
translation provider's existing text projection. Common caption markup becomes
plain text with paragraph, list and line breaks; plain strings retain their exact
whitespace and literal entities. Null and empty strings remain explicit results,
so rules choose whether to omit them. Invalid JSON types and strings larger than
1 MiB report errors without exposing the caption in the error message. Retained
source text and stored translation results keep their original bytes.

A read-only assessment of all 379,449 post revisions found 2,723 HTML captions
with 2,304 distinct values. The actual jq evaluator matched the original catalog
reader's projection for every distinct value in 72 bounded batches. Combined
comparison work took 14 ms, excluding database opening, source loading and HTTP.
The assessment creates no metadata decisions and activates no policy.

The regression first reproduced the missing jq function. Focused actual-source
utility, jq, translation, metadata, plugin, SQLite and API tests passed in
17 seconds; repository lint reported zero issues. Coverage includes readable
provider input with unchanged original output, native preview/apply/restart,
explicit-clear protection, absent source values, type/size errors and preserved
source evidence. All 11 applied source hashes match the tested draft. The
preceding typed-default increment passed the full fork gate; this backend-only
follow-up did not repeat unchanged UI, producer, archive or backup suites.

Schema 1000080 remains current. Complete policy conversion, representative
file/folder/source preview comparison and all remaining release gates continue.
No production service, imported policy activation or bucket setting changed.


## Complete disabled policy rehearsal — 2026-10-05

All 1,697 frozen source collections now have explicit native policy bindings in
the isolated schema-1000080 rehearsal. Import updated 1,694 collection scope
revisions while preserving their existing active/disabled state. Only seven
explicit folder scopes apply defaults to ordinary scans; three collections with
no file observations remain unbound for review. The seven earlier folder-policy
drafts and their receipts remain unchanged, giving 1,704 disabled policies in
total. The logical media root remains disabled and unbound, and no source jobs
were admitted.

The bindings preserve legacy settings and rule provenance, including deliberate
replacement or review dispositions. Rules cover retained original/translated
captions, readable text, UTC dates, shared post URLs, explicit folder defaults,
filename fallback and protection of existing selected metadata. The proposed
organized rule uses six native field requirements and separates artwork; it
still requires review before activation. The frozen input's file documents have
no performer, studio, tag, genre or group metadata; five folder documents provide
performers and one provides a studio. These archive-specific conversions do not
claim to implement every possible future legacy configuration.

Native definition validation and 205 independent rule comparisons passed,
including 100 retained original-caption pairs, 100 translation samples, manual
scan defaults and invalid/incomplete inputs. Actual HTTP import and scope updates
took 55 seconds, recovered a deliberately lost response and verified every
receipt again after reopening. A separate comparison checked all 282 tables and
36,973,887 original rows, including SQLite value types. Only the 1,694 declared
current collection revision pointers changed; selected metadata, original source
evidence and jobs remained unchanged. New policies, scope revisions and receipts
matched the frozen plans. Integrity checking passed with zero foreign-key
violations; the comparison took 538 seconds.

Source coverage remains a release blocker: all 414,561 retained NFO captures lack
attachment manifests, while current metadata source selection requires an
attachment-to-media decision. Their original post/file evidence and documents
are retained, but these captures cannot yet be selected through that path. The
next capability is reviewed post-to-scene/image associations independent of
attachment identity or album order, followed by representative populated-library
previews. This must preserve explicit unlinks and current file/entity guards
without inventing album membership. The revised collection scopes also require
reconciliation with saved discovery/enrichment handoffs before activation.

The verified copy is now selected by `.local/native-rehearsal-current.json`.
Removing the superseded closed rehearsal recovered 20.62 GB and left 146 GiB
free, preserving the original compatible snapshot and current native copy.
Private bindings, receipts and comparison reports remain under `.local/`.
Production services, policy activation, bucket configuration and the frozen
compatible release are unchanged.


## Reviewed post-to-media source selection — 2026-10-05

Schema 1000081 adds direct post-to-scene/image decisions independently of
attachment identity and album order. Application APIs expose current choices,
history and durable request receipts. New decisions require current post/media
revisions and the complete reviewed decision set. A media merge that combines
conflicting choices requires review; resolving it preserves original decisions
and their replacement history. Exact request replay survives restart and later
changes.

A whole-post rejection suppresses attachment-derived metadata and automatic
source-gallery membership. Existing gallery synchronization preserves manual
members, cover and ordering; merged conflicts hold synchronization for review.
Ingestion and historical album matching respect the same rejection. The current
metadata-policy sample picker accepts a capture plus either an attachment or a
reviewed post decision, retaining the direct choice in selected-field provenance.
Rejecting a link keeps already selected metadata and its original provenance.
No direct association invents an attachment, an album or depicted performers.

The complete fork gate passed in 1,452 seconds after building real embedded
assets: 615 UI tests, 533 producer tests, eight library tests, 108 archive tests,
262 backup tests, zero lint issues and all tagged Go packages. API and SQLite
tests took 1,149 and 1,276 seconds with the documented 30-minute timeout override.
All 35 source and contract-document hashes match the validated run. Focused
tests also cover NFO selection without manifests, field provenance, stale
decisions, explicit unlinks, merge resolution, UUID adoption, migration failure,
anonymisation and repeated ingestion. All 20 metadata-policy browser workflows
passed in Chromium and WebKit; mobile and desktop screenshots were inspected.

The full isolated library migrated from schema 1000080 and reopened successfully
in 388 seconds. Ten real source scopes returned the same 104 sample choices as
the independently queried baseline in 141 milliseconds, excluding opening and
browser/HTTP time. Independent reconciliation checked all 282 original tables
and 36,980,679 original rows, including value types. Only the declared schema
version transition and new migration-history row changed. Integrity checking
passed with zero foreign-key violations; comparison took 554 seconds. All four
new tables remain empty, and all imported policies remain disabled.

A separate read-only coverage assessment found post/media evidence for 410,090
of the 414,561 retained NFO captures. Coverage does not establish current matching
authority. Bounded historical matching must still validate retained file proofs,
current ownership and explicit choices before selecting associations. That batch
service, association review UI and representative populated-library policy
previews remain required. No production service, worker, policy activation,
bucket setting or frozen compatible release changed.


## Historical post-to-media matching — 2026-10-05

Schema 1000082 adds guarded matching of retained catalog post/file evidence to
native scenes and images. Repeated evidence for one media identity produces one
candidate; independently proven media can share a post. Matching checks current
file and archive generations, retained path/content proofs and unique ownership.
It preserves every explicit post choice, including undecided, and holds competing
observations, merge conflicts and unresolved attachment rejections for review.
These are checks of retained library evidence, not fresh hashes of media bytes.

The application API provides indexed discovery, read-only previews, guarded
apply, immutable request receipts and linked proof references. Selected proofs
and entity revisions are checked again before commit. A retry recovers its
original outcome even after restart or a later unlink. Existing gallery
synchronization runs once per post and protects manual choices; no attachment,
album, selected metadata or performer attribution is invented by a direct link.
The installed `stash-backfill-post-media` client saves bounded immutable plan
parts, verifies their complete contents before applying, and resumes from
server receipts. Its plans retain neither application keys nor plugin settings.

The full fork gate passed in 1,689 seconds: 615 UI tests, 540 producer tests,
eight library tests, 108 archive tests, 262 backup tests, zero lint issues and all
tagged Go packages. API and SQLite tests took 1,233 and 1,362 seconds with the
documented 30-minute timeout override. Full-library discovery then exposed an
optimizer choice that sorted the evidence set on every page. Selecting the
existing post cursor index returned identical 100-post pages in 0.25–0.33 ms,
compared with 0.44–0.50 seconds before, in local SQLite measurements. The isolated
query change passed focused SQLite and actual Python/HTTP tests in 28 seconds
and a subsequent lint run; its hashes are recorded separately from the full gate.

The isolated library migrated from schema 1000081 and reopened in 461 seconds.
Ten source scopes retained the same 104 existing metadata-source choices. An
independent comparison checked all 286 original tables and 36,980,680 original
rows, including SQLite value types. Only the schema transition and new migration
history row changed; all three new tables were empty. Integrity passed with no
foreign-key violations. That comparison took 1,041 seconds while other isolated
validation jobs were also running.

The actual native API and installed client prepared all 254,453 evidenced posts
in 255 saved batches, taking 442 seconds after opening the library. Independent
SQL joins and Python checks verified every saved preview: 411,690 candidates
were supported by 415,028 valid retained file proofs, with no mismatches. The
comparison took 288 seconds, including waits for later batches to be prepared.
Fixture tests separately cover explicit choices, ambiguity, changed generations,
merged media, interrupted writes, lost responses and restart.

Full-library application completed against the candidate copy in 3,239 seconds
after opening it, selecting 411,690 links across all 254,453 posts. One deliberately
lost committed response recovered its original receipt. A cold reopen and receipt
recovery passed in 415 seconds, including 296 seconds opening the database; all
254,453 recovery requests were GETs and no new decisions were submitted.
Independent verification compared every receipt, selected link, immutable
decision and all 415,028 proof references with the saved plans, recomputing request
hashes and deterministic decision UUIDs. It passed in 28 seconds. Final independent
reconciliation compared all 286 original tables and 36,980,680 rows, including
SQLite value types. Only the declared schema history and per-post revision
increments changed; selected metadata, original evidence and gallery memberships
remained intact. Integrity passed with zero foreign-key violations, taking 512
seconds for the complete comparison. The verified rehearsal pointer now selects
schema 1000082. Removing the closed, superseded schema-81 copy reclaimed 20.27 GB
while retaining the original compatible snapshot. Production services, workers,
policy activation, bucket settings and the frozen compatible release are unchanged.

## Native scene/image source review — 2026-10-05

Scene and image detail now provide Sources review through desktop tabs and mobile
section navigation. The initial list performs bounded indexed lookups for only
the selected media, including retained candidates, current links and rejected or
merged choices. Cards show a compact title and date, source URLs and current link
state. Capture details load when expanded, share metadata by revision and
distinguish an unknown historical observation time from the time it was recorded.
Raw source/profile payloads and plugin settings are absent from these responses.

Explicit link, unlink and attachment-default choices retain current post/media
revision guards and the complete reviewed decision set. The browser persists
exact request bytes in deployment-scoped IndexedDB before sending. Lost replies
and reloads recover the original receipt before retrying, and separate tabs
serialize pending choices. Definitive stale-revision rejection allows a new
review; request-UUID conflicts, transport errors and incorrect receipts preserve
the unresolved intent. Successful changes refresh the affected card and relevant
active entity queries. Existing metadata and manual gallery choices remain
protected by the shared association services.

The UI gate passed 624 tests. The final Chromium/WebKit run passed 42 source-review,
scene-detail and image-file checks, including actual separate browser tabs and
deployment prefixes. An earlier stable run also passed all 18 existing metadata
review cases. Desktop/mobile screenshots were inspected and retained. The full
fork gate passed in 1,829 seconds against the frozen set of 23 source/contract
files: 624 UI tests, 540 producer tests, eight library tests, 108 archive tests,
262 backup tests, zero lint issues and all tagged Go packages. API and SQLite
tests took 1,181 and 1,300 seconds with the documented 30-minute timeout override.

Read-only store measurements on the populated schema-81 library inspected 30
scene/image identities and 70 post summaries. Median page time was 1.48 ms and
maximum 19.55 ms, excluding HTTP and browser rendering. The actual query plans
used media-specific indexes for all three source branches. Single-card reads
agreed with their list entries; no payload reconstruction was involved.

## Populated policy preview checks — 2026-10-05

A separate rehearsal opened the schema-82 candidate with SQLite read-only and
query-only modes. Its 212 cases cover 100 retained paired NFO captures, 88
translation-bearing source samples, 12 effective folder-policy cases and 12
held folder-only cases. Source selection found each of the 188 reviewed captures
through the bounded source picker. Independent retained-data projections agreed
for 496 title/details/date values and 188 unions of existing and source URLs.

Of the folder cases, seven effective policies produced unsourced previews with
no fabricated capture or attribution. The other 17 cases were correctly gated
because their collections remain disabled. The initial private harness assumed
all folder collections were active; that assertion failed before any write.
Inspection confirmed the retained disabled definitions, and the final run tests
those gates explicitly alongside the effective policies. Twelve of the original
200 retained-source samples lacked an eligible active, owned media/file pair;
their exclusions are recorded rather than silently described as checked.

All 789 proposed field results with preserved choices stayed protected. Draft
previews of the current definitions agreed with ordinary previews, while
creation simulation identified 100 already-organized cases separately. The
complete check took 3.63 seconds, excluding compilation. The logical library
root remains unbound, so this establishes policy-service behavior and source
selection, not final HTTP/browser file selection against a bound production
root. All policies remain disabled and no library metadata was changed.

A read-only follow-up identified the remaining operational scope reconciliation:
227,443 pending enrichment targets, 324,169 pending translation targets and eight
discovery listing definitions retain earlier collection revisions. Historical
completion and exclusion records also retain their original revisions, as they
should. These counts identify work to inspect and prepare for reviewed handoff;
they are not permission to rewrite historical scope or restart completed jobs.
The three retained source runs still match their current collection revisions.
No source job was admitted or retried by this audit.

Inspection of the actual scheduling contracts narrowed that work. Translation
targets deliberately preserve the historical collection revision of their input
text; their admission and publication do not need it rewritten to the current
definition. All 227,443 pending enrichment targets still name active posts and
collections and have no current job binding, but fetching requires reviewed
handoff to the new collection revision. Of the eight discovery definitions, two
are already superseded historical listings and six remain eligible for scope
review; their saved cursors and recovery relationships must remain intact.

## Reviewed transfer of unstarted enrichment — 2026-10-06

Schema 1000083 adds a bounded review operation for pending enrichment whose
collection definition changed before any worker attempt. It records the original
pending revision, an intermediate review hold, the existing activation receipt
and the replacement pending target in one transaction. Original source scope,
priority and retry deadline remain intact. Any previous worker binding excludes
the target from this path, including an old attempt whose current target revision
is unbound; saved checkpoints stay on their existing recovery path.

The application API discovers candidates through a collection/revision index,
previews up to 100 explicitly selected targets, applies an exact plan digest and
recovers immutable receipts before reconsidering current eligibility. The
`stash-review-enrichment-collections` command saves private immutable batch plans,
checks the reviewed digest again before use and recovers a committed operation
after a response is lost. It does not start jobs, enable policies or change media
metadata. Source, schedule, collection and destination conflicts require review.

Focused native/archive/API tests passed in 30.39 seconds, including existing
activation and enrichment contracts. Tests cover pagination, stale choices,
duplicate destinations, schedules and history, previous worker/checkpoint
preservation, atomic rollback after activation, restart and backup recovery,
anonymisation, migration collisions, and startup refusal before writes when
receipt history or the historical collection definition is corrupt. The actual
Python client reviewed 105 targets in two batches against native HTTP, recovered
a deliberately lost committed response and replayed both batches without another
Apply. Invalid and stale requests return their documented 400/409 responses.

A further read-only audit of the verified schema-82 library found that all
227,443 pending targets in older scopes across 815 active collections have no
worker history at any revision. This supports preparing this review path; it does
not replace a saved-plan comparison or authorize a source job. Independent scope
preparation recorded 2,768 batches with 227,443 unique, unoccupied destinations.
All 815 collection changes add only the logical root and relative directory;
their source definitions otherwise agree with the retained versions. That root
remains disabled and unbound.

The full fork gate passed in 1,866 seconds against the frozen implementation:
624 UI tests, 540 producer tests, eight library tests, 108 archive tests, 262
backup tests, zero lint issues and all tagged Go packages. API and SQLite tests
took 1,302 and 1,415 seconds with the documented 30-minute timeout override.
Fresh producer/backup installations were checked as part of this run. A
consistent schema-82 candidate copy completed in 215 seconds. The implementation
was committed and pushed as `41626b899`; its lint, build and preview-image CI
workflows passed.

The schema-83 migration completed in 147 seconds and a fresh reopen in 122
seconds. Independent comparison checked all 38,885,232 original rows across
289 tables, preserving cell values and SQLite types; only the schema version
and one migration-history row changed. Both new review tables were empty,
integrity checking passed and no foreign-key violations were found. The total
migration harness elapsed time includes an 825-second pause used to serialize
disk-heavy checks; that pause is not migration processing time.

The frozen native API then prepared all 2,768 plans in 103 seconds after opening
the database. Independent verification reproduced the nested and outer plan
digests, deterministic destination identities, exact source revisions and
schedules, and current collection definitions against the untouched schema-82
copy. All 227,443 destinations remained unique and unoccupied; no prior worker
bindings existed. Application through the frozen API completed in 362 seconds,
with exactly 2,768 Apply requests. A deliberately lost committed response was
recovered through its receipt without a duplicate Apply.

Fresh-process recovery reopened the database in 142 seconds and recovered all
2,768 receipts with read-only GET requests in 71 seconds, with zero Apply
requests. Independent receipt verification passed in 19 seconds, reproducing
every input/plan digest, old-target hold and exclusion, replacement target,
schedule and history record. It checked 682,329 added history rows and confirmed
that no worker binding exists for either side of any transfer.

Final reconciliation passed in 238 seconds. All 38,885,232 original rows across
289 tables retain their identities, values and SQLite types except the exact
reviewed changes to the old targets' state, revision, reason and update time,
the schema version and one migration-history row. Selected metadata, source
evidence, translation provenance, discovery state and previous receipts are
unchanged. Integrity checking passed and foreign-key violations remain zero.
Schema 1000083 is now the canonical rehearsal checkpoint. After proving that
the superseded schema-82 database and search cache were closed, their removal
recovered 19.88 GiB and left 137.76 GiB free. The original compatible snapshot,
frozen import inputs and current verified database remain available.

A separate read-only inspection confirms that the six current discovery searches
contain 569 targets and no native jobs, pages, candidates or detail jobs. Their
two superseded originals retain 188 targets and their historical cursors. Those
immutable definitions and recovery links require a separate reviewed binding
operation before future execution. No production service, source job, root or
metadata policy was activated by this rehearsal.

## Reviewed collection bindings for discovery searches — 2026-10-06

Schema 1000084 preserves original listing definitions and adds immutable
collection-binding reviews with a derived effective definition. Only collection
revision/root associations change; source identity, original creation time,
extractor, policy, retry deadline, cursor, target UUIDs and recovery references
remain intact. A prior worker job, even cancelled or failed, excludes a search
from this path. Superseded originals cannot become active again. Source account,
URL, namespace or kind changes require a different source review.

The application API provides bounded candidates, read-only preview, exact-plan
application and immutable receipt recovery. Worker grants, readiness, fetching,
detail verification and capture publication use the approved definition.
Original CreateListing requests and historical activation/recovery receipts can
still resolve their recorded digest after a later review. Clearing a root remains
an explicit null association. Disabled roots remain unable to fetch.

`stash-review-discovery-collections` saves private, non-overwriting plans bound to
the selected endpoint and a reviewed file digest. It verifies the previous and
proposed definition hashes, rechecks the saved file before use, and recovers a
committed receipt before attempting another Apply. Review does not admit jobs,
change a worker credential or activate a root.

Focused native/archive/API checks passed in 49.08 seconds, with SQLite at 46.79
seconds and API at 33.05 seconds. Coverage includes source-change and stale-plan
rejection, prior worker history, changed producer grants, disabled and cleared
roots, ordinary and detail publication, both orders of scope/recovery review,
historical receipt replay, chained reviews, backup restore and anonymisation.
Migration fixtures preserve original searches and refuse unknown-object
collisions. Startup refuses corrupted historical definitions or effective views
before writing. The real Python/HTTP check saved three plans, recovered a lost
committed response without repeating Apply, reopened the database, and recovered
all receipts after subsequent worker admission and collection retirement.

The complete fork gate passed in 1,528.89 seconds: 624 UI tests, 540 producer
tests, eight library tests, 108 portable archive tests, 262 backup tests, zero
lint issues and all Go packages. The API package passed in 1,201.54 seconds and
SQLite in 1,316.47 seconds. All 34 changed source files retained the hashes
recorded before the gate.

An independent read-only audit of the populated schema-83 copy prepared six
exact reviews covering 569 current targets. Only the collection root and path
changed. Two superseded original searches retain another 188 targets and their
saved cursors; neither is eligible for another scope review.

The consistent 21.83 GB database copy completed in 32.33 seconds. Native
pre-migration validation took 273.42 seconds, migration 169.23 seconds and fresh
reopen 154.79 seconds. The independent schema-only comparison then checked every
original row identity, cell value and SQLite type across 291 tables and
40,255,427 rows. Only the schema version/history bookkeeping changed; integrity
and foreign-key checks passed.

The frozen native HTTP harness prepared all six plans and independently checked
their original/proposed definitions, collection inputs and serialized digests.
Apply used six POSTs and seven receipt GETs: the first committed response was
deliberately dropped, and retry recovered its receipt without another Apply.
The six reviews took 49.55 milliseconds after opening the database. A fresh
process then recovered all six receipts with six GETs and no POSTs, taking
17.13 milliseconds after its 140.70-second database open. Separate SQL and
byte-level JSON checks verified all review rows and all eight effective/original
searches, retaining all eight historical activation receipts. No job was admitted.

The final whole-row comparison passed in 195.62 seconds. All original cells
remain unchanged apart from the verified migration bookkeeping; the only
application data additions are the six immutable review rows. The verified
rehearsal pointer now selects schema 1000084. After an open-handle check, removing
the superseded schema-83 database, WAL/shared-memory files and rebuildable search
cache recovered 20.33 GiB, leaving about 139.29 GiB free. The original compatible
snapshot and all saved review/reconciliation evidence remain retained.

The code checkpoint is `de9feaf7d`. Bound-root HTTP/browser policy verification,
live worker conversion and the full plan's remaining release gates are still
required. No production settings, services or source jobs have changed.

## Populated metadata previews with real file bindings — 2026-10-06

The schema-84 rehearsal now exercises the native HTTP sample-file and source
pickers, saved-policy previews, draft previews and creation-event previews against
the actual media paths. A private mount namespace exposes the media read-only at
its recorded path; only the isolated database and harness directory are writable.
The server admits the required read/preview routes and temporary root binding
changes. It starts no worker or metadata-application endpoint.

All 212 retained cases passed: 121 images and 91 scenes, comprising 188 source
cases and 24 folder cases. The requests preserved the prior independently
verified source values, protected fields and URL unions. All 195 eligible cases
reported the saved disabled policy; the other 17 remained gated by their
collection. Creation previews also matched the saved organized-at-creation and
disabled-policy outcomes. The driver made 401 GETs, 620 read-only POSTs and two
root-binding PUTs in 4.45 seconds after a fresh 140.36-second native database open.
The complete invocation took 145.01 seconds.

The first attempt used an incorrect private-harness sample URL, omitting the
`metadata-policy` path component. The production client already used the correct
route. The failed attempt restored the root and retained its two history rows;
correcting the driver required no product-code change. The successful retry
likewise restored the disabled, unbound root. Independent SQLite checks compared
all 1,483 schema objects, all 292 table counts, every original root-history row
and both root heads. Each attempt added exactly its two expected root revisions;
the other root stayed unchanged. This is a scoped root/schema/count check, not
a repeated whole-library cell comparison.

Private evidence is in `.local/native-bound-policy-preview-20261006/`, including
the frozen inputs, failed-attempt record, HTTP reports and independent root
reconciliation.

The populated browser rehearsal also passed all 32 cases in 61.31 seconds:
eight representative scene/image samples, each at 390 and 1280 pixels in
Chromium and WebKit. The samples cover retained paired NFOs, translations,
effective rules without a source capture, and disabled collection gates. The
fixture uses the production component and CSS, actual GraphQL resolvers and
native API routes, and the application's scrolling layout. Its 908 copied UI
source files still match the working source hashes; no API responses are mocked.
The browser searches the library, selects the actual file and capture, compares
32 existing-item and 24 creation previews against the retained expectations,
opens/closes the sample-data section and checks its JSON. It also checks both
the document and content container for horizontal overflow and rejects failed
resources, page errors and unexpected mutation requests. Representative phone-
and desktop-width screenshots were inspected.

The browser invocation took 204.90 seconds, including a fresh 142.83-second
native database open. Its server handled 32 GraphQL searches, 161 GETs, 56 draft
preview POSTs, one directory-probe POST and two root-binding PUTs. Two earlier
browser attempts exposed fixture module-loading and scroll/font configuration
issues; their artifacts and root history are retained. Correcting those fixture
issues required no product-code changes. Independent checks again verified all
schema objects, table counts and root history. The selected rehearsal root is
disabled and unbound at revision 11; the other root is unchanged. The canonical
rehearsal pointer now includes the HTTP and browser receipts. These checks cover
the populated preview component; separate application-navigation checks remain
the evidence for its surrounding routes and drawer.

The broader native UI, live caller conversion, backup/restore and production
cutover gates remain open; production has not changed.

## Standalone source-post inspection API — 2026-10-06

The application can now browse source posts without starting from a scene or
image. UUID pages and exact native UUID, qualified source-ID or retained-URL
lookups return compact summaries. URL lookup retains multiple matching posts;
it does not invent a consolidation decision. Summaries include at most three
identifiers and URLs, with explicit continuation flags, plus a bounded latest
capture excerpt. Unknown historical observation times remain distinct from
recording times. No raw post/profile payloads are loaded for these pages.

Separate targeted endpoints expose all qualified identifiers, selected capture
publishers, media associations and the current album choice. Publisher accounts
resolve consolidation and explicit unlink decisions without selecting depicted
performers. Media rows deduplicate redirected identities before cursor paging,
preserve conflicting or rejected choices and report attachment evidence
separately. They do not repeat post captions/captures per media item. Deleted
media/gallery identities retain their UUID and state without an active local ID.
Album inspection preserves disabled choices and never creates a gallery.

The media candidate query starts from the selected post's evidence, explicit
choices and current attachment links. It resolves at most 8,192 retained media
identities and reports an oversized review instead of hiding a partial set.
Indexed reverse redirects preserve decisions made before a media merge. Existing
scene/image source review shares the association inspection code.

Focused SQLite and HTTP tests pass, covering URL ambiguity, qualified identity
lookup, pagination, compact Unicode titles, unknown capture clocks, rejected
links, merged conflicts, deleted identities, publisher consolidation/unlink and
disabled gallery choices. Query-plan assertions verify indexed post/URL/identity
entry points. The first focused attempt could not write the sandboxed Go cache;
the first cache-enabled run exposed an invalid namespace in a new test fixture.
Correcting that fixture required no domain-code workaround.

Read-only checks on the populated schema-84 rehearsal also passed for 100 post
samples and 603 timed statements, with independent candidate/URL comparisons
and indexed query plans. The database's size and modification time were unchanged.
This checks SQLite statements, not end-to-end API latency or a new full-library
reconciliation. Private receipts are in `.local/native-post-browser-20261006/`.

The full validation sequence passed across two runs. Generation and the real UI
build, 624 UI tests, 540 producer tests, eight library tests, 108 archive tests
and 262 backup tests passed before two staticcheck conversion findings stopped
the initial gate. After replacing the two equivalent identifier struct literals
with typed conversions, `validate-backend` passed with zero lint findings and the
complete Go integration suite. The staged UI was kept separate from those inputs.
Receipts are `native_post_browser_full_gate` and
`native_post_browser_backend_gate` under
`.local/native-discovery-client-20261004/`.

This increment changes no schema or production data. The standalone page and
desktop/mobile navigation, broader archive management/manual intake and all
remaining caller conversion, compatibility removal, backup/restore and cutover
gates remain open.

## Standalone source-post browser — 2026-10-06

Source posts now has a native application route, desktop utility-menu entry and
mobile drawer entry. Scene/image source cards open the selected post directly.
The browse page supports exact stored URLs, qualified source IDs and archive
UUIDs with 25-row cursor pages. A shared URL can return multiple posts; opening
one does not infer that they are duplicates. Direct links load the selected
post without fetching the browse queue.

The detail page groups shared text and captures by revision and loads publisher
accounts, media associations, album choices and identifiers independently on
expansion. Current links, rejected choices, conflicts and retained evidence stay
distinct. Deleted library identities remain visible without links to reused
local IDs; disabled albums remain disabled. Account/media actions lead to their
existing review controls. Inspection performs no writes. The implementation
uses the existing Base UI wrappers, typed forms, localized labels and shared
scrolling/navigation conventions.

The new client passes 15 tests for exact filters, session/deployment prefixes,
response identity guards, cursor ordering, unknown observation times and
deleted/redirected associations. Integrated Chromium and WebKit checks passed
all 32 cases, covering the standalone page and existing scene/image review on
phone and desktop widths, direct navigation, lazy reads, pagination, retry,
disabled albums and overflow. Browser responses in these checks are fixture
APIs; they do not establish populated-archive browser latency. The earlier
backend HTTP and populated SQLite checks are separate evidence.

The first integrated browser run overlapped with code generation, which caused
Vite hot reloads and duplicate fixture roots. The trace confirms those reloads;
rerunning after generation finished passed without changing product code or
weakening browser assertions. Private reports use the
`native_post_browser_integrated_` prefix under
`.local/native-discovery-client-20261004/`. The integrated full fork gate passed
in 1,498.28 seconds: generation and real embedded assets, 639 UI tests, 540
producer tests, eight library tests, 108 archive tests, 262 backup tests, zero
Go lint findings and the complete Go integration suite.

This increment changes no schema, production service or worker activation.
Broader archive management/manual intake, caller conversion, compatibility
removal, backup/restore and reviewed production cutover remain open.

## Performer source accounts and identity history — 2026-10-06

Performer pages now expose current account ownership in a desktop tab and mobile
section. Existing links offer Manage account link and open the selected native
account review. An empty state explains that purchased/directly scanned media
needs no source account. Identity history expands separately and shows retained
UUID redirects and creation/retirement times; old local IDs are historical
values without links to reused records. These reads never assign depicted
performers or create source accounts/posts.

Two targeted APIs resolve the requested performer UUID and enumerate current
canonical accounts or retained performer identities. Account lookup follows
performer merges and UUID adoption, excludes superseded/unlinked/undecided
choices, and deduplicates consolidated accounts. Reverse identity groups are
bounded to 1,024; oversized groups report an error instead of truncating ownership.
Both lists use UUID cursors and independent read transactions. The UI rejects a
changed canonical performer between pages and offers a fresh read.

Focused SQLite, HTTP and query-plan tests pass using the staged source overlay,
including merge/adoption, account consolidation and unlink, deleted performers,
reused local IDs, pagination, invalid scopes, restart and oversized history.
Read-only populated SQL checks compared 110 performers against an independently
resolved current-head/redirect map, including the existing imelizabethtran
identity. All four imported current account links were included in that map.
The 220 timed statements and indexed plans passed; the rehearsal database size
and modification time stayed unchanged. This is SQL-statement verification,
not populated browser/API latency or a new full-library reconciliation.

The first populated plan check found that SQLite preferred scanning its small
four-row ownership-head table. The query now explicitly selects the account-head
index, and focused tests plus the populated comparison pass again. No data or
schema migration was required. The client passes five scope/response tests and
the staged UI passes TypeScript, browser types, contract lint and localization.
All 12 Chromium/WebKit cases passed for desktop/mobile layout, merged ownership,
lazy history, manual-media empty state, retry, bounded pagination and changes
between pages. Representative screenshots were inspected. All copied UI source
files match the integrated application after excluding one generated timestamp
comment; no generated source was edited. Browser API responses are fixtures.

Private source overlays, browser receipts, source comparison and populated SQL
reports are under `.local/native-performer-sources-20261006/` and use the
`native_performer_sources_` check prefix in
`.local/native-discovery-client-20261004/`. The integrated full fork gate passed
in 1,532.92 seconds: generation and real embedded assets, 644 UI tests, 540
producer tests, eight library tests, 108 archive tests, 262 backup tests, zero
Go lint findings and the complete Go integration suite. This increment changes
no schema or production state. Remaining archive management/manual intake,
caller conversion, compatibility removal, backup/restore and production cutover
gates remain open.

## Ordered source album inspection — 2026-10-06

Source posts and gallery detail pages now share a read-only ordered album view.
It retains mixed image/video positions, repeated media and compact missing
ranges, and separates list completeness, attachment choices, whole-post link
conflicts, manual gallery exclusions and registered file counts. A file count
is not a mount-availability check or evidence of a running/failed download.
Disabled gallery choices, deleted media/gallery identities and forgotten posts
remain inspectable; deleted local IDs are not navigable. Gallery reverse lookup
follows up to 1,024 retained identities through indexed current associations and
keeps every post's own source order.

The APIs use bounded source-list reads without reconstructing post/profile
payloads or writing gallery state. A response signature prevents the UI from
combining pages with changed selections or associations. The desktop tab and
mobile section use shared Base UI wrappers; post order expands independently.
Manual galleries remain valid with no fabricated source post.

Focused repository/HTTP/query-plan checks cover gaps, repeated media, paging,
manual exclusions, rejected/conflicting links, merge redirects, deletion, reused
IDs and restart. One test initially used a stale gallery revision after scene
removal; reading its actual current revision fixed the fixture. A later
registered-file fixture omitted the required primary flag and was corrected.
Six new client tests and 15 existing post-client tests passed, as did TypeScript,
browser types, contract lint and localization. All 28 staged Chromium/WebKit
album and post-browser cases passed with fixture APIs; representative mobile and
desktop screenshots were inspected. These browser checks do not establish
populated-archive latency.

Private staged sources and receipts are under
`.local/native-album-browser-20261006/`, with check results using the
`native_album_browser_` prefix in `.local/native-discovery-client-20261004/`.
Read-only repository checks on the populated schema-84 rehearsal also passed:
95 posts, 143 album pages, 314 returned rows and 50 galleries. Source positions
matched an independent join of selected immutable lists; gallery lookups matched
a separately resolved map of all 559 current gallery heads. The sample included
133 selected media slots but no repeated attachments or missing ranges; those
cases are covered by the focused fixtures, not this sample. Timed repository
calls totaled 104 ms, with a 2.88 ms maximum. This is neither end-to-end HTTP/UI
latency nor a full-library reconciliation. SQLite query-only mode was enabled,
and database size/mtime were unchanged.

All integrated and staged UI source files match except the generated timestamp
comment, normalized in memory for comparison. The full fork gate passed in
1,518.68 seconds: generation, real embedded UI assets, 650 UI tests, 540 producer
tests, eight library tests, 108 archive tests, 262 backup tests, zero Go lint
findings and the complete Go integration suite. This increment changes no schema
or production state. Guarded album
association/selection editing, backfill controls, live download-state inspection,
manual intake, remaining caller conversion, backup/restore and cutover remain open.

## Historical album matching review — 2026-10-06

Post and gallery source-album views now expose Match existing media. A typed
policy form defaults to source IDs and offers the original Reddit filename
policy explicitly. The read-only preview separates verified matches, preserved
choices, unresolved candidates, new-gallery metadata and membership changes.
Candidate file evidence expands independently, and large local lists render in
bounded batches. Disabled, ineligible, conflicting and unchanged previews do not
offer Apply. Opening the controls never downloads files or submits a job.

Apply, retry and cancellation save their exact intent before making a mutation.
The deployment-scoped browser journal serializes competing tabs. Lost submission
responses recover through the original receipt; opening an admitted job performs
only reads. Generic job/UUID conflicts stay pending until verified. Cancellation
has no server receipt, so recovery checks the original job's current state. A
proven stale cancellation reloads that job before offering another action.
Neither cancellation nor notification retry undoes or repeats committed changes.
The UI reports publication separately from notification completion and preserves
the original event across explicit retry. Visible active jobs poll read-only;
job history and attempts load independently with bounded cursors.

Publication refreshes the affected source order and enclosing gallery association.
It does not rescan the library or remount the review controls. Shared route links
now merge button classes consistently, preserving their outlined appearance.
All text uses localized labels and existing Base UI wrappers.

Nineteen new client tests and six existing album-reader tests pass. TypeScript,
browser types, React/data-contract lint and localization were checked in the
private UI stage. The first browser fixture omitted the identity route's prefix
handling; another fixture retained file/link values after clearing its media.
Those fixtures were corrected. The browser also caught a recovery prompt showing
before the original request settled; that UI timing is fixed. StrictMode's
duplicate read effects are accounted for while still checking that a committed
event refreshes its association once. All 24 new Chromium/WebKit cases pass,
including phone/desktop preview and lost-response recovery, concurrent tabs,
wrong receipts, stale previews/cancellation, committed notification retry,
post-gallery refresh and bounded history. The 28 existing album/post browser
cases also pass. After the final link-class adjustment, all four affected visual
cases passed again and representative screenshots were inspected. These browser
checks use fixture APIs, not populated-archive browser latency.

Staged sources and promotion hashes are under
`.local/native-album-review-20261006/`; check receipts use the
`native_album_review_` prefix in `.local/native-discovery-client-20261004/`.
The integrated full fork gate passed in 1,515.7 seconds: generation, real embedded
assets, 669 UI tests, 540 producer tests, eight library tests, 108 archive tests,
262 backup tests, zero-issue Go lint and complete Go integration. Its receipt is
`native_album_review_integrated_full_gate.json`; all 18 promoted source hashes
still match the browser-checked stage. This increment changes no schema or
production state. Explicit album association/source-list editing, live download
states, manual intake/post consolidation, remaining caller and compatibility
conversion, backup/restore and reviewed production cutover remain required.


## Source-list selection review API — 2026-10-06

The application can now inspect each distinct retained attachment list once,
compare an exact list with the current choice, and save a pinned, automatic or
disabled selection. Repeated captures share one list row and a bounded witness
lookup. Preview and Apply use the existing source-order validation; automatic
selection starts with the chosen list and permits later compatible ingestion.
Selection changes leave gallery membership, media associations, metadata and
downloads to their separate reviewed operations.

Schema 1000085 adds immutable `attachment_selection_reviews` with the exact
request, original decision and integrity signature. Apply validates the current
post revision and preview digest, commits choice and receipt together, and
recovers an identical original request before checking later state. Reusing a
request UUID for different input is a conflict; a changed preview has a distinct
response. Startup and receipt reads validate original decision history and scope.
Anonymisation removes receipts before source evidence. Original receipts survive
later choices, source forgetting, restart and portable export/relocated restore.

Focused repository and real HTTP checks cover guarded writes, immutable history,
unique-list pagination, shared captures, sparse/repeated positions, transaction
rollback, exact replay and cross-origin rejection. Query-plan checks verify
scoped indexes. The complete fork gate passed in 1,715.8 seconds, including
generation, real embedded assets, 669 UI tests, 540 producer tests, eight library
tests, 108 archive tests, 262 backup tests, zero Go lint findings and the complete
Go integration suite.

The full schema-84 copy migrated and reopened successfully. An independent
read-only comparison verified all 40,255,444 original rows across 292 tables,
including cell types and binary string values, with the separately verified
schema-version change and one new migration-history row. Existing schema objects,
selected metadata, source evidence, policy activation and SQLite sequences were
preserved; integrity and foreign-key checks passed. The new receipt table is
empty on this copy: no source-list choice or job was activated.

Read-only repository checks covered 110 populated posts, 110 unique lists
representing 589 captures, 330 previews and 630 returned source entries. Their
positions and identifiers matched independent SQL. Timed repository calls totaled
58 ms, with a 2.73 ms maximum; this does not measure end-to-end browser latency.
The database size/mtime remained unchanged. Private scripts and reports are under
`.local/native-album-selection-review-20261006/`; validation receipts use the
`native_album_selection_review_` prefix in `.local/native-discovery-client-20261004/`.
The source-list editor has separately passed staged phone/desktop browser checks,
but is not part of this backend increment. Production remains unchanged.


## Source-list selection editor — 2026-10-06

Post and gallery source-order views now expose Choose source list. The typed
form pages distinct retained lists, previews the current and proposed order, and
saves a pinned, automatic or disabled choice. Source positions preserve gaps and
repeated media references. Previewing and saving order are separate from applying
gallery membership. Source-list history and its large reference sets expand in
bounded pages through existing Base UI components and localized labels.

The browser saves exact requests before mutation. Its deployment-scoped journal
serializes competing tabs, checks the original receipt before retrying, and
retains uncertain conflicts. Opening the panel performs reads only. Only a proven
stale preview can be cleared for review again; a fresh form replaces that preview
even if the post revision has not changed. Saved order refreshes the affected
post without reloading gallery associations or remounting matching controls.
Refresh failures remain distinct from successful saves. Delivery may finish after
the panel closes and still refreshes the affected source order.

Nineteen client/journal tests pass. TypeScript, browser types, React/data-contract
lint, Biome accessibility checks and localization passed in the private stage.
All 74 Chromium/WebKit cases passed: 22 new source-list cases plus 52 existing
album/post cases. The new cases include phone/desktop read-only previews and
lost-response recovery, repeated positions, stale/conflicting requests, wrong
receipts, competing tabs, post-save refresh failures, closing during Apply,
disabled selection with no lists and independent history/list paging.
Representative WebKit phone and Chromium desktop screenshots were inspected.
These checks use fixture APIs, not live archive browser latency.

All 12 promoted UI files match the checked stage; the private browser filesystem
allowlist was excluded. Sources, hashes and receipts are under
`.local/native-album-selection-review-20261006/` and
`.local/native-discovery-client-20261004/`. The integrated full fork gate passed in
1,572.6 seconds, including generation, real embedded UI assets, 688 UI tests,
540 producer tests, eight library tests, 108 archive tests, 262 backup tests,
zero Go lint findings and complete Go integration. All promoted source hashes
still match the checked stage. This increment changes no schema or production
state. Explicit gallery/attachment associations, live download states, remaining
archive workflows, caller conversion, production backup proof and cutover remain
required.

## Gallery and attachment association review APIs — 2026-10-06

Typed, read-only previews now validate an explicit post-to-gallery or
attachment-to-media choice. Apply saves the decision with its exact original
request in schema 1000086; receipts and bounded history support recovery after
lost responses, later choices, UUID adoption and restart. Choices preserve
gallery membership, manual exclusions, metadata and files. Attachment review
respects a post-wide rejection and accepts media converted from an image to a
video. Separate matching/synchronization controls handle gallery effects.

A reproduced gallery adoption bug checked only the supplied UUID and missed a
retained claim on an older UUID after a merge. Adoption now checks the bounded,
indexed reverse identity group. Another post cannot take a claimed survivor;
the original owner can explicitly reassociate or disable its claim. Tests cover
two-step merges, preserved members and the 1,024-alias limit.

Focused SQLite and real HTTP checks pass, including current revision/kind/scope
guards, post rejections, read-only previews, exact replay, separate request/stale
conflicts, same-origin enforcement, late-error rollback, immutable history,
deletion/forgetting, anonymisation and startup corruption rejection before writes.
Portable export and relocated restore preserve both receipt families and their
original decisions. The selected attachment-kind query uses scoped indexes on
the populated rehearsal database. The complete fork gate passed in 1,843 seconds,
including generation, embedded assets, 688 UI tests, 540 producer tests, eight
library tests, 108 archive tests, 262 backup tests, zero Go lint findings and the
complete Go integration suite.

The schema-85 copy migrated to schema 1000086 and reopened successfully. Independent
comparison verified all 40,255,445 original rows across 293 tables, including cell
types and binary string values, with the separately verified schema-version
change and one new migration-history row. Existing schema objects, selected
metadata, source evidence, policy activation and SQLite sequences were unchanged;
integrity and foreign-key checks passed. Both new receipt tables remain empty:
the rehearsal applied no association choice and admitted no source job.

Read-only repository checks covered 108 populated posts, 124 attachments and 522
previews. Timed calls totaled 136 ms, with a 4.47 ms maximum. This measures backend
queries, not browser latency; database size and modification time were unchanged.
Sources and private evidence are under
`.local/native-gallery-association-review-20261006/`, with test receipts under
`.local/native-discovery-client-20261004/native_source_association_review_*`.
Production remains unchanged. The association editor is staged separately, with
client tests and type checks passed; its browser validation and the remaining
transition gates are not complete.

## Gallery and attachment association editor — 2026-10-06

Source-album views now provide Edit gallery link and per-attachment Edit media
link controls. Typed forms use bounded library searches, resolve the selected
entity's current UUID/revision and compare current and proposed choices before
Apply. Gallery choices can be linked or disabled; attachment choices can be
linked, rejected or returned to automatic matching. Converted image-to-video
media is supported. Existing reasons are retained, and field changes discard
the previous preview. Association choices leave membership, metadata and files
to their separate operations.

Both editors share a deployment-scoped browser journal. They save exact requests
before delivery, recover original receipts and preserve uncertain conflicts.
Only a proven stale request can be cleared for review again. Opening the editor
performs reads only. Saving refreshes the affected source order and enclosing
association; a closed attachment dialog can finish delivery and refresh repeated
slots. Successful saves and failed refreshes are reported separately. Immutable
history loads independently in bounded pages through existing Base UI wrappers.

Nineteen client/journal tests pass, along with staged TypeScript, browser types,
React/data-contract lint, Biome accessibility checks and localization. All 102
Chromium/WebKit browser cases passed: 28 new association cases and 74 existing
album/post cases. New cases cover phone/desktop preview and save, lost-response
recovery, stale and uncertain conflicts, wrong receipts, refresh failures,
rejection/automatic choices and closing during attachment delivery. An initial
test helper incorrectly expected a fresh form when reopening a pending request;
it now expects the recovery controls. Representative Chromium desktop/phone and
WebKit phone screenshots were inspected. These checks use fixture APIs, not
populated-archive browser latency.

All 17 promoted UI files match the checked stage, excluding its private browser
filesystem allowlist. Sources and hashes are under
`.local/native-gallery-association-review-20261006/`; check receipts use the
`native_source_association_` prefix in `.local/native-discovery-client-20261004/`.
The integrated full fork gate passed in 1,567.5 seconds, including generation,
real embedded assets, 707 UI tests, 540 producer tests, eight library tests,
108 archive tests, 262 backup tests, zero Go lint findings and complete Go
integration. All 18 frozen source hashes match the passed gate. Production is
unchanged; live download states, remaining archive workflows, caller/compatibility conversion,
production backup proof and reviewed cutover are still required.

## Mixed-media source album playback — 2026-10-06

Source-album views now open one image/video viewer in retained source order.
Repeated attachments retain separate positions, and unknown ranges stay compact
placeholders. Playback uses the selected library media kind, including images
converted to videos. Explicitly rejected/conflicting links, manual exclusions,
deleted media and attachments with no selected item or registered files remain
visible without fetching media. Registered files permit a playback attempt; they
do not prove that bytes are online or that a download is underway.

The viewer reuses the existing image zoom and scene player. Mobile images zoom
inside the album, adjacent videos retain one player, and closing releases
playback. Exact selected IDs guard asynchronous media responses. Source pages
load only when reached, retain position on retry and stop playback if a later
page has a different source signature. The viewer changes no associations or
metadata; ordinary configured scene activity tracking remains in the player.

Eight focused tests passed, alongside staged TypeScript, browser types,
React/data-contract checks, Biome and localization. All 18 new Chromium/WebKit
cases passed across the final runs, covering phone/desktop playback, gaps,
unavailable choices, deleted media, late responses, bounded paging and retry,
changed lists, adjacent-video reuse, close cleanup and mobile zoom reset. An
adjacent-video fixture initially referenced a nonexistent UUID constant; after
correction both browser cases passed. The 42 existing association/album cases
also passed. Four final image/video visual cases passed after arranging the
mobile navigation buttons together; representative desktop and phone screenshots
were inspected. These checks use synthetic media and fixture APIs.

All ten promoted files match the checked stage under
`.local/native-album-playback-20261006/`, excluding the private browser filesystem
allowlist. Receipts use the `native_album_playback_` prefix in
`.local/native-discovery-client-20261004/`. The integrated full fork gate passed
in 1,562.9 seconds, including generation, real embedded assets, 715 UI tests,
540 producer tests, eight library tests, 108 archive tests, 262 backup tests,
zero Go lint findings and complete Go integration. All 11 frozen source hashes
matched the passed gate before updating this validation record. This increment
changes no schema or production state. Live attachment
download reporting, manual intake, post consolidation, remaining caller and
compatibility conversion, production backup proof and reviewed cutover remain open.

## Attachment download reporting backend — 2026-10-06

Schema 1000087 adds immutable download reports tied to exact source attachments,
capture receipts and historical worker attempts. Reports distinguish started,
downloaded, failed, excluded and skipped transfers. One start and one terminal
report per transfer preserve original acknowledgements and delayed delivery.
Downloaded reports require the exact attachment's file-verification receipt;
they do not mark media imported or online. A bounded application history endpoint
derives active/interrupted state from the current server lease and keeps file
verification status separate. Reads leave metadata, associations and files intact.

Focused service and SQLite checks pass for scoped ownership, late and reversed
delivery, expiry, exact replay, conflicting phases/sequences, receipt failure
rollback, missing-receipt commit rejection, restart, indexed history, anonymisation,
startup corruption refusal and populated schema-86 receipt preservation. The real
HTTP handler test and standalone export/relocated restore also pass. An HTTP
fixture initially used a sub-millisecond source-window boundary, which the existing
run contract rejects; correcting the fixture to millisecond precision passed.

Sources and stage evidence are under `.local/native-attachment-download-20261006/`;
validation receipts use `native_attachment_download_` in
`.local/native-discovery-client-20261004/`. Existing ingestion/source-run and
prior migration regression checks pass. Generation, embedded UI, 715 UI tests,
540 producer tests, 8 library tests, 108 archive tests and 262 backup tests passed.
The initial full command stopped on a test-only `sqlclosecheck` issue. Changing
that query cleanup to `defer` was the only code edit before clean lint and the
complete Go integration suite passed; both command results are retained in
`combined-full-gate.json` rather than describing the initial command as successful.

The full-copy schema-86 migration and fresh schema-87 reopen passed. Independent
comparison verified all 40,255,446 original rows, preserving exact cell values,
source evidence, metadata selections and disabled policies. Integrity passed with
zero foreign-key violations. The only domain change is the empty download-report
table and the receipt constraint expansion, with the expected migration history
and version transition. Producer outbox and gallery-dl lifecycle delivery, album
status UI and actual caller activation remain separate required work. Production
remains unchanged.

## Durable producer download reports and receipt backups — 2026-10-06

The gallery-dl adapter now queues starts before bytes and terminal reports for
downloaded, failed, excluded and unresolved skipped outputs. Fallback URLs retain
one transfer. Capture row sequences survive acknowledgement and restart; reports
keep the original source-attempt owner/fence even when the current file finishes
after lease loss. Both the file event and downloaded report must be durable before
gallery-dl updates its archive. Queue exhaustion leaves the archive unacknowledged
and retains completed files for recovery. Worker execution requires the new
backend capability before claiming download work.

Outbox schema 16 preserves older event bytes, acknowledgements and every existing
request/job journal. Delivery waits for exact capture/file dependencies and rejects
an acknowledgement that claims media intake or names a different capture. The
standalone archive verifier now supports those queues, checks retained report
history and dependency scope, and preserves pending/lost-response bytes.

All 551 producer tests and 113 standalone archive tests pass. The first private
producer run found one configuration-migration assertion that still expected only
capture/file events; the corrected assertion checks the downloaded report and
file dependency, and the complete promoted suite passed. The real Go HTTP worker
test passes for the CLI, host launcher and n8n fixture, including deliberately
lost download-report and run-finish responses. Each fixture exports/restores the
native library and producer queue, compares receipt-boundary proofs, validates
the relocated schema and recovers the original download receipt after reopening.
The complete API suite also passed in 574 seconds, along with clean Go lint,
all 262 host backup tests and the remaining Go/Python snapshot and collector
checks. Validation receipts and the 23-file source manifest are retained under
`.local/native-attachment-download-20261006/`. Native live worker activation,
installed runtime/profile refresh, album status UI and the remaining transition
gates remain open. No production configuration or bucket policy changed.

## Grouped download status and history — 2026-10-06

Attachment reports now have a grouped read API and a bounded batch summary for
album cards. Each transfer keeps the first server receipt as its stable cursor;
delayed start/outcome delivery updates one entry without moving it between pages.
The production query uses attachment receipt ordering and the existing unique
phase index, limiting scope/label joins to the selected page. Original immutable
reports and receipts remain available. No schema change is required.

Album cards share status reads for at most 25 distinct attachments. Visible groups
refresh without overlapping requests, and offscreen/hidden groups stop polling.
Download history uses the existing Base UI dialog and expandable sections, with
source/root labels, outcome explanations, separate file-check status, received
and reported times, and optional technical references. Older entries paginate
on demand. Existing library links and playback remain independent of reports.

Focused repository/history and production-query-plan tests passed, including
terminal-before-start delivery, stable pagination and expired run ownership. The
actual CLI/host/n8n HTTP fixture and portable restore passed with the new routes.
Eight client/polling tests, application/browser types, lint and locales passed.
All 106 affected Chromium/WebKit browser cases passed. Eight final history
cases also passed after keeping the dialog title/close control visible during
scrolling; the WebKit mobile screenshot was inspected. The integrated full fork
gate passed: generation and embedded UI, 723 UI tests, 551 producer, eight library,
113 archive and 262 host backup tests, clean Go lint and complete Go integration.
The initial gate stopped on a Go parser style check; after an equivalent switch
rewrite, the resumed Go gate passed in 1,383.7 seconds. Both receipts are retained.

Evidence is under `.local/native-download-status-20261006/` and uses
`native_download_status_` validation receipts. Native worker activation, manual
intake/post consolidation, remaining caller and compatibility work, complete
production backup/restore and cutover remain open. Production is unchanged.

## Application local-file admission — 2026-10-06

Purchased videos and images now have application-authenticated preview, guarded
admission, saved-request recovery and revision-checked cancellation. A bound
folder/manual-batch collection supplies native performer and filename rules.
The operation creates no producer, source account, post or capture. It uses the
existing immutable archive-job submission and `media.verify` worker, so no schema
change or new populated database copy is needed.

Preview binds the confined filesystem identity, path generation, root,
collection and policy revisions. Admission repeats those checks through commit.
The worker verifies actual bytes and probes media before committing registration,
then resumes generated previews and durable notifications separately. Stale
manual reviews fail before registration; an interrupted notification retry does
not reapply metadata. Cancellation retains any already committed registration.
An unavailable worker refuses new work but still recovers prior admissions.

Focused tests passed for real MP4 and image imports with a purchased-only
performer, filename inheritance, explicit title clears, idempotent rescans,
replaced files and restored modification times, changed policies/roots, admission
rollback, scoped HTTP access, interrupted effects and cancellation. Portable
export/relocated restore passed independent row comparisons and fresh Go request
recovery for completed and pending work, with zero producer or source records.
The integrated full fork gate passed in 1,491.8 seconds: generation and embedded
UI, all 723 UI tests, producer/library/archive/backup suites, clean Go lint and
complete Go integration tests.

Evidence is retained under `.local/native-manual-intake-20261006/` and the
`native_manual_intake_` check receipts. The native file picker, batch workflow,
explicit retry controls and remaining transition gates are still required.
Production and live workers are unchanged.


## Collection file picker, batch review and explicit retry — 2026-10-06

Native folder/manual-batch collections now expose a bounded file picker through
the existing desktop/mobile collection route. The selected folder is confined
to the reviewed root and collection prefix. Listings use sorted continuation
pages with directory/root/definition checks, skip unsupported visual inputs and
partial files, and reject directories beyond the explicit inspection limit.
They do not scan the library or recursively traverse collections.

A batch of up to 25 files uses the collection's existing performer/filename rules.
The browser durably stores the complete review before admitting any file, retains
uncertain requests across reload, checks receipts before sending, and separates
registration from completed follow-up work. It supports revision-checked cancel
and explicit retry. Terminal attempts remain in the database; equivalent retries
coalesce while preserving the original publication and hook identities. Committed
metadata is never reapplied by a retry, and uncommitted work still requires its
original preview through transaction commit.

Focused filesystem/HTTP checks, actual media retry/restart and portable restore
checks pass. The full manual/producer-file worker regression set passed. Eight
client recovery tests, application/browser types, lint and locales pass. The
collection/rule/picker browser suite passed 50 cases before adding retry controls;
20 final import/retry cases also passed in Chromium and WebKit at 390 and 1280
pixels. The mobile screenshot was inspected. The promoted integrated full gate
passed in 1,615 seconds: generation, embedded assets, 731 UI tests, 551 producer
tests, eight library tests, 113 archive tests, 262 backup tests, clean Go lint
and the complete Go integration suite. All 26 frozen file hashes matched the
validated worktree before updating this validation record.
Evidence is under `.local/native-manual-browser-20261006/` and the corresponding
`native_manual_browser_` receipts. No schema copy is needed. Production and live
workers remain unchanged; broader archive management and all remaining transition
release gates remain open.

## Read-only comparison before post consolidation — 2026-10-06

The application API can now compare two explicit post UUIDs in one read
transaction. It returns complete bounded identity/choice sets and compact source
summaries. Qualified ID and namespace disagreements, source-list ordering/count
conflicts, disabled selections, differing galleries, explicit media unlinks and
attachment choices remain visible. Media redirects are resolved independently
of the original decision UUIDs, including deleted media without stale local IDs.
Comparison cannot authorize or perform a merge. Original captures, receipt
history, pending work and metadata selections retain their existing scopes.

This distinction matters in the populated archive: legacy/native Instagram
records can share a post URL, while distinct native stories can share a highlights
URL. Shared URLs and equal content therefore cannot trigger automatic identity
consolidation. Duplicate-post mutation and the complete native review UI remain
required under the full transition plan.

Focused real SQLite and HTTP checks cover repeated reads/reopen, conflicting
stable IDs, partial and reordered source lists, separate attachment UUIDs,
explicit unlinks, conflicting galleries, merged/deleted media, invalid scopes
and rejected oversized responses. Query plans begin at post-scoped indexes.
All ten comparison tests passed, all Go integration callers compiled, and
promoted Go lint reported zero issues. The lint tool
could not inspect virtual added files in the initial Go overlay; its normal
promoted-source invocation completed successfully.

The read-only schema-87 probe inspected 69 pairs, returning 32 selected album
rows, 121 attachment rows, 148 explicit media choices and 187 identifiers across
those comparisons; a post can occur in more than one pair.
Independent table counts and shared-URL joins matched the responses. Median
comparison time was 0.554 ms and maximum time 4.813 ms for these samples;
the database size and modification time were unchanged. These are selected
comparison measurements, not a whole-library or concurrent-ingestion benchmark.
Evidence is under `.local/native-post-consolidation-20261006/` and the
`native_post_comparison_` receipts. No schema change or populated copy was needed.
Production and live workers remain unchanged.

## Canonical post-identity storage — 2026-10-06

Schema 88 adds indexed canonical post membership and immutable consolidation
receipts while retaining the original `source_posts` layout and every historical
reference. Later consolidation flattens the current identity lookup while
preserving the original direct redirects. Exact request replay survives further
merges, restart and retained deletion. Forgetting a group member tombstones all
members; replay does not recreate a forgotten post.

The internal writer requires a managed transaction, complete bounded identity
review and matching publication context. Its precommit guard rolls back a failed
publication even if a caller catches the error. A group may contain at most 256
original posts and 8,192 qualified identifiers. Different non-legacy upstream
identifiers remain incompatible; shared URLs cannot authorize consolidation.
Public repository methods inspect identity, paginated membership and direct
history only. No application mutation route is exposed. Existing album/media
choices, pending publication and the final merge UI still need integration.

Focused tests cover chained merges, exact retries after later merges and reopen,
original-capture replay, stale review, incompatible IDs, injected publication
failure, tombstone propagation, anonymisation, review bounds and indexed reads.
Migration tests retain original typed rows, reject unknown schema collisions
atomically and refuse inconsistent stored identity before writing. The portable
export/relocated-restore test preserves nonempty merge history, both original
album selections and captures, then validates a fresh native open and recovers
the original receipt.

The isolated schema-87 archive was copied with SQLite backup and migrated by the
actual Go database implementation. Schema-88 fresh reopen and independent
reconciliation passed: all 40,255,447 original rows in 296 tables and all original
table layouts/schema definitions remain unchanged. Exactly 256,990 self-root
identity rows were added, along with one migration-history record. Consolidation
and write-context tables are empty. Integrity and foreign-key checks pass; no
jobs, policies, links or merges were activated. The copy took 116.3 seconds;
migration took 448.0 seconds and fresh reopen 222.3 seconds while other integration
work was running. These concurrent rehearsal measurements are not a production
downtime estimate. Independent reconciliation took 502.0 seconds.

The integrated gate passed generation, the embedded application, 731 UI tests,
551 producer tests, eight library tests, 113 archive tests and 262 host backup
tests. Two Go lint findings were corrected; the resumed backend gate passed with
clean lint and the complete Go integration suite in 1,576.0 seconds. SQLite's
integration package completed in 1,545.7 seconds. All 14 promoted source hashes
were verified before the documentation was updated. Evidence is under
`.local/native-post-identity-20261006/` and the `native_post_identity_` receipts.
Production and live workers remain unchanged.


## Consolidation choice review and shared source-list provenance — 2026-10-06

The internal consolidation review now inspects every member of both canonical
post groups. It retains gallery/media/attachment conflicts on earlier aliases,
checks the total source-list budget before loading entries, and includes selected
library revisions in its signature. Different original attachment UUIDs with the
same qualified reference do not by themselves conflict. Oversized reviews fail
explicitly rather than omitting decisions. This remains an internal review, not
an application merge route or authorization to apply an incomplete plan.

Schema 89 preserves all columns, row IDs and existing references in attachment
selection decisions while allowing their capture/manifest foreign keys to point
to original evidence from the same canonical post identity. Native insert guards
and startup validation reject unrelated evidence. Original captures, manifests,
entries, selection history and saved review receipts retain their original
owners. Partial lists from different members can contribute to a new selection;
pinned/disabled choices remain protected. Attachment representatives use only
contributing manifests and are deterministic. New album metadata keeps the
selected original capture as its provenance.

Focused regressions cover chained identities, original capture replay, explicit
exclusions, review receipt recovery after another merge/restart, unrelated source
rejection in both repository and SQL writes, invalid stored provenance rejected
before startup writes, indexed lookup plans, preserved migration rows (including
forgotten posts), unknown-table collision rollback, and portable export/relocated
restore with a nonempty cross-member selection and gallery metadata. The related
HTTP checks and the repository's pinned lint check pass. Test command details
and receipts are under `.local/native-post-selection-provenance-20261006/` and the
`native_post_selection_provenance_` checks.

The broader related storage checks passed in 255.2 seconds, ingestion checks in
70.6 seconds and HTTP checks in 37.3 seconds. The initial combined invocation
also named a nonexistent API package and therefore exited unsuccessfully even
though storage/ingestion passed; the correct API package was run separately and
passed. The corrected migration helper was rechecked, and lint reports no issues.
These are focused integration checks, not a complete `validate-fork` run.

The complete integrated gate subsequently passed in 1,762.8 seconds: generation,
embedded assets, 731 UI tests, 551 producer tests, eight library tests, 113 archive
tests, 262 host backup tests, clean Go lint and the complete Go integration suite.
The SQLite integration package completed in 1,583.1 seconds. All 19 committed
file hashes matched the validated source before this documentation update.

The isolated schema-88 archive was copied with SQLite backup and migrated by the
actual Go implementation. Fresh reopen and independent reconciliation passed.
Comparison covered 40,512,438 original rows in 299 tables, preserving all original
application values and column layouts. The schema version advanced and one
migration-history record was added. Exactly two foreign-key definitions changed
and two provenance guards were added; their definitions were independently
checked. Integrity and foreign-key checks pass. No post consolidation, policy
activation or job admission occurred. The copy took 31.6 seconds, migration
267.4 seconds, fresh reopen 259.8 seconds and independent comparison 250.6 seconds.
These concurrent rehearsal timings are not a production downtime estimate.

The verified rehearsal pointer now selects schema 89, retaining the earlier
catalog, policy and pending-work reconciliation references. After checking for
open handles, the superseded schema-88 database and search files were removed,
recovering 20.4 GiB and leaving 117.4 GiB free. The original compatible snapshot,
current verified rehearsal and small evidence receipts are retained. Canonical current
media/gallery resolution, equivalent-attachment decisions, pending-publication
guards and the final post-merge API/UI remain unfinished. Production, live
workers and backup policies are unchanged.

## Media and gallery choices across post consolidation — 2026-10-06

Schema 90 retains the existing post-media decision, head and supersession layouts
and adds explicit proof for replacements across original post owners. Proof names
the identity-consolidation event and must establish the old owner's ancestry at
that event. Revision counters from different original posts are not compared.
The deferred foreign key rejects orphan proof at commit; startup validates stored
history, and anonymisation removes the new proof rows in dependency order.

The internal writer checks the complete bounded set of current choices across
the canonical group and merged media identities, then records one new decision.
Original capture ownership and metadata provenance remain unchanged. Exact
request replay works across later post merges, deleted media and restart. An
existing association to deleted media can be carried without recreating a scene
or permitting source metadata imports into it. Ordinary media edits still require
an active library record.

The internal gallery operation retires the reviewed original associations and
records one linked or disabled choice. Original decisions, both existing galleries
and their contents remain intact until the encompassing operation synchronizes
the final choices. Source synchronization recognizes earlier member posts while
preserving manual additions, exclusions, cover protection and edited metadata.
Outside-group claims and stale input fail atomically, including when the caller
catches an error after original heads have been removed.

Focused tests cover chained histories, exact recovery, deleted media, unrelated
captures, current lookup indexes, original-row migration preservation, schema
collision rollback, readonly startup rejection, anonymisation and gallery
protections. SQL tests reject missing/unrelated consolidation proof and roll back
an orphan proof at transaction commit. Portable export and relocated restore
preserve nonempty media replacement proof, transferred gallery ownership, original
captures and metadata links, then reopen through the real native validator and
recover the original request results. Existing gallery/album and selection
provenance regressions pass. Receipts and source hashes are under
`.local/native-post-media-consolidation-20261006/` and the corresponding
`native_post_media_consolidation_`/`native_post_gallery_consolidation_` checks.

These are internal building blocks, without an application merge mutation route.
Equivalent attachment choices, canonical current readers, the encompassing saved
review/result, pending-publication guards and API/UI integration remain unfinished.
The complete release gate passed, including generation, embedded assets, 731 UI
tests, the Python suites, clean Go lint and all Go integration tests. The actual
populated migration took 149.44 seconds, with a fresh reopen in 146.41 seconds.
Independent comparison checked all 40,512,439 original rows across 299 baseline
tables: original application cells and column layouts remain unchanged. The
schema marker advances once, one migration-history row is added, three scope
guards change and the empty consolidation-proof table and its indexes/guards
match their expected definitions. Integrity is `ok`, foreign-key violations are
zero, and no merge, policy or source job was activated.

The authoritative rehearsal now points to schema 90. After verifying no process
held the superseded schema-89 files, those files were removed, recovering
21.90 GB. The original compatible snapshot, current verified copy, comparison
reports and source hashes remain. Production, live workers and backup policies
are unchanged.

## Shared attachment choices and canonical media reads — 2026-10-06

Attachment choices now resolve through canonical post identity plus the exact
qualified attachment reference. One current decision can serve original source
slots from several consolidated posts; original attachments, captures, manifests
and decision history remain unchanged. Internal consolidation checks all original
heads and selected media revisions, then publishes one choice and retires the
other heads in a managed transaction. Caught late failures roll back the whole
operation. Existing deleted-media choices can be retained without resurrection.

Current attachment reads, batched album choices and source-kind hints follow the
shared owner. Ingest honors shared rejections and checks candidates across all
equivalent original attachments. Current post-media reads follow the canonical
post and retain unresolved conflicts; new ordinary writes cannot bypass another
original owner's decision. Exact original requests and history remain recoverable.
Metadata source sampling exposes original captures through the shared choices,
preserves original provenance/cursors and suppresses competing attachment heads
or a canonical post unlink.

The attachment context explicitly returns its requested UUID separately from the
current owner. The editor uses the current owner for new requests while recovering
saved original requests first, followed by any pending request at that owner.
It preserves exact saved bodies, separate history cursors and explicit retry after
definitive rejection. Read-only panel opening never sends pending operations.

Focused repository, query-plan, receipt/restart, portable-restore and real HTTP
checks pass. Client lint/type checks and 20 protocol/outbox tests pass. All 38
Chromium/WebKit association browser cases pass, including desktop/mobile alias
edits, lost-response recovery after a merge with unavailable context, stale
original requests and pending requests under both identities. Source hashes and
receipts are under `.local/native-attachment-choice-consolidation-20261006/`.
The integrated full fork gate remains pending. No schema migration is added.

Canonical source-list/gallery and post-browse integration, final selection-head
retirement, pending-publication guards and the encompassing post-merge API/UI
remain required. These changes do not expose the internal merge primitive or
change production, worker activation or backup policies.

## Source-list choice consolidation — 2026-10-06

The internal merge can now combine compatible reviewed source lists, pin one
original capture or explicitly disable selection. It requires the latest merge,
current post revision and every existing choice. It rejects conflicting,
duplicate, unrelated and unreviewed additional lists before retiring pointers.
Original captures, manifests, decisions and review receipts remain unchanged;
caught late failures roll back both the identity merge and its selections.

Focused selection/review, chained-merge, restart, scoped-query and rollback tests
pass, including recovery of an original saved review after later merges. The
previous integrated gate found one test still writing through an old post UUID;
its corrected reviewed-consolidation path passes. Evidence is under
`.local/native-post-selection-consolidation-20261006/`. No schema migration is
added. Canonical readers/editors and the encompassing merge API/UI remain separate
integration work; the next full gate is still required before pushing.

## Canonical source albums and saved edits — 2026-10-06

Source-list selections, gallery associations and ordered album pages now follow
the canonical post identity. Ordinary new edits require its current UUID and
revision; retained choices on earlier aliases cause an explicit conflict until
the encompassing merge resolves them. Gallery-to-post lists deduplicate merged
post aliases. Manifest discovery pages the indexed source lists of each original
member before sorting a bounded union, retaining original capture witnesses
without loading their payloads.

The application identity context returns requested and canonical records.
Album responses similarly distinguish the requested UUID from the current post.
Source-list and gallery editors recover saved original requests before pending
canonical requests and use the current identity for new edits. Original history,
complete merge comparison and immutable receipts retain their original scopes.
Ingestion selects through the current post while preserving capture provenance.

Focused SQLite checks cover canonical selections/albums, merged manifest pages,
gallery deduplication, rejection of ordinary alias writes, original receipts and
portable restore. Real HTTP and capture-intake/recovery checks also pass. Client
validation includes app/browser TypeScript, Biome, ESLint and 65 tests. All 124
Chromium/WebKit browser cases pass across source-list/association editing, source
albums, post browsing and album review. An earlier browser run had one WebKit
context closure; the final expanded run passes that case without a workaround.

On the read-only schema-90 rehearsal, 69 selected-post and manifest-pagination
queries return the same results as the prior readers; the slowest new query took
8.3 ms. That populated copy has no post consolidations, so this comparison proves
unchanged-data behavior rather than full-library merged-post performance. Merged
groups are covered by the repository and browser fixtures. Source hashes,
query results and check receipts are retained under
`.local/native-canonical-post-albums-20261006/` and
`.local/native-discovery-client-20261004/`.

No schema migration or production change is added. The integrated full gate
passed generation, embedded assets, 737 UI tests, Python suites and Go lint.
All Go packages except SQLite passed; two gallery fixtures still used the prior
original-owner current-read/write expectations. The following integration
increment updates those fixtures and retains explicit checks of original-head
retirement and preserved gallery history. The encompassing merge transaction/API/UI,
pending-work publication and all broader transition gates remain required.

## Canonical post browsing and shared current evidence — 2026-10-06

Post browsing, exact identity/URL lookup, media review and selected publishers now
follow consolidated identities. Canonical resolution and deduplication precede
pagination, so early-sorting original aliases are not lost behind a canonical
cursor. Sharing a URL does not combine unrelated posts. Responses distinguish
the requested UUID from the current identity; original URL witnesses, captures,
saved requests and decision histories retain their owners.

Expanded URL pages select one stable witness per exact value before pagination.
Capture and identifier pages read bounded indexed ranges from original members;
capture metadata remains shared and payload bodies are not loaded. Metadata
policies use the same complete, deduplicated URL set while retaining the original
selected capture as field provenance. Unresolved media/attachment choices remain
visible, and a whole-post rejection remains separate from an attachment link.

A real chained-merge fixture covers canonical cursors, shared URL witnesses,
capture observation/recording clocks, current publisher selection, conflicting
choices, original receipt recovery after restart and unchanged original evidence.
The metadata preview/apply test verifies deduplicated merged URLs, a read-only
preview and original capture provenance on the resulting field decision.
Focused query, repository, HTTP and metadata checks pass. Client validation
includes 64 protocol/outbox tests, TypeScript, Biome and ESLint.

The six-file browser run passed 145 cases, exposed one test that reloaded before
its simulated request committed and one WebKit context closure, and stopped
before five cases ran. The corrected recovery test waits for the simulated
response failure. All 68 source-review/association cases then passed Chromium
and WebKit, including both earlier failures and the previously unrun cases.
The other 84 browser cases had passed in the initial run; no production UI
workaround was added.

Read-only comparisons on the populated schema-90 rehearsal agree for 250 queries
across 25 sampled posts; the slowest new query took 4.4 ms. That copy has no post
consolidations, so merged behavior is covered by the separate real repository
fixture. Source hashes and receipts are under
`.local/native-canonical-post-browser-20261006/` and the corresponding
`native_canonical_post_browser_` checks.

Gallery regression and portable-restore checks now pass, including original-head
retirement, canonical reads, retained manual members, cover protection and metadata.
No schema or production state changes. The combined full fork gate now passes:
generation, real embedded UI, 739 UI tests, 551 producer tests, eight library
tests, 113 archive tests, 262 backup tests, Go lint and full Go integration.
The complete reviewed merge/API/UI,
pending-publication integration and broader transition/cutover work remain open.

## Historical matching across post identities — 2026-10-06

Schema 1000091 changes the post/media file-proof scope guard to accept evidence
from any original member of a consolidated post identity. It retains the original
evidence/appearance/file-match chain and every existing row. Historical schemas
still use their original validation during migration. New requests require the
current post UUID; exact prior receipt recovery happens before current-state
checks and remains available after another merge or restart.

Both matching services use bounded indexed evidence from all original members.
Equivalent attachments match by their qualified source-media reference, while
filename matching checks original observations against the group's qualified
post IDs. New derived evidence keeps the attachment's original owner. Unrelated
evidence is excluded, explicit choices remain protected, and discovery applies
its cursor after resolving current identities. Read-only checks on the populated
schema-90 rehearsal agree for 168 queries across 50 posts; the slowest new query
took 15.4 ms. That copy has no post merges; separate SQLite fixtures cover them.

An unpublished album job stops with `album_preview_changed` if consolidation
invalidates its plan or leaves source choices unsettled. An already-published
job resumes only notification delivery with its original event/post/result.
The UI resolves the current identity for new work, recovers original saved work
before current saved work, and keeps pending requests intact while inspecting
older jobs. Clearing a rejected cancellation retains the job for fresh review.
Command-line preparation similarly resolves and deduplicates explicit old post
IDs, without retargeting saved plans or consulting identity during recovery.

Focused Go/HTTP, migration, chained-merge, notification recovery, corruption guard
and portable restore checks pass. Restore testing exposed an optimizer join
reordering after statistics were rebuilt; fixing the bounded candidate join
keeps the evidence lookup indexed. UI type/lint checks and 38 client tests pass.
All 46 album Chromium/WebKit cases pass, including stale cancellation and pending
request recovery. All 30 producer/backfill tests pass, including identity scope,
alias deduplication and immutable original-plan recovery.

The populated schema-91 migration and fresh reopen pass on a separate library
copy in 449.7 seconds. Independent comparison preserves every original cell and
type across 300 tables and 40,512,440 rows; integrity is clean and there are no
foreign-key violations. Only the file-proof scope guard, schema version and one
migration-history row change. No post merge or worker is activated on that copy.
The complete release gate passes in 1,936.7 seconds: generation, real embedded
UI, 740 UI tests, 554 producer tests, eight library tests, 113 archive tests,
262 backup tests, Go lint and full Go integration, including API and SQLite.
The current rehearsal pointer now selects schema 91 and retains all prior
catalog, policy and queued-work verification references. The superseded schema-90
copy was retired after checking open handles, recovering 21.90 GB while keeping
the original compatible snapshot. Source hashes, receipts and rehearsal scripts
are retained under
`.local/native-canonical-post-backfill-20261006/`. This increment does not expose
the encompassing post-merge mutation or activate any production writer/worker.
The complete reviewed merge API/UI and broader transition gates remain required.

## Atomic post merge review and notification recovery — 2026-10-06

The reviewed post merge now resolves source-list, gallery, post-media and
attachment choices in one managed transaction. Its digest covers original
identity members, current decisions and library state; stale reviews and caught
late errors cannot leave a partial merge. Schema 1000092 stores immutable exact
requests, original results and decision ownership. Retained captures and prior
producer/backfill receipts are preserved across later merges and restart.
Gallery changes admit durable notification work, with separate saved retry
requests and indexed history so recovery works after reload or on another device.

The native Source posts screen now exposes target lookup, original choices,
explicit conflict resolution and a preview of source order and gallery changes.
It writes a durable browser request before transport and recovers its original
receipt before attempting delivery again. Original post redirects cannot hide a
pending request. Notification retry/cancellation leaves the committed merge in
place. Existing Base UI controls and expandable sections work on desktop/mobile;
large choice lists render in bounded portions.

Focused backend migration, atomicity, publication, HTTP, restart, integrity,
portable restore and hook tests passed; the new notification-history checks
passed in 56.0 seconds and pinned Go lint reported zero issues. The typed client
accepts a synthetic response captured from the real Go routes; all 12 recovery
and validation tests passed. All 36 Chromium/WebKit merge and existing post
browser cases passed, including conflicting links, stale previews, lost replies,
redirect recovery and a failed post refresh after a successful merge. UI types,
component lint and localization checks passed. The full-copy schema-92 migration and fresh reopen passed in 461.2 seconds.
Independent reconciliation checked 40,512,441 original rows across all 300
original tables, preserving every typed cell value; integrity was clean with
zero foreign-key violations. The four new receipt tables are empty and no job or
policy was activated. These concurrent rehearsal measurements are not a
production downtime estimate. The combined release gate passed in 1,928.3 seconds: generation,
real embedded assets, 752 UI tests, 554 producer tests, eight library tests,
113 archive tests, 262 backup tests, zero Go lint issues and full Go integration,
including the API and SQLite suites.

Production, installed producers, active scheduling and S3 policies remain
unchanged. Shared import/job history, remaining caller/compatibility conversion,
full production backup/restore and request-cost evidence, cutover observation,
retirement and owner acceptance remain required.


## Shared archive activity — 2026-10-06

Schema 1000093 adds four history indexes for bounded native job and source-run
pages. The application API exposes summaries, original collection revisions,
current post/media identities, safe relative file paths and lazy attempt history.
The readers neither execute work nor disclose worker arguments or settings.
Source-run completion and individual attempt outcomes remain separate.

The Archive activity route uses existing Base UI controls, localized filters,
shared desktop/mobile navigation, collection lookup and links to native review
screens. It preserves successful results during failed refreshes and discards
stale responses when scope changes. The selected active item polls; other rows
and collapsed histories do not. This is activity inspection, not the remaining
shared import-review queue or a new generic mutation API.

Focused repository/API/migration/context tests passed, including absent media,
renamed collections, original target bindings, shared translation work, published
galleries and merged post redirects. All 14 Chromium/WebKit cases pass across
mobile and desktop, including lazy paging, invalid detail responses and recovery.
TypeScript, component lint, localization and six typed-client tests passed;
pinned Go lint reported zero issues. The populated migration and fresh reopen
passed. Independent comparison preserved all 40,512,442 original rows across 300
tables, with four new indexes, one migration-history row, clean integrity and no
foreign-key violations. Existing jobs and source evidence were unchanged.

The first combined gate found that older migration fixtures retained the new
source-run indexes, and SQLite selected an unbounded primary-key walk for one
existing work-history cursor. The fixture removal chain now removes schema 93
first, and selected-work history explicitly uses its dedicated work index.
The affected migration/history checks pass (202.4 seconds). The corrected
combined release gate passed in 1,931.8 seconds: generation, real embedded UI,
758 UI tests, 554 producer tests, eight library tests, 113 archive tests,
262 backup tests, zero Go lint issues and full Go integration. Production has
not changed.

## Host worker service packaging — 2026-10-06

The native producer now includes systemd user service/timer templates and a
placeholder environment file. One service instance runs the reviewed profile
list through `dispatch-all`; its timer schedules the next cycle after the
previous service invocation becomes inactive. Existing native leases coordinate
other producers and callers. Exit 2 remains a pending/review cycle result, not
proof of scrape or media completion.

The unit parser accepts both templates without warnings. The README documents
the scoped Stash token, per-runtime outbox/profile configuration, backup inventory,
and the separate n8n container runtime requirement. These files are packaged
only; no live units, launchers, producer identities or outboxes were activated.

## Native import history — 2026-10-06

The application API and desktop/mobile Import history route now expose compact
catalog and automation snapshot summaries. UUID keyset paging reads only the
selected page; details use eleven catalog or four automation progress lookups.
The current collection label can change without rewriting the retained snapshot.
Manifests, original payloads and settings are excluded.

Transfer receipt, per-step import progress and current associations are shown
separately. A missing progress row means the step has not run. Historical warning
counts remain historical after subsequent association changes. Retained cleanup
and operational state is not displayed as active work. This reader does not
upload input, run importers, change associations or activate policies/jobs.

The typed client rejects mismatched snapshots, invalid counters, unexpected
families and non-advancing pages. Native expandable sections load technical
details and step counters on demand. Failed refreshes preserve successful
same-scope results; switching import kind drops the old scope, and returning
from details restores list position.

Focused repository and HTTP tests, five client contracts and all eight
Chromium/WebKit checks passed. UI types, component lint and localization passed;
pinned Go lint reported zero issues. Read-only checks on the populated schema-93
copy paged all 1,697 catalogs in 14.8 ms and read their fixed progress sets in
56.3 ms. Query plans use primary-key searches without temporary sorts. These
local measurements are not a deployed latency guarantee. No schema migration
is added. The combined release gate passed in 2,111.7 seconds, including
generation, real embedded assets, 763 UI tests, all producer/archive/backup
suites, zero Go lint issues and full Go integration. Production is unchanged.

The schema-93 activity checkpoint was published and selected as the current
verified rehearsal. After checking open handles, its superseded schema-92
development database was retired, reclaiming 21.90 GB and leaving about 96.6 GiB
free. The original compatible snapshot and verified schema-93 copy remain.

## Media test runtime in CI — 2026-10-07

The published activity revision passed CI lint and browser checks. Its Linux
binary build also passed, but both backend validation and image-publication
validation failed because the runner lacked `ffmpeg`. The real-MP4 manual intake
test correctly requires it; the failures were not a migration or import mismatch.
Both Ubuntu test environments now explicitly install the FFmpeg package and
verify both `ffmpeg` and `ffprobe` before running the existing tests. No test is
skipped or weakened. Cached actionlint 1.7.12 validation passes for both changed
workflows. The next revision's CI and native image publication must still pass
before its artifact can be selected for deployment.

## Shared saved-action recovery — 2026-10-07

The Saved actions route discovers browser requests across fourteen existing
native review/intake protocols, using the current public endpoint as its scope.
Bounded key cursors avoid loading every request body. Each owning protocol
validates its selected records before compact summaries are shown. Opening the
list sends no HTTP request. Existing requests are retained during a failed read,
and invalid records remain visible without exposing their bodies or erasing them.

Explicit Resume uses each original receipt/recovery flow. Lost replies, rejected
choices, admitted work and retained file batches stay distinct. Rejected choices
open their original review, including pending collection/root creation forms;
media links use their original scene/image scope. Attachment and notification
destinations resolve through checked read-only context/receipt lookups when
opened. Job progress remains on the server and does not imply that all browser
actions or all archive conflicts have been resolved.

All fifteen paging/discovery/destination tests pass, along with the existing
outbox regression suites. TypeScript, browser fixture types, component lint and
localization checks pass. All fourteen Chromium/WebKit cases pass for desktop
and mobile navigation, lazy technical references, lost replies across reload,
rejection, storage failure, paging and damaged records. The mobile layout was
inspected at 390 pixels without horizontal overflow. Main route generation and
the complete release gate passed in 1,953.8 seconds, including all UI, producer,
library, archive and backup checks, zero Go lint issues, and full Go integration.
No database migration or production configuration change is introduced.

## AVIF test runtime in CI — 2026-10-07

The next published revision passed lint and browser CI, and FFmpeg installation
succeeded in both backend environments. The image-rendering regression then
identified the missing `avifdec` executable. Both workflows now install
`libavif-bin` alongside FFmpeg and verify the encoder and decoder versions before
validation. This is the same Ubuntu package used by the CUDA runtime image.
Actionlint passes. The complete local release gate also passed its image tests;
the revised CI jobs and native image publication still require verification.
No assertion or supported media path was removed to bypass the failure.

## Cross-runtime thumbnail colour fixture — 2026-10-07

With the media tools installed, Ubuntu CI reached a colour comparison failure
for the 32×24 HLG thumbnail. The same failure reproduced in an isolated Ubuntu
24.04 container: independent JPEG/AVIF chroma reconstruction at the saturated
test pattern's sharp edges exceeded the mean-channel error limit. The 64×48
rendition passed, and both AVIF encoders showed the thumbnail difference.

The colour regression now uses a smooth full-range RGB fixture. It keeps the
existing error limit, thumbnail dimensions, SDR/PQ/HLG cases, colour metadata,
bit-depth checks and available gain-map paths. A deliberate test-only substitution
of HDR bytes for the SDR fallback still fails all four PQ/HLG encoder cases,
with mean errors of 44.94–62.19 against the unchanged limit of 12. Production
rendering code is unchanged. The image package passes locally and in the isolated
Ubuntu runtime with its required test fixtures; pinned Go lint reports zero
issues. CI must rerun before selecting a native deployment artifact.

The published revision `21a615ddd88f6dcc0bb78792525578eeacf33fab` subsequently
passed Build, Go lint, browser tests and GHCR publication. Read-only registry
inspection confirms its full revision label and source image digest
`sha256:bbd6250fafbef07436f3c439b7a9589446f808957c9c21efef70757dd0d139a4`.
This verifies the source image; no production wrapper or deployment was changed.

## Shared review queue and Keep current value — 2026-10-07

The implementation adds a current review queue for account ownership,
post/media associations and retained catalog fields. Bounded UUID pages link to
existing targeted review screens through shared desktop/mobile navigation.
An empty inspected batch with a continuation cursor offers Continue checking;
it does not claim that review is complete. Explicit unlinks, current merge
conflicts and field-specific review outcomes retain their existing semantics.

Schema 1000094 records Keep current value receipts without changing selected
metadata, protection, provenance or entity revisions. Unsupported fields and
unresolved names can be declined without a replacement. Keep and Apply have
distinct durable request identities and success messages. Original receipts
survive later edits, identity adoption, merges, deletion and portable restore.
Keep emits no metadata-update notification. Historical source edits remain
available after their review outcome is recorded.

Focused backend, HTTP, restart, staleness, anonymisation and portable restore
checks pass. The typed client accepts populated responses captured from the
real Go route. UI types, formatting, localization and 33 client/recovery tests
pass; Go lint reports zero issues. All 36 Chromium/WebKit cases pass, including
mobile navigation, empty continuation pages, lost Keep replies and stale
responses. The initial browser failures were fixture errors: assertions did
not open the collapsed current-field section, and a synthetic second page
reused its continuation UUID. Both were corrected without changing the product
or relaxing response validation. Mobile and desktop layouts were inspected.

A consistent 21.90 GB schema-93 database copy migrated to schema 94 and reopened
successfully in 586.4 seconds. Independent reconciliation preserved every typed
cell in 40,512,443 original rows across 300 tables, with clean integrity and zero
foreign-key violations. Only the empty Keep receipt table, its indexes/triggers
and one migration-history row were added. Existing jobs and source evidence are
unchanged. These local measurements are not a production downtime estimate.
Read-only candidate queries on the populated copy completed in 1.81 seconds for
media, 0.27 ms for retained metadata and 0.05 ms for an account page; these probes
do not measure the full HTTP route or establish deployed latency.

The combined release gate passed in 2,090.7 seconds, including generation,
embedded assets, 789 UI tests, 554 producer tests, 8 library tests, 113 archive
tests, 262 backup tests, zero Go lint issues and full Go integration. It has not
been deployed. Live host/n8n conversion, complete backup/restore and request-cost
proof, cutover observation, retirement and owner acceptance remain required.
A fresh read-only handoff audit confirms the 49 inventoried host files
and all 14 inventoried n8n workflow versions/statuses are unchanged; all nine
active workflows have matching published versions. The three staged conversion
input graphs still match live. This verifies definitions, not worker health or
scrape completion.

Inspection of the active workflow graphs also identified retired performer
filters, an invalid performer-URL selection and two automatic database-migration
nodes. A private draft corrects the three query shapes and stops on schema
mismatch; all fourteen remaining GraphQL operations validate against the native
schema. This is static contract evidence only. Native source registration,
association-safe add/remove actions, heartbeat conversion and actual n8n runtime
verification remain prerequisites to publishing the complete workflow changes.

## Native source management for automation — 2026-10-07

The producer package adds a source-management command that uses the existing
application definition API and scoped current-target lookup. It saves complete
plans before mutations, recovers original revisions after lost responses,
preserves later owner edits and existing account/path choices, and declines
ambiguous or inactive registrations. Disabling a source retains its evidence
and media. A root grant is required to establish complete target lookup; the
application API key remains necessary for mutations. Shared worker locks cover
host/container coordination and common-boundary backup capture.

Thirteen focused producer tests pass. A real Go/SQLite HTTP test passed across
separate Python processes: one creation response was deliberately lost, replay
created no duplicate, two sources were disabled, and a subsequent owner edit
survived old-plan replay without another write. No schema migration is needed.
The n8n parent-execution heartbeat is also packaged as a read-only helper; its
staged converter preserves the existing graph and coordination semantics.

The combined gate passed in 2,001.7 seconds: generation, embedded UI, 789 UI
tests, 571 producer tests, 8 library tests, 113 archive tests, 262 backup tests,
zero Go lint issues and full Go integration. The isolated installed package
matches source and includes both commands. An isolated image built from the
current n8n base passed 62 expression evaluations and three real Wait checkpoints
for the scraper children, plus seven heartbeat expressions. Workflow/credential
identities remain intact; no scraper commands executed. The read-only heartbeat
also matched 25 actual execution rows at inspection time; this does not establish
scraper health or completion.

These commands are not installed in live workers or activated in n8n. Actual
parent/removal conversion, source policy and performer review integration, full
coordinated backup/restore, measured publication costs and production cutover
remain required. Registration alone does not create a metadata policy; new-source
automation must install its reviewed defaults before dispatching media intake.

## Native parent and source-removal workflows — 2026-10-07

The native n8n source helper saves registration/removal plans, metadata defaults
and exact before/after list bytes before mutating the server. It initializes
metadata policies only for new collections, preserves existing owner policies,
and returns the original receipt after later edits. Incomplete retries decline
changed sources, policies, account links or lists. A stable execution/node/item
identity cannot be reused with a different requested action or account.

Performer removal follows current native account links and canonical account
identifiers, with pagination and ambiguity checks. It never selects a performer
by name or alias. The converted parents use native intake and metadata policies
in place of their former scan/autotag/implicit performer-creation steps. They
retain their inputs, child workflow IDs, waiting and error branches. Source
registration does not certify scrape completion; the children still check their
original durable backfill requests. List tracking begins after registration, so
scheduled runs and the initial backfill share native source-window coordination.

Fourteen focused producer tests pass, including lost policy replies, interrupted
list/receipt publication, changed inputs, later owner edits, ambiguity and exact
list preservation. The real Go/SQLite HTTP check passed in 3.4 seconds: six
collections and policies were created, a lost first-policy reply recovered with
only the five remaining saves, and linked-account removal disabled exactly those
six sources. A different performer's matching display name did not affect the
selection. Nine account identifiers exercised full identifier pagination.

All nine active workflow conversions are staged privately. In an isolated image
built from the current n8n base, the five new parent/removal graphs passed 37
expression evaluations, four per-item normalization checks, nine result checks
and three executions of the actual command node using help/error fixtures.
The three scraper children in the composed set passed 62 expression evaluations
and three real Wait checkpoints. The first new command-node harness lacked the
installed runtime's cancellation signal; adding that required fixture method
resolved it without changing product code. No source mutation or scrape command
ran in these container checks.

The combined release gate passed in 1,933.9 seconds: generation, embedded UI,
789 UI tests, 585 producer tests, 8 library tests, 113 archive tests, 262 backup
tests, zero Go lint issues and full Go integration. All eleven gated source files
matched their recorded hashes before this result was added.

The deployment handoff now has thirteen download profiles and seventeen metadata
profiles verified against the installed host and isolated n8n runtimes. They
share the same adapter digest and use the canonical imported library root,
replacing the older staged prototype-root profiles. Coomer and Kemono still
request originals. The download-profile dependency inspection resolves thirteen
profiles, fifteen download archives and their required configuration files; it
uses an explicit fixture outbox and is not a coordinated capture.

Runtime defaults, subscription lists, source-operation state, credential references
and the complete worker inventory still require coordinated backup coverage and
actual deployment. Original executions must drain or be frozen before old graph/
helper retirement. No live workflow, worker or production database has switched.

The superseded schema-93 rehearsal and its exact auxiliary files were removed
after no-open-handle checks and verification of the selected schema-94 copy.
The original compatible snapshot and schema-94 rehearsal remain. This reclaimed
20.4 GiB; large subsequent restores use the separate owned tank scratch directory.

## Source-operation state in coordinated backups — 2026-10-07

Worker inventory declarations can now include a source-management runtime.
Its subscription lists, metadata defaults, lock root and complete operation-state
tree enter the same component stage as the producer outboxes and download
archives. This retains unfinished account/source requests as well as completed
receipts. The runtime must identify the worker's media root; container path
mappings apply to all of its dependencies. Directory membership, per-file hashes
and bounded traversal reject incomplete or changed input before server capture.

Version-2 retained inventories include these trees and still read version-1
captures. Sealed retries verify the original staged bytes without consulting
later live operations. Tests exercise actual SQLite component capture, changed
and moved live operation state, container mappings, missing lists, extra files,
symlinks, special files, size limits, barrier coverage and malformed retained
inventories. All 270 backup tests passed against the isolated draft; all nineteen
inventory tests pass after promotion and the aggregate traversal bound.

The installed backup modules match source. Their dependency resolver inspected
all thirty staged download/metadata profiles, fifteen archives and the explicit
source-operation runtime. The existing fixture outbox and empty private state
directory are inspection inputs, not live producer captures. The live Reddit
subscription list changed between two inspections; its different hash was
detected and retained in the handoff evidence. Final capture must fence the old
writers, which do not participate in native publication locks.

The combined release gate passed in 1,959.3 seconds: generation, embedded UI,
789 UI tests, 585 producer tests, 8 library tests, 113 archive tests, 270 backup
tests, zero Go lint issues and full Go integration. All seven source files
matched their recorded gate hashes before this result was added. The initial
attempt stopped because the workspace sandbox made the existing Go build cache
read-only; the successful rerun used the same source with cache access restored.

Real producer provisioning and the complete coordinated capture/restore remain
pending. No backup schedule, cloud policy, workflow or production writer changed.

## SQLite workflow state in coordinated backups — 2026-10-07

The portable archive accepts an `operating_database` component for external
SQLite state such as n8n's execution database. It uses the existing SQLite
snapshot and integrity checks, captures committed WAL contents after producer
outboxes, and retains database metadata for verification during restore.
Component-stage retries reuse the original snapshot, including after a lost
server reply and removal of the live database. Execution-ID sequences survive
pruning and restore, so new executions do not reuse retained caller identities.

All 117 archive tests and 270 host backup tests passed after installing the
updated isolated runtime. New cases cover WAL-backed pending executions and
their payloads, execution-ID sequences, lost replies, changed/missing live state,
invalid SQLite and foreign-key data, and missing or altered database metadata.
The first focused run found an incorrect expected error message in a new test;
the implementation correctly rejected the mismatched metadata.

A read-only snapshot of the actual n8n database passed integrity and foreign-key
checks: 1,773,125,632 bytes, 148 tables, 43 workflows and 107 retained executions.
Its retained execution counter was 1,819,469. Capture and verification took
9.0 seconds in the owned tank rehearsal directory. No workflow was activated.
This checks the database capture path; it is not a coordinated production backup.
Matching encryption/configuration files, external execution payloads, writer
coordination and the populated full restore drill remain required.

## External workflow payload inventory — 2026-10-07

Worker declarations now accept `state_directories` for changing external payload
trees such as n8n's `binaryData`. Container mappings apply before traversal.
Each new backup enumerates regular files and empty directories, streams file
checksums, and rejects membership or identity changes before the native
checkpoint. The existing retained inventory preserves the original tree and
bytes on sealed retry. Binary payloads have bounded traversal without inheriting
the source-operation JSON size limit; source plans retain their runtime limits.

All 274 host backup tests passed after installing the changed runtime. New
checks cover container paths, payloads larger than source-plan limits, newly
created files, unsafe paths, changed payloads before sealing, and the combined
WAL database/payload retry after live state changes or disappears. The installed
inventory modules match the reviewed source hashes.

The real thirty-profile inspection now resolves 101 components, including 42
n8n payload/metadata files. Its outbox is still an explicit inspection fixture,
not a production producer capture. The first inspection rejected the new
container directory until its explicit host mount mapping was added.

Frozen rehearsal inputs preserve the previously captured n8n database, its
configuration, hook, original/replacement workflow exports, and binary payloads.
All nineteen database-referenced payloads have matching metadata and sizes. An
offline container using the prepared n8n runtime decrypted all nine saved
credentials with the retained configuration; no plaintext values were emitted
and no workflow engine started.

The owned tank scratch now also has a held media snapshot and an isolated ZFS
clone. It shares the existing media data and does not modify live files or
services. Preparation of the complete schema-94 database and original artwork
is running separately. These are rehearsal inputs from distinct source views;
the full coordinated capture, relocated restore, producer replay and production
cutover gates remain open.

## Populated restore preparation and verification cache — 2026-10-07

The isolated schema-94 copy passed SQLite integrity, foreign-key and native
identity checks. Its 21,901,381,632-byte database references exactly the same
238,574 external artwork identities as the previously verified portable archive.
Restoring those original files into the owned tank scratch is now running, with
encoded/uncompressed hashes and original MD5 identities checked as files land.
No production writer or original migration copy changed.

The initial preparation used SQLite's 2,000 KiB default cache and took 3,455.3
seconds through database copy, verification and artwork inventory comparison.
A concurrent read-only check of the same unchanged copied database with a
256 MiB page cache passed all integrity, foreign-key and native identity checks
in 1,017.0 seconds, with 296,448 KiB peak process RSS. These observations shared
disk I/O and are not an isolated comparative benchmark. The archive verifier now
uses that bounded connection-local cache; it retains every verification check.
The installed runtime passed all 117 archive and 274 host backup tests.

The native host worker runtime is installed in its separate, inactive location.
All 113 installed producer modules match source, and its pinned gallery-dl,
yt-dlp and adapter identities match the prepared n8n image. All twenty host
profiles and six referenced cookie files passed read-only readiness checks.
No scrape/API command, credential provisioning, unit installation or schedule
change occurred. Coordinated capture, complete relocated restore and production
cutover are still pending.

## Populated artwork capture and restore preflight — 2026-10-07

The isolated input preparation finished: schema 1000094, a 21,901,381,632-byte
library and all 238,574 original artwork files (36,496,053,336 bytes). Encoded and
decoded hashes and original artwork identities passed. Total preparation time
was 6,362.7 seconds, including the earlier database verification. The inputs
remain separate source views; this is not a coordinated production snapshot.

A real timing probe exposed a release blocker in artwork capture. The existing
provider exceeded its 120-second checkpoint deadline after making 2,295 of 2,400
sample links. Flushing every new hard link separately was too slow on the owned
ZFS rehearsal storage. Linux 5.8+ now uses a checked filesystem-wide `syncfs`
after retaining the links and before publishing the pin manifest; other
platforms retain per-file `fsync`. Filesystem writeback errors and deadline
expiry still prevent sealing. The exact same sample completed in 0.59 seconds;
the complete 238,574-file capture then passed in about eleven seconds. These
are observed runs on the rehearsal filesystem, not a production latency promise.

All 121 portable archive tests and 274 host backup tests pass. New cases verify
the flush occurs before publication, failure and deadline handling preserve an
unsealed attempt, descriptor cleanup, and per-file fallback on kernels without
filesystem writeback error reporting. The installed modules match the tested
source. Documentation also corrects the earlier description of failed captures:
partial pins are retained for fenced abandonment, not immediately discarded.

The isolated HTTP preflight passed with a fresh native database, two scoped
producers, real held ZFS media capture and portable restore. Each producer had
an acknowledged event, an accepted event with a lost acknowledgement, and an
undelivered event. Restored delivery completed both outstanding events without
duplicates. A staged filesystem operation also recovered from the portable
journal into a new directory. This small preflight validates the rehearsal
driver; the populated capture/restore is the next gate.

The published Stash image for `9739f848a` is verified by its full revision and
digest. Its wrapper build is running with that explicit source digest. The
configured origin and live deployment confirmed the publication destination
after an initial automatic approval rejection; the evidence-backed retry was
approved. No production service or cloud backup policy changed.

## Host S3 publication and inventory proof — 2026-10-07

The native wrapper finished publishing, with its explicit Stash source digest
verified in all three variants. The separate host backup environment is now
installed and verified against revision `5a98dda4d`: twenty archive modules,
113 producer modules, sixteen host modules and seven command entry points.
It remains inactive; no production command or schedule has switched.

A complete read-only cloud inventory found 14,964 Standard metadata objects and
299,270 Deep Archive media objects. Its 315 LIST calls produced exactly 315 HTTP
attempts, with no retries. Repeating that inventory daily projects $0.04725 in
LIST charges over thirty days at the observed Oregon price. This is inventory
overhead, not a total backup bill or a measurement of native daily change volume.
The [operational proof](native-backup-operational-rehearsal.json) records the
current official pricing source and the measurement limits.

The actual `NativeBackupSession` now passed publication and restore against an
isolated prefix in the existing metadata bucket. It used a small native library,
two real fixture outboxes, the frozen 335 MB host ledger, three accompanying
ledger/manifest files and the existing compatible master. One already archived
video was adopted through its full checksum and local bytes. The checkpoint,
artwork provider and held ZFS view used their real implementations. Publication,
history receipt, retention protection and resumed local release completed.
The resulting 23 encoded objects totalled 108,306,186 bytes in Standard.
The production current manifest, media objects and cloud policies were unchanged.

Two private-driver mistakes were resolved without restarting the committed
backup. An initial preflight supplied `sudo` without its required absolute path
and stopped before capture or cloud writes. The later run published, released
and restored correctly, but its assertion expected three small-metadata HEADs.
Lifecycle reconciliation actually needs nine HEADs and three tag reads there.
A separate read-only recovery downloaded the same published archive afresh,
verified the native schema, producer receipts and all four host ledgers, and
confirmed every unchanged encoded chunk required no HEAD, tag read or upload.
Both restored outboxes delivered their lost-acknowledgement and pending events;
exact replay added no duplicate events. Initial upload request counters were
not retained by the failed driver; the saved counters describe the recovery.

The full populated local run separately sealed its coordinated checkpoint and
matched all 769,643 indexed physical files by path, size and mtime against its
immutable media view. Packaging and restoration are still running. Neither the
small cloud drill nor path comparison proves a full populated cloud restore.

The inventory exposed a remaining migration issue: all 282,112 objects required
by the current media master are present, but 57,725 older videos lack additional
S3 checksum headers. All 9,016 selected image-archive objects have full-object
checksums. Read-only local probes matched the observed SSE-S3 ETags for one
single-part video and one thirteen-part video using the installed uploader's
16 MiB part size. This is exploratory evidence, not implemented adoption or
permission to reupload older media. Verified adoption must be implemented and
reconciled before the native publisher handles the whole library.

The daily role cannot read bucket versioning, and the separate administrative
AWS login requires refresh. Exact metadata lifecycle and narrowly scoped daily
permissions are prepared but not installed. Standard cleanup, actual native
daily churn, final production cutover and observed operating cycles remain open.

## Verified adoption of historical cold videos — 2026-10-07

The host publisher can now retain existing filename-based video objects that
predate S3's additional checksum headers. It compares the complete local file
with the remote plaintext/SSE-S3 single-part MD5 or historical multipart ETag,
trying the observed 5 MiB and 16 MiB part layouts. Each adopted descriptor also
requires independently calculated local SHA-256. Parts, count and metadata
alone cannot authorize adoption; unsupported encryption or contradictory
checksums fail without uploading a replacement.

The fallback is explicit in the historical video path. New content keys,
uploads and archive publication still require full-object S3 checksums, including
recovery of an interrupted upload. The existing durable media ledger retains
adopted evidence, so an unchanged subsequent inventory can reuse it without
per-object requests. Offline reconstruction and streamed downloads validate
both the saved historical checksum and local SHA-256. Changed source bytes use
a new immutable key after the original object's identity has been established.

All 288 backup tests passed in 63.6 seconds. New coverage exercises complete
publisher runs for all three historical layouts, restart/request reuse, local
edits, response corruption, SHA-256 mismatches, contradictory headers,
encryption restrictions and strict new-object verification. No test uses real
AWS or rclone. A separate read-only check used the actual implementation with
three existing cloud videos, including both multipart sizes. All matched their
complete local bytes using six HEAD requests and no upload, download, thaw or
production-ledger change. An initial private sample selection mistakenly chose
a modern object for the 5 MiB case and stopped at its header assertion; filtering
the saved inventory correctly resolved that driver error.

The [operational evidence](native-backup-operational-rehearsal.json) retains
these measurements. Hashing a 1 GiB in-memory sample took 3.63 seconds with the
additional historical digests versus 0.85 seconds previously; this measures CPU
overhead, not disk throughput or the duration of a whole-library migration.
Initial adoption still requires reading the relevant local media. The complete
library pass, populated coordinated restore, actual daily-change/request costs,
cloud retention setup and production cutover remain open.

## Resumable whole-media verification and live caller audit — 2026-10-07

Every video in the frozen compatible backup master exists in the separately held
read-only media snapshot: 273,096 paths and 10,481,030,221,828 bytes, with no
missing, nonregular or size-mismatched paths. This is a path/size preflight, not
a checksum comparison or a common production capture boundary.

A bounded operational run then verified all 9,016 required image-archive S3
checksum headers and read the complete local contents of 500 videos totalling
6,604,813,720 bytes. All 500 matched their cloud checksums. The run took 226.4
seconds, with 300 LIST and 9,516 HEAD requests and no retries. Its isolated ledger
retains per-file signatures and verified object identities. It did not upload,
download, thaw, tag or delete any cloud object, or alter the production ledger.
Header verification does not claim to have restored the archived image bytes.

The full selection is now being checked with four low-priority readers. On
resume, one fresh inventory reused all 9,016 archive records and 500 video
records without additional HEAD requests or repeated local hashing. Missing or
changed evidence records an unresolved issue; only complete successful coverage
can produce the final media manifest. The portable restore rehearsal continues
independently with its original binary, checkpoint and runtime. Neither long
check has a final passing result yet.

Read-only service inspection confirmed the compatible Stash and n8n are running
and the existing scrape/backup timers are enabled. Inspected stopped services
reported success; some translation and scan work was still active.
No schedules changed. The caller audit also confirmed an outstanding conversion:
`dedupe-scraped-content` still uses `scrape_catalog` to prepare/reconcile deletion
and preserve orphan NFO metadata. Both its scheduled unit and pre-backup wrapper
must use native plan/apply before those catalogs can be retired. The post-dedupe
recent-directory scan/autotag flow and staged manual library helpers also need
their final native handoff. Existing live launchers cannot simply be retained
unchanged at cutover.

The full media run subsequently retained 4,489 verified video receipts and
found two same-size local/remote content differences, totalling 36,345,921 bytes.
Independent full-object CRC and single-part MD5 checks both differ; each current
local file matches its frozen copy. The remote objects were last changed on
2026-03-16. Their original contents were not retrieved. These are unresolved
backup differences, not successful adoptions. The native publisher can preserve
the old objects while writing the current bytes to new immutable keys; an added
host regression verifies that first-adoption path and subsequent unchanged reuse.
Fifteen focused changed-media tests passed, and the final new regression passed
in both fixture variants. No real media upload has been performed.

Four concurrent media readers slowed artwork packaging from approximately 44
to 110 seconds per thousand files. That one verification process was suspended
with `SIGSTOP`, keeping its memory and durable receipts. Resume the same process
after the original populated restore finishes; do not start a replacement merely
because it is stopped. Packaging returned to about 49 seconds per thousand files
in the first post-suspension observation. The overall migration goal remains
active, and the dedupe/caller conversion work can proceed independently.

## Native dedupe host client and restart proof — 2026-10-07

The producer package now includes `stash-dedupe`. It uses bounded fclones group
reports, retains the oldest physical copy, and delegates every removal to the
schema-95 native service. It saves the fixed scope, private manifest and each
exact request before submission. After interruption it reads the committed
receipt before replaying; an uncertain result or reused-request conflict cannot
silently acquire a new UUID. Pairs receive fresh previews sequentially so a
previous primary-file update cannot invalidate every later saved request.

The caller holds its state lock, the existing backup/dedupe lock, and all declared
native-worker publication barriers through discovery and application. It checks
root/mount/lock identities before subsequent operations and rejects out-of-root,
symlink, repeated, unsupported or size-changed candidates. Fclones never receives
a removal command. Ineligible owners and known stale/unequal-byte rejections
remain review outcomes, separate from committed removal counts. Pending intents
retain the active run; completed runs retain inspectable manifests and receipts.

All 597 producer tests passed in 51.1 seconds. The subsequent process-output/
deadline regression and final client checks passed all thirteen focused tests.
The actual Go/Python HTTP test dropped the first committed deletion response,
closed/reopened the native database and restarted Python. It recovered the
original receipt without a second Apply, then previewed and removed the next
duplicate against the changed primary/owner state. The existing selected title
and surviving primary remained correct. The HTTP checks passed in 14.8 seconds;
the final pinned Go lint reported zero issues.

Only isolated fixture files were removed. The installed host launcher, pre-backup
wrapper, service/timers and production database remain unchanged. Installation
still needs reviewed worker-lock inventory, client-state backup coverage,
service exit/timestamp handling, and the separate orphan-sidecar/direct-scan
handoff. Legacy catalogs cannot be retired on the strength of this client test.
The full populated restore continues with its original schema-94 binary and
checkpoint; whole-media reconciliation remains suspended until that run finishes.

## Compact dedupe state, host scheduling and backup coverage — 2026-10-07

The not-yet-installed native dedupe client now uses one private SQLite journal
instead of a manifest/request/result file tree. It retains exact manifests,
intents, results and run summaries, with atomic completion and one active run.
This keeps a large candidate batch within the backup component limit and avoids
generating thousands of tiny operating-state files. Foreign or earlier
development-only journals are refused rather than silently replaced.

`stash-dedupe-host` supplies explicit runtime configuration, private key-file
authentication, the existing 24-hour cooldown and pre-backup bypass. Pending
requests resume despite a recent completion stamp. Busy locks skip without a
new stamp; uncertainty/failure preserves the prior timestamp. Completed review
cases remain explicit while permitting the backup to continue. The key is read
for API requests rather than copied into the candidate finder's environment.

The version-3 worker inventory can declare this maintenance runtime. It requires
an inventoried media root and the complete worker publication boundary, captures
the configuration and key file, and snapshots `dedupe.sqlite3` as an
`operating_database`. The inherited backup descriptor must itself own an
exclusive flock on the configured library lock. A second descriptor observing
another process's lock is rejected. Restore testing retained a pending request
committed only in an open WAL, then reopened it with its original identity.

All 604 producer tests and 293 backup tests passed. The actual Go/Python API
restart test now uses the host launcher and private key file; it verifies the
lost-response recovery and completion timestamp. Four older fixture connections
were also closed explicitly while preserving their transaction commits: their
deferred ResourceWarnings had been contaminating another CLI test's captured
stderr. The warning was fixed at the allocation source rather than suppressed.

The concrete host service, pre-backup and manual wrapper replacements are staged
under `.local/native-host-maintenance-20261007/installation`. Shell parsing and
host systemd verification passed. The staged dependency rehearsal resolved all
30 worker profiles, 62 components, their shared publication directory and the
existing backup lock. It used an empty isolated dedupe journal and a fixture key;
no real dedupe request or production change occurred. The live journal/key,
installed package refresh and final inventory are activation steps, and the
direct-file scan handoff must precede removing the old post-dedupe trigger.

The full populated rehearsal sealed its 238,693-artifact archive and progressed
to the relocated restore. Its original Python process was verified alive with
archive/restore descriptors; the restored library file is 21,901,381,632 bytes.
There is still no final passing restore/replay receipt. The compatible service
remains active, and root free space remains above 79 GiB.

## Resumable scheduled local-file discovery — 2026-10-07

`stash-intake-folders` uses the existing application-authenticated manual-intake
API, without source credentials or an in-memory scan queue. It recursively reads
bounded directory pages, persists its position and exact admissions in one private
SQLite journal, and recovers receipts before admitting additional files. The
configured pending limit survives process and server restarts. Queued admission
never becomes a completed import merely because an HTTP request succeeded.

Explicit collections provide filename/performer defaults. Nested scopes use the
most specific configured collection; declared source scopes are excluded. Files
with old timestamps are still discovered in new/nested directories. Existing
indexed files form an explicit initial baseline. Later filesystem or policy
changes can trigger intake, while registration itself does not produce another
request. The preview's additional opaque file signature includes confined file
identity/change-time evidence without changing existing admission signatures.
The native UI validates the additional response field.

Changed directory pages restart from their beginning without repeating completed
file versions. Uncertain replies retain the original request. Known stale-file
rejections and failed/cancelled attempts remain recorded; unchanged terminal
attempts do not loop automatically. The service holds the shared backup lock
during admissions, and worker inventory includes its config, private key and
`intake.sqlite3` as an operating database. A capture/reopen fixture preserved its
original pending request. Packaged service/timer units run bounded invocations
independently of dedupe and passed host systemd verification.

The real Python/Go fixture drops an accepted response, restarts both client and
database, disables the worker, and verifies recovery without another Apply. Once
that request is explicitly cancelled, only the next file is admitted. A real
image-worker test verifies that registration preserves the discovery version and
that overwriting bytes while restoring size/mtime changes it. The initial version
fixture omitted the worker's required effects callback; the corrected fixture
passed. Full producer (614), backup (294), library (8), archive (121), UI (789),
build/generation and lint checks passed. The combined full Go integration suite
hit its 20-minute per-package limit in API/SQLite fixture tests; the aggregate
gate did not pass. The ingest package completed in 1,014 seconds. All reported
failures were aggregate timeouts, rather than assertion failures; a complete
rerun with sufficient package time remains required before publication.

Read-only scope inspection found 915 disabled directory collections: seven
explicit folder-policy scopes and 908 rootless historical membership groups.
The latter deliberately retain directory membership evidence and must not be
automatically activated as manual scan scopes. Deployment must prepare the actual
manual-root/folder coverage and current source exclusions, preserve ZIP scanning,
and finish sidecar cleanup before replacing the old recents helper. This increment
does not install a live launcher, activate a policy or change production.

Evidence is under `.local/native-folder-intake-20261007`; the full gate uses
`.local/native-discovery-client-20261004/native_folder_intake_full_gate.log`.
The original populated restore is still importing its sealed archive; its media
verification companion remains intentionally suspended to preserve I/O bandwidth.

## Automatic manual-intake scope selection — 2026-10-07

The scheduled client now configures base scopes once. The backend selects the
most specific active folder policy that applies to scans, including policies
created later through Stash. Equal-depth conflicts remain explicit and do not
prevent discovery of a more specific child. Disabled policies can mask inherited
values, and configured policies that opt out of scans are respected. Saved
requests retain the automatic base UUID/revision as well as their selected policy
collection. The backend rechecks source ownership, policy choice and revisions
at admission and before the first file registration commits. The client also
prevents policy changes from stacking a second active request for the same path.

An initial populated audit caught a real distinction missed by the small fixtures:
20 imported collections use whole-root access because their files span renamed
account directories. Excluding that permission prefix would block all manual
intake. Whole-root access now excludes only directories with direct native source
file evidence; mixed parent folders remain traversable. Exact source observations
and producer file-job history reserve individual destinations, including newly
submitted files without folder history. A producer admission supersedes automatic
manual work that has not registered its file yet. Explicit interactive imports
remain available for review.

The revised read-only audit passed 1,710 indexed lookups: 1,674 narrow source
prefixes, 34 additional folders with source evidence, the root and a new manual
folder. Those 34 folders contain 4,773 indexed files; being outside a catalog's
single prefix did not mean they were manual purchases. No catalog/association
rows were edited to obtain this result. The lookup uses the existing directory
and file-observation indexes and does not scan capture bodies or the whole library.

Focused tests cover root-wide access, renamed/Unicode folders, mixed subfolders,
source registration after preview/admission, policy selection, database restart,
real image publication and producer/manual races. The Python/Go HTTP fixture adds
a new source and child policy after restarting, then verifies only the new manual
file is admitted without changing host scope configuration. Fourteen Python
scheduler tests and nine UI request/recovery tests pass. The first UI test used
the wrong fixture method and was corrected to `submitSaved`. One complete gate
stopped on formatting; a later run was deliberately stopped when the populated
scope bug was found. Complete validation of the final correction remains required.

Private staging under `.local/native-scan-scope-20261007/installation` includes
the root-wide manual collection/policy API requests, service, timer, environment
and configuration. The default policy supplies a filename title for new files
without guessing performers or marking them organized. The existing seven
disabled folder policies need their source/manual intent resolved separately;
the 908 unbound membership groups are not activated. The selected canonical root
remains disabled/unbound. Installation, final backup inventory and live intake
verification are still cutover steps, and the compatible service is unchanged.

## Manual helper handoff and sidecar retirement inventory — 2026-10-07

The remaining host caller audit found the image-title helper still using retired
filters and a scan helper invoked by Home Assistant. Native stdlib replacements
now accompany the two converted tagging tools in `integrations/library`. Title
repair uses canonical filters, bounded ID-sorted selection and primary visual
files. Ambiguous performer names require an explicit ID; dry runs send no writes,
and an unconfirmed title edit stops later edits. An optional empty-title filter
preserves existing titles. The explicit scan helper retains cover/phash generation
and ZIP support through the existing scanner, reporting admission separately from
completion. It does not become the scheduled discovery worker.

All twelve helper tests pass. Five actual generated query/variable sets validate
against the native schema; the first check caught a missing inline fragment on
the `VisualFile` union, which was corrected before staging. Private deployment
preparation under `.local/native-library-handoff-20261007` contains nine exact
files, private-key launchers and backup component declarations. It preserves the
Home Assistant command prefix. Live commands, keys and application state were not
changed; installed dry runs and a real ZIP scan remain cutover checks.

A read-only sidecar inventory found zero NFO/text files and zero traversal errors
in both the held media snapshot (781,527 files) and the live tree (781,910 files).
The selected native copy already retains 272,556 source documents/content bodies.
No files were removed. These observations allow the old orphan-sidecar cleanup
step to retire without a replacement cleanup engine, provided the final quiesced
inventory and imported document reconciliation also pass. The live observation is
not a common backup boundary. Reports are under
`.local/native-sidecar-audit-20261007`.

Deduplication remains fclones-backed: fclones discovers candidates, while Stash
preserves associations and performs journaled removal. The original full restore
and release gate continue; media reconciliation remains suspended until that
restore completes. None of these preparations activates production writers.

## Final release validation and restore verification progress — 2026-10-07

The complete release gate for the final manual-intake scope correction passed:
`make GO_TEST_TIMEOUT=40m generate ui validate-fork` completed in 2,065 seconds.
All 17 frozen scope files still match the validated revision. The separately
checked manual helpers also match their committed sources. Commits through
`bbcd61496` are pushed; its Build, Lint, v3 browser and GHCR publication workflows
have succeeded. Production remains on the frozen compatible image.

A further read-only audit identified 516 disabled, local-only catalog collections
whose old `legacy_catalog` kind would incorrectly exclude them from manual
intake. The staged API plan changes their kind to `directory`, retains their
disabled state and rebinds their existing disabled policies. Two existing explicit
folder defaults can also be activated for local content. The audit excluded
collections with remote definitions, nonlocal captures, source-post links,
credentials or outstanding job references. The 1,036 proposed operations remain
unapplied: they require an isolated API rehearsal and fresh evidence/revision
checks at the final writer boundary. Historical rootless membership groups remain
disabled. Evidence is under `.local/native-local-collection-audit-20261007`.

Current backup and producer packages are installed in separate inactive release
directories. Original verifier runtimes and live commands were not changed.
The new producer fingerprint requires matching staged worker profiles and n8n
packaging before activation; an installed package alone does not establish a
complete handoff.

The original populated restore has reached its final independent artwork hash
check. That implementation visits an unordered set of paths and reports no
progress for this phase. Future restores now visit sorted paths and report the
number checked every 1,000 artifacts, while retaining every checksum and the
last-written completion receipt. All 121 archive tests and 26 native backup-store
tests pass with that change. The already-running restore continues with its
original runtime. Its final receipt, populated schema-95 migration, media
reconciliation, publication/cost proof and production cutover are still pending.

## Separate artwork pins from bulk backup staging — 2026-10-07

The production mount audit found original artwork on the system filesystem while
the larger backup work can use `/tank`. The host backup configuration now accepts
`artwork_pin_directory` independently of `state_directory`. The former retains
hard links on the original artwork filesystem; database/component snapshots,
packed archives and restore scratch remain in the latter. Both retain the
configured free-space reserve. The complete configuration still binds every
unfinished run, so moving its pins cannot silently detach existing recovery state.

All 295 host backup tests pass. A real two-filesystem test keeps bulk state on a
different device, replaces an original artwork path, reopens its retained pin and
verifies the original inode/bytes. It also rejects changing the pin location on
retry. Deployment must place native server checkpoints on the spacious staging
filesystem as well; this setting does not move the server's own backup directory.
No live storage paths or backup schedules were changed.

## Coordinate external workflow writers during capture — 2026-10-08

Worker publication barriers do not stop n8n's execution database and payload
pruning. The host backup now accepts explicit `quiesce_containers` and a bounded
`quiesce_timeout`. It acquires worker barriers before pausing external containers,
captures the declared database/payload components and retained filesystem views,
then resumes them before the large native database copy and remote publication.
Each pause is owned by a transient user systemd service with an exact container
ID and an independent resume deadline. Already-paused containers are refused;
originally stopped containers remain stopped. Replacements and expired guards
cannot seal a new checkpoint. Sealed retries inspect only the original evidence.

All 123 archive and 304 backup tests pass. An isolated Podman test using the
staged n8n worker image verified matching SQLite/payload capture, normal resume,
automatic resume after the publisher exits without cleanup, expiry refusal and
sealed replay. It used fixture data with networking disabled and checked the
live n8n container stayed unchanged. The proof is retained under
`.local/native-container-boundary-20261008`; production configuration and daily
backup activation remain pending the full migration/restore gates.

The original populated rehearsal reached its enclosing eight-hour Go test
deadline while independently hashing restored artwork. The original Python child
remains alive and continues from the same sealed checkpoint. The export/import
has not been restarted. A separately compiled continuation uses frozen schema-94
code and refuses to run before that child succeeds and both original processes
are gone; it covers the remaining deletion-journal recovery and producer replay.
The timeout is retained as a failed harness result. Full restore success remains
unproven until the original child's receipt and those remaining checks pass.

## Pinned runtime and deployment preparation — 2026-10-08

The selected `bbcd61496` source image and its explicitly selected wrapper are
recorded by digest under `.local/native-coordinated-restore-20261007`. The exact
wrapper started a fresh native database and restarted it in an isolated container
with networking disabled, no published ports and fixture-only mounts. Both runs
reported schema 1000095; the embedded UI, native media-root route, native lineage
and exported database validator passed. The live Stash container was unchanged.
The first probe used an incorrect API path and failed; the corrected probe is
retained separately under `.local/native-stash-deployment-20261008`.

The staged Stash unit uses a separate native configuration/database directory,
server checkpoints on `/tank`, an empty native plugin directory and the pinned
wrapper. It removes obsolete catalog mounts and gives populated validation a
30-minute startup allowance. The initial staging helper passed the wrong Quadlet
environment variable and therefore inspected the existing units. Corrected
validation uses `QUADLET_UNIT_DIRS`, checks the exact generated image and mounts,
and verifies only the two intended units. Neither unit nor configuration is
installed.

The prepared daily backup service retains the existing 03:00 timer, pre-backup
dedupe invocation and `--compact` setting. Its timeout grows from three to
48 hours to accommodate the initial full native publication. The same oneshot
service prevents overlapping timer invocations. Configuration now declares the
health check and backup service/timer among 38 explicit components, in addition
to resolved worker dependencies. This is preparation, not proof of a final
common production capture.

The original restore has finished its independent artifact checks and is running
the frozen schema-94 snapshot validator. A guarded continuation chain waits for
the original success receipt and process exit, then runs recovery/replay, a
separate populated schema-95 copy/migration and full typed reconciliation. It
resumes the same suspended cold-media auditor only after those checks pass.
Each stage rechecks its frozen inputs. None of these checks changes production
or replaces the pending full backup/cost and cutover evidence.

## Manual helper runtime proof and release-check target — 2026-10-08

The five staged manual-helper modules match their committed source hashes and
passed a real runtime check against the pinned native wrapper in 35.9 seconds.
The isolated library contained a fixture MP4, a two-image ZIP and one loose
image. Both scanner jobs reached `FINISHED`; repeating the scan preserved IDs
and metadata. Canonical/alias attachment covered the scene, three images and ZIP
gallery with one-item pagination and batches. Gallery tagging selected only its
two images. Filename repair retained an existing title, repaired empty titles
and was idempotent; duplicate performer names required an explicit ID. Every
dry run left the observed library unchanged. The container had no network,
published ports or production mounts, and the live Stash identity was unchanged.
Evidence is under `.local/native-library-handoff-20261008/fixture-v1`.

This verifies the actual staged modules and application API. Installation of
the host launchers, their private key path and the Home Assistant command path
still requires the native cutover. The same probe showed that the GHCR compile
step omitted `UPDATE_REPO`, despite the regular binary build already setting it.
That step now passes the repository identity through the existing Makefile linker
flag. Corrected image publication and wrapper verification remain pending; frozen
compatible images and the running restore binaries are untouched.

## Corrected image selection and deployment checks — 2026-10-08

Source `74d96dc3a` and wrapper revision `5dab1643965a` passed publication. The
selected source digest is `sha256:befb9f77f1004d4581361b1e8893e6cf527aaf2dbdc48e51dbb325f80b2cec35`;
the hardware-accelerated wrapper is
`sha256:96a551cc2f1dc5159bc6259a667ba8cb5d4ceef23a0bd08f8322f828a4d93fdb`.
Their binaries have identical SHA-256 values. The extracted validator accepts a
native schema-95 fixture and rejects a foreign database without changing either.
Fresh startup, restart, embedded UI, native routes and closed-snapshot validation
pass in a network-isolated container with no production mounts or published
ports. Startup logs verify that update requests target `notsafeforgit/stash`.
The initial probe incorrectly expected linker flags in trimmed build metadata;
its failed result is retained, and runtime verification supplies the actual proof.

Evidence and fresh unit/configuration preparation are under
`.local/native-stash-deployment-74d96dc3a-20261008`. Quadlet validation checks the
exact image, native mounts and complete generated unit set. Only the validator
path changed in the pending backup configuration; original restore binaries and
inactive Python runtimes remain intact. These are staged deployment checks.
The populated restore, full schema-95 reconciliation, live handoff and backup
activation gates remain required.

## Complete populated local restore proof — 2026-10-08

The original capture/restore child completed successfully in 38,821 seconds.
It restored and verified 238,693 artifacts, including all 238,574 original
artwork files and the populated schema-94 database. All 769,643 indexed media
paths matched the held ZFS snapshot by path, size and modification time. The
restored n8n state retained 43 workflows, nine credentials verified offline with
the restored configuration, and all 19 referenced execution payloads. This is
local restore evidence; it does not complete full cold-media checksum adoption
or populated S3 publication.

The original enclosing Go test's eight-hour timeout remains a failed result.
The guarded continuation subsequently passed in 2,235 seconds using its frozen
schema-94 runtime. It recovered the exact bytes from an interrupted, uncommitted
file deletion and replayed both restored producer outboxes. Each producer
recovered its two outstanding acknowledgements; replaying again added zero
events. The complete receipt is
`/tank/stash-native-rehearsal-20261007/coordinated-run/evidence/coordinated-result.json`.
The existing release chain then copied the selected database into its separate
schema-95 candidate and started migration. Full typed reconciliation, the
populated policy/API and wrapper checks, and production cutover remain pending.

The older schema-77 rehearsal's 238,574 artwork entries matched the verified
replacement exactly. After the complete recovery/replay result and a fresh
privileged read-only process-use check, only its obsolete compressed-object
directory was removed. This reclaimed 39.1 GiB and left approximately 117 GiB
free on the system filesystem. Original manifests, inventories and verification
records remain, together with the replacement archive, restored database and
selected migration baseline. The cleanup receipt is
`.local/native-coordinated-restore-20261007/old-bundle-retirement.json`.
Production writers, media and backup schedules were unchanged.

## Stash-box protocol checks and lookup corrections — 2026-10-08

New HTTP protocol fixtures found and reproduced two retained client defects:
multi-performer lookup skipped nonempty names, and a shortened fingerprint
response panicked while reconstructing the input order. Batch lookup now skips
empty names and preserves the position of every supplied performer. Fingerprint
lookup rejects missing/extra batch results and null scene entries as errors.
It retains the 40-scene request limit, MD5/OSHASH/signed-pHash conversion and
empty-input behavior.

The passing fixtures also cover authentication, cancellation while rate-limited,
remote performer deletion/merge flags, studio/tag metadata, endpoint-qualified
fingerprint submission and multipart scene drafts with original image bytes.
Submission tests use local HTTP servers only. Focused client/model tests and the
repository-pinned linter pass. An initial draft fixture incorrectly supplied an
unloaded file relationship; the corrected fixture supplies an explicitly loaded
empty list. The original failing results remain available.

A separate compiled-client probe made exactly twenty read-only requests across
the four configured services: StashDB, ThePornDB, JavStash and FansDB. Each passed
authentication and the generated performer, fingerprint, studio and tag lookup
queries. Lookup inputs used synthetic IDs/fingerprints; no real metadata, user
details or credentials were retained. No live drafts or fingerprints were
submitted. The public aggregate is
[native-stash-box-verification.json](native-stash-box-verification.json).
These checks verify the client boundary, not installed native launchers or the
remaining manager-level remote-merge handling. The selected `74d96dc3a` image
predates the two client fixes; final image selection must include them without
replacing the frozen binaries used by the ongoing populated verification.

The compatible browsing baseline now records sixty actual HTTP requests across
scene, image and performer pages. Warm p95 latency was 9–19 ms; the first image
request took 1.38 seconds. Before measuring native performance, the release
budgets were fixed at 100 ms idle p95, 250 ms ingestion p95, three seconds for
the first request and five seconds for any individual request, with no read
failures. A disposable harness passed 288 capture events, 24 completed file jobs
(twelve images and twelve videos) and 180 concurrent browsing requests. The
full-library run and complete original-row comparison remain queued after the
populated startup check. An image's exposed port with no host binding initially
failed an overly strict fixture guard; the corrected guard checks actual port
bindings. Both original failures and corrected smoke evidence are retained.
Browser rendering and thumbnail download latency are separate acceptance checks.

## Stash-box performer refresh and merge safety — 2026-10-08

Remote performer refresh now resolves local ownership before following merge
redirects. A bounded chain retains the selected local performer ID and native
UUID. Deleted records, unavailable targets, cycles, excessive chains and links
to a different or ambiguous local performer stop the refresh. The update
transaction rechecks the selected endpoint's original link and the destination's
current ownership. Other providers' IDs remain intact. Adding an already linked
remote performer does not create another local performer, including when the
link appears between lookup and the create transaction. Exact-name lookup also
rejects multiple remote matches while interactive search retains all candidates.

Real SQLite and local HTTP fixtures cover successful chained merges, native UUID
preservation, deleted/missing/conflicting targets, changed links, concurrent link
creation and duplicate-add prevention. The complete focused manager/client/model
check passed in 31.0 seconds; the repository-pinned linter reports zero issues.
No live performer edits or remote submissions were used for validation. The
configured-endpoint reads remain the preceding twenty-request proof; generated
query documents are unchanged. The aggregate report retains both checkpoints.
Final native image selection must include these changes. Attribution of
Stash-box metadata choices still needs a separate audit against the transition
plan; these results do not claim that wider requirement is complete.

## Attributed provider import storage — 2026-10-08

The attribution audit confirmed that retained stash-box IDs did not identify
which field values were accepted from a provider. Native schema 1000096 now adds
immutable provider-import receipts keyed to the native entity identity. Each
receipt records endpoint, remote ID, application path, accepted values and time.
Values are read from the library in the edit's write transaction, including
merged relationships. A failed receipt prevents commit even if its error is
accidentally swallowed. Later manual edits never rewrite the historical import.

Batch performer creation and refresh now record selected fields and the final
provider ID after a remote merge. Field exclusions remain effective. The native
API exposes bounded, indexed import history for an identity. UUID adoption,
performer merges and deletion preserve the original attribution; anonymization
removes it. Relationship snapshots use native UUIDs, performer dates preserve
precision, and artwork snapshots contain digests instead of duplicate image
bytes. The table is part of normal database snapshots, without a new backup
component. The contract and remaining integration work are documented in
[native-provider-metadata.md](native-provider-metadata.md).

The final SQLite, manager and HTTP checks passed in 44.8 seconds, covering all
four supported entity field shapes, configured batch exclusions, rollback,
adoption/merge/deletion/restart, malformed requests, schema-95 migration,
foreign-table collision, corruption rejection and anonymization. Existing
schema-94/95 migration fixtures and metadata choice tests also pass. The pinned
linter reports zero issues. Early tests caught an uninitialized entity-store
reader and an unconfigured artwork test fixture; their failing logs are retained.
The checked scope is recorded in
[native-provider-metadata-verification.json](native-provider-metadata-verification.json).

Studio/tag batches, scene identification and related entity creation, interactive
scrape selection, and history/current-field attribution views still need wiring.
This checkpoint does not complete the plan's stash-box metadata requirement.
The existing schema-95 populated rehearsal retains its frozen runtime and inputs;
the later schema-96 application needs its own populated migration/image checks.
Production, producer launchers and the compatible release tags remain unchanged.

## Provider attribution in batch imports and identification — 2026-10-08

Studio/tag batches now record accepted fields and parent/category imports.
Scene identification records the scene and every newly created related entity
within its existing transaction. Missing or failed provider receipts abort the
operation, with no successful hook notification. A fixture deliberately fails
the final scene receipt after related imports, proves the entire operation rolls
back, and retries successfully using the same source result. Ordinary scrapers
without a stash-box endpoint retain their separate behavior.

Creation receipts include required names while excluding invalid optional
values that creation discarded. Tag parent values use native UUIDs. Studio image
loading now uses the selected `images` entry directly rather than dereferencing
the deprecated optional single-image field. The real identification fixture
loads an image with only the current array present and records its digest.

Studio refreshes retain the explicitly selected local ID when matching supplies
no stored ID. Studio/tag batches reject conflicting matched or endpoint-linked
owners and refuse to replace a different current provider link. Duplicate exact
tag-name results require selection by remote ID. A failed parent creation leaves
no stale stored ID in its reusable source result. Identification's create-missing
option now preserves an already matched parent studio's curated metadata.

The complete identification package passed in 11.3 seconds; all focused
Stash-box manager tests passed in 67.1 seconds. SQLite fixtures cover exclusions,
parent/category relationships, rollback/retry, matched-parent preservation,
invalid optional values, provider ownership conflicts and remote tag ambiguity.
The pinned linter reports zero issues. Initial identification fixture failures
were test setup errors: a configuration import cycle, omitted migration
registration, and an invalid qualified struct field after moving the tests to an
external package. Original results are retained. The follow-up evidence is in
[native-provider-import-paths-verification.json](native-provider-import-paths-verification.json).
Interactive selection/save and attribution UI remain required; production and
the frozen populated verification inputs are unchanged.

Separately, the original populated schema-95 migration finished its fresh reopen
at 05:38 UTC. The full run took 7,127 seconds; opening validation took 2,095
seconds, migration 2,443 seconds, and fresh reopening 2,589 seconds. No jobs or
policies were activated. Its existing owner automatically advanced to full typed
reconciliation, with that child process confirmed live. These long validation
times do not establish acceptable application startup performance; the queued
wrapper/performance gates and later schema-96 verification still apply.

## Interactive provider choices and history — 2026-10-08

Scene and performer scrape review now carries explicitly selected provider
fields into Save. Later manual edits remove a field's pending attribution, even
when typed back to its previous value; discard and successful save clear the
pending choices. List merges can preserve multiple selected sources. Related
performers, studios and tags use their own remote IDs, and a manually substituted
name receives no provider attribution. The existing related-item create action
still occurs during Apply, independently of saving the parent form.

GraphQL create/update mutations validate that every selected field is allowed
and included in the same edit. The final saved values and receipts commit
together. Invalid receipts roll back the whole edit and emit no success hook.
Merge values reject provider selections explicitly. The UI exposes lazy, paged
history on scene metadata review and performer/studio/tag history tabs, with
earlier performer identities available through Source accounts. Historical
accepted values are clearly distinguished from current field origin.

The complete API package passed in 915 seconds. All 796 UI tests, types, locale
and native-contract checks passed, and real embedded assets built successfully.
Eight provider browser checks passed across Chromium and WebKit, including
mobile and desktop scene review; forty mobile-toolbar checks also passed. The
pinned Go linter reports zero issues. Initial fixture/environment failures and
the earlier eight-minute aggregate API timeout are retained alongside the
corrected results in
[native-provider-review-verification.json](native-provider-review-verification.json).
Populated schema-96 migration, final-image startup/performance, owner acceptance
and production cutover remain outstanding.

Separately, schema-95 reconciliation passed at 06:38 UTC: all 40,512,444 original
rows retained their typed values, integrity was good and there were no foreign
key violations. The downstream policy rehearsal stopped before opening the
candidate because its expected total incorrectly used schema 93's row count.
Schema 94 had added exactly one migration-history row; per-table reconciliation
accounts for the difference. The failed check and its stopped dependent
supervisors remain intact. A separately compiled continuation uses the original
schema-95 source and identical 1,038 policy requests with the corrected count.
Its first supervisor stopped at an outdated result-path guard before invoking
the test; the corrected supervisor is now running. Production is unchanged.

## Populated release checks and current n8n handoff — 2026-10-08

The policy handoff and schema-96 migration/reconciliation passed. The selected
`15b45e2cd` source image and `90b8355` wrapper passed populated startup and restart
in 262 and 172 seconds, preserving all table counts and published filter state.
The wrapper no longer downloads the retired plugin dependency bundle at startup;
Python requirements require an explicit opt-in and an explicit file.

The separate populated load completed 344 captures and 24 image/video jobs
alongside 180 browsing requests. Scene, image and performer list p95 latencies
under ingestion were 20, 37 and 22 milliseconds, within the predefined budgets.
The full comparison then preserved every original value and SQLite type across
40,513,487 rows in 307 tables; the fixture added 8,380 rows and had no foreign-key
violations. These are isolated API measurements, not browser or website timing.

The original fixture/configuration and comparison failures remain recorded. The
startup fixture initially omitted the marker for its already-published filter
migration. The first row-comparison guard incorrectly rejected a retained WAL;
the corrected comparison reads committed WAL through SQLite and verifies it
unchanged. A final expected-total guard used the schema-95 input count, omitting
schema 96's single migration-history row. Every table's count was reconciled
against the migration report without repeating the successful comparison or
load. No database contents or release budgets were changed for these fixes.

Production n8n updated to 2.42.4 while the earlier native extension used 2.42.3.
A separate extension now preserves the current base image's layers and runtime
settings. Its 121 installed producer modules match source; all nine converted
workflow graphs and ten container profiles passed their runtime checks.
Both main n8n and its native worker are staged on this exact image. The existing
Twitter/Reddit host command names and four scheduled submitters are also staged,
with offline checks and systemd validation. The 73-file release includes these
paths and adds their six missing backup components without changing cloud policy.

The [verification report](native-populated-release-verification.json) records
the images, measurements, corrections and evidence hashes. Production remains
on the compatible Stash release. Whole-media verification, real native cloud
publication/retention verification, a fresh common production boundary,
migration, coherent activation and owner acceptance remain required.

## Captured performer profile links — 2026-10-08

Schema 1000097 adds profile URL importing to native capture publication and
account ownership review. It reads only bio, website and profile link fields
already present in gallery-dl metadata, requires the captured profile's stable
ID to match the publisher, and appends distinct URLs to that account's linked
performer. Feed owners, depicted-performer associations and display-name matches
do not select the recipient. Host/n8n producer configuration and retention policy
are unchanged; no extra profile fetch or redirect resolution is introduced.

Per-account link evidence is shared across captures. Full, partial and bulk
performer edits remember removed URLs in the primary database, including URLs
removed before their first automatic import. Retrying a capture, restarting,
relinking an account, adopting a UUID or merging performers cannot restore these
links. Explicit manual re-addition remains available. Merge choices also retain
removals when a source performer's URL is omitted from the destination. Both
new tables are included in database backup and removed from anonymized exports.

Focused extraction/storage/ingestion tests, surrounding domain regressions,
performer/account/ingest API checks, Python producer HTTP delivery and the pinned
Go linter passed. The full archive, SQLite and ingestion suites passed in
1,581 seconds. A follow-up connects automatic performer URL updates to ordinary
after-commit plugin notifications, including late ownership links and account
consolidation. Real SQLite and ingestion tests cover rollback, replay and no-op
captures; API/producer checks and the pinned linter also passed. The populated
schema-97 migration/reconciliation passed using the prior startup fixture,
avoiding another 20 GiB copy. It preserved every original cell value and SQLite
type in all 40,513,487 rows, with clean integrity and no foreign-key violations.
The `9c0edb356` application and backup validator were published and verified.
The exact wrapper passed populated startup in 149.7 seconds and restart in
186.8 seconds; all 309 table counts, published filters, entity counts and complete
database bytes stayed unchanged. The corresponding [release proof](native-profile-release-verification.json)
records its artifacts and local receipts. Final configuration selection, a fresh
production boundary and coherent deployment remain outstanding.

## Direct gallery-dl and yt-dlp source coverage — 2026-10-08

Read-only inspection of the actual host command confirmed it is the unmodified
upstream entry point. The shared config still calls legacy catalog preparation
and completion processors; neither replacement is in the staged 73-file release.
The native worker's post/attachment and publication-window adapters also reject
yt-dlp and some generic categories. Existing publisher parsing or filename tests
do not establish download support. This remains part of the original plan.

The source HTTP boundary now includes the pinned yt-dlp bridge. Its separate
YoutubeDL client checks native ownership and service reservations before source
requests, restores rate-limit/timeout/lease/turn failures swallowed by upstream
exception handling, and preserves fatal error logging/recovery behavior.
Optional probes retain their fallback behavior. The guard stops checking when
control passes to a prepared media download, allowing that file to finish after
lease loss while refusing further source traversal. Construction overrides are
restored on each yield, error and cancellation.

Direct ThisVid extraction returns an unresolved reference to Generic, without a
video ID. That reference is now resolved without downloading media before any
file message. The real pinned ThisVid extractor and yt-dlp metadata merge run
in a fixture with a local substitute for the final KVS result. Playlist fixtures
exercise the real bridge and yt-dlp extractors. Native post/attachment identity,
undated-source coverage, other cross-host scopes, the manual CLI and actual
runtime/profile installation remain required; the tests do not claim them.

Ten focused runtime tests, the full 628-test producer suite and the real
Python/native-HTTP delivery test passed. This Python increment changes producer
fingerprints, so the previously staged host and n8n packages must be rebuilt
with the final adapter implementation before activation. Production is unchanged.

## Resolved yt-dlp post and attachment identity — 2026-10-08

Producer and backend now identify resolved yt-dlp videos using the actual leaf
extractor and video ID. Generic IDs also include their source webpage, preventing
same-basename collisions across pages without using rotating download URLs or
local filenames. The pinned bridge supplies a small `ytdl_media` v1 marker only
after resolving the video. Playlist and member labels cannot become video IDs
or declare albums. Publisher IDs retain their existing separate account semantics;
neither publishers nor collectors automatically assign depicted performers.

Source descriptions and actual publication timestamps now have matching
producer/backend projections. An upload date without a timestamp stays day-only,
and absent dates stay absent. Timestamped leaves use the existing half-open
source windows. Undated/day-only traversal still requires explicit implementation;
it currently fails visibly instead of certifying a publication-time window.

Forty-five shared cases cover identities, retained attachment evidence, date
precision, malformed inputs and URL normalization. These exposed a Go/Python
path-rendering difference, corrected by preserving the original path spelling
while normalizing the scheme/host and excluding fragments. The actual pinned
ThisVid/Generic delegation produces matching retained post/attachment references.
Real SQLite tests retain separate videos on separate pages, reuse a repeated
post and share its publisher without assigning ownership.

The native HTTP worker test now exercises a timestamped yt-dlp leaf through
durable admission, download/file reporting, lost-response recovery and exported
receipt restore. All four caller cases and the existing producer HTTP test
passed. The 632-test producer suite and full archive suite passed; the final
identity corrections passed the focused Python, Go, SQLite and HTTP checks.
The complete ingestion package exceeded a ten-minute aggregate limit while
creating an ordinary manual-intake fixture. That failed receipt is retained;
its verbose rerun uses the repository's forty-minute migration-test budget and
must finish before recording full-suite success.

Native schema 1000097 is unchanged. The implementation remains uninstalled.
Undated traversal, permitted cross-host source requests, actual direct/manual
launch integration, remaining generic gallery-dl paths and refreshed host/n8n
runtime/profile artifacts remain necessary before coherent cutover.

## Configured scans without publication dates — 2026-10-08

Native schema 1000098 adds an explicit `traversal` window basis for download
profiles whose sources do not reliably supply publication timestamps. Its
`until` is the frozen request time and `since` must be null. Completion means
the configured scan finished with its reviewed filters and stop rules; it does
not certify a publication interval or exhaustive historical coverage. Existing
published requests keep their encoding and semantics. Date-based launchers and
n8n backfills require published profiles.

The server and durable offline queue distinguish these work identities even
when policy hashes match. Pending scans coalesce to the newest request time,
while a request newer than the active attempt remains pending. Target/root
exclusion still prevents concurrent downloads. Worker capability negotiation,
profile fingerprints and claimed-window checks prevent mode confusion. Database
triggers reject unsupported or mixed bases; the schema version prevents an older
binary from interpreting scans as date coverage. Migration fixtures preserve
existing run, attempt and request rows and verify retries and restart behavior.

The pinned ThisVid/Generic path now queues and finishes an undated video without
inventing a timestamp or confusing the collection with its publisher. The native
HTTP worker case covers an undated yt-dlp leaf, durable admission, lost-response
recovery and exported receipt verification. Archive verification reconstructs
the original Go digest, including the basis, after the producer releases its
HTTP body. Eight shared digest cases and corruption tests cover that boundary.

Archive activity uses the existing Base UI/shadcn presentation to distinguish
pending/completed scans and their request times from publication windows. Mobile
and desktop navigation and both Chromium/WebKit engines passed, including
screenshots of scan coverage with no “all earlier history” claim.

Validation passed: 640 producer tests, 125 portable-archive tests, the 797-test
v3 validation suite, 16 browser checks, generation/build, embedded assets, Go
lint, real SQLite source-run/backfill/migration checks and all five native HTTP
worker variants. Initial checks exposed and corrected a schema-version parameter
type, test setup errors and the archive verifier's older template assumption.
One test run overlapped binding regeneration and failed on a temporarily missing
generated file; subsequent runs use completed generation. The host browser cache
was absent, so browser checks used the existing pinned Playwright container.
The prior yt-dlp increment's original full ingestion rerun also passed in 695s.

This remains development code. The latest verified populated application release
is still schema 1000097. The new schema needs its populated migration/release
proof. Host/n8n producer packages, reviewed profiles and backup verification
packages must be rebuilt from the final code before activation. The actual
manual gallery-dl entry point, remaining generic extractor paths and permitted
cross-host source requests still require integration before coherent cutover.

The original complete gate subsequently passed: `make validate-fork
GO_TEST_TIMEOUT=40m`, including all Go integration packages, producer (640),
manual-library (12), archive (125), backup (304), and UI (797) tests plus lint.
The gate completed in 2,400.995 seconds; it was observed to completion without
restarting it. The separate Chromium/WebKit activity suite passed all 16 checks.
This validates the development increment, not a populated schema-98 migration
or production activation. The remaining manual scraper adapters are being
prepared in an isolated checkout while preserving this exact tested change.


## Pinned Tumblr and gallery file intake — 2026-10-08

The inspected manual gallery-dl configuration also enables Tumblr, JPGfish and
LeakGallery. Their native post/attachment adapters now use captured source IDs
and explicit membership evidence. Tumblr photos share the same source post and
ordered album manifest. Chevereto pages retain individual file identities; their
album/folder labels do not invent publishers or source-post groupings. LeakGallery
HTML files share a partial manifest without claiming a complete album. The
pinned extractor's swallowed pagination errors now fail the native attempt.
Unsupported audio retains an exclusion without being downloaded.

The new evidence marker preserves historical payload partitioning. The complete
archive normalization suite passed, along with all 646 producer tests and three
real HTTP worker/restore cases for Tumblr, JPGfish and LeakGallery. The latter
exercise lost completion/report responses, capture/attachment association,
durable file admission and exported/restored receipts. The shared Go/Python
contract covers 17 valid/invalid metadata cases. Early failures were a fixture
album-label/type error and missing generated/embedded files in the isolated
checkout. The first main-checkout HTTP run exposed an incomplete Tumblr fixture
which attempted a live blog-info request; the corrected fixture supplies that
captured blog and now explicitly forbids live source requests.

The preceding configured-scan increment passed the entire fork gate and was
committed/pushed as `d2421ab9ef7544262339a273f743982398dc1fc5`. This adapter work
is a separate verified development increment. No production
configuration or scraper command has been switched. Cross-host source handling,
the manual command handoff, final artifacts and the coherent migration remain.

The complete gate's generation, UI (797), producer (646), manual-library (12),
archive (125), and backup (304) stages also passed. It then reported one lint
capitalization issue in a new error string, now corrected. The resumed
`make validate-backend GO_TEST_TIMEOUT=60m` gate passed in 2,054.900 seconds,
including lint and every Go integration package; earlier passed stages were
retained rather than rerun. Six real catalog-import cases now verify identity
continuity for JPGfish, Imglike, Putmega, LeakGallery and two yt-dlp namespaces.
The existing importer adds a captured native identifier to a legacy-only post
without changing its UUID, original captures or immutable snapshot proofs.
Restart and acknowledged replay preserve those identities without duplication.
Conflicting claims still require review. The final populated import must use a
fresh common backup boundary; it cannot replace a completed catalog snapshot
with a second snapshot UUID for the same catalog/source pair.


## Observed source dependencies and mixed collections — 2026-10-08

Schema 1000099 adds canonical HTTP origins to new download dependency
reservations. Gallery-dl source requests and yt-dlp source probes use the actual
requested host for reservation and failure attribution. URL paths, query values
and fragments are excluded from the stored origin. The existing service-scope
equivalence, independent-download policy, shared provider cooldowns, download
priority, fairness and lease/definition checks remain in force. Unknown sibling
hosts stay separate. Historical dependency rows retain their original values
with no invented origin; exact-window retries retain newly captured origins.

Explicit mixed-source collections may now admit downloads while each resolved
post retains its own native namespace and identifier. Such collections cannot
run namespace-specific metadata enrichment. The real HTTP worker test exercises
a mixed yt-dlp collection with an additional metadata host, durable intake, lost
response recovery and export/restore. Focused migration/restart/pacing checks and
all 647 producer tests pass. Initial test setup mistakes were corrected: the run
model stores collection identity instead of a namespace, a traversal has no
lower publication bound, and existing independent downloads share cooldowns
without exclusive ownership of an entire website.

The preceding web-media increment is committed as
`0c4c4c520574fafa4f965872b106b49b5c90743c`. The origins gate passed preparation,
including 797 UI tests, 647 producer tests, the library/archive/backup suites and
zero Go lint issues. Its Go run passed every package except one migration test
whose old four-column expectation omitted the new nullable origin column. That
assertion now verifies the preserved values and absent historical origin. Its
focused check and the complete resumed lint/backend gate passed; the latter
completed in 2,046.3 seconds with every Go package successful.
Production remains compatible; the latest populated verified
release is schema 1000097. The manual gallery-dl entry point, rebuilt application
and host/n8n/backup runtimes, final migration rehearsal, coordinated backups and
coherent cutover still remain.

## Durable manual gallery-dl requests — 2026-10-08

`stash-gallery-dl` now accepts explicit URLs and input files, including `ytdl:`
selection, through the native registration and download services. It retains
the first invocation's inputs, profile policies and configured-scan time before
mutations. Source and metadata-policy receipts recover lost responses before
any runnable call is queued. Existing source definitions, policies and disabled
state remain authoritative; new mixed collections do not infer account or
performer ownership. Only newly registered sources receive metadata defaults.

Foreground work uses admitted run IDs belonging to that invocation, normal
leases, shared provider pacing and the durable file outbox. Queue-only, explicit
resume, bounded waiting and local dry preview are supported. A completed source
scan does not claim completed file intake. The two reviewed adapter profiles
share one verified root and publication lock; `ytdl:` remains an extractor
selector while native requests and retained source URLs use the actual HTTP URL.

The complete integrated repository gate passes: 797 UI tests, 657 producer tests,
12 library tests, 125 archive tests, 304 backup tests, all Go packages and zero
Go lint issues. The real HTTP manual test recovers lost registration/policy/finish responses,
preserves an existing owner's choices, delivers actual file receipts and restores
all four saved registration files. A disposable installed wheel matches all
125 current modules and produces identical profile hashes. The staged host
launcher passes help/dry-preview and incomplete/private-configuration checks.
Conversion of the actual global configuration preserves Coomer/Kemono originals;
its backup inspection resolves 29 dependencies and the manual request state tree.

The manual changes are integrated and verified. The existing live `gallery-dl`
symlink and scraper configuration remain unchanged. Final installed runtimes,
all host/n8n policy hashes, dispatcher
selection, manual state provisioning and the combined backup/deployment manifest
must be refreshed before the coordinated writer handoff.

## Final schema-99 artifacts and cloud recovery — 2026-10-08

[The release verification report](native-schema99-release-verification.json)
records the final application image at `0ea2c05fb` and producer/backup packages
at `d863a41af`. The intervening manual increment changes producer code and
application tests, with no application implementation change. All application
CI checks and the complete local repository gate passed.

The populated migration preserves all 40,513,488 existing typed rows. Integrity
and foreign-key checks pass. The exact published wrapper starts the migrated
library in 173.9 seconds and restarts it in 232.4 seconds, preserving all 309
tables, saved filter state and the complete database bytes. Scene, image,
gallery and performer counts agree through the live isolated API.

The final installed packages match the selected source. All 32 worker profiles
preserve the reviewed scraper settings, including Coomer/Kemono originals.
The combined inactive selection contains 77 files and nine verified n8n workflow
graphs. Its four Quadlets and 16 generated/native service and timer definitions
pass validation. Both manual profiles, their shared outbox and durable request
state are included in the backup dependency inventory. No production launcher,
workflow or service has switched to these artifacts.

A real S3 rehearsal using schema 99 and the final installed backup package
published approximately 103 MiB in 23 compressed objects, restored the database,
frozen host ledgers and both producer queues, and recovered pending/lost-response
deliveries without duplicates. After restarting the publisher, unchanged data
required zero chunk HEADs, zero chunk tag reads and zero PUTs; only one inventory
page and bounded metadata checks were needed. This verifies final-code cloud
recovery, not a full current production backup or daily metadata churn.

The daily backup user now has the missing `s3:GetBucketVersioning` permission
scoped to the metadata bucket. The reviewed metadata lifecycle is installed and
verified by the final backup runtime. Existing user policies and the media bucket
are unchanged; metadata bucket versioning remains disabled. Retention expires
explicitly retired native metadata while preserving the configured retained
snapshots and their shared objects. Setup tagged or deleted no objects.

The completed schema-96 load-test copy was reclaimed, freeing about 21 GiB while
retaining its receipts and both migration baselines. The final schema-99 load
measurement and complete row comparison use the original performance budgets
and preserve at least 50 GiB of free space. The historical media audit continues
in its original runtime. Current full backup verification, a fresh common
production boundary, coherent writer activation, scheduled-cycle verification
and owner acceptance remain before merging `develop`.

The final load check subsequently passed with 344 captures, 24 completed file
jobs and 180 concurrent browsing requests. During ingestion, scene/image/performer
p95 response times were 19.4/40.9/22.6 ms, below the unchanged 250 ms budget.
Every one of the schema-99 baseline's 40,513,490 rows survived with identical
values and SQLite types. The comparison included committed WAL pages; the schema
was unchanged and there were no foreign-key violations. Its disposable database copy was then
reclaimed, leaving approximately 82 GiB free. The two migration baselines and
all release/performance receipts remain available.

## Production imports and native backup recovery — 2026-10-08

All 1,711 production catalog snapshots completed their 20,532 import phases.
Their immutable receipts retain 78 catalogs requiring relation review and 81
requiring media review; these are preserved review outcomes, not guessed links.
The 925,869-record automation snapshot has been uploaded, and its queues and
checkpoints are being mapped. Independent reconciliation, post/media matching,
source albums and policy handoff still gate public startup.

The live backup ledger now includes the already-published cold-media receipts:
274,466 video paths, 513,333 image-file entries and 283,634 media objects. The
handoff held the backup lock, verified the original live files against the held
snapshot, checked every prepared manifest object against its local receipt, and
retained the previous ledger and footprints. Other ledgers were unchanged. It
made no cloud requests or master-manifest publication. Its first preflight
stopped before mutation on unused inspection sidecars; the corrected check
requires no open handles and an empty WAL. A complete coordinated native backup
and isolated restore are still required.

The last backup recovery helper depended on `scrape_catalog.cli` to check the
old catalog root. The tracked recovery unit now requeues the regular native
backup service directly, guarded by the mounted media dataset, interrupted-run
marker and native configuration. The existing boot/retry cadence is preserved.
Both unit files are now included in backup coverage. Host systemd validation
passes, and the inactive deployment verifies 80 files, 49 static backup
components, 141 dynamic components and 32 worker profiles. Both recovery units
remain held; no worker/application binary was rebuilt for this configuration
change.

The remote `v2.5-compatible-final` tag and all eight frozen image tags were
reverified against their recorded commit/digests. No tag or image changed.
Discovery recovery, final inventory, the closed SSD database move, public
startup and actual n8n publication verification have prepared guarded drivers;
none of those handoffs has run yet. The private native API remains active while
public Stash and scraper schedules remain stopped. See the updated
[production verification report](native-production-cutover-verification.json).

All automation phases subsequently completed. Independent reconciliation now
verifies 5,937,272 retained source records across 7,127 chunks, including every
catalog record family and the original automation values and SQLite types.
All 8,290,833 original library rows are accounted for; integrity and foreign-key
checks pass. The comparison initially stopped on two URLs added by the requested
captured-profile-link feature. Both additions are backed by explicit account
ownership, stable source IDs and the original captured profile records. The
associated two performer timestamps match their publisher decisions. All 1,510
original performer URLs, 1,941 performer names and other original values remain
unchanged. The failed comparison was retained and the verifier was corrected to
check this specific evidence; no data was reverted or imports replayed.

The six dependent domain services resumed only after that comparison passed.
Post/media previews cover a scope of 261,642 posts, with independent preview and
receipt checks before source albums and the 1,718 disabled policy definitions
are applied. Source registration and operational recovery remain gated on these
steps; the public application and scraper schedules are still held.

All 261,642 post previews then passed independent comparison: 423,849 media
candidates are backed by 427,207 valid file proofs. Applying those associations
is in progress. A coordinated native backup and isolated restore now explicitly
precede the public switch. The queued backup controller waits for all domain and
policy checks, captures the private API with producers still stopped, and uses
the already-verified media ledger without cold-media compaction or cleanup.
The installed production backup configuration is unchanged. A fresh isolated
ZFS probe verified read-only capture, retained original bytes, replay and release;
only its disposable dataset was removed. No live-root activation, credential
provisioning, database promotion or public startup has occurred. A fresh backup
after controlled ingestion still gates resuming scraper schedules.

Post/media application and independent receipt reconciliation completed on
October 9 (UTC): all 261,642 post receipts, 423,849 selected associations and 427,207
file proofs match their saved plans. There were no review or unavailable
outcomes in this matching scope. Gallery previews then checked all 5,131 posts
with retained attachment selections; 2,857 qualify for new source galleries
and 2,274 are ineligible. Applying that plan is in progress, with all 1,340
original galleries and 505,653 image memberships included in preservation
checks. Policies, source registration and the pre-public backup/restore remain
gated on those checks.

Controlled host, n8n and direct-file intake checks are prepared without starting
scrapes. The selected host scan retains its original publication window; the
n8n check uses an existing interrupted backfill and the stored workflow, with
accepted completion and skip decisions preserved. Verification requires actual
source completion, matching producer/native event digests, completed file jobs,
live media associations and independently checked sample bytes. The n8n launch
records intent before its request and can recover the actual execution after a
lost response without submitting another workflow. These checks still require
the coordinated backup/restore and verified public handoff.

Gallery application and preservation checks subsequently passed: 2,857 source
galleries contain 7,669 memberships, while every original gallery and membership
remains intact. All 1,718 migrated policy definitions are verified and disabled.
All 3,106 source URLs and their policy definitions are registered under the
disabled root, with existing definitions unchanged and no source jobs activated.
The six local-policy operations and 792 operational recovery scopes are prepared;
the eight scopes requiring review remain identified separately. All nine
prerequisites for the pre-public backup now pass.

The first backup stopped before capturing components or publishing to S3 because
two opaque component labels retained `@` from systemd template filenames. The
backup format permits only portable name characters. Their labels now use
`-template.` while preserving the original file paths and contents. Validation
covers all 189 effective components from the 49 static and 141 dynamic inputs.
The publisher fenced and abandoned the unsealed checkpoint, retained its
identity and failure evidence, and cleared its active pointer. The corrected
retry has sealed its coordinated checkpoint and is continuing the backup.
The final installed-file verifier now checks this
one-file configuration overlay and validates component names, roles and
uniqueness. Runtime implementations and public-writer barriers are unchanged;
coordinated publication and isolated restore are still pending.

Read-only checks against the populated private API also passed: 18 requests
verified all 4,197 galleries are discoverable and sampled empty, image, video
and mixed source albums against their SQLite memberships. The source routes
preserve attachment order across pagination. The slowest sampled request took
10.6 ms. This verifies the gallery data path; rendered desktop/mobile owner
acceptance still follows the public handoff.

A selected metadata-only execution check is prepared with separate snapshot,
admission, execution and verification phases. Its read-only preservation check
ran against one existing scene, 14 related tables and the actual file bytes.
Later execution must produce a native capture/publication while preserving
library fields, relationships and media bytes. A due Twitter target was chosen;
the retained future Reddit retry deadlines remain unchanged. No source fetch or
metadata-job admission has occurred.

Runtime inspection found n8n's external Code-node runner must accompany the main
process during controlled startup. Its installed image matches n8n 2.42.4; both
images contain the same 2.42.3 task-runner package. A configuration overlay is
prepared to pin that existing image, include its service definition and the
existing forward-auth hook in coordinated backups, and pause the runner along
with n8n/Redis during future capture. Exact publisher deduplication validates
191 effective components from 51 static and 141 dynamic inputs. The overlay,
updated final inventory check and three-container startup are prepared only;
installation requires the current pre-public backup/restore to finish. The
sealed run and its configuration remain unchanged. Scheduled native workers
are confirmed inactive, and the public application remains off.

The native translation worker had another deployment dependency: the selected
container lacked the `trans` executable. The application image and all three
native wrapper variants now install Translate Shell and exercise its interpreter
dependencies during the build. Both repository changes are pushed. Wrapper
build 37871713513 published the variants against the existing verified Stash
source digest; the selected Debian wrapper contains the exact same application
binary as the current snapshot validator. A real sample translation passed
using the backend's exact arguments. Fresh startup, restart and native snapshot
validation also passed with translation enabled in an isolated container with
no external network or production mounts.

The new wrapper is prepared as a one-file change to the inactive Stash service
definition. Its installer requires the current backup and isolated restore to
pass, starts no services and leaves production translation disabled. The
private handoff, public startup and installed-file verifier now require this
overlay while retaining the original release evidence. No running application,
database, producer credential or sealed backup has changed.

The schedule audit accounts for all ten previously active timers: seven retain
their original cadence, and the three catalog timers are replaced by native
workers. The three new native worker timers remain inactive and disabled.
The home-directory backup still needs a bounded caller update: it references
the old Stash backup directory, incorrectly rejects the normal null result from
`backupDatabase(download: false)`, and its ordinary filters exclude native
producer state. Coordinated S3 capture already covers that state. The existing
home-backup timer and unrelated backup behavior remain unchanged while this
additional path is corrected.

The first coordinated backup continues packaging its sealed checkpoint. The
saved artwork source contains 240,872 files and approximately 36.9 GB, in addition
to the 22.2 GB native database. This is a substantial initial local packaging
step; it is not a completed cloud publication or restore. The original owner
and publisher remain alive with increasing I/O, and system disk free space
remains above the required 50 GiB reserve. Post-write backup/restore, actual
scheduled-cycle checks, retirement and owner acceptance remain outstanding.

The home-backup repair is now prepared and tested, but not installed. It reads
the existing private native backup key file, accepts the successful null API
response, and requires a new native snapshot before upload. The three latest
validated native snapshots are staged with hard links on `/tank`, avoiding a
second full database allocation on the system disk. Only native snapshot names
are eligible for retention, after both existing syncs succeed; changed or newly
created snapshots prevent pruning. Existing legacy backups remain untouched.

The installed worker inventory supplies 15 download archives, two producer
outboxes and two intake/dedupe databases. All 19 were snapshotted successfully
using the SQLite backup API, with committed WAL included, standalone DELETE
journal mode and unchanged source hashes. The 44.1 MB of temporary copies were
removed after verification. Eight fixture tests also cover API errors and
redirects, native schema validation, hard-link staging, retention races and
real rclone routing with the original user filters. A full empty schema99
database passed the native staging check. These home copies remain individually
consistent supplementary backups; the coordinated S3 bundle supplies complete
archive recovery. Actual scheduled capture and cloud sync still need verification.

Five immutable helper/filter resources will be installed first. Only the native
backup configuration and existing home-backup entrypoint change, with the
entrypoint replaced atomically last. Original user filters, the older snapshot
helper, client configuration and timer remain in use or unchanged. The installer
requires the pre-public restore and n8n dependency overlay, takes both backup
locks, and starts no service. Its 11 added static backup paths bring the final
planned inventory to 62 static inputs and 202 effective components, validated
with the installed publisher. Final inventory verification requires this
overlay together with the n8n and translation overlays. The current sealed
backup still uses its original configuration and is not changed by preparation.

The remaining worker/schedule steps are now prepared separately. Translation
startup changes only its enable flag and restarts the verified native image
after controlled ingestion checks. Its read-only verifier requires a newly
captured provider result, a succeeded native job and matching frozen target
history, post evidence, deadlines and priorities. Six focused fixture cases
pass, including rejection of a migrated cache masquerading as a new provider
call. No production setting or translation target has changed.

The post-controlled backup driver uses the installed publisher and final
configuration, including all three n8n runtime dependencies in capture. It
requires actual host/n8n/manual/enrichment/translation completion proofs, then
publishes and restores into a new isolated directory with media compaction and
remote cleanup deferred. Any interrupted sealed generation must be recovered
from its original identity. The driver is prepared, not executed.

Later schedule resumption preserves all seven existing timer definitions and
adds the three native worker timers. Nine obsolete unit definitions will first
be retained and reversibly masked so their retirement does not depend on the
temporary cutover sentinel. The other 15 schedule/service barriers are explicitly
accounted for. Each n8n parent uses its exact saved published version and a
checksum-guarded REST request; an uncertain response retains intent and cannot
repeat the request. Verification checks the actual published-version table and
completed publication outbox. Syntax, timer hashes and the full 28-barrier
partition passed preparation checks; no unit, timer or workflow was changed.
Starting these timers will still require observing their real execution results.

The populated private application now passes 27 rendered checks in desktop
Chromium and mobile WebKit. Four gallery samples cover unavailable, image,
video and mixed albums. Displayed source positions and media links match their
API responses. The mobile section picker works, album cards scroll above the
fixed footer, and all eight native archive destinations appear in the mobile
navigation drawer. Those destinations load without API or application errors;
source-order sections also expand and collapse with the keyboard.

The browser checks allowed only reads, including the audited batch POST that
reads download status. The database commit counter, file identities, sizes and
modification times remained unchanged; the complete WAL checksum also matched.
WebKit's diagnostic about the unsupported `interactive-widget` viewport option
is recorded separately. Earlier test-guard failures and their corrections are
retained. These checks do not replace owner acceptance or exercise mutations.
The original sealed backup continues packing artwork with over 116 GiB free;
its publication and isolated restore remain prerequisites for live handoff.

A further host dependency audit found an important launch-path omission:
Instagram's existing `journal.conf` drop-in still replaces the newly installed
native service command with the old catalog scraper. The service is inactive
behind its cutover barrier. n8n also retains an unused mount of the old NFO
translation hook. Neither issue is resolved merely by checking the main unit
file, so the prepared schedule step now checks ten effective systemd commands
after reload and before opening any timer barriers.

A separate cleanup is prepared for after the first restore and the n8n/home
backup overlays. It neutralizes that Instagram override, removes the unused
mount and dormant global catalog processors, routes three existing manual
backup/restore commands through the installed native runtime, and retires two
old catalog/translation commands. Original bytes are retained before replacement;
replacing the command symlink leaves its repository target intact. All 32 native
profiles and 79 private configuration references remain unchanged. Active and
held workflow graphs do not reference the retired commands.

Ten filesystem/command checks and five actual CLI checks passed. Host and selected
n8n producer environments also pass without the external catalog package. The
prepared final backup inventory now contains 69 static inputs and 209 effective
components, including the original launch/config bytes. Final inventory, n8n
startup, runtime verification and post-controlled backup require this cleanup.
No installed file, service, schedule or current sealed backup has changed.
