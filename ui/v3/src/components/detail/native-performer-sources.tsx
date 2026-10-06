import { useCallback, useEffect, useRef, useState } from "react";
import { Link } from "@tanstack/react-router";
import { useIntl } from "react-intl";
import { ChevronDown } from "lucide-react";
import { useMsg } from "@/hooks/message";
import {
  createPerformerSourceAPI,
  type PerformerSourceAPI,
  type PerformerIdentity,
} from "@/core/native-archive/performer-source-api";
import type { AccountPerformer } from "@/core/native-archive/account-review-api";
import { NativeArchiveError } from "@/core/native-archive/client";
import {
  AccountName,
  AccountService,
  PerformerName,
} from "../archive/accounts/shared";
import { Button, buttonVariants } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Spinner } from "@/components/ui/spinner";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
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
import {
  Collapsible,
  CollapsibleTrigger,
  CollapsibleContent,
} from "@/components/ui/collapsible";

type SourcePage<T> = {
  requested_uuid: string;
  performer: AccountPerformer;
  items: T[];
};

function useSourcePage<T extends { uuid: string }>(
  load: (
    after: string | undefined,
    signal: AbortSignal,
  ) => Promise<SourcePage<T>>,
  pageLimit: number,
) {
  const [data, setData] = useState<SourcePage<T>>();
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState<unknown>();
  const [more, setMore] = useState(false);
  const [version, setVersion] = useState(0);
  const pending = useRef<AbortController | null>(null);
  // biome-ignore lint/correctness/useExhaustiveDependencies: Reload explicitly replaces the current bounded page.
  useEffect(() => {
    const controller = new AbortController();
    setData(undefined);
    setBusy(true);
    setError(undefined);
    setMore(false);
    void load(undefined, controller.signal)
      .then((page) => {
        if (controller.signal.aborted) return;
        setData(page);
        setMore(page.items.length === pageLimit);
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted) setError(error);
      })
      .finally(() => {
        if (!controller.signal.aborted) setBusy(false);
      });
    return () => {
      controller.abort();
      pending.current?.abort();
    };
  }, [load, pageLimit, version]);
  async function loadMore() {
    if (!data || busy) return;
    const controller = new AbortController();
    pending.current = controller;
    setBusy(true);
    setError(undefined);
    try {
      const page = await load(data.items.at(-1)?.uuid, controller.signal);
      if (controller.signal.aborted) return;
      if (
        page.performer.uuid !== data.performer.uuid ||
        page.performer.revision !== data.performer.revision ||
        page.performer.state !== data.performer.state ||
        page.performer.local_id !== data.performer.local_id
      )
        throw new NativeArchiveError(409, "performer_changed");
      setData({ ...page, items: [...data.items, ...page.items] });
      setMore(page.items.length === pageLimit);
    } catch (error) {
      if (!controller.signal.aborted) setError(error);
    } finally {
      if (!controller.signal.aborted) setBusy(false);
    }
  }
  return {
    data,
    busy,
    error,
    more,
    loadMore,
    reload: () => setVersion((n) => n + 1),
  };
}

function SourceError({ error, retry }: { error: unknown; retry: () => void }) {
  const msg = useMsg();
  const missing = error instanceof NativeArchiveError && error.status === 404;
  const code = error instanceof NativeArchiveError ? error.code : "";
  return (
    <Alert variant="destructive">
      <AlertTitle>
        {missing
          ? msg("performer_sources.missing", "This performer is unavailable")
          : msg("performer_sources.failed", "Could not load performer sources")}
      </AlertTitle>
      <AlertDescription>
        <p>
          {code === "performer_source_limit"
            ? msg(
                "performer_sources.limit",
                "This performer's identity history is too large for this view. Individual links remain available in Account review.",
              )
            : code === "performer_changed"
              ? msg(
                  "performer_sources.changed",
                  "This performer changed. Reload to view the current source accounts.",
                )
              : msg(
                  "performer_sources.retry_help",
                  "Check your connection and access to Stash, then retry.",
                )}
        </p>
        <Button variant="outline" size="sm" onClick={retry}>
          {msg("actions.retry", "Retry")}
        </Button>
      </AlertDescription>
    </Alert>
  );
}

function IdentityRow({ row }: { row: PerformerIdentity }) {
  const intl = useIntl();
  const msg = useMsg();
  return (
    <Card size="sm">
      <CardHeader>
        <CardTitle>
          {row.state === "active"
            ? msg("performer_sources.current", "Current identity")
            : row.state === "redirected"
              ? msg("performer_sources.redirected", "Redirected identity")
              : msg("performer_sources.deleted", "Deleted identity")}
        </CardTitle>
        <CardDescription>
          <code data-selectable-text className="wrap-anywhere">
            {row.uuid}
          </code>
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-2 text-sm">
        {row.original_id !== null && (
          <p>
            {intl.formatMessage(
              {
                id: "performer_sources.original_id",
                defaultMessage: "Original library ID: {id}",
              },
              { id: row.original_id },
            )}
          </p>
        )}
        {row.redirect_to && (
          <div className="flex flex-col gap-1">
            <span>
              {msg(
                "performer_sources.redirect_to",
                "Redirects to archive identity",
              )}
            </span>
            <code data-selectable-text className="wrap-anywhere">
              {row.redirect_to}
            </code>
          </div>
        )}
        <p>
          {intl.formatMessage(
            {
              id: "performer_sources.created_at",
              defaultMessage: "Identity created: {date}",
            },
            {
              date: intl.formatDate(row.created_at, {
                dateStyle: "medium",
                timeStyle: "short",
              }),
            },
          )}
        </p>
        {row.retired_at && (
          <p>
            {intl.formatMessage(
              {
                id: "performer_sources.retired_at",
                defaultMessage: "Identity retired: {date}",
              },
              {
                date: intl.formatDate(row.retired_at, {
                  dateStyle: "medium",
                  timeStyle: "short",
                }),
              },
            )}
          </p>
        )}
      </CardContent>
    </Card>
  );
}

