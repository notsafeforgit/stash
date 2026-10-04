import { useEffect, useId, useState } from "react";
import { useForm } from "@tanstack/react-form";
import { useMsg } from "@/hooks/message";
import { cn } from "@/lib/utils";
import { useListScrollRestoration } from "@/components/list/use-list-scroll-restoration";
import {
  accountSearchSchema,
  createAccountReviewAPI,
  type Account,
  type AccountFilter,
} from "@/core/native-archive/account-review-api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import {
  Field,
  FieldGroup,
  FieldLabel,
  FieldError,
} from "@/components/ui/field";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from "@/components/ui/card";
import {
  Empty,
  EmptyHeader,
  EmptyTitle,
  EmptyDescription,
} from "@/components/ui/empty";
import { ReviewError } from "@/components/detail/native-metadata/shared";
import { AccountName, AccountOwner, AccountService } from "./accounts/shared";
import { AccountEditor } from "./accounts/editor";

function AccountSearch({
  filter,
  onChange,
}: {
  filter: AccountFilter;
  onChange: (filter: AccountFilter) => void;
}) {
  const msg = useMsg();
  const id = useId();
  const form = useForm({
    defaultValues: filter,
    validators: { onChange: accountSearchSchema },
    onSubmit: ({ value }) => onChange(value),
  });
  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        event.stopPropagation();
        void form.handleSubmit();
      }}
    >
      <FieldGroup>
        <form.Field name="q">
          {(field) => (
            <Field data-invalid={!field.state.meta.isValid}>
              <FieldLabel htmlFor={`${id}-query`}>
                {msg("account_review.search", "Search accounts or identifiers")}
              </FieldLabel>
              <Input
                id={`${id}-query`}
                value={field.state.value}
                maxLength={256}
                aria-invalid={!field.state.meta.isValid}
                onBlur={field.handleBlur}
                onChange={(event) => field.handleChange(event.target.value)}
              />
              {!field.state.meta.isValid && (
                <FieldError>
                  {msg(
                    "account_review.search_invalid",
                    "Use a shorter search without control characters.",
                  )}
                </FieldError>
              )}
            </Field>
          )}
        </form.Field>
        <form.Field name="ownership">
          {(field) => (
            <Field>
              <FieldLabel id={`${id}-state`}>
                {msg("account_review.status", "Ownership status")}
              </FieldLabel>
              <ToggleGroup
                aria-labelledby={`${id}-state`}
                variant="outline"
                value={[field.state.value]}
                onValueChange={(values) => {
                  if (values[0]) field.handleChange(values[0]);
                }}
              >
                <ToggleGroupItem value="undecided">
                  {msg("account_review.undecided", "Needs review")}
                </ToggleGroupItem>
                <ToggleGroupItem value="linked">
                  {msg("account_review.linked_filter", "Linked")}
                </ToggleGroupItem>
                <ToggleGroupItem value="unlinked">
                  {msg("account_review.unlinked_filter", "Unlinked")}
                </ToggleGroupItem>
                <ToggleGroupItem value="all">
                  {msg("account_review.all", "All")}
                </ToggleGroupItem>
              </ToggleGroup>
            </Field>
          )}
        </form.Field>
      </FieldGroup>
      <form.Subscribe selector={(state) => state.canSubmit}>
        {(canSubmit) => (
          <Button type="submit" className="w-fit" disabled={!canSubmit}>
            {msg("account_review.search_action", "Find accounts")}
          </Button>
        )}
      </form.Subscribe>
    </form>
  );
}

/** A selected-account review workflow. Keep its search page while editing so
 * saving an owner refreshes just that card and preserves its paging cursor. */
