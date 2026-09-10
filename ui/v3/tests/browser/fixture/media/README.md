Synthetic 160×90 slate-gray AVC video with a 440 Hz AAC audio track. No library
media is used. Generate `audio.mp4` and the full fMP4 HLS playlist from this
directory with:

```sh
ffmpeg -f lavfi -i color=c=slategray:s=160x90:r=30:d=12 \
  -f lavfi -i sine=frequency=440:sample_rate=48000:duration=12 \
  -c:v libx264 -profile:v main -pix_fmt yuv420p -bf 0 -g 60 -keyint_min 60 \
  -sc_threshold 0 -c:a aac -b:a 32k -movflags +faststart audio.mp4
ffmpeg -i audio.mp4 -c copy -hls_time 2 -hls_list_size 0 \
  -hls_segment_type fmp4 -hls_playlist_type vod \
  -hls_segment_filename hls/segment-%d.m4s hls/stream.m3u8
ffmpeg -i audio.mp4 -t 2 -c copy -movflags +faststart short.mp4
```

`clip/stream.m3u8` uses segments 3 and 4 from the same file, retaining their
timestamps and setting `MEDIA-SEQUENCE:3` to exercise a trimmed marker playlist.
The fixture uses absolute URLs and Stash's `stream.m3u8` path convention, as the
real GraphQL stream list does. `?start=` configures the player's initial segment;
`?end=` selects the clip's relative timeline.

The `?short` source fixture alternates the two-second `short.mp4` with the
four-second marker playlist. Its autoplay test waits for natural EOF without
seeking to the last frames, which can stall WebKit's native decoder.

Tests use the production `SceneVideo` and `CanPlayEffect`. Source/engine changes
use the existing metadata pre-positioning and explicit resume, as in
`useScenePlayerSources`. Clip seeks and `source.engine.hlsJs.startPosition` use
the relative clip timeline; full-scene HLS starts use the source URL's scene time.
They check DOM identity, progressing playback, start positions, mute preservation
and repeated natural `ended` transitions. The fixture uses no B-frames to keep decoded-frame
timestamps near the requested segment boundary; an initial buffer gap can leave
WebKit waiting at the seek target. Headless WebKit does not establish physical
iPhone autoplay policy or ManagedMediaSource behavior.
