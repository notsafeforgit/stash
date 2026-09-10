import { useState } from "react";
import { useIntl } from "react-intl";
import { Link, useRouterState } from "@tanstack/react-router";
import { Check, ChevronUp, Menu, Search } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { MobileNavSheet } from "@/components/layout/mobile-nav-sheet";
import { MobileToolbarRow } from "@/components/layout/mobile-toolbar";
import { MobileSearchRow } from "@/components/layout/mobile-search-row";
import { useMobileKeyboardLayout } from "@/hooks/use-mobile-keyboard-layout";
import { useMobileSearch } from "@/hooks/use-mobile-search";
import { useMediaQuery } from "@/utils/screen";
import { cn } from "@/lib/utils";
import { getSettingsSection, SETTINGS_NAV_ITEMS } from "./settings-navigation";
import {
  SettingsSearchInput,
  SettingsSearchResults,
  useSettingsSearch,
} from "./settings-search";

export function SettingsNav() {
  const intl = useIntl();
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const section = getSettingsSection(pathname);
  const search = useSettingsSearch();
  const mobile = useMediaQuery("(max-width: 767px)");
  if (mobile) return <MobileSettingsNav section={section} search={search} />;

  return (
    <div className="flex w-56 shrink-0 flex-col gap-3 border-r pr-3">
      <div className="flex flex-col gap-1">
        <SettingsSearchInput {...search} />
        {search.query.trim() && (
          <div className="max-h-80 overflow-y-auto rounded-md border bg-card">
            <SettingsSearchResults
              results={search.results}
              onSelect={() => search.setQuery("")}
            />
          </div>
        )}
      </div>
      <nav
        className="flex flex-col gap-1"
        aria-label={intl.formatMessage({
          id: "accessibility.settings_sections",
        })}
      >
        {SETTINGS_NAV_ITEMS.map((item) => (
          <Link
            key={item.to}
            to={item.to}
            aria-current={item === section ? "page" : undefined}
            className={cn(
              "rounded-md px-3 py-2 text-sm font-medium transition-colors",
              item === section
                ? "bg-muted text-foreground"
                : "text-muted-foreground hover:bg-muted/50 hover:text-foreground",
            )}
          >
            {intl.formatMessage({
              id: item.labelId,
              defaultMessage: item.defaultLabel,
            })}
          </Link>
        ))}
      </nav>
    </div>
  );
}

function MobileSettingsNav({
  section,
  search,
}: {
  section: ReturnType<typeof getSettingsSection>;
  search: ReturnType<typeof useSettingsSearch>;
}) {
  const intl = useIntl();
  const [panel, setPanel] = useState<"navigation" | "sections" | null>(null);
  const { isOpen, inputRef, buttonRef, rowRef, openSearch, closeSearch } =
    useMobileSearch();
  const keyboardRef = useMobileKeyboardLayout<HTMLDivElement>();
  const sectionsLabel = intl.formatMessage({
    id: "accessibility.settings_sections",
  });

  return (
    <>
      <MobileNavSheet
        open={panel === "navigation"}
        onOpenChange={(open) => setPanel(open ? "navigation" : null)}
      />
      <div
        ref={keyboardRef}
        data-mobile-settings-footer
        className="mobile-keyboard-layout @container flex shrink-0 flex-col border-t bg-background pb-[env(safe-area-inset-bottom,0px)]"
      >
        <MobileToolbarRow>
          {isOpen ? (
            <MobileSearchRow rowRef={rowRef} onClose={closeSearch}>
              <SettingsSearchInput
                {...search}
                inputRef={inputRef}
                mobile
                onClose={closeSearch}
              />
            </MobileSearchRow>
          ) : (
            <>
              <Button
                variant="ghost"
                size="icon-lg"
                className="size-11 shrink-0"
                onClick={() => setPanel("navigation")}
                aria-label={intl.formatMessage({ id: "navigation" })}
              >
                <Menu />
              </Button>
              <DropdownMenu
                open={panel === "sections"}
                onOpenChange={(open) => setPanel(open ? "sections" : null)}
              >
                <DropdownMenuTrigger
                  render={
                    <Button
                      variant="ghost"
                      className="h-11 min-w-0 flex-1 justify-between px-3"
                      aria-label={sectionsLabel}
                      aria-description={
                        section &&
                        intl.formatMessage({
                          id: section.labelId,
                          defaultMessage: section.defaultLabel,
                        })
                      }
                    />
                  }
                >
                  <span className="truncate">
                    {section
                      ? intl.formatMessage({
                          id: section.labelId,
                          defaultMessage: section.defaultLabel,
                        })
                      : intl.formatMessage({ id: "settings" })}
                  </span>
                  <ChevronUp data-icon="inline-end" />
                </DropdownMenuTrigger>
                <DropdownMenuContent
                  side="top"
                  align="center"
                  className="w-72 max-w-[calc(100vw-1.5rem)] max-h-[min(60svh,var(--available-height))]"
                  aria-label={sectionsLabel}
                >
                  <DropdownMenuGroup>
                    {SETTINGS_NAV_ITEMS.map((item) => (
                      <DropdownMenuItem
                        key={item.to}
                        nativeButton={false}
                        render={
                          <Link
                            to={item.to}
                            aria-current={item === section ? "page" : undefined}
                          />
                        }
                        className="min-h-11 justify-between px-3"
                      >
                        {intl.formatMessage({
                          id: item.labelId,
                          defaultMessage: item.defaultLabel,
                        })}
                        {item === section && <Check />}
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuGroup>
                </DropdownMenuContent>
              </DropdownMenu>
              <Button
                ref={buttonRef}
                variant="ghost"
                size="icon-lg"
                className="size-11 shrink-0"
                onClick={openSearch}
                aria-label={intl.formatMessage({
                  id: "accessibility.search_settings",
                })}
              >
                <Search />
              </Button>
            </>
          )}
        </MobileToolbarRow>
        {isOpen && search.query.trim() && (
          // Results share layout with the bar and take at most half the visible
          // viewport. The settings form stays mounted in the remaining space.
          <div
            className="order-first overflow-y-auto overscroll-contain border-b"
            style={{
              maxHeight:
                "calc((100dvh - var(--mobile-keyboard-inset, 0px)) / 2)",
            }}
          >
            <SettingsSearchResults
              results={search.results}
              mobile
              onSelect={() => {
                search.setQuery("");
                closeSearch();
              }}
            />
          </div>
        )}
      </div>
    </>
  );
}
