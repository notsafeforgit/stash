import { useId, useState } from "react";
import { useForm } from "@tanstack/react-form";
import { useMsg } from "@/hooks/message";
import {
  postMergeInputSchema,
  type PostMergeInput,
  type PostMergePreview,
} from "@/core/native-archive/post-consolidation-schema";
import type { PostConsolidationAPI } from "@/core/native-archive/post-consolidation-api";
import type {
  PostSummary,
  SourcePostAPI,
} from "@/core/native-archive/source-post-api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import {
  Field,
  FieldGroup,
  FieldLabel,
  FieldDescription,
  FieldError,
} from "@/components/ui/field";
import { PostSection } from "../shared";
import { MergePostPicker } from "./picker";
import {
  MergeSelectionChoice,
  MergeGalleryChoice,
  MergeMediaChoices,
  MergeAttachmentChoices,
} from "./choices";
import { MergePlan, MergeOriginals } from "./summary";
import { MergeError, useMergeLabels } from "./shared";

function MergeForm({
  api,
  source,
  target,
  onApply,
  onBack,
}: {
  api: PostConsolidationAPI;
  source: string;
  target: PostSummary;
  onApply: (input: PostMergeInput, preview: PostMergePreview) => Promise<void>;
  onBack: () => void;
}) {
  const msg = useMsg();
  const labels = useMergeLabels();
  const id = useId();
  const [comparison, setComparison] = useState<PostMergePreview>();
  const [reviewed, setReviewed] = useState<{
    input: PostMergeInput;
    preview: PostMergePreview;
  }>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const form = useForm({
    defaultValues: {
      source_uuid: source,
      destination_uuid: target.uuid,
      media: [],
      attachments: [],
      reason: "",
    } as PostMergeInput,
    validators: { onChange: postMergeInputSchema },
    onSubmit: async ({ value }) => {
      setBusy(true);
      setError(undefined);
      setReviewed(undefined);
      try {
        const result = await api.preview(value);
        setComparison(result.preview);
        setReviewed(result);
      } catch (error) {
        setError(error);
      } finally {
        setBusy(false);
      }
    },
  });
  async function apply() {
    if (!reviewed?.preview.ready || busy) return;
    setBusy(true);
    try {
      await onApply(reviewed.input, reviewed.preview);
    } finally {
      setBusy(false);
    }
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
      <p className="wrap-anywhere" data-selectable-text>
        {msg("post_merge.target", "Post to keep")}: {labels.name(target)}
      </p>
      <Button type="button" variant="outline" disabled={busy} onClick={onBack}>
        {msg("post_merge.change_target", "Choose another post")}
      </Button>
      <FieldGroup>
        {comparison && (
          <>
            <MergeOriginals posts={comparison.posts} />
            <form.Field name="selection">
              {(field) => (
                <MergeSelectionChoice
                  posts={comparison.posts}
                  value={field.state.value}
                  disabled={busy}
                  onChange={(value) => {
                    field.handleChange(value);
                    setReviewed(undefined);
                  }}
                />
              )}
            </form.Field>
            <form.Field name="gallery">
              {(field) => (
                <MergeGalleryChoice
                  posts={comparison.posts}
                  value={field.state.value}
                  disabled={busy}
                  onChange={(value) => {
                    field.handleChange(value);
                    setReviewed(undefined);
                  }}
                />
              )}
            </form.Field>
            <PostSection
              title={msg(
                "post_merge.media_choices",
                "Choose post links for media",
              )}
            >
              <form.Field name="media">
                {(field) => (
                  <MergeMediaChoices
                    posts={comparison.posts}
                    value={field.state.value}
                    disabled={busy}
                    onChange={(value) => {
                      field.handleChange(value);
                      setReviewed(undefined);
                    }}
                  />
                )}
              </form.Field>
            </PostSection>
            <PostSection
              title={msg(
                "post_merge.attachment_choices",
                "Choose attachment links",
              )}
            >
              <form.Field name="attachments">
                {(field) => (
                  <MergeAttachmentChoices
                    posts={comparison.posts}
                    value={field.state.value}
                    disabled={busy}
                    onChange={(value) => {
                      field.handleChange(value);
                      setReviewed(undefined);
                    }}
                  />
                )}
              </form.Field>
            </PostSection>
          </>
        )}
        <form.Field name="reason">
          {(field) => (
            <Field
              data-disabled={busy}
              data-invalid={!field.state.meta.isValid}
            >
              <FieldLabel htmlFor={id}>
                {msg("source_review.reason", "Reason (optional)")}
              </FieldLabel>
              <Input
                id={id}
                value={field.state.value}
                disabled={busy}
                maxLength={4096}
                aria-invalid={!field.state.meta.isValid}
                onBlur={field.handleBlur}
                onChange={(event) => {
                  field.handleChange(event.target.value);
                  setReviewed(undefined);
                }}
              />
              <FieldDescription>
                {msg(
                  "post_merge.reason_help",
                  "Record why these entries represent the same source post.",
                )}
              </FieldDescription>
              {!field.state.meta.isValid && (
                <FieldError>
                  {msg(
                    "source_review.reason_length",
                    "Shorten the reason before saving.",
                  )}
                </FieldError>
              )}
            </Field>
          )}
        </form.Field>
      </FieldGroup>
      {error !== undefined && <MergeError error={error} />}
      {reviewed && <MergePlan preview={reviewed.preview} />}
      <div className="flex flex-wrap gap-2">
        <form.Subscribe selector={(state) => state.canSubmit}>
          {(canSubmit) => (
            <Button
              type="submit"
              variant="outline"
              disabled={busy || !canSubmit}
            >
              {busy && <Spinner data-icon="inline-start" />}
              {msg("post_merge.preview", "Preview merge")}
            </Button>
          )}
        </form.Subscribe>
        <Button
          type="button"
          disabled={busy || !reviewed?.preview.ready}
          onClick={() => void apply()}
        >
          {msg("post_merge.apply", "Merge into selected post")}
        </Button>
      </div>
    </form>
  );
}

export function MergeEditor({
  api,
  posts,
  source,
  onApply,
}: {
  api: PostConsolidationAPI;
  posts: SourcePostAPI;
  source: string;
  onApply: (input: PostMergeInput, preview: PostMergePreview) => Promise<void>;
}) {
  const [target, setTarget] = useState<PostSummary>();
  const msg = useMsg();
  return (
    <div className="flex flex-col gap-4">
      <p>
        {msg(
          "post_merge.intro",
          "Merge duplicate records of the same source post. Choose the post to keep, then preview the source lists, media links and gallery changes. Different posts by the same creator should stay separate.",
        )}
      </p>
      {target ? (
        <MergeForm
          key={target.uuid}
          api={api}
          source={source}
          target={target}
          onApply={onApply}
          onBack={() => setTarget(undefined)}
        />
      ) : (
        <MergePostPicker api={posts} source={source} onSelect={setTarget} />
      )}
    </div>
  );
}
