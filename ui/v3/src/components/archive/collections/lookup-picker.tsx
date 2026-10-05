import { useEffect, useState, type ReactNode } from "react";
import { useMsg } from "@/hooks/message";
import { accountSearchSchema } from "@/core/native-archive/account-review-api";
import {
  Combobox,
  ComboboxInput,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxList,
  ComboboxItem,
} from "@/components/ui/combobox";

export function LookupPicker<T extends { uuid: string }>({
  id,
  value,
  disabled,
  search,
  label,
  renderItem,
  onChange,
}: {
  id: string;
  value: T | null;
  disabled: boolean;
  search: (q: string, signal: AbortSignal) => Promise<T[]>;
  label: (value: T) => string;
  renderItem?: (value: T) => ReactNode;
  onChange: (value: T | null) => void;
}) {
  const msg = useMsg();
  const [query, setQuery] = useState("");
  const [page, setPage] = useState<{ query: string; rows: T[] }>();
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    setBusy(true);
    setError(false);
    const timer = setTimeout(async () => {
      try {
        if (!accountSearchSchema.shape.q.safeParse(query).success) return;
        const rows = await search(query, controller.signal);
        if (!controller.signal.aborted) setPage({ query, rows });
      } catch {
        if (!controller.signal.aborted) setError(true);
      } finally {
        if (!controller.signal.aborted) setBusy(false);
      }
    }, 250);
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [query, search]);
  const rows = !busy && !error && page?.query === query ? page.rows : [];
  return (
    <div className="flex flex-col gap-2">
      <Combobox<T>
        items={rows}
        filter={null}
        value={value}
        disabled={disabled}
        itemToStringLabel={label}
        itemToStringValue={(item) => item.uuid}
        isItemEqualToValue={(item, selected) => item.uuid === selected.uuid}
        onValueChange={onChange}
        onInputValueChange={(text, details) => {
          if (
            details.reason === "input-change" ||
            details.reason === "input-clear"
          )
            setQuery(text);
        }}
      >
        <ComboboxInput
          id={id}
          disabled={disabled}
          aria-busy={busy}
          maxLength={256}
          showClear
        />
        <ComboboxContent>
          <ComboboxEmpty>
            {busy
              ? msg("archive_review.searching", "Searching…")
              : error
                ? msg(
                    "archive_review.search_failed",
                    "Search failed. Try again.",
                  )
                : msg("archive_review.no_matches", "No matches")}
          </ComboboxEmpty>
          <ComboboxList>
            {(item: T) => (
              <ComboboxItem key={item.uuid} value={item}>
                <span className="wrap-anywhere">
                  {renderItem ? renderItem(item) : label(item)}
                </span>
              </ComboboxItem>
            )}
          </ComboboxList>
        </ComboboxContent>
      </Combobox>
      {rows.length === 25 && (
        <p className="text-sm text-muted-foreground">
          {msg(
            "collections.refine",
            "More results may match. Refine your search.",
          )}
        </p>
      )}
    </div>
  );
}
