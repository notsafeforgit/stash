# Connect gallery-dl to Stash

Use `stash-gallery` to download into your Stash library with source posts,
captured metadata, account review and source albums. It is an optional worker
you run alongside Stash. It does not require n8n, a plugin, S3, old catalogs or
the original deployment's scripts.

The worker uses the existing Stash ingestion API and durable producer queue.
Stash owns metadata, associations, permissions and file verification; gallery-dl
owns website access and extraction. Downloaders never open Stash's database.

## 1. Prepare Stash and a shared media folder

Enable **Settings → Services → Source tracking** and restart Stash. Add or
choose an active folder in **Settings → Library**. That same folder must be
accessible to the worker. Container paths can differ: `/media/downloads` in
Stash might be `/srv/media/downloads` on the host. Both must represent the same
files, with matching relative paths and suitable read/write permissions.

Create an application API key in Stash's settings. Supply it through the
`STASH_API_KEY` environment variable for setup and source registration. For
example, in an interactive Bash shell:

```bash
read -r -s -p 'Stash API key: ' STASH_API_KEY
export STASH_API_KEY
```

The setup command saves a separate ingestion token scoped to the selected
folder. It never saves your application API key. Website cookies, tokens and
proxy settings stay in your local gallery-dl configuration.

## 2. Install the worker

Use Linux, Python 3.12 or newer, `uv`, Git and FFmpeg. Python's SQLite library
must include the [WAL-reset fix](https://sqlite.org/wal.html#walreset): SQLite
3.51.3 or newer, or the 3.50.7/3.44.6 maintenance backports. Check the interpreter
you intend to use, not the separate `sqlite3` command:

```sh
python3 -c 'import sqlite3; print(sqlite3.sqlite_version)'
```

From a checkout of this fork:

```sh
uv tool install --python python3 './integrations/gallery-dl[gallery]'
stash-gallery --help
```

The `gallery` extra installs the gallery-dl source revision and yt-dlp version
tested with this integration. Select another installed interpreter with
`--python /path/to/python` when needed. The original `gallery-dl` command remains
available; use `stash-gallery download` for native Stash tracking.

## 3. Connect one folder

List the existing folders to find the UUID of the one you want:

```sh
stash-gallery folders --endpoint https://stash.example
```

Then connect it, using the **worker's** path to that folder:

```sh
stash-gallery setup \
  --endpoint https://stash.example \
  --root FOLDER_UUID \
  --media-path /srv/media/downloads

stash-gallery check
```

Setup registers a producer, issues a folder-scoped ingestion token and creates
the local queue, download profiles and metadata defaults. It does not download
anything or start a schedule. `check` validates local profiles, pinned mounts,
the downloader runtime and Stash permissions without contacting source sites;
it also reports the Python SQLite version.

To use an existing gallery-dl JSON config, add
`--gallery-config /path/to/gallery-dl.json` to **setup**. Repeat that option for
ordered overrides. Defaults supply the media destination and a download archive;
your files override them. Website credentials are referenced from the original
files, not copied into the producer queue. Site naming, filters, originals
preferences and postprocessors remain yours. A configured output base outside
the selected folder is rejected. Relative paths resolve against the setup
directory, so use absolute paths in shared configs.

The default setup directory is `$XDG_STATE_HOME/stash-gallery`, or
`~/.local/state/stash-gallery`. To choose another durable local filesystem, put
`--directory /persistent/stash-gallery` **before the subcommand**, on every
invocation. Keep it outside the media folder and off network filesystems.
Do not reuse an existing deployment's worker directory. For multiple producer
processes writing the same media folder, follow the [shared lock and worker
configuration contract](README.md#worker-profiles-and-execution).

Rerun the identical setup command if interrupted. It reuses its producer and
saved token; it does not replace the queue. If a token was issued but its secret
never reached the setup directory, retry revokes that unusable setup token and
issues a replacement. An existing completed setup cannot be rebound to a
different server or folder.

## 4. Download and resume

```sh
stash-gallery download https://www.reddit.com/user/Example/
stash-gallery download https://x.com/Example
stash-gallery download -i /path/to/urls.txt
stash-gallery download ytdl:https://video.example/watch/example
```

Lists accept one URL per line, blank lines and whole-line `#` comments, up to
50 distinct URLs per invocation. New sources are registered through Stash's
application API. Existing active sources keep their settings and ownership;
disabled, retired or ambiguous sources require review.

Metadata defaults import available titles, descriptions and post URLs when a
scene/image is created, with a filename title fallback. They leave existing
edits alone. Publishers are not automatically treated as depicted performers;
use account review for ownership, including aggregator accounts. Change metadata
policies in Stash's collection settings.

Only supported source adapters can be ingested. Current adapters include Reddit
(including supported external media), Twitter, Instagram, Bluesky, TikTok,
Kemono/Coomer, Tumblr and the supported gallery/yt-dlp sources documented in the
[integration reference](README.md). Installing gallery-dl does not automatically
add native evidence support for every extractor it contains.

Preview target selection and local settings with:

```sh
stash-gallery download --dry-run https://www.reddit.com/user/Example/
```

Each real invocation prints a saved call UUID. Ctrl-C, a Stash restart or a
temporary outage leaves work resumable:

```sh
stash-gallery resume CALL_UUID
stash-gallery status
stash-gallery drain
```

`status` reports local queue counts. `drain` only sends already saved events.
Successful source traversal does not mean all Stash file-verification jobs have
finished; inspect Stash activity/receipts for that. Keep the full setup directory
and its referenced configs until pending work completes.

## 5. Optional background operation

Queue a request without downloading in the foreground:

```sh
stash-gallery download --queue-only -i /path/to/urls.txt
stash-gallery worker
```

`worker` performs one dispatch using the same persistent queue. Schedule it
repeatedly with your service manager; it resumes queued requests and delivers
saved events. It needs only the saved scoped token. The application API key is
needed when registering new URLs, not for dispatch or delivery.

For example, a user systemd service and timer (substitute the absolute executable
and state paths; `command -v stash-gallery` prints the executable):

```ini
# ~/.config/systemd/user/stash-gallery.service
[Unit]
Description=Download queued Stash sources

[Service]
Type=oneshot
ExecStart=/home/USER/.local/bin/stash-gallery --directory /persistent/stash-gallery worker
TimeoutStartSec=infinity
SuccessExitStatus=2
```

```ini
# ~/.config/systemd/user/stash-gallery.timer
[Unit]
Description=Resume queued Stash downloads

[Timer]
OnBootSec=1m
OnUnitInactiveSec=30s

[Install]
WantedBy=timers.target
```

Enable with `systemctl --user daemon-reload` and
`systemctl --user enable --now stash-gallery.timer`. A running service does not
get a second instance from its timer. This drains admitted work; it does **not**
subscribe to daily checks or fetch extra metadata. For routine monitoring,
per-site parallel workers, request budgets and n8n, use the
[source-run guide](../../docs/native-source-runs.md) and
[worker reference](README.md#automatic-source-dispatch).

Back up the complete setup directory, local gallery-dl config/credential files
and media with your chosen backup system. It contains a private
`credential.json`, so keep it mode 700 with files mode 600. When upgrading an
active worker, retain the old runtime until saved work finishes, or use the
[policy-upgrade procedure](README.md#stash-restart-recovery). Do not replace the
outbox to clear an error. Setup-only changes do not change worker policy hashes.
