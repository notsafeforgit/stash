import { useState } from "react";
import { useIntl } from "react-intl";
import { useMsg } from "@/hooks/message";
import type {
  FieldDecision,
  MetadataReviewAPI,
  MetadataFields,
} from "@/core/native-archive/metadata-review-api";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from "@/components/ui/card";
import {
  Expandable,
  ReviewError,
  Value,
  fieldMessages,
  type EntityNames,
} from "./shared";

function DecisionHistory({
  api,
  entityUUID,
  field,
  entity,
}: {
  api: MetadataReviewAPI;
  entityUUID: string;
  field: string;
  entity: EntityNames;
}) {
  const msg = useMsg();
  const intl = useIntl();
  const [rows, setRows] = useState<FieldDecision[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [more, setMore] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<unknown>();
  async function load() {
    setBusy(true);
    setError(undefined);
    try {
      const page = await api.decisions(
        entityUUID,
        field,
        rows.at(-1)?.sequence,
      );
      setRows((current) => [...current, ...page]);
      setMore(page.length === api.pageLimit);
      setLoaded(true);
    } catch (error) {
      setError(error);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="flex flex-col gap-3">
      {rows.map((row) => (
        <div key={row.uuid} className="flex flex-col gap-1">
          <span className="text-xs text-muted-foreground">
            {intl.formatDate(row.created_at, {
              dateStyle: "medium",
              timeStyle: "short",
            })}
          </span>
          <Value value={row.value} entity={entity} />
          <p className="text-xs text-muted-foreground">{row.reason}</p>
        </div>
      ))}
      {loaded && rows.length === 0 && (
        <p className="text-sm">
          {msg("archive_review.no_decisions", "No recorded field choices.")}
        </p>
      )}
      {error !== undefined && <ReviewError error={error} />}
      {more && (
        <Button
          variant="outline"
          size="sm"
          disabled={busy}
          onClick={() => void load()}
        >
          {busy && <Spinner data-icon="inline-start" />}
          {loaded
            ? msg("archive_review.load_more", "Load more")
            : msg("archive_review.load_history", "Load choice history")}
        </Button>
      )}
    </div>
  );
}

export function FieldSummary({
  fields,
  api,
  entity,
}: {
  fields: MetadataFields;
  api: MetadataReviewAPI;
  entity: EntityNames;
}) {
  const msg = useMsg();
  const intl = useIntl();
  return (
    <Expandable
      title={msg("archive_review.current_fields", "Current field choices")}
    >
      {fields.fields.map((field) => (
        <Card key={field.definition.name} size="sm">
          <CardHeader>
            <CardTitle>
              {fieldMessages[field.definition.name]
                ? intl.formatMessage({
                    id: fieldMessages[field.definition.name],
                  })
                : field.definition.name}
            </CardTitle>
            <CardDescription>
              {field.protected
                ? msg(
                    "archive_review.protected",
                    "Protected from automatic updates",
                  )
                : msg("archive_review.inherited", "Automatic updates allowed")}
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-2">
            <Value
              value={field.value}
              references={field.references}
              entity={entity}
            />
            {field.decision?.reason && (
              <p className="text-xs text-muted-foreground">
                {field.decision.reason}
              </p>
            )}
            <Expandable
              title={msg("archive_review.choice_history", "Choice history")}
            >
              <DecisionHistory
                key={field.decision?.uuid ?? field.definition.name}
                api={api}
                entityUUID={fields.entity.uuid}
                field={field.definition.name}
                entity={entity}
              />
            </Expandable>
          </CardContent>
        </Card>
      ))}
    </Expandable>
  );
}
