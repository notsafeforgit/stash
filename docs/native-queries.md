# Native entity queries

The native list queries use one filter expression contract. The former flat
entity-filter arguments and integer-ID aliases are rejected by GraphQL.

| Query | Expression argument | Result items |
| --- | --- | --- |
| `findScenes` | `scene_filter_ast` | `scenes` |
| `findImages` | `image_filter_ast` | `images` |
| `findPerformers` | `performer_filter_ast` | `performers` |
| `findStudios` | `studio_filter_ast` | `studios` |
| `findGalleries` | `gallery_filter_ast` | `galleries` |
| `findGroups` | `group_filter_ast` | `groups` |
| `findTags` | `tag_filter_ast` | `tags` |
| `findSceneMarkers` | `scene_marker_filter_ast` | `scene_markers` |

`filter: FindFilterType` still supplies text search, sorting, direction, page and
page size. Search combines with the expression. An omitted expression does not
restrict the query. A nonempty `ids: [ID!]` selects those entities directly;
it takes precedence over expression/search selection. An empty ID list is not
an explicit empty selection, so callers with no selected IDs should omit the
request rather than issue an unrestricted list query.

For example, the gallery image viewer selects images belonging to one gallery:

```graphql
query GalleryImages($criteria: FilterASTInput!, $page: Int!) {
  findImages(
    image_filter_ast: $criteria
    filter: { page: $page, per_page: 40, sort: "path" }
  ) {
    count
    images { id title }
  }
}
```

```json
{
  "criteria": {
    "root": {
      "condition": {
        "field": "galleries",
        "value": { "modifier": "INCLUDES", "value": ["1387"] }
      }
    }
  },
  "page": 1
}
```

A node contains either a `condition` or a `group`. A group supplies `operator`
(`AND` or `OR`) and `children`, which are further nodes. Criterion values use the
query shape for that field; relationship IDs are strings. This differs from
the labeled values retained in saved filters. The native UI's criterion models
and `makeFilterAst()` construct the query shape; historical object conversions
remain at import boundaries.

`count` describes the full matching selection. Scene `duration`/`filesize` and
image `megapixels`/`filesize` also aggregate the full matching selection, even
when items are paginated or omitted from the response. Filtered totals include
associated secondary files and preserve fractional megapixels. Explicit-ID
lookups retain their existing primary-file totals.

The scene and image duplicate queries, including their paginated group forms,
also accept only their corresponding filter expression. `ALL` retains matching
members of duplicate groups; `ANY` includes the whole group when a member
matches. The distance, duration and pagination controls remain unchanged.

This retirement covers the eight list queries and duplicate queries above.
File/folder nested filters and internal repository object filters have separate
consumers and are tracked in the transition plan.
StashDB protocol bindings and public media/share URLs are unchanged.

## Groups

Groups are the sole collection model for these API relationships. Use
`findGroup`, `findGroups`, `groupCreate`, `groupUpdate`, `groupDestroy` and
`groupsDestroy`. Entity relationships/counts use `groups`/`group_count`.
Scene create/update inputs use `groups: [{group_id, scene_index}]`; bulk scene
updates use `group_ids`. Export requests select `groups`.

The `Movie` types, query/mutation aliases, nested movie fields and `MOVIE`
scrape enum are removed. Scraper discovery and results expose `GROUP` and
`ScrapedGroup`; provider data using the old movie shape is normalized once at
the input boundary. An explicit provider `groups` list takes precedence.
Filename parsing supports `{group}` and returns `groups` with `group_id`.
Historical `{movie}` patterns remain readable input and produce the same result.
Plugins receive one `Group.*` event after a successful change.

This does not rewrite historical metadata export files or saved configuration
keys. Their one-time migration remains separate from the native API contract.

The catalog plugin's native client conversion is staged on its
`native-api-transition` branch. Its group requests replace movie aliases, and
its tagged reload queries use expressions without retrying an empty selection
against the entire library. Keep the installed compatible package until the
coordinated native cutover. The manual helpers in `integrations/library` already
use this expression contract.
