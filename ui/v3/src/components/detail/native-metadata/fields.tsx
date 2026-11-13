import { useIntl } from "react-intl";
import { useMsg } from "@/hooks/message";
import type { MetadataFields } from "@/core/native-archive/metadata-review-api";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from "@/components/ui/card";
import { Expandable, Value, fieldMessages, type EntityNames } from "./shared";

export function FieldSummary({
  fields,
  entity,
}: {
  fields: MetadataFields;
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
          </CardContent>
        </Card>
      ))}
    </Expandable>
  );
}
