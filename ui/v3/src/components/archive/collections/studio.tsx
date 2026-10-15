import { useCallback, useMemo, useState } from "react";
import { useForm } from "@tanstack/react-form";
import { useMsg } from "@/hooks/message";
import {
  createSourceStudioAPI,
  type SourceStudio,
  type SourceStudioAssignment,
} from "@/core/native-archive/source-studio-api";
import { ExistingEntityPicker } from "@/components/detail/native-metadata/picker";
import { ReviewError } from "@/components/detail/native-metadata/shared";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from "@/components/ui/card";
import { Field, FieldLabel, FieldGroup } from "@/components/ui/field";
import { Spinner } from "@/components/ui/spinner";
import { usePostRead } from "../posts/read";
import { SourceStudios } from "../source-studios";

type StudioAPI = ReturnType<typeof createSourceStudioAPI>;

function StudioForm({
  api,
  current,
  disabled,
  saved,
}: {
  api: StudioAPI;
  current: SourceStudioAssignment;
  disabled: boolean;
  saved: () => void;
}) {
  const msg = useMsg();
  const [error, setError] = useState<unknown>();
  const form = useForm({
    defaultValues: { studio: current.studio as SourceStudio | null },
    onSubmit: async ({ value }) => {
      setError(undefined);
      try {
        await api.save(current, value.studio);
        saved();
      } catch (error) {
        setError(error);
      }
    },
  });
  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        event.stopPropagation();
        void form.handleSubmit();
      }}
    >
      <FieldGroup>
        <form.Subscribe selector={(state) => state.isSubmitting}>
          {(submitting) => (
            <form.Field name="studio">
              {(field) => (
                <Field>
                  <FieldLabel
                    htmlFor={`source-studio-${current.collection_uuid}`}
                  >
                    {msg("studio", "Studio")}
                  </FieldLabel>
                  {field.state.value && (
                    <SourceStudios studios={[field.state.value]} />
                  )}
                  <ExistingEntityPicker
                    kind="studio"
                    id={`source-studio-${current.collection_uuid}`}
                    disabled={disabled || submitting}
                    onChange={(choice) =>
                      field.handleChange({ uuid: choice.id, name: choice.name })
                    }
                  />
                  <div className="flex flex-wrap gap-2">
                    <Button type="submit" disabled={disabled || submitting}>
                      {msg("actions.save", "Save")}
                    </Button>
                    <Button
                      type="button"
                      variant="outline"
                      disabled={disabled || submitting || !field.state.value}
                      onClick={() => field.handleChange(null)}
                    >
                      {msg("actions.clear", "Clear")}
                    </Button>
                  </div>
                </Field>
              )}
            </form.Field>
          )}
        </form.Subscribe>
        {error !== undefined && <ReviewError error={error} retry={saved} />}
      </FieldGroup>
    </form>
  );
}

export function CollectionStudio({
  endpoint,
  collection,
  disabled,
}: {
  endpoint: string;
  collection: string;
  disabled: boolean;
}) {
  const msg = useMsg();
  const api = useMemo(() => createSourceStudioAPI(endpoint), [endpoint]);
  const load = useCallback(
    (signal: AbortSignal) => api.assignment(collection, signal),
    [api, collection],
  );
  const read = usePostRead(load);
  return (
    <Card>
      <CardHeader>
        <CardTitle>
          {msg("source_studios.collection_title", "Source studio")}
        </CardTitle>
        <CardDescription>
          {msg(
            "source_studios.help",
            "Associate this source with an existing studio, such as a subreddit or repost account. Posts show every source studio. Source albums inherit a primary studio only when there is one clear match; explicit gallery choices are preserved.",
          )}
        </CardDescription>
      </CardHeader>
      <CardContent>
        {read.busy && <Spinner />}
        {read.error !== undefined && (
          <ReviewError error={read.error} retry={read.retry} />
        )}
        {read.value && (
          <StudioForm
            key={`${read.value.collection_uuid}:${read.value.revision}`}
            api={api}
            current={read.value}
            disabled={disabled || read.busy || read.error !== undefined}
            saved={read.retry}
          />
        )}
      </CardContent>
    </Card>
  );
}
