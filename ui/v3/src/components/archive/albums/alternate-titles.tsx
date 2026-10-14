import { ChevronDown } from "lucide-react";
import { useIntl } from "react-intl";
import { useMsg } from "@/hooks/message";
import { Button } from "@/components/ui/button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";

export function GalleryAlternateTitles({
  titles,
  primaryTitle,
}: {
  titles: readonly string[];
  primaryTitle?: string | null;
}) {
  const msg = useMsg();
  const intl = useIntl();
  const uniqueTitles = [...new Set(titles.map((title) => title.trim()))].filter(
    (title) => title !== "" && title !== primaryTitle?.trim(),
  );
  if (uniqueTitles.length === 0) return null;

  return (
    <div className="flex min-w-0 flex-col gap-2">
      <span className="text-sm text-muted-foreground">
        {msg("gallery_origin.alternate_titles", "Alternate post titles")}
      </span>
      <ul className="m-0 flex list-none flex-col gap-2 p-0">
        {uniqueTitles.slice(0, 3).map((title) => (
          <li
            key={title}
            className="whitespace-pre-wrap wrap-anywhere"
            data-selectable-text
          >
            {title}
          </li>
        ))}
      </ul>
      {uniqueTitles.length > 3 && (
        <Collapsible>
          <CollapsibleTrigger render={<Button variant="ghost" size="sm" />}>
            {intl.formatMessage(
              {
                id: "gallery_origin.more_titles",
                defaultMessage:
                  "{count, plural, one {# more title} other {# more titles}}",
              },
              { count: uniqueTitles.length - 3 },
            )}
            <ChevronDown data-icon="inline-end" />
          </CollapsibleTrigger>
          <CollapsibleContent>
            <ul className="m-0 flex list-none flex-col gap-2 p-0 pt-2">
              {uniqueTitles.slice(3).map((title) => (
                <li
                  key={title}
                  className="whitespace-pre-wrap wrap-anywhere"
                  data-selectable-text
                >
                  {title}
                </li>
              ))}
            </ul>
          </CollapsibleContent>
        </Collapsible>
      )}
    </div>
  );
}
