import { useIntl } from "react-intl";

export function PageSummary({
  total,
  page,
  pageSize,
}: {
  total: number;
  page: number;
  pageSize: number;
}) {
  const intl = useIntl();
  const pages = Math.ceil(total / pageSize);
  return (
    <p className="text-sm text-muted-foreground tabular-nums" role="status">
      {intl.formatMessage(
        {
          id: "pagination.summary",
          defaultMessage:
            "Page {page, number} of {pages, number} · {total, plural, one {# item} other {# items}}",
        },
        { page: pages === 0 ? 0 : Math.min(page, pages), pages, total },
      )}
    </p>
  );
}
