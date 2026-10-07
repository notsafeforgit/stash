#!/usr/bin/env python3
"""Set one performer's image titles from their primary filenames."""

import argparse
from pathlib import PurePosixPath
import sys

from stash_library import StashClient, StashError, add_connection_arguments, filter_ast


PERFORMER_QUERY = """query TitlePerformer($id: ID!) {
  findPerformer(id: $id) { id name }
}"""
UPDATE_QUERY = """mutation FilenameTitle($input: ImageUpdateInput!) {
  imageUpdate(input: $input) { id title }
}"""


def resolve_performer(client, name, performer_id, per_page):
    if performer_id is not None:
        performer = client.call(PERFORMER_QUERY, {"id": performer_id}).get("findPerformer")
        if not isinstance(performer, dict) or performer.get("id") != performer_id:
            raise StashError(f"Performer ID {performer_id!r} was not found")
        return performer
    performers = client.find_all("Performer", "id name",
                                 filter_ast(name={"value": name, "modifier": "EQUALS"}), per_page)
    if not performers:
        raise StashError(f"No performer found with name {name!r}")
    if len(performers) != 1:
        candidates = ", ".join(f"{item['name']} (id={item['id']})" for item in performers)
        raise StashError(f"Multiple performers matched; use --performer-id: {candidates}")
    return performers[0]


def title_changes(client, performer_id, per_page, only_empty=False):
    images = client.find_all("Image", "id title visual_files { ... on BaseFile { path } }",
                             filter_ast(performers={"value": [performer_id], "modifier": "INCLUDES"}),
                             per_page)
    changes = []
    for item in images:
        if (only_empty and item.get("title")) or not item["visual_files"]:
            continue
        # Native visual_files keeps the primary file first, including ZIP members.
        title = PurePosixPath(item["visual_files"][0]["path"]).stem
        if title and item.get("title") != title:
            changes.append({"id": item["id"], "title": title})
    return changes


def apply_titles(client, changes, dry_run=False):
    for change in changes:
        if not dry_run:
            result = client.call(UPDATE_QUERY, {"input": change}).get("imageUpdate")
            if not isinstance(result, dict) or any(result.get(key) != value for key, value in change.items()):
                raise StashError(f"Image {change['id']} did not confirm its new title; later edits were stopped")
        print(f"{'DRY RUN: would update' if dry_run else 'Updated'} image {change['id']}: {change['title']!r}")
    return len(changes)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("performer", nargs="?", help="Exact canonical performer name")
    parser.add_argument("--performer-id", help="Select an explicit performer when names are ambiguous")
    parser.add_argument("--only-empty", action="store_true", help="Leave existing titles unchanged")
    add_connection_arguments(parser)
    args = parser.parse_args()
    if bool(args.performer) == bool(args.performer_id):
        parser.error("provide either a performer name or --performer-id")
    client = StashClient.from_args(args)
    performer = resolve_performer(client, args.performer, args.performer_id, args.per_page)
    changes = title_changes(client, performer["id"], args.per_page, args.only_empty)
    count = apply_titles(client, changes, args.dry_run)
    print(f"{'DRY RUN: selected' if args.dry_run else 'Updated'} images={count} for {performer['name']!r}")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except StashError as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        raise SystemExit(1)
    except KeyboardInterrupt:
        raise SystemExit(130)
