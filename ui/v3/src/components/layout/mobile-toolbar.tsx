import type { ComponentProps } from "react";
import { useIntl } from "react-intl";
import { Menu } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { useMobileNavigation } from "./mobile-navigation";

/** Every mobile toolbar keeps navigation first, including search and selection. */
export function MobileToolbarRow({
  className,
  children,
  onNavigate,
  ...props
}: ComponentProps<"div"> & { onNavigate?: () => void }) {
  const intl = useIntl();
  const openNavigation = useMobileNavigation();
  return (
    <div
      data-mobile-toolbar-row
      className={cn(
        "mobile-toolbar-row flex h-14 items-center gap-[var(--mobile-toolbar-gap)]",
        className,
      )}
      {...props}
    >
      <Button
        variant="ghost"
        size="icon-lg"
        className="size-11 shrink-0"
        onClick={() => {
          onNavigate?.();
          openNavigation();
        }}
        aria-label={intl.formatMessage({
          id: "accessibility.open_navigation_menu",
          defaultMessage: "Open navigation menu",
        })}
      >
        <Menu />
      </Button>
      {children}
    </div>
  );
}
