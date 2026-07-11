import { useIntl } from "react-intl";
import type * as GQL from "src/core/generated-graphql";
import { Badge } from "src/components/ui/badge";
import { defineMergeField, type AnyMergeFieldDef } from "./merge-types";
import { MergeEmptyPreview } from "./merge-field-row";
import { formatGender } from "src/utils/enum-labels";

type Performer = GQL.PerformerDataFragment;
type PerformerUpdate = GQL.PerformerUpdateInput;

const trimmed = (s: string | null | undefined) => (s ?? "").trim();
const sameStr = (a: string | null | undefined, b: string | null | undefined) =>
  trimmed(a) === trimmed(b);

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
function sameStrSet(a: string[], b: string[]): boolean {
  if (a.length !== b.length) return false;
  const aSet = new Set(a);
  for (const x of b) if (!aSet.has(x)) return false;
  return true;
}

// Tiny wrapper so the merge field's static `preview` lambda can pull
// the localised gender label from intl context — the def list is
// constructed at module scope so it can't call `useIntl` itself.
function GenderPreview({ value }: { value: GQL.GenderEnum | null }) {
  const intl = useIntl();
  if (!value) return <MergeEmptyPreview />;
  return <span>{formatGender(intl, value)}</span>;
}

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

// Aliases are returned as `{ alias, ignore_auto_tag }`. For preview /
// equality we treat them as a strings-with-ignore-flag tuple keyed
// on the alias text (case-insensitive). Combine drops dupes.
interface PerformerAlias {
  alias: string;
  ignore_auto_tag: boolean;
}

// String-typed scalars all share this shape — packaged as a small
// helper to keep the def list scannable. Project assigns directly
// onto the matching update-input key.
function stringField(
  key: keyof PerformerUpdate & string,
  labelId: string,
  defaultLabel: string,
  read: (p: Performer) => string | null | undefined,
): AnyMergeFieldDef<Performer, PerformerUpdate> {
  return defineMergeField<Performer, PerformerUpdate, string>({
    key,
    labelId,
    defaultLabel,
    read: (p) => read(p) ?? "",
    isEmpty: (v) => trimmed(v).length === 0,
    isEqual: sameStr,
    preview: (v) => (v.trim() ? <span>{v}</span> : <MergeEmptyPreview />),
    toUpdate: (i, v) => {
      // Cast: the keyof guarantees this assignment is valid for
      // string-typed fields on PerformerUpdateInput; helper avoids
      // duplicating one-line projections across half the def list.
      (i as Record<string, unknown>)[key] = v;
    },
  });
}

export const PERFORMER_MERGE_FIELDS: readonly AnyMergeFieldDef<
  Performer,
  PerformerUpdate
