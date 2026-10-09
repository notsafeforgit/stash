import { useEffect, useId, useRef, useState } from "react";
import { useForm } from "@tanstack/react-form";
import { z } from "zod";
import { useMsg } from "@/hooks/message";
import {
  mediaRootInputSchema,
  type MediaRootAPI,
  type MediaRootInput,
} from "@/core/native-archive/media-root-api";
import { NativeArchiveError } from "@/core/native-archive/client";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Spinner } from "@/components/ui/spinner";
import {
  Field,
  FieldContent,
  FieldGroup,
  FieldLabel,
  FieldDescription,
  FieldError,
} from "@/components/ui/field";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { useCollectionLabels } from "../collections/shared";
import { ReviewError } from "@/components/detail/native-metadata/shared";

const formSchema = mediaRootInputSchema
  .extend({
    bound: z.boolean(),
    serverPath: z.string().max(4096),
  })
  .superRefine((value, context) => {
    if (
      value.bound
        ? !value.binding || value.serverPath !== value.binding.path
        : value.binding !== null
    )
      context.addIssue({
        code: "custom",
        path: ["serverPath"],
        message: "Check the folder before saving.",
      });
  });

export function MediaRootError({
  error,
  retry,
}: {
  error: unknown;
  retry?: () => void;
}) {
  const msg = useMsg();
  if (
    !(error instanceof NativeArchiveError) ||
    error.code !== "invalid_root_binding"
  )
    return <ReviewError error={error} retry={retry} />;
  return (
    <Alert variant="destructive">
      <AlertTitle>
        {msg("media_roots.invalid_binding", "The folder could not be verified")}
      </AlertTitle>
      <AlertDescription>
        <p>
          {msg(
            "media_roots.invalid_binding_help",
            "Check the server path and mount, then check the folder again before saving a new binding.",
          )}
        </p>
        {error.detail && (
          <p data-selectable-text className="whitespace-pre-wrap wrap-anywhere">
            {error.detail}
          </p>
        )}
        {retry && (
          <Button type="button" variant="outline" onClick={retry}>
            {msg("actions.retry", "Retry")}
          </Button>
        )}
      </AlertDescription>
    </Alert>
  );
}

