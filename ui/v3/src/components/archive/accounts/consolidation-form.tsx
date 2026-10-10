import { useId, useState } from "react";
import { useIntl } from "react-intl";
import { useForm } from "@tanstack/react-form";
import { z } from "zod";
import { useMsg } from "@/hooks/message";
import {
  accountSchema,
  ownershipReasonSchema,
  type Account,
  type AccountReviewAPI,
} from "@/core/native-archive/account-review-api";
import type {
  AccountConsolidationAPI,
  ConsolidationPreview,
} from "@/core/native-archive/account-consolidation-api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
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
import { AccountName, AccountOwner, PerformerName } from "./shared";
import { AccountIdentifiers } from "./evidence";
import { AccountPicker } from "./account-picker";

const modeSchema = z.enum(["preserve", "linked", "unlinked", "undecided"]);
const formSchema = z
  .object({
    target: accountSchema.nullable(),
    mode: modeSchema,
    performer: z
      .object({
        id: z.string().regex(/^[1-9]\d*$/),
        name: z.string(),
        disambiguation: z.string().nullish(),
      })
      .nullable(),
    reason: ownershipReasonSchema,
    ack: z.boolean(),
  })
  .refine(
    (value) =>
      value.target !== null &&
      (value.mode !== "linked" || value.performer !== null),
  );

function ReviewedAccount({
  account,
  api,
  destination,
}: {
  account: Account;
  api: AccountReviewAPI;
  destination: boolean;
}) {
  const msg = useMsg();
  return (
    <div className="min-w-0 rounded-lg border p-3 flex flex-col gap-2">
      <p className="font-medium">
        {destination
          ? msg("account_consolidation.keep", "Keep as the current account")
          : msg("account_consolidation.redirect", "Redirect this account")}
      </p>
      <p>
        <AccountName account={account} />
      </p>
      <p data-selectable-text className="text-xs wrap-anywhere">
        {account.uuid}
      </p>
      <AccountOwner ownership={account.ownership} tracked={account.tracked} />
      {account.identifiers.map((item) => (
        <p
          key={item.uuid}
          data-selectable-text
          className="text-sm wrap-anywhere"
        >
          {item.reference.kind}: {item.reference.value}
        </p>
      ))}
      <AccountIdentifiers
        key={`${account.uuid}:${account.revision}`}
        api={api}
        account={account.uuid}
      />
    </div>
  );
}

