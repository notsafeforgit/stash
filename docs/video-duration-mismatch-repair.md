# Video Duration Mismatch Repair Notes

Use the v3 `duration_mismatch` scene filter to build a repair queue. Do not
replace originals in-place; write repaired files to a new path, rescan, verify
duration/frame behavior, then replace manually if the result is good.

## 1. Try a lossless remux first

This rebuilds container/index/timestamps without re-encoding.

```bash
ffmpeg -v warning -fflags +genpts -i "$in" -map 0 -c copy -movflags +faststart "$out.mp4"
```

For MKV output:

```bash
ffmpeg -v warning -fflags +genpts -i "$in" -map 0 -c copy "$out.mkv"
```

## 2. Fix reserved H.264/HEVC color metadata

Use this for files where ffmpeg reports `Invalid color space` and ffprobe shows
reserved color metadata.

H.264:

```bash
ffmpeg -v warning -i "$in" -map 0 -c copy \
  -bsf:v h264_metadata=colour_primaries=1:transfer_characteristics=1:matrix_coefficients=1 \
  "$out.mp4"
```

HEVC:

```bash
ffmpeg -v warning -i "$in" -map 0 -c copy \
  -bsf:v hevc_metadata=colour_primaries=1:transfer_characteristics=1:matrix_coefficients=1 \
  "$out.mp4"
```

## 3. Re-encode only if the stream is actually truncated/corrupt

This is lossy, but can salvage the decodable portion when remuxing cannot fix
the file.

```bash
ffmpeg -v warning -err_detect ignore_err -i "$in" \
  -map 0:v:0 -map 0:a? \
  -c:v libx264 -crf 18 -preset slow \
  -c:a copy -shortest \
  "$out.mp4"
```
