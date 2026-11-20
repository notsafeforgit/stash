import { useIntl, type IntlShape } from "react-intl";
import { Badge } from "@/components/ui/badge";
import { reviewSummaryLimit } from "@/core/native-archive/review-queue-api";
import { cn } from "@/lib/utils";

export function reviewAttentionLabel(intl: IntlShape, count: number) {
  return count >= reviewSummaryLimit
    ? intl.formatMessage(
        {
          id: "review_queue.attention_many",
          defaultMessage: "More than {limit} held sources to review",
        },
        { limit: reviewSummaryLimit - 1 },
      )
    : intl.formatMessage(
        {
          id: "review_queue.attention",
          defaultMessage:
            "{count, plural, one {# held source} other {# held sources}} to review",
        },
        { count },
      );
}

/** Count on the navigation item that leads to the decisions. */
export function ReviewAttentionBadge({
  count,
  className,
}: {
  count?: number;
  className?: string;
}) {
  const intl = useIntl();
  if (!count) return null;
  return (
    <Badge className={className}>
      <span aria-hidden="true">
        {count >= reviewSummaryLimit ? `${reviewSummaryLimit - 1}+` : count}
      </span>
      <span className="sr-only">{reviewAttentionLabel(intl, count)}</span>
    </Badge>
  );
}

/** Marks a menu trigger whose menu holds a badged item. Its button names the
 * count in its accessible label. */
export function ReviewAttentionDot({ className }: { className?: string }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        "pointer-events-none absolute top-1.5 right-1.5 size-2 rounded-full bg-primary ring-2 ring-background",
        className,
      )}
    />
  );
}
