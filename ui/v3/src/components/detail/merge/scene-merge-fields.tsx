import type * as GQL from "src/core/generated-graphql";
import { Badge } from "src/components/ui/badge";
import { defineMergeField, type AnyMergeFieldDef } from "./merge-types";
import { MergeEmptyPreview } from "./merge-field-row";

type Scene = GQL.SlimSceneDataFragment;
type SceneUpdate = GQL.SceneUpdateInput;

// ── Small helpers ────────────────────────────────────────────────────────────

const trimmed = (s: string | null | undefined) => (s ?? "").trim();
const sameStr = (a: string | null | undefined, b: string | null | undefined) =>
  trimmed(a) === trimmed(b);

function ChipList({ items }: { items: string[] }) {
  if (items.length === 0) return <MergeEmptyPreview />;
  return (
    <div className="flex flex-wrap gap-1">
      {items.map((label, i) => (
        <Badge key={`${i}:${label}`} variant="secondary">
          {label}
        </Badge>
      ))}
    </div>
  );
}

interface IdName {
  id: string;
  name: string;
}

function uniqById<T extends { id: string }>(items: T[]): T[] {
  const seen = new Set<string>();
  const out: T[] = [];
  for (const item of items) {
    if (seen.has(item.id)) continue;
    seen.add(item.id);
    out.push(item);
  }
  return out;
}

function uniqStrings(items: string[]): string[] {
  return Array.from(new Set(items.filter((s) => s.length > 0)));
}

function sameIdSet(a: IdName[], b: IdName[]): boolean {
  if (a.length !== b.length) return false;
  const aIds = new Set(a.map((x) => x.id));
  for (const x of b) if (!aIds.has(x.id)) return false;
  return true;
}

// ── Field defs ───────────────────────────────────────────────────────────────

export const SCENE_MERGE_FIELDS: readonly AnyMergeFieldDef<
  Scene,
  SceneUpdate
