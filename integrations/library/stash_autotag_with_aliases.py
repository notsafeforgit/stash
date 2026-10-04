#!/usr/bin/env python3
"""Attach performers by canonical name or alias in media paths using native Stash."""

import argparse
import re
import sys
from typing import List, Optional, Sequence, Set

from stash_library import StashClient, StashError, add_connection_arguments, filter_ast

BOUNDARY_CLASS = '[\\\\/ \\t_\\.\\-\\(\\)\\[\\]\\{\\},:+&\'"]'

def _term_to_flexible_regex(term: str) -> str:
    term = term.strip()
    if not term:
        return ''
    tokens = re.split('\\s+', term)
    esc = [re.escape(t) for t in tokens if t]
    if not esc:
        return ''
    if len(esc) == 1:
        return esc[0]
    return '[ _\\.-]+'.join(esc)

def build_combined_path_regex(name: str, aliases: Sequence[str], loose: bool=False) -> str:
    raw_terms = [name] + list(aliases or [])
    seen: Set[str] = set()
    parts: List[str] = []
    for t in raw_terms:
        t = (t or '').strip()
        if not t:
            continue
        k = t.casefold()
        if k in seen:
            continue
        seen.add(k)
        p = _term_to_flexible_regex(t)
        if p:
            parts.append(p)
    if not parts:
        return '(?!)'
    inner = '(?:%s)' % '|'.join(parts)
    if loose:
        return f'(?i){inner}'
    return f'(?i)(?:^|{BOUNDARY_CLASS}){inner}(?:{BOUNDARY_CLASS}|$)'

def parse_id_flags(performer_id: Optional[List[str]], performer_ids: Optional[str]) -> List[str]:
    ids: List[str] = []
    if performer_id:
        ids.extend([str(x).strip() for x in performer_id if str(x).strip()])
    if performer_ids:
        ids.extend([x.strip() for x in performer_ids.split(',') if x.strip()])
    seen: Set[str] = set()
    out: List[str] = []
    for x in ids:
        if x not in seen:
            seen.add(x)
            out.append(x)
    return out


def find_performers(client, ids, per_page):
    fields = "id name alias_list ignore_auto_tag"
    if not ids:
        return client.find_all("Performer", fields, per_page=per_page)
    performers = []
    for performer_id in ids:
        data = client.call("query LibraryPerformer($id: ID!) { findPerformer(id: $id) { " + fields + " } }", {"id": performer_id})
        performer = data.get("findPerformer")
        if performer:
            performers.append(performer)
        else:
            print(f"WARNING: performer id {performer_id} not found", file=sys.stderr)
    return performers


def matching_ids(client, entity, performer, per_page, loose=False, debug=False):
    regex = build_combined_path_regex(performer["name"], performer["alias_list"], loose=loose)
    if debug:
        print(f"  {entity} path regex={regex}", file=sys.stderr)
    ast = filter_ast(
        path={"value": regex, "modifier": "MATCHES_REGEX"},
        performers={"value": [performer["id"]], "modifier": "EXCLUDES"},
    )
    return [item["id"] for item in client.find_all(entity, ast=ast, per_page=per_page)]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    add_connection_arguments(parser)
    parser.add_argument("--loose", action="store_true")
    parser.add_argument("--debug", action="store_true")
    parser.add_argument("--performer-id", action="append")
    parser.add_argument("--performer-ids")
    parser.add_argument("--include-ignored", action="store_true")
    args = parser.parse_args()
    client = StashClient.from_args(args)
    performers = find_performers(client, parse_id_flags(args.performer_id, args.performer_ids), args.per_page)
    if not args.include_ignored:
        performers = [performer for performer in performers if not performer["ignore_auto_tag"]]
    print(f"Performers to process: {len(performers)}", file=sys.stderr)
    totals = {"Scene": 0, "Image": 0, "Gallery": 0}
    for index, performer in enumerate(performers, 1):
        print(f"[{index}/{len(performers)}] Performer {performer['id']} :: {performer['name']!r}", file=sys.stderr)
        for entity in totals:
            # Complete pagination before attaching, since attachment changes the exclusion filter.
            ids = matching_ids(client, entity, performer, args.per_page, args.loose, args.debug)
            count = client.add_relationship(entity, ids, "performer_ids", performer["id"], args.chunk_size, args.dry_run)
            totals[entity] += count
    label = "DRY RUN (no mutations sent)" if args.dry_run else "UPDATED"
    print(f"{label}: scenes={totals['Scene']} images={totals['Image']} galleries={totals['Gallery']}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except StashError as exc:
        print(f"ERROR: {exc}", file=sys.stderr)
        raise SystemExit(1)
    except KeyboardInterrupt:
        raise SystemExit(130)
