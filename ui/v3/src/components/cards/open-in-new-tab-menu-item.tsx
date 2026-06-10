import { useIntl } from "react-intl";
import { ContextMenuItem } from "src/components/ui/context-menu";

interface OpenInNewTabMenuItemProps {
  href: string;
}

/**
 * Drop-in `<ContextMenuItem>` that opens the entity's detail page in a new
 * browser tab. Used across card context menus so users can quickly compare
 * or keep one entity open while browsing others.
 *
 * `noopener,noreferrer` opt the new tab out of `window.opener` access and
 * Referer leakage — same defaults as `<a target="_blank" rel="noopener">`.
 */
export function OpenInNewTabMenuItem({ href }: OpenInNewTabMenuItemProps) {
  const intl = useIntl();
  return (
    <ContextMenuItem
      onClick={() => window.open(href, "_blank", "noopener,noreferrer")}
    >
      {intl.formatMessage({
        id: "actions.open_in_new_tab",
        defaultMessage: "Open in new tab",
      })}
    </ContextMenuItem>
  );
}