>[] = [
  defineMergeField<Scene, SceneUpdate, string>({
    key: "title",
    labelId: "title",
    defaultLabel: "Title",
    read: (s) => s.title ?? "",
    isEmpty: (v) => trimmed(v).length === 0,
    isEqual: sameStr,
    preview: (v) => (v.trim() ? <span>{v}</span> : <MergeEmptyPreview />),
    toUpdate: (i, v) => {
      i.title = v;
    },
  }),
  defineMergeField<Scene, SceneUpdate, string>({
    key: "code",
    labelId: "scene_code",
    defaultLabel: "Studio code",
    read: (s) => s.code ?? "",
    isEmpty: (v) => trimmed(v).length === 0,
    isEqual: sameStr,
    preview: (v) =>
      v.trim() ? (
        <span className="font-mono" data-selectable-text>
          {v}
        </span>
      ) : (
        <MergeEmptyPreview />
      ),
    toUpdate: (i, v) => {
      i.code = v;
    },
  }),
  defineMergeField<Scene, SceneUpdate, string>({
    key: "details",
    labelId: "details",
    defaultLabel: "Details",
    read: (s) => s.details ?? "",
    isEmpty: (v) => trimmed(v).length === 0,
    isEqual: sameStr,
    preview: (v) =>
      v.trim() ? (
        <div className="whitespace-pre-wrap max-h-32 overflow-y-auto">{v}</div>
      ) : (
        <MergeEmptyPreview />
      ),
    toUpdate: (i, v) => {
      i.details = v;
    },
  }),
  defineMergeField<Scene, SceneUpdate, string>({
    key: "director",
    labelId: "director",
    defaultLabel: "Director",
    read: (s) => s.director ?? "",
    isEmpty: (v) => trimmed(v).length === 0,
    isEqual: sameStr,
    preview: (v) => (v.trim() ? <span>{v}</span> : <MergeEmptyPreview />),
    toUpdate: (i, v) => {
      i.director = v;
    },
  }),
  defineMergeField<Scene, SceneUpdate, string>({
    key: "date",
    labelId: "date",
    defaultLabel: "Date",
    read: (s) => s.date ?? "",
    isEmpty: (v) => trimmed(v).length === 0,
    isEqual: sameStr,
    preview: (v) =>
      v ? <span className="tabular-nums">{v}</span> : <MergeEmptyPreview />,
    toUpdate: (i, v) => {
      i.date = v;
    },
  }),
  defineMergeField<Scene, SceneUpdate, number | null>({
    key: "rating100",
    labelId: "rating",
    defaultLabel: "Rating",
    read: (s) => s.rating100 ?? null,
    isEmpty: (v) => v == null,
    isEqual: (a, b) => a === b,
    preview: (v) =>
      v == null ? (
        <MergeEmptyPreview />
      ) : (
        <span className="tabular-nums">{v}/100</span>
      ),
    toUpdate: (i, v) => {
      i.rating100 = v;
    },
  }),
  defineMergeField<Scene, SceneUpdate, boolean>({
    key: "organized",
    labelId: "organized",
    defaultLabel: "Organized",
    // Treat `false` as "no real opinion" so it doesn't surface as a
    // conflict — only show this row when at least one entity has it
    // explicitly true.
    read: (s) => !!s.organized,
    isEmpty: (v) => v === false,
    isEqual: (a, b) => a === b,
    preview: (v) => <span>{v ? "Yes" : "No"}</span>,
    toUpdate: (i, v) => {
      i.organized = v;
    },
  }),
  defineMergeField<Scene, SceneUpdate, { id: string; name: string } | null>({
    key: "studio",
    labelId: "studio",
    defaultLabel: "Studio",
    read: (s) => (s.studio ? { id: s.studio.id, name: s.studio.name } : null),
    isEmpty: (v) => v == null,
    isEqual: (a, b) => (a?.id ?? null) === (b?.id ?? null),
    preview: (v) =>
      v ? <Badge variant="secondary">{v.name}</Badge> : <MergeEmptyPreview />,
    toUpdate: (i, v) => {
      i.studio_id = v?.id ?? null;
    },
  }),
  defineMergeField<Scene, SceneUpdate, string[]>({
    key: "urls",
    labelId: "urls",
    defaultLabel: "URLs",
    read: (s) => s.urls ?? [],
    isEmpty: (v) => v.length === 0,
    isEqual: (a, b) => a.length === b.length && a.every((x) => b.includes(x)),
    preview: (v) =>
      v.length === 0 ? (
        <MergeEmptyPreview />
      ) : (
        <ul className="flex flex-col gap-0.5 min-w-0">
          {v.map((url) => (
            <li key={url} className="truncate">
              <span
                className="text-xs text-muted-foreground"
                data-selectable-text
              >
                {url}
              </span>
            </li>
          ))}
        </ul>
      ),
    combine: (vals) => uniqStrings(vals.flat()),
    toUpdate: (i, v) => {
      i.urls = v;
    },
  }),
  defineMergeField<Scene, SceneUpdate, IdName[]>({
    key: "performers",
    labelId: "performers",
    defaultLabel: "Performers",
    read: (s) => s.performers?.map((p) => ({ id: p.id, name: p.name })) ?? [],
    isEmpty: (v) => v.length === 0,
    isEqual: sameIdSet,
    preview: (v) => <ChipList items={v.map((p) => p.name)} />,
    combine: (vals) => uniqById(vals.flat()),
    toUpdate: (i, v) => {
      i.performer_ids = v.map((p) => p.id);
    },
  }),
  defineMergeField<Scene, SceneUpdate, IdName[]>({
    key: "tags",
    labelId: "tags",
    defaultLabel: "Tags",
    read: (s) => s.tags?.map((t) => ({ id: t.id, name: t.name })) ?? [],
    isEmpty: (v) => v.length === 0,
    isEqual: sameIdSet,
    preview: (v) => <ChipList items={v.map((t) => t.name)} />,
    combine: (vals) => uniqById(vals.flat()),
    toUpdate: (i, v) => {
      i.tag_ids = v.map((t) => t.id);
    },
  }),
  defineMergeField<Scene, SceneUpdate, IdName[]>({
    key: "galleries",
    labelId: "galleries",
    defaultLabel: "Galleries",
    read: (s) =>
      s.galleries?.map((g) => ({
        id: g.id,
        name: g.title ?? g.id,
      })) ?? [],
    isEmpty: (v) => v.length === 0,
    isEqual: sameIdSet,
    preview: (v) => <ChipList items={v.map((g) => g.name)} />,
    combine: (vals) => uniqById(vals.flat()),
    toUpdate: (i, v) => {
      i.gallery_ids = v.map((g) => g.id);
    },
  }),
  defineMergeField<
    Scene,
    SceneUpdate,
    Array<{ id: string; name: string; scene_index: number | null }>
  >({
    key: "groups",
    labelId: "groups",
    defaultLabel: "Groups",
    read: (s) =>
      s.groups?.map((sg) => ({
        id: sg.group.id,
        name: sg.group.name,
        scene_index: sg.scene_index ?? null,
      })) ?? [],
    isEmpty: (v) => v.length === 0,
    isEqual: (a, b) =>
      a.length === b.length &&
      a.every((x) =>
        b.some((y) => y.id === x.id && y.scene_index === x.scene_index),
      ),
    preview: (v) => (
      <ChipList
        items={v.map((g) =>
          g.scene_index != null ? `${g.name} (#${g.scene_index})` : g.name,
        )}
      />
    ),
    combine: (vals) => {
      const seen = new Set<string>();
      const out: Array<{
        id: string;
        name: string;
        scene_index: number | null;
      }> = [];
      for (const list of vals) {
        for (const g of list) {
          if (seen.has(g.id)) continue;
          seen.add(g.id);
          out.push(g);
        }
      }
      return out;
    },
    toUpdate: (i, v) => {
      i.groups = v.map((g) => ({
        group_id: g.id,
        scene_index: g.scene_index,
      }));
    },
  }),
];
