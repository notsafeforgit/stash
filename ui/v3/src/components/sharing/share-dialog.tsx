import { useId, useState } from "react";
import { useMutation } from "@apollo/client/react";
import { useForm } from "@tanstack/react-form";
import { z } from "zod";
import { startOfDay } from "date-fns";
import * as GQL from "@/core/generated-graphql";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { DateInput } from "@/components/ui/date-input";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
} from "@/components/ui/field";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { Spinner } from "@/components/ui/spinner";
import { useMsg } from "@/hooks/message";
import { useToast } from "@/hooks/toast";
import {
  shareSelectionSchema,
  ShareTargetPicker,
  type ShareSelection,
} from "./share-target-picker";
import { ShareLinkDialog } from "./share-link-dialog";

export function localDateTime(date: Date) {
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60_000);
  return local.toISOString().slice(0, 16);
}

type ShareDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  targets?: ShareSelection[];
  share?: GQL.MediaShareFieldsFragment;
};

export function ShareDialog(props: ShareDialogProps) {
  const [link, setLink] = useState<{ id: string; url: string }>();
  return (
    <>
      {props.open && (
        <ShareForm
          {...props}
          onCreated={(result) => {
            setLink(result);
            props.onOpenChange(false);
          }}
        />
      )}
      {link && (
        <ShareLinkDialog link={link} onClose={() => setLink(undefined)} />
      )}
    </>
  );
}

