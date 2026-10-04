import { useEffect, useState } from "react";
import { useIntl } from "react-intl";
import { ChevronDown } from "lucide-react";
import { useMsg } from "@/hooks/message";
import type {
  Account,
  AccountReviewAPI,
} from "@/core/native-archive/account-review-api";
import {
  createAccountConsolidationAPI,
  consolidationApplySchema,
  type ConsolidationPreview,
  type ConsolidationRecord,
  type AccountConsolidationAPI,
} from "@/core/native-archive/account-consolidation-api";
import {
  createAccountConsolidationOutbox,
  type SavedConsolidationReview,
} from "@/core/native-archive/account-consolidation-outbox";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import {
  Collapsible,
  CollapsibleTrigger,
  CollapsibleContent,
} from "@/components/ui/collapsible";
import { ReviewError } from "@/components/detail/native-metadata/shared";
import { AccountOrigin } from "./shared";
import { ConsolidationForm } from "./consolidation-form";

function ConsolidationHistory({
  api,
  account,
  onSelect,
}: {
  api: AccountConsolidationAPI;
  account: string;
  onSelect: (uuid?: string) => void;
}) {
  const msg = useMsg();
  const intl = useIntl();
  const [rows, setRows] = useState<ConsolidationRecord[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [more, setMore] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  async function load() {
    setBusy(true);
    setError(undefined);
    try {
      const page = await api.history(account, rows.at(-1)?.sequence);
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
        {msg("account_consolidation.history", "Consolidation history")}
        <ChevronDown data-icon="inline-end" />
      </CollapsibleTrigger>
      <CollapsibleContent className="flex flex-col gap-3 pt-3">
        <p className="text-sm text-muted-foreground">
          {msg(
            "account_consolidation.history_help",
            "Consolidations involving this account record. Open either record to inspect its evidence and earlier history.",
          )}
        </p>
        {rows.map((row) => (
          <div
            key={row.uuid}
            className="flex flex-col gap-2 rounded-lg border p-3"
          >
            <p>
              {intl.formatDate(row.created_at)} ·{" "}
              <AccountOrigin origin={row.origin} />
            </p>
            <p data-selectable-text className="text-xs wrap-anywhere">
              {row.source_uuid} → {row.destination_uuid}
            </p>
            {row.reason && <p>{row.reason}</p>}
            {row.accepted_identifier_conflicts && (
              <p>
                {msg(
                  "account_consolidation.ack_recorded",
                  "Conflicting stable identifiers were acknowledged.",
                )}
              </p>
            )}
            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                variant="outline"
                onClick={() => onSelect(row.source_uuid)}
              >
                {msg(
                  "account_consolidation.open_source",
                  "Open source account",
                )}
              </Button>
              <Button
                type="button"
                variant="outline"
                onClick={() => onSelect(row.destination_uuid)}
              >
                {msg(
                  "account_consolidation.open_destination",
                  "Open resulting account",
                )}
              </Button>
            </div>
          </div>
        ))}
        {loaded && rows.length === 0 && (
          <p>
            {msg(
              "account_consolidation.no_history",
              "No consolidations recorded.",
            )}
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

export function AccountConsolidation({
  account,
  accounts,
  disabled,
  onApplied,
  onRefresh,
  onLockChange,
  onSelect,
}: {
  account: Account;
  accounts: AccountReviewAPI;
  disabled: boolean;
  onApplied: (source: string, destination: string) => Promise<void>;
  onRefresh: () => void;
  onLockChange: (locked: boolean) => void;
  onSelect: (uuid?: string) => void;
}) {
  const msg = useMsg();
  const [api] = useState(() =>
    createAccountConsolidationAPI(accounts.endpoint),
  );
  const [outbox] = useState(() => createAccountConsolidationOutbox(api));
  const [saved, setSaved] = useState<SavedConsolidationReview | null>(null);
  const [storageReady, setStorageReady] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const [applied, setApplied] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const accountID = account.uuid;
  // biome-ignore lint/correctness/useExhaustiveDependencies: Explicit storage retry re-reads the selected account's saved request.
  useEffect(() => {
    let current = true;
    setStorageReady(false);
    outbox
      .read(accountID)
      .then((value) => {
        if (current) {
          setSaved(value);
          setStorageReady(true);
        }
      })
      .catch((error: unknown) => {
        if (current) setError(error);
      });
    return () => {
      current = false;
    };
  }, [outbox, accountID, refresh]);
  useEffect(() => {
    onLockChange(!storageReady || busy || saved !== null);
  }, [onLockChange, storageReady, busy, saved]);

  async function deliver(preview?: ConsolidationPreview) {
    setBusy(true);
    setError(undefined);
    setApplied(false);
    try {
      if (preview) setSaved(await outbox.prepare(accountID, preview));
      const receipt = await outbox.deliver(accountID);
      setApplied(true);
      await onApplied(
        receipt.request.source_uuid,
        receipt.request.destination_uuid,
      );
    } catch (error) {
      setError(error);
    } finally {
      try {
        setSaved(await outbox.read(accountID));
        setStorageReady(true);
      } catch (error) {
        setStorageReady(false);
        setError(error);
      }
      setBusy(false);
    }
  }
  async function reviewAgain() {
    if (saved?.state !== "rejected") return;
    setBusy(true);
    setError(undefined);
    try {
      await outbox.forgetRejected(
        accountID,
        consolidationApplySchema.parse(JSON.parse(saved.body)).request_uuid,
      );
      setRefresh((value) => value + 1);
      onRefresh();
    } catch (error) {
      setError(error);
    } finally {
      setBusy(false);
    }
  }
  function retry() {
    setError(undefined);
    setRefresh((value) => value + 1);
    onRefresh();
  }
  return (
    <div className="flex flex-col gap-3">
      {applied && (
        <Alert>
          <AlertTitle>
            {msg("account_consolidation.saved", "Account consolidation saved")}
          </AlertTitle>
          <AlertDescription>
            {msg(
              "account_consolidation.saved_help",
              "The original account now resolves to the selected account. Its evidence and review history remain available.",
            )}
          </AlertDescription>
        </Alert>
      )}
      {error !== undefined &&
        (applied ? (
          <Alert variant="destructive">
            <AlertTitle>
              {msg(
                "account_consolidation.refresh_failed",
                "The consolidation is saved, but this view could not be refreshed",
              )}
            </AlertTitle>
            <AlertDescription>
              <Button
                type="button"
                variant="outline"
                disabled={busy}
                onClick={retry}
              >
                {msg("actions.retry", "Retry")}
              </Button>
            </AlertDescription>
          </Alert>
        ) : (
          <ReviewError error={error} retry={retry} />
        ))}
      {saved && (
        <Alert>
          <AlertTitle>
            {saved.state === "pending"
              ? msg(
                  "account_consolidation.pending",
                  "An account consolidation needs confirmation",
                )
              : msg("archive_review.changed", "This choice has changed")}
          </AlertTitle>
          <AlertDescription>
            <div className="flex flex-col gap-3">
              <p>
                {saved.state === "pending"
                  ? msg(
                      "account_consolidation.pending_help",
                      "Check the saved request before changing this account. Stash will check its original receipt or retry the same consolidation.",
                    )
                  : msg(
                      "archive_review.changed_help",
                      "Load a fresh preview before applying this choice.",
                    )}
              </p>
              <Button
                type="button"
                variant="outline"
                disabled={busy || !storageReady}
                onClick={() =>
                  void (saved.state === "pending" ? deliver() : reviewAgain())
                }
              >
                {saved.state === "pending"
                  ? msg(
                      "account_consolidation.recover",
                      "Check and retry saved consolidation",
                    )
                  : msg("account_review.review_again", "Review again")}
              </Button>
            </div>
          </AlertDescription>
        </Alert>
      )}
      {!account.redirect_to && (
        <Collapsible>
          <CollapsibleTrigger
            render={
              <Button
                type="button"
                variant="ghost"
                className="w-full justify-between"
              />
            }
          >
            {msg(
              "account_consolidation.title",
              "Consolidate duplicate account records",
            )}
            <ChevronDown data-icon="inline-end" />
          </CollapsibleTrigger>
          <CollapsibleContent className="pt-3">
            <ConsolidationForm
              key={`${account.uuid}:${account.revision}:${refresh}`}
              account={account}
              accounts={accounts}
              api={api}
              disabled={disabled || busy || !storageReady || saved !== null}
              onApply={deliver}
              onRefresh={onRefresh}
            />
          </CollapsibleContent>
        </Collapsible>
      )}
      <ConsolidationHistory
        key={`${account.uuid}:${account.revision}`}
        account={account.uuid}
        api={api}
        onSelect={onSelect}
      />
    </div>
  );
}
