import { useEffect, useId, useRef, useState } from "react";
import { useForm } from "@tanstack/react-form";
import { useIntl } from "react-intl";
import { z } from "zod";
import { useMsg } from "@/hooks/message";
import type { Collection } from "@/core/native-archive/collection-api";
import type {
  MetadataPolicyAPI,
  PolicyDefinition,
  PolicyKind,
  PolicyPreview,
  PolicySampleFile,
  PolicySampleSource,
  PolicyDraftInput,
} from "@/core/native-archive/metadata-policy-api";
import { createMetadataReviewAPI } from "@/core/native-archive/metadata-review-api";
import { NativeArchiveError } from "@/core/native-archive/client";
import {
  Expandable,
  fieldMessages,
} from "@/components/detail/native-metadata/shared";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import {
  Field,
  FieldGroup,
  FieldLabel,
  FieldDescription,
} from "@/components/ui/field";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectGroup,
  SelectItem,
} from "@/components/ui/select";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { PolicySamplePicker, type PolicySampleChoice } from "./sample-picker";
import { PolicyError } from "./error";

function PreviewResult({ value }: { value: PolicyPreview }) {
  const intl = useIntl();
  const msg = useMsg();
  const states = {
    ready: msg(
      "metadata_policy.preview_ready",
      "These rules are enabled for the selected event.",
    ),
    disabled: msg(
      "metadata_policy.preview_disabled",
      "This policy is disabled. Values below are shown only for testing.",
    ),
    not_enabled_for_event: msg(
      "metadata_policy.preview_event_disabled",
      "These rules are disabled for the selected event. Available candidate values are shown only for testing.",
    ),
    organized_at_creation: msg(
      "metadata_policy.preview_organized",
      "The sample is organized, so creation rules would be skipped.",
    ),
    collection_changed: msg(
      "metadata_policy.preview_collection_changed",
      "The collection changed or this file is outside its active folder. Reload the saved collection.",
    ),
  };
  const statuses = {
    ready: msg("metadata_policy.would_update", "Would update"),
    unchanged: msg("metadata_policy.unchanged", "Already matches"),
    protected: msg("metadata_policy.protected", "Preserves an explicit choice"),
    review: msg("metadata_policy.needs_review", "Needs review"),
    omitted: msg("metadata_policy.omitted", "Expression returned no value"),
    older_capture: msg(
      "metadata_policy.older_capture",
      "Keeps newer source metadata",
    ),
  };
  return (
    <div className="flex flex-col gap-3">
      <Alert>
        <AlertTitle>
          {msg(
            "metadata_policy.preview_only",
            "Preview only — nothing was changed",
          )}
        </AlertTitle>
        <AlertDescription>{states[value.state]}</AlertDescription>
      </Alert>
      {value.changes.map((change) => (
        <Field key={change.field}>
          <FieldLabel>
            {intl.formatMessage({
              id: fieldMessages[change.field] ?? change.field,
            })}{" "}
            · {statuses[change.status]}
          </FieldLabel>
          <FieldDescription>
            {msg("metadata_policy.current_value", "Current value")}
          </FieldDescription>
          <pre
            data-selectable-text
            className="max-h-48 overflow-auto whitespace-pre-wrap wrap-anywhere text-xs"
          >
            {JSON.stringify(change.current, null, 2)}
          </pre>
          {Object.hasOwn(change, "value") && (
            <>
              <FieldDescription>
                {msg("metadata_policy.proposed_value", "Proposed value")}
              </FieldDescription>
              <pre
                data-selectable-text
                className="max-h-48 overflow-auto whitespace-pre-wrap wrap-anywhere text-xs"
              >
                {JSON.stringify(change.value, null, 2)}
              </pre>
            </>
          )}
          {change.message && <p className="text-sm">{change.message}</p>}
          {change.names?.map((name) => (
            <p key={name.name} className="text-sm">
              {name.name}:{" "}
              {name.candidates
                .map(
                  (candidate) =>
                    `${candidate.name}${candidate.disambiguation ? ` (${candidate.disambiguation})` : ""}`,
                )
                .join(", ") || msg("archive_review.no_matches", "No matches")}
              {name.more ? "…" : ""}
            </p>
          ))}
        </Field>
      ))}
      {value.data && (
        <Expandable
          title={msg("metadata_policy.sample_data", "Sample data for jq")}
        >
          <pre
            data-selectable-text
            className="max-h-96 overflow-auto whitespace-pre-wrap wrap-anywhere text-xs"
          >
            {JSON.stringify(value.data, null, 2)}
          </pre>
        </Expandable>
      )}
    </div>
  );
}

