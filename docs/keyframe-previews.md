# Keyframe scrub previews

The coarse scrub tier shows the source keyframe at or before the finger
position while a drag moves quickly. The exact-frame tier, the silent 240p
`stream.scrub.*` HLS session described in the
[player guide](../ui/v3/docs/player.md), is unchanged and replaces the still
once its frame decodes. All three routes use the scene's primary file, like
the scrub stream. The server implementation is
[keyframe_service.go](../pkg/ffmpeg/keyframe_service.go) with its index scan
and still extraction beside it; the routes are in
[routes_scene_scrub.go](../internal/api/routes_scene_scrub.go).

## Routes

### `GET /scene/{sceneId}/keyframes`

Returns the first video stream's keyframes (sync samples):

```json
{"version":"3f2a9c0d5e7b1a4c6d80","duration":1834.567,"times":[0,2.002,4.004]}
```

- `times` are ascending, unique keyframe times in [scene time](#scene-time),
  in seconds with millisecond precision. The first entry is normally 0.
  Keyframes before the container start, edit-list preroll (`D`) and corrupt
  (`C`) packets are omitted because a keyframe-only decode never displays
  them. An index of more than 50,000 keyframes (all-intra sources) is thinned
  to an evenly spaced subset; ordinary long-GOP files are never thinned.
- `version` is opaque. Clients use it as a cache key and send it back as `v`.
- `duration` is the library's file duration.
- `Cache-Control: private, no-cache` and `ETag: "<version>"`. A matching
  `If-None-Match` returns 304 without index work.
- 404: no primary file, the file is missing, or no decodable keyframes. 503
  with `Retry-After`: the scan timed out (600 seconds, the failure lifetime),
  or was abandoned or shut down (30 seconds). 503 without `Retry-After`: the
  stream manager is unavailable. Clients treat the index as optional and fall
  back to raw times.

### `GET /scene/{sceneId}/keyframe?t=<seconds>&v=<version>`

Returns `image/jpeg` of the keyframe whose time is the greatest ≤ `t` + 10 ms.
A `t` before the first keyframe returns the first keyframe.

- `X-Keyframe-Time: <seconds>` (three decimals, scene time) is the time of the
  returned frame. `Access-Control-Expose-Headers: X-Keyframe-Time` exposes it.
- With a cached index, `t` is resolved through it and the still is decoded at
  exactly that keyframe: the response is a function of `(version, t)`. When `v`
  equals the current version, the response has
  `Cache-Control: private, max-age=31536000, immutable`. A URL using an index
  time is the canonical URL for that keyframe.
- With a missing or stale `v` the current file's frame is served with
  `Cache-Control: no-cache`.
- Without an index, the request waits up to 250 ms for one (short files index
  in tens of milliseconds) and otherwise seeks directly. That frame's time is
  still exact, but some demuxers pick an earlier keyframe (Matroska with
  B-frames) or the following one (MPEG-TS), so the response is always
  `no-cache`. The started scan keeps running for later requests.
- `t` must be a finite number of seconds from 0 to 10⁷, otherwise 400. 404: no
  primary file or the file is missing. 503 with `Retry-After: 1`: the
  server-wide extraction limit is reached; back off. 500: extraction failed;
  fall back to the other tiers.

### `GET /scene/{sceneId}/keyframes/stills?t=<t1>,<t2>,…&v=<version>`

Returns the keyframes for up to 16 comma-separated times in one response, for
filling a thumbnail strip without a request per frame.

- Each time resolves through the index exactly as `/keyframe` does: the
  greatest keyframe ≤ `t` + 10 ms, or the first keyframe for an earlier `t`.
  Times may repeat or resolve to the same keyframe; each keyframe is extracted
  once.
- The body is `application/octet-stream`: the slots' JPEGs concatenated in
  request order, not a sprite sheet. `X-Keyframe-Times` lists each slot's
  keyframe time (three decimals, scene time) and `X-Keyframe-Lengths` each
  slot's byte length, both comma-separated and in request order.
  `Access-Control-Expose-Headers` exposes both. A slot whose keyframe could not
  be decoded has length 0 and the time of the index keyframe it resolved to.
- `Cache-Control: private, max-age=31536000, immutable` when `v` equals the
  current version and every slot has an image; otherwise `no-cache`.
- A batch needs the index. Without a cached one the request waits up to
  2 seconds for the scan, then returns 503 with `Retry-After: 2` while the
  scan continues. There is no index-less fallback as for `/keyframe`; use
  single stills meanwhile. 404: no primary file, the file is missing, or it
  has no decodable keyframes. 503 with `Retry-After: 600`: the scan timed out.
- 400: `t` is missing, lists more than 16 times, or any time is not a finite
  number of seconds from 0 to 10⁷. 503 with `Retry-After: 1`: the extraction
  limit is reached. 500: no slot could be decoded, including when the whole
  request exceeds 10 seconds.
- Stills already cached by either route are served without ffmpeg. The others
  are decoded by one ffmpeg process, which a cancelled request kills.

One process with one input per keyframe would keep every decoder alive until
it exits: about 3 GB for sixteen 4K HEVC frames. Batches instead read the file
through the concat demuxer, one short segment per keyframe, opened one after
another and decoded by a single keyframe-only decoder, so a batch needs about
the memory of one still. Each segment starts 131 ms before its keyframe's file
timestamp (scene time plus the container start), like ffmpeg's own `-ss`
allowance for B-frames, so MPEG-TS, which seeks by decode timestamp, does not
resume after the keyframe. It ends 2 ms after the keyframe. Indexed containers
resume at the preceding keyframe, which a `select` filter drops before scaling.
The `metadata` filter logs each frame's segment, so every image is matched to
its slot by its exact time; a frame more than 2 ms from its index time leaves
the slot empty.

Stills are 240p like the scrub stream and use its software sizing
(`ScaleMax` at the LOW tier): the shorter display edge is scaled to 240 pixels,
never upscaled, with an even longer edge. HDR (HLG/PQ) is tone mapped to SDR
with the same software conversion as the H.264 stream; for a single frame it
scales before tone mapping, which keeps 4K HDR stills cheap. Color tags come
from the library row, overridden by those the index scan observed. Software
decoding applies the display rotation.
Decoding is keyframe-only (`-skip_frame nokey`) with slice threading, without
hardware initialization or the configured live-transcode arguments. JPEG
quality is `-q:v 5`; a typical still is 3–15 KB.

## Scene time

Scene time is the timeline used by ffmpeg `-ss` input seeking: packet
presentation time minus the container start time (`format.start_time`, which
can be negative when audio priming precedes the video). The scrub stream's
`start=`, HLS segment seeks and "generate screenshot at time" all use input
`-ss`, so index times, still times and player times agree. The index scan and
the still extraction both derive their times this way; still extraction runs
with `-copyts` and reads the container start and decoded frame time from
ffmpeg's own log, so the reported time is the frame actually returned. Frame
times use showinfo's integer `pts` and time base: ffmpeg before 7.0 prints
`pts_time` with six significant digits, which loses milliseconds after
1000 seconds.

Tests generate MP4, Matroska (negative container start) and MPEG-TS (10-second
start offset, with and without audio) fixtures with a one-second GOP. They check
that each index time is the frame `ffmpeg -ss` decodes first, and that stills
return the correct keyframe and time. MPEG-TS has no seek index: input seeking
there resumes at the next keyframe, so only keyframe times are exact for it.

## Versions and caches

The cache key combines the file ID, `files.generation`, the live file size and
modification time, and an index revision. Generations advance when Stash
observes location or byte changes; the live stat also catches changes that a
scan has not observed. The public `version` is a truncated SHA-256 digest of
that key, the still pipeline revision and the stored color transfer, so it
reveals no internal keys and changes when stills would differ.

Indexes are built lazily, only when a scene is scrubbed; nothing backfills the
library. They are kept in a 64-entry memory LRU and in one SQLite file,
`<cache>/keyframes.sqlite` (with its WAL), managed by
[pkg/keyframecache](../pkg/keyframecache/store.go). An index is stored as
delta-encoded millisecond varints (about two bytes per keyframe) plus the
container start time and the stream's color tags. Canonical stills are stored
in the same file keyed by version and keyframe time; single and batch requests
share them. Indexes and stills have separate 64 MiB budgets,
each evicted least-recently-used, so the file stays at about 128 MiB. The file is
disposable: a corrupt or unreadable cache is deleted and recreated, and it is
safe to delete while Stash is stopped.

## Work limits

| Limit | Value |
| --- | --- |
| Concurrent index scans | 1; other files queue |
| Index scan timeout | 60 s; a failed or timed-out file is not rescanned for 10 minutes |
| Abandoned scan | cancelled 10 s after its last waiting request leaves |
| Index request wait | 2 minutes including queueing |
| Concurrent extractions | 3 server-wide; more are refused with 503 |
| Batch share | a batch counts as one extraction but cannot take the last free one, which stays available to single stills |
| Still extraction timeout | 5 s |
| Batch size | 16 times |
| Batch request timeout | 10 s, including up to 2 s waiting for the index |

The scan is `ffprobe -select_streams v:0 -show_entries packet=pts_time,dts_time,flags`:
nothing is decoded, but most containers still read every packet, so a long file
can take seconds. Concurrent requests for the same file share one scan, and still
requests during a drag rejoin it. Cancelling a request kills its ffmpeg process
group. Debug logging reports each scan and still with its ffmpeg time.

## Access

All three routes are library routes under the normal authentication middleware,
like `stream.scrub.*`. Shares use an explicit route allowlist that contains
neither the scrub stream nor these routes, and offline players have no server
URL. A signed playback URL signs the scene's `/scene/{id}/stream` prefix, which
covers `stream.scrub.*`; the keyframe paths of the same scene are verified
against that prefix too, so the player can keep the playback URL's query
parameters. A signature never covers another scene.

## Measurements

`go test ./pkg/ffmpeg -run '^$' -bench Keyframe` generates one-second-GOP
fixtures and measures the complete still extraction, including process start.
On a Ryzen 9 7950X3D with Fedora's ffmpeg 7.1 (about 65 ms of that is process
start-up): 1080p H.264 98 ms, 1080p AV1 94 ms and 2160p HEVC 10-bit PQ 141 ms
per still. Scanning a 60-second 1080p file takes 92 ms, mostly ffprobe start-up;
216,000 packets (two hours at 30 fps) take 0.6 s from the page cache. Scans of
large files are otherwise limited by read throughput.

A batch of 16 keyframes (`-bench KeyframeStills`) takes 210 ms for 1080p H.264
and 321 ms for 2160p HEVC 10-bit PQ, against 1.62 s and 2.67 s for the same 16
stills extracted one after another. On a two-hour MP4, whose sample tables each
segment reparses, 16 keyframes spread over the file take 0.26 s. Peak memory is
about 105 MB for a 1080p batch and 300 MB for a 4K HEVC batch, close to one
4K still (285 MB); one input per keyframe needed 460 MB and 3.2 GB.
