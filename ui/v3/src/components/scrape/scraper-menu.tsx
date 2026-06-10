import { useIntl } from "react-intl";
import { ChevronDown, Sparkles } from "lucide-react";
import * as GQL from "src/core/generated-graphql";
import { Button } from "src/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "src/components/ui/dropdown-menu";
import type {
  AvailableScraper,
  AvailableStashBox,
  ScrapeSource,
} from "./use-available-scrapers";

/**
 * Action requested when a source is picked. NAME (search-style) is preferred
 * over FRAGMENT when the source supports it. URL scraping isn't routed
 * through this menu — those flows trigger from a URL field.
 */
export type ScrapeAction = GQL.ScrapeType.Name | GQL.ScrapeType.Fragment;

function preferredScraperAction(s: AvailableScraper): ScrapeAction | null {
  if (s.supports.includes(GQL.ScrapeType.Name)) return GQL.ScrapeType.Name;
  if (s.supports.includes(GQL.ScrapeType.Fragment))
    return GQL.ScrapeType.Fragment;
  return null;
}

interface ScraperMenuProps {
  scrapers: AvailableScraper[];
  stashBoxes: AvailableStashBox[];
  onPick: (source: ScrapeSource, action: ScrapeAction) => void;
  disabled?: boolean;
}

export function ScraperMenu({
  scrapers,
  stashBoxes,
  onPick,
  disabled,
}: ScraperMenuProps) {
  const intl = useIntl();

  // Skip scrapers whose only supported action is URL — there's no menu-driven
  // entry point for those.
  const usableScrapers = scrapers.filter(
    (s) => preferredScraperAction(s) !== null,
  );

  if (usableScrapers.length === 0 && stashBoxes.length === 0) return null;

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={disabled}
          />
        }
      >
        <Sparkles />
        {intl.formatMessage({
          id: "actions.scrape_with",
          defaultMessage: "Scrape with…",
        })}
        <ChevronDown />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="min-w-[220px]">
        {stashBoxes.length > 0 && (
          <DropdownMenuGroup>
            <DropdownMenuLabel>
              {intl.formatMessage({
                id: "config.stash_boxes",
                defaultMessage: "Stash-boxes",
              })}
            </DropdownMenuLabel>
            {stashBoxes.map((b) => (
              <DropdownMenuItem
                key={b.endpoint}
                onClick={() => onPick(b, GQL.ScrapeType.Name)}
              >
                {b.name}
              </DropdownMenuItem>
            ))}
          </DropdownMenuGroup>
        )}
        {stashBoxes.length > 0 && usableScrapers.length > 0 && (
          <DropdownMenuSeparator />
        )}
        {usableScrapers.length > 0 && (
          <DropdownMenuGroup>
            {stashBoxes.length > 0 && (
              <DropdownMenuLabel>
                {intl.formatMessage({
                  id: "config.scrapers",
                  defaultMessage: "Scrapers",
                })}
              </DropdownMenuLabel>
            )}
            {usableScrapers.map((s) => {
              const action = preferredScraperAction(s)!;
              return (
                <DropdownMenuItem key={s.id} onClick={() => onPick(s, action)}>
                  {s.name}
                </DropdownMenuItem>
              );
            })}
          </DropdownMenuGroup>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
