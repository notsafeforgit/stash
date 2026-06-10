# Evaluation: v3 Plan vs Actual Implementation

## Context

Evaluating `ui/v3/docs/plan.md` (Phases 0-6, with Phase 7 not yet attempted) against the actual state of the v3 UI codebase and the v2.5 modifications on `local/test-filter-ui`.

---

## Phase-by-Phase Assessment

### Phase 0: Scaffold v3 project — COMPLETE, matches plan

All infrastructure in place: Vite 8, TanStack Router, Apollo 4, Tailwind v4, shadcn/ui, GraphQL codegen with TypedDocumentNode. Directory structure matches the plan.

### Phase 1: Architecture principles — WELL FOLLOWED

| Principle | Status | Notes |
|---|---|---|
| Kill StashService.ts | **Done** | No StashService.ts in v3; components use Apollo hooks directly |
| One generic list | **Done** | `EntityListPage<TData, TItem>` config pattern; each entity route is a thin config |
| Composable cards | **Done** | `EntityCard` compound component with sub-components; per-entity cards compose it |
| No formik | **Done** | No formik dependency |
| Colocate, don't centralize | **Mostly done** | Hooks like `useFilterState`, `useListSelect` live in `components/list/` near their consumers. Some shared hooks remain in `hooks/` but they're genuinely app-wide |
| No implicit any | **In progress** | `strict: true` in tsconfig; filter.ts has a few TODO comments |
| No monoliths (<500 lines) | **Mostly done** | FilterBuilder was split from the 2354-line monolith into FilterBuilder + GroupEditor + ConditionEditor. EntityCard is well-decomposed |
| Per-view fragments | **Done** | SceneCard uses minimal fragment; separate mobile vs desktop fragments |
| Mobile-first | **Done** | Components use Tailwind breakpoint modifiers; MobileListChrome exists |

### Phase 2: Backend integration — COMPLETE, matches plan

Makefile targets added. Dev server on port 3002.

### Phase 3: App shell & layout — MOSTLY COMPLETE

| Item | Status | Notes |
|---|---|---|
| Provider tree | **Done** | `ApolloProvider` > `ThemeProvider` > `ConfigLoader` (which wraps `LocaleProvider` > `ConfigurationProvider`) > `RouterProvider` |
| ThemeProvider | **Done** | Light/dark/system, localStorage persistence, system preference listener |
| IntlProvider | **Done** | Dynamic locale loading from server config, merges default + chosen + custom overrides |
| ConfigurationProvider | **Done** | Wired into provider tree via ConfigLoader |
| AppShell layout | **Done** | Header + main + BottomTabBar structure |
| Header | **Done** | Desktop nav with NavLinks + UserMenu, hidden on mobile |
| BottomTabBar | **Done** | Mobile-only, with "More" dropdown |
| MobileNavSheet | **Done** | Slide-out left sheet |
| Route stubs | **Done** | All 8 entity list routes + detail route stubs |
| Keyboard shortcuts | **Done** | `g s`, `g p`, etc. working |
| **Responsive shell (3 breakpoints)** | **Partial** | Plan specifies 3 distinct layouts (mobile/tablet/desktop) with bottom tab bar on mobile, inline nav on tablet, sidebar on desktop. Bottom bar and header exist, but the plan's acceptance criteria notes the 3-breakpoint layout isn't fully validated |
| `tsc --noEmit` | **Not passing** | Plan's acceptance criteria marks this unchecked |

### Phase 4: Generalize Filter AST — COMPLETE

**Frontend:**
- `scene-filter-ast.ts` renamed to `filter-ast.ts` with generic types
- `filter.ts` uses `filterAst?: FilterASTNode` (generic)
- `astCriterionOptions(mode)` implemented
- `makeFilterAST()` exists and works for all modes
- Saved key is `__filter_ast` with legacy compat

**Backend:**
- All 8 AST filter handler files created + `ast_filter_base.go`
- `QueryAST()` on all 8 repository interfaces
- GraphQL schema updated with `filter_ast` params on all 8 find queries
- All 8 resolvers handle the 3-path flow (IDs > AST > legacy)
- All mock files updated

**One unchecked item:** `make generate` — plan notes this hasn't been confirmed to succeed after schema changes. This is important because the generated Go/TS files need to be regenerated.

### Phase 5: Filter builder UI — COMPLETE

