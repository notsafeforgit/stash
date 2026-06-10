/**
 * Sidebar replacement for the offline scene list. Drops the GraphQL
 * FilterBuilder (most criteria don't apply to local data) and exposes
 * the filters that do: status (download lifecycle), studio,
 * performers, tags. Options are derived from the entries themselves —
 * no server query.
 *
 * Filter state lives in the parent (OfflineSceneListPage) as a
 * single `OfflineExtraFilter` value passed in/out via props. The
 * predicates run inside `offline-list-source.ts`.
 */

import type React from "react";
import { useMemo, useState } from "react";
import { useIntl } from "react-intl";
import { Funnel, X } from "lucide-react";
import { Button } from "src/components/ui/button";
import {
  Combobox,
  ComboboxChips,
  ComboboxChip,
  ComboboxClear,
  ComboboxChipsInput,
  ComboboxContent,
  ComboboxList,
  ComboboxItem,
  useComboboxAnchor,
} from "src/components/ui/combobox";
import { ToggleGroup, ToggleGroupItem } from "src/components/ui/toggle-group";
import type { OfflineEntry } from "./offline-db";
import {
  EMPTY_OFFLINE_FILTER,
  type OfflineExtraFilter,
  type OfflineStatusFilter,
} from "./offline-list-source";

interface IdLabel {
  id: string;
  label: string;
}

const STATUS_OPTIONS: ReadonlyArray<{
  value: OfflineStatusFilter;
  messageID: string;
}> = [
  { value: "complete", messageID: "offline.filter.status.complete" },
  { value: "downloading", messageID: "offline.filter.status.downloading" },
  { value: "queued", messageID: "offline.filter.status.queued" },
  { value: "error", messageID: "offline.filter.status.error" },
];

export interface OfflineFilterSidebarProps {
  entries: OfflineEntry[];
  filter: OfflineExtraFilter;
  onChange: (next: OfflineExtraFilter) => void;
}

