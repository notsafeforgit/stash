import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { Menu } from "lucide-react";
import { Button } from "@/components/ui/button";
import { MobileNavSheet } from "./mobile-nav-sheet";
import { useNavItems } from "./nav-items";
import { MobileToolbarRow } from "./mobile-toolbar";

/**
 * Compact bottom nav. Matches the list-page top bar's height + left-aligned
 * hamburger convention so pages without their own toolbar (home, etc.) share the
 * same visual language. The hamburger opens the full top-level page drawer
 * (`MobileNavSheet`); the inline tabs are quick jumps to the most-used
 * primary destinations.
 */
export function BottomTabBar() {
  const allTabs = useNavItems({ placement: "main" });
  // First 3 stay as primary inline tabs; everything else is reachable via
  // the drawer behind the hamburger.
  const primary = allTabs.slice(0, 3);
  const [navOpen, setNavOpen] = useState(false);

  return (
    <>
      <nav className="bottom-tab-bar mobile-safe-footer @container shrink-0 border-t bg-background md:hidden">
        <MobileToolbarRow>
          <Button
            variant="ghost"
            size="icon-lg"
            className="size-11 shrink-0"
            onClick={() => setNavOpen(true)}
            aria-label="Open navigation menu"
          >
            <Menu />
          </Button>

          {primary.map((item) => (
            <Link
              key={item.to}
              to={item.to}
              // Exact match — see nav-items.tsx for the rationale (the
              // Markers route is nested under `/scenes`, so prefix-match
              // would activate Scenes too on `/scenes/markers`).
              // `includeSearch: false` so list pages stay highlighted when
              // their URL carries filter params (e.g. /images?fa=...).
              activeOptions={{ exact: true, includeSearch: false }}
              className="flex min-h-11 min-w-0 flex-1 items-center justify-center gap-1.5 rounded-md text-xs font-medium text-muted-foreground transition-colors hover:text-foreground [&.active]:bg-accent [&.active]:text-foreground"
            >
              {item.icon}
              <span>{item.label}</span>
            </Link>
          ))}
        </MobileToolbarRow>
      </nav>

      <MobileNavSheet open={navOpen} onOpenChange={setNavOpen} />
    </>
  );
}