function ShareForm({
  open,
  onOpenChange,
  targets = [],
  share,
  onCreated,
}: ShareDialogProps & {
  onCreated: (link: { id: string; url: string }) => void;
}) {
  const msg = useMsg();
  const toast = useToast();
  const id = useId();
  const [now] = useState(Date.now);
  const [preset, setPreset] = useState(share ? "custom" : "24");
  const [create] = useMutation(GQL.MediaShareCreateDocument, {
    refetchQueries: [GQL.MediaSharesDocument],
  });
  const [update] = useMutation(GQL.MediaShareUpdateDocument, {
    refetchQueries: [GQL.MediaSharesDocument],
  });
  const form = useForm({
    defaultValues: {
      label: share?.label ?? msg("sharing.default_label", "Shared media"),
      expiry: localDateTime(
        share ? new Date(share.expires_at) : new Date(now + 86_400_000),
      ),
      allowDownload: share?.allow_download ?? false,
      showMetadata: share?.show_metadata ?? false,
      targets:
        share?.items.map((item) => ({
          kind: item.kind,
          id: item.id,
          name: item.title || item.id,
        })) ?? targets,
    },
    validators: {
      onChange: z.object({
        label: z
          .string()
          .trim()
          .min(1, msg("sharing.label_required", "Enter a label."))
          .max(200, msg("sharing.label_long", "Use at most 200 characters.")),
        expiry: z.string().refine(
          (value) => {
            if (!z.iso.datetime({ local: true }).safeParse(value).success)
              return false;
            const time = new Date(value).getTime();
            return (
              Number.isFinite(time) &&
              time > now &&
              time <= now + 30 * 86_400_000
            );
          },
          msg(
            "sharing.expiry_invalid",
            "Choose a future expiry within 30 days.",
          ),
        ),
        allowDownload: z.boolean(),
        showMetadata: z.boolean(),
        targets: z
          .array(shareSelectionSchema)
          .min(1, msg("sharing.select_required", "Select at least one item."))
          .max(2000),
      }),
    },
    onSubmit: async ({ value }) => {
      try {
        const input = {
          label: value.label.trim(),
          expires_at: new Date(value.expiry).toISOString(),
          allow_download: value.allowDownload,
          show_metadata: value.showMetadata,
        };
        if (share) {
          await update({ variables: { input: { ...input, id: share.id } } });
          toast.success(msg("sharing.saved", "Share updated"));
          onOpenChange(false);
        } else {
          const result = await create({
            variables: {
              input: {
                ...input,
                targets: value.targets.map(({ kind, id: targetId }) => ({
                  kind,
                  id: targetId,
                })),
              },
            },
          });
          if (result.data)
            onCreated({
              id: result.data.mediaShareCreate.share.id,
              url: result.data.mediaShareCreate.url,
            });
        }
      } catch (error) {
        toast.error(error);
      }
    },
  });
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        showCloseButton={false}
        className="max-h-[calc(100dvh-2rem)] grid-cols-1 grid-rows-[auto_minmax(0,1fr)_auto] sm:max-w-xl"
      >
        <DialogHeader>
          <DialogTitle>
            {share
              ? msg("sharing.edit", "Edit share")
              : msg("sharing.create", "Create share")}
          </DialogTitle>
        </DialogHeader>
        <form
          id={`${id}-form`}
          className="-m-1 min-h-0 min-w-0 overflow-x-hidden overflow-y-auto overscroll-contain p-1"
          onSubmit={(event) => {
            event.preventDefault();
            event.stopPropagation();
            void form.handleSubmit();
          }}
        >
          <FieldGroup>
            <DialogDescription>
              {msg(
                "sharing.fixed_scope",
                "Only selected scenes, images and the current images in each gallery are included. Future additions and related entities are never added automatically. Up to 2,000 media items per share.",
              )}
            </DialogDescription>
            <form.Field name="label">
              {(field) => (
                <Field data-invalid={field.state.meta.errors.length > 0}>
                  <FieldLabel htmlFor={`${id}-label`}>
                    {msg("sharing.label", "Label")}
                  </FieldLabel>
                  <Input
                    id={`${id}-label`}
                    value={field.state.value}
                    maxLength={200}
                    onChange={(event) => field.handleChange(event.target.value)}
                    onBlur={field.handleBlur}
                  />
                  <FieldDescription>
                    {msg("sharing.label_visible", "Recipients see this label.")}
                  </FieldDescription>
                  <FieldError errors={field.state.meta.errors} />
                </Field>
              )}
            </form.Field>
            <form.Field name="targets">
              {(field) => (
                <Field data-invalid={field.state.meta.errors.length > 0}>
                  <FieldLabel>
                    {msg("sharing.selection", "Included items")}
                  </FieldLabel>
                  {share ? (
                    <ul className="max-h-32 min-w-0 overflow-y-auto text-sm text-muted-foreground [overflow-wrap:anywhere]">
                      {field.state.value.map((item) => (
                        <li key={`${item.kind}:${item.id}`}>{item.name}</li>
                      ))}
                    </ul>
                  ) : (
                    <ShareTargetPicker
                      value={field.state.value}
                      onChange={field.handleChange}
                    />
                  )}
                  <FieldError errors={field.state.meta.errors} />
                </Field>
              )}
            </form.Field>
            <form.Field name="expiry">
              {(field) => (
                <FieldSet
                  className="grid min-w-0"
                  data-invalid={field.state.meta.errors.length > 0}
                >
                  <FieldLegend variant="label">
                    {msg("sharing.expires", "Expires")}
                  </FieldLegend>
                  <ToggleGroup
                    value={[preset]}
                    onValueChange={(values) => {
                      const next = values[0];
                      if (!next) return;
                      setPreset(next);
                      if (next !== "custom")
                        field.handleChange(
                          localDateTime(
                            new Date(Date.now() + Number(next) * 3_600_000),
                          ),
                        );
                    }}
                    variant="outline"
                    className="flex-wrap"
                  >
                    <ToggleGroupItem value="1">
                      {msg("sharing.one_hour", "1 hour")}
                    </ToggleGroupItem>
                    <ToggleGroupItem value="24">
                      {msg("sharing.one_day", "24 hours")}
                    </ToggleGroupItem>
                    <ToggleGroupItem value="168">
                      {msg("sharing.one_week", "7 days")}
                    </ToggleGroupItem>
                    <ToggleGroupItem value="custom">
                      {msg("sharing.custom", "Custom")}
                    </ToggleGroupItem>
                  </ToggleGroup>
                  <DateInput
                    isTime
                    captionLayout="label"
                    value={field.state.value}
                    invalid={field.state.meta.errors.length > 0}
                    disabledDays={(date) =>
                      date < startOfDay(now) ||
                      date > startOfDay(now + 30 * 86_400_000)
                    }
                    onValueChange={(value) => {
                      setPreset("custom");
                      field.handleChange(value.replace(" ", "T"));
                    }}
                  />
                  <FieldError errors={field.state.meta.errors} />
                </FieldSet>
              )}
            </form.Field>
            <form.Field name="showMetadata">
              {(field) => (
                <Field orientation="horizontal">
                  <Checkbox
                    id={`${id}-metadata`}
                    checked={field.state.value}
                    onCheckedChange={field.handleChange}
                  />
                  <FieldContent>
                    <FieldLabel htmlFor={`${id}-metadata`}>
                      {msg("sharing.show_titles", "Show titles")}
                    </FieldLabel>
                    <FieldDescription>
                      {msg(
                        "sharing.metadata_description",
                        "Add saved titles to the recipient view. Without them, media uses generic labels. The share label, expiry and basic playback information remain visible; other library metadata stays private.",
                      )}
                    </FieldDescription>
                  </FieldContent>
                </Field>
              )}
            </form.Field>
            <form.Field name="allowDownload">
              {(field) => (
                <Field orientation="horizontal">
                  <Checkbox
                    id={`${id}-downloads`}
                    checked={field.state.value}
                    onCheckedChange={field.handleChange}
                  />
                  <FieldContent>
                    <FieldLabel htmlFor={`${id}-downloads`}>
                      {msg(
                        "sharing.allow_originals",
                        "Allow original downloads",
                      )}
                    </FieldLabel>
                    <FieldDescription>
                      {msg(
                        "sharing.download_description",
                        "Permit downloading the original files, which may contain embedded metadata. When off, originals are blocked, but recipients can still save or record the media shown to them.",
                      )}
                    </FieldDescription>
                  </FieldContent>
                </Field>
              )}
            </form.Field>
          </FieldGroup>
        </form>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
          >
            {msg("actions.cancel", "Cancel")}
          </Button>
          <form.Subscribe
            selector={(state) => [state.canSubmit, state.isSubmitting]}
          >
            {([canSubmit, submitting]) => (
              <Button
                type="submit"
                form={`${id}-form`}
                disabled={!canSubmit || submitting}
              >
                {submitting && <Spinner data-icon="inline-start" />}
                {share
                  ? msg("actions.save", "Save")
                  : msg("sharing.create", "Create share")}
              </Button>
            )}
          </form.Subscribe>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
