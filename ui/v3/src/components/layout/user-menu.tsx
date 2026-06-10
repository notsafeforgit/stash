import { Link } from "@tanstack/react-router";
import { BarChart3, Heart, HelpCircle, Settings } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

export function UserMenu() {
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
        <DropdownMenuSeparator />
        <DropdownMenuItem
          render={
            <a
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
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
