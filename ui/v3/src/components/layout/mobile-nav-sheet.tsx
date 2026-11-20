import { useEffect, useRef } from "react";
import { Link } from "@tanstack/react-router";
import { Home, Settings } from "lucide-react";
import { useIntl } from "react-intl";
import {
  BottomSheet,
  BottomSheetHeader,
  BottomSheetTitle,
} from "@/components/ui/bottom-sheet";
import { Button } from "@/components/ui/button";
import { useNavItems } from "./nav-items";
import { ReviewAttentionBadge } from "./review-attention";

interface MobileNavSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Runs once the closed sheet's content has left the page. */
  onClosed?: () => void;
}

export function MobileNavSheet({
  open,
  onOpenChange,
  onClosed,
}: MobileNavSheetProps) {
  const intl = useIntl();
  const items = useNavItems({ placement: ["main", "mobile", "utility"] });
  return (
    <BottomSheet
      open={open}
      onOpenChange={onOpenChange}
      backdrop="dimmed"
      className="data-ending-style:duration-100"
    >
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
      <nav
        data-mobile-navigation
        className="grid min-h-0 grid-cols-4 gap-1 overflow-y-auto px-2 pb-3"
      >
        <Button
          variant="ghost"
          className="h-auto flex-col gap-1.5 py-3 text-muted-foreground [&.active]:bg-accent [&.active]:text-foreground [&>svg]:size-6"
          render={
            <Link
              to="/"
              preload="viewport"
              activeOptions={{ exact: true, includeSearch: false }}
              onClick={() => onOpenChange(false)}
            />
          }
        >
          <Home className="size-6" />
          <span className="text-xs font-medium">
            {intl.formatMessage({ id: "home", defaultMessage: "Home" })}
          </span>
        </Button>
        {items.map((item) => (
          <Button
            key={item.to}
            variant="ghost"
            className="relative h-auto flex-col gap-1.5 py-3 text-muted-foreground [&.active]:bg-accent [&.active]:text-foreground [&>svg]:size-6"
            render={
              <Link
                to={item.to}
                search={item.search}
                activeOptions={{ exact: true, includeSearch: false }}
                onClick={() => onOpenChange(false)}
              />
            }
          >
            <span className="[&>svg]:size-6">{item.icon}</span>
            <span className="text-center text-xs font-medium wrap-anywhere whitespace-normal">
              {item.label}
            </span>
            <ReviewAttentionBadge
              className="absolute top-1 right-1"
              count={item.badge}
            />
          </Button>
        ))}
        <Button
          variant="ghost"
          className="h-auto flex-col gap-1.5 py-3 text-muted-foreground [&.active]:bg-accent [&.active]:text-foreground"
          render={
            <Link
              to="/settings"
              activeOptions={{ exact: true, includeSearch: false }}
              onClick={() => onOpenChange(false)}
            />
          }
        >
          <Settings className="size-6" />
          <span className="text-xs font-medium">
            {intl.formatMessage({ id: "settings", defaultMessage: "Settings" })}
          </span>
        </Button>
      </nav>
      {onClosed && <UnmountSignal onUnmount={onClosed} />}
    </BottomSheet>
  );
}

// ui-sdk reports a settled close before the closed content unmounts. The token
// skips StrictMode's mount check, which runs cleanup and setup back to back.
function UnmountSignal({ onUnmount }: { onUnmount: () => void }) {
  const mounted = useRef<object>(undefined);
  useEffect(() => {
    const token = {};
    mounted.current = token;
    return () =>
      queueMicrotask(() => {
        if (mounted.current === token) onUnmount();
      });
  }, [onUnmount]);
  return null;
}
