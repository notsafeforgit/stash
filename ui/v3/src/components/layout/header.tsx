import { Link } from "@tanstack/react-router";
import { DownloadTray } from "src/components/offline/download-tray";
import { useConfigurationContextOptional } from "src/hooks/config";
import { getAppTitle } from "src/hooks/title";
import { NavLinks } from "./nav-items";
import { UserMenu } from "./user-menu";

export function Header() {
  const config = useConfigurationContextOptional();
  const appTitle = getAppTitle(config?.configuration.ui.title);

  return (
    <header className="hidden md:flex sticky top-0 z-50 h-14 items-center border-b bg-background/95 px-4 backdrop-blur supports-[backdrop-filter]:bg-background/60">
      <Link
        to="/"
        className="mr-4 max-w-64 shrink-0 truncate text-lg font-semibold hover:opacity-80"
      >
        {appTitle}
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
