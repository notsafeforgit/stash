# iPhone clip batches through FileFlows

This optional integration joins uploaded clips using the existing adaptive
AV1/Opus FileFlows pipeline and imports the finished video through Stash's
native local-file API. It does not create source posts or retain NFO files.

The shortcut reads each selected video's recording date before exporting it,
assigns a unique clip name, and hashes the exported file. A JSON manifest carries
the title, description, and clip timestamps. For multiple clips, the shortcut
offers **Recording date (oldest first)** or **Choose clip order**. The latter
shows clips with their recording date, filename and available preview; choose
the first clip, then each next clip from those remaining. This happens before
any upload files are created, so cancelling leaves Photos untouched.

The manifest's `order` is `recorded_at` for chronological order or `input` to
follow the `clips` array exactly. Older manifests without `order` retain
chronological ordering; equal timestamps retain input order. Unknown modes fail
without encoding or deleting clips. The scene date always defaults to the
oldest clip's local calendar date, independently of playback order. Set `date` in the
manifest to a different ISO calendar date when another uploader supplies it.

Uploads use a unique `stash-upload-<batch_id>.part` folder. Stash's automatic
folder discovery ignores that unfinished folder. FileFlows verifies the
manifest and every clip hash before encoding. Incomplete uploads are held and
retried after five minutes. Invalid or unexpected files fail without deleting
originals. Uploading through a phone file provider still needs a successful
connection; retries must finish the same batch folder.

After its existing output validation, FileFlows publishes a small
`stash-import-<batch_id>.json` beside the final MKV, then cleans up the exact
unchanged source snapshot. The delivery timer submits the file and metadata
using one durable Stash request. Stash applies the supplied metadata and the
folder's performer policy in the media registration transaction. Retrying a
request after an interrupted response does not duplicate the import. Previously
curated conflicting fields are retained and reported for review.

The small pending JSON is removed only when Stash acknowledges successful media
registration without conflicts. It remains available during restarts, failed
imports, and conflicts. API credentials stay on the host, outside the shortcut
and FileFlows. No additional source history or discovery records are created.

## Install

1. Export the existing iPhone concat entry and adaptive pipeline flows as JSON.
   `build_flows.py ENTRY.json PIPELINE.json OUTPUT_DIR` creates separate phone
   flows without editing the existing ones. Its source anchors are deliberately
   strict: a changed base pipeline needs review rather than a guessed patch.
   Import `pipeline.json`, then `entry.json`, and select the new entry in the
   upload folder library. The existing encode-and-verify subflow is reused.
   Use folder processing, with a short scan interval or file-system events.
2. Copy `intake.py` to the host and configure only the output directories it
   should deliver. Collection UUIDs refer to existing active Stash directory
   collections with their performer policies. Paths in `relative_path` are
   relative to the Stash media root, not the host filesystem root.
3. Run `uv run --no-project --python /usr/bin/python3 intake.py --config CONFIG`.
   The example user service and timer run it once per minute without overlap.
   A nonempty error report means the pending handoff needs attention; the video
   and metadata are preserved. Do not edit a manifest after admission to Stash.
4. `build_shortcuts.py WRAPPER.shortcut PROCESSOR.shortcut OUTPUT_DIR` builds
   unsigned replacements named **Upload Jess Videos** and **Process and Organize
   Videos**, matching the original shortcuts and the wrapper's helper reference.
   The original folder
   bookmarks and post-upload Photos deletion action are preserved. Signing and
   running the resulting files requires macOS/iOS; Linux validates their plist
   structure but cannot execute Apple Shortcuts. Import the processing shortcut
   before the upload wrapper, replace the existing shortcuts when prompted, and
   check the folder bookmarks on the iPhone. `sign-on-mac.sh` signs both files
   under these original names.

Example host configuration (keep it private):

```json
{
  "endpoint": "http://localhost:8009",
  "api_key_file": "/home/user/.config/stash/api-key",
  "state_dir": "/home/user/.config/stash-fileflows/state",
  "folders": [{
    "path": "/host/media/Phone uploads",
    "relative_path": "Phone uploads",
    "collection_uuid": "YOUR-DIRECTORY-COLLECTION-UUID"
  }]
}
```

Stash must advertise `file_metadata: true` under
`/api/v3/archive/manual-intake/capabilities`. Keep the configured state directory
and pending manifests in the host backup along with media. The integration
requires no FileFlows API license: flow imports and library setup use its normal
web console.

Run `python3 -m unittest discover -s integrations/fileflows -v` for the transport
recovery tests. The actual FileFlows pipeline should also be exercised with
synthetic clips when its preparation or publication nodes change.
