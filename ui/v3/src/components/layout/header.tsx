import { Link } from "@tanstack/react-router";
import { NavLinks } from "./nav-items";
import { UserMenu } from "./user-menu";
import { DownloadTray } from "src/components/offline/download-tray";

export function Header() {
  return (
    <header className="hidden md:flex sticky top-0 z-50 h-14 items-center border-b bg-background/95 px-4 backdrop-blur supports-[backdrop-filter]:bg-background/60">
      <Link to="/" className="mr-4 text-lg font-semibold hover:opacity-80">
        Stash
      </Link>

      <nav className="flex flex-1">
        <NavLinks className="flex flex-row gap-0.5" />
      </nav>

      <div className="ml-auto flex items-center gap-1">
        <DownloadTray />
        <UserMenu />
      </div>
    </header>
  );
}
