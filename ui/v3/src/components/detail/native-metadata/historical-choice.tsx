import { useId, useState } from "react";
import { useIntl } from "react-intl";
import { useForm } from "@tanstack/react-form";
import { z } from "zod";
import { useMsg } from "@/hooks/message";
import {
  nameSelectionSchema,
  type EditCandidate,
  type EditInput,
  type EditPreview,
  type MetadataReviewAPI,
  type MetadataFields,
  type NativeIdentity,
} from "@/core/native-archive/metadata-review-api";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from "@/components/ui/card";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import {
  Field,
  FieldGroup,
  FieldLabel,
  FieldDescription,
} from "@/components/ui/field";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectGroup,
  SelectItem,
} from "@/components/ui/select";
import { Value, ReviewError, fieldMessages, type EntityNames } from "./shared";
import { ExistingEntityPicker, type SearchChoice } from "./picker";

function PreviewChoice({
  api,
  initial,
  definition,
  fields,
  entity,
  blocked,
  onApply,
}: {
  api: MetadataReviewAPI;
  initial: EditInput;
  definition?: MetadataFields["fields"][number];
  fields: MetadataFields;
  entity: EntityNames;
  blocked: boolean;
  onApply: (preview: EditPreview) => Promise<void>;
}) {
  const msg = useMsg();
  const intl = useIntl();
  const id = useId();
  const form = useForm({
    defaultValues: { selections: {} as NonNullable<EditInput["selections"]> },
    validators: {
      onChange: z.object({
        selections: z.record(z.string(), nameSelectionSchema),
      }),
    },
  });
  const [preview, setPreview] = useState<EditPreview>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  async function load(selections = form.state.values.selections) {
    setPreview(undefined);
    setBusy(true);
    setError(undefined);
    try {
      setPreview(await api.preview({ ...initial, selections }));
    } catch (error) {
      setError(error);
    } finally {
      setBusy(false);
    }
  }
  async function select(name: string, uuid: string, revision: number) {
    const selections = {
      ...form.state.values.selections,
      [name]: { uuid, revision },
    };
    form.setFieldValue("selections", selections);
    await load(selections);
  }
  async function pick(
    name: string,
    kind: NativeIdentity["kind"],
    choice: SearchChoice,
  ) {
    setPreview(undefined);
    setBusy(true);
    setError(undefined);
    try {
      const identity = await api.identity(kind, choice.id);
      await select(name, identity.uuid, identity.revision);
    } catch (error) {
      setError(error);
      setBusy(false);
    }
  }
  return (
    <Card size="sm">
      <CardHeader>
        <CardTitle>
          {definition && fieldMessages[definition.definition.name]
            ? intl.formatMessage({
                id: fieldMessages[definition.definition.name],
              })
            : initial.source_field}
        </CardTitle>
        <CardDescription>
          {msg(
            "archive_review.preview_help",
            "Compare this retained choice with your current metadata before applying it.",
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {error !== undefined && <ReviewError error={error} />}
        {preview &&
          (preview.status === "unsupported" ? (
            <p>
              {msg(
                "archive_review.unsupported",
                "This retained field cannot be applied to this entity.",
              )}
            </p>
          ) : (
            <>
              <FieldGroup>
                <Field>
                  <FieldLabel>
                    {msg("archive_review.current", "Current value")}
                  </FieldLabel>
                  <Value
                    value={preview.current_value}
                    references={definition?.references}
                    entity={entity}
                  />
                </Field>
                <Field>
                  <FieldLabel>
                    {msg("archive_review.proposed", "Proposed value")}
                  </FieldLabel>
                  {preview.status === "ready" ? (
                    <Value
                      value={preview.value}
                      references={definition?.references}
                      entity={entity}
                      preview={preview}
                    />
                  ) : (
                    <FieldDescription>
                      {msg(
                        "archive_review.resolve_names",
                        "Choose an existing entry for every unresolved name. Nothing is partially applied.",
                      )}
                    </FieldDescription>
                  )}
                </Field>
              </FieldGroup>
              {preview.protected && (
                <Badge variant="secondary">
                  {msg(
                    "archive_review.replaces_protected",
                    "Replaces a protected choice",
                  )}
                </Badge>
              )}
              {preview.mode === "inherit" && (
                <Alert>
                  <AlertTitle>
                    {msg("archive_review.release", "Allow automatic updates")}
                  </AlertTitle>
                  <AlertDescription>
                    {msg(
                      "archive_review.release_help",
                      "Keeps the current value and removes its protection. An allowed metadata policy can update it later.",
                    )}
                  </AlertDescription>
                </Alert>
              )}
              <FieldGroup>
                {preview.names?.map((name, index) => {
                  const items = [...name.candidates];
                  if (
                    name.selected &&
                    !items.some((item) => item.uuid === name.selected?.uuid)
                  )
                    items.push(name.selected);
                  const options = [
                    {
                      value: "",
                      label: msg("archive_review.choose", "Choose an entry"),
                    },
                    ...items.map((item) => ({
                      value: item.uuid,
                      label: `${item.name}${item.disambiguation ? ` (${item.disambiguation})` : ""} (#${item.local_id})`,
                    })),
                  ];
                  const referenceKind = fields.fields.find(
                    (field) => field.definition.name === preview.field,
                  )?.definition.reference_kind;
                  return (
                    <Field key={name.name}>
                      <FieldLabel htmlFor={`${id}-${index}`}>
                        {name.name}
                      </FieldLabel>
                      <Select
                        items={options}
                        value={name.selected?.uuid ?? ""}
                        disabled={busy || blocked}
                        onValueChange={(value) => {
                          const item = items.find(
                            (item) => item.uuid === value,
                          );
                          if (item)
                            void select(name.name, item.uuid, item.revision);
                        }}
                      >
                        <SelectTrigger id={`${id}-${index}`}>
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectGroup>
                            {options.map((option) => (
                              <SelectItem
                                key={option.value}
                                value={option.value}
                                disabled={!option.value}
                              >
                                {option.label}
                              </SelectItem>
                            ))}
                          </SelectGroup>
                        </SelectContent>
                      </Select>
                      {name.more && (
                        <FieldDescription>
                          {msg(
                            "archive_review.more_candidates",
                            "More than 100 entries match. Search the library to choose the correct one.",
                          )}
                        </FieldDescription>
                      )}
                      {referenceKind && (
                        <>
                          <FieldLabel htmlFor={`${id}-search-${index}`}>
                            {msg(
                              "archive_review.choose_other",
                              "Choose another existing entry",
                            )}
                          </FieldLabel>
                          <ExistingEntityPicker
                            kind={referenceKind}
                            id={`${id}-search-${index}`}
                            disabled={busy || blocked}
                            onChange={(choice) =>
                              void pick(name.name, referenceKind, choice)
                            }
                          />
                        </>
                      )}
                    </Field>
                  );
                })}
              </FieldGroup>
            </>
          ))}
      </CardContent>
      <CardFooter className="flex flex-wrap gap-2">
        <Button
          variant="outline"
          disabled={busy || blocked}
          onClick={() => void load()}
        >
          {busy && <Spinner data-icon="inline-start" />}
          {msg("archive_review.preview", "Preview choice")}
        </Button>
        {preview?.status === "ready" && (
          <Button
            disabled={busy || blocked}
            onClick={async () => {
              setBusy(true);
              setError(undefined);
              try {
                await onApply(preview);
                setPreview(undefined);
              } catch (error) {
                setError(error);
                setPreview(undefined);
              } finally {
                setBusy(false);
              }
            }}
          >
            {msg("archive_review.apply", "Apply this choice")}
          </Button>
        )}
      </CardFooter>
    </Card>
  );
}

export function HistoricalChoice({
  candidate,
  api,
  fields,
  entity,
  blocked,
  onApply,
}: {
  candidate: EditCandidate;
  api: MetadataReviewAPI;
  fields: MetadataFields;
  entity: EntityNames;
  blocked: boolean;
  onApply: (preview: EditPreview) => Promise<void>;
}) {
  const msg = useMsg();
  const [history, setHistory] =
    useState<Awaited<ReturnType<MetadataReviewAPI["history"]>>>();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  async function load() {
    setBusy(true);
    setError(undefined);
    try {
      setHistory(await api.history(candidate.history_uuid));
    } catch (error) {
      setError(error);
    } finally {
      setBusy(false);
    }
  }
  return (
    <Card size="sm">
      <CardHeader>
        <CardTitle data-selectable-text className="wrap-anywhere">
          {candidate.relative_path}
        </CardTitle>
        <CardDescription>
          {candidate.source_time ||
            msg("archive_review.unknown_time", "Original edit time unknown")}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {error !== undefined && (
          <ReviewError error={error} retry={() => void load()} />
        )}
        {history?.edits.map((edit) => (
          <PreviewChoice
            key={edit.field}
            api={api}
            initial={{
              entity_uuid: fields.entity.uuid,
              history_uuid: candidate.history_uuid,
              match_uuid: candidate.match_uuid,
              source_field: edit.field,
            }}
            definition={fields.fields.find(
              (field) => field.definition.name === edit.target_field,
            )}
            fields={fields}
            entity={entity}
            blocked={blocked}
            onApply={onApply}
          />
        ))}
        {!history && (
          <Button variant="outline" disabled={busy} onClick={() => void load()}>
            {busy && <Spinner data-icon="inline-start" />}
            {msg("archive_review.review", "Review retained fields")}
          </Button>
        )}
      </CardContent>
    </Card>
  );
}
