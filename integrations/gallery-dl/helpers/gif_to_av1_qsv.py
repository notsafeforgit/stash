#!/usr/bin/env python3
import argparse
import json
import os
import shutil
import subprocess
import tempfile
import sys
from pathlib import Path


def eprint(*args, **kwargs):
    print(*args, file=sys.stderr, **kwargs)


def run_capture(cmd: list[str]) -> subprocess.CompletedProcess:
    return subprocess.run(
        cmd,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        check=False,
    )


def preserve_source_times(
    gif_path: Path,
    out_path: Path,
    src_stat: os.stat_result,
    dry_run: bool,
) -> bool:
    if dry_run:
        print(
            f"PRESERVE TIMES {gif_path} -> {out_path} "
            f"(atime_ns={src_stat.st_atime_ns}, mtime_ns={src_stat.st_mtime_ns})"
        )
        return True

    try:
        os.utime(out_path, ns=(src_stat.st_atime_ns, src_stat.st_mtime_ns))
        print(
            f"PRESERVED times on {out_path} "
            f"(mtime_ns={src_stat.st_mtime_ns})"
        )
        return True
    except OSError as exc:
        eprint(f"Failed to preserve timestamps on {out_path}: {exc}")
        return False


def verify_tools(ffmpeg_bin: str, ffprobe_bin: str) -> None:
    if shutil.which(ffmpeg_bin) is None:
        raise SystemExit(f"ffmpeg binary not found: {ffmpeg_bin}")

    if shutil.which(ffprobe_bin) is None:
        raise SystemExit(f"ffprobe binary not found: {ffprobe_bin}")

    probe = run_capture([ffmpeg_bin, "-hide_banner", "-encoders"])
    if probe.returncode != 0:
        raise SystemExit(
            "Failed to query ffmpeg encoders.\n"
            f"stdout:\n{probe.stdout}\n\nstderr:\n{probe.stderr}"
        )

    encoders_text = probe.stdout + "\n" + probe.stderr
    if "av1_qsv" not in encoders_text:
        raise SystemExit(
            "Your ffmpeg build does not expose av1_qsv.\n"
            "You need an ffmpeg build with Intel QSV AV1 encode support."
        )


def find_gifs(src: Path, recursive: bool) -> list[Path]:
    candidates = src.rglob("*") if recursive else src.iterdir()
    gifs = [p for p in candidates if p.is_file() and p.suffix.lower() == ".gif"]
    return sorted(gifs)


def gather_inputs(src: Path, recursive: bool) -> tuple[list[Path], Path]:
    if not src.exists():
        raise FileNotFoundError(f"Source does not exist: {src}")

    if src.is_file():
        if src.suffix.lower() != ".gif":
            print(f"Source is not a GIF, nothing to do: {src}")
            return [], src.parent
        return [src], src.parent

    if src.is_dir():
        return find_gifs(src, recursive), src

    raise FileNotFoundError(f"Source is neither a file nor a directory: {src}")


def output_path_for(src_root: Path, gif_path: Path, dest_root: Path | None) -> Path:
    if dest_root is None:
        return gif_path.with_suffix(".mkv")

    rel = gif_path.relative_to(src_root)
    return (dest_root / rel).with_suffix(".mkv")


def build_ffmpeg_cmd(
    ffmpeg_bin: str,
    input_path: Path,
    output_path: Path,
    quality: int,
    compression_level: int,
    overwrite: bool,
    device: str | None,
) -> list[str]:
    cmd = [ffmpeg_bin, "-hide_banner", "-loglevel", "warning"]
    cmd.append("-y" if overwrite else "-n")

    if device:
        cmd += [
            "-init_hw_device", f"qsv=hw,child_device={device}",
            "-filter_hw_device", "hw",
        ]

    cmd += [
        "-i", str(input_path),
        "-map", "0:v:0",
        "-an",
        "-sn",
        "-dn",
        "-vf", "pad=ceil(iw/2)*2:ceil(ih/2)*2,format=nv12",
        "-c:v", "av1_qsv",
        "-global_quality", str(quality),
        "-compression_level", str(compression_level),
        "-f", "matroska", str(output_path),
    ]
    return cmd


