import { useIntl } from "react-intl";
import { ContextMenuItem } from "src/components/ui/context-menu";
import { useListContextOptional } from "src/components/list/list-provider";

/**
 * Drop-in `<ContextMenuItem>` that selects every item on the current page.
 * Sourced from the surrounding list context so any card can render it
 * without prop-drilling. No-op (and absent) when there is no list context.
 */
export function SelectAllMenuItem() {
  const { selectable, onSelectAll } = useListContextOptional();
  const intl = useIntl();
  if (!selectable) return null;
  return (
    <ContextMenuItem onClick={onSelectAll}>
      {intl.formatMessage({
        id: "actions.select_all",
        defaultMessage: "Select All",
      })}
    </ContextMenuItem>
  );
}
