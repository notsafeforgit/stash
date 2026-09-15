import { Menu } from "lucide-react";
import { useMsg } from "@/hooks/message";
import { TvIconButton } from "./tv-icon-button";

export function TvNavigationButton({ onClick }: { onClick: () => void }) {
  const msg = useMsg();
  return (
    <TvIconButton
      aria-label={msg("navigation", "Navigation")}
      onClick={onClick}
    >
      <Menu />
    </TvIconButton>
  );
}
