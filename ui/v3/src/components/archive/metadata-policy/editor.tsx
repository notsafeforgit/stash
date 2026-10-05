import { useEffect, useState } from "react";
import { ChevronDown } from "lucide-react";
import { useMsg } from "@/hooks/message";
import type {
  Collection,
  CollectionAPI,
} from "@/core/native-archive/collection-api";
import {
  createMetadataPolicyAPI,
  policyInputSchema,
  type MetadataPolicy,
  type MetadataPolicyAPI,
  type PolicyDefinition,
  type PolicyInput,
} from "@/core/native-archive/metadata-policy-api";
import {
  createMetadataPolicyOutbox,
  type SavedPolicy,
} from "@/core/native-archive/metadata-policy-outbox";
import {
  policyDefinitionFromForm,
  policyFormValues,
  type PolicyFields,
} from "@/core/native-archive/metadata-policy-form";
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
import {
  Collapsible,
  CollapsibleTrigger,
  CollapsibleContent,
} from "@/components/ui/collapsible";
import { MetadataPolicyForm } from "./form";
import { MetadataPolicyHistory } from "./history";
import { MetadataPolicyPreview } from "./preview";
import { PolicyError } from "./error";

function Editor({
  api,
  collections,
  collection,
  disabled,
}: {
  api: MetadataPolicyAPI;
  collections: CollectionAPI;
  collection: Collection;
  disabled: boolean;
}) {
  const msg = useMsg();
  const [outbox] = useState(() => createMetadataPolicyOutbox(api));
  const [data, setData] = useState<{
    collection: Collection;
    policy: MetadataPolicy | null;
    input: PolicyInput;
    fields: PolicyFields;
  }>();
  const [saved, setSaved] = useState<SavedPolicy | null>(null);
  const [reviewDraft, setReviewDraft] = useState<Pick<
    PolicyInput,
    "definition" | "reason"
  > | null>(null);
  const [ready, setReady] = useState(false);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState<unknown>();
  const [applied, setApplied] = useState(false);
  const [refresh, setRefresh] = useState(0);
  // biome-ignore lint/correctness/useExhaustiveDependencies: A collection revision change or retry reloads this policy and its durable pending request.
  useEffect(() => {
    const controller = new AbortController();
    async function load() {
      setBusy(true);
      setReady(false);
      setError(undefined);
      try {
        const pending = await outbox.read(collection.uuid);
        if (controller.signal.aborted) return;
        setSaved(pending);
        const [current, policy, scene, image] = await Promise.all([
          collections.collection(collection.uuid, controller.signal),
          api.policy(collection.uuid, controller.signal),
          api.fields("scene", controller.signal),
          api.fields("image", controller.signal),
        ]);
        const input: PolicyInput = pending
          ? policyInputSchema.parse(JSON.parse(pending.body))
          : {
              collection_uuid: current.uuid,
              expected_collection_revision: current.revision,
              expected_revision: policy?.revision ?? 0,
              definition:
                reviewDraft?.definition ??
                policyDefinitionFromForm(policyFormValues(policy)),
              reason: reviewDraft?.reason ?? "",
            };
        if (!controller.signal.aborted) {
          setData({
            collection: current,
            policy,
            input,
            fields: { scene, image },
          });
          setReady(true);
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
    collections,
    outbox,
    collection.uuid,
    collection.revision,
    reviewDraft,
    refresh,
  ]);

  async function deliver(definition?: PolicyDefinition, reason = "") {
    if (disabled || busy || (definition && (!ready || !data || saved))) return;
    setBusy(true);
    setError(undefined);
    setApplied(false);
    try {
      if (definition && data)
        setSaved(await outbox.prepare({ ...data.input, definition, reason }));
      await outbox.deliver(collection.uuid);
      setApplied(true);
      setReady(false);
      setReviewDraft(null);
      setRefresh((value) => value + 1);
    } catch (error) {
      setError(error);
    } finally {
      try {
        setSaved(await outbox.read(collection.uuid));
      } catch (error) {
        setReady(false);
        setError(error);
      }
      setBusy(false);
    }
  }
  async function reviewAgain() {
    if (disabled || busy || saved?.state !== "rejected") return;
    setBusy(true);
    setReady(false);
    setError(undefined);
    try {
      const input = policyInputSchema.parse(JSON.parse(saved.body));
      await outbox.forgetRejected(collection.uuid, saved.body);
      setReviewDraft({ definition: input.definition, reason: input.reason });
      setRefresh((value) => value + 1);
    } catch (error) {
      setError(error);
      setBusy(false);
    }
  }
  function reload() {
    setReady(false);
    setBusy(true);
    setRefresh((value) => value + 1);
  }
  return (
    <div className="flex flex-col gap-4">
      {busy && <Spinner />}
      {applied && (
        <Alert>
          <AlertTitle>
            {msg("metadata_policy.saved", "Metadata policy saved")}
          </AlertTitle>
          <AlertDescription>
            {msg(
              "metadata_policy.saved_help",
              "The rules are recorded for future processing. Existing library metadata has not been reapplied.",
            )}
          </AlertDescription>
        </Alert>
      )}
      {error !== undefined &&
        (applied ? (
          <Alert variant="destructive">
            <AlertTitle>
              {msg(
                "metadata_policy.refresh_failed",
                "The policy is saved, but this view could not be refreshed",
              )}
            </AlertTitle>
            <AlertDescription>
              <Button
                type="button"
                variant="outline"
                disabled={busy || disabled}
                onClick={reload}
              >
                {msg("actions.retry", "Retry")}
              </Button>
            </AlertDescription>
          </Alert>
        ) : (
          <PolicyError
            error={error}
            retry={busy || disabled ? undefined : reload}
          />
        ))}
      {saved && (
        <Alert>
          <AlertTitle>
            {saved.state === "pending"
              ? msg(
                  "metadata_policy.pending",
                  "Confirm this saved policy change",
                )
              : msg("metadata_policy.rejected", "Review the rejected draft")}
          </AlertTitle>
          <AlertDescription>
            <p>
              {saved.state === "pending"
                ? msg(
                    "metadata_policy.pending_help",
                    "This browser has an unconfirmed save. Check the recorded policy history and safely retry the same change if needed.",
                  )
                : msg(
                    "metadata_policy.rejected_help",
                    "Keep this draft and load the latest collection and policy revisions before editing and saving it again.",
                  )}
            </p>
            <Button
              type="button"
              variant="outline"
              disabled={busy || disabled}
              onClick={() =>
                void (saved.state === "pending" ? deliver() : reviewAgain())
              }
            >
              {saved.state === "pending"
                ? msg(
                    "metadata_policy.recover",
                    "Check and retry policy change",
                  )
                : msg("metadata_policy.edit_rejected", "Review and edit draft")}
            </Button>
          </AlertDescription>
        </Alert>
      )}
      {reviewDraft && !saved && (
        <Alert>
          <AlertTitle>
            {msg(
              "metadata_policy.draft_retained",
              "Your draft is ready for review",
            )}
          </AlertTitle>
          <AlertDescription>
            {msg(
              "metadata_policy.draft_retained_help",
              "The current collection and policy have been reloaded. Review your retained draft and the saved policy history before saving again.",
            )}
          </AlertDescription>
        </Alert>
      )}
      {data && (
        <>
          {data.policy &&
            data.policy.collection_revision !== data.collection.revision && (
              <Alert>
                <AlertTitle>
                  {msg(
                    "metadata_policy.stale",
                    "These rules were saved for an earlier collection definition",
                  )}
                </AlertTitle>
                <AlertDescription>
                  {msg(
                    "metadata_policy.stale_help",
                    "Review the current folder and source, then save the policy to bind it to this collection revision. Until then, these rules do not apply.",
                  )}
                </AlertDescription>
              </Alert>
            )}
          <p data-selectable-text className="wrap-anywhere">
            {data.collection.label}
            {data.collection.path_prefix
              ? ` · ${data.collection.path_prefix}`
              : ""}
          </p>
          {data.collection.state === "retired" && (
            <p>
              {msg(
                "metadata_policy.retired",
                "Policies for retired collections are read-only.",
              )}
            </p>
          )}
          <MetadataPolicyForm
            key={`${refresh}:${data.collection.revision}:${data.policy?.revision ?? 0}`}
            api={api}
            initialValues={{
              ...policyFormValues({ definition: data.input.definition }),
              reason: data.input.reason,
            }}
            fields={data.fields}
            disabled={
              disabled ||
              !ready ||
              busy ||
              !!saved ||
              data.collection.state === "retired"
            }
            onSave={deliver}
            preview={(definition) => (
              <MetadataPolicyPreview
                api={api}
                collection={data.collection}
                policyRevision={data.policy?.revision ?? 0}
                definition={definition}
                onReload={reload}
              />
            )}
          />
          {data.policy && (
            <MetadataPolicyHistory
              key={data.policy.revision}
              api={api}
              id={collection.uuid}
            />
          )}
        </>
      )}
    </div>
  );
}

export function MetadataPolicyEditor({
  collections,
  collection,
  disabled,
}: {
  collections: CollectionAPI;
  collection: Collection;
  disabled: boolean;
}) {
  const msg = useMsg();
  const [api] = useState(() => createMetadataPolicyAPI(collections.endpoint));
  const [open, setOpen] = useState(false);
  const [opened, setOpened] = useState(false);
  return (
    <Card>
      <CardHeader>
        <CardTitle>{msg("metadata_policy.title", "Metadata rules")}</CardTitle>
        <CardDescription>
          {msg(
            "metadata_policy.help",
            "Choose which source fields populate scenes and images, or assign fixed performers and other values to files scanned in this folder.",
          )}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <Collapsible
          open={open}
          onOpenChange={(value) => {
            setOpen(value);
            if (value) setOpened(true);
          }}
        >
          <CollapsibleTrigger
            render={<Button type="button" variant="outline" />}
          >
            {msg("metadata_policy.edit", "Edit metadata rules")}
            <ChevronDown data-icon="inline-end" />
          </CollapsibleTrigger>
          <CollapsibleContent keepMounted className="pt-4">
            {opened && (
              <Editor
                api={api}
                collections={collections}
                collection={collection}
                disabled={disabled || !open}
              />
            )}
          </CollapsibleContent>
        </Collapsible>
      </CardContent>
    </Card>
  );
}
