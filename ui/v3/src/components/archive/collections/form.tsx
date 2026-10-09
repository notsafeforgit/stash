import { useCallback, useId, useState } from "react";
import { useForm } from "@tanstack/react-form";
import { useMsg } from "@/hooks/message";
import {
  collectionInputSchema,
  type CollectionAPI,
  type CollectionInput,
  type MediaRoot,
} from "@/core/native-archive/collection-api";
import {
  createAccountReviewAPI,
  type Account,
} from "@/core/native-archive/account-review-api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Field,
  FieldGroup,
  FieldLabel,
  FieldDescription,
  FieldError,
} from "@/components/ui/field";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectGroup,
  SelectItem,
} from "@/components/ui/select";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { AccountName, AccountService } from "../accounts/shared";
import { LookupPicker } from "./lookup-picker";
import { useCollectionLabels } from "./shared";

export function CollectionForm({
  api,
  input,
  root,
  account,
  disabled,
  onSave,
}: {
  api: CollectionAPI;
  input: CollectionInput;
  root: MediaRoot | null;
  account: Account | null;
  disabled: boolean;
  onSave: (input: CollectionInput) => Promise<void>;
}) {
  const msg = useMsg();
  const id = useId();
  const labels = useCollectionLabels();
  const [accounts] = useState(() => createAccountReviewAPI(api.endpoint));
  const [selectedRoot, setRoot] = useState(root);
  const [selectedAccount, setAccount] = useState(account);
  const roots = useCallback(
    async (query: string, signal: AbortSignal) =>
      (await api.roots(query, signal)).filter(
        (item) => item.state === "active",
      ),
    [api],
  );
  const sources = useCallback(
    async (query: string, signal: AbortSignal) =>
      (
        await accounts.accounts(
          { q: query, namespace: "", ownership: "all" },
          "",
          signal,
        )
      ).filter((item) => item.uuid === item.canonical_uuid),
    [accounts],
  );
  const form = useForm({
    defaultValues: input,
    validators: { onChange: collectionInputSchema },
    onSubmit: ({ value }) => onSave(value),
  });
  const kindItems = Object.entries(labels.kinds).map(([value, label]) => ({
    value,
    label,
  }));
  const strings = [
    {
      key: "label",
      label: msg("collections.name", "Collection name"),
      help: undefined,
      max: 1024,
    },
    {
      key: "target_url",
      label: msg("collections.target", "Source URL"),
      help: msg(
        "collections.target_help",
        "Use one profile URL per Reddit account; its retrieval passes are handled automatically. Other feeds and searches remain separate. Leave blank for direct file imports.",
      ),
      max: 8192,
    },
  ] as const;
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
        {strings.map(({ key, label, help, max }) => (
          <form.Field key={key} name={key}>
            {(field) => (
              <Field
                data-invalid={!field.state.meta.isValid}
                data-disabled={disabled}
              >
                <FieldLabel htmlFor={`${id}-${key}`}>{label}</FieldLabel>
                <Input
                  id={`${id}-${key}`}
                  value={field.state.value}
                  maxLength={max}
                  disabled={disabled}
                  aria-invalid={!field.state.meta.isValid}
                  onBlur={field.handleBlur}
                  onChange={(event) => field.handleChange(event.target.value)}
                />
                {help && <FieldDescription>{help}</FieldDescription>}
                {!field.state.meta.isValid && (
                  <FieldError>
                    {msg(
                      "collections.invalid_text",
                      "Enter a valid value within the field limit, without control characters.",
                    )}
                  </FieldError>
                )}
              </Field>
            )}
          </form.Field>
        ))}
        <form.Field name="kind">
          {(field) => (
            <Field data-disabled={disabled}>
              <FieldLabel htmlFor={`${id}-kind`}>
                {msg("collections.kind", "Collection type")}
              </FieldLabel>
              <Select
                items={kindItems}
                value={field.state.value}
                disabled={disabled}
                onValueChange={(value) => {
                  const kind =
                    collectionInputSchema.shape.kind.safeParse(value);
                  if (kind.success) field.handleChange(kind.data);
                }}
              >
                <SelectTrigger id={`${id}-kind`} className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {kindItems.map((item) => (
                      <SelectItem key={item.value} value={item.value}>
                        {item.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              {field.state.value === "legacy_catalog" && (
                <FieldDescription>
                  {msg(
                    "collections.imported_catalog_help",
                    "Groups posts and files imported from an old catalog. Its status applies only to this collection. The same account can have separate active scrape targets.",
                  )}
                </FieldDescription>
              )}
            </Field>
          )}
        </form.Field>
        <Field data-disabled={disabled}>
          <FieldLabel htmlFor={`${id}-account`}>
            {msg("collections.account", "Source account")}
          </FieldLabel>
          <LookupPicker
            id={`${id}-account`}
            value={selectedAccount}
            disabled={disabled}
            search={sources}
            label={(item) =>
              item.label || item.identifiers[0]?.reference.value || item.uuid
            }
            renderItem={(item) => (
              <span className="flex flex-col gap-1">
                <span>
                  <AccountName account={item} /> ·{" "}
                  <AccountService namespace={item.namespace} />
                </span>
                <span className="text-xs text-muted-foreground">
                  {item.identifiers.find(
                    (identifier) => identifier.reference.kind === "id",
                  )?.reference.value ?? item.uuid}
                </span>
              </span>
            )}
            onChange={(value) => {
              setAccount(value);
              form.setFieldValue("account_uuid", value?.uuid ?? null);
              if (value) form.setFieldValue("namespace", value.namespace);
            }}
          />
          <FieldDescription>
            {msg(
              "collections.account_help",
              "Identifies the publisher or account being scraped. Performer attribution is a separate choice, including for aggregator accounts.",
            )}
          </FieldDescription>
        </Field>
        <form.Field name="namespace">
          {(field) => (
            <Field
              data-invalid={!field.state.meta.isValid}
              data-disabled={disabled || !!selectedAccount}
            >
              <FieldLabel htmlFor={`${id}-namespace`}>
                {msg("collections.namespace", "Source service")}
              </FieldLabel>
              <Input
                id={`${id}-namespace`}
                disabled={disabled || !!selectedAccount}
                value={field.state.value}
                placeholder="native:reddit"
                maxLength={128}
                aria-invalid={!field.state.meta.isValid}
                onBlur={field.handleBlur}
                onChange={(event) => field.handleChange(event.target.value)}
              />
              <FieldDescription>
                {msg(
                  "collections.namespace_help",
                  "Filled from the source account when selected. For another scraper source, use its qualified service name; leave blank for folders or manual batches.",
                )}
              </FieldDescription>
              {!field.state.meta.isValid && (
                <FieldError>
                  {msg(
                    "collections.namespace_invalid",
                    "Use a qualified service such as native:reddit or mirror:coomer:onlyfans.",
                  )}
                </FieldError>
              )}
            </Field>
          )}
        </form.Field>
        <Field data-disabled={disabled}>
          <FieldLabel htmlFor={`${id}-root`}>
            {msg("collections.root", "Media root")}
          </FieldLabel>
          <LookupPicker
            id={`${id}-root`}
            value={selectedRoot}
            disabled={disabled}
            search={roots}
            label={(item) =>
              `${item.label}${item.binding ? ` · ${item.binding.path}` : ""}`
            }
            onChange={(value) => {
              setRoot(value);
              form.setFieldValue("root_uuid", value?.uuid ?? null);
              form.setFieldValue(
                "path_prefix",
                value ? form.getFieldValue("path_prefix") || "." : "",
              );
            }}
          />
          <FieldDescription>
            {msg(
              "collections.root_help",
              "Choose a registered media root to associate a folder with this collection.",
            )}
          </FieldDescription>
        </Field>
        <form.Field name="path_prefix">
          {(field) => (
            <Field
              data-disabled={disabled || !selectedRoot}
              data-invalid={!field.state.meta.isValid}
            >
              <FieldLabel htmlFor={`${id}-path`}>
                {msg("collections.path", "Folder within the media root")}
              </FieldLabel>
              <Input
                id={`${id}-path`}
                value={field.state.value}
                maxLength={4096}
                disabled={disabled || !selectedRoot}
                aria-invalid={!field.state.meta.isValid}
                onBlur={field.handleBlur}
                onChange={(event) => field.handleChange(event.target.value)}
              />
              <FieldDescription>
                {msg(
                  "collections.path_help",
                  "Use a relative folder such as Purchased/River, or . for the entire root.",
                )}
              </FieldDescription>
              {!field.state.meta.isValid && (
                <FieldError>
                  {msg(
                    "collections.path_invalid",
                    "Use a relative path without empty segments, backslashes or parent-directory references.",
                  )}
                </FieldError>
              )}
            </Field>
          )}
        </form.Field>
        <form.Field name="state">
          {(field) => (
            <Field data-disabled={disabled}>
              <FieldLabel id={`${id}-state`}>
                {msg("collections.state", "Status")}
              </FieldLabel>
              <ToggleGroup
                aria-labelledby={`${id}-state`}
                value={[field.state.value]}
                disabled={disabled}
                variant="outline"
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
                  "collections.state_help",
                  "Active allows configured ingestion. Disabled pauses it. Retired marks this collection as no longer in use. Choose Active and save to restore either. All states retain existing media and history.",
                )}
              </FieldDescription>
              <FieldDescription>
                {msg(
                  "collections.state_scope_help",
                  "This status does not change other collections for the same account or edit gallery-dl target lists. Activating a collection does not start a scrape; scheduling and metadata rules are configured separately.",
                )}
              </FieldDescription>
            </Field>
          )}
        </form.Field>
        <form.Field name="reason">
          {(field) => (
            <Field
              data-disabled={disabled}
              data-invalid={!field.state.meta.isValid}
            >
              <FieldLabel htmlFor={`${id}-reason`}>
                {msg("collections.reason", "Reason for this change (optional)")}
              </FieldLabel>
              <Input
                id={`${id}-reason`}
                value={field.state.value}
                maxLength={4096}
                disabled={disabled}
                aria-invalid={!field.state.meta.isValid}
                onBlur={field.handleBlur}
                onChange={(event) => field.handleChange(event.target.value)}
              />
              {!field.state.meta.isValid && (
                <FieldError>
                  {msg(
                    "collections.invalid_text",
                    "Enter a valid value within the field limit, without control characters.",
                  )}
                </FieldError>
              )}
            </Field>
          )}
        </form.Field>
      </FieldGroup>
      <form.Subscribe selector={(state) => state.canSubmit}>
        {(canSubmit) => (
          <Button
            type="submit"
            className="w-fit"
            disabled={disabled || !canSubmit}
          >
            {msg("collections.save", "Save collection")}
          </Button>
        )}
      </form.Subscribe>
    </form>
  );
}
