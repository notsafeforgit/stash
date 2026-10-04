"""Shared native GraphQL client for the manual library helpers (stdlib only)."""

import argparse
import json
import os
from urllib import error, request


class StashError(RuntimeError):
    pass


class NoRedirect(request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def positive_size(value):
    number = int(value)
    if not 1 <= number <= 1000:
        raise argparse.ArgumentTypeError("must be between 1 and 1000")
    return number


def add_connection_arguments(parser):
    parser.add_argument("--graphql-url", default=os.environ.get("STASH_GRAPHQL_URL", "http://localhost:8009/graphql"))
    parser.add_argument("--api-key", default=os.environ.get("STASH_API_KEY"))
    parser.add_argument("--api-key-header", default=os.environ.get("STASH_API_KEY_HEADER", "ApiKey"))
    parser.add_argument("--per-page", type=positive_size, default=200)
    parser.add_argument("--chunk-size", type=positive_size, default=200)
    parser.add_argument("--dry-run", action="store_true")


def filter_ast(**conditions):
    children = [{"condition": {"field": field, "value": value}} for field, value in conditions.items()]
    if len(children) == 1:
        return {"root": children[0]}
    return {"root": {"group": {"operator": "AND", "children": children}}}


def completed_count(result, requested_ids):
    """Never turn admission, partial data or an old response into completion."""
    if not isinstance(result, dict):
        raise StashError("Missing native bulk-update acknowledgment")
    if result.get("status") == "QUEUED":
        raise StashError(f"Bulk update was queued (job {result.get('job_id')}); completion is unconfirmed. Inspect the job before retrying.")
    updated = result.get("updated_ids")
    if (
        result.get("status") != "COMPLETED"
        or result.get("job_id") is not None
        or type(result.get("selected_count")) is not int
        or result["selected_count"] != len(requested_ids)
        or not isinstance(updated, list)
        or any(not isinstance(item, str) for item in updated)
        or len(updated) != len(requested_ids)
        or set(updated) != set(requested_ids)
    ):
        raise StashError("Bulk update did not confirm the selected IDs; inspect its status before retrying")
    return len(updated)


class StashClient:
    def __init__(self, url, api_key=None, api_key_header="ApiKey", timeout=120):
        self.url = url
        self.headers = {"Content-Type": "application/json"}
        if api_key:
            self.headers[api_key_header] = api_key
        self.timeout = timeout
        self.opener = request.build_opener(NoRedirect)

    @classmethod
    def from_args(cls, args):
        return cls(args.graphql_url, args.api_key, args.api_key_header)

    def call(self, query, variables=None):
        body = json.dumps({"query": query, "variables": variables or {}}).encode()
        req = request.Request(self.url, data=body, headers=self.headers, method="POST")
        try:
            with self.opener.open(req, timeout=self.timeout) as response:
                result = json.load(response)
        except error.HTTPError as exc:
            raise StashError(f"GraphQL HTTP error {exc.code}") from None
        except (error.URLError, TimeoutError, ValueError) as exc:
            raise StashError(f"GraphQL request failed ({type(exc).__name__})") from None
        if not isinstance(result, dict) or result.get("errors") or not isinstance(result.get("data"), dict):
            raise StashError("GraphQL rejected the request or returned an invalid result")
        return result["data"]

    def find_all(self, entity, fields="id", ast=None, per_page=200):
        plural = {"Scene": "scenes", "Image": "images", "Gallery": "galleries", "Performer": "performers", "Tag": "tags"}[entity]
        query = f"""query LibraryItems($ast: FilterASTInput, $filter: FindFilterType) {{
          find{plural.title()}({entity.lower()}_filter_ast: $ast, filter: $filter) {{
            {plural} {{ {fields} }}
          }}
        }}"""
        seen = set()
        items = []
        page = 1
        while True:
            data = self.call(query, {"ast": ast, "filter": {"page": page, "per_page": per_page, "sort": "id", "direction": "ASC"}})
            batch = data[f"find{plural.title()}"][plural]
            new = [item for item in batch if item["id"] not in seen]
            items.extend(new)
            seen.update(item["id"] for item in new)
            if len(batch) < per_page:
                return items
            if not new:
                raise StashError("Pagination made no progress; selection is incomplete")
            page += 1

    def add_relationship(self, entity, ids, field, related_id, chunk_size=200, dry_run=False):
        ids = list(dict.fromkeys(ids))
        if dry_run:
            return len(ids)
        query = f"""mutation LibraryBulkUpdate($input: Bulk{entity}UpdateInput!) {{
          result: bulk{entity}Update(input: $input) {{ status job_id selected_count updated_ids }}
        }}"""
        total = 0
        for offset in range(0, len(ids), chunk_size):
            batch = ids[offset:offset + chunk_size]
            result = self.call(query, {"input": {"ids": batch, field: {"ids": [related_id], "mode": "ADD"}}})
            total += completed_count(result.get("result"), batch)
        return total
