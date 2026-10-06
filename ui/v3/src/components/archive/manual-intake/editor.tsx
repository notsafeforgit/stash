import { useEffect, useState } from "react";
import { ChevronDown, X } from "lucide-react";
import { useIntl } from "react-intl";
import { useMsg } from "@/hooks/message";
import type { Collection } from "@/core/native-archive/collection-api";
import { NativeArchiveError } from "@/core/native-archive/client";
import {
  createManualIntakeAPI,
  type ManualIntakeAPI,
  type ManualDirectoryEntry,
  type ManualFilePreview,
} from "@/core/native-archive/manual-intake-api";
import {
  createManualIntakeOutbox,
  type SavedManualBatch,
} from "@/core/native-archive/manual-intake-outbox";
import {
  createMetadataPolicyAPI,
  type MetadataPolicy,
} from "@/core/native-archive/metadata-policy-api";
import { Button } from "@/components/ui/button";
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
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import { Spinner } from "@/components/ui/spinner";
import { ReviewError } from "@/components/detail/native-metadata/shared";
import { ManualFilePicker } from "./picker";
import { ManualBatch } from "./batch";
import { FileSize } from "./shared";

function Editor({
  api,
  collection,
  disabled,
}: {
  api: ManualIntakeAPI;
  collection: Collection;
  disabled: boolean;
}) {
  const msg = useMsg(),
    intl = useIntl();
  const [outbox] = useState(() => createManualIntakeOutbox(api));
  const [policies] = useState(() => createMetadataPolicyAPI(api.endpoint));
  const [saved, setSaved] = useState<SavedManualBatch | null>(null);
  const [ready, setReady] = useState(false),
    [busy, setBusy] = useState(false);
  const [available, setAvailable] = useState(false),
    [error, setError] = useState<unknown>();
  const [selected, setSelected] = useState<ManualDirectoryEntry[]>([]);
  const [review, setReview] = useState<{
    files: ManualFilePreview[];
    policy: MetadataPolicy | null;
  }>();
  useEffect(() => {
    const controller = new AbortController();
    void (async () => {
      try {
        const batch = await outbox.read(collection.uuid);
        if (controller.signal.aborted) return;
        setSaved(batch);
        const capabilities = await api.capabilities(controller.signal);
        if (controller.signal.aborted) return;
        setAvailable(capabilities.file_ingestion);
        setReady(true);
      } catch (failure) {
        if (!controller.signal.aborted) setError(failure);
      }
    })();
    return () => controller.abort();
  }, [api, outbox, collection.uuid]);
  async function action(work: () => Promise<unknown>) {
    setBusy(true);
    setError(undefined);
    try {
      await work();
    } catch (failure) {
      setError(failure);
    } finally {
      try {
        setSaved(await outbox.read(collection.uuid));
      } catch (failure) {
        setError(failure);
        setReady(false);
      }
      setBusy(false);
    }
  }
  async function refresh() {
    const capability = await api.capabilities();
    setAvailable(capability.file_ingestion);
    setReady(true);
    if (saved) await outbox.inspect(collection.uuid);
  }
  async function preview() {
    setReview(undefined);
    const policy = await policies.policy(collection.uuid);
    const files: ManualFilePreview[] = [];
    for (const entry of selected) {
      if (entry.kind === "directory") continue;
      const file = await api.preview({
        collection_uuid: collection.uuid,
        relative_path: entry.relative_path,
        media_kind: entry.kind,
      });
      if (
        file.collection_revision !== collection.revision ||
        file.root_uuid !== collection.root_uuid ||
        file.policy_revision !== (policy?.revision ?? 0) ||
        (policy && policy.collection_revision !== collection.revision) ||
        (files[0] && files[0].root_revision !== file.root_revision)
      )
        throw new NativeArchiveError(409, "intake_preview_changed");
      files.push(file);
    }
    setReview({ files, policy });
  }
  const locked = disabled || busy;
  const canBrowse =
    ready && collection.state === "active" && !!collection.root_uuid;
  const stale =
    error instanceof NativeArchiveError &&
    [
      "intake_preview_changed",
      "intake_cancel_changed",
      "directory_changed",
      "directory_mismatch",
    ].includes(error.code);
  return (
    <div className="flex min-w-0 flex-col gap-4">
      {busy && <Spinner aria-label={msg("actions.loading", "Loading…")} />}
      {!!error &&
        (stale ? (
          <Alert variant="destructive">
            <AlertTitle>
              {msg("manual_intake.changed", "This import review has changed")}
            </AlertTitle>
            <AlertDescription>
              {msg(
                "manual_intake.changed_help",
                "Refresh the collection and review its current files and metadata rules before trying again. Saved imports remain available below.",
              )}
            </AlertDescription>
          </Alert>
        ) : (
          <ReviewError error={error} retry={() => void action(refresh)} />
        ))}
      {ready && !available && (
        <Alert>
          <AlertTitle>
            {msg(
              "manual_intake.unavailable",
              "File import worker is unavailable",
            )}
          </AlertTitle>
          <AlertDescription>
            {msg(
              "manual_intake.unavailable_help",
              "You can inspect saved imports. New imports can be submitted when file ingestion is enabled.",
            )}
            <Button
              variant="outline"
              disabled={locked}
              onClick={() => void action(refresh)}
            >
              {msg("manual_intake.availability", "Check import availability")}
            </Button>
          </AlertDescription>
        </Alert>
      )}
      {saved ? (
        <ManualBatch
          batch={saved}
          disabled={locked}
          canSubmit={available}
          onRefresh={() => void action(refresh)}
          onDeliver={() => void action(() => outbox.deliver(collection.uuid))}
          onCancel={(status) =>
            void action(() => outbox.cancel(collection.uuid, status))
          }
          onRetry={(status) =>
            void action(() => outbox.retry(collection.uuid, status))
          }
          onDismiss={() =>
            void action(async () => {
              await outbox.dismiss(collection.uuid, saved.batch_uuid);
              setSelected([]);
              setReview(undefined);
            })
          }
        />
      ) : canBrowse ? (
        review ? (
          <Card>
            <CardHeader>
              <CardTitle>
                {msg("manual_intake.review", "Review selected files")}
              </CardTitle>
              <CardDescription>
                {msg(
                  "manual_intake.review_help",
                  "The worker verifies each file before adding it to the library. Matching content may reuse an existing item. Each file has its own result.",
                )}
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
              {review.files.map((file) => (
                <div
                  key={file.relative_path}
                  className="flex min-w-0 flex-wrap gap-2"
                >
                  <p data-selectable-text className="min-w-0 wrap-anywhere">
                    {file.relative_path}
                  </p>
                  <FileSize bytes={file.size} />
                </div>
              ))}
              <p>
                {review.policy?.definition.enabled
                  ? msg(
                      "manual_intake.rules_enabled",
                      "This batch uses the collection’s saved metadata rules. Existing selected values and explicit clears are preserved.",
                    )
                  : msg(
                      "manual_intake.rules_disabled",
                      "Automatic metadata rules are disabled. To assign a performer or use filenames as titles, save Metadata rules above, then review these files again.",
                    )}
              </p>
              <div className="flex flex-wrap gap-2">
                <Button
                  variant="outline"
                  disabled={locked}
                  onClick={() => setReview(undefined)}
                >
                  {msg("manual_intake.change_selection", "Change selection")}
                </Button>
                <Button
                  disabled={locked || !available}
                  onClick={() =>
                    void action(async () => {
                      const batch = await outbox.prepare(review.files);
                      setSaved(batch);
                      await outbox.deliver(collection.uuid);
                    })
                  }
                >
                  {msg("manual_intake.import", "Import selected files")}
                </Button>
              </div>
            </CardContent>
          </Card>
        ) : (
          <>
            <ManualFilePicker
              api={api}
              collection={collection}
              disabled={locked}
              selected={selected}
              onSelect={setSelected}
            />
            <p>
              {intl.formatMessage(
                {
                  id: "manual_intake.selected_count",
                  defaultMessage: "{count} of 25 files selected",
                },
                { count: selected.length },
              )}
            </p>
            {selected.map((entry) => (
              <div
                key={entry.relative_path}
                className="flex min-w-0 items-center gap-2"
              >
                <p
                  data-selectable-text
                  className="min-w-0 flex-1 wrap-anywhere text-sm"
                >
                  {entry.relative_path}
                </p>
                <Button
                  variant="ghost"
                  size="icon"
                  disabled={locked}
                  aria-label={intl.formatMessage(
                    {
                      id: "manual_intake.remove",
                      defaultMessage: "Remove {name} from selection",
                    },
                    { name: entry.name },
                  )}
                  onClick={() =>
                    setSelected((rows) =>
                      rows.filter(
                        (file) => file.relative_path !== entry.relative_path,
                      ),
                    )
                  }
                >
                  <X data-icon="inline-start" />
                </Button>
              </div>
            ))}
            <Button
              className="w-fit"
              disabled={locked || !selected.length}
              onClick={() => void action(preview)}
            >
              {msg("manual_intake.preview", "Review selected files")}
            </Button>
          </>
        )
      ) : ready ? (
        <p>
          {msg(
            "manual_intake.inactive",
            "Use an active folder or batch collection with a bound media root to import files.",
          )}
        </p>
      ) : (
        !error && <Spinner aria-label={msg("actions.loading", "Loading…")} />
      )}
    </div>
  );
}
export function ManualIntake({
  endpoint,
  collection,
  disabled,
}: {
  endpoint: string;
  collection: Collection;
  disabled: boolean;
}) {
  const msg = useMsg();
  const [api] = useState(() => createManualIntakeAPI(endpoint));
  const [open, setOpen] = useState(false),
    [opened, setOpened] = useState(false);
  if (collection.kind !== "directory" && collection.kind !== "manual_batch")
    return null;
  return (
    <Card>
      <CardHeader>
        <CardTitle>
          {msg("manual_intake.title", "Import local files")}
        </CardTitle>
        <CardDescription>
          {msg(
            "manual_intake.help",
            "Choose files already stored in this collection’s folder. Set a performer once in Metadata rules above; filenames are read automatically. No source account or post is required.",
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
          <CollapsibleTrigger render={<Button variant="outline" />}>
            {msg("manual_intake.open", "Choose files or check imports")}
            <ChevronDown data-icon="inline-end" />
          </CollapsibleTrigger>
          <CollapsibleContent keepMounted className="pt-4">
            {opened && (
              <Editor
                api={api}
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
