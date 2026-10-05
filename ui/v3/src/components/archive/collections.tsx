import { useCallback, useEffect, useId, useState } from "react";
import { useForm } from "@tanstack/react-form";
import { Plus } from "lucide-react";
import { useMsg } from "@/hooks/message";
import { cn } from "@/lib/utils";
import { useListScrollRestoration } from "@/components/list/use-list-scroll-restoration";
import {
  collectionFilterSchema,
  collectionQuerySchema,
  createCollectionAPI,
  type Collection,
  type CollectionFilter,
} from "@/core/native-archive/collection-api";
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
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectGroup,
  SelectItem,
} from "@/components/ui/select";
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
import { CollectionEditor } from "./collections/editor";

function CollectionSearch({
  filter,
  onChange,
}: {
  filter: CollectionFilter;
  onChange: (filter: CollectionFilter) => void;
}) {
  const msg = useMsg();
  const labels = useCollectionLabels();
  const id = useId();
  const form = useForm({
    defaultValues: filter,
    validators: { onChange: collectionQuerySchema },
    onSubmit: ({ value }) => onChange(value),
  });
  const kinds = [
    { value: "", label: msg("collections.all_types", "All types") },
    ...Object.entries(labels.kinds).map(([value, label]) => ({ value, label })),
  ];
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
              <FieldLabel htmlFor={`${id}-query`}>
                {msg(
                  "collections.search",
                  "Search names, source URLs or folders",
                )}
              </FieldLabel>
              <Input
                id={`${id}-query`}
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
        <form.Field name="kind">
          {(field) => (
            <Field>
              <FieldLabel htmlFor={`${id}-kind`}>
                {msg("collections.kind", "Collection type")}
              </FieldLabel>
              <Select
                items={kinds}
                value={field.state.value}
                onValueChange={(value) => {
                  const kind =
                    collectionFilterSchema.shape.kind.safeParse(value);
                  if (kind.success) field.handleChange(kind.data);
                }}
              >
                <SelectTrigger id={`${id}-kind`} className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {kinds.map((item) => (
                      <SelectItem key={item.value} value={item.value}>
                        {item.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>
          )}
        </form.Field>
      </FieldGroup>
      <form.Subscribe selector={(state) => state.canSubmit}>
        {(canSubmit) => (
          <Button type="submit" className="w-fit" disabled={!canSubmit}>
            {msg("collections.search_action", "Find collections")}
          </Button>
        )}
      </form.Subscribe>
    </form>
  );
}

export function Collections({
  filter,
  selected,
  create = false,
  onFilterChange,
  onSelect,
}: {
  filter: CollectionFilter;
  selected?: string;
  create?: boolean;
  onFilterChange: (filter: CollectionFilter) => void;
  onSelect: (id?: string, create?: boolean) => void;
}) {
  const msg = useMsg();
  const labels = useCollectionLabels();
  const [api] = useState(() => createCollectionAPI());
  const [cursor, setCursor] = useState("");
  const [previous, setPrevious] = useState<string[]>([]);
  const [page, setPage] = useState<{
    key: string;
    rows: Collection[];
    more: boolean;
    next: string;
  }>();
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState<unknown>();
  const [refresh, setRefresh] = useState(0);
  const [scroller, setScroller] = useState<HTMLDivElement | null>(null);
  const { q, state, kind } = filter;
  const queryKey = JSON.stringify([q, state, kind, cursor]);
  // biome-ignore lint/correctness/useExhaustiveDependencies: Retry refreshes one bounded page; editing updates the affected card.
  useEffect(() => {
    const controller = new AbortController();
    setBusy(true);
    setError(undefined);
    api
      .collections({ q, state, kind }, cursor, controller.signal)
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
  }, [api, q, state, kind, cursor, queryKey, refresh]);
  const current = page?.key === queryKey ? page : undefined;
  useListScrollRestoration(
    "collections",
    scroller,
    !selected && !!current && !busy,
    queryKey,
  );
  const changed = useCallback(
    (collection: Collection) => {
      if (create) {
        setCursor("");
        setPrevious([]);
        setRefresh((value) => value + 1);
        return;
      }
      // Match SQLite LIKE's ASCII case folding without treating %/_ as wildcards.
      const fold = (value: string) =>
        value.replace(/[A-Z]/g, (letter) => letter.toLowerCase());
      const matchesQuery =
        collection.uuid === q ||
        [collection.label, collection.target_url, collection.path_prefix].some(
          (value) => fold(value).includes(fold(q)),
        );
      setPage((page) =>
        page
          ? {
              ...page,
              rows: page.rows.flatMap((row) =>
                row.uuid !== collection.uuid
                  ? [row]
                  : matchesQuery &&
                      (!state || state === collection.state) &&
                      (!kind || kind === collection.kind)
                    ? [collection]
                    : [],
              ),
            }
          : page,
      );
    },
    [create, kind, q, state],
  );
  return (
    <div
      ref={setScroller}
      className="min-h-0 flex-1 overflow-y-auto"
      data-scroll-restoration-id="collections"
    >
      <div className="mx-auto flex w-full max-w-4xl flex-col gap-6 p-4 md:p-6">
        <header className="flex flex-col gap-2">
          <h1 className="text-2xl font-semibold">
            {msg("collections.title", "Source collections")}
          </h1>
          <p className="text-muted-foreground">
            {msg(
              "collections.description",
              "Manage scraped sources and groups of directly imported files.",
            )}
          </p>
        </header>
        {selected && (
          <CollectionEditor
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
            {msg("collections.new", "New collection")}
          </Button>
          <CollectionSearch filter={filter} onChange={onFilterChange} />
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
              current.rows.map((collection) => (
                <Card key={collection.uuid}>
                  <CardHeader>
                    <CardTitle>{collection.label}</CardTitle>
                    <CardDescription>
                      {labels.kinds[collection.kind]} ·{" "}
                      {labels.states[collection.state]}
                    </CardDescription>
                  </CardHeader>
                  <CardContent className="flex flex-col gap-2">
                    {collection.target_url && (
                      <p data-selectable-text className="wrap-anywhere">
                        {collection.target_url}
                      </p>
                    )}
                    {collection.path_prefix && (
                      <p data-selectable-text className="wrap-anywhere">
                        {collection.path_prefix}
                      </p>
                    )}
                  </CardContent>
                  <CardFooter>
                    <Button
                      type="button"
                      variant="outline"
                      onClick={() => onSelect(collection.uuid)}
                    >
                      {msg("collections.manage", "Manage collection")}
                    </Button>
                  </CardFooter>
                </Card>
              ))
            ) : (
              <Empty>
                <EmptyHeader>
                  <EmptyTitle>
                    {msg("collections.empty", "No matching collections")}
                  </EmptyTitle>
                  <EmptyDescription>
                    {msg(
                      "collections.empty_help",
                      "Try a different search or create a collection for a source or folder.",
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
