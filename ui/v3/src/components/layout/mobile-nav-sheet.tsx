import { Link } from "@tanstack/react-router";
import { Home, Settings } from "lucide-react";
import { useIntl } from "react-intl";
import {
  BottomSheet,
  BottomSheetHeader,
  BottomSheetTitle,
} from "@/components/ui/bottom-sheet";
import { buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { useNavItems } from "./nav-items";

interface MobileNavSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function MobileNavSheet({ open, onOpenChange }: MobileNavSheetProps) {
  const intl = useIntl();
  const items = useNavItems({ placement: "main" });
  return (
    <BottomSheet open={open} onOpenChange={onOpenChange}>
      <BottomSheetHeader className="py-2">
        <BottomSheetTitle>
          {intl.formatMessage({
            id: "navigation",
            defaultMessage: "Navigation",
          })}
        </BottomSheetTitle>
      </BottomSheetHeader>
      {/* `activeOptions={{ exact: true, includeSearch: false }}` defeats TanStack Router's
          default prefix-match active behavior. Without it, the Markers
          route (`/scenes/markers`) would also activate the Scenes link
          (`/scenes`) since one path is a prefix of the other. Trade-off:
          scene-detail pages (`/scenes/$sceneId`) no longer highlight the
          Scenes link either. */}
      <nav className="grid min-h-0 grid-cols-4 gap-1 overflow-y-auto px-2 pb-3">
        <Link
          to="/"
          activeOptions={{ exact: true, includeSearch: false }}
          onClick={() => onOpenChange(false)}
          className={cn(
            buttonVariants({ variant: "ghost" }),
            "h-auto flex-col gap-1.5 py-3 text-muted-foreground [&.active]:bg-accent [&.active]:text-foreground [&>svg]:size-6",
          )}
        >
          <Home className="size-6" />
          <span className="text-xs font-medium">
            {intl.formatMessage({ id: "home", defaultMessage: "Home" })}
          </span>
        </Link>
        {items.map((item) => (
          <Link
            key={item.to}
            to={item.to}
            activeOptions={{ exact: true, includeSearch: false }}
            onClick={() => onOpenChange(false)}
            className={cn(
              buttonVariants({ variant: "ghost" }),
              "h-auto flex-col gap-1.5 py-3 text-muted-foreground [&.active]:bg-accent [&.active]:text-foreground [&>svg]:size-6",
            )}
          >
            <span className="[&>svg]:size-6">{item.icon}</span>
            <span className="text-xs font-medium">{item.label}</span>
          </Link>
        ))}
        <Link
          to="/settings"
          activeOptions={{ exact: true, includeSearch: false }}
          onClick={() => onOpenChange(false)}
          className={cn(
            buttonVariants({ variant: "ghost" }),
            "h-auto flex-col gap-1.5 py-3 text-muted-foreground [&.active]:bg-accent [&.active]:text-foreground",
          )}
        >
          <Settings className="size-6" />
          <span className="text-xs font-medium">
            {intl.formatMessage({ id: "settings", defaultMessage: "Settings" })}
          </span>
        </Link>
      </nav>
    </BottomSheet>
  );
}
