#!/usr/bin/env python3
"""Apply an existing tag to a native Stash collection's scenes or images."""

import argparse
import sys

from stash_library import StashClient, StashError, add_connection_arguments, filter_ast


def auto_apply_modes(entity_type, apply_to):
    if apply_to == "auto":
        return {
            "gallery": ["images"], "image": ["images"], "scene": ["scenes"],
            "group": ["scenes"], "movie": ["scenes"],
            "performer": ["scenes", "images"], "studio": ["scenes", "images"], "tag": ["scenes", "images"],
        }[entity_type]
    modes = ["scenes", "images"] if apply_to == "both" else [apply_to]
    if entity_type in {"group", "movie", "scene"} and modes != ["scenes"]:
        raise StashError(f"{entity_type} can only target scenes")
    if entity_type == "image" and modes != ["images"]:
        raise StashError("image can only target images")
    return modes


def resolve_tag_id(client, tag_name, per_page):
    tags = client.find_all("Tag", "id name", filter_ast(name={"value": tag_name, "modifier": "EQUALS"}), per_page)
    if not tags:
        raise StashError(f"No tag found with name {tag_name!r}")
    exact_case = [tag for tag in tags if tag["name"] == tag_name]
    if len(exact_case) == 1:
        return exact_case[0]["id"]
    if len(tags) != 1:
        matches = ", ".join(f"{tag['name']} (id={tag['id']})" for tag in tags)
        raise StashError(f"Multiple tags matched {tag_name!r}; refusing to guess: {matches}")
    return tags[0]["id"]


def selected_ids(client, mode, source, source_id, per_page):
    if source in {"scene", "image"}:
        return [source_id]
    field = {"gallery": "galleries", "performer": "performers", "studio": "studios", "group": "groups", "movie": "groups", "tag": "tags"}[source]
    ast = filter_ast(**{field: {"value": [source_id], "modifier": "INCLUDES"}})
    return [item["id"] for item in client.find_all("Scene" if mode == "scenes" else "Image", ast=ast, per_page=per_page)]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("entity_type", choices=["gallery", "performer", "studio", "group", "movie", "tag", "scene", "image"])
    parser.add_argument("entity_id")
    parser.add_argument("tag_name", help="Exact existing tag name")
    parser.add_argument("--apply-to", choices=["auto", "scenes", "images", "both"], default="auto")
    add_connection_arguments(parser)
    args = parser.parse_args()
    client = StashClient.from_args(args)
    modes = auto_apply_modes(args.entity_type, args.apply_to)
    tag_id = resolve_tag_id(client, args.tag_name, args.per_page)
    # Freeze the entire selection before adding a tag that may change a filter.
    selections = {mode: selected_ids(client, mode, args.entity_type, args.entity_id, args.per_page) for mode in modes}
    print(f"Resolved tag {args.tag_name!r} -> id={tag_id}")
    for mode, ids in selections.items():
        count = client.add_relationship("Scene" if mode == "scenes" else "Image", ids, "tag_ids", tag_id, args.chunk_size, args.dry_run)
        print(f"{'DRY RUN: selected' if args.dry_run else 'Updated'} {mode}={count}")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except StashError as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        raise SystemExit(1)
    except KeyboardInterrupt:
        raise SystemExit(130)