function IdentityHistory({ id, api }: { id: string; api: PerformerSourceAPI }) {
  const msg = useMsg();
  const load = useCallback(
    async (after: string | undefined, signal: AbortSignal) => {
      const page = await api.identities(id, after, signal);
      return { ...page, items: page.identities };
    },
    [api, id],
  );
  const state = useSourcePage(load, api.pageLimit);
  return (
    <div className="flex flex-col gap-3">
      <p className="text-sm text-muted-foreground">
        {msg(
          "performer_sources.history_help",
          "Retained UUIDs keep older links working after merges or identity migration. Historical library IDs are evidence and do not link to current records.",
        )}
      </p>
      {state.busy && (
        <Spinner
          aria-label={msg(
            "performer_sources.loading_history",
            "Loading identity history",
          )}
        />
      )}
      {state.error !== undefined && (
        <SourceError error={state.error} retry={state.reload} />
      )}
      {state.data?.items.map((row) => (
        <IdentityRow key={row.uuid} row={row} />
      ))}
      {state.more && (
        <Button
          variant="outline"
          disabled={state.busy}
          onClick={() => void state.loadMore()}
        >
          {msg("performer_sources.more_history", "Load more identities")}
        </Button>
      )}
    </div>
  );
}

export function NativePerformerSources({ localId }: { localId: string }) {
  const msg = useMsg();
  const [api] = useState(() => createPerformerSourceAPI());
  const [historyOpen, setHistoryOpen] = useState(false);
  const load = useCallback(
    async (after: string | undefined, signal: AbortSignal) => {
      const identity = await api.identity(localId, signal);
      const page = await api.accounts(identity.uuid, after, signal);
      return { ...page, items: page.accounts };
    },
    [api, localId],
  );
  const state = useSourcePage(load, api.pageLimit);
  const data = state.data;
  return (
    <div className="flex min-h-0 flex-col gap-4 p-4 md:flex-1 md:overflow-y-auto">
      <p className="text-sm text-muted-foreground">
        {msg(
          "performer_sources.help",
          "These source accounts have a recorded ownership link to this performer, including links retained through merges. Account ownership does not assign performers to media.",
        )}
      </p>
      <div>
        <Button variant="outline" disabled={state.busy} onClick={state.reload}>
          {msg("performer_sources.refresh", "Refresh source accounts")}
        </Button>
      </div>
      {state.busy && (
        <Spinner
          aria-label={msg(
            "performer_sources.loading",
            "Loading source accounts",
          )}
        />
      )}
      {state.error !== undefined && (
        <SourceError error={state.error} retry={state.reload} />
      )}
      {data && (
        <>
          {String(data.performer.local_id) !== localId && (
            <Alert>
              <AlertTitle>
                {msg(
                  "performer_sources.current_owner",
                  "Current performer identity",
                )}
              </AlertTitle>
              <AlertDescription>
                <PerformerName performer={data.performer} />
              </AlertDescription>
            </Alert>
          )}
          {data.items.length === 0 && (
            <Empty>
              <EmptyHeader>
                <EmptyTitle>
                  {msg("performer_sources.empty", "No linked source accounts")}
                </EmptyTitle>
                <EmptyDescription>
                  {msg(
                    "performer_sources.empty_help",
                    "A performer can have directly scanned or purchased media without a source account. Review an account to add an explicit ownership link.",
                  )}
                </EmptyDescription>
              </EmptyHeader>
              <Link
                className={buttonVariants({ variant: "outline" })}
                to="/account-review"
                search={{ ownership: "all", q: "", namespace: "" }}
              >
                {msg("performer_sources.open_review", "Open account review")}
              </Link>
            </Empty>
          )}
          {data.items.map((account) => (
            <Card size="sm" key={account.uuid}>
              <CardHeader>
                <CardTitle className="wrap-anywhere">
                  <AccountName account={account} />
                </CardTitle>
                <CardDescription>
                  <AccountService namespace={account.namespace} />
                </CardDescription>
              </CardHeader>
              <CardContent>
                <Badge variant="outline">
                  {msg("performer_sources.linked", "Linked to this performer")}
                </Badge>
              </CardContent>
              <CardFooter>
                <Link
                  className={buttonVariants({ variant: "outline" })}
                  to="/account-review"
                  search={{
                    ownership: "all",
                    q: "",
                    namespace: account.namespace,
                    account: account.uuid,
                  }}
                >
                  {msg("performer_sources.manage", "Manage account link")}
                </Link>
              </CardFooter>
            </Card>
          ))}
          {state.more && (
            <Button
              variant="outline"
              disabled={state.busy}
              onClick={() => void state.loadMore()}
            >
              {msg("performer_sources.more_accounts", "Load more accounts")}
            </Button>
          )}
          <Collapsible open={historyOpen} onOpenChange={setHistoryOpen}>
            <CollapsibleTrigger
              render={
                <Button variant="outline" className="w-full justify-between" />
              }
            >
              {msg("performer_sources.history", "Identity history")}
              <ChevronDown data-icon="inline-end" />
            </CollapsibleTrigger>
            <CollapsibleContent className="pt-4">
              {historyOpen && (
                <IdentityHistory
                  key={data.requested_uuid}
                  id={data.requested_uuid}
                  api={api}
                />
              )}
            </CollapsibleContent>
          </Collapsible>
        </>
      )}
    </div>
  );
}
