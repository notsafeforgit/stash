import { useId } from "react";
import { useForm } from "@tanstack/react-form";
import { useMsg } from "@/hooks/message";
import {
  postFilterSchema,
  postQuerySchema,
  type PostFilter,
} from "@/core/native-archive/source-post-api";
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

export function PostSearch({
  filter,
  onChange,
}: {
  filter: PostFilter;
  onChange: (value: PostFilter) => void;
}) {
  const id = useId();
  const msg = useMsg();
  const form = useForm({
    defaultValues: filter,
    validators: { onChange: postQuerySchema },
    onSubmit: ({ value }) => onChange(postFilterSchema.parse(value)),
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
        <form.Field name="mode">
          {(field) => (
            <Field>
              <FieldLabel id={`${id}-mode`}>
                {msg("source_posts.find_by", "Find posts by")}
              </FieldLabel>
              <ToggleGroup
                variant="outline"
                multiple={false}
                className="flex-wrap"
                aria-labelledby={`${id}-mode`}
                value={[field.state.value]}
                onValueChange={(values) => {
                  const mode = postFilterSchema.shape.mode.safeParse(values[0]);
                  if (mode.success && values[0]) field.handleChange(mode.data);
                }}
              >
                <ToggleGroupItem value="all">
                  {msg("source_posts.browse", "Browse")}
                </ToggleGroupItem>
                <ToggleGroupItem value="url">
                  {msg("source_posts.url", "Post URL")}
                </ToggleGroupItem>
                <ToggleGroupItem value="source_id">
                  {msg("source_posts.source_id", "Source ID")}
                </ToggleGroupItem>
                <ToggleGroupItem value="uuid">
                  {msg("source_posts.archive_id", "Archive ID")}
                </ToggleGroupItem>
              </ToggleGroup>
            </Field>
          )}
        </form.Field>
        <form.Subscribe selector={(state) => state.values.mode}>
          {(mode) => (
            <>
              {mode === "source_id" && (
                <form.Field name="namespace">
                  {(field) => (
                    <Field data-invalid={!field.state.meta.isValid}>
                      <FieldLabel htmlFor={`${id}-namespace`}>
                        {msg("source_posts.namespace", "Source namespace")}
                      </FieldLabel>
                      <Input
                        id={`${id}-namespace`}
                        value={field.state.value}
                        maxLength={128}
                        aria-invalid={!field.state.meta.isValid}
                        onBlur={field.handleBlur}
                        onChange={(event) =>
                          field.handleChange(event.target.value)
                        }
                      />
                      <FieldDescription>
                        {msg(
                          "source_posts.namespace_help",
                          "For example: native:reddit, native:twitter or mirror:coomer:onlyfans.",
                        )}
                      </FieldDescription>
                      {!field.state.meta.isValid && (
                        <FieldError>
                          {msg(
                            "source_posts.namespace_invalid",
                            "Enter a complete source namespace.",
                          )}
                        </FieldError>
                      )}
                    </Field>
                  )}
                </form.Field>
              )}
              {mode !== "all" && (
                <form.Field name="value">
                  {(field) => (
                    <Field data-invalid={!field.state.meta.isValid}>
                      <FieldLabel htmlFor={`${id}-value`}>
                        {mode === "url"
                          ? msg("source_posts.url", "Post URL")
                          : mode === "uuid"
                            ? msg("source_posts.archive_id", "Archive ID")
                            : msg("source_posts.source_id", "Source ID")}
                      </FieldLabel>
                      <Input
                        id={`${id}-value`}
                        value={field.state.value}
                        maxLength={8192}
                        aria-invalid={!field.state.meta.isValid}
                        onBlur={field.handleBlur}
                        onChange={(event) =>
                          field.handleChange(event.target.value)
                        }
                      />
                      <FieldDescription>
                        {msg(
                          "source_posts.exact_help",
                          "Use the exact stored URL or identifier. Shared URLs can match more than one post.",
                        )}
                      </FieldDescription>
                      {!field.state.meta.isValid && (
                        <FieldError>
                          {msg(
                            "source_posts.value_invalid",
                            "Enter a valid value for this lookup type.",
                          )}
                        </FieldError>
                      )}
                    </Field>
                  )}
                </form.Field>
              )}
            </>
          )}
        </form.Subscribe>
      </FieldGroup>
      <form.Subscribe selector={(state) => state.canSubmit}>
        {(canSubmit) => (
          <Button type="submit" className="w-fit" disabled={!canSubmit}>
            {msg("source_posts.find", "Find posts")}
          </Button>
        )}
      </form.Subscribe>
    </form>
  );
}
