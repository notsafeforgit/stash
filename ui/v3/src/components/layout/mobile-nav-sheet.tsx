import { Link } from "@tanstack/react-router";
import { Home, Settings } from "lucide-react";
import { BottomSheet } from "@/components/ui/bottom-sheet";
import { buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { useNavItems } from "./nav-items";

interface MobileNavSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function MobileNavSheet({ open, onOpenChange }: MobileNavSheetProps) {
  const items = useNavItems({ placement: "main" });
  return (
    <BottomSheet open={open} onOpenChange={onOpenChange}>
      {/* `activeOptions={{ exact: true, includeSearch: false }}` defeats TanStack Router's
          default prefix-match active behavior. Without it, the Markers
          route (`/scenes/markers`) would also activate the Scenes link
          (`/scenes`) since one path is a prefix of the other. Trade-off:
          scene-detail pages (`/scenes/$sceneId`) no longer highlight the
          Scenes link either. */}
      <nav className="grid grid-cols-4 gap-1 px-2 pt-4 pb-6">
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
          <span className="text-xs font-medium">Home</span>
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
          <span className="text-xs font-medium">Settings</span>
        </Link>
      </nav>
    </BottomSheet>
  );
}
