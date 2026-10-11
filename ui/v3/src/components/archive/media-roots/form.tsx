import { useId, useState } from "react";
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
} from "@/components/ui/field";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import {
  Collapsible,
  CollapsibleTrigger,
  CollapsibleContent,
} from "@/components/ui/collapsible";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { DirectorySelectionDialog } from "@/components/shared/directory-selection-dialog";
import { useCollectionLabels } from "../collections/shared";
import { ReviewError } from "@/components/detail/native-metadata/shared";

const formSchema = mediaRootInputSchema.extend({
  serverPath: z.string().max(4096),
  offline: z.boolean(),
  scan: z.object({
    revision: z.number().int().nonnegative(),
    enabled: z.boolean(),
    exclude_video: z.boolean(),
    exclude_image: z.boolean(),
  }),
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
            "Check the server path and mount, or mark the folder offline under Advanced folder options.",
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
  const [browse, setBrowse] = useState(false);
  const [error, setError] = useState<unknown>();
  const blocked = disabled || busy;
  const form = useForm({
    defaultValues: {
      ...input,
      scan: input.scan ?? {
        revision: 0,
        enabled: true,
        exclude_video: false,
        exclude_image: false,
      },
      serverPath: input.binding?.path ?? input.unbound_path ?? "",
      offline: !input.binding && !!input.unbound_path,
    },
    validators: { onChange: formSchema },
    onSubmit: async ({ value }) => {
      setBusy(true);
      setError(undefined);
      try {
        const { serverPath, offline, ...request } = value;
        let binding = input.binding;
        if (!serverPath || offline) binding = null;
        else if (
          !binding ||
          binding.path !== serverPath ||
          (input.state !== "active" && value.state === "active")
        ) {
          binding = await api.probe(serverPath);
        }
        await onSave({
          ...request,
          binding,
          unbound_path: binding ? "" : serverPath,
        });
      } catch (error) {
        setError(error);
      } finally {
        setBusy(false);
      }
    },
  });
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
              <FieldLabel htmlFor={`${id}-name`}>
                {msg("media_roots.name", "Folder name")}
              </FieldLabel>
              <Input
                id={`${id}-name`}
                value={field.state.value}
                disabled={blocked}
                maxLength={1024}
                aria-invalid={!field.state.meta.isValid}
                onBlur={field.handleBlur}
                onChange={(event) => field.handleChange(event.target.value)}
              />
            </Field>
          )}
        </form.Field>
        <form.Field name="serverPath">
          {(field) => (
            <Field
              data-disabled={blocked}
              data-invalid={!field.state.meta.isValid}
            >
              <FieldLabel htmlFor={`${id}-path`}>
                {msg("media_roots.path", "Folder path on the Stash server")}
              </FieldLabel>
              <Input
                id={`${id}-path`}
                value={field.state.value}
                disabled={blocked}
                maxLength={4096}
                aria-invalid={!field.state.meta.isValid}
                onBlur={field.handleBlur}
                onChange={(event) => field.handleChange(event.target.value)}
              />
              <FieldDescription>
                {msg(
                  "library_folders.path_help",
                  "Use the path visible to Stash, such as /media/porn inside its container. Saving does not move files or start a scan.",
                )}
              </FieldDescription>
              <Button
                className="w-fit"
                type="button"
                variant="outline"
                disabled={blocked}
                onClick={() => setBrowse(true)}
              >
                {msg("actions.browse", "Browse")}
              </Button>
            </Field>
          )}
        </form.Field>
        <form.Field name="scan.enabled">
          {(field) => (
            <Field orientation="horizontal" data-disabled={blocked}>
              <Switch
                id={`${id}-scan`}
                checked={field.state.value}
                disabled={blocked}
                onCheckedChange={field.handleChange}
              />
              <FieldContent>
                <FieldLabel htmlFor={`${id}-scan`}>
                  {msg("library_folders.scan", "Include in library scans")}
                </FieldLabel>
                <FieldDescription>
                  {msg(
                    "library_folders.scan_help",
                    "Discover files in this folder during scans. Turning this off preserves existing media and allows downloads to continue.",
                  )}
                </FieldDescription>
              </FieldContent>
            </Field>
          )}
        </form.Field>
        <form.Field name="scan.exclude_video">
          {(field) => (
            <Field orientation="horizontal" data-disabled={blocked}>
              <Switch
                id={`${id}-video`}
                checked={!field.state.value}
                disabled={blocked}
                onCheckedChange={(value) => field.handleChange(!value)}
              />
              <FieldLabel htmlFor={`${id}-video`}>
                {msg("library_folders.videos", "Scan videos")}
              </FieldLabel>
            </Field>
          )}
        </form.Field>
        <form.Field name="scan.exclude_image">
          {(field) => (
            <Field orientation="horizontal" data-disabled={blocked}>
              <Switch
                id={`${id}-images`}
                checked={!field.state.value}
                disabled={blocked}
                onCheckedChange={(value) => field.handleChange(!value)}
              />
              <FieldLabel htmlFor={`${id}-images`}>
                {msg("library_folders.images", "Scan images")}
              </FieldLabel>
            </Field>
          )}
        </form.Field>
      </FieldGroup>
      <Collapsible>
        <CollapsibleTrigger render={<Button type="button" variant="ghost" />}>
          {msg("library_folders.advanced", "Advanced folder options")}
        </CollapsibleTrigger>
        <CollapsibleContent className="pt-4">
          <FieldGroup>
            <form.Field name="offline">
              {(field) => (
                <Field orientation="horizontal" data-disabled={blocked}>
                  <Switch
                    id={`${id}-offline`}
                    checked={field.state.value}
                    disabled={blocked}
                    onCheckedChange={field.handleChange}
                  />
                  <FieldContent>
                    <FieldLabel htmlFor={`${id}-offline`}>
                      {msg("library_folders.offline", "Folder is offline")}
                    </FieldLabel>
                    <FieldDescription>
                      {msg(
                        "library_folders.offline_help",
                        "Keep the path and associations without verifying this mount. Turn this off when it is available again. Downloads cannot use an unverified folder.",
                      )}
                    </FieldDescription>
                  </FieldContent>
                </Field>
              )}
            </form.Field>
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
                      "library_folders.state_help",
                      "Active folders can scan and receive downloads. Disabled pauses both. Retired marks a folder no longer in use; it can be reactivated. These settings do not delete media or associations.",
                    )}
                  </FieldDescription>
                </Field>
              )}
            </form.Field>
          </FieldGroup>
        </CollapsibleContent>
      </Collapsible>
      {error !== undefined && <MediaRootError error={error} />}
      <form.Subscribe selector={(state) => state.canSubmit}>
        {(canSubmit) => (
          <Button
            className="w-fit"
            type="submit"
            disabled={blocked || !canSubmit}
          >
            {busy && <Spinner data-icon="inline-start" />}
            {msg("media_roots.save", "Save folder")}
          </Button>
        )}
      </form.Subscribe>
      {browse && (
        <DirectorySelectionDialog
          open={browse}
          onOpenChange={setBrowse}
          title={msg("actions.add_directory", "Add directory")}
          onConfirm={(paths) => {
            setBrowse(false);
            if (paths[0]) {
              form.setFieldValue("serverPath", paths[0]);
              if (!form.getFieldValue("label"))
                form.setFieldValue(
                  "label",
                  paths[0].split(/[\\/]/u).filter(Boolean).at(-1) ?? paths[0],
                );
            }
          }}
        />
      )}
    </form>
  );
}
