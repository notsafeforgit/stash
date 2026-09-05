import { useIntl } from "react-intl";
import type * as GQL from "src/core/generated-graphql";
import { Badge } from "src/components/ui/badge";
import { defineMergeField, type AnyMergeFieldDef } from "./merge-types";
import { MergeEmptyPreview } from "./merge-field-row";
import { formatCircumcised, formatGender } from "src/utils/enum-labels";

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

function CircumcisedPreview({ value }: { value: GQL.CircumcisedEnum | null }) {
  const intl = useIntl();
  if (!value) return <MergeEmptyPreview />;
  return <span>{formatCircumcised(intl, value)}</span>;
}

function BooleanPreview({ value }: { value: boolean }) {
  const intl = useIntl();
  return (
    <span>
      {intl.formatMessage({
        id: value ? "yes" : "no",
        defaultMessage: value ? "Yes" : "No",
      })}
    </span>
  );
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

type PerformerStashID = Performer["stash_ids"][number];
type CustomFieldMap = Record<string, unknown>;

function sameStashIDSet(a: PerformerStashID[], b: PerformerStashID[]) {
  if (a.length !== b.length) return false;
  const aSet = new Set(a.map((v) => `${v.endpoint}\n${v.stash_id}`));
  return b.every((v) => aSet.has(`${v.endpoint}\n${v.stash_id}`));
}

function combineStashIDs(values: PerformerStashID[][]) {
  const byEndpoint = new Map<string, PerformerStashID>();
  for (const list of values) {
    for (const value of list) {
      if (!byEndpoint.has(value.endpoint))
        byEndpoint.set(value.endpoint, value);
    }
  }
  return Array.from(byEndpoint.values());
}

function sameCustomFieldValue(a: unknown, b: unknown) {
  return Object.is(a, b);
}

function sameCustomFields(a: CustomFieldMap, b: CustomFieldMap) {
  const aKeys = Object.keys(a);
  const bKeys = Object.keys(b);
  return (
    aKeys.length === bKeys.length &&
    aKeys.every(
      (key) => Object.hasOwn(b, key) && sameCustomFieldValue(a[key], b[key]),
    )
  );
}

function combineCustomFields(values: CustomFieldMap[]) {
  const combined: CustomFieldMap = {};
  for (const value of values) {
    for (const [key, next] of Object.entries(value)) {
      if (!Object.hasOwn(combined, key)) combined[key] = next;
    }
  }
  return combined;
}

function customFieldLabels(value: CustomFieldMap) {
  return Object.keys(value)
    .sort((a, b) => a.localeCompare(b))
    .map((key) => `${key}: ${String(value[key])}`);
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
  defineMergeField<Performer, PerformerUpdate, GQL.CircumcisedEnum | null>({
    key: "circumcised",
    labelId: "circumcised",
    defaultLabel: "Circumcised",
    read: (p) => p.circumcised ?? null,
    isEmpty: (v) => v == null,
    isEqual: (a, b) => a === b,
    preview: (v) => <CircumcisedPreview value={v} />,
    toUpdate: (i, v) => {
      i.circumcised = v;
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
  defineMergeField<Performer, PerformerUpdate, number | null>({
    key: "penis_length",
    labelId: "penis_length",
    defaultLabel: "Penis length",
    read: (p) => p.penis_length ?? null,
    isEmpty: (v) => v == null,
    isEqual: (a, b) => a === b,
    preview: (v) =>
      v == null ? (
        <MergeEmptyPreview />
      ) : (
        <span className="tabular-nums">{v} cm</span>
      ),
    toUpdate: (i, v) => {
      i.penis_length = v;
    },
  }),
  stringField(
    "measurements",
    "measurements",
    "Measurements",
    (p) => p.measurements,
  ),
  stringField("fake_tits", "fake_tits", "Fake tits", (p) => p.fake_tits),
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
  defineMergeField<Performer, PerformerUpdate, boolean>({
    key: "favorite",
    labelId: "favorite",
    defaultLabel: "Favourite",
    read: (p) => p.favorite,
    isEmpty: (v) => !v,
    isEqual: (a, b) => a === b,
    preview: (v) => <BooleanPreview value={v} />,
    toUpdate: (i, v) => {
      i.favorite = v;
    },
  }),
  defineMergeField<Performer, PerformerUpdate, boolean>({
    key: "ignore_auto_tag",
    labelId: "ignore_auto_tag",
    defaultLabel: "Ignore auto tag",
    read: (p) => p.ignore_auto_tag,
    isEmpty: (v) => !v,
    isEqual: (a, b) => a === b,
    preview: (v) => <BooleanPreview value={v} />,
    toUpdate: (i, v) => {
      i.ignore_auto_tag = v;
    },
  }),
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
  defineMergeField<Performer, PerformerUpdate, PerformerStashID[]>({
    key: "stash_ids",
    labelId: "stash_ids",
    defaultLabel: "Stash IDs",
    read: (p) => p.stash_ids ?? [],
    isEmpty: (v) => v.length === 0,
    isEqual: sameStashIDSet,
    preview: (v) => (
      <div data-selectable-text>
        <ChipList items={v.map((s) => `${s.endpoint}: ${s.stash_id}`)} />
      </div>
    ),
    combine: combineStashIDs,
    toUpdate: (i, v) => {
      i.stash_ids = v.map((s) => ({
        endpoint: s.endpoint,
        stash_id: s.stash_id,
        updated_at: s.updated_at,
      }));
    },
  }),
  defineMergeField<Performer, PerformerUpdate, CustomFieldMap>({
    key: "custom_fields",
    labelId: "custom_fields.title",
    defaultLabel: "Custom fields",
    read: (p) => p.custom_fields ?? {},
    isEmpty: (v) => Object.keys(v).length === 0,
    isEqual: sameCustomFields,
    preview: (v) => <ChipList items={customFieldLabels(v)} />,
    combine: combineCustomFields,
    toUpdate: (i, v) => {
      i.custom_fields = { full: v };
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
