import { useEffect, useId, useState } from "react";
import { useForm } from "@tanstack/react-form";
import { z } from "zod";
import { Folder, ArrowUp, Search } from "lucide-react";
import { useMsg } from "@/hooks/message";
import type { Collection } from "@/core/native-archive/collection-api";
import {
  manualBatchLimit,
  type ManualDirectoryEntry,
  type ManualDirectoryPage,
  type ManualIntakeAPI,
} from "@/core/native-archive/manual-intake-api";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import {
  Field,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
  FieldError,
} from "@/components/ui/field";
import { Empty, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Spinner } from "@/components/ui/spinner";
import { ReviewError } from "@/components/detail/native-metadata/shared";
import { FileSize } from "./shared";

const searchSchema = z.object({
  q: z
    .string()
    .refine(
      (q) => new TextEncoder().encode(q).length <= 256 && !/\p{Cc}/u.test(q),
    ),
});
export function ManualFilePicker({
  api,
  collection,
  disabled,
  selected,
  onSelect,
}: {
  api: ManualIntakeAPI;
  collection: Collection;
  disabled: boolean;
  selected: ManualDirectoryEntry[];
  onSelect: (entries: ManualDirectoryEntry[]) => void;
}) {
  const msg = useMsg(),
    id = useId();
  const [location, setLocation] = useState({
    folder: collection.path_prefix,
    q: "",
    cursors: [] as ManualDirectoryPage[],
    refresh: 0,
  });
  const [page, setPage] = useState<ManualDirectoryPage>();
  const [error, setError] = useState<unknown>();
  const [loading, setLoading] = useState(true);
  const form = useForm({
    defaultValues: { q: "" },
    validators: { onChange: searchSchema },
    onSubmit: ({ value }) => {
      setLocation((old) => ({ ...old, q: value.q, cursors: [] }));
    },
  });
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setPage(undefined);
    setError(undefined);
    void api
      .directory(
        collection,
        location.folder,
        location.q,
        location.cursors.at(-1),
        controller.signal,
      )
      .then((result) => {
        if (!controller.signal.aborted) setPage(result);
      })
      .catch((failure: unknown) => {
        if (!controller.signal.aborted) setError(failure);
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false);
      });
    return () => controller.abort();
  }, [api, collection, location]);
  function navigate(folder: string) {
    form.reset();
    setLocation({ folder, q: "", cursors: [], refresh: 0 });
  }
  return (
    <div className="flex min-w-0 flex-col gap-4">
      <div className="flex flex-wrap items-center gap-2">
        <Button
          variant="outline"
          size="sm"
          disabled={
            disabled || loading || location.folder === collection.path_prefix
          }
          onClick={() =>
            navigate(location.folder.split("/").slice(0, -1).join("/") || ".")
          }
        >
          <ArrowUp data-icon="inline-start" />
          {msg("manual_intake.parent", "Parent folder")}
        </Button>
        <p data-selectable-text className="min-w-0 wrap-anywhere text-sm">
          {location.folder}
        </p>
      </div>
      <form
        className="flex flex-col gap-3"
        onSubmit={(event) => {
          event.preventDefault();
          event.stopPropagation();
          void form.handleSubmit();
        }}
      >
        <FieldGroup>
          <form.Field name="q">
            {(field) => (
              <Field data-invalid={!field.state.meta.isValid}>
                <FieldLabel htmlFor={`${id}-search`}>
                  {msg("manual_intake.search", "Find files in this folder")}
                </FieldLabel>
                <Input
                  id={`${id}-search`}
                  value={field.state.value}
                  disabled={disabled || loading}
                  onChange={(event) => field.handleChange(event.target.value)}
                  onBlur={field.handleBlur}
                  aria-invalid={!field.state.meta.isValid}
                />
                {!field.state.meta.isValid && (
                  <FieldError>
                    {msg(
                      "account_review.search_invalid",
                      "Use a shorter search without control characters.",
                    )}
                  </FieldError>
                )}
              </Field>
            )}
          </form.Field>
        </FieldGroup>
        <form.Subscribe selector={(state) => state.canSubmit}>
          {(canSubmit) => (
            <Button
              type="submit"
              variant="outline"
              className="w-fit"
              disabled={disabled || loading || !canSubmit}
            >
              <Search data-icon="inline-start" />
              {msg("actions.search", "Search")}
            </Button>
          )}
        </form.Subscribe>
      </form>
      {loading && <Spinner aria-label={msg("actions.loading", "Loading…")} />}
      {!!error && (
        <ReviewError
          error={error}
          retry={() =>
            setLocation((old) => ({
              ...old,
              cursors: [],
              refresh: old.refresh + 1,
            }))
          }
        />
      )}
      {page && (
        <>
          {page.entries.length === 0 ? (
            <Empty>
              <EmptyHeader>
                <EmptyTitle>
                  {msg(
                    "manual_intake.empty",
                    "No supported files or folders found",
                  )}
                </EmptyTitle>
              </EmptyHeader>
            </Empty>
          ) : (
            <FieldSet disabled={disabled || loading}>
              <FieldLegend>
                {msg("manual_intake.choose", "Choose images and videos")}
              </FieldLegend>
              <FieldGroup>
                {page.entries.map((entry, index) =>
                  entry.kind === "directory" ? (
                    <Button
                      key={entry.relative_path}
                      variant="ghost"
                      className="h-auto min-h-10 justify-start whitespace-normal text-left"
                      onClick={() => navigate(entry.relative_path)}
                    >
                      <Folder data-icon="inline-start" />
                      <span className="min-w-0 wrap-anywhere">
                        {entry.name}
                      </span>
                    </Button>
                  ) : (
                    <Field key={entry.relative_path} orientation="horizontal">
                      <Checkbox
                        id={`${id}-file-${index}`}
                        checked={selected.some(
                          (file) => file.relative_path === entry.relative_path,
                        )}
                        disabled={
                          disabled ||
                          (!selected.some(
                            (file) =>
                              file.relative_path === entry.relative_path,
                          ) &&
                            selected.length >= manualBatchLimit)
                        }
                        onCheckedChange={(checked) =>
                          onSelect(
                            checked
                              ? [...selected, entry]
                              : selected.filter(
                                  (file) =>
                                    file.relative_path !== entry.relative_path,
                                ),
                          )
                        }
                      />
                      <FieldLabel
                        htmlFor={`${id}-file-${index}`}
                        className="min-w-0 flex-1 flex-wrap"
                      >
                        <span className="min-w-0 wrap-anywhere">
                          {entry.name}
                        </span>
                        <FileSize bytes={entry.size} />
                      </FieldLabel>
                    </Field>
                  ),
                )}
              </FieldGroup>
            </FieldSet>
          )}
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              disabled={disabled || loading || !location.cursors.length}
              onClick={() =>
                setLocation((old) => ({
                  ...old,
                  cursors: old.cursors.slice(0, -1),
                }))
              }
            >
              {msg("manual_intake.previous_page", "Previous files")}
            </Button>
            <Button
              variant="outline"
              disabled={disabled || loading || !page.next_after}
              onClick={() =>
                setLocation((old) => ({
                  ...old,
                  cursors: [...old.cursors, page],
                }))
              }
            >
              {msg("manual_intake.next_page", "Next files")}
            </Button>
          </div>
        </>
      )}
    </div>
  );
}
