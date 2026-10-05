import { useCallback, useEffect, useId, useState } from "react";
import { useForm } from "@tanstack/react-form";
import { Plus } from "lucide-react";
import { useMsg } from "@/hooks/message";
import { cn } from "@/lib/utils";
import { useListScrollRestoration } from "@/components/list/use-list-scroll-restoration";
import {
  createMediaRootAPI,
  mediaRootFilterSchema,
  type MediaRoot,
  type MediaRootFilter,
} from "@/core/native-archive/media-root-api";
import { requestUUID } from "@/core/native-archive/review-storage";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import {
  Field,
  FieldGroup,
  FieldLabel,
  FieldError,
} from "@/components/ui/field";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from "@/components/ui/card";
import {
  Empty,
  EmptyHeader,
  EmptyTitle,
  EmptyDescription,
} from "@/components/ui/empty";
import { ReviewError } from "@/components/detail/native-metadata/shared";
import { useCollectionLabels } from "./collections/shared";
import { MediaRootEditor } from "./media-roots/editor";

function RootSearch({
  filter,
  onChange,
}: {
  filter: MediaRootFilter;
  onChange: (filter: MediaRootFilter) => void;
}) {
  const msg = useMsg();
  const id = useId();
  const labels = useCollectionLabels();
  const form = useForm({
    defaultValues: filter,
    validators: { onChange: mediaRootFilterSchema },
    onSubmit: ({ value }) => onChange(value),
  });
  return (
    <form
      className="flex flex-col gap-4"
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
              <FieldLabel htmlFor={`${id}-q`}>
                {msg("media_roots.search", "Search root names or server paths")}
              </FieldLabel>
              <Input
                id={`${id}-q`}
                value={field.state.value}
                maxLength={256}
                aria-invalid={!field.state.meta.isValid}
                onBlur={field.handleBlur}
                onChange={(event) => field.handleChange(event.target.value)}
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
        <form.Field name="state">
          {(field) => (
            <Field>
              <FieldLabel id={`${id}-state`}>
                {msg("collections.state", "Status")}
              </FieldLabel>
              <ToggleGroup
                aria-labelledby={`${id}-state`}
                variant="outline"
                value={[field.state.value]}
                onValueChange={(values) => {
                  if (values[0] !== undefined) field.handleChange(values[0]);
                }}
              >
                <ToggleGroupItem value="">
                  {msg("collections.all_states", "All")}
                </ToggleGroupItem>
                <ToggleGroupItem value="active">
                  {labels.states.active}
                </ToggleGroupItem>
                <ToggleGroupItem value="disabled">
                  {labels.states.disabled}
                </ToggleGroupItem>
                <ToggleGroupItem value="retired">
                  {labels.states.retired}
                </ToggleGroupItem>
              </ToggleGroup>
            </Field>
          )}
        </form.Field>
      </FieldGroup>
      <form.Subscribe selector={(state) => state.canSubmit}>
        {(canSubmit) => (
          <Button type="submit" className="w-fit" disabled={!canSubmit}>
            {msg("media_roots.search_action", "Find media roots")}
          </Button>
        )}
      </form.Subscribe>
    </form>
  );
}

