import { useCallback, useEffect, useId, useRef, useState } from "react";
import { useForm } from "@tanstack/react-form";
import { useIntl } from "react-intl";
import { z } from "zod";
import { useMsg } from "@/hooks/message";
import { ownershipReasonSchema } from "@/core/native-archive/account-review-api";
import {
  selectionModeSchema,
  type AttachmentSelectionAPI,
  type SelectionPreview,
} from "@/core/native-archive/attachment-selection-api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import {
  Field,
  FieldLabel,
  FieldDescription,
  FieldError,
} from "@/components/ui/field";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@/components/ui/select";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { ReviewError } from "@/components/detail/native-metadata/shared";
import { useAlbumPages } from "./read";
import { SelectionListDetails, useSelectionLabels } from "./selection-details";

const formSchema = z
  .object({
    mode: selectionModeSchema,
    manifest: z.string(),
    reason: ownershipReasonSchema,
  })
  .refine((value) => value.mode === "disabled" || value.manifest !== "");

export function SelectionForm({
  api,
  post,
  revision,
  disabled,
  onApply,
}: {
  api: AttachmentSelectionAPI;
  post: string;
  revision: number;
  disabled: boolean;
  onApply: (preview: SelectionPreview) => Promise<void>;
}) {
  const msg = useMsg();
  const intl = useIntl();
  const labels = useSelectionLabels();
  const id = useId();
  const [preview, setPreview] = useState<SelectionPreview>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const pending = useRef<AbortController | null>(null);
  useEffect(() => () => pending.current?.abort(), []);
  const load = useCallback(
    async (after: string | undefined, signal: AbortSignal) => {
      const items = await api.manifests(post, after, signal);
      return {
        items,
        signature: post,
        header: null,
        next:
          items.length === api.pageLimit ? (items.at(-1)?.uuid ?? null) : null,
      };
    },
    [api, post],
  );
  const lists = useAlbumPages(load);
  const form = useForm({
    defaultValues: {
      mode: "pinned" as z.infer<typeof selectionModeSchema>,
      manifest: "",
      reason: "",
    },
    validators: { onChange: formSchema },
    onSubmit: async ({ value }) => {
      if (disabled || pending.current) return;
      const source = lists.data?.items.find(
        (item) => item.uuid === value.manifest,
      );
      if (value.mode !== "disabled" && !source) return;
      const controller = new AbortController();
      pending.current = controller;
      setBusy(true);
      setError(undefined);
      setPreview(undefined);
      try {
        const result = await api.preview(
          {
            post_uuid: post,
            post_revision: revision,
            mode: value.mode,
            reason: value.reason,
            ...(value.mode !== "disabled" && source
              ? { capture_uuid: source.capture_uuid }
              : {}),
          },
          controller.signal,
        );
        if (!controller.signal.aborted) setPreview(result);
      } catch (error) {
        if (!controller.signal.aborted) setError(error);
      } finally {
        if (!controller.signal.aborted) setBusy(false);
        if (pending.current === controller) pending.current = null;
      }
    },
  });
  const blocked = disabled || busy;
  const options = (lists.data?.items ?? []).map((item, index) => ({
    value: item.uuid,
    label: intl.formatMessage(
      {
        id: "attachment_selection.list_option",
        defaultMessage:
          "List {number}: {count, number} positions · {completeness}",
      },
      {
        number: index + 1,
        count: item.entry_count,
        completeness: item.complete
          ? msg("source_albums.complete", "Complete source list")
          : msg("source_albums.partial", "Partial source list"),
      },
    ),
  }));
  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        event.stopPropagation();
        void form.handleSubmit();
      }}
    >
      <form.Field name="mode">
        {(field) => (
          <Field data-disabled={blocked}>
            <FieldLabel id={`${id}-mode`}>
              {msg("attachment_selection.behavior", "Source-list behavior")}
            </FieldLabel>
            <ToggleGroup
              aria-labelledby={`${id}-mode`}
              variant="outline"
              value={[field.state.value]}
              disabled={blocked}
              onValueChange={(values) => {
                if (values[0]) {
                  field.handleChange(values[0]);
                  setPreview(undefined);
                }
              }}
              className="flex-wrap"
            >
              {selectionModeSchema.options.map((mode) => (
                <ToggleGroupItem key={mode} value={mode}>
                  {labels[mode]}
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
            <FieldDescription>
              {field.state.value === "pinned"
                ? msg(
                    "attachment_selection.pinned_help",
                    "Use the exact order in this list and protect it from automatic replacement.",
                  )
                : field.state.value === "automatic"
                  ? msg(
                      "attachment_selection.automatic_help",
                      "Start with this list. Later compatible source captures may add known positions; conflicts still require review.",
                    )
                  : msg(
                      "attachment_selection.disabled_help",
                      "Stop selecting source order automatically. Retained evidence and existing gallery members stay available.",
                    )}
            </FieldDescription>
          </Field>
        )}
      </form.Field>
      <form.Subscribe selector={(state) => state.values.mode}>
        {(mode) =>
          mode !== "disabled" && (
            <>
              <form.Field name="manifest">
                {(field) => (
                  <Field data-disabled={blocked}>
                    <FieldLabel htmlFor={`${id}-list`}>
                      {msg(
                        "attachment_selection.source_list",
                        "Retained source list",
                      )}
                    </FieldLabel>
                    <Select
                      items={options}
                      value={field.state.value || null}
                      disabled={blocked}
                      onValueChange={(value) => {
                        field.handleChange(value ?? "");
                        setPreview(undefined);
                      }}
                    >
                      <SelectTrigger id={`${id}-list`} className="w-full">
                        <SelectValue
                          placeholder={msg(
                            "attachment_selection.choose_list",
                            "Choose a source list",
                          )}
                        />
                      </SelectTrigger>
                      <SelectContent>
                        {options.map((option) => (
                          <SelectItem key={option.value} value={option.value}>
                            {option.label}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <FieldDescription>
                      {msg(
                        "attachment_selection.unique_lists",
                        "Identical lists from repeated captures appear once. Preview a list to inspect its positions.",
                      )}
                    </FieldDescription>
                  </Field>
                )}
              </form.Field>
              {lists.busy && <Spinner />}
              {lists.error !== undefined && (
                <ReviewError error={lists.error} retry={lists.reload} />
              )}
              {lists.data?.items.length === 0 && (
                <p className="text-sm text-muted-foreground">
                  {msg(
                    "attachment_selection.no_lists",
                    "No source attachment lists have been retained for this post.",
                  )}
                </p>
              )}
              {lists.data?.next !== null && lists.data?.next !== undefined && (
                <Button
                  type="button"
                  variant="outline"
                  disabled={lists.busy || blocked}
                  onClick={() => void lists.more()}
                >
                  {msg(
                    "attachment_selection.more_lists",
                    "Load more source lists",
                  )}
                </Button>
              )}
            </>
          )
        }
      </form.Subscribe>
      <form.Field name="reason">
        {(field) => (
          <Field data-disabled={blocked}>
            <FieldLabel htmlFor={`${id}-reason`}>
              {msg("attachment_selection.reason", "Reason (optional)")}
            </FieldLabel>
            <Input
              id={`${id}-reason`}
              value={field.state.value}
              disabled={blocked}
              onBlur={field.handleBlur}
              onChange={(event) => {
                field.handleChange(event.target.value);
                setPreview(undefined);
              }}
            />
            <FieldError errors={field.state.meta.errors} />
          </Field>
        )}
      </form.Field>
      {error !== undefined && <ReviewError error={error} />}
      <form.Subscribe
        selector={(state) => ({
          valid: state.canSubmit,
          mode: state.values.mode,
          manifest: state.values.manifest,
        })}
      >
        {({ valid, mode, manifest }) => (
          <Button
            type="submit"
            variant="outline"
            disabled={
              blocked ||
              !valid ||
              (mode !== "disabled" &&
                !options.some((item) => item.value === manifest))
            }
          >
            {busy && <Spinner data-icon="inline-start" />}
            {msg("attachment_selection.preview", "Preview source order")}
          </Button>
        )}
      </form.Subscribe>
      {preview && (
        <div className="flex flex-col gap-4">
          <div className="grid gap-3 md:grid-cols-2">
            <SelectionListDetails
              key={`current:${preview.digest}`}
              value={preview.current}
              title={msg("attachment_selection.current", "Current choice")}
            />
            <SelectionListDetails
              key={`proposed:${preview.digest}`}
              value={preview.proposed}
              title={msg("attachment_selection.proposed", "Proposed choice")}
            />
          </div>
          <p className="text-sm text-muted-foreground">
            {msg(
              "attachment_selection.separate_gallery",
              "This saves source order only. Use Match existing media to preview and apply gallery membership changes.",
            )}
          </p>
          {preview.changed ? (
            <Button
              type="button"
              disabled={blocked}
              onClick={() => {
                if (!blocked) void onApply(preview);
              }}
            >
              {msg("attachment_selection.apply", "Save source-list choice")}
            </Button>
          ) : (
            <p>
              {msg(
                "attachment_selection.unchanged",
                "This source-list choice is already saved.",
              )}
            </p>
          )}
        </div>
      )}
    </form>
  );
}