- SceneFilterBuilderDialog split into `FilterBuilder.tsx` + `GroupEditor.tsx` + `ConditionEditor.tsx`
- All renamed from Scene-specific to generic (FilterASTGroupNode, etc.)
- 21 criterion renderer files in `components/filters/Filters/`
- All ported to shadcn/ui (comboboxes, inputs, etc.)
- Saved filter flow working
- Drag-and-drop with dnd-kit
- Pinned fields per mode
- Copy/paste JSON

**Acceptance criteria from plan (all unchecked in plan):** The plan lists 13 criteria all marked `[ ]`. Based on code exploration, most appear functionally implemented but haven't been formally validated (no automated tests for UI).

### Phase 6: List views — COMPLETE

- `EntityList.tsx` / `EntityListPage.tsx` implemented with full config pattern
- All 8 entity list routes configured as thin config files
- Card components for all 8 entities
- Supporting hooks: `useFilterState`, `useListSelect`, `useListKeyboardShortcuts`, `useCachedQueryResult`, `useListPrefetch`
- `ListToolbar`, `ListPagination` components
- Mobile chrome with bottom sheet
- HoverScrubber for gallery/scene cards
- No StashService imports

**Acceptance criteria from plan (all unchecked):** 14 criteria listed. Most appear implemented in code but formally unvalidated.

---

## v2.5 Modifications (on `local/test-filter-ui` branch)

The v2.5 codebase has been simplified in several ways that appear to be cleanup/experimentation for the filter UI work:

| Change | Impact |
|---|---|
| Removed debounce logic from `util.ts` | Queries fire immediately on filter change |
| Removed `useListPrefetch` from `util.ts` | No sliding-window prefetch |
| Removed `showSavedFilters`/`showFilterButton`/`showSearch` toggle props from FilterSidebar | Simplification — always shows these |
| Removed render props from CriterionEditor | Filters now self-contained |
| BooleanFilter/OptionFilter changed from dropdowns to radio buttons | UI simplification |
| Removed `FindScenesMobile` query and `scene_filter_ast` from scene.graphql | Removed AST from v2.5 GQL queries (AST is v3-only now) |
| Deleted `scene-mobile.graphql` | Mobile fragment removed from v2.5 |
| Added `stream`, `funscript`, `caption` to scene-slim paths | Extended scene paths |

These v2.5 changes seem intentional — stripping v2.5 back to basics while v3 takes over the advanced features (AST filtering, mobile-specific fragments, prefetching).

---

## Key Findings

### What's going well
1. Architecture principles from Phase 1 are consistently followed throughout
2. The generic `EntityListPage` config pattern successfully eliminated copy-paste across 8 entity types
3. Filter AST generalization is thorough on both frontend and backend
4. StashService.ts elimination is complete — clean Apollo 4 usage
5. Component decomposition is good — the 2354-line monolith was successfully split

### Gaps and risks
1. **`tsc --noEmit` not passing** — Plan acknowledges this. Unknown how many errors remain from bulk-copied files
2. **`make generate` unconfirmed** — Schema changes for all 8 entity types haven't been validated through codegen
3. **Acceptance criteria all unchecked** — Phases 5 and 6 list extensive acceptance criteria that are all `[ ]`. No evidence of systematic validation
4. **Scene detail page exists but wasn't in the plan scope** — `routes/scenes/$sceneId.tsx` is fully implemented with player, tabs, mutations — this appears to be ahead of Phase 7
5. **filter.ts TODO** — Line 60 has `// TODO: handle customCriteria`
6. **SelectableFilter** — Has `{/* TODO item count */}` comment

### What's next (Phase 7 readiness)
The codebase is well-positioned for Phase 7 (Detail views):
- Scene detail is already done (validates the pattern)
- All infrastructure (routing, GraphQL, cards, layout) is in place
- 6 entity detail pages remain as stubs ("coming soon")
- `DetailPage` compound component needs to be built (or may already exist given scene detail works)

---

## Recommendations before starting Phase 7

1. **Run `tsc --noEmit`** to understand the current error count and fix blockers
2. **Run `make generate`** to confirm codegen works with the schema changes
3. **Check if scene detail uses a `DetailPage` compound component** or is a one-off — if one-off, the shared primitive should be extracted before building 6 more detail pages
4. **Update plan acceptance criteria** for Phases 3-6 to reflect actual status
5. **Resolve the `customCriteria` TODO** in filter.ts if it affects any entity type's filtering
