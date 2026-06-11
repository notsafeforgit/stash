import { FormattedMessage, useIntl } from "react-intl";
import { ToggleGroup, ToggleGroupItem } from "src/components/ui/toggle-group";

export const DUPLICATE_FILTER_SCOPES = ["all", "any"] as const;

export type DuplicateFilterScope = (typeof DUPLICATE_FILTER_SCOPES)[number];

export const DEFAULT_DUPLICATE_FILTER_SCOPE: DuplicateFilterScope = "all";

export function isDuplicateFilterScope(
  value: unknown,
): value is DuplicateFilterScope {
  const scopes: readonly string[] = DUPLICATE_FILTER_SCOPES;
  return typeof value === "string" && scopes.includes(value);
}

export function DuplicateFilterScopeToggle({
  value,
  onValueChange,
}: {
  value: DuplicateFilterScope;
  onValueChange: (value: DuplicateFilterScope) => void;
}) {
  const intl = useIntl();

  return (
    <label className="flex items-center gap-2 text-sm">
      <span className="text-muted-foreground">
        <FormattedMessage
          id="dupe_check.filter_scope"
          defaultMessage="Filter match"
        />
      </span>
      <ToggleGroup<DuplicateFilterScope>
        value={[value]}
        onValueChange={(values) => {
          const next = values[0];
          if (next) onValueChange(next);
        }}
        variant="outline"
        size="sm"
        aria-label={intl.formatMessage({
          id: "dupe_check.filter_scope_aria",
          defaultMessage: "Duplicate group filter match",
        })}
      >
        <ToggleGroupItem<DuplicateFilterScope>
          value="all"
          aria-label={intl.formatMessage({
            id: "dupe_check.filter_scope_all_aria",
            defaultMessage: "All items in the duplicate group must match",
          })}
        >
          <FormattedMessage
            id="dupe_check.filter_scope_all"
            defaultMessage="All"
          />
        </ToggleGroupItem>
        <ToggleGroupItem<DuplicateFilterScope>
          value="any"
          aria-label={intl.formatMessage({
            id: "dupe_check.filter_scope_any_aria",
            defaultMessage: "Any item in the duplicate group can match",
          })}
        >
          <FormattedMessage
            id="dupe_check.filter_scope_any"
            defaultMessage="Any"
          />
        </ToggleGroupItem>
      </ToggleGroup>
    </label>
  );
}