export function OfflineFilterSidebar({
  entries,
  filter,
  onChange,
}: OfflineFilterSidebarProps) {
  const intl = useIntl();

  // ── Option sources from entries (deduped by id) ──
  const studios = useMemo(() => collectIdLabel(entries, "studio"), [entries]);
  const performers = useMemo(
    () => collectIdLabel(entries, "performers"),
    [entries],
  );
  const tags = useMemo(() => collectIdLabel(entries, "tags"), [entries]);

  // ── Toggles ──
  function setStudios(ids: string[]) {
    onChange({ ...filter, studioIds: new Set(ids) });
  }
  function setPerformers(ids: string[]) {
    onChange({ ...filter, performerIds: new Set(ids) });
  }
  function setTags(ids: string[]) {
    onChange({ ...filter, tagIds: new Set(ids) });
  }

  const activeCount =
    filter.statuses.size +
    filter.studioIds.size +
    filter.performerIds.size +
    filter.tagIds.size;

  return (
    <div className="flex flex-col gap-4 p-3 text-sm">
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-2 font-medium">
          <Funnel className="size-4" />
          <span>{intl.formatMessage({ id: "search_filter.edit_filter" })}</span>
        </div>
        {activeCount > 0 && (
          <Button
            size="sm"
            variant="ghost"
            onClick={() => onChange(EMPTY_OFFLINE_FILTER)}
          >
            <X className="size-3" />
            {intl.formatMessage({ id: "actions.clear" })}
          </Button>
        )}
      </div>

      <Section
        title={intl.formatMessage({ id: "offline.filter.status.title" })}
      >
        <ToggleGroup
          multiple
          variant="outline"
          size="sm"
          value={Array.from(filter.statuses)}
          onValueChange={(values) =>
            onChange({
              ...filter,
              statuses: new Set(values as OfflineStatusFilter[]),
            })
          }
        >
          {STATUS_OPTIONS.map((opt) => (
            <ToggleGroupItem key={opt.value} value={opt.value}>
              {intl.formatMessage({ id: opt.messageID })}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </Section>

      {studios.length > 0 && (
        <Section title={intl.formatMessage({ id: "studios" })}>
          <IdLabelMultiSelect
            options={studios}
            value={Array.from(filter.studioIds)}
            onValueChange={setStudios}
            placeholder={intl.formatMessage({ id: "actions.search" })}
          />
        </Section>
      )}

      {performers.length > 0 && (
        <Section title={intl.formatMessage({ id: "performers" })}>
          <IdLabelMultiSelect
            options={performers}
            value={Array.from(filter.performerIds)}
            onValueChange={setPerformers}
            placeholder={intl.formatMessage({ id: "actions.search" })}
          />
        </Section>
      )}

      {tags.length > 0 && (
        <Section title={intl.formatMessage({ id: "tags" })}>
          <IdLabelMultiSelect
            options={tags}
            value={Array.from(filter.tagIds)}
            onValueChange={setTags}
            placeholder={intl.formatMessage({ id: "actions.search" })}
          />
        </Section>
      )}
    </div>
  );
}

// ── Section wrapper ──────────────────────────────────────────────────────────

function Section({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-2">
      <div className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
        {title}
      </div>
      {children}
    </div>
  );
}

// ── Multi-select combobox ────────────────────────────────────────────────────

function IdLabelMultiSelect({
  options,
  value,
  onValueChange,
  placeholder,
}: {
  options: IdLabel[];
  value: string[];
  onValueChange: (ids: string[]) => void;
  placeholder: string;
}) {
  const anchor = useComboboxAnchor();
  const [inputValue, setInputValue] = useState("");

  // Local label lookup so chips render the human label even when the
  // underlying option list churns (e.g. entries refresh while the
  // sidebar is open).
  const labelMap = useMemo(() => {
    const m = new Map<string, string>();
    for (const o of options) m.set(o.id, o.label);
    return m;
  }, [options]);

  const filtered = useMemo(() => {
    const q = inputValue.trim().toLowerCase();
    if (!q) return options;
    return options.filter((o) => o.label.toLowerCase().includes(q));
  }, [options, inputValue]);

  return (
    <Combobox<string, true>
      multiple
      autoHighlight={inputValue.length > 0}
      value={value}
      onValueChange={(ids: string[]) => {
        onValueChange(ids);
        setInputValue("");
      }}
      onInputValueChange={(v: string) => setInputValue(v)}
      itemToStringLabel={(id: string | null) =>
        labelMap.get(id ?? "") ?? id ?? ""
      }
    >
      <ComboboxChips ref={anchor}>
        {value.map((id) => (
          <ComboboxChip key={id}>{labelMap.get(id) ?? id}</ComboboxChip>
        ))}
        <ComboboxChipsInput placeholder={`${placeholder}…`} />
        {value.length > 0 && <ComboboxClear />}
      </ComboboxChips>
      <ComboboxContent anchor={anchor}>
        <ComboboxList>
          {filtered.map((o) => (
            <ComboboxItem key={o.id} value={o.id}>
              {o.label}
            </ComboboxItem>
          ))}
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  );
}

// ── Helpers ──────────────────────────────────────────────────────────────────

function collectIdLabel(
  entries: OfflineEntry[],
  field: "studio" | "performers" | "tags",
): IdLabel[] {
  const map = new Map<string, string>();
  for (const e of entries) {
    if (field === "studio") {
      if (e.studio_id && (e.studio_name ?? "").length > 0) {
        map.set(e.studio_id, e.studio_name!);
      }
      continue;
    }
    for (const item of e[field]) {
      if (!map.has(item.id)) map.set(item.id, item.name);
    }
  }
  return Array.from(map, ([id, label]) => ({ id, label })).sort((a, b) =>
    a.label.localeCompare(b.label),
  );
}
