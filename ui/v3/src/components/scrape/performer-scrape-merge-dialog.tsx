import React, { useMemo, useState } from "react";
import { useIntl } from "react-intl";
import { useMutation } from "@apollo/client/react";
import * as GQL from "src/core/generated-graphql";
import { GenderEnum, CircumcisedEnum } from "src/core/generated-graphql";
import { useToast } from "src/hooks/toast";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "src/components/ui/dialog";
import { Button } from "src/components/ui/button";
import { Spinner } from "src/components/ui/spinner";
import { ToggleGroup, ToggleGroupItem } from "src/components/ui/toggle-group";
import type {
  AliasEntry,
  PerformerFormValues,
} from "src/components/detail/performer-edit-form";
import { formatGender, formatCircumcised } from "src/utils/enum-labels";
import type { ScrapeSource } from "./use-available-scrapers";
import {
  ScrapedTagRow,
  type ScrapedTagResolution,
  defaultResolution,
} from "./scraped-tag-row";
import {
  ImageMergeRow,
  type MergeMode,
  RowShell,
  emptyOrText,
} from "./scrape-merge-shared";

type ScrapedPerformer = GQL.ScrapedPerformerDataFragment;

// ── Helpers ───────────────────────────────────────────────────────────────────

const VALID_GENDERS = new Set<string>(Object.values(GenderEnum));
const VALID_CIRCUMCISED = new Set<string>(Object.values(CircumcisedEnum));

function parseGender(s: string | null | undefined): GenderEnum | "" {
  if (!s) return "";
  const upper = s.toUpperCase().replace(/[\s-]/g, "_");
  return VALID_GENDERS.has(upper) ? (upper as GenderEnum) : "";
}

function parseCircumcised(s: string | null | undefined): CircumcisedEnum | "" {
  if (!s) return "";
  const upper = s.toUpperCase();
  return VALID_CIRCUMCISED.has(upper) ? (upper as CircumcisedEnum) : "";
}

/** Scraped height arrives as a free-form string (often "175 cm" or "175"). */
function parseHeightCm(s: string | null | undefined): string {
  if (!s) return "";
  const m = s.match(/(\d+(?:\.\d+)?)/);
  return m ? String(Math.round(parseFloat(m[1]))) : "";
}

function parseScrapedAliases(s: string | null | undefined): AliasEntry[] {
  if (!s) return [];
  return s
    .split(",")
    .map((a) => a.trim())
    .filter(Boolean)
    .map((alias) => ({ alias, ignore_auto_tag: false }));
}

interface SimpleStringFieldDef<K extends keyof PerformerFormValues> {
  field: K;
  label: string;
  scrapedValue: string | null | undefined;
  parse?: (s: string | null | undefined) => PerformerFormValues[K];
  /** Override how the projected value renders. Defaults to String(value). */
  format?: (v: PerformerFormValues[K]) => string;
}

// ── Component ─────────────────────────────────────────────────────────────────

interface PerformerScrapeMergeDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  current: PerformerFormValues;
  /** The performer's existing image URL (a server-resolvable path).
   *  Separate from `current.image` because the form's `image` field
   *  is the *new* image to upload — null means "don't change" — so
   *  it can't double as the display value for the current image.
   *  When omitted the row falls back to `current.image`, which keeps
   *  the create-flow path (no existing record yet) showing whatever
   *  data-URL the form already has staged. */
  existingImagePath?: string | null;
  scraped: ScrapedPerformer | null;
  /** Source the scrape came from. When this is a stash-box and the scraped
   *  payload includes a remote_site_id, the dialog offers a stash-id row so
   *  the user can attach the matched id to the form. */
  source?: ScrapeSource | null;
  onApply: (patch: Partial<PerformerFormValues>) => void;
}

