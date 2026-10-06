import { useEffect, useId, useMemo, useRef, useState } from "react";
import { useForm } from "@tanstack/react-form";
import { z } from "zod";
import { useMsg } from "@/hooks/message";
import { ownershipReasonSchema } from "@/core/native-archive/account-review-api";
import {
  createMetadataReviewAPI,
  type NativeIdentity,
} from "@/core/native-archive/metadata-review-api";
import { NativeArchiveError } from "@/core/native-archive/client";
import type {
  GalleryAssociationAPI,
  GalleryAssociationPreview,
  AttachmentMediaAPI,
  AttachmentMediaPreview,
  AttachmentMediaContext,
} from "@/core/native-archive/association-review-api";
import type { PolicySampleChoice } from "../metadata-policy/sample-picker";
import { AssociationPicker } from "./association-picker";
import { AssociationChoice } from "./association-details";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Field,
  FieldGroup,
  FieldLabel,
  FieldDescription,
  FieldError,
} from "@/components/ui/field";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { Spinner } from "@/components/ui/spinner";
import { ReviewError } from "@/components/detail/native-metadata/shared";

type Props = (
  | {
      family: "gallery";
      api: Pick<GalleryAssociationAPI, "endpoint" | "preview">;
      current: Awaited<ReturnType<GalleryAssociationAPI["context"]>>;
      onApply: (preview: GalleryAssociationPreview) => Promise<void>;
    }
  | {
      family: "attachment";
      api: Pick<AttachmentMediaAPI, "endpoint" | "preview">;
      current: AttachmentMediaContext;
      onApply: (preview: AttachmentMediaPreview) => Promise<void>;
    }
) & { disabled: boolean };
const formSchema = z
  .object({
    state: z.enum(["linked", "disabled", "unlinked", "undecided"]),
    kind: z.enum(["gallery", "scene", "image"]),
    target: z
      .object({ id: z.string().regex(/^[1-9]\d*$/), label: z.string() })
      .nullable(),
    reason: ownershipReasonSchema,
  })
  .refine((value) => value.state !== "linked" || value.target !== null);
type FormValues = z.infer<typeof formSchema>;
type Preview =
  | { family: "gallery"; value: GalleryAssociationPreview }
  | { family: "attachment"; value: AttachmentMediaPreview };

