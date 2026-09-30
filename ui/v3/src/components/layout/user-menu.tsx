import { useIntl } from "react-intl";
import { Link } from "@tanstack/react-router";
import { BarChart3, Heart, HelpCircle, Settings } from "lucide-react";
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

export function UserMenu() {
  const intl = useIntl();
  const utilityItems = useNavItems({ placement: "utility" });
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button variant="ghost" size="icon" aria-label="More options" />
        }
      >
        <Settings className="size-4" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {utilityItems.length > 0 && (
          <>
            <DropdownMenuGroup>
              {utilityItems.map((item) => (
                <DropdownMenuItem key={item.to} render={<Link to={item.to} />}>
                  {item.icon}
                  {item.label}
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
                href="https://docs.stashapp.cc"
                target="_blank"
                rel="noopener noreferrer"
              />
            }
            className="flex items-center gap-2"
          >
            <HelpCircle className="size-4" />
            Help
          </DropdownMenuItem>
          <DropdownMenuItem
            render={
              <a
                aria-label={intl.formatMessage({ id: "donate" })}
                href="https://opencollective.com/stashapp"
                target="_blank"
                rel="noopener noreferrer"
              />
            }
            className="flex items-center gap-2"
          >
            <Heart className="size-4" />
            Donate
          </DropdownMenuItem>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
