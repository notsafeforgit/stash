import { useEffect, useState } from "react";
import { ArrowLeft } from "lucide-react";
import { useMsg } from "@/hooks/message";
import {
  ownershipApplySchema,
  type Account,
  type AccountReviewAPI,
  type OwnershipPreview,
} from "@/core/native-archive/account-review-api";
import {
  createAccountReviewOutbox,
  type SavedOwnershipReview,
} from "@/core/native-archive/account-review-outbox";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from "@/components/ui/card";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import { ReviewError } from "@/components/detail/native-metadata/shared";
import { AccountName, AccountOwner, AccountService } from "./shared";
import { AccountHistory, AccountIdentifiers } from "./evidence";
import { OwnershipForm } from "./ownership-form";
import { AccountConsolidation } from "./consolidation";

export function AccountEditor({
  api,
  accountID,
  onSelect,
  onChanged,
}: {
  api: AccountReviewAPI;
  accountID: string;
  onSelect: (uuid?: string) => void;
  onChanged: (account: Account) => void;
}) {
  const msg = useMsg();
  const [outbox] = useState(() => createAccountReviewOutbox(api));
  const [account, setAccount] = useState<Account>();
  const [accountReady, setAccountReady] = useState(false);
  const [saved, setSaved] = useState<SavedOwnershipReview | null>(null);
  const [storageReady, setStorageReady] = useState(false);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState<unknown>();
  const [applied, setApplied] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const [consolidationLocked, setConsolidationLocked] = useState(true);
  // biome-ignore lint/correctness/useExhaustiveDependencies: Explicit retry reloads this selected account and its browser journal.
  useEffect(() => {
    const controller = new AbortController();
    async function load() {
      setBusy(true);
      setError(undefined);
      setStorageReady(false);
      setAccountReady(false);
      try {
        const pending = await outbox.read(accountID);
        if (controller.signal.aborted) return;
        setSaved(pending);
        setStorageReady(true);
        const result = await api.account(accountID, controller.signal);
        if (!controller.signal.aborted) {
          setAccount(result);
          setAccountReady(true);
        }
      } catch (error) {
        if (!controller.signal.aborted) setError(error);
      } finally {
        if (!controller.signal.aborted) setBusy(false);
      }
    }
    void load();
    return () => controller.abort();
  }, [api, outbox, accountID, refresh]);

  async function deliver(preview?: OwnershipPreview) {
    setBusy(true);
    setError(undefined);
    setApplied(false);
    try {
      if (preview) setSaved(await outbox.prepare(accountID, preview));
      await outbox.deliver(accountID);
      setApplied(true);
      setAccountReady(false);
      // Refresh this account only. The parent updates its existing card without
      // starting another search or re-reading the catalog library.
      const current = await api.account(accountID);
      setAccount(current);
      setAccountReady(true);
      onChanged(current);
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
        ownershipApplySchema.parse(JSON.parse(saved.body)).request_uuid,
      );
      setRefresh((value) => value + 1);
    } catch (error) {
      setError(error);
      setBusy(false);
    }
  }
  async function consolidated(source: string, destination: string) {
    setAccountReady(false);
    const [from, to] = await Promise.all([
      api.account(source),
      api.account(destination),
    ]);
    setAccount(from);
    onChanged(from);
    onChanged(to);
    setAccountReady(true);
  }
  function refreshSelected() {
    // Disable the current form in the same render as the refresh request. A
    // rejected consolidation must not expose an editable form which the later
    // account response will immediately replace.
    setAccountReady(false);
    setBusy(true);
    setRefresh((value) => value + 1);
  }
  return (
    <div className="flex flex-col gap-4">
      <Button
        type="button"
        variant="ghost"
        className="w-fit"
        onClick={() => onSelect()}
      >
        <ArrowLeft data-icon="inline-start" />
        {msg("account_review.back", "Back to accounts")}
      </Button>
      {busy && <Spinner />}
      {applied && (
        <Alert>
          <AlertTitle>
            {msg("account_review.saved", "Ownership choice saved")}
          </AlertTitle>
          <AlertDescription>
            {msg(
              "account_review.saved_help",
              "The choice is recorded in the native archive, including its history.",
            )}
          </AlertDescription>
        </Alert>
      )}
      {error !== undefined &&
        (applied ? (
          <Alert variant="destructive">
            <AlertTitle>
              {msg(
                "account_review.refresh_failed",
                "The choice is saved, but this view could not be refreshed",
              )}
            </AlertTitle>
            <AlertDescription>
              <Button
                type="button"
                variant="outline"
                disabled={busy}
                onClick={() => setRefresh((value) => value + 1)}
              >
                {msg("actions.retry", "Retry")}
              </Button>
            </AlertDescription>
          </Alert>
        ) : (
          <ReviewError
            error={error}
            retry={() => setRefresh((value) => value + 1)}
          />
        ))}
      {saved && (
        <Alert>
          <AlertTitle>
            {saved.state === "pending"
              ? msg(
                  "account_review.pending",
                  "An ownership change needs confirmation",
                )
              : msg("archive_review.changed", "This choice has changed")}
          </AlertTitle>
          <AlertDescription>
            <div className="flex flex-col gap-3">
              <p>
                {saved.state === "pending"
                  ? msg(
                      "account_review.pending_help",
                      "Check the saved request before making another choice. Stash will recover its receipt or retry the same request.",
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
                      "account_review.recover",
                      "Check and retry saved change",
                    )
                  : msg("account_review.review_again", "Review again")}
              </Button>
            </div>
          </AlertDescription>
        </Alert>
      )}
      {account && (
        <Card>
          <CardHeader>
            <CardTitle>
              <AccountName account={account} />
            </CardTitle>
            <CardDescription>
              <AccountService namespace={account.namespace} />
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-5">
            <div>
              <p className="text-sm text-muted-foreground">
                {msg("account_review.current_owner", "Current owner")}
              </p>
              <AccountOwner ownership={account.ownership} />
            </div>
            {account.redirect_to ? (
              <Alert>
                <AlertTitle>
                  {msg(
                    "account_review.consolidated",
                    "This account was consolidated",
                  )}
                </AlertTitle>
                <AlertDescription>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => onSelect(account.canonical_uuid)}
                  >
                    {msg(
                      "account_review.open_canonical",
                      "Open current account",
                    )}
                  </Button>
                </AlertDescription>
              </Alert>
            ) : (
              <OwnershipForm
                key={`form:${account.uuid}:${account.revision}`}
                api={api}
                account={account}
                disabled={
                  busy ||
                  !storageReady ||
                  !accountReady ||
                  saved !== null ||
                  consolidationLocked
                }
                onApply={deliver}
                onRefresh={refreshSelected}
              />
            )}
            <AccountConsolidation
              account={account}
              accounts={api}
              disabled={
                busy || !storageReady || !accountReady || saved !== null
              }
              onApplied={consolidated}
              onRefresh={refreshSelected}
              onLockChange={setConsolidationLocked}
              onSelect={onSelect}
            />
            <AccountIdentifiers
              key={`identifiers:${account.uuid}:${account.revision}`}
              api={api}
              account={account.uuid}
            />
            <AccountHistory
              key={`history:${account.uuid}:${account.revision}`}
              api={api}
              account={account.uuid}
            />
          </CardContent>
        </Card>
      )}
    </div>
  );
}