export function MediaRootForm({
  api,
  input,
  disabled,
  onSave,
}: {
  api: MediaRootAPI;
  input: MediaRootInput;
  disabled: boolean;
  onSave: (input: MediaRootInput) => Promise<void>;
}) {
  const msg = useMsg();
  const labels = useCollectionLabels();
  const id = useId();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const [checked, setChecked] = useState(false);
  const pending = useRef<AbortController | null>(null);
  useEffect(() => () => pending.current?.abort(), []);
  const blocked = disabled || busy;
  const form = useForm({
    defaultValues: {
      ...input,
      bound: !!input.binding,
      serverPath: input.binding?.path ?? "",
    },
    validators: { onChange: formSchema },
    onSubmit: ({ value }) => {
      const { bound: _bound, serverPath: _serverPath, ...request } = value;
      return onSave(request);
    },
  });
  async function probe() {
    if (blocked) return;
    const controller = new AbortController();
    pending.current?.abort();
    pending.current = controller;
    setBusy(true);
    setChecked(false);
    setError(undefined);
    try {
      const binding = await api.probe(
        form.getFieldValue("serverPath"),
        controller.signal,
      );
      if (!controller.signal.aborted) {
        form.setFieldValue("serverPath", binding.path);
        form.setFieldValue("binding", binding);
        setChecked(true);
      }
    } catch (error) {
      if (!controller.signal.aborted) setError(error);
    } finally {
      if (!controller.signal.aborted) setBusy(false);
    }
  }
  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        event.stopPropagation();
        if (!blocked) void form.handleSubmit();
      }}
    >
      <FieldGroup>
        <form.Field name="label">
          {(field) => (
            <Field
              data-disabled={blocked}
              data-invalid={!field.state.meta.isValid}
            >
              <FieldLabel htmlFor={`${id}-label`}>
                {msg("media_roots.name", "Root name")}
              </FieldLabel>
              <Input
                id={`${id}-label`}
                disabled={blocked}
                value={field.state.value}
                maxLength={1024}
                aria-invalid={!field.state.meta.isValid}
                onBlur={field.handleBlur}
                onChange={(event) => field.handleChange(event.target.value)}
              />
            </Field>
          )}
        </form.Field>
        <form.Field name="bound">
          {(field) => (
            <Field orientation="horizontal" data-disabled={blocked}>
              <Switch
                id={`${id}-bound`}
                checked={field.state.value}
                disabled={blocked}
                onCheckedChange={(value) => {
                  field.handleChange(value);
                  form.setFieldValue(
                    "binding",
                    value &&
                      form.getFieldValue("serverPath") === input.binding?.path
                      ? input.binding
                      : null,
                  );
                  setChecked(false);
                  setError(undefined);
                }}
              />
              <FieldContent>
                <FieldLabel htmlFor={`${id}-bound`}>
                  {msg(
                    "media_roots.bound",
                    "Associate a folder on this server",
                  )}
                </FieldLabel>
                <FieldDescription>
                  {msg(
                    "media_roots.bound_help",
                    "An unbound root keeps its identity and collection paths for a later restore or relocation. It cannot accept file ingestion until a folder is bound.",
                  )}
                </FieldDescription>
              </FieldContent>
            </Field>
          )}
        </form.Field>
        <form.Subscribe selector={(state) => state.values.bound}>
          {(bound) =>
            bound && (
              <>
                <form.Field name="serverPath">
                  {(field) => (
                    <Field
                      data-disabled={blocked}
                      data-invalid={!field.state.meta.isValid}
                    >
                      <FieldLabel htmlFor={`${id}-path`}>
                        {msg(
                          "media_roots.path",
                          "Folder path on the Stash server",
                        )}
                      </FieldLabel>
                      <Input
                        id={`${id}-path`}
                        disabled={blocked}
                        value={field.state.value}
                        maxLength={4096}
                        aria-invalid={!field.state.meta.isValid}
                        onBlur={field.handleBlur}
                        onChange={(event) => {
                          field.handleChange(event.target.value);
                          form.setFieldValue(
                            "binding",
                            event.target.value === input.binding?.path
                              ? input.binding
                              : null,
                          );
                          setChecked(false);
                          setError(undefined);
                        }}
                      />
                      <FieldDescription>
                        {msg(
                          "media_roots.path_help",
                          "Use the existing absolute path visible to Stash, including its container mount. Registration does not move files, create folders or start a scan.",
                        )}
                      </FieldDescription>
                    </Field>
                  )}
                </form.Field>
                <Button
                  type="button"
                  variant="outline"
                  className="w-fit"
                  disabled={blocked}
                  onClick={() => void probe()}
                >
                  {busy && <Spinner data-icon="inline-start" />}
                  {msg("media_roots.check", "Check folder")}
                </Button>
                {checked && (
                  <Alert>
                    <AlertTitle>
                      {msg("media_roots.checked", "Folder checked")}
                    </AlertTitle>
                    <AlertDescription>
                      {msg(
                        "media_roots.checked_help",
                        "Stash can open this directory. Saving a new binding or reactivating a root checks this directory identity again.",
                      )}
                    </AlertDescription>
                  </Alert>
                )}
                {error !== undefined && <MediaRootError error={error} />}
              </>
            )
          }
        </form.Subscribe>
        <form.Field name="state">
          {(field) => (
            <Field data-disabled={blocked}>
              <FieldLabel id={`${id}-state`}>
                {msg("collections.state", "Status")}
              </FieldLabel>
              <ToggleGroup
                variant="outline"
                aria-labelledby={`${id}-state`}
                disabled={blocked}
                value={[field.state.value]}
                onValueChange={(values) => {
                  if (values[0]) field.handleChange(values[0]);
                }}
              >
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
              <FieldDescription>
                {msg(
                  "media_roots.state_help",
                  "Disabled pauses use of this root. Retired marks it as no longer in use. Both retain their folder binding and can be restored by choosing Active and saving, which verifies the folder again. Neither deletes media or collections.",
                )}
              </FieldDescription>
            </Field>
          )}
        </form.Field>
        <form.Field name="reason">
          {(field) => (
            <Field
              data-disabled={blocked}
              data-invalid={!field.state.meta.isValid}
            >
              <FieldLabel htmlFor={`${id}-reason`}>
                {msg("collections.reason", "Reason for this change (optional)")}
              </FieldLabel>
              <Input
                id={`${id}-reason`}
                value={field.state.value}
                disabled={blocked}
                aria-invalid={!field.state.meta.isValid}
                onBlur={field.handleBlur}
                onChange={(event) => field.handleChange(event.target.value)}
              />
            </Field>
          )}
        </form.Field>
      </FieldGroup>
      <form.Subscribe selector={(state) => state.values}>
        {(values) => {
          const valid = formSchema.safeParse(values).success;
          return (
            <>
              {!valid && (
                <FieldError>
                  {msg(
                    "media_roots.invalid_form",
                    "Enter a root name and check any new folder before saving. Remove control characters from names and reasons.",
                  )}
                </FieldError>
              )}
              <Button
                type="submit"
                className="w-fit"
                disabled={blocked || !valid}
              >
                {msg("media_roots.save", "Save media root")}
              </Button>
            </>
          );
        }}
      </form.Subscribe>
    </form>
  );
}
