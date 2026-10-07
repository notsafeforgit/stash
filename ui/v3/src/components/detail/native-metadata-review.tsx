import { useEffect, useState } from "react";
import { useApolloClient } from "@apollo/client/react";
import { History } from "lucide-react";
import { useMsg } from "@/hooks/message";
import * as GQL from "@/core/generated-graphql";
import { affectedActiveQueries } from "@/core/mutation-invalidation";
import {
  createMetadataReviewAPI,
  editApplySchema,
  type EditCandidate,
  type EditPreview,
  type MetadataFields,
} from "@/core/native-archive/metadata-review-api";
import {
  createMetadataReviewOutbox,
  type ReviewTarget,
  type SavedReview,
} from "@/core/native-archive/metadata-review-outbox";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import {
  Empty,
  EmptyHeader,
  EmptyTitle,
  EmptyDescription,
} from "@/components/ui/empty";
import { ReviewError, type EntityNames } from "./native-metadata/shared";
import { FieldSummary } from "./native-metadata/fields";
import { HistoricalChoice } from "./native-metadata/historical-choice";

/** Key this component by kind/local ID so navigation cannot retain another
 * entity's pending query, choices or result. The outbox survives that remount. */
export function NativeMetadataReview({
  kind,
  entity,
}: {
  kind: ReviewTarget["kind"];
  entity: EntityNames;
}) {
  const msg = useMsg();
  const client = useApolloClient();
  const [api] = useState(() => createMetadataReviewAPI());
  const [outbox] = useState(() => createMetadataReviewOutbox(api));
  const [fields, setFields] = useState<MetadataFields>();
  const [edits, setEdits] = useState<EditCandidate[]>([]);
  const [more, setMore] = useState(false);
  const [saved, setSaved] = useState<SavedReview | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const [outcome, setOutcome] = useState<"applied" | "kept">();
  const [refreshFailed, setRefreshFailed] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const target = { kind, localId: entity.id };
  // biome-ignore lint/correctness/useExhaustiveDependencies: The refresh counter explicitly reloads after a committed edit or a requested retry.
  useEffect(() => {
    const controller = new AbortController();
    const target = { kind, localId: entity.id };
    async function load() {
      setBusy(true);
      setError(undefined);
      try {
        const pending = await outbox.read(target);
        if (!controller.signal.aborted) setSaved(pending);
        const identity = await api.identity(kind, entity.id, controller.signal);
        const [state, alternatives] = await Promise.all([
          api.fields(identity.uuid, controller.signal),
          api.edits(identity.uuid, undefined, controller.signal),
        ]);
        if (!controller.signal.aborted) {
          setFields(state);
          setEdits(alternatives);
          setMore(alternatives.length === api.pageLimit);
        }
      } catch (error) {
        if (!controller.signal.aborted) setError(error);
      } finally {
        if (!controller.signal.aborted) setBusy(false);
      }
    }
    void load();
    return () => controller.abort();
  }, [api, outbox, kind, entity.id, refresh]);

  async function reconcile() {
    setRefreshFailed(false);
    try {
      await client.refetchQueries({
        include: affectedActiveQueries(
          client,
          kind === "scene" ? GQL.SceneUpdateDocument : GQL.ImageUpdateDocument,
        ),
      });
    } catch {
      setRefreshFailed(true);
    }
    setRefresh((value) => value + 1);
  }
  async function deliver(preview?: EditPreview, keepCurrent = false) {
    setBusy(true);
    setError(undefined);
    setOutcome(undefined);
    setRefreshFailed(false);
    try {
      if (preview) setSaved(await outbox.prepare(target, preview, keepCurrent));
      const receipt = await outbox.deliver(target);
      setOutcome(receipt.kept_current ? "kept" : "applied");
      if (receipt.kept_current) setRefresh((value) => value + 1);
      else await reconcile();
    } finally {
      try {
        setSaved(await outbox.read(target));
      } finally {
        setBusy(false);
      }
    }
  }
  async function recover() {
    try {
      await deliver();
    } catch (error) {
      setError(error);
    }
  }
  async function moreEdits() {
    if (!fields) return;
    setBusy(true);
    setError(undefined);
    try {
      const page = await api.edits(fields.entity.uuid, edits.at(-1));
      setEdits((current) => [...current, ...page]);
      setMore(page.length === api.pageLimit);
    } catch (error) {
      setError(error);
    } finally {
      setBusy(false);
    }
  }
  return (
    <section
      aria-label={msg("archive_review.title", "Metadata review")}
      className="flex flex-col gap-4"
    >
      <p className="text-sm text-muted-foreground">
        {msg(
          "archive_review.intro",
          "Review retained catalog edits for this item. Applying a choice updates only this scene or image.",
        )}
      </p>
      {busy && (
        <Spinner
          aria-label={msg("archive_review.loading", "Loading metadata review")}
        />
      )}
      {error !== undefined && (
        <ReviewError
          error={error}
          retry={() => setRefresh((value) => value + 1)}
        />
      )}
      {outcome && (
        <Alert>
          <AlertTitle>
            {outcome === "kept"
              ? msg("archive_review.kept", "Current value kept")
              : msg("archive_review.applied", "Choice applied")}
          </AlertTitle>
          <AlertDescription>
            {msg(
              "archive_review.applied_help",
              "The original source evidence remains available in the archive.",
            )}
            {refreshFailed && (
              <>
                <p>
                  {msg(
                    "archive_review.refresh_failed",
                    "The change is saved, but other library views could not be refreshed.",
                  )}
                </p>
                <Button variant="outline" onClick={() => void reconcile()}>
                  {msg("actions.retry", "Retry")}
                </Button>
              </>
            )}
          </AlertDescription>
        </Alert>
      )}
      {saved && (
        <Alert>
          <History />
          <AlertTitle>
            {saved.state === "rejected"
              ? msg(
                  "archive_review.rejected",
                  "Saved choice needs a new preview",
                )
              : msg(
                  "archive_review.pending",
                  "A saved change needs confirmation",
                )}
          </AlertTitle>
          <AlertDescription>
            <p>
              {msg(
                "archive_review.pending_help",
                "Recover this browser's saved request before applying another choice. A retry checks for the original result first.",
              )}
            </p>
            <Button
              variant="outline"
              disabled={busy}
              onClick={() =>
                void (async () => {
                  if (saved.state === "pending") await recover();
                  else {
                    try {
                      await outbox.forgetRejected(
                        target,
                        editApplySchema.parse(JSON.parse(saved.body))
                          .request_uuid,
                      );
                      setSaved(await outbox.read(target));
                      setRefresh((value) => value + 1);
                    } catch (error) {
                      setError(error);
                    }
                  }
                })()
              }
            >
              {saved.state === "pending"
                ? msg("archive_review.recover", "Check and retry saved change")
                : msg("archive_review.fresh_preview", "Review again")}
            </Button>
          </AlertDescription>
        </Alert>
      )}
      {fields && <FieldSummary fields={fields} api={api} entity={entity} />}
      {fields && edits.length === 0 && (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>
              {msg("archive_review.no_edits", "No retained file edits")}
            </EmptyTitle>
            <EmptyDescription>
              {msg(
                "archive_review.no_edits_help",
                "No historical catalog edits are linked to this item's files. Ordinary library metadata is shown above.",
              )}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}
      {fields &&
        edits.map((candidate) => (
          <HistoricalChoice
            key={`${candidate.history_uuid}:${candidate.match_uuid}`}
            candidate={candidate}
            api={api}
            fields={fields}
            entity={entity}
            blocked={busy || !!saved}
            onApply={deliver}
          />
        ))}
      {more && (
        <Button
          variant="outline"
          disabled={busy}
          onClick={() => void moreEdits()}
        >
          {msg("archive_review.load_more", "Load more")}
        </Button>
      )}
    </section>
  );
}
