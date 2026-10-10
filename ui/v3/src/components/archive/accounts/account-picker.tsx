import { useEffect, useState } from "react";
import { useMsg } from "@/hooks/message";
import {
  accountSearchSchema,
  accountUUIDSchema,
  type Account,
  type AccountReviewAPI,
} from "@/core/native-archive/account-review-api";
import { NativeArchiveError } from "@/core/native-archive/client";
import {
  Combobox,
  ComboboxInput,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxList,
  ComboboxItem,
} from "@/components/ui/combobox";

function accountLabel(account: Account) {
  const reference = account.identifiers[0]?.reference;
  return [
    account.label,
    reference && `${reference.kind}: ${reference.value}`,
    account.uuid,
  ]
    .filter(Boolean)
    .join(" · ");
}

export function AccountPicker({
  api,
  source,
  id,
  disabled,
  onChange,
}: {
  api: AccountReviewAPI;
  source: Account;
  id: string;
  disabled: boolean;
  onChange: (account: Account) => void;
}) {
  const msg = useMsg();
  const [query, setQuery] = useState("");
  const [page, setPage] = useState<{ query: string; rows: Account[] }>();
  const [error, setError] = useState(false);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    const controller = new AbortController();
    setError(false);
    setBusy(true);
    const timer = setTimeout(async () => {
      try {
        if (!query || !accountSearchSchema.shape.q.safeParse(query).success)
          return;
        const rows = accountUUIDSchema.safeParse(query).success
          ? [await api.account(query, controller.signal)]
          : await api.accounts(
              {
                q: query,
                namespace: source.namespace,
                ownership: "all",
                scope: "all",
              },
              "",
              controller.signal,
            );
        if (!controller.signal.aborted) setPage({ query, rows });
      } catch (error) {
        if (!controller.signal.aborted) {
          if (error instanceof NativeArchiveError && error.status === 404)
            setPage({ query, rows: [] });
          else setError(true);
        }
      } finally {
        if (!controller.signal.aborted) setBusy(false);
      }
    }, 250);
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, [api, query, source.namespace]);
  const current =
    !busy && !error && query && page?.query === query ? page.rows : [];
  const candidates = current.filter(
    (item) =>
      item.uuid !== source.uuid &&
      item.uuid === item.canonical_uuid &&
      item.namespace === source.namespace,
  );
  return (
    <div className="flex flex-col gap-2">
      <Combobox<Account>
        items={candidates}
        filter={null}
        value={null}
        disabled={disabled}
        itemToStringLabel={accountLabel}
        itemToStringValue={(item) => item.uuid}
        onValueChange={(item) => {
          if (item) onChange(item);
        }}
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
          placeholder={msg(
            "account_consolidation.search",
            "Search another account on this service",
          )}
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
            {(item: Account) => (
              <ComboboxItem key={item.uuid} value={item}>
                <span className="wrap-anywhere">{accountLabel(item)}</span>
              </ComboboxItem>
            )}
          </ComboboxList>
        </ComboboxContent>
      </Combobox>
      {current.length === api.pageLimit && (
        <p className="text-sm text-muted-foreground">
          {msg(
            "account_consolidation.refine",
            "More accounts may match. Refine your search using a handle, service ID or archive account ID.",
          )}
        </p>
      )}
    </div>
  );
}
