import { useState } from "react";
import { useIntl } from "react-intl";
import { ChevronDown } from "lucide-react";
import { useMsg } from "@/hooks/message";
import type {
  AccountEvidence,
  AccountIdentifier,
  AccountOwnership,
  AccountReviewAPI,
} from "@/core/native-archive/account-review-api";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Separator } from "@/components/ui/separator";
import {
  Collapsible,
  CollapsibleTrigger,
  CollapsibleContent,
} from "@/components/ui/collapsible";
import { ReviewError } from "@/components/detail/native-metadata/shared";
import { AccountOwner, AccountOrigin, AccountEvidenceBasis } from "./shared";

function Evidence({
  api,
  identifier,
}: {
  api: AccountReviewAPI;
  identifier: AccountIdentifier;
}) {
  const msg = useMsg();
  const intl = useIntl();
  const [rows, setRows] = useState<AccountEvidence[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [more, setMore] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  async function load() {
    setBusy(true);
    setError(undefined);
    try {
      const page = await api.evidence(identifier.uuid, rows.at(-1)?.key);
      setRows((old) => [
        ...new Map([...old, ...page].map((item) => [item.key, item])).values(),
      ]);
      setMore(page.length === api.pageLimit);
      setLoaded(true);
    } catch (error) {
      setError(error);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="flex flex-col gap-2">
      {rows.map((row) => (
        <div key={row.key} className="flex flex-col gap-1">
          <p>
            <AccountEvidenceBasis basis={row.basis} /> ·{" "}
            <AccountOrigin origin={row.origin} />
          </p>
          <p className="text-sm text-muted-foreground">
            {msg("account_review.observed", "Observed")}{" "}
            {intl.formatDate(row.first_observed)} —{" "}
            {intl.formatDate(row.last_observed)}
          </p>
          <pre
            data-selectable-text
            className="whitespace-pre-wrap wrap-anywhere text-xs"
          >
            {JSON.stringify(row.details, null, 2)}
          </pre>
        </div>
      ))}
      {loaded && rows.length === 0 && (
        <p>{msg("account_review.no_evidence", "No retained evidence.")}</p>
      )}
      {error !== undefined && <ReviewError error={error} />}
      {(!loaded || more) && (
        <Button
          type="button"
          variant="outline"
          size="sm"
          disabled={busy}
          onClick={() => void load()}
        >
          {busy && <Spinner data-icon="inline-start" />}
          {loaded
            ? msg("archive_review.load_more", "Load more")
            : msg("account_review.show_evidence", "Show evidence")}
        </Button>
      )}
    </div>
  );
}

export function AccountIdentifiers({
  api,
  account,
}: {
  api: AccountReviewAPI;
  account: string;
}) {
  const msg = useMsg();
  const [rows, setRows] = useState<AccountIdentifier[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [more, setMore] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  async function load() {
    setBusy(true);
    setError(undefined);
    try {
      const page = await api.identifiers(account, rows.at(-1)?.uuid);
      setRows((old) => [
        ...new Map([...old, ...page].map((item) => [item.uuid, item])).values(),
      ]);
      setMore(page.length === api.pageLimit);
      setLoaded(true);
    } catch (error) {
      setError(error);
    } finally {
      setBusy(false);
    }
  }
  return (
    <Collapsible
      onOpenChange={(open) => {
        if (open && !loaded && !busy) void load();
      }}
    >
      <CollapsibleTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            className="w-full justify-between"
          />
        }
      >
        {msg("account_review.identifiers", "Account identifiers and evidence")}
        <ChevronDown data-icon="inline-end" />
      </CollapsibleTrigger>
      <CollapsibleContent className="flex flex-col gap-4 pt-3">
        <p className="text-sm text-muted-foreground">
          {msg(
            "account_review.identifier_help",
            "Handles, service IDs and profile URLs below are claims about this account. A shared handle can still refer to several accounts.",
          )}
        </p>
        <p data-selectable-text className="wrap-anywhere text-xs">
          {msg("account_review.archive_id", "Archive account ID")}: {account}
        </p>
        {rows.map((row) => (
          <div key={row.uuid} className="flex flex-col gap-2">
            <Separator />
            <p data-selectable-text className="wrap-anywhere">
              {row.reference.namespace} · {row.reference.kind} ·{" "}
              {row.reference.value}
            </p>
            <Evidence api={api} identifier={row} />
          </div>
        ))}
        {loaded && rows.length === 0 && (
          <p>
            {msg("account_review.no_identifiers", "No identifiers recorded.")}
          </p>
        )}
        {error !== undefined && (
          <ReviewError error={error} retry={() => void load()} />
        )}
        {busy ? (
          <Spinner />
        ) : (
          more && (
            <Button type="button" variant="outline" onClick={() => void load()}>
              {msg("archive_review.load_more", "Load more")}
            </Button>
          )
        )}
      </CollapsibleContent>
    </Collapsible>
  );
}

export function AccountHistory({
  api,
  account,
}: {
  api: AccountReviewAPI;
  account: string;
}) {
  const msg = useMsg();
  const intl = useIntl();
  const [rows, setRows] = useState<AccountOwnership[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [more, setMore] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  async function load() {
    setBusy(true);
    setError(undefined);
    try {
      const page = await api.history(account, rows.at(-1)?.revision);
      setRows((old) => [
        ...new Map(
          [...old, ...page].map((item) => [item.decision_uuid, item]),
        ).values(),
      ]);
      setMore(page.length === api.pageLimit);
      setLoaded(true);
    } catch (error) {
      setError(error);
    } finally {
      setBusy(false);
    }
  }
  return (
    <Collapsible
      onOpenChange={(open) => {
        if (open && !loaded && !busy) void load();
      }}
    >
      <CollapsibleTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            className="w-full justify-between"
          />
        }
      >
        {msg("account_review.history", "Ownership history")}
        <ChevronDown data-icon="inline-end" />
      </CollapsibleTrigger>
      <CollapsibleContent className="flex flex-col gap-3 pt-3">
        {rows.map((row) => (
          <div key={row.decision_uuid} className="flex flex-col gap-1">
            <Separator />
            <AccountOwner ownership={row} />
            <p className="text-sm text-muted-foreground">
              {intl.formatDate(row.created_at)} ·{" "}
              <AccountOrigin origin={row.origin} />
            </p>
            {row.reason && <p>{row.reason}</p>}
          </div>
        ))}
        {loaded && rows.length === 0 && (
          <p>
            {msg("account_review.no_history", "No ownership choices recorded.")}
          </p>
        )}
        {error !== undefined && (
          <ReviewError error={error} retry={() => void load()} />
        )}
        {busy ? (
          <Spinner />
        ) : (
          more && (
            <Button type="button" variant="outline" onClick={() => void load()}>
              {msg("archive_review.load_more", "Load more")}
            </Button>
          )
        )}
      </CollapsibleContent>
    </Collapsible>
  );
}