function SelectedSample({
  api,
  collection,
  policyRevision,
  definition,
  kind,
  choice,
  onReload,
}: {
  api: MetadataPolicyAPI;
  collection: Collection;
  policyRevision: number;
  definition: PolicyDefinition | null;
  kind: PolicyKind;
  choice: PolicySampleChoice;
  onReload: () => void;
}) {
  const msg = useMsg();
  const intl = useIntl();
  const id = useId();
  const [identityAPI] = useState(() => createMetadataReviewAPI(api.endpoint));
  const [sample, setSample] = useState<{
    entity: string;
    files: PolicySampleFile[];
    sources: PolicySampleSource[];
    moreFiles: boolean;
    moreSources: boolean;
  }>();
  const [error, setError] = useState<unknown>();
  const [busy, setBusy] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const [result, setResult] = useState<{ key: string; value: PolicyPreview }>();
  const pending = useRef<AbortController | null>(null);
  const form = useForm({
    defaultValues: {
      file: "",
      source: "",
      event: "existing" as "existing" | "create",
    },
    validators: {
      onChange: z.object({
        file: z.string(),
        source: z.string(),
        event: z.enum(["existing", "create"]),
      }),
    },
  });
  // biome-ignore lint/correctness/useExhaustiveDependencies: Retry reloads only the selected sample.
  useEffect(() => {
    const controller = new AbortController();
    setBusy(true);
    setError(undefined);
    setSample(undefined);
    setResult(undefined);
    form.setFieldValue("file", "");
    form.setFieldValue("source", "");
    async function load() {
      try {
        const identity = await identityAPI.identity(
          kind,
          choice.id,
          controller.signal,
        );
        if (identity.kind !== kind || String(identity.local_id) !== choice.id)
          throw new NativeArchiveError(0, "identity_mismatch");
        const [files, sources] = await Promise.all([
          api.files(
            collection.uuid,
            collection.revision,
            identity.uuid,
            "",
            controller.signal,
          ),
          api.sources(
            collection.uuid,
            collection.revision,
            identity.uuid,
            undefined,
            controller.signal,
          ),
        ]);
        if (controller.signal.aborted) return;
        setSample({
          entity: identity.uuid,
          files,
          sources,
          moreFiles: files.length === api.pageLimit,
          moreSources: sources.length === api.pageLimit,
        });
        if (files.length === 1)
          form.setFieldValue("file", files[0]?.file_uuid ?? "");
      } catch (error) {
        if (!controller.signal.aborted) setError(error);
      } finally {
        if (!controller.signal.aborted) setBusy(false);
      }
    }
    void load();
    return () => {
      controller.abort();
      pending.current?.abort();
    };
  }, [
    api,
    identityAPI,
    choice.id,
    collection.uuid,
    collection.revision,
    form,
    kind,
    refresh,
  ]);
  async function more(type: "files" | "sources") {
    if (!sample) return;
    const controller = new AbortController();
    pending.current?.abort();
    pending.current = controller;
    setBusy(true);
    setError(undefined);
    try {
      if (type === "files") {
        const page = await api.files(
          collection.uuid,
          collection.revision,
          sample.entity,
          sample.files.at(-1)?.file_uuid,
          controller.signal,
        );
        if (!controller.signal.aborted)
          setSample({
            ...sample,
            files: [...sample.files, ...page],
            moreFiles: page.length === api.pageLimit,
          });
      } else {
        const page = await api.sources(
          collection.uuid,
          collection.revision,
          sample.entity,
          sample.sources.at(-1),
          controller.signal,
        );
        if (!controller.signal.aborted)
          setSample({
            ...sample,
            sources: [...sample.sources, ...page],
            moreSources: page.length === api.pageLimit,
          });
      }
    } catch (error) {
      if (!controller.signal.aborted) setError(error);
    } finally {
      if (!controller.signal.aborted) setBusy(false);
    }
  }
  async function evaluate(request: PolicyDraftInput, key: string) {
    const controller = new AbortController();
    pending.current?.abort();
    pending.current = controller;
    setBusy(true);
    setError(undefined);
    try {
      const value = await api.draft(request, controller.signal);
      if (!controller.signal.aborted) setResult({ key, value });
    } catch (error) {
      if (!controller.signal.aborted) setError(error);
    } finally {
      if (!controller.signal.aborted) setBusy(false);
    }
  }
  const fileItems = [
    { value: "", label: msg("metadata_policy.choose_file", "Choose a file") },
    ...(sample?.files.map((file) => ({
      value: file.file_uuid,
      label: file.relative_path,
    })) ?? []),
  ];
  const sourceItems = [
    {
      value: "",
      label: msg(
        "metadata_policy.no_source",
        "No source capture (file and library data only)",
      ),
    },
    ...(sample?.sources.map((source) => ({
      value: `${source.capture_uuid}:${source.attachment_uuid}`,
      label: `${source.title || msg("metadata_policy.untitled", "Untitled")} · ${source.captured_at ? intl.formatDate(source.captured_at, { dateStyle: "medium", timeStyle: "short" }) : msg("metadata_policy.unknown_time", "Unknown observation time")} · ${source.capture_uuid.slice(0, 8)}/${source.attachment_uuid.slice(0, 8)}`,
    })) ?? []),
  ];
  return (
    <FieldGroup>
      {busy && <Spinner />}
      {error !== undefined && (
        <PolicyError
          error={error}
          retry={
            busy
              ? undefined
              : error instanceof NativeArchiveError &&
                  error.code === "preview_changed"
                ? onReload
                : () => setRefresh((value) => value + 1)
          }
        />
      )}
      {sample?.files.length === 0 && (
        <p>
          {msg(
            "metadata_policy.no_sample_files",
            "No physical files from this item fall inside the saved collection folder. ZIP members are not supported by folder policies yet.",
          )}
        </p>
      )}
      <form.Field name="file">
        {(field) => (
          <Field>
            <FieldLabel htmlFor={`${id}-file`}>
              {msg("metadata_policy.sample_file", "Sample file")}
            </FieldLabel>
            <Select
              items={fileItems}
              value={field.state.value}
              disabled={busy}
              onValueChange={(value) => field.handleChange(value ?? "")}
            >
              <SelectTrigger id={`${id}-file`} className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {fileItems.map((item) => (
                    <SelectItem key={item.value} value={item.value}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            {sample?.moreFiles && (
              <Button
                type="button"
                variant="outline"
                disabled={busy}
                onClick={() => void more("files")}
              >
                {msg("metadata_policy.more_files", "Load more files")}
              </Button>
            )}
          </Field>
        )}
      </form.Field>
      <form.Field name="source">
        {(field) => (
          <Field>
            <FieldLabel htmlFor={`${id}-source`}>
              {msg("metadata_policy.sample_source", "Source capture")}
            </FieldLabel>
            <Select
              items={sourceItems}
              value={field.state.value}
              disabled={busy}
              onValueChange={(value) => field.handleChange(value ?? "")}
            >
              <SelectTrigger id={`${id}-source`} className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {sourceItems.map((item) => (
                    <SelectItem key={item.value} value={item.value}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            {sample?.moreSources && (
              <Button
                type="button"
                variant="outline"
                disabled={busy}
                onClick={() => void more("sources")}
              >
                {msg("metadata_policy.more_sources", "Load more captures")}
              </Button>
            )}
          </Field>
        )}
      </form.Field>
      <form.Field name="event">
        {(field) => (
          <Field>
            <FieldLabel id={`${id}-event`}>
              {msg("metadata_policy.sample_event", "Event to simulate")}
            </FieldLabel>
            <ToggleGroup
              aria-labelledby={`${id}-event`}
              variant="outline"
              value={[field.state.value]}
              onValueChange={(values) => {
                if (values[0] === "create" || values[0] === "existing")
                  field.handleChange(values[0]);
              }}
            >
              <ToggleGroupItem value="existing">
                {msg("metadata_policy.existing_event", "Existing item")}
              </ToggleGroupItem>
              <ToggleGroupItem value="create">
                {msg("metadata_policy.creation_event", "New item")}
              </ToggleGroupItem>
            </ToggleGroup>
            <FieldDescription>
              {msg(
                "metadata_policy.simulation_help",
                "Uses this item's current fields. New item tests creation conditions without creating or changing anything.",
              )}
            </FieldDescription>
          </Field>
        )}
      </form.Field>
      <form.Subscribe selector={(state) => state.values}>
        {(values) => {
          const source = sample?.sources.find(
            (source) =>
              `${source.capture_uuid}:${source.attachment_uuid}` ===
              values.source,
          );
          const input: PolicyDraftInput | null =
            definition && sample && values.file
              ? {
                  collection_uuid: collection.uuid,
                  expected_collection_revision: collection.revision,
                  expected_policy_revision: policyRevision,
                  definition,
                  entity_uuid: sample.entity,
                  file_uuid: values.file,
                  event: values.event,
                  include_data: true,
                  ...(source
                    ? {
                        source: {
                          capture_uuid: source.capture_uuid,
                          attachment_uuid: source.attachment_uuid,
                        },
                      }
                    : {}),
                }
              : null;
          const key = JSON.stringify(input);
          return (
            <>
              <Button
                type="button"
                variant="outline"
                disabled={busy || !input}
                className="w-fit"
                onClick={() => {
                  if (input) void evaluate(input, key);
                }}
              >
                {msg("metadata_policy.test", "Test draft mappings")}
              </Button>
              {result?.key === key && <PreviewResult value={result.value} />}
            </>
          );
        }}
      </form.Subscribe>
    </FieldGroup>
  );
}

export function MetadataPolicyPreview({
  api,
  collection,
  policyRevision,
  definition,
  onReload,
}: {
  api: MetadataPolicyAPI;
  collection: Collection;
  policyRevision: number;
  definition: PolicyDefinition | null;
  onReload: () => void;
}) {
  const msg = useMsg();
  const id = useId();
  const form = useForm({
    defaultValues: {
      kind: "scene" as PolicyKind,
      choice: null as PolicySampleChoice | null,
    },
    validators: {
      onChange: z.object({
        kind: z.enum(["scene", "image"]),
        choice: z.object({ id: z.string(), label: z.string() }).nullable(),
      }),
    },
  });
  return (
    <Expandable
      title={msg("metadata_policy.test_sample", "Test with a scene or image")}
    >
      <FieldGroup>
        <form.Field name="kind">
          {(field) => (
            <Field>
              <FieldLabel id={`${id}-kind`}>
                {msg("metadata_policy.sample_kind", "Sample type")}
              </FieldLabel>
              <ToggleGroup
                aria-labelledby={`${id}-kind`}
                variant="outline"
                value={[field.state.value]}
                onValueChange={(values) => {
                  if (values[0] === "scene" || values[0] === "image") {
                    field.handleChange(values[0]);
                    form.setFieldValue("choice", null);
                  }
                }}
              >
                <ToggleGroupItem value="scene">
                  {msg("scenes", "Scenes")}
                </ToggleGroupItem>
                <ToggleGroupItem value="image">
                  {msg("images", "Images")}
                </ToggleGroupItem>
              </ToggleGroup>
            </Field>
          )}
        </form.Field>
        <form.Subscribe selector={(state) => state.values}>
          {(values) => (
            <>
              <form.Field name="choice">
                {(field) => (
                  <Field>
                    <FieldLabel htmlFor={`${id}-choice`}>
                      {msg("metadata_policy.sample_entity", "Scene or image")}
                    </FieldLabel>
                    <PolicySamplePicker
                      key={values.kind}
                      id={`${id}-choice`}
                      kind={values.kind}
                      value={field.state.value}
                      disabled={!collection.root_uuid}
                      onChange={field.handleChange}
                    />
                  </Field>
                )}
              </form.Field>
              {!collection.root_uuid && (
                <p>
                  {msg(
                    "metadata_policy.needs_folder",
                    "Associate a media root and folder with this collection before testing file rules.",
                  )}
                </p>
              )}
              {values.choice && (
                <SelectedSample
                  key={`${collection.revision}:${values.kind}:${values.choice.id}`}
                  api={api}
                  collection={collection}
                  policyRevision={policyRevision}
                  definition={definition}
                  kind={values.kind}
                  choice={values.choice}
                  onReload={onReload}
                />
              )}
            </>
          )}
        </form.Subscribe>
      </FieldGroup>
    </Expandable>
  );
}
