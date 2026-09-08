import { useId, useState } from "react";
import { useForm } from "@tanstack/react-form";
import { useIntl } from "react-intl";
import { z } from "zod";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Field,
  FieldError,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import {
  inspectRecoverySource,
  type RecoverySource,
} from "./offline-migration";

/** Browsers that cannot enumerate IndexedDB still support recovery from a
 * known previous address. Inspection is local and cannot contact that server. */
export function OfflineSourceAddressForm({
  onFound,
  disabled,
}: {
  onFound: (source: RecoverySource) => void;
  disabled: boolean;
}) {
  const intl = useIntl();
  const id = useId();
  const [error, setError] = useState<string>();
  const form = useForm({
    defaultValues: { address: "" },
    validators: { onChange: z.object({ address: z.url() }) },
    onSubmit: async ({ value }) => {
      setError(undefined);
      try {
        const source = await inspectRecoverySource(value.address);
        if (!source.entries.length && !source.invalid)
          setError(
            intl.formatMessage({
              id: "offline.recovery.address_empty",
              defaultMessage: "No saved downloads were found for that address.",
            }),
          );
        else onFound(source);
      } catch (reason) {
        setError(reason instanceof Error ? reason.message : String(reason));
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
        <form.Field name="address">
          {(field) => (
            <Field
              data-invalid={field.state.meta.errors.length > 0}
              data-disabled={disabled}
            >
              <FieldLabel htmlFor={id}>
                {intl.formatMessage({
                  id: "offline.recovery.address",
                  defaultMessage: "Previous server address",
                })}
              </FieldLabel>
              <Input
                id={id}
                type="url"
                disabled={disabled}
                value={field.state.value}
                onChange={(event) => field.handleChange(event.target.value)}
                onBlur={field.handleBlur}
                aria-invalid={field.state.meta.errors.length > 0}
              />
              <FieldError errors={field.state.meta.errors} />
            </Field>
          )}
        </form.Field>
        {error && <FieldError>{error}</FieldError>}
        <form.Subscribe
          selector={(state) => [state.canSubmit, state.isSubmitting]}
        >
          {([canSubmit, submitting]) => (
            <Button
              type="submit"
              variant="outline"
              size="sm"
              disabled={disabled || !canSubmit || submitting}
            >
              {intl.formatMessage({
                id: "offline.recovery.inspect",
                defaultMessage: "Find saved downloads",
              })}
            </Button>
          )}
        </form.Subscribe>
      </FieldGroup>
    </form>
  );
}