export function ConsolidationForm({
  account,
  accounts,
  api,
  disabled,
  onApply,
  onRefresh,
}: {
  account: Account;
  accounts: AccountReviewAPI;
  api: AccountConsolidationAPI;
  disabled: boolean;
  onApply: (preview: ConsolidationPreview) => Promise<void>;
  onRefresh: () => void;
}) {
  const msg = useMsg();
  const intl = useIntl();
  const id = useId();
  const [preview, setPreview] = useState<ConsolidationPreview>();
  const [conflicts, setConflicts] = useState<
    ConsolidationPreview["identifier_conflicts"]
  >([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  const form = useForm({
    defaultValues: {
      target: null as Account | null,
      mode: "preserve" as z.infer<typeof modeSchema>,
      performer: null as SearchChoice | null,
      reason: "",
      ack: false,
    },
    validators: { onChange: formSchema },
    onSubmit: async ({ value }) => {
      if (!value.target) return;
      setBusy(true);
      setError(undefined);
      setPreview(undefined);
      try {
        const target =
          value.mode === "linked" && value.performer
            ? await accounts.performer(value.performer.id)
            : undefined;
        const result = await api.preview({
          source_uuid: account.uuid,
          destination_uuid: value.target.uuid,
          ownership_mode: value.mode === "preserve" ? "preserve" : "choose",
          ...(value.mode !== "preserve"
            ? {
                ownership: {
                  state: value.mode,
                  ...(target
                    ? {
                        performer_uuid: target.uuid,
                        performer_revision: target.revision,
                      }
                    : {}),
                },
              }
            : {}),
          accept_identifier_conflicts: value.ack,
          reason: value.reason,
        });
        setPreview(result);
        setConflicts(result.identifier_conflicts);
      } catch (error) {
        setError(error);
      } finally {
        setBusy(false);
      }
    },
  });
  const blocked = disabled || busy;
  async function apply() {
    if (!preview?.ready) return;
    setBusy(true);
    try {
      await onApply(preview);
      setPreview(undefined);
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
      <p className="text-sm text-muted-foreground">
        {msg(
          "account_consolidation.help",
          "Consolidate only duplicate records for the same service account. Keep distinct accounts separate and link them to their performer. Identifiers, source evidence and history are retained.",
        )}
      </p>
      <FieldGroup>
        <form.Field name="target">
          {(field) => (
            <Field data-disabled={blocked}>
              <FieldLabel htmlFor={`${id}-target`}>
                {msg("account_consolidation.target", "Account to keep")}
              </FieldLabel>
              <AccountPicker
                api={accounts}
                source={account}
                id={`${id}-target`}
                disabled={blocked}
                onChange={(target) => {
                  field.handleChange(target);
                  setPreview(undefined);
                  setConflicts([]);
                  form.setFieldValue("ack", false);
                }}
              />
              {field.state.value && (
                <div>
                  <AccountName account={field.state.value} />
                  <p data-selectable-text className="text-xs wrap-anywhere">
                    {field.state.value.uuid}
                  </p>
                </div>
              )}
            </Field>
          )}
        </form.Field>
        <form.Field name="mode">
          {(field) => (
            <Field data-disabled={blocked}>
              <FieldLabel id={`${id}-mode`}>
                {msg("account_consolidation.ownership", "Resulting ownership")}
              </FieldLabel>
              <ToggleGroup
                aria-labelledby={`${id}-mode`}
                variant="outline"
                className="flex-wrap"
                value={[field.state.value]}
                disabled={blocked}
                onValueChange={(values) => {
                  if (values[0]) {
                    field.handleChange(values[0]);
                    setPreview(undefined);
                  }
                }}
              >
                <ToggleGroupItem value="preserve">
                  {msg(
                    "account_consolidation.preserve",
                    "Preserve compatible choice",
                  )}
                </ToggleGroupItem>
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
                  "account_consolidation.ownership_help",
                  "Preserve keeps an existing choice when the other account is undecided, or when both choices agree. Conflicting choices need an explicit resulting owner, unlink or review-later decision.",
                )}
              </FieldDescription>
            </Field>
          )}
        </form.Field>
        <form.Subscribe selector={(state) => state.values.mode}>
          {(mode) =>
            mode === "linked" && (
              <form.Field name="performer">
                {(field) => (
                  <Field data-disabled={blocked}>
                    <FieldLabel htmlFor={`${id}-performer`}>
                      {msg(
                        "account_consolidation.performer",
                        "Resulting performer",
                      )}
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
                maxLength={4096}
                value={field.state.value}
                disabled={blocked}
                onBlur={field.handleBlur}
                aria-invalid={!field.state.meta.isValid}
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
      {conflicts.length > 0 && (
        <Alert>
          <AlertTitle>
            {msg(
              "account_consolidation.ids_conflict",
              "Conflicting stable identifiers",
            )}
          </AlertTitle>
          <AlertDescription>
            <div className="flex flex-col gap-3">
              <p>
                {msg(
                  "account_consolidation.ids_help",
                  "Different stable IDs usually mean different accounts. Check the retained evidence before confirming they represent the same account.",
                )}
              </p>
              {conflicts.map((conflict) => (
                <p
                  key={`${conflict.namespace}:${conflict.kind}`}
                  data-selectable-text
                  className="wrap-anywhere"
                >
                  {conflict.namespace} · {conflict.kind}:{" "}
                  {conflict.values.join(", ")}
                </p>
              ))}
              <form.Field name="ack">
                {(field) => (
                  <Field orientation="horizontal" data-disabled={blocked}>
                    <Checkbox
                      id={`${id}-ack`}
                      checked={field.state.value}
                      disabled={blocked}
                      onCheckedChange={(checked) => {
                        field.handleChange(checked);
                        setPreview(undefined);
                      }}
                    />
                    <FieldLabel htmlFor={`${id}-ack`}>
                      {msg(
                        "account_consolidation.ack",
                        "I reviewed the conflicting IDs and confirm this is the same account",
                      )}
                    </FieldLabel>
                  </Field>
                )}
              </form.Field>
            </div>
          </AlertDescription>
        </Alert>
      )}
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
          [
            state.canSubmit,
            state.values.target,
            state.values.mode,
            state.values.performer,
          ] as const
        }
      >
        {([canSubmit, target, mode, performer]) => (
          <Button
            type="submit"
            variant="outline"
            disabled={
              blocked ||
              !canSubmit ||
              !target ||
              (mode === "linked" && !performer)
            }
          >
            {busy && <Spinner data-icon="inline-start" />}
            {msg("account_consolidation.preview", "Preview consolidation")}
          </Button>
        )}
      </form.Subscribe>
      {preview && (
        <Alert>
          <AlertTitle>
            {msg(
              "account_consolidation.preview_title",
              "Review account consolidation",
            )}
          </AlertTitle>
          <AlertDescription>
            <div className="flex flex-col gap-4">
              <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
                <ReviewedAccount
                  account={preview.source}
                  api={accounts}
                  destination={false}
                />
                <ReviewedAccount
                  account={preview.destination}
                  api={accounts}
                  destination
                />
              </div>
              <p>
                {intl.formatMessage(
                  {
                    id: "account_consolidation.counts",
                    defaultMessage:
                      "{accounts} account records and {identifiers} identifier claims will belong to the resulting account.",
                  },
                  {
                    accounts: preview.member_count,
                    identifiers: preview.identifier_count,
                  },
                )}
              </p>
              <div>
                <p className="font-medium">
                  {msg(
                    "account_consolidation.ownership",
                    "Resulting ownership",
                  )}
                </p>
                {preview.performer ? (
                  <PerformerName performer={preview.performer} />
                ) : preview.ownership?.state === "unlinked" ? (
                  msg("account_review.unlinked", "Explicitly unlinked")
                ) : preview.ownership ? (
                  msg("account_review.undecided", "Needs review")
                ) : (
                  msg(
                    "account_consolidation.owner_conflict",
                    "The current ownership choices conflict. Choose the resulting ownership explicitly, then preview again.",
                  )
                )}
              </div>
              {preview.blockers.includes("identifiers") && (
                <p>
                  {msg(
                    "account_consolidation.ack_required",
                    "Review and acknowledge the conflicting IDs, then preview again.",
                  )}
                </p>
              )}
              <p>
                {msg(
                  "account_consolidation.effect",
                  "This joins account identity records. It does not merge performers, move files or change depicted performers on scenes and images.",
                )}
              </p>
              <Button
                type="button"
                disabled={blocked || !preview.ready}
                onClick={() => void apply()}
              >
                {msg(
                  "account_consolidation.apply",
                  "Consolidate these account records",
                )}
              </Button>
            </div>
          </AlertDescription>
        </Alert>
      )}
    </form>
  );
}