export function AssociationForm(props: Props) {
  const msg = useMsg(),
    id = useId();
  const identityAPI = useMemo(
    () => createMetadataReviewAPI(props.api.endpoint),
    [props.api.endpoint],
  );
  const [preview, setPreview] = useState<Preview>();
  const [busy, setBusy] = useState(false),
    [error, setError] = useState<unknown>();
  const pending = useRef<AbortController | null>(null);
  useEffect(() => () => pending.current?.abort(), []);
  const currentItem =
    props.family === "gallery"
      ? (props.current.album?.gallery ?? null)
      : props.current.media;
  const currentState =
    props.family === "gallery"
      ? props.current.album?.state
      : props.current.current?.state;
  const defaults: FormValues = {
    state: currentState ?? "linked",
    kind:
      props.family === "gallery"
        ? "gallery"
        : currentItem?.kind === "image"
          ? "image"
          : "scene",
    target: currentItem?.local_id
      ? {
          id: String(currentItem.local_id),
          label: `${currentItem.title || msg("metadata_policy.untitled", "Untitled")} (#${currentItem.local_id})`,
        }
      : null,
    reason:
      props.family === "gallery"
        ? (props.current.album?.reason ?? "")
        : (props.current.current?.reason ?? ""),
  };
  const postActive =
    props.family === "gallery"
      ? props.current.post.state === "active"
      : props.current.post_state === "active";
  const blocked = props.disabled || busy || !postActive;
  const form = useForm({
    defaultValues: defaults,
    validators: { onChange: formSchema },
    onSubmit: async ({ value }) => {
      if (blocked || pending.current) return;
      const controller = new AbortController();
      pending.current = controller;
      setBusy(true);
      setError(undefined);
      setPreview(undefined);
      try {
        let target: NativeIdentity | undefined;
        if (value.state === "linked") {
          if (!value.target) return;
          target = await identityAPI.identity(
            value.kind,
            value.target.id,
            controller.signal,
          );
          if (
            target.kind !== value.kind ||
            String(target.local_id) !== value.target.id
          )
            throw new NativeArchiveError(0, "invalid_response");
        }
        if (props.family === "gallery") {
          if (value.state !== "linked" && value.state !== "disabled")
            throw new NativeArchiveError(0, "invalid_choice");
          const result = await props.api.preview(
            {
              post_uuid: props.current.post.uuid,
              post_revision: props.current.post.revision,
              state: value.state,
              reason: value.reason,
              ...(target
                ? {
                    gallery_uuid: target.uuid,
                    gallery_revision: target.revision,
                  }
                : {}),
            },
            controller.signal,
          );
          if (!controller.signal.aborted)
            setPreview({ family: "gallery", value: result });
        } else {
          if (value.state === "disabled")
            throw new NativeArchiveError(0, "invalid_choice");
          const result = await props.api.preview(
            {
              post_uuid: props.current.post_uuid,
              post_revision: props.current.post_revision,
              attachment_uuid: props.current.attachment.uuid,
              attachment_revision: props.current.attachment.revision,
              state: value.state,
              reason: value.reason,
              ...(target
                ? { media_uuid: target.uuid, media_revision: target.revision }
                : {}),
            },
            controller.signal,
          );
          if (!controller.signal.aborted)
            setPreview({ family: "attachment", value: result });
        }
      } catch (error) {
        if (!controller.signal.aborted) setError(error);
      } finally {
        if (!controller.signal.aborted) setBusy(false);
        if (pending.current === controller) pending.current = null;
      }
    },
  });
  const stateOptions =
    props.family === "gallery"
      ? [
          {
            value: "linked" as const,
            label: msg("association_review.link_gallery", "Link gallery"),
          },
          {
            value: "disabled" as const,
            label: msg("association_review.disable", "Disable"),
          },
        ]
      : [
          {
            value: "linked" as const,
            label: msg("association_review.link_media", "Link media"),
          },
          {
            value: "unlinked" as const,
            label: msg("association_review.reject", "Reject"),
          },
          {
            value: "undecided" as const,
            label: msg("association_review.automatic", "Automatic"),
          },
        ];
  const kindOptions = [
    { value: "scene" as const, label: msg("source_albums.video", "Video") },
    { value: "image" as const, label: msg("source_albums.image", "Image") },
  ];
  const currentPreviewState =
    preview?.family === "gallery"
      ? preview.value.current?.state
      : preview?.value.current.current?.state;
  const currentPreviewItem =
    preview?.family === "gallery"
      ? (preview.value.current?.gallery ?? null)
      : (preview?.value.current.media ?? null);
  async function apply() {
    if (blocked || !preview?.value.changed) return;
    if (props.family === "gallery" && preview.family === "gallery")
      await props.onApply(preview.value);
    if (props.family === "attachment" && preview.family === "attachment")
      await props.onApply(preview.value);
  }
  return (
    <form
      className="flex flex-col gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        event.stopPropagation();
        void form.handleSubmit();
      }}
    >
      {!postActive && (
        <p>
          {msg(
            "association_review.forgotten",
            "This source post was forgotten. Its history remains available, but new links cannot be saved.",
          )}
        </p>
      )}
      {!preview && (
        <AssociationChoice
          title={msg("association_review.current", "Current choice")}
          state={currentState}
          item={currentItem}
        />
      )}
      <FieldGroup>
        <form.Field name="state">
          {(field) => (
            <Field data-disabled={blocked}>
              <FieldLabel id={`${id}-state`}>
                {msg("association_review.behavior", "Association behavior")}
              </FieldLabel>
              <ToggleGroup
                aria-labelledby={`${id}-state`}
                variant="outline"
                value={[field.state.value]}
                disabled={blocked}
                className="flex-wrap"
                onValueChange={(values) => {
                  if (values[0]) {
                    field.handleChange(values[0]);
                    setPreview(undefined);
                  }
                }}
              >
                {stateOptions.map((option) => (
                  <ToggleGroupItem key={option.value} value={option.value}>
                    {option.label}
                  </ToggleGroupItem>
                ))}
              </ToggleGroup>
              <FieldDescription>
                {field.state.value === "disabled"
                  ? msg(
                      "association_review.disable_help",
                      "Stop automatic association with a gallery. Existing galleries and their members stay available.",
                    )
                  : field.state.value === "unlinked"
                    ? msg(
                        "association_review.reject_help",
                        "Keep this source attachment unlinked until you explicitly choose otherwise.",
                      )
                    : field.state.value === "undecided"
                      ? msg(
                          "association_review.automatic_help",
                          "Clear the selected media. Later ingestion may link a uniquely verified file; historical matching keeps this review choice.",
                        )
                      : props.family === "gallery"
                        ? msg(
                            "association_review.gallery_help",
                            "Choose an existing manual or source gallery. Folder and ZIP galleries cannot be adopted. Preview checks other posts' claims, including merged galleries.",
                          )
                        : msg(
                            "association_review.media_help",
                            "Choose the library image or video for this source attachment. Converted animations can be videos even if the source listed an image.",
                          )}
              </FieldDescription>
            </Field>
          )}
        </form.Field>
        <form.Subscribe selector={(state) => state.values.state}>
          {(state) =>
            state === "linked" && (
              <>
                {props.family === "attachment" && (
                  <form.Field name="kind">
                    {(field) => (
                      <Field data-disabled={blocked}>
                        <FieldLabel id={`${id}-kind`}>
                          {msg(
                            "association_review.media_kind",
                            "Library media type",
                          )}
                        </FieldLabel>
                        <ToggleGroup
                          aria-labelledby={`${id}-kind`}
                          variant="outline"
                          value={[field.state.value]}
                          disabled={blocked}
                          onValueChange={(values) => {
                            if (values[0]) {
                              field.handleChange(values[0]);
                              form.setFieldValue("target", null);
                              setPreview(undefined);
                            }
                          }}
                        >
                          {kindOptions.map((option) => (
                            <ToggleGroupItem
                              key={option.value}
                              value={option.value}
                            >
                              {option.label}
                            </ToggleGroupItem>
                          ))}
                        </ToggleGroup>
                      </Field>
                    )}
                  </form.Field>
                )}
                <form.Subscribe selector={(state) => state.values.kind}>
                  {(kind) => (
                    <form.Field name="target">
                      {(field) => (
                        <Field data-disabled={blocked}>
                          <FieldLabel htmlFor={`${id}-target`}>
                            {props.family === "gallery"
                              ? msg(
                                  "association_review.gallery_target",
                                  "Existing gallery",
                                )
                              : msg(
                                  "association_review.media_target",
                                  "Existing media",
                                )}
                          </FieldLabel>
                          <AssociationPicker
                            key={kind}
                            kind={kind}
                            id={`${id}-target`}
                            disabled={blocked}
                            value={field.state.value}
                            onChange={(choice: PolicySampleChoice | null) => {
                              field.handleChange(choice);
                              setPreview(undefined);
                            }}
                          />
                        </Field>
                      )}
                    </form.Field>
                  )}
                </form.Subscribe>
              </>
            )
          }
        </form.Subscribe>
        <form.Field name="reason">
          {(field) => (
            <Field
              data-disabled={blocked}
              data-invalid={field.state.meta.errors.length > 0}
            >
              <FieldLabel htmlFor={`${id}-reason`}>
                {msg("association_review.reason", "Reason (optional)")}
              </FieldLabel>
              <Input
                id={`${id}-reason`}
                value={field.state.value}
                disabled={blocked}
                aria-invalid={field.state.meta.errors.length > 0}
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
      </FieldGroup>
      {error !== undefined && <ReviewError error={error} />}
      <form.Subscribe
        selector={(state) => ({
          valid: state.canSubmit,
          state: state.values.state,
          target: state.values.target,
        })}
      >
        {({ valid, state, target }) => (
          <Button
            type="submit"
            variant="outline"
            disabled={blocked || !valid || (state === "linked" && !target)}
          >
            {busy && <Spinner data-icon="inline-start" />}
            {msg("association_review.preview", "Preview association")}
          </Button>
        )}
      </form.Subscribe>
      {preview && (
        <div className="flex flex-col gap-4">
          <div className="grid gap-3 md:grid-cols-2">
            <AssociationChoice
              title={msg("association_review.current", "Current choice")}
              state={currentPreviewState}
              item={currentPreviewItem}
            />
            <AssociationChoice
              title={msg("association_review.proposed", "Proposed choice")}
              state={preview.value.input.state}
              item={preview.value.proposed}
            />
          </div>
          <p className="text-sm text-muted-foreground">
            {msg(
              "association_review.separate_sync",
              "Saving this link leaves gallery members, metadata and files intact. Use Match existing media to review gallery synchronization separately.",
            )}
          </p>
          {preview.value.changed ? (
            <Button
              type="button"
              disabled={blocked}
              onClick={() => void apply()}
            >
              {msg("association_review.apply", "Save association choice")}
            </Button>
          ) : (
            <p>
              {msg(
                "association_review.unchanged",
                "This association choice is already saved.",
              )}
            </p>
          )}
        </div>
      )}
    </form>
  );
}