def verify_output_with_ffprobe(out_path: Path, ffprobe_bin: str) -> tuple[bool, str]:
    if not out_path.exists():
        return False, f"output does not exist: {out_path}"

    try:
        st = out_path.stat()
    except OSError as exc:
        return False, f"could not stat output: {out_path} ({exc})"

    if st.st_size <= 0:
        return False, f"output is empty: {out_path}"

    cmd = [
        ffprobe_bin,
        "-v", "error",
        "-print_format", "json",
        "-show_streams",
        "-show_format",
        str(out_path),
    ]
    proc = run_capture(cmd)
    if proc.returncode != 0:
        return False, f"ffprobe failed for {out_path}: {proc.stderr.strip() or proc.stdout.strip()}"

    try:
        data = json.loads(proc.stdout)
    except json.JSONDecodeError as exc:
        return False, f"ffprobe returned invalid JSON for {out_path}: {exc}"

    streams = data.get("streams", [])
    if not isinstance(streams, list) or not streams:
        return False, f"ffprobe found no streams in {out_path}"

    video_streams = [s for s in streams if s.get("codec_type") == "video"]
    if not video_streams:
        return False, f"ffprobe found no video stream in {out_path}"

    v0 = video_streams[0]
    codec_name = v0.get("codec_name")
    width = v0.get("width")
    height = v0.get("height")

    if codec_name != "av1":
        return False, f"video stream is not AV1 in {out_path} (codec_name={codec_name!r})"

    if not isinstance(width, int) or width <= 0 or not isinstance(height, int) or height <= 0:
        return False, f"video stream dimensions invalid in {out_path} (width={width}, height={height})"

    fmt = data.get("format", {})
    if isinstance(fmt, dict):
        size_str = fmt.get("size")
        if size_str is not None:
            try:
                if int(size_str) <= 0:
                    return False, f"container size reported as zero in {out_path}"
            except (TypeError, ValueError):
                pass

    return True, f"verified AV1 video stream ({width}x{height})"


def cleanup_bad_output(out_path: Path) -> None:
    try:
        if out_path.exists():
            out_path.unlink()
            print(f"REMOVED bad output: {out_path}")
    except OSError as exc:
        eprint(f"Failed to remove bad output {out_path}: {exc}")


def sync_directory(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY)
    try: os.fsync(fd)
    finally: os.close(fd)


def delete_source_file(gif_path: Path, dry_run: bool) -> bool:
    if dry_run:
        print(f"  would delete source: {gif_path}")
        return True

    try:
        gif_path.unlink()
        sync_directory(gif_path.parent)
        print(f"DELETED source: {gif_path}")
        return True
    except FileNotFoundError:
        print(f"Source already gone: {gif_path}")
        return True
    except OSError as exc:
        eprint(f"Failed to delete source GIF {gif_path}: {exc}")
        return False


def transcode_one(
    ffmpeg_bin: str,
    ffprobe_bin: str,
    gif_path: Path,
    out_path: Path,
    quality: int,
    compression_level: int,
    overwrite: bool,
    dry_run: bool,
    device: str | None,
    delete_source: bool,
) -> bool:
    out_path.parent.mkdir(parents=True, exist_ok=True)

    try:
        src_stat = gif_path.stat()
    except FileNotFoundError:
        print(f"SKIP missing source: {gif_path}")
        return True
    except OSError as exc:
        eprint(f"Failed to stat source GIF {gif_path}: {exc}")
        return False

    if out_path.exists() and not overwrite and not delete_source:
        ok, msg = verify_output_with_ffprobe(out_path, ffprobe_bin)
        if not ok: eprint(msg)
        return ok

    # A legacy destination may be a playable but incomplete prior encode.
    # When deleting the GIF, produce a fresh, complete output before deletion.
    if dry_run:
        temporary = out_path.with_name('.' + out_path.name + '.part')
    else:
        fd, name = tempfile.mkstemp(prefix='.' + out_path.name + '.', suffix='.part', dir=out_path.parent)
        os.close(fd); temporary = Path(name)
    cmd = build_ffmpeg_cmd(
        ffmpeg_bin=ffmpeg_bin,
        input_path=gif_path,
        output_path=temporary,
        quality=quality,
        compression_level=compression_level,
        overwrite=True,
        device=device,
    )

    print(f"ENCODE {gif_path} -> {out_path}")

    if dry_run:
        print("  " + subprocess.list2cmdline(cmd))
        print(f"  would preserve source times from: {gif_path}")
        if delete_source:
            print(f"  would verify with ffprobe, then delete source: {gif_path}")
        return True

    proc = subprocess.run(cmd, check=False)
    if proc.returncode != 0:
        eprint(f"FAILED ({proc.returncode}): {gif_path}")
        cleanup_bad_output(temporary)
        return False

    ok, msg = verify_output_with_ffprobe(temporary, ffprobe_bin)
    if not ok:
        eprint(f"Verification failed for {out_path}: {msg}")
        cleanup_bad_output(temporary)
        return False

    print(f"VERIFY OK {out_path}: {msg}")

    if not preserve_source_times(
        gif_path=gif_path,
        out_path=temporary,
        src_stat=src_stat,
        dry_run=False,
    ):
        return False

    if (gif_path.stat().st_size, gif_path.stat().st_mtime_ns, gif_path.stat().st_ctime_ns) != (src_stat.st_size, src_stat.st_mtime_ns, src_stat.st_ctime_ns):
        cleanup_bad_output(temporary)
        eprint("Source changed during conversion; source retained")
        return False
    with temporary.open('rb') as file: os.fsync(file.fileno())
    os.replace(temporary, out_path)
    sync_directory(out_path.parent)
    if delete_source:
        if not delete_source_file(gif_path, dry_run=False):
            return False

    return True


