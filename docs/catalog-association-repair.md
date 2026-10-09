# Imported catalog associations

Imported posts can retain an account in `posts.account_key` or an author directory
even when their old NFO-derived captures contain no author fields. The explicit
`catalog-association-v1` repair promotes that evidence into ordinary native
publisher decisions. It does not rewrite import receipts or source payloads.

The repair considers only imported catalog post-account claims and imported
directory membership. Known gallery-dl directory templates supply Reddit,
Twitter, Instagram, Bluesky and TikTok handles; Twitter numeric IDs and Bluesky
DIDs also participate when present. Saved feeds, subreddit folders and arbitrary
display-name directories are not author evidence. Coomer/Kemono folder usernames
can resolve existing accounts within the post's mirror service namespace,
including retained username/label identifiers when the account is labeled with
a numeric ID. They never establish native OnlyFans, Fansly or Patreon numeric IDs.

Exactly one canonical account must match. A conflicting captured author, multiple
account candidates, an existing publisher choice or an explicit unlink prevents
automatic replacement. Checks cover every retained capture of a canonical post,
including merged representations. Oversized scopes stay unresolved. The repair
does not create accounts or fabricate captures when evidence is missing.

Optionally, an account without an ownership decision is linked to the unique
existing performer whose canonical name or alias exactly matches its label or
retained handle (or an imported mirror-account label), ignoring ASCII case. All
matching names participate: a canonical
name does not take precedence over somebody else's alias. Multiple performers
remain unresolved and their candidate UUIDs are reported. Existing ownership
choices, including an explicit unlink or return to undecided review, are retained.
The chosen performer UUID follows subsequent merges. No scene/image performer
tags are added: publisher ownership and depicted people remain separate.

These links use `capture_publisher_decisions` and `account_performer_decisions`
in the main database, with migration provenance and the repair policy in their
reason. Normal native backups include them. Posts show the resulting publisher
and its account owner; **Review account** opens the existing ownership editor.
This explicit historical repair does not loosen the author-ID rules for new
gallery-dl events.

Authenticated admin endpoints:

- `GET /api/v3/archive/catalog-associations/posts?after=<uuid>&limit=25`
- `GET /api/v3/archive/posts/<uuid>/catalog-association-preview`
- `POST /api/v3/archive/catalog-associations/backfill` with
  `{"post_uuids":["<uuid>"],"link_owners":true}` (at most 25 posts).

Writes recheck the complete relevant scope in one short transaction. Repeating
a batch is safe and preserves later manual choices. The helper advances a small
checkpoint after each batch and writes unresolved rows to a local review report:

```sh
python3 scripts/backfill_catalog_associations.py \
  --server http://127.0.0.1:8009 \
  --api-key-file /path/to/private-api-key \
  --state /path/to/catalog-associations.json \
  --link-owners
```

The helper never reads/writes the live database directly or walks media files.
Its checkpoint/report is operational progress, not a second catalog. A retry
after a lost response may count a previously committed link as already linked;
the database decisions, not those progress counters, are the authoritative totals.