>[] = [
  stringField("name", "canonical_name", "Canonical name", (p) => p.name),
  stringField(
    "disambiguation",
    "disambiguation",
    "Disambiguation",
    (p) => p.disambiguation,
  ),
  defineMergeField<Performer, PerformerUpdate, GQL.GenderEnum | null>({
    key: "gender",
    labelId: "gender",
    defaultLabel: "Gender",
    read: (p) => p.gender ?? null,
    isEmpty: (v) => v == null,
    isEqual: (a, b) => a === b,
    preview: (v) => <GenderPreview value={v} />,
    toUpdate: (i, v) => {
      i.gender = v;
    },
  }),
  stringField("birthdate", "birthdate", "Birthdate", (p) => p.birthdate),
  stringField("death_date", "death_date", "Death date", (p) => p.death_date),
  stringField("ethnicity", "ethnicity", "Ethnicity", (p) => p.ethnicity),
  stringField("country", "country", "Country", (p) => p.country),
  stringField("eye_color", "eye_color", "Eye color", (p) => p.eye_color),
  stringField("hair_color", "hair_color", "Hair color", (p) => p.hair_color),
  defineMergeField<Performer, PerformerUpdate, number | null>({
    key: "height_cm",
    labelId: "height_cm",
    defaultLabel: "Height (cm)",
    read: (p) => p.height_cm ?? null,
    isEmpty: (v) => v == null,
    isEqual: (a, b) => a === b,
    preview: (v) =>
      v == null ? (
        <MergeEmptyPreview />
      ) : (
        <span className="tabular-nums">{v} cm</span>
      ),
    toUpdate: (i, v) => {
      i.height_cm = v;
    },
  }),
  defineMergeField<Performer, PerformerUpdate, number | null>({
    key: "weight",
    labelId: "weight",
    defaultLabel: "Weight",
    read: (p) => p.weight ?? null,
    isEmpty: (v) => v == null,
    isEqual: (a, b) => a === b,
    preview: (v) =>
      v == null ? (
        <MergeEmptyPreview />
      ) : (
        <span className="tabular-nums">{v} kg</span>
      ),
    toUpdate: (i, v) => {
      i.weight = v;
    },
  }),
  stringField(
    "measurements",
    "measurements",
    "Measurements",
    (p) => p.measurements,
  ),
  stringField("tattoos", "tattoos", "Tattoos", (p) => p.tattoos),
  stringField("piercings", "piercings", "Piercings", (p) => p.piercings),
  stringField(
    "career_start",
    "career_start_year",
    "Career start",
    (p) => p.career_start,
  ),
  stringField(
    "career_end",
    "career_end_year",
    "Career end",
    (p) => p.career_end,
  ),
  defineMergeField<Performer, PerformerUpdate, number | null>({
    key: "rating100",
    labelId: "rating",
    defaultLabel: "Rating",
    read: (p) => p.rating100 ?? null,
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
  stringField("details", "details", "Details", (p) => p.details),
  defineMergeField<Performer, PerformerUpdate, string[]>({
    key: "urls",
    labelId: "urls",
    defaultLabel: "URLs",
    read: (p) => p.urls ?? [],
    isEmpty: (v) => v.length === 0,
    isEqual: sameStrSet,
    preview: (v) =>
      v.length === 0 ? (
        <MergeEmptyPreview />
      ) : (
        <ul className="flex flex-col gap-0.5 min-w-0">
          {v.map((url) => (
            <li key={url} className="truncate">
              <span className="text-xs text-muted-foreground">{url}</span>
            </li>
          ))}
        </ul>
      ),
    combine: (vals) => uniqStrings(vals.flat()),
    toUpdate: (i, v) => {
      i.urls = v;
    },
  }),
  defineMergeField<Performer, PerformerUpdate, PerformerAlias[]>({
    key: "aliases",
    labelId: "names_and_aliases",
    defaultLabel: "Names and aliases",
    read: (p) =>
      p.aliases?.map((a) => ({
        alias: a.alias,
        ignore_auto_tag: a.ignore_auto_tag,
      })) ?? [],
    isEmpty: (v) => v.length === 0,
    isEqual: (a, b) => {
      if (a.length !== b.length) return false;
      const key = (x: PerformerAlias) =>
        `${x.alias.toLowerCase()}|${x.ignore_auto_tag ? 1 : 0}`;
      const aSet = new Set(a.map(key));
      for (const x of b) if (!aSet.has(key(x))) return false;
      return true;
    },
    preview: (v) => <ChipList items={v.map((a) => a.alias)} />,
    combine: (vals) => {
      const seen = new Set<string>();
      const out: PerformerAlias[] = [];
      for (const list of vals) {
        for (const a of list) {
          const k = a.alias.toLowerCase();
          if (seen.has(k)) continue;
          seen.add(k);
          out.push(a);
        }
      }
      return out;
    },
    toUpdate: (i, v) => {
      i.aliases = v.map((a) => ({
        alias: a.alias,
        ignore_auto_tag: a.ignore_auto_tag,
      }));
    },
  }),
  defineMergeField<Performer, PerformerUpdate, IdName[]>({
    key: "tags",
    labelId: "tags",
    defaultLabel: "Tags",
    read: (p) => p.tags?.map((t) => ({ id: t.id, name: t.name })) ?? [],
    isEmpty: (v) => v.length === 0,
    isEqual: sameIdSet,
    preview: (v) => <ChipList items={v.map((t) => t.name)} />,
    combine: (vals) => uniqById(vals.flat()),
    toUpdate: (i, v) => {
      i.tag_ids = v.map((t) => t.id);
    },
  }),
  defineMergeField<Performer, PerformerUpdate, string | null>({
    key: "image_path",
    labelId: "image",
    defaultLabel: "Image",
    read: (p) => p.image_path ?? null,
    isEmpty: (v) => !v,
    isEqual: (a, b) => a === b,
    preview: (v) =>
      v ? (
        <img
          src={v}
          alt=""
          loading="lazy"
          className="max-h-32 rounded object-cover"
        />
      ) : (
        <MergeEmptyPreview />
      ),
    // Project as `image` (a URL string the backend resolves and
    // re-stores as the destination's image). The PerformerUpdateInput
    // accepts either a URL or a base64 data URL — the existing image
    // path is a URL the server already trusts.
    toUpdate: (i, v) => {
      i.image = v;
    },
  }),
];
