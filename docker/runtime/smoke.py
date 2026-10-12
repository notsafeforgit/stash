"""Exercise the published container without a GPU, network or existing library.

Run GPU encoding separately on the deployment host, where a real device exists.
This uses only ephemeral containers and tmpfs, never production volumes.
"""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import time
import uuid


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--engine", default="docker", choices=["docker", "podman"])
    parser.add_argument("--image", required=True)
    parser.add_argument("--binary", type=Path, required=True)
    args = parser.parse_args()

    def command(*parts, check=True):
        result = subprocess.run([args.engine, *parts], text=True, capture_output=True,
                                timeout=90, check=False)
        if check and result.returncode:
            raise RuntimeError(f"Container check failed: {parts[0]}\n{result.stdout}\n{result.stderr}")
        return result

    mounts = [part for path in ("/config", "/generated", "/cache", "/blobs", "/metadata", "/pip-install")
              for part in ("--tmpfs", path + ":rw,mode=1777")]
    base = ["--network=none", *mounts]

    def run(program, *, options=()):
        return command("run", "--rm", *base, *options, args.image, *program).stdout.strip()

    digest = hashlib.sha256(args.binary.read_bytes()).hexdigest()
    actual = run(["sha256sum", "/usr/bin/stash"]).split()[0]
    assert actual == digest, "Container binary differs from the validated Actions artifact"

    probe = ["python3", "-c", "import os,json,sys; print(json.dumps([os.getuid(),os.getgid(),os.getgroups(),sys.argv[1:]]))",
             "one argument with spaces", "*.literal"]
    result = json.loads(run(probe))
    assert result[:2] == [911, 911], result
    result = json.loads(run(probe, options=["-e", "PUID=12345", "-e", "PGID=12346", "-e", "AVGID=12347,12348"]))
    assert result[:2] == [12345, 12346] and {12347, 12348}.issubset(result[2]), result
    assert result[3] == probe[3:], result
    result = json.loads(run(probe, options=["--user", "12345:12346", "--group-add", "12347"]))
    assert result[:2] == [12345, 12346] and 12347 in result[2], result

    # Native startup must reject a legacy layout rather than open a new empty DB.
    invalid = command("run", "--rm", *base, "-e", "STASH_CONFIG_FILE=/root/.stash/config.yml",
                      args.image, "true", check=False)
    assert invalid.returncode != 0 and "Mount your configuration at /config" in invalid.stderr
    missing = command("run", "--rm", *base, "-e", "INSTALL_PYTHON_REQUIREMENTS=true",
                      args.image, "true", check=False)
    assert missing.returncode != 0 and "explicit nonempty" in missing.stderr
    forbidden = command("run", "--rm", *base, "--entrypoint", "sh", args.image, "-ec",
                        "touch /config/config.yml; chmod 600 /config/config.yml; exec /usr/local/bin/stash-entrypoint true",
                        check=False)
    assert forbidden.returncode != 0 and "readable and writable by 911:911" in forbidden.stderr

    run(["sh", "-ec", """
      ffmpeg -hide_banner -encoders > /tmp/encoders
      for encoder in h264_qsv h264_vaapi h264_nvenc; do grep -q "$encoder" /tmp/encoders; done
      ffmpeg -v error -f lavfi -i testsrc2=size=64x64:rate=1 -frames:v 1 -c:v libx264 /tmp/test.mp4
      ffprobe -v error -show_entries stream=codec_name -of csv=p=0 /tmp/test.mp4 | grep -qx h264
      vips black /tmp/test.png 16 16
      vips copy /tmp/test.png /tmp/test.avif
      vipsheader /tmp/test.avif
      avifenc --version
      avifgainmaputil help
      uv --version
      trans -no-init -version
    """])
    name = "stash-runtime-smoke-" + uuid.uuid4().hex[:12]
    try:
        command("run", "-d", "--name", name, *base, args.image)
        for _ in range(60):
            health = command("exec", name, "curl", "--fail", "--silent", "--max-time", "2",
                             "http://127.0.0.1:9999/healthz", check=False)
            if health.returncode == 0:
                break
            state = json.loads(command("inspect", name).stdout)[0]["State"]
            if not state["Running"]:
                raise RuntimeError(command("logs", name).stdout)
            time.sleep(1)
        else:
            raise RuntimeError("Native server did not become healthy\n" + command("logs", name).stdout)
        assert command("exec", name, "cat", "/proc/1/comm").stdout.strip() == "stash"
        command("stop", "--time", "15", name)
        state = json.loads(command("inspect", name).stdout)[0]["State"]
        assert state["ExitCode"] == 0, state
    finally:
        command("rm", "-f", name, check=False)
    print(json.dumps({"runtime": "native", "binary_sha256": digest, "checks": "passed"}))


if __name__ == "__main__":
    main()