export function PerformerScrapeMergeDialog({
  open,
  onOpenChange,
  current,
  existingImagePath,
  scraped,
  source,
  onApply,
}: PerformerScrapeMergeDialogProps) {
  const intl = useIntl();
  const toast = useToast();

  // Build the row config from the scraped payload. Each entry knows its label,
  // current display, scraped display, accept-state and how to project the
  // accepted scraped value into a PerformerFormValues patch slice. Multi-value
  // rows (urls/aliases) also receive a mergeMode + setter so the user can
  // pick between unioning with current and replacing it wholesale.
  type RowRenderArgs = {
    accepted: boolean;
    setAccepted: (v: boolean) => void;
    mergeMode: MergeMode;
    setMergeMode: (m: MergeMode) => void;
    /** Cross-row hint used by the alias row only: when the scraped name
     *  differs from the current name, the "loser" of that decision is
     *  surfaced as an alias addition. Computed at render/apply time
     *  from the name row's accept state. */
    extraAliasAddition?: AliasEntry;
  };
  type PreparedRow = {
    key: string;
    render: (args: RowRenderArgs) => React.ReactNode;
    apply: (
      patch: Partial<PerformerFormValues>,
      mergeMode: MergeMode,
      extraAliasAddition?: AliasEntry,
    ) => void;
  };

  const stringRows = useMemo<PreparedRow[]>(() => {
    if (!scraped) return [];
    const defs: SimpleStringFieldDef<keyof PerformerFormValues>[] = [
      {
        field: "name",
        label: intl.formatMessage({
          id: "canonical_name",
          defaultMessage: "Canonical name",
        }),
        scrapedValue: scraped.name,
      },
      {
        field: "disambiguation",
        label: "Disambiguation",
        scrapedValue: scraped.disambiguation,
      },
      {
        field: "gender",
        label: "Gender",
        scrapedValue: scraped.gender,
        parse: (s) =>
          parseGender(s) as PerformerFormValues[keyof PerformerFormValues],
        format: (v) => formatGender(intl, v as string),
      },
      {
        field: "birthdate",
        label: "Birthdate",
        scrapedValue: scraped.birthdate,
      },
      {
        field: "death_date",
        label: "Death date",
        scrapedValue: scraped.death_date,
      },
      { field: "country", label: "Country", scrapedValue: scraped.country },
      {
        field: "ethnicity",
        label: "Ethnicity",
        scrapedValue: scraped.ethnicity,
      },
      {
        field: "hair_color",
        label: "Hair colour",
        scrapedValue: scraped.hair_color,
      },
      {
        field: "eye_color",
        label: "Eye colour",
        scrapedValue: scraped.eye_color,
      },
      {
        field: "height_cm",
        label: "Height (cm)",
        scrapedValue: scraped.height,
        parse: (s) =>
          parseHeightCm(s) as PerformerFormValues[keyof PerformerFormValues],
      },
      { field: "weight", label: "Weight (kg)", scrapedValue: scraped.weight },
      {
        field: "measurements",
        label: "Measurements",
        scrapedValue: scraped.measurements,
      },
      {
        field: "penis_length",
        label: "Penis length (cm)",
        scrapedValue: scraped.penis_length,
      },
      {
        field: "circumcised",
        label: "Circumcised",
        scrapedValue: scraped.circumcised,
        parse: (s) =>
          parseCircumcised(s) as PerformerFormValues[keyof PerformerFormValues],
        format: (v) => formatCircumcised(intl, v as string),
      },
      {
        field: "fake_tits",
        label: "Fake tits",
        scrapedValue: scraped.fake_tits,
      },
      {
        field: "career_start",
        label: "Career start",
        scrapedValue: scraped.career_start,
      },
      {
        field: "career_end",
        label: "Career end",
        scrapedValue: scraped.career_end,
      },
      { field: "tattoos", label: "Tattoos", scrapedValue: scraped.tattoos },
      {
        field: "piercings",
        label: "Piercings",
        scrapedValue: scraped.piercings,
      },
      { field: "details", label: "Details", scrapedValue: scraped.details },
    ];

    return defs.flatMap((def) => {
      const newValue = def.parse
        ? def.parse(def.scrapedValue)
        : ((def.scrapedValue ?? "") as PerformerFormValues[typeof def.field]);
      const currentValue = current[def.field];
      const newDisplay = def.format
        ? def.format(newValue)
        : String(newValue ?? "");
      const currentDisplay = def.format
        ? def.format(currentValue)
        : String(currentValue ?? "");
      // Skip when scraped contributes nothing or already matches current.
      if (!newDisplay) return [];
      if (newDisplay === currentDisplay) return [];

      return [
        {
          key: def.field,
          render: ({ accepted, setAccepted }) => (
            <RowShell
              key={def.field}
              label={def.label}
              accepted={accepted}
              onAcceptedChange={setAccepted}
              current={emptyOrText(currentDisplay)}
              scraped={newDisplay}
            />
          ),
          apply: (patch) => {
            (patch as Record<string, unknown>)[def.field] = newValue;
          },
        } satisfies PreparedRow,
      ];
    });
  }, [scraped, current, intl]);

  // Aliases — parse comma-separated; user picks merge (union) vs overwrite.
  // The row also surfaces the "loser" name (current vs scraped) as an alias
  // addition when those two names differ, so that whichever name the user
  // doesn't pick survives as a searchable alias on the performer.
  const aliasRow = useMemo<PreparedRow | null>(() => {
    const baseIncoming = scraped?.aliases
      ? parseScrapedAliases(scraped.aliases)
      : [];
    const existingNamesLC = new Set(
      current.aliases.map((a) => a.alias.toLowerCase()),
    );
    const baseIncomingLC = new Set(
      baseIncoming.map((a) => a.alias.toLowerCase()),
    );
    const baseAdditions = baseIncoming.filter(
      (a) => !existingNamesLC.has(a.alias.toLowerCase()),
    );
    const sameSet =
      baseAdditions.length === 0 &&
      current.aliases.length === baseIncomingLC.size &&
      current.aliases.every((a) => baseIncomingLC.has(a.alias.toLowerCase()));

    // Whether the name row will exist (and thus might contribute a loser
    // name). The alias row needs to render in that case even when the
    // scraped payload has no aliases of its own.
    const scrapedName = scraped?.name?.trim() ?? "";
    const currentName = current.name.trim();
    const nameDiffers =
      !!scrapedName && scrapedName.toLowerCase() !== currentName.toLowerCase();

    if (sameSet && !nameDiffers) return null;
    if (baseIncoming.length === 0 && !nameDiffers) return null;

    function withExtra(extra: AliasEntry | undefined): {
      incoming: AliasEntry[];
      additions: AliasEntry[];
    } {
      if (!extra) {
        return { incoming: baseIncoming, additions: baseAdditions };
      }
      const k = extra.alias.toLowerCase();
      if (existingNamesLC.has(k)) {
        return { incoming: baseIncoming, additions: baseAdditions };
      }
      if (baseIncomingLC.has(k)) {
        const withPolicy = (entry: AliasEntry) =>
          entry.alias.toLowerCase() === k
            ? {
                ...entry,
                ignore_auto_tag: entry.ignore_auto_tag || extra.ignore_auto_tag,
              }
            : entry;
        return {
          incoming: baseIncoming.map(withPolicy),
          additions: baseAdditions.map(withPolicy),
        };
      }
      return {
        incoming: [...baseIncoming, extra],
        additions: [...baseAdditions, extra],
      };
    }

    // Hide the merge-mode toggle (and force "merge" semantics in apply)
    // when the scraped payload contributes no aliases of its own. There
    // is nothing to overwrite with — the loser name on its own should
    // not be allowed to wipe the current alias list.
    const allowMergeMode =
      current.aliases.length > 0 && baseIncoming.length > 0;

    return {
      key: "aliases",
      render: ({
        accepted,
        setAccepted,
        mergeMode,
        setMergeMode,
        extraAliasAddition,
      }) => {
        const { incoming, additions } = withExtra(extraAliasAddition);
        const effectiveMode: MergeMode = allowMergeMode ? mergeMode : "merge";
        return (
          <RowShell
            key="aliases"
            label={intl.formatMessage({
              id: "names_and_aliases",
              defaultMessage: "Names and aliases",
            })}
            accepted={accepted}
            onAcceptedChange={setAccepted}
            mergeMode={allowMergeMode ? mergeMode : undefined}
            onMergeModeChange={allowMergeMode ? setMergeMode : undefined}
            current={
              current.aliases.length === 0 ? (
                emptyOrText(null)
              ) : (
                <span className="break-words">
                  {current.aliases.map((a) => a.alias).join(", ")}
                </span>
              )
            }
            scraped={
              effectiveMode === "overwrite" || current.aliases.length === 0 ? (
                <span className="break-words">
                  {incoming.map((a) => a.alias).join(", ")}
                </span>
              ) : additions.length === 0 ? (
                <span className="italic text-muted-foreground">
                  {intl.formatMessage({
                    id: "scrape.no_additions",
                    defaultMessage:
                      "Nothing to add — switch to Overwrite to drop the others.",
                  })}
                </span>
              ) : (
                <span className="break-words">
                  <span className="text-emerald-500 mr-1 select-none">+</span>
                  {additions.map((a) => a.alias).join(", ")}
                </span>
              )
            }
          />
        );
      },
      apply: (patch, mergeMode, extraAliasAddition) => {
        const { incoming, additions } = withExtra(extraAliasAddition);
        const effectiveMode: MergeMode = allowMergeMode ? mergeMode : "merge";
        if (effectiveMode === "overwrite") {
          patch.aliases = incoming;
        } else {
          patch.aliases = [...current.aliases, ...additions];
        }
      },
    };
  }, [scraped, current.aliases, current.name, intl]);

  // URLs — user picks merge (union) vs overwrite.
  const urlRow = useMemo<PreparedRow | null>(() => {
    const incoming = scraped?.urls ?? [];
    if (incoming.length === 0) return null;
    const existing = new Set(current.urls);
    const incomingSet = new Set(incoming);
    const additions = incoming.filter((u) => !existing.has(u));
    const sameSet =
      additions.length === 0 &&
      current.urls.length === incomingSet.size &&
      current.urls.every((u) => incomingSet.has(u));
    if (sameSet) return null;
    return {
      key: "urls",
      render: ({ accepted, setAccepted, mergeMode, setMergeMode }) => (
        <RowShell
          key="urls"
          label="URLs"
          accepted={accepted}
          onAcceptedChange={setAccepted}
          mergeMode={current.urls.length > 0 ? mergeMode : undefined}
          onMergeModeChange={current.urls.length > 0 ? setMergeMode : undefined}
          current={
            current.urls.length === 0 ? (
              emptyOrText(null)
            ) : (
              <ul className="flex flex-col gap-0.5">
                {current.urls.map((u) => (
                  <li key={u} className="break-all text-xs">
                    {u}
                  </li>
                ))}
              </ul>
            )
          }
          scraped={
            mergeMode === "overwrite" || current.urls.length === 0 ? (
              <ul className="flex flex-col gap-0.5">
                {incoming.map((u) => (
                  <li key={u} className="break-all text-xs">
                    {u}
                  </li>
                ))}
              </ul>
            ) : additions.length === 0 ? (
              <span className="italic text-muted-foreground text-xs">
                {intl.formatMessage({
                  id: "scrape.no_additions",
                  defaultMessage:
                    "Nothing to add — switch to Overwrite to drop the others.",
                })}
              </span>
            ) : (
              <ul className="flex flex-col gap-0.5">
                {additions.map((u) => (
                  <li key={u} className="break-all text-xs">
                    <span className="text-emerald-500 mr-1 select-none">+</span>
                    {u}
                  </li>
                ))}
              </ul>
            )
          }
        />
      ),
      apply: (patch, mergeMode) => {
        if (mergeMode === "overwrite") {
          patch.urls = incoming;
        } else {
          patch.urls = [...current.urls, ...additions];
        }
      },
    };
  }, [scraped, current.urls, intl]);

  // Image is rendered as a separate component (its own state + lightbox), so
  // it isn't routed through the PreparedRow plumbing. The dialog still tracks
  // accept-state and selected-index for it.
  const scrapedImages = scraped?.images ?? [];
  // `current.image` is only set when the user has staged a new upload in the
  // form (data-URL or the like) — it's null in the common case where the
  // performer just has a server-side image. `existingImagePath` is the
  // canonical "this performer's current image" URL; fall back to
  // `current.image` so the create flow (no record yet) still works.
  const currentImageDisplay = existingImagePath ?? current.image ?? null;
  const showImageRow =
    scrapedImages.length > 0 &&
    !(
      currentImageDisplay &&
      scrapedImages.length === 1 &&
      currentImageDisplay === scrapedImages[0]
    );
  const [selectedImageIndex, setSelectedImageIndex] = useState(0);
  // Tracks the image-row lightbox so the outer Dialog can swallow Escape
  // while the lightbox is the foreground overlay (otherwise both close).
  const [lightboxOpen, setLightboxOpen] = useState(false);

  // Stash-id row — only when the scrape came from a stash-box and the result
  // has a remote_site_id. Adds (or updates) `{endpoint, stash_id}` in the
  // form's stash_ids list. No "merge mode" — there's only one id per
  // endpoint, and we don't want to surface either choice for that.
  const stashIdRow = useMemo<PreparedRow | null>(() => {
    if (!scraped) return null;
    if (source?.kind !== "stashBox") return null;
    if (!scraped.remote_site_id) return null;
    const endpoint = source.endpoint;
    const newId = scraped.remote_site_id;
    const existing = current.stash_ids.find((s) => s.endpoint === endpoint);
    if (existing && existing.stash_id === newId) return null;

    return {
      key: "stash_ids",
      render: ({ accepted, setAccepted }) => (
        <RowShell
          key="stash_ids"
          label="Stash ID"
          accepted={accepted}
          onAcceptedChange={setAccepted}
          current={
            existing ? (
              <span className="break-all text-xs">{existing.stash_id}</span>
            ) : (
              emptyOrText(null)
            )
          }
          scraped={<span className="break-all text-xs">{newId}</span>}
        />
      ),
      apply: (patch) => {
        const next = [...current.stash_ids];
        const idx = next.findIndex((s) => s.endpoint === endpoint);
        if (idx >= 0) next[idx] = { endpoint, stash_id: newId };
        else next.push({ endpoint, stash_id: newId });
        patch.stash_ids = next;
      },
    };
  }, [scraped, source, current.stash_ids]);

  const simpleRows = useMemo(
    () =>
      [...stringRows, aliasRow, urlRow, stashIdRow].filter(
        (r): r is PreparedRow => r !== null,
      ),
    [stringRows, aliasRow, urlRow, stashIdRow],
  );

  // Per-row accept state, keyed by row key. Defaults to accepted.
  const [accepted, setAccepted] = useState<Record<string, boolean>>({});
  function isAccepted(key: string): boolean {
    return accepted[key] ?? true;
  }
  function setAcceptedFor(key: string, value: boolean) {
    setAccepted((curr) => ({ ...curr, [key]: value }));
  }

  // Per-row merge mode for multi-value fields (urls / aliases / tags).
  // Defaults to "merge" — overwrite is opt-in.
  const [mergeModes, setMergeModes] = useState<Record<string, MergeMode>>({});
  function getMergeMode(key: string): MergeMode {
    return mergeModes[key] ?? "merge";
  }
  function setMergeModeFor(key: string, mode: MergeMode) {
    setMergeModes((curr) => ({ ...curr, [key]: mode }));
  }

  // Tag resolutions — these are not a single accept toggle; the user picks
  // skip / pick existing / create per scraped tag. New tags additive on top
  // of `current.tags`.
  const scrapedTags = scraped?.tags ?? [];
  const [tagResolutions, setTagResolutions] = useState<
    Record<number, ScrapedTagResolution>
  >({});

  function getTagRes(i: number): ScrapedTagResolution {
    return tagResolutions[i] ?? defaultResolution(scrapedTags[i]);
  }
  function setTagRes(i: number, next: ScrapedTagResolution) {
    setTagResolutions((curr) => ({ ...curr, [i]: next }));
  }

  // Reset state every time the dialog reopens with a fresh payload — without
  // this, a previous scrape's accept toggles bleed into the next one.
  React.useEffect(() => {
    if (open) {
      setAccepted({});
      setTagResolutions({});
      setMergeModes({});
      setSelectedImageIndex(0);
      setLightboxOpen(false);
    }
  }, [open]);

  const [createTag] = useMutation(GQL.TagCreateDocument);
  const [applying, setApplying] = useState(false);

  // The "loser" name is whichever of (current name, scraped name) the user
  // does not pick — surfaced as an alias addition so neither name is
  // silently lost when they differ. Returns `undefined` when the names
  // match (or the scraped payload has no name) — in that case the name
  // row doesn't render and there's nothing to fold in.
  function autoTagPolicyFor(name: string): boolean {
    const key = name.trim().toLowerCase();
    if (key === current.name.trim().toLowerCase()) {
      return current.ignore_primary_name_auto_tag;
    }
    return (
      current.aliases.find((alias) => alias.alias.trim().toLowerCase() === key)
        ?.ignore_auto_tag ?? false
    );
  }

  function computeLoserName(): AliasEntry | undefined {
    const scrapedName = scraped?.name?.trim() ?? "";
    const currentName = current.name.trim();
    if (
      !scrapedName ||
      scrapedName.toLowerCase() === currentName.toLowerCase()
    ) {
      return undefined;
    }
    const alias = isAccepted("name") ? currentName : scrapedName;
    return {
      alias,
      ignore_auto_tag: autoTagPolicyFor(alias),
    };
  }

  async function handleApply() {
    if (!scraped) return;
    setApplying(true);
    try {
      const patch: Partial<PerformerFormValues> = {};

      const loserName = computeLoserName();
      for (const row of simpleRows) {
        if (isAccepted(row.key)) {
          row.apply(
            patch,
            getMergeMode(row.key),
            row.key === "aliases" ? loserName : undefined,
          );
        }
      }
      if (patch.name) {
        patch.ignore_primary_name_auto_tag = autoTagPolicyFor(patch.name);
      }

      // Image lives outside the PreparedRow plumbing. Apply if accepted and
      // there is at least one scraped image to choose from.
      if (showImageRow && isAccepted("image")) {
        const safeIndex = Math.max(
          0,
          Math.min(selectedImageIndex, scrapedImages.length - 1),
        );
        patch.image = scrapedImages[safeIndex];
      }

      // Resolve tag picks. Run all "create" mutations in parallel, fold the
      // resulting ids into a single set of new EntityOptions, then union with
      // current.tags. Tags the user marked "skip" simply contribute nothing.
      const existingIds = new Set(current.tags.map((t) => t.id));
      const additions = new Map<string, { id: string; name: string }>();

      const tagsMode = getMergeMode("tags");
      const createPromises: Promise<void>[] = [];
      for (let i = 0; i < scrapedTags.length; i++) {
        const res = getTagRes(i);
        if (res.kind === "skip") continue;
        if (res.kind === "existing") {
          // In overwrite mode every resolved tag becomes part of the new
          // list; in merge mode skip ones already in current.tags so the
          // additions Map only carries diffs.
          if (tagsMode === "overwrite" || !existingIds.has(res.option.id)) {
            additions.set(res.option.id, res.option);
          }
          continue;
        }
        if (res.kind === "create") {
          createPromises.push(
            createTag({
              variables: { input: { name: res.name } },
            }).then((result) => {
              const created = result.data?.tagCreate;
              if (created) {
                additions.set(created.id, {
                  id: created.id,
                  name: created.name,
                });
              }
            }),
          );
        }
      }

      try {
        await Promise.all(createPromises);
      } catch (e) {
        toast.error(e);
        setApplying(false);
        return;
      }

      if (tagsMode === "overwrite") {
        // Replace current tags entirely with the resolved scraped set
        // (including the create-mode toggle being meaningful even when
        // additions.size is 0 — the user wanted to wipe their tag list).
        patch.tags = [...additions.values()];
      } else if (additions.size > 0) {
        patch.tags = [...current.tags, ...additions.values()];
      }

      onApply(patch);
      onOpenChange(false);
    } finally {
      setApplying(false);
    }
  }

  if (!scraped) return null;

  const hasAnyContent =
    simpleRows.length > 0 || scrapedTags.length > 0 || showImageRow;

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        // Swallow attempts to close the merge dialog while the image
        // lightbox is the foreground overlay — Escape would otherwise be
        // handled by both layers.
        if (!next && lightboxOpen) return;
        onOpenChange(next);
      }}
    >
      <DialogContent className="max-w-3xl">
        <DialogHeader>
          <DialogTitle>
            {intl.formatMessage({
              id: "scrape.review_results",
              defaultMessage: "Review scraped results",
            })}
          </DialogTitle>
        </DialogHeader>

        <div className="max-h-[60vh] overflow-y-auto -mx-6 px-6">
          {!hasAnyContent && (
            <p className="text-sm text-muted-foreground py-4 text-center">
              {intl.formatMessage({
                id: "scrape.no_changes",
                defaultMessage:
                  "Nothing new from this scrape — every field already matches the form.",
              })}
            </p>
          )}

          {simpleRows.length > 0 && (
            <div className="grid grid-cols-[24px_1fr_1fr] gap-3 sticky top-0 bg-background pt-2 pb-1 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
              <div />
              <div>
                {intl.formatMessage({
                  id: "scrape.column_current",
                  defaultMessage: "Current",
                })}
              </div>
              <div>
                {intl.formatMessage({
                  id: "scrape.column_scraped",
                  defaultMessage: "Scraped",
                })}
              </div>
            </div>
          )}

          {(() => {
            const loserName = computeLoserName();
            return simpleRows.map((row) =>
              row.render({
                accepted: isAccepted(row.key),
                setAccepted: (v) => setAcceptedFor(row.key, v),
                mergeMode: getMergeMode(row.key),
                setMergeMode: (m) => setMergeModeFor(row.key, m),
                extraAliasAddition:
                  row.key === "aliases" ? loserName : undefined,
              }),
            );
          })()}

          {showImageRow && (
            <ImageMergeRow
              accepted={isAccepted("image")}
              onAcceptedChange={(v) => setAcceptedFor("image", v)}
              currentImage={currentImageDisplay}
              scrapedImages={scrapedImages}
              selectedIndex={selectedImageIndex}
              setSelectedIndex={setSelectedImageIndex}
              onLightboxOpenChange={setLightboxOpen}
            />
          )}

          {scrapedTags.length > 0 && (
            <div className="mt-4">
              <div className="text-xs font-semibold uppercase tracking-wide text-muted-foreground py-1 flex items-center gap-2 flex-wrap">
                <span>
                  {intl.formatMessage({ id: "tags", defaultMessage: "Tags" })}
                </span>
                {current.tags.length > 0 && (
                  <ToggleGroup<MergeMode>
                    value={[getMergeMode("tags")]}
                    onValueChange={(vals) => {
                      const next = vals[0];
                      if (next) setMergeModeFor("tags", next);
                    }}
                    variant="outline"
                    size="sm"
                    aria-label={intl.formatMessage({
                      id: "scrape.merge_mode",
                      defaultMessage: "Merge mode",
                    })}
                  >
                    <ToggleGroupItem<MergeMode> value="merge">
                      {intl.formatMessage({
                        id: "scrape.merge_mode_merge",
                        defaultMessage: "Merge",
                      })}
                    </ToggleGroupItem>
                    <ToggleGroupItem<MergeMode> value="overwrite">
                      {intl.formatMessage({
                        id: "scrape.merge_mode_overwrite",
                        defaultMessage: "Overwrite",
                      })}
                    </ToggleGroupItem>
                  </ToggleGroup>
                )}
              </div>
              <div>
                {scrapedTags.map((t, i) => (
                  <ScrapedTagRow
                    key={`${t.name}-${i}`}
                    scraped={t}
                    value={getTagRes(i)}
                    onChange={(next) => setTagRes(i, next)}
                  />
                ))}
              </div>
            </div>
          )}
        </div>

        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={applying}
          >
            {intl.formatMessage({
              id: "actions.cancel",
              defaultMessage: "Cancel",
            })}
          </Button>
          <Button
            type="button"
            onClick={handleApply}
            disabled={applying || !hasAnyContent}
          >
            {applying && <Spinner className="size-4" />}
            {intl.formatMessage({
              id: "actions.apply",
              defaultMessage: "Apply",
            })}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