export function MediaRoots({
  filter,
  selected,
  create = false,
  onFilterChange,
  onSelect,
}: {
  filter: MediaRootFilter;
  selected?: string;
  create?: boolean;
  onFilterChange: (filter: MediaRootFilter) => void;
  onSelect: (id?: string, create?: boolean) => void;
}) {
  const msg = useMsg();
  const labels = useCollectionLabels();
  const [api] = useState(() => createMediaRootAPI());
  const [cursor, setCursor] = useState("");
  const [previous, setPrevious] = useState<string[]>([]);
  const [page, setPage] = useState<{
    key: string;
    rows: MediaRoot[];
    more: boolean;
    next: string;
  }>();
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState<unknown>();
  const [refresh, setRefresh] = useState(0);
  const [scroller, setScroller] = useState<HTMLDivElement | null>(null);
  const { q, state } = filter;
  const queryKey = JSON.stringify([q, state, cursor]);
  // biome-ignore lint/correctness/useExhaustiveDependencies: Retry refreshes one bounded page. Editing updates the affected card.
  useEffect(() => {
    const controller = new AbortController();
    setBusy(true);
    setError(undefined);
    api
      .roots({ q, state }, cursor, controller.signal)
      .then((rows) => {
        if (!controller.signal.aborted)
          setPage({
            key: queryKey,
            rows,
            more: rows.length === api.pageLimit,
            next: rows.at(-1)?.uuid ?? "",
          });
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted) setError(error);
      })
      .finally(() => {
        if (!controller.signal.aborted) setBusy(false);
      });
    return () => controller.abort();
  }, [api, q, state, cursor, queryKey, refresh]);
  const current = page?.key === queryKey ? page : undefined;
  useListScrollRestoration(
    "media-roots",
    scroller,
    !selected && !!current && !busy,
    queryKey,
  );
  const changed = useCallback(
    (root: MediaRoot) => {
      if (create) {
        setCursor("");
        setPrevious([]);
        setRefresh((value) => value + 1);
        return;
      }
      const fold = (value: string) =>
        value.replace(/[A-Z]/g, (letter) => letter.toLowerCase());
      const matches =
        (root.uuid === q ||
          [root.label, root.binding?.path ?? ""].some((value) =>
            fold(value).includes(fold(q)),
          )) &&
        (!state || root.state === state);
      setPage((page) =>
        page
          ? {
              ...page,
              rows: page.rows.flatMap((row) =>
                row.uuid !== root.uuid ? [row] : matches ? [root] : [],
              ),
            }
          : page,
      );
    },
    [q, state, create],
  );
  return (
    <div
      ref={setScroller}
      className="min-h-0 flex-1 overflow-y-auto"
      data-scroll-restoration-id="media-roots"
    >
      <div className="mx-auto flex w-full max-w-4xl flex-col gap-6 p-4 md:p-6">
        <header className="flex flex-col gap-2">
          <h1 className="text-2xl font-semibold">
            {msg("media_roots.title", "Media roots")}
          </h1>
          <p className="text-muted-foreground">
            {msg(
              "media_roots.description",
              "Manage the server folders behind portable collection paths.",
            )}
          </p>
        </header>
        {selected && (
          <MediaRootEditor
            key={selected}
            api={api}
            id={selected}
            create={create}
            onBack={() => onSelect()}
            onChanged={changed}
          />
        )}
        <div className={cn("flex flex-col gap-6", selected && "hidden")}>
          <Button
            type="button"
            className="w-fit"
            onClick={() => onSelect(requestUUID(), true)}
          >
            <Plus data-icon="inline-start" />
            {msg("media_roots.new", "New media root")}
          </Button>
          <RootSearch filter={filter} onChange={onFilterChange} />
          {busy && <Spinner />}
          {error !== undefined && (
            <ReviewError
              error={error}
              retry={() => setRefresh((value) => value + 1)}
            />
          )}
          {!busy &&
            error === undefined &&
            current &&
            (current.rows.length ? (
              current.rows.map((root) => (
                <Card key={root.uuid}>
                  <CardHeader>
                    <CardTitle>{root.label}</CardTitle>
                    <CardDescription>
                      {labels.states[root.state]}
                    </CardDescription>
                  </CardHeader>
                  <CardContent>
                    <p data-selectable-text className="wrap-anywhere">
                      {root.binding?.path ??
                        msg("media_roots.unbound", "No local folder")}
                    </p>
                  </CardContent>
                  <CardFooter>
                    <Button
                      type="button"
                      variant="outline"
                      onClick={() => onSelect(root.uuid)}
                    >
                      {msg("media_roots.manage", "Manage root")}
                    </Button>
                  </CardFooter>
                </Card>
              ))
            ) : (
              <Empty>
                <EmptyHeader>
                  <EmptyTitle>
                    {msg("media_roots.empty", "No matching media roots")}
                  </EmptyTitle>
                  <EmptyDescription>
                    {msg(
                      "media_roots.empty_help",
                      "Try another search or register a root for an existing server folder.",
                    )}
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            ))}
          <div className="flex gap-2">
            <Button
              type="button"
              variant="outline"
              disabled={busy || !previous.length}
              onClick={() => {
                setCursor(previous.at(-1) ?? "");
                setPrevious((values) => values.slice(0, -1));
              }}
            >
              {msg("actions.previous", "Previous")}
            </Button>
            <Button
              type="button"
              variant="outline"
              disabled={busy || error !== undefined || !current?.more}
              onClick={() => {
                if (current) {
                  setPrevious((values) => [...values, cursor]);
                  setCursor(current.next);
                }
              }}
            >
              {msg("actions.next", "Next")}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}
