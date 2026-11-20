import { useIntl } from "react-intl";
import { Link } from "@tanstack/react-router";
import { BarChart3, HelpCircle, Settings } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

import { useNavItems } from "./nav-items";
import {
  ReviewAttentionBadge,
  ReviewAttentionDot,
  reviewAttentionLabel,
} from "./review-attention";

export function UserMenu() {
  const intl = useIntl();
  const utilityItems = useNavItems({ placement: "utility" });
  const reviews = utilityItems.reduce((n, item) => n + (item.badge ?? 0), 0);
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            size="icon"
            className="relative"
            aria-label={
              reviews
                ? `More options, ${reviewAttentionLabel(intl, reviews)}`
                : "More options"
            }
          />
        }
      >
        <Settings className="size-4" />
        {reviews > 0 && <ReviewAttentionDot />}
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {utilityItems.length > 0 && (
          <>
            <DropdownMenuGroup>
              {utilityItems.map((item) => (
                <DropdownMenuItem
                  key={item.to}
                  render={<Link to={item.to} search={item.search} />}
                >
                  {item.icon}
                  {item.label}
                  <ReviewAttentionBadge
                    className="ml-auto"
                    count={item.badge}
                  />
                </DropdownMenuItem>
              ))}
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
          </>
        )}
        <DropdownMenuGroup>
          <DropdownMenuItem
            render={<Link to="/stats" />}
            className="flex items-center gap-2"
          >
            <BarChart3 className="size-4" />
            Stats
          </DropdownMenuItem>
          <DropdownMenuItem
            render={<Link to="/settings" />}
            className="flex items-center gap-2"
          >
            <Settings className="size-4" />
            Settings
          </DropdownMenuItem>
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <DropdownMenuItem
            render={
              <a
                aria-label={intl.formatMessage({ id: "help" })}
                href="https://github.com/notsafeforgit/stash/blob/develop/docs/README.md"
                target="_blank"
                rel="noopener noreferrer"
              />
            }
            className="flex items-center gap-2"
          >
            <HelpCircle className="size-4" />
            Help
          </DropdownMenuItem>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
