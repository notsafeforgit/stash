import { ChevronDown } from "lucide-react";
import { useMsg } from "@/hooks/message";
import {
  NativeArchiveError,
  type NativeIdentity,
  type EditPreview,
} from "@/core/native-archive/metadata-review-api";
import { Button } from "@/components/ui/button";
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert";
import {
  Collapsible,
  CollapsibleTrigger,
  CollapsibleContent,
} from "@/components/ui/collapsible";

export const fieldMessages: Record<string, string> = {
  title: "title",
  code: "scene_code",
  details: "details",
  date: "date",
  rating100: "rating",
  organized: "organized",
  urls: "urls",
  custom_fields: "custom_fields.title",
  studio: "studio",
  performers: "performers",
  tags: "tags",
  groups: "groups",
  director: "director",
  production_date: "production_date",
  photographer: "photographer",
};

export type EntityNames = {
  id: string;
  performers: { id: string; name: string }[];
  tags: { id: string; name: string }[];
  studio?: { id: string; name: string } | null;
  groups?: { group: { id: string; name: string } }[];
};

export function ReviewError({
  error,
  retry,
}: {
  error: unknown;
  retry?: () => void;
}) {
  const msg = useMsg();
  const stale =
    error instanceof NativeArchiveError && error.code === "preview_changed";
  return (
    <Alert variant="destructive">
      <AlertTitle>
        {stale
          ? msg("archive_review.changed", "This choice has changed")
          : msg("archive_review.failed", "Could not complete this step")}
      </AlertTitle>
      <AlertDescription>
        <p>
          {stale
            ? msg(
                "archive_review.changed_help",
                "Load a fresh preview before applying this choice.",
              )
            : msg(
                "archive_review.failed_help",
                "Check your connection and access to Stash, then retry. An unconfirmed change remains saved in this browser.",
              )}
        </p>
        {retry && (
          <Button variant="outline" size="sm" onClick={retry}>
            {msg("actions.retry", "Retry")}
          </Button>
        )}
      </AlertDescription>
    </Alert>
  );
}

export function Expandable({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}) {
  return (
    <Collapsible>
      <CollapsibleTrigger
        render={
          <Button
            type="button"
            variant="ghost"
            className="w-full justify-between"
          />
        }
      >
        {title}
        <ChevronDown data-icon="inline-end" />
      </CollapsibleTrigger>
      <CollapsibleContent className="flex flex-col gap-3 pt-2">
        {children}
      </CollapsibleContent>
    </Collapsible>
  );
}

export function Value({
  value,
  references = [],
  entity,
  preview,
}: {
  value: unknown;
  references?: NativeIdentity[];
  entity: EntityNames;
  preview?: EditPreview;
}) {
  const msg = useMsg();
  function label(id: string) {
    const chosen = preview?.names
      ?.flatMap((name) => (name.selected ? [name.selected] : []))
      .find((item) => item.uuid === id);
    if (chosen) return chosen.name;
    const ref = references.find((item) => item.uuid === id);
    const options =
      ref?.kind === "performer"
        ? entity.performers
        : ref?.kind === "tag"
          ? entity.tags
          : ref?.kind === "group"
            ? (entity.groups?.map((item) => item.group) ?? [])
            : ref?.kind === "studio" && entity.studio
              ? [entity.studio]
              : [];
    return (
      options.find((item) => item.id === String(ref?.local_id))?.name ?? id
    );
  }
  let text: string;
  if (
    value === null ||
    value === undefined ||
    value === "" ||
    (Array.isArray(value) && value.length === 0)
  )
    text = msg("archive_review.empty_value", "No value");
  else if (typeof value === "boolean")
    text = value ? msg("true", "True") : msg("false", "False");
  else if (typeof value === "string") text = label(value);
  else if (Array.isArray(value))
    text = value
      .map((item: unknown) =>
        typeof item === "string" ? label(item) : JSON.stringify(item),
      )
      .join("\n");
  else text = JSON.stringify(value, null, 2);
  return (
    <p
      data-selectable-text
      className="whitespace-pre-wrap wrap-anywhere text-sm"
    >
      {text}
    </p>
  );
}
