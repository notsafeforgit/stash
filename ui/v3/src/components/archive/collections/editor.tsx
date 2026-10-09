import { useEffect, useState } from "react";
import { ArrowLeft } from "lucide-react";
import { useMsg } from "@/hooks/message";
import {
  type Collection,
  type CollectionAPI,
  type CollectionInput,
  type MediaRoot,
  collectionInputSchema,
} from "@/core/native-archive/collection-api";
import {
  type Account,
  createAccountReviewAPI,
} from "@/core/native-archive/account-review-api";
import {
  createCollectionOutbox,
  type SavedCollection,
} from "@/core/native-archive/collection-outbox";
import { NativeArchiveError } from "@/core/native-archive/client";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from "@/components/ui/card";
import { ReviewError } from "@/components/detail/native-metadata/shared";
import { CollectionForm } from "./form";
import { CollectionHistory } from "./history";
import { MetadataPolicyEditor } from "../metadata-policy/editor";
import { ManualIntake } from "../manual-intake/editor";

export function CollectionEditor({
  api,
  id,
  create,
  onBack,
  onChanged,
  onCanonicalSource,
}: {
  api: CollectionAPI;
  id: string;
  create: boolean;
  onBack: () => void;
  onChanged: (collection: Collection) => void;
  onCanonicalSource?: (id: string) => void;
}) {
  const msg = useMsg();
  const [outbox] = useState(() => createCollectionOutbox(api));
  const [accounts] = useState(() => createAccountReviewAPI(api.endpoint));
  const [data, setData] = useState<{
    input: CollectionInput;
    current?: Collection;
    root: MediaRoot | null;
    account: Account | null;
  }>();
  const [saved, setSaved] = useState<SavedCollection | null>(null);
  const [ready, setReady] = useState(false);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState<unknown>();
  const [applied, setApplied] = useState(false);
  const [refresh, setRefresh] = useState(0);
  // biome-ignore lint/correctness/useExhaustiveDependencies: Refresh reloads this one collection and its durable pending request.
  useEffect(() => {
    const controller = new AbortController();
    async function load() {
      setBusy(true);
      setReady(false);
      setError(undefined);
      try {
        const pending = await outbox.read(id);
        if (controller.signal.aborted) return;
        setSaved(pending);
        let current: Collection | undefined;
        try {
          current = await api.collection(id, controller.signal);
          if (current.canonical_uuid && onCanonicalSource) {
            if (pending) throw new NativeArchiveError(409, "preview_changed");
            onCanonicalSource(current.canonical_uuid);
            return;
          }
        } catch (error) {
          if (
            !(
              create &&
              error instanceof NativeArchiveError &&
              error.status === 404
            )
          )
            throw error;
        }
        const input: CollectionInput = pending
          ? collectionInputSchema.parse(JSON.parse(pending.body))
          : current
            ? {
                uuid: id,
                expected_revision: current.revision,
                reason: "",
                label: current.label,
                kind: current.kind,
                namespace: current.namespace,
                state: current.state,
                target_url: current.target_url,
                account_uuid: current.account_uuid,
                root_uuid: current.root_uuid,
                path_prefix: current.path_prefix,
              }
            : {
                uuid: id,
                expected_revision: 0,
                reason: "",
                label: "",
                kind: "directory",
                state: "active",
                namespace: "",
                target_url: "",
                account_uuid: null,
                root_uuid: null,
                path_prefix: "",
              };
        const [root, account] = await Promise.all([
          input.root_uuid ? api.root(input.root_uuid, controller.signal) : null,
          input.account_uuid
            ? accounts.account(input.account_uuid, controller.signal)
            : null,
        ]);
        if (!controller.signal.aborted) {
          setData({ input, current, root, account });
          setReady(true);
          if (current) onChanged(current);
        }
      } catch (error) {
        if (!controller.signal.aborted) setError(error);
      } finally {
        if (!controller.signal.aborted) setBusy(false);
      }
    }
    void load();
    return () => controller.abort();
  }, [
    api,
    accounts,
    outbox,
    id,
    create,
    onChanged,
    onCanonicalSource,
    refresh,
  ]);
  async function deliver(input?: CollectionInput) {
    setBusy(true);
    setError(undefined);
    setApplied(false);
    try {
      if (input) setSaved(await outbox.prepare(input));
      await outbox.deliver(id);
      setApplied(true);
      setReady(false);
      setRefresh((value) => value + 1);
    } catch (error) {
      setError(error);
    } finally {
      try {
        setSaved(await outbox.read(id));
      } catch (error) {
        setReady(false);
        setError(error);
      }
      setBusy(false);
    }
  }
  async function reviewAgain() {
    if (saved?.state !== "rejected") return;
    setBusy(true);
    setReady(false);
    setError(undefined);
    try {
      await outbox.forgetRejected(id, saved.body);
      setRefresh((value) => value + 1);
    } catch (error) {
      setError(error);
      setBusy(false);
    }
  }
  return (
    <div className="flex flex-col gap-4">
      <Button type="button" variant="ghost" className="w-fit" onClick={onBack}>
        <ArrowLeft data-icon="inline-start" />
        {msg("collections.back", "Back to collections")}
      </Button>
      {busy && <Spinner />}
      {applied && (
        <Alert>
          <AlertTitle>
            {msg("collections.saved", "Collection saved")}
          </AlertTitle>
          <AlertDescription>
            {msg(
              "collections.saved_help",
              "The archive has recorded this definition and its history.",
            )}
          </AlertDescription>
        </Alert>
      )}
      {error !== undefined &&
        (applied ? (
          <Alert variant="destructive">
            <AlertTitle>
              {msg(
                "collections.refresh_failed",
                "The collection is saved, but this view could not be refreshed",
              )}
            </AlertTitle>
            <AlertDescription>
              <Button
                type="button"
                variant="outline"
                disabled={busy}
                onClick={() => {
                  setReady(false);
                  setBusy(true);
                  setRefresh((value) => value + 1);
                }}
              >
                {msg("actions.retry", "Retry")}
              </Button>
            </AlertDescription>
          </Alert>
        ) : (
          <ReviewError
            error={error}
            retry={() => {
              setReady(false);
              setBusy(true);
              setRefresh((value) => value + 1);
            }}
          />
        ))}
      {saved && (
        <Alert>
          <AlertTitle>
            {saved.state === "pending"
              ? msg("collections.pending", "Confirm this saved change")
              : msg(
                  "collections.conflict",
                  "Review the current collection before saving again",
                )}
          </AlertTitle>
          <AlertDescription>
            <p>
              {saved.state === "pending"
                ? msg(
                    "collections.pending_help",
                    "This browser has an unconfirmed save. Check its recorded revision and safely retry the same change if needed.",
                  )
                : msg(
                    "collections.conflict_help",
                    "The change was rejected or another definition occupies its revision. Reload the current collection to review a new change.",
                  )}
            </p>
            <Button
              type="button"
              variant="outline"
              disabled={busy}
              onClick={() =>
                void (saved.state === "pending" ? deliver() : reviewAgain())
              }
            >
              {saved.state === "pending"
                ? msg("collections.recover", "Check and retry saved change")
                : msg("collections.reload", "Review current collection")}
            </Button>
          </AlertDescription>
        </Alert>
      )}
      {data && (
        <Card>
          <CardHeader>
            <CardTitle>
              {data.current?.label ?? msg("collections.new", "New collection")}
            </CardTitle>
            <CardDescription>
              {msg(
                "collections.editor_help",
                "Define a source or a group of imported files, and optionally associate its folder.",
              )}
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            {data.current?.state === "retired" && (
              <p>
                {msg(
                  "collections.retired_help",
                  "This collection is retired. Choose Active or Disabled and save to restore it with the same identity and history.",
                )}
              </p>
            )}
            <CollectionForm
              key={`${refresh}:${data.current?.revision ?? 0}`}
              api={api}
              input={data.input}
              root={data.root}
              account={data.account}
              disabled={!ready || busy || !!saved}
              onSave={deliver}
            />
            {data.current && (
              <CollectionHistory
                key={data.current.revision}
                api={api}
                id={id}
              />
            )}
            <p data-selectable-text className="wrap-anywhere font-mono text-xs">
              {id}
            </p>
          </CardContent>
        </Card>
      )}
      {data?.current && (
        <MetadataPolicyEditor
          collections={api}
          collection={data.current}
          disabled={!ready || busy || !!saved}
        />
      )}
      {data?.current && (
        <ManualIntake
          endpoint={api.endpoint}
          collection={data.current}
          disabled={!ready || busy || !!saved}
        />
      )}
    </div>
  );
}