export function AccountReview({
  filter,
  selected,
  onFilterChange,
  onSelect,
}: {
  filter: AccountFilter;
  selected?: string;
  onFilterChange: (filter: AccountFilter) => void;
  onSelect: (uuid?: string) => void;
}) {
  const msg = useMsg();
  const [api] = useState(() => createAccountReviewAPI());
  const [cursor, setCursor] = useState("");
  const [previous, setPrevious] = useState<string[]>([]);
  const [page, setPage] = useState<{
    key: string;
    rows: Account[];
    next: string;
    more: boolean;
  }>();
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState<unknown>();
  const [refresh, setRefresh] = useState(0);
  const [scroller, setScroller] = useState<HTMLDivElement | null>(null);
  const { q, namespace, ownership } = filter;
  const queryKey = JSON.stringify([q, namespace, ownership, cursor]);
  // biome-ignore lint/correctness/useExhaustiveDependencies: Retry explicitly refreshes this page. Individual ownership edits update its existing card.
  useEffect(() => {
    const controller = new AbortController();
    setBusy(true);
    setError(undefined);
    api
      .accounts({ q, namespace, ownership }, cursor, controller.signal)
      .then((rows) => {
        if (!controller.signal.aborted)
          setPage({
            key: queryKey,
            rows,
            next: rows.at(-1)?.uuid ?? "",
            more: rows.length === api.pageLimit,
          });
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted) setError(error);
      })
      .finally(() => {
        if (!controller.signal.aborted) setBusy(false);
      });
    return () => controller.abort();
  }, [api, q, namespace, ownership, cursor, queryKey, refresh]);
  const current = page?.key === queryKey ? page : undefined;
  useListScrollRestoration(
    "account-review",
    scroller,
    !selected && !!current && !busy,
    queryKey,
  );
  function changed(account: Account) {
    setPage((page) =>
      page
        ? {
            ...page,
            rows: page.rows.flatMap((row) =>
              row.uuid !== account.uuid
                ? [row]
                : ownership === "all" ||
                    (account.ownership?.state ?? "undecided") === ownership
                  ? [account]
                  : [],
            ),
          }
        : page,
    );
  }
  return (
    <div
      ref={setScroller}
      className="min-h-0 flex-1 overflow-y-auto"
      data-scroll-restoration-id="account-review"
    >
      <div className="mx-auto flex w-full max-w-4xl flex-col gap-6 p-4 md:p-6">
        <header className="flex flex-col gap-2">
          <h1 className="text-2xl font-semibold">
            {msg("account_review.title", "Account review")}
          </h1>
          <p className="text-muted-foreground">
            {msg(
              "account_review.description",
              "Link service accounts to the performers who own them. Aggregator accounts can remain unlinked.",
            )}
          </p>
        </header>
        {selected && (
          <AccountEditor
            key={selected}
            api={api}
            accountID={selected}
            onSelect={onSelect}
            onChanged={changed}
          />
        )}
        <div
          hidden={selected !== undefined}
          className={cn(
            "flex flex-col gap-6",
            selected !== undefined && "hidden",
          )}
        >
          <AccountSearch
            key={JSON.stringify(filter)}
            filter={filter}
            onChange={onFilterChange}
          />
          {error !== undefined && (
            <ReviewError
              error={error}
              retry={() => setRefresh((value) => value + 1)}
            />
          )}
          {busy && <Spinner />}
          {current && (
            <div
              aria-busy={busy}
              className="grid grid-cols-1 gap-4 md:grid-cols-2"
            >
              {current.rows.map((account) => (
                <Card key={account.uuid}>
                  <CardHeader>
                    <CardTitle>
                      <AccountName account={account} />
                    </CardTitle>
                    <CardDescription>
                      <AccountService namespace={account.namespace} />
                    </CardDescription>
                  </CardHeader>
                  <CardContent className="flex flex-col gap-2">
                    <AccountOwner ownership={account.ownership} />
                    {account.identifiers.slice(0, 2).map((item) => (
                      <p
                        key={item.uuid}
                        data-selectable-text
                        className="text-sm text-muted-foreground wrap-anywhere"
                      >
                        {item.reference.value}
                      </p>
                    ))}
                  </CardContent>
                  <CardFooter>
                    <Button
                      type="button"
                      variant="outline"
                      onClick={() => onSelect(account.uuid)}
                    >
                      {account.ownership?.state === "linked"
                        ? msg("account_review.change_link", "Change link")
                        : msg("account_review.review", "Review account")}
                    </Button>
                  </CardFooter>
                </Card>
              ))}
            </div>
          )}
          {!busy && current?.rows.length === 0 && (
            <Empty>
              <EmptyHeader>
                <EmptyTitle>
                  {msg("account_review.empty", "No accounts on this page")}
                </EmptyTitle>
                <EmptyDescription>
                  {msg(
                    "account_review.empty_help",
                    "Try another search or ownership status, or move to another page.",
                  )}
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          )}
          <div className="flex justify-between gap-3">
            <Button
              type="button"
              variant="outline"
              disabled={busy || previous.length === 0}
              onClick={() => {
                setCursor(previous.at(-1) ?? "");
                setPrevious((items) => items.slice(0, -1));
              }}
            >
              {msg("account_review.previous", "Previous accounts")}
            </Button>
            <Button
              type="button"
              variant="outline"
              disabled={busy || !current?.more}
              onClick={() => {
                if (current) {
                  setPrevious((items) => [...items, cursor]);
                  setCursor(current.next);
                }
              }}
            >
              {msg("account_review.next", "Next accounts")}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
