# Native manual library helpers

These stdlib-only Python clients replace the audited host scripts
`~/bin/tag_stash_collections.py`, `~/bin/stash_autotag_with_aliases.py`,
`~/bin/update_image_titles_stash_performer.py` and `~/bin/stash_scan_and_generate.py`
when the native Stash API is activated. They use canonical filter ASTs and the
[native bulk result](../../docs/native-bulk-updates.md). They are staged here;
the live compatible scripts have not been replaced.

Set `STASH_GRAPHQL_URL` (default `http://localhost:8009/graphql`) and supply the
existing Stash application key through `STASH_API_KEY` when authentication is
required. No key is embedded. `--graphql-url`, `--api-key` and `--api-key-header`
also exist; prefer the environment for keys. These manual tools use library
application access, rather than a source producer's ingestion token.

```sh
# Inspect the images belonging to a gallery before applying an existing tag.
python3 integrations/library/tag_stash_collections.py gallery 1387 "example" --dry-run

# Inspect path matches for one performer, including their aliases.
python3 integrations/library/stash_autotag_with_aliases.py --performer-id 871 --dry-run

# Review filename titles for one performer's images, keeping existing titles.
python3 integrations/library/update_image_titles_stash_performer.py --performer-id 871 --only-empty --dry-run

# Queue the ordinary scanner explicitly, including ZIP-gallery support.
python3 integrations/library/stash_scan_and_generate.py /media/porn/new-album.zip
```

Remove `--dry-run` to apply. Tagging supports gallery, performer, studio, group,
tag, scene and image sources with `--apply-to auto|scenes|images|both`. The old
`movie` command-line spelling maps to the native group filter. Tag-name ambiguity
is reported rather than guessed; exact case takes precedence.

Autotagging accepts repeated `--performer-id` or comma-separated `--performer-ids`;
omitting both processes the library. It honors `ignore_auto_tag` unless
`--include-ignored` is set. Canonical names and aliases match path boundaries;
`--loose` permits substring matches. `--debug` prints the resulting expressions.
Missing explicit performers never widen the request to the whole library.

The tagging and title tools use bounded pages sorted by ID and collect the selection before
mutating it. This avoids shifting their own exclusion filters during pagination;
concurrent unrelated writers can still change the library between reads.
`--per-page` and `--chunk-size` accept 1–1000 (default 200).

Each explicit-ID batch must return `COMPLETED` and exactly its requested IDs.
Queued, malformed or failed responses stop later batches and exit unsuccessfully.
Earlier batches may already have committed. Inspect any reported job before
retrying; relationship `ADD` preserves existing links. Dry runs send no mutations.

Title repair accepts an exact canonical name or `--performer-id`. Duplicate names
require an ID. It uses the primary visual file's basename without its extension,
skips missing files and unchanged titles, and stops on an unconfirmed edit. It
replaces existing titles unless `--only-empty` is supplied. This explicit repair
command does not change automatic import policy.

The scan helper requires a path as seen by the server, queues cover and video
perceptual-hash generation, and reports the admitted job ID. Queue admission is
not scan completion; inspect the job in Stash. Scheduled discovery uses
`stash-intake-folders` and its durable journal separately. This helper preserves
the existing Home Assistant command path at installation.

At cutover, preserve the compatible helpers at the common backup boundary, then
install these four scripts and `stash_library.py` together in the chosen directory.
Configure the native endpoint/key and repeat the dry runs against the migrated
library before enabling writes. Do not copy the old scripts' embedded keys into
the repository. `make validate-library` runs the offline helper regressions and
is included in the fork gate and CI.
