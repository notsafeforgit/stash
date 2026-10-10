import { useId, useState } from "react";
import { useForm } from "@tanstack/react-form";
import { z } from "zod";
import { useMsg } from "@/hooks/message";
import {
  ownershipStateSchema,
  ownershipReasonSchema,
  type Account,
  type AccountReviewAPI,
  type OwnershipPreview,
} from "@/core/native-archive/account-review-api";
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
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import {
  ExistingEntityPicker,
  searchChoiceLabel,
  type SearchChoice,
} from "@/components/detail/native-metadata/picker";
import { ReviewError } from "@/components/detail/native-metadata/shared";
import { AccountOwner, PerformerName } from "./shared";

const formSchema = z
  .object({
    state: ownershipStateSchema,
    choice: z
      .object({
        id: z.string().regex(/^[1-9]\d*$/),
        name: z.string(),
        disambiguation: z.string().nullish(),
      })
      .nullable(),
    reason: ownershipReasonSchema,
  })
  .refine((value) => value.state !== "linked" || value.choice !== null);

export function OwnershipForm({
  api,
  account,
  disabled,
  onApply,
  onRefresh,
}: {
  api: AccountReviewAPI;
  account: Account;
  disabled: boolean;
  onApply: (preview: OwnershipPreview) => Promise<void>;
  onRefresh: () => void;
}) {
  const msg = useMsg();
  const id = useId();
  const [preview, setPreview] = useState<OwnershipPreview>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const form = useForm({
    defaultValues: {
      state: "linked" as z.infer<typeof ownershipStateSchema>,
      choice: null as SearchChoice | null,
      reason: "",
    },
    validators: { onChange: formSchema },
    onSubmit: async ({ value }) => {
      setBusy(true);
      setError(undefined);
      setPreview(undefined);
      try {
        const target =
          value.state === "linked" && value.choice
            ? await api.performer(value.choice.id)
            : undefined;
        setPreview(
          await api.preview({
            account_uuid: account.uuid,
            account_revision: account.revision,
            state: value.state,
            reason: value.reason,
            ...(target
              ? {
                  performer_uuid: target.uuid,
                  performer_revision: target.revision,
                }
              : {}),
          }),
        );
      } catch (error) {
        setError(error);
      } finally {
        setBusy(false);
      }
    },
  });
  const blocked = disabled || busy;
  async function apply() {
    if (!preview) return;
    setBusy(true);
    setError(undefined);
    try {
      await onApply(preview);
      setPreview(undefined);
    } catch (error) {
      setError(error);
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
      <FieldGroup>
        <form.Field name="state">
          {(field) => (
            <Field data-disabled={blocked}>
              <FieldLabel id={`${id}-choice`}>
                {msg("account_review.action", "Ownership choice")}
              </FieldLabel>
              <ToggleGroup
                aria-labelledby={`${id}-choice`}
                variant="outline"
                value={[field.state.value]}
                disabled={blocked}
                onValueChange={(values) => {
                  if (values[0]) {
                    field.handleChange(values[0]);
                    setPreview(undefined);
                  }
                }}
              >
                <ToggleGroupItem value="linked">
                  {msg("account_review.link", "Link performer")}
                </ToggleGroupItem>
                <ToggleGroupItem value="unlinked">
                  {msg("account_review.unlink", "Unlink")}
                </ToggleGroupItem>
                <ToggleGroupItem value="undecided">
                  {msg("account_review.reset", "Review later")}
                </ToggleGroupItem>
              </ToggleGroup>
              <FieldDescription>
                {msg(
                  "account_review.choice_help",
                  "Unlink leaves this account without a performer association. Review later clears the choice and returns the account to the review queue.",
                )}
              </FieldDescription>
            </Field>
          )}
        </form.Field>
        <form.Subscribe selector={(state) => state.values.state}>
          {(state) =>
            state === "linked" && (
              <form.Field name="choice">
                {(field) => (
                  <Field data-disabled={blocked}>
                    <FieldLabel htmlFor={`${id}-performer`}>
                      {msg("performer", "Performer")}
                    </FieldLabel>
                    <ExistingEntityPicker
                      kind="performer"
                      id={`${id}-performer`}
                      disabled={blocked}
                      onChange={(choice) => {
                        field.handleChange(choice);
                        setPreview(undefined);
                      }}
                    />
                    {field.state.value && (
                      <p>{searchChoiceLabel(field.state.value)}</p>
                    )}
                    <FieldDescription>
                      {msg(
                        "account_review.performer_help",
                        "Choose the existing performer explicitly. Matching names or aliases alone do not establish ownership.",
                      )}
                    </FieldDescription>
                  </Field>
                )}
              </form.Field>
            )
          }
        </form.Subscribe>
        <form.Field name="reason">
          {(field) => (
            <Field
              data-disabled={blocked}
              data-invalid={!field.state.meta.isValid}
            >
              <FieldLabel htmlFor={`${id}-reason`}>
                {msg("account_review.reason", "Reason (optional)")}
              </FieldLabel>
              <Input
                id={`${id}-reason`}
                value={field.state.value}
                maxLength={4096}
                disabled={blocked}
                aria-invalid={!field.state.meta.isValid}
                onBlur={field.handleBlur}
                onChange={(event) => {
                  field.handleChange(event.target.value);
                  setPreview(undefined);
                }}
              />
              {!field.state.meta.isValid && (
                <FieldError>
                  {msg(
                    "account_review.reason_invalid",
                    "Use a shorter reason without control characters.",
                  )}
                </FieldError>
              )}
            </Field>
          )}
        </form.Field>
      </FieldGroup>
      {error !== undefined && (
        <ReviewError
          error={error}
          retry={() => {
            setError(undefined);
            setPreview(undefined);
            onRefresh();
          }}
        />
      )}
      <form.Subscribe
        selector={(state) =>
          [state.canSubmit, state.values.state, state.values.choice] as const
        }
      >
        {([canSubmit, state, choice]) => (
          <Button
            type="submit"
            variant="outline"
            disabled={blocked || !canSubmit || (state === "linked" && !choice)}
          >
            {busy && <Spinner data-icon="inline-start" />}
            {msg("account_review.preview", "Preview ownership")}
          </Button>
        )}
      </form.Subscribe>
      {preview && (
        <Alert>
          <AlertTitle>
            {msg("account_review.preview_title", "Review ownership change")}
          </AlertTitle>
          <AlertDescription>
            <div className="flex flex-col gap-3">
              <div>
                <p>{msg("account_review.current_owner", "Current owner")}</p>
                <AccountOwner
                  ownership={preview.account.ownership}
                  tracked={preview.account.tracked}
                />
              </div>
              <div>
                <p>{msg("account_review.proposed_owner", "Proposed owner")}</p>
                {preview.performer ? (
                  <PerformerName performer={preview.performer} />
                ) : preview.input.state === "unlinked" ? (
                  msg("account_review.unlinked", "Explicitly unlinked")
                ) : (
                  msg("account_review.undecided", "Needs review")
                )}
              </div>
              <p>
                {msg(
                  "account_review.attribution_help",
                  "This records account ownership. Depicted performers on scenes and images are unchanged.",
                )}
              </p>
              <Button
                type="button"
                disabled={blocked}
                onClick={() => void apply()}
              >
                {msg("account_review.apply", "Apply ownership change")}
              </Button>
            </div>
          </AlertDescription>
        </Alert>
      )}
    </form>
  );
}
