import { useId, useState } from "react";
import { useMutation } from "@apollo/client/react";
import { useForm } from "@tanstack/react-form";
import { z } from "zod";
import { useIntl } from "react-intl";
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
  allShareMediaTypes,
  matchingShareSource,
  type ShareSelection,
  type MatchingShareSelection,
  type ShareEntitySource,
} from "./share-source";
import { ShareLinkDialog } from "./share-link-dialog";

export function localDateTime(date: Date) {
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60_000);
  return local.toISOString().slice(0, 16);
}

type ShareDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  targets?: ShareSelection[];
  matching?: MatchingShareSelection;
  entity?: ShareEntitySource;
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
  matching,
  entity,
  share,
  onCreated,
}: ShareDialogProps & {
  onCreated: (link: { id: string; url: string }) => void;
}) {
  const msg = useMsg();
  const intl = useIntl();
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
      label:
        share?.label ??
        entity?.name ??
        (targets.length === 1 ? targets[0]?.name : undefined) ??
        msg("sharing.default_label", "Shared media"),
      expiry: localDateTime(
        share ? new Date(share.expires_at) : new Date(now + 86_400_000),
      ),
      allowDownload: share?.allow_download ?? false,
      showMetadata: share?.show_metadata ?? false,
      scope: targets.length ? "selection" : entity ? "entity" : "filter",
      mode: GQL.ShareMembership.Snapshot,
      mediaTypes: allShareMediaTypes,
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
        scope: z.string(),
        mode: z.enum(GQL.ShareMembership),
        mediaTypes: z
          .array(z.enum(GQL.ShareEntityKind))
          .min(
            1,
            msg(
              "sharing.media_types_required",
              "Choose at least one media type.",
            ),
          ),
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
                targets:
                  value.scope === "selection"
                    ? targets.map(({ kind, id: targetId }) => ({
                        kind,
                        id: targetId,
                      }))
                    : undefined,
                source:
                  value.scope === "filter" && matching
                    ? matchingShareSource(matching, value.mode)
                    : value.scope === "entity" && entity
                      ? {
                          mode: value.mode,
                          entity: { kind: entity.kind, id: entity.id },
                          media_types: value.mediaTypes,
                        }
                      : undefined,
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
                "sharing.scope_description",
                "Choose what this link shares. Deleted media disappears from every type of share.",
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
            {share ? (
              <Field>
                <FieldLabel>
                  {msg("sharing.selection", "Included items")}
                </FieldLabel>
                <FieldDescription>
                  {share.source_name ||
                    (share.source_kind === GQL.ShareSourceKind.Filter
                      ? msg("sharing.filtered_list", "Filtered list")
                      : intl.formatMessage(
                          {
                            id: "sharing.selected_count",
                            defaultMessage:
                              "{count, plural, one {# selected item} other {# selected items}}",
                          },
                          { count: share.selection_count },
                        ))}
                  {" · "}
                  {share.mode === GQL.ShareMembership.Ongoing
                    ? msg("sharing.ongoing", "Ongoing")
                    : msg("sharing.snapshot", "Snapshot")}
                </FieldDescription>
                <FieldDescription>
                  {msg(
                    "sharing.scope_immutable",
                    "To change what is shared, create a new link.",
                  )}
                </FieldDescription>
              </Field>
            ) : (
              <>
                <form.Field name="scope">
                  {(field) => (
                    <FieldSet>
                      <FieldLegend variant="label">
                        {msg("sharing.selection", "Included items")}
                      </FieldLegend>
                      {matching && targets.length > 0 && (
                        <ToggleGroup
                          variant="outline"
                          value={[field.state.value]}
                          onValueChange={([value]) => {
                            if (value) field.handleChange(value);
                          }}
                        >
                          <ToggleGroupItem value="selection">
                            {msg("sharing.selected_items", "Selected items")}
                          </ToggleGroupItem>
                          <ToggleGroupItem value="filter">
                            {msg("sharing.filtered_list", "Filtered list")}
                          </ToggleGroupItem>
                        </ToggleGroup>
                      )}
                      <FieldDescription>
                        {field.state.value === "selection"
                          ? intl.formatMessage(
                              {
                                id: "sharing.selected_count",
                                defaultMessage:
                                  "{count, plural, one {# selected item} other {# selected items}}",
                              },
                              { count: targets.length },
                            )
                          : (entity?.name ??
                            msg(
                              "sharing.filtered_description",
                              "All items matching this list’s current search and filters, across every page.",
                            ))}
                      </FieldDescription>
                      {field.state.value === "selection" && (
                        <FieldDescription className="line-clamp-2 [overflow-wrap:anywhere]">
                          {targets
                            .slice(0, 3)
                            .map((item) => item.name)
                            .join(" · ")}
                        </FieldDescription>
                      )}
                    </FieldSet>
                  )}
                </form.Field>
                {entity && (
                  <form.Field name="mediaTypes">
                    {(field) => (
                      <FieldSet
                        data-invalid={field.state.meta.errors.length > 0}
                      >
                        <FieldLegend variant="label">
                          {msg("sharing.media_types", "Media types")}
                        </FieldLegend>
                        <ToggleGroup
                          multiple
                          variant="outline"
                          value={field.state.value}
                          onValueChange={field.handleChange}
                          aria-invalid={field.state.meta.errors.length > 0}
                        >
                          <ToggleGroupItem value={GQL.ShareEntityKind.Scene}>
                            {msg("scenes", "Scenes")}
                          </ToggleGroupItem>
                          <ToggleGroupItem value={GQL.ShareEntityKind.Image}>
                            {msg("images", "Images")}
                          </ToggleGroupItem>
                          <ToggleGroupItem value={GQL.ShareEntityKind.Gallery}>
                            {msg("galleries", "Galleries")}
                          </ToggleGroupItem>
                        </ToggleGroup>
                        <FieldError errors={field.state.meta.errors} />
                      </FieldSet>
                    )}
                  </form.Field>
                )}
                <form.Subscribe selector={(state) => state.values.scope}>
                  {(scope) =>
                    scope !== "selection" ? (
                      <form.Field name="mode">
                        {(field) => (
                          <FieldSet>
                            <FieldLegend variant="label">
                              {msg("sharing.membership", "Membership")}
                            </FieldLegend>
                            <ToggleGroup
                              variant="outline"
                              value={[field.state.value]}
                              onValueChange={([value]) => {
                                if (value) field.handleChange(value);
                              }}
                            >
                              <ToggleGroupItem
                                value={GQL.ShareMembership.Snapshot}
                              >
                                {msg("sharing.snapshot", "Snapshot")}
                              </ToggleGroupItem>
                              <ToggleGroupItem
                                value={GQL.ShareMembership.Ongoing}
                              >
                                {msg("sharing.ongoing", "Ongoing")}
                              </ToggleGroupItem>
                            </ToggleGroup>
                            <FieldDescription>
                              {field.state.value ===
                              GQL.ShareMembership.Snapshot
                                ? msg(
                                    "sharing.snapshot_description",
                                    "Include what matches when you create the link. Later additions are not included. Snapshots support up to 2,000 media items.",
                                  )
                                : msg(
                                    "sharing.ongoing_description",
                                    "Keep these rules and include current and future matches. Items disappear when they stop matching. Changing this list’s filters later does not change the share.",
                                  )}
                            </FieldDescription>
                          </FieldSet>
                        )}
                      </form.Field>
                    ) : (
                      <FieldDescription>
                        {msg(
                          "sharing.selection_description",
                          "Keep this selection and the current contents of selected galleries. Later additions are not included. Snapshots support up to 2,000 media items.",
                        )}
                      </FieldDescription>
                    )
                  }
                </form.Subscribe>
              </>
            )}
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
                      {msg("sharing.allow_originals", "Show download button")}
                    </FieldLabel>
                    <FieldDescription>
                      {msg(
                        "sharing.download_description",
                        "Show a button to download the original file. Playback may use a compatible video stream, but downloads keep the original format. Recipients can still save media without this button, including original files served for viewing.",
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