def parse_args() -> argparse.Namespace:
    p = argparse.ArgumentParser(
        description="Convert GIFs in a folder or a single GIF to AV1 MKV using Intel QSV (av1_qsv)."
    )
    p.add_argument("source", help="Source folder containing GIFs, or a single GIF file")
    p.add_argument(
        "--dest",
        help="Optional destination root folder for MKVs. If omitted, MKVs are written next to the GIFs.",
    )
    p.add_argument(
        "--recursive",
        action="store_true",
        help="Recurse into subdirectories when source is a directory",
    )
    p.add_argument(
        "--ffmpeg",
        default="ffmpeg",
        help="Path to ffmpeg binary (default: ffmpeg)",
    )
    p.add_argument(
        "--ffprobe",
        default="ffprobe",
        help="Path to ffprobe binary (default: ffprobe)",
    )
    p.add_argument(
        "--device",
        default=None,
        help="Optional QSV device render node, e.g. /dev/dri/renderD128",
    )
    p.add_argument(
        "--quality",
        type=int,
        default=28,
        help="QSV global quality. Lower = better quality / larger files. Default: 28",
    )
    p.add_argument(
        "--compression-level",
        type=int,
        default=4,
        help="QSV speed/quality tradeoff. Higher = faster / worse quality. Default: 4",
    )
    p.add_argument(
        "--overwrite",
        action="store_true",
        help="Overwrite existing MKVs",
    )
    p.add_argument(
        "--delete-source",
        action="store_true",
        help="Delete the source GIF after successful ffmpeg encode and ffprobe validation, or if a valid AV1 MKV already exists",
    )
    p.add_argument(
        "--dry-run",
        action="store_true",
        help="Print what would be done without encoding",
    )
    return p.parse_args()


def main() -> int:
    args = parse_args()

    src = Path(args.source).expanduser().resolve()

    dest_root = None
    if args.dest:
        dest_root = Path(args.dest).expanduser().resolve()
        dest_root.mkdir(parents=True, exist_ok=True)

    if not (1 <= args.quality <= 51):
        eprint("--quality must be between 1 and 51")
        return 2

    if args.compression_level < 0:
        eprint("--compression-level must be >= 0")
        return 2

    verify_tools(
        ffmpeg_bin=args.ffmpeg,
        ffprobe_bin=args.ffprobe,
    )

    try:
        gifs, src_root = gather_inputs(src, args.recursive)
    except FileNotFoundError as exc:
        eprint(str(exc))
        return 2

    if not gifs:
        print("No GIF files found.")
        return 0

    print(f"Found {len(gifs)} GIF(s).")

    ok = 0
    failed = 0

    for gif_path in gifs:
        out_path = output_path_for(src_root, gif_path, dest_root)
        success = transcode_one(
            ffmpeg_bin=args.ffmpeg,
            ffprobe_bin=args.ffprobe,
            gif_path=gif_path,
            out_path=out_path,
            quality=args.quality,
            compression_level=args.compression_level,
            overwrite=args.overwrite,
            dry_run=args.dry_run,
            device=args.device,
            delete_source=args.delete_source,
        )
        if success:
            ok += 1
        else:
            failed += 1

    print(f"Done. ok={ok} failed={failed}")
    return 1 if failed else 0


if __name__ == "__main__":
    raise SystemExit(main())
