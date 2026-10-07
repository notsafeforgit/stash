import { useCallback, useId, useState } from "react";
import { useForm } from "@tanstack/react-form";
import { useMsg } from "@/hooks/message";
import {
  activitySearchSchema,
  jobKindSchema,
  jobStateSchema,
  runStateSchema,
  type ActivitySearch,
} from "@/core/native-archive/activity-schema";
import {
  createCollectionAPI,
  type Collection,
} from "@/core/native-archive/collection-api";
import { LookupPicker } from "../collections/lookup-picker";
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { Button } from "@/components/ui/button";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { useActivityLabels, useActivityRead } from "./shared";

function CollectionFilter({
  id,
  value,
  onChange,
}: {
  id: string;
  value: string;
  onChange: (value: string) => void;
}) {
  const msg = useMsg();
  const [api] = useState(() => createCollectionAPI());
  const [refresh, setRefresh] = useState(0);
  const load = useCallback(
    (signal: AbortSignal) =>
      value ? api.collection(value, signal) : Promise.resolve(null),
    [api, value],
  );
  const read = useActivityRead(value, load, refresh);
  const search = useCallback(
    (q: string, signal: AbortSignal) =>
      api.collections({ q, kind: "", state: "" }, "", signal),
    [api],
  );
  return (
    <div className="flex flex-col gap-2">
      <LookupPicker<Collection>
        id={id}
        value={read.current ?? null}
        disabled={Boolean(value && read.busy && !read.current)}
        search={search}
        label={(row) => row.label}
        onChange={(row) => onChange(row?.uuid ?? "")}
      />
      {value && read.busy && !read.current && (
        <FieldDescription>
          {msg(
            "archive_activity.loading_collection",
            "Loading selected collection…",
          )}
        </FieldDescription>
      )}
      {value && read.error !== undefined && (
        <Alert variant="destructive">
          <AlertTitle>
            {msg(
              "archive_activity.collection_failed",
              "Could not load the selected collection",
            )}
          </AlertTitle>
          <AlertDescription>
            <p>
              {msg(
                "archive_activity.collection_filter_retained",
                "The collection filter is still applied. Retry or clear it to show all collections.",
              )}
            </p>
            <div className="flex flex-wrap gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={() => setRefresh((value) => value + 1)}
              >
                {msg("actions.retry", "Retry")}
              </Button>
              <Button variant="outline" size="sm" onClick={() => onChange("")}>
                {msg(
                  "archive_activity.clear_collection",
                  "Clear collection filter",
                )}
              </Button>
            </div>
          </AlertDescription>
        </Alert>
      )}
    </div>
  );
}

export function ActivityFilters({
  value,
  onChange,
}: {
  value: ActivitySearch;
  onChange: (value: ActivitySearch) => void;
}) {
  const msg = useMsg();
  const labels = useActivityLabels();
  const prefix = useId();
  const form = useForm({
    defaultValues: value,
    validators: {
      onChange: ({ value }) =>
        activitySearchSchema.safeParse(value).error?.message,
    },
    onSubmit: ({ value }) => onChange({ ...value, item: undefined }),
  });
  const states =
    value.view === "jobs" ? jobStateSchema.options : runStateSchema.options;
  const kinds = [
    { value: "", label: msg("archive_activity.all_kinds", "All job types") },
    ...jobKindSchema.options.map((value) => ({
      value,
      label: labels.kinds[value],
    })),
  ];
  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        void form.handleSubmit();
      }}
    >
      <FieldGroup>
        {value.view === "jobs" ? (
          <form.Field name="kind">
            {(field) => (
              <Field>
                <FieldLabel htmlFor={`${prefix}-kind`}>
                  {msg("archive_activity.job_type", "Job type")}
                </FieldLabel>
                <Select
                  items={kinds}
                  value={field.state.value}
                  onValueChange={(value) =>
                    field.handleChange(
                      activitySearchSchema.shape.kind.parse(value ?? ""),
                    )
                  }
                >
                  <SelectTrigger id={`${prefix}-kind`} className="w-full">
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
        ) : (
          <form.Field name="collection">
            {(field) => (
              <Field>
                <FieldLabel htmlFor={`${prefix}-collection`}>
                  {msg("archive_activity.collection", "Source collection")}
                </FieldLabel>
                <CollectionFilter
                  id={`${prefix}-collection`}
                  value={field.state.value}
                  onChange={field.handleChange}
                />
                <FieldDescription>
                  {msg(
                    "archive_activity.all_collections",
                    "Leave empty to show all source collections.",
                  )}
                </FieldDescription>
              </Field>
            )}
          </form.Field>
        )}
        <form.Field name="state">
          {(field) => (
            <Field>
              <FieldLabel id={`${prefix}-state`}>
                {msg("archive_activity.status", "Status")}
              </FieldLabel>
              <ToggleGroup
                aria-labelledby={`${prefix}-state`}
                variant="outline"
                value={[field.state.value || "all"]}
                onValueChange={(values) => {
                  const value = values[0];
                  if (value)
                    field.handleChange(
                      activitySearchSchema.shape.state.parse(
                        value === "all" ? "" : value,
                      ),
                    );
                }}
                className="flex-wrap"
              >
                <ToggleGroupItem value="all">
                  {msg("archive_activity.all_states", "All")}
                </ToggleGroupItem>
                {states.map((state) => (
                  <ToggleGroupItem key={state} value={state}>
                    {labels.states[state]}
                  </ToggleGroupItem>
                ))}
              </ToggleGroup>
            </Field>
          )}
        </form.Field>
        <div>
          <Button type="submit" variant="outline">
            {msg("archive_activity.apply_filters", "Apply filters")}
          </Button>
        </div>
      </FieldGroup>
    </form>
  );
}
